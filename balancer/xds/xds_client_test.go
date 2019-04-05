// +build go1.12

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

package xds

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	xdscorepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	xdsendpointpb "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	xdsdiscoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	testServiceName = "test/foo"
	testCDSReq      = &xdspb.DiscoveryRequest{
		Node: &xdscorepb.Node{
			Metadata: &types.Struct{
				Fields: map[string]*types.Value{
					grpcHostname: {
						Kind: &types.Value_StringValue{StringValue: testServiceName},
					},
				},
			},
		},
		TypeUrl: cdsType,
	}
	testEDSReq = &xdspb.DiscoveryRequest{
		Node: &xdscorepb.Node{
			Metadata: &types.Struct{
				Fields: map[string]*types.Value{
					endpointRequired: {
						Kind: &types.Value_BoolValue{BoolValue: true},
					},
				},
			},
		},
		ResourceNames: []string{testServiceName},
		TypeUrl:       edsType,
	}
	testEDSReqWithoutEndpoints = &xdspb.DiscoveryRequest{
		Node: &xdscorepb.Node{
			Metadata: &types.Struct{
				Fields: map[string]*types.Value{
					endpointRequired: {
						Kind: &types.Value_BoolValue{BoolValue: false},
					},
				},
			},
		},
		ResourceNames: []string{testServiceName},
		TypeUrl:       edsType,
	}
	testCluster = &xdspb.Cluster{
		Name:                 testServiceName,
		ClusterDiscoveryType: &xdspb.Cluster_Type{Type: xdspb.Cluster_EDS},
		LbPolicy:             xdspb.Cluster_ROUND_ROBIN,
	}
	marshaledCluster, _ = proto.Marshal(testCluster)
	testCDSResp         = &xdspb.DiscoveryResponse{
		Resources: []types.Any{
			{
				TypeUrl: cdsType,
				Value:   marshaledCluster,
			},
		},
		TypeUrl: cdsType,
	}
	testClusterLoadAssignment = &xdspb.ClusterLoadAssignment{
		ClusterName: testServiceName,
		Endpoints: []xdsendpointpb.LocalityLbEndpoints{
			{
				Locality: &xdscorepb.Locality{
					Region:  "asia-east1",
					Zone:    "1",
					SubZone: "sa",
				},
				LbEndpoints: []xdsendpointpb.LbEndpoint{
					{
						HostIdentifier: &xdsendpointpb.LbEndpoint_Endpoint{
							Endpoint: &xdsendpointpb.Endpoint{
								Address: &xdscorepb.Address{
									Address: &xdscorepb.Address_SocketAddress{
										SocketAddress: &xdscorepb.SocketAddress{
											Address: "1.1.1.1",
											PortSpecifier: &xdscorepb.SocketAddress_PortValue{
												PortValue: 10001,
											},
											ResolverName: "dns",
										},
									},
								},
								HealthCheckConfig: nil,
							},
						},
						Metadata: &xdscorepb.Metadata{
							FilterMetadata: map[string]*types.Struct{
								"xx.lb": {
									Fields: map[string]*types.Value{
										"endpoint_name": {
											Kind: &types.Value_StringValue{
												StringValue: "some.endpoint.name",
											},
										},
									},
								},
							},
						},
					},
				},
				LoadBalancingWeight: &types.UInt32Value{
					Value: 1,
				},
				Priority: 0,
			},
		},
	}
	marshaledClusterLoadAssignment, _ = proto.Marshal(testClusterLoadAssignment)
	testEDSResp                       = &xdspb.DiscoveryResponse{
		Resources: []types.Any{
			{
				TypeUrl: edsType,
				Value:   marshaledClusterLoadAssignment,
			},
		},
		TypeUrl: edsType,
	}
	testClusterLoadAssignmentWithoutEndpoints = &xdspb.ClusterLoadAssignment{
		ClusterName: testServiceName,
		Endpoints: []xdsendpointpb.LocalityLbEndpoints{
			{
				Locality: &xdscorepb.Locality{
					SubZone: "sa",
				},
				LoadBalancingWeight: &types.UInt32Value{
					Value: 128,
				},
				Priority: 0,
			},
		},
		Policy: nil,
	}
	marshaledClusterLoadAssignmentWithoutEndpoints, _ = proto.Marshal(testClusterLoadAssignmentWithoutEndpoints)
	testEDSRespWithoutEndpoints                       = &xdspb.DiscoveryResponse{
		Resources: []types.Any{
			{
				TypeUrl: edsType,
				Value:   marshaledClusterLoadAssignmentWithoutEndpoints,
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

func (ttd *testTrafficDirector) StreamAggregatedResources(s xdsdiscoverypb.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
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

func (ttd *testTrafficDirector) DeltaAggregatedResources(xdsdiscoverypb.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
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
	doCDS                bool
	expectedRequests     []*xdspb.DiscoveryRequest
	responsesToSend      []*xdspb.DiscoveryResponse
	expectedADSResponses []proto.Message
	adsErr               error
	svrErr               error
}

func setupServer(t *testing.T) (addr string, td *testTrafficDirector, cleanup func()) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen failed due to: %v", err)
	}
	svr := grpc.NewServer()
	td = newTestTrafficDirector()
	xdsdiscoverypb.RegisterAggregatedDiscoveryServiceServer(svr, td)
	go svr.Serve(lis)
	return lis.Addr().String(), td, func() {
		svr.Stop()
		lis.Close()
	}
}

func (s) TestXdsClientResponseHandling(t *testing.T) {
	for _, test := range []*testConfig{
		{
			doCDS:                true,
			expectedRequests:     []*xdspb.DiscoveryRequest{testCDSReq, testEDSReq},
			responsesToSend:      []*xdspb.DiscoveryResponse{testCDSResp, testEDSResp},
			expectedADSResponses: []proto.Message{testCluster, testClusterLoadAssignment},
		},
		{
			doCDS:                false,
			expectedRequests:     []*xdspb.DiscoveryRequest{testEDSReqWithoutEndpoints},
			responsesToSend:      []*xdspb.DiscoveryResponse{testEDSRespWithoutEndpoints},
			expectedADSResponses: []proto.Message{testClusterLoadAssignmentWithoutEndpoints},
		},
	} {
		testXdsClientResponseHandling(t, test)
	}
}

func testXdsClientResponseHandling(t *testing.T, test *testConfig) {
	addr, td, cleanup := setupServer(t)
	defer cleanup()
	adsChan := make(chan proto.Message, 10)
	newADS := func(ctx context.Context, i proto.Message) error {
		adsChan <- i
		return nil
	}
	client := newXDSClient(addr, testServiceName, test.doCDS, balancer.BuildOptions{}, newADS, func(context.Context) {}, func() {})
	defer client.close()
	go client.run()

	for _, expectedReq := range test.expectedRequests {
		req := td.getReq()
		if req.err != nil {
			t.Fatalf("ads RPC failed with err: %v", req.err)
		}
		if !proto.Equal(req.req, expectedReq) {
			t.Fatalf("got ADS request %T %v, expected: %T %v", req.req, req.req, expectedReq, expectedReq)
		}
	}

	for i, resp := range test.responsesToSend {
		td.sendResp(&response{resp: resp})
		ads := <-adsChan
		if !proto.Equal(ads, test.expectedADSResponses[i]) {
			t.Fatalf("received unexpected ads response, got %v, want %v", ads, test.expectedADSResponses[i])
		}
	}
}

func (s) TestXdsClientLoseContact(t *testing.T) {
	for _, test := range []*testConfig{
		{
			doCDS:           true,
			responsesToSend: []*xdspb.DiscoveryResponse{},
		},
		{
			doCDS:           false,
			responsesToSend: []*xdspb.DiscoveryResponse{testEDSRespWithoutEndpoints},
		},
	} {
		testXdsClientLoseContactRemoteClose(t, test)
	}

	for _, test := range []*testConfig{
		{
			doCDS:           false,
			responsesToSend: []*xdspb.DiscoveryResponse{testCDSResp}, // CDS response when in custom mode.
		},
		{
			doCDS:           true,
			responsesToSend: []*xdspb.DiscoveryResponse{{}}, // response with 0 resources is an error case.
		},
		{
			doCDS:           true,
			responsesToSend: []*xdspb.DiscoveryResponse{testCDSResp},
			adsErr:          errors.New("some ads parsing error from xdsBalancer"),
		},
	} {
		testXdsClientLoseContactADSRelatedErrorOccur(t, test)
	}
}

func testXdsClientLoseContactRemoteClose(t *testing.T, test *testConfig) {
	addr, td, cleanup := setupServer(t)
	defer cleanup()
	adsChan := make(chan proto.Message, 10)
	newADS := func(ctx context.Context, i proto.Message) error {
		adsChan <- i
		return nil
	}
	contactChan := make(chan *loseContact, 10)
	loseContactFunc := func(context.Context) {
		contactChan <- &loseContact{}
	}
	client := newXDSClient(addr, testServiceName, test.doCDS, balancer.BuildOptions{}, newADS, loseContactFunc, func() {})
	defer client.close()
	go client.run()

	// make sure server side get the request (i.e stream created successfully on client side)
	td.getReq()

	for _, resp := range test.responsesToSend {
		td.sendResp(&response{resp: resp})
		// make sure client side receives it
		<-adsChan
	}
	cleanup()

	select {
	case <-contactChan:
	case <-time.After(2 * time.Second):
		t.Fatal("time out when expecting lost contact signal")
	}
}

func testXdsClientLoseContactADSRelatedErrorOccur(t *testing.T, test *testConfig) {
	addr, td, cleanup := setupServer(t)
	defer cleanup()

	adsChan := make(chan proto.Message, 10)
	newADS := func(ctx context.Context, i proto.Message) error {
		adsChan <- i
		return test.adsErr
	}
	contactChan := make(chan *loseContact, 10)
	loseContactFunc := func(context.Context) {
		contactChan <- &loseContact{}
	}
	client := newXDSClient(addr, testServiceName, test.doCDS, balancer.BuildOptions{}, newADS, loseContactFunc, func() {})
	defer client.close()
	go client.run()

	// make sure server side get the request (i.e stream created successfully on client side)
	td.getReq()

	for _, resp := range test.responsesToSend {
		td.sendResp(&response{resp: resp})
	}

	select {
	case <-contactChan:
	case <-time.After(2 * time.Second):
		t.Fatal("time out when expecting lost contact signal")
	}
}

func (s) TestXdsClientExponentialRetry(t *testing.T) {
	cfg := &testConfig{
		svrErr: status.Errorf(codes.Aborted, "abort the stream to trigger retry"),
	}
	addr, td, cleanup := setupServer(t)
	defer cleanup()

	adsChan := make(chan proto.Message, 10)
	newADS := func(ctx context.Context, i proto.Message) error {
		adsChan <- i
		return nil
	}
	contactChan := make(chan *loseContact, 10)
	loseContactFunc := func(context.Context) {
		contactChan <- &loseContact{}
	}
	client := newXDSClient(addr, testServiceName, cfg.doCDS, balancer.BuildOptions{}, newADS, loseContactFunc, func() {})
	defer client.close()
	go client.run()

	var secondRetry, thirdRetry time.Time
	for i := 0; i < 3; i++ {
		// make sure server side get the request (i.e stream created successfully on client side)
		td.getReq()
		td.sendResp(&response{err: cfg.svrErr})

		select {
		case <-contactChan:
			if i == 1 {
				secondRetry = time.Now()
			}
			if i == 2 {
				thirdRetry = time.Now()
			}
		case <-time.After(2 * time.Second):
			t.Fatal("time out when expecting lost contact signal")
		}
	}
	if thirdRetry.Sub(secondRetry) < 1*time.Second {
		t.Fatalf("interval between second and third retry is %v, expected > 1s", thirdRetry.Sub(secondRetry))
	}
}
