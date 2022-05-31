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

package controller

import (
	"context"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/pubsub"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/testing/protocmp"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	basepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	routepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	httppb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	anypb "github.com/golang/protobuf/ptypes/any"
	structpb "github.com/golang/protobuf/ptypes/struct"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	goodLDSTarget1           = "lds.target.good:1111"
	goodLDSTarget2           = "lds.target.good:2222"
	goodRouteName1           = "GoodRouteConfig1"
	goodRouteName2           = "GoodRouteConfig2"
	goodEDSName              = "GoodClusterAssignment1"
	uninterestingDomain      = "uninteresting.domain"
	goodClusterName1         = "GoodClusterName1"
	goodClusterName2         = "GoodClusterName2"
	uninterestingClusterName = "UninterestingClusterName"
	httpConnManagerURL       = "type.googleapis.com/envoy.config.filter.network.http_connection_manager.v2.HttpConnectionManager"
)

var (
	goodNodeProto = &basepb.Node{
		Id: "ENVOY_NODE_ID",
		Metadata: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"TRAFFICDIRECTOR_GRPC_HOSTNAME": {
					Kind: &structpb.Value_StringValue{StringValue: "trafficdirector"},
				},
			},
		},
	}
	goodLDSRequest = &xdspb.DiscoveryRequest{
		Node:          goodNodeProto,
		TypeUrl:       version.V2ListenerURL,
		ResourceNames: []string{goodLDSTarget1},
	}
	goodRDSRequest = &xdspb.DiscoveryRequest{
		Node:          goodNodeProto,
		TypeUrl:       version.V2RouteConfigURL,
		ResourceNames: []string{goodRouteName1},
	}
	goodCDSRequest = &xdspb.DiscoveryRequest{
		Node:          goodNodeProto,
		TypeUrl:       version.V2ClusterURL,
		ResourceNames: []string{goodClusterName1},
	}
	goodEDSRequest = &xdspb.DiscoveryRequest{
		Node:          goodNodeProto,
		TypeUrl:       version.V2EndpointsURL,
		ResourceNames: []string{goodEDSName},
	}
	goodHTTPConnManager1 = &httppb.HttpConnectionManager{
		RouteSpecifier: &httppb.HttpConnectionManager_Rds{
			Rds: &httppb.Rds{
				ConfigSource: &basepb.ConfigSource{
					ConfigSourceSpecifier: &basepb.ConfigSource_Ads{Ads: &basepb.AggregatedConfigSource{}},
				},
				RouteConfigName: goodRouteName1,
			},
		},
	}
	marshaledConnMgr1 = testutils.MarshalAny(goodHTTPConnManager1)
	goodListener1     = &xdspb.Listener{
		Name: goodLDSTarget1,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: marshaledConnMgr1,
		},
	}
	marshaledListener1 = testutils.MarshalAny(goodListener1)
	goodListener2      = &xdspb.Listener{
		Name: goodLDSTarget2,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: marshaledConnMgr1,
		},
	}
	marshaledListener2     = testutils.MarshalAny(goodListener2)
	noAPIListener          = &xdspb.Listener{Name: goodLDSTarget1}
	marshaledNoAPIListener = testutils.MarshalAny(noAPIListener)
	badAPIListener2        = &xdspb.Listener{
		Name: goodLDSTarget2,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   []byte{1, 2, 3, 4},
			},
		},
	}
	badlyMarshaledAPIListener2, _ = proto.Marshal(badAPIListener2)
	goodLDSResponse1              = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			marshaledListener1,
		},
		TypeUrl: version.V2ListenerURL,
	}
	goodLDSResponse2 = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			marshaledListener2,
		},
		TypeUrl: version.V2ListenerURL,
	}
	emptyLDSResponse          = &xdspb.DiscoveryResponse{TypeUrl: version.V2ListenerURL}
	badlyMarshaledLDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: version.V2ListenerURL,
				Value:   []byte{1, 2, 3, 4},
			},
		},
		TypeUrl: version.V2ListenerURL,
	}
	badResourceTypeInLDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{marshaledConnMgr1},
		TypeUrl:   version.V2ListenerURL,
	}
	ldsResponseWithMultipleResources = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			marshaledListener2,
			marshaledListener1,
		},
		TypeUrl: version.V2ListenerURL,
	}
	noAPIListenerLDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{marshaledNoAPIListener},
		TypeUrl:   version.V2ListenerURL,
	}
	goodBadUglyLDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			marshaledListener2,
			marshaledListener1,
			{
				TypeUrl: version.V2ListenerURL,
				Value:   badlyMarshaledAPIListener2,
			},
		},
		TypeUrl: version.V2ListenerURL,
	}
	badlyMarshaledRDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: version.V2RouteConfigURL,
				Value:   []byte{1, 2, 3, 4},
			},
		},
		TypeUrl: version.V2RouteConfigURL,
	}
	badResourceTypeInRDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{marshaledConnMgr1},
		TypeUrl:   version.V2RouteConfigURL,
	}
	noVirtualHostsRouteConfig = &xdspb.RouteConfiguration{
		Name: goodRouteName1,
	}
	marshaledNoVirtualHostsRouteConfig = testutils.MarshalAny(noVirtualHostsRouteConfig)
	noVirtualHostsInRDSResponse        = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			marshaledNoVirtualHostsRouteConfig,
		},
		TypeUrl: version.V2RouteConfigURL,
	}
	goodRouteConfig1 = &xdspb.RouteConfiguration{
		Name: goodRouteName1,
		VirtualHosts: []*routepb.VirtualHost{
			{
				Domains: []string{uninterestingDomain},
				Routes: []*routepb.Route{
					{
						Match: &routepb.RouteMatch{PathSpecifier: &routepb.RouteMatch_Prefix{Prefix: ""}},
						Action: &routepb.Route_Route{
							Route: &routepb.RouteAction{
								ClusterSpecifier: &routepb.RouteAction_Cluster{Cluster: uninterestingClusterName},
							},
						},
					},
				},
			},
			{
				Domains: []string{goodLDSTarget1},
				Routes: []*routepb.Route{
					{
						Match: &routepb.RouteMatch{PathSpecifier: &routepb.RouteMatch_Prefix{Prefix: ""}},
						Action: &routepb.Route_Route{
							Route: &routepb.RouteAction{
								ClusterSpecifier: &routepb.RouteAction_Cluster{Cluster: goodClusterName1},
							},
						},
					},
				},
			},
		},
	}
	marshaledGoodRouteConfig1 = testutils.MarshalAny(goodRouteConfig1)
	goodRouteConfig2          = &xdspb.RouteConfiguration{
		Name: goodRouteName2,
		VirtualHosts: []*routepb.VirtualHost{
			{
				Domains: []string{uninterestingDomain},
				Routes: []*routepb.Route{
					{
						Match: &routepb.RouteMatch{PathSpecifier: &routepb.RouteMatch_Prefix{Prefix: ""}},
						Action: &routepb.Route_Route{
							Route: &routepb.RouteAction{
								ClusterSpecifier: &routepb.RouteAction_Cluster{Cluster: uninterestingClusterName},
							},
						},
					},
				},
			},
			{
				Domains: []string{goodLDSTarget1},
				Routes: []*routepb.Route{
					{
						Match: &routepb.RouteMatch{PathSpecifier: &routepb.RouteMatch_Prefix{Prefix: ""}},
						Action: &routepb.Route_Route{
							Route: &routepb.RouteAction{
								ClusterSpecifier: &routepb.RouteAction_Cluster{Cluster: goodClusterName2},
							},
						},
					},
				},
			},
		},
	}
	marshaledGoodRouteConfig2 = testutils.MarshalAny(goodRouteConfig2)
	goodRDSResponse1          = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			marshaledGoodRouteConfig1,
		},
		TypeUrl: version.V2RouteConfigURL,
	}
	goodRDSResponse2 = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			marshaledGoodRouteConfig2,
		},
		TypeUrl: version.V2RouteConfigURL,
	}
)

type watchHandleTestcase struct {
	rType        xdsresource.ResourceType
	resourceName string

	responseToHandle *xdspb.DiscoveryResponse
	wantHandleErr    bool
	wantUpdate       interface{}
	wantUpdateMD     xdsresource.UpdateMetadata
	wantUpdateErr    bool
}

type testUpdateReceiver struct {
	f func(rType xdsresource.ResourceType, d map[string]interface{}, md xdsresource.UpdateMetadata)
}

func (t *testUpdateReceiver) NewListeners(d map[string]xdsresource.ListenerUpdateErrTuple, metadata xdsresource.UpdateMetadata) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(xdsresource.ListenerResource, dd, metadata)
}

func (t *testUpdateReceiver) NewRouteConfigs(d map[string]xdsresource.RouteConfigUpdateErrTuple, metadata xdsresource.UpdateMetadata) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(xdsresource.RouteConfigResource, dd, metadata)
}

func (t *testUpdateReceiver) NewClusters(d map[string]xdsresource.ClusterUpdateErrTuple, metadata xdsresource.UpdateMetadata) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(xdsresource.ClusterResource, dd, metadata)
}

func (t *testUpdateReceiver) NewEndpoints(d map[string]xdsresource.EndpointsUpdateErrTuple, metadata xdsresource.UpdateMetadata) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(xdsresource.EndpointsResource, dd, metadata)
}

func (t *testUpdateReceiver) NewConnectionError(error) {}

func (t *testUpdateReceiver) newUpdate(rType xdsresource.ResourceType, d map[string]interface{}, metadata xdsresource.UpdateMetadata) {
	t.f(rType, d, metadata)
}

// testWatchHandle is called to test response handling for each xDS.
//
// It starts the xDS watch as configured in test, waits for the fake xds server
// to receive the request (so watch callback is installed), and calls
// handleXDSResp with responseToHandle (if it's set). It then compares the
// update received by watch callback with the expected results.
func testWatchHandle(t *testing.T, test *watchHandleTestcase) {
	t.Helper()

	fakeServer, cleanup := startServer(t)
	defer cleanup()

	type updateErr struct {
		u   interface{}
		md  xdsresource.UpdateMetadata
		err error
	}
	gotUpdateCh := testutils.NewChannel()

	v2c, err := newTestController(&testUpdateReceiver{
		f: func(rType xdsresource.ResourceType, d map[string]interface{}, md xdsresource.UpdateMetadata) {
			if rType == test.rType {
				switch test.rType {
				case xdsresource.ListenerResource:
					dd := make(map[string]xdsresource.ListenerUpdateErrTuple)
					for n, u := range d {
						dd[n] = u.(xdsresource.ListenerUpdateErrTuple)
					}
					gotUpdateCh.Send(updateErr{dd, md, nil})
				case xdsresource.RouteConfigResource:
					dd := make(map[string]xdsresource.RouteConfigUpdateErrTuple)
					for n, u := range d {
						dd[n] = u.(xdsresource.RouteConfigUpdateErrTuple)
					}
					gotUpdateCh.Send(updateErr{dd, md, nil})
				case xdsresource.ClusterResource:
					dd := make(map[string]xdsresource.ClusterUpdateErrTuple)
					for n, u := range d {
						dd[n] = u.(xdsresource.ClusterUpdateErrTuple)
					}
					gotUpdateCh.Send(updateErr{dd, md, nil})
				case xdsresource.EndpointsResource:
					dd := make(map[string]xdsresource.EndpointsUpdateErrTuple)
					for n, u := range d {
						dd[n] = u.(xdsresource.EndpointsUpdateErrTuple)
					}
					gotUpdateCh.Send(updateErr{dd, md, nil})
				}
			}
		},
	}, fakeServer.Address, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v2c.Close()

	// Register the watcher, this will also trigger the v2Client to send the xDS
	// request.
	v2c.AddWatch(test.rType, test.resourceName)

	// Wait till the request makes it to the fakeServer. This ensures that
	// the watch request has been processed by the v2Client.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := fakeServer.XDSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for an xDS request: %v", err)
	}

	// Directly push the response through a call to handleXDSResp. This bypasses
	// the fakeServer, so it's only testing the handle logic. Client response
	// processing is covered elsewhere.
	//
	// Also note that this won't trigger ACK, so there's no need to clear the
	// request channel afterwards.
	if _, _, _, err := v2c.handleResponse(test.responseToHandle); (err != nil) != test.wantHandleErr {
		t.Fatalf("v2c.handleRDSResponse() returned err: %v, wantErr: %v", err, test.wantHandleErr)
	}

	wantUpdate := test.wantUpdate
	cmpOpts := cmp.Options{
		cmpopts.EquateEmpty(), protocmp.Transform(),
		cmpopts.IgnoreFields(xdsresource.UpdateMetadata{}, "Timestamp"),
		cmpopts.IgnoreFields(xdsresource.UpdateErrorMetadata{}, "Timestamp"),
		cmp.FilterValues(func(x, y error) bool { return true }, cmpopts.EquateErrors()),
	}
	uErr, err := gotUpdateCh.Receive(ctx)
	if err == context.DeadlineExceeded {
		t.Fatal("Timeout expecting xDS update")
	}
	gotUpdate := uErr.(updateErr).u
	if diff := cmp.Diff(gotUpdate, wantUpdate, cmpOpts); diff != "" {
		t.Fatalf("got update : %+v, want %+v, diff: %s", gotUpdate, wantUpdate, diff)
	}
	gotUpdateMD := uErr.(updateErr).md
	if diff := cmp.Diff(gotUpdateMD, test.wantUpdateMD, cmpOpts); diff != "" {
		t.Fatalf("got update : %+v, want %+v, diff: %s", gotUpdateMD, test.wantUpdateMD, diff)
	}
	gotUpdateErr := uErr.(updateErr).err
	if (gotUpdateErr != nil) != test.wantUpdateErr {
		t.Fatalf("got xDS update error {%v}, wantErr: %v", gotUpdateErr, test.wantUpdateErr)
	}
}

// startServer starts a fake XDS server and also returns a ClientConn
// connected to it.
func startServer(t *testing.T) (*fakeserver.Server, func()) {
	t.Helper()
	fs, sCleanup, err := fakeserver.StartServer()
	if err != nil {
		t.Fatalf("Failed to start fake xDS server: %v", err)
	}
	return fs, sCleanup
}

func newTestController(p pubsub.UpdateHandler, controlPlanAddr string, n *basepb.Node, b func(int) time.Duration, l *grpclog.PrefixLogger) (*Controller, error) {
	c, err := New(&bootstrap.ServerConfig{
		ServerURI:    controlPlanAddr,
		Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
		TransportAPI: version.TransportV2,
		NodeProto:    n,
	}, p, nil, l, b)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func newStringP(s string) *string {
	return &s
}
