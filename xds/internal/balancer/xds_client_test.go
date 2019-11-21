/*
 *
 * Copyright 2019 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package balancer

import (
	"context"
	"io"
	"net"
	"testing"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	endpointpb "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	xdsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	lrsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/load_stats/v2"
	"github.com/golang/protobuf/proto"
	anypb "github.com/golang/protobuf/ptypes/any"
	durationpb "github.com/golang/protobuf/ptypes/duration"
	structpb "github.com/golang/protobuf/ptypes/struct"
	wrpb "github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/status"
	xdsclient "google.golang.org/grpc/xds/internal/client"
)

const (
	edsType = "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment"
)

var (
	testServiceName           = "test/foo"
	testEDSClusterName        = "test/service/eds"
	testClusterLoadAssignment = &xdspb.ClusterLoadAssignment{
		ClusterName: testEDSClusterName,
		Endpoints: []*endpointpb.LocalityLbEndpoints{{
			Locality: &corepb.Locality{
				Region:  "asia-east1",
				Zone:    "1",
				SubZone: "sa",
			},
			LbEndpoints: []*endpointpb.LbEndpoint{{
				HostIdentifier: &endpointpb.LbEndpoint_Endpoint{
					Endpoint: &endpointpb.Endpoint{
						Address: &corepb.Address{
							Address: &corepb.Address_SocketAddress{
								SocketAddress: &corepb.SocketAddress{
									Address: "1.1.1.1",
									PortSpecifier: &corepb.SocketAddress_PortValue{
										PortValue: 10001,
									},
									ResolverName: "dns",
								},
							},
						},
						HealthCheckConfig: nil,
					},
				},
				Metadata: &corepb.Metadata{
					FilterMetadata: map[string]*structpb.Struct{
						"xx.lb": {
							Fields: map[string]*structpb.Value{
								"endpoint_name": {
									Kind: &structpb.Value_StringValue{
										StringValue: "some.endpoint.name",
									},
								},
							},
						},
					},
				},
			}},
			LoadBalancingWeight: &wrpb.UInt32Value{
				Value: 1,
			},
			Priority: 0,
		}},
	}
	marshaledClusterLoadAssignment, _ = proto.Marshal(testClusterLoadAssignment)
	testEDSResp                       = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: edsType,
				Value:   marshaledClusterLoadAssignment,
			},
		},
		TypeUrl: edsType,
	}
)

type testTrafficDirector struct {
	reqChan  chan *request
	respChan chan *response
}

type request struct {
	req *xdspb.DiscoveryRequest
	err error
}

type response struct {
	resp *xdspb.DiscoveryResponse
	err  error
}

func (ttd *testTrafficDirector) StreamAggregatedResources(s xdsgrpc.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	for {
		req, err := s.Recv()
		if err != nil {
			ttd.reqChan <- &request{
				req: nil,
				err: err,
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
		ttd.reqChan <- &request{
			req: req,
			err: nil,
		}
		if req.TypeUrl == edsType {
			break
		}
	}

	for {
		select {
		case resp := <-ttd.respChan:
			if resp.err != nil {
				return resp.err
			}
			if err := s.Send(resp.resp); err != nil {
				return err
			}
		case <-s.Context().Done():
			return s.Context().Err()
		}
	}
}

func (ttd *testTrafficDirector) DeltaAggregatedResources(xdsgrpc.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	return status.Error(codes.Unimplemented, "")
}

func (ttd *testTrafficDirector) sendResp(resp *response) {
	ttd.respChan <- resp
}

func (ttd *testTrafficDirector) getReq() *request {
	return <-ttd.reqChan
}

func newTestTrafficDirector() *testTrafficDirector {
	return &testTrafficDirector{
		reqChan:  make(chan *request, 10),
		respChan: make(chan *response, 10),
	}
}

type testConfig struct {
	edsServiceName       string
	expectedRequests     []*xdspb.DiscoveryRequest
	responsesToSend      []*xdspb.DiscoveryResponse
	expectedADSResponses []proto.Message
}

func setupServer(t *testing.T) (addr string, td *testTrafficDirector, lrss *lrsServer, cleanup func()) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen failed due to: %v", err)
	}
	svr := grpc.NewServer()
	td = newTestTrafficDirector()
	lrss = &lrsServer{
		drops: make(map[string]uint64),
		reportingInterval: &durationpb.Duration{
			Seconds: 60 * 60, // 1 hour, each test can override this to a shorter duration.
			Nanos:   0,
		},
	}
	xdsgrpc.RegisterAggregatedDiscoveryServiceServer(svr, td)
	lrsgrpc.RegisterLoadReportingServiceServer(svr, lrss)
	go svr.Serve(lis)
	return lis.Addr().String(), td, lrss, func() {
		svr.Stop()
		lis.Close()
	}
}

func (s) TestXdsClientResponseHandling(t *testing.T) {
	for _, test := range []*testConfig{
		{
			// Test that if clusterName is not set, dialing target is used.
			expectedRequests: []*xdspb.DiscoveryRequest{{
				TypeUrl:       edsType,
				ResourceNames: []string{testServiceName}, // ResourceName is dialing target.
				Node:          &corepb.Node{},
			}},
		},
		{
			edsServiceName: testEDSClusterName,
			expectedRequests: []*xdspb.DiscoveryRequest{{
				TypeUrl:       edsType,
				ResourceNames: []string{testEDSClusterName},
				Node:          &corepb.Node{},
			}},
			responsesToSend:      []*xdspb.DiscoveryResponse{testEDSResp},
			expectedADSResponses: []proto.Message{testClusterLoadAssignment},
		},
	} {
		testXdsClientResponseHandling(t, test)
	}
}

func testXdsClientResponseHandling(t *testing.T, test *testConfig) {
	addr, td, _, cleanup := setupServer(t)
	defer cleanup()
	adsChan := make(chan *xdsclient.EDSUpdate, 10)
	newADS := func(ctx context.Context, i *xdsclient.EDSUpdate) error {
		adsChan <- i
		return nil
	}
	client := newXDSClientWrapper(addr, test.edsServiceName, balancer.BuildOptions{Target: resolver.Target{Endpoint: testServiceName}}, "", nil, newADS, func(context.Context) {}, func() {})
	defer client.close()

	for _, expectedReq := range test.expectedRequests {
		req := td.getReq()
		if req.err != nil {
			t.Fatalf("ads RPC failed with err: %v", req.err)
		}
		if !proto.Equal(req.req, expectedReq) {
			t.Fatalf("got ADS request %T, expected: %T, diff: %s", req.req, expectedReq, cmp.Diff(req.req, expectedReq, cmp.Comparer(proto.Equal)))
		}
	}

	for i, resp := range test.responsesToSend {
		td.sendResp(&response{resp: resp})
		ads := <-adsChan
		want, err := xdsclient.ParseEDSRespProto(test.expectedADSResponses[i].(*xdspb.ClusterLoadAssignment))
		if err != nil {
			t.Fatalf("parsing wanted EDS response failed: %v", err)
		}
		if !cmp.Equal(ads, want) {
			t.Fatalf("received unexpected ads response, got %v, want %v", ads, test.expectedADSResponses[i])
		}
	}
}
