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

package client

import (
	"errors"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	basepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	routepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	httppb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	anypb "github.com/golang/protobuf/ptypes/any"
	structpb "github.com/golang/protobuf/ptypes/struct"
)

const (
	defaultTestTimeout       = 1 * time.Second
	goodLDSTarget1           = "lds.target.good:1111"
	goodLDSTarget2           = "lds.target.good:2222"
	goodRouteName1           = "GoodRouteConfig1"
	goodRouteName2           = "GoodRouteConfig2"
	goodEDSName              = "GoodClusterAssignment1"
	uninterestingRouteName   = "UninterestingRouteName"
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
		TypeUrl:       ldsURL,
		ResourceNames: []string{goodLDSTarget1},
	}
	goodRDSRequest = &xdspb.DiscoveryRequest{
		Node:          goodNodeProto,
		TypeUrl:       rdsURL,
		ResourceNames: []string{goodRouteName1},
	}
	goodCDSRequest = &xdspb.DiscoveryRequest{
		Node:          goodNodeProto,
		TypeUrl:       cdsURL,
		ResourceNames: []string{goodClusterName1},
	}
	goodEDSRequest = &xdspb.DiscoveryRequest{
		Node:          goodNodeProto,
		TypeUrl:       edsURL,
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
	marshaledConnMgr1, _ = proto.Marshal(goodHTTPConnManager1)
	emptyHTTPConnManager = &httppb.HttpConnectionManager{
		RouteSpecifier: &httppb.HttpConnectionManager_Rds{
			Rds: &httppb.Rds{},
		},
	}
	emptyMarshaledConnMgr, _ = proto.Marshal(emptyHTTPConnManager)
	connMgrWithScopedRoutes  = &httppb.HttpConnectionManager{
		RouteSpecifier: &httppb.HttpConnectionManager_ScopedRoutes{},
	}
	marshaledConnMgrWithScopedRoutes, _ = proto.Marshal(connMgrWithScopedRoutes)
	goodListener1                       = &xdspb.Listener{
		Name: goodLDSTarget1,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgr1,
			},
		},
	}
	marshaledListener1, _ = proto.Marshal(goodListener1)
	goodListener2         = &xdspb.Listener{
		Name: goodLDSTarget2,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgr1,
			},
		},
	}
	marshaledListener2, _     = proto.Marshal(goodListener2)
	noAPIListener             = &xdspb.Listener{Name: goodLDSTarget1}
	marshaledNoAPIListener, _ = proto.Marshal(noAPIListener)
	badAPIListener1           = &xdspb.Listener{
		Name: goodLDSTarget1,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   []byte{1, 2, 3, 4},
			},
		},
	}
	badAPIListener2 = &xdspb.Listener{
		Name: goodLDSTarget2,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   []byte{1, 2, 3, 4},
			},
		},
	}
	badlyMarshaledAPIListener2, _ = proto.Marshal(badAPIListener2)
	badResourceListener           = &xdspb.Listener{
		Name: goodLDSTarget1,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: ldsURL,
				Value:   marshaledListener1,
			},
		},
	}
	listenerWithEmptyHTTPConnMgr = &xdspb.Listener{
		Name: goodLDSTarget1,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   emptyMarshaledConnMgr,
			},
		},
	}
	listenerWithScopedRoutesRouteConfig = &xdspb.Listener{
		Name: goodLDSTarget1,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgrWithScopedRoutes,
			},
		},
	}
	goodLDSResponse1 = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: ldsURL,
				Value:   marshaledListener1,
			},
		},
		TypeUrl: ldsURL,
	}
	goodLDSResponse2 = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: ldsURL,
				Value:   marshaledListener2,
			},
		},
		TypeUrl: ldsURL,
	}
	emptyLDSResponse          = &xdspb.DiscoveryResponse{TypeUrl: ldsURL}
	badlyMarshaledLDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: ldsURL,
				Value:   []byte{1, 2, 3, 4},
			},
		},
		TypeUrl: ldsURL,
	}
	badResourceTypeInLDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgr1,
			},
		},
		TypeUrl: ldsURL,
	}
	ldsResponseWithMultipleResources = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: ldsURL,
				Value:   marshaledListener2,
			},
			{
				TypeUrl: ldsURL,
				Value:   marshaledListener1,
			},
		},
		TypeUrl: ldsURL,
	}
	noAPIListenerLDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: ldsURL,
				Value:   marshaledNoAPIListener,
			},
		},
		TypeUrl: ldsURL,
	}
	goodBadUglyLDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: ldsURL,
				Value:   marshaledListener2,
			},
			{
				TypeUrl: ldsURL,
				Value:   marshaledListener1,
			},
			{
				TypeUrl: ldsURL,
				Value:   badlyMarshaledAPIListener2,
			},
		},
		TypeUrl: ldsURL,
	}
	badlyMarshaledRDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: rdsURL,
				Value:   []byte{1, 2, 3, 4},
			},
		},
		TypeUrl: rdsURL,
	}
	badResourceTypeInRDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgr1,
			},
		},
		TypeUrl: rdsURL,
	}
	emptyRouteConfig             = &xdspb.RouteConfiguration{}
	marshaledEmptyRouteConfig, _ = proto.Marshal(emptyRouteConfig)
	noDomainsInRouteConfig       = &xdspb.RouteConfiguration{
		VirtualHosts: []*routepb.VirtualHost{{}},
	}
	noVirtualHostsInRDSResponse = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: rdsURL,
				Value:   marshaledEmptyRouteConfig,
			},
		},
		TypeUrl: rdsURL,
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
	marshaledGoodRouteConfig1, _ = proto.Marshal(goodRouteConfig1)
	goodRouteConfig2             = &xdspb.RouteConfiguration{
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
	marshaledGoodRouteConfig2, _ = proto.Marshal(goodRouteConfig2)
	goodRDSResponse1             = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: rdsURL,
				Value:   marshaledGoodRouteConfig1,
			},
		},
		TypeUrl: rdsURL,
	}
	goodRDSResponse2 = &xdspb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: rdsURL,
				Value:   marshaledGoodRouteConfig2,
			},
		},
		TypeUrl: rdsURL,
	}
)

// TestV2ClientBackoffAfterRecvError verifies if the v2Client backoffs when it
// encounters a Recv error while receiving an LDS response.
func (s) TestV2ClientBackoffAfterRecvError(t *testing.T) {
	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	// Override the v2Client backoff function with this, so that we can verify
	// that a backoff actually was triggerred.
	boCh := make(chan int, 1)
	clientBackoff := func(v int) time.Duration {
		boCh <- v
		return 0
	}

	callbackCh := make(chan struct{})
	v2c := newV2Client(&testUpdateReceiver{
		f: func(string, map[string]interface{}) { close(callbackCh) },
	}, cc, goodNodeProto, clientBackoff, nil)
	defer v2c.close()
	t.Log("Started xds v2Client...")

	// v2c.watchLDS(goodLDSTarget1, func(u ldsUpdate, err error) {})
	v2c.addWatch(ldsURL, goodLDSTarget1)
	if _, err := fakeServer.XDSRequestChan.Receive(); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS request")
	}
	t.Log("FakeServer received request...")

	fakeServer.XDSResponseChan <- &fakeserver.Response{Err: errors.New("RPC error")}
	t.Log("Bad LDS response pushed to fakeServer...")

	timer := time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		t.Fatal("Timeout when expecting LDS update")
	case <-boCh:
		timer.Stop()
		t.Log("v2Client backed off before retrying...")
	case <-callbackCh:
		t.Fatal("Received unexpected LDS callback")
	}
}

// TestV2ClientRetriesAfterBrokenStream verifies the case where a stream
// encountered a Recv() error, and is expected to send out xDS requests for
// registered watchers once it comes back up again.
func (s) TestV2ClientRetriesAfterBrokenStream(t *testing.T) {
	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	callbackCh := testutils.NewChannel()
	v2c := newV2Client(&testUpdateReceiver{
		f: func(typeURL string, d map[string]interface{}) {
			if typeURL == ldsURL {
				if u, ok := d[goodLDSTarget1]; ok {
					t.Logf("Received LDS callback with ldsUpdate {%+v}", u)
					callbackCh.Send(struct{}{})
				}
			}
		},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	defer v2c.close()
	t.Log("Started xds v2Client...")

	v2c.addWatch(ldsURL, goodLDSTarget1)
	if _, err := fakeServer.XDSRequestChan.Receive(); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS request")
	}
	t.Log("FakeServer received request...")

	fakeServer.XDSResponseChan <- &fakeserver.Response{Resp: goodLDSResponse1}
	t.Log("Good LDS response pushed to fakeServer...")

	if _, err := callbackCh.Receive(); err != nil {
		t.Fatal("Timeout when expecting LDS update")
	}

	// Read the ack, so the next request is sent after stream re-creation.
	if _, err := fakeServer.XDSRequestChan.Receive(); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS ACK")
	}

	fakeServer.XDSResponseChan <- &fakeserver.Response{Err: errors.New("RPC error")}
	t.Log("Bad LDS response pushed to fakeServer...")

	val, err := fakeServer.XDSRequestChan.Receive()
	if err == testutils.ErrRecvTimeout {
		t.Fatalf("Timeout expired when expecting LDS update")
	}
	gotRequest := val.(*fakeserver.Request)
	if !proto.Equal(gotRequest.Req, goodLDSRequest) {
		t.Fatalf("gotRequest: %+v, wantRequest: %+v", gotRequest.Req, goodLDSRequest)
	}
}

// TestV2ClientWatchWithoutStream verifies the case where a watch is started
// when the xds stream is not created. The watcher should not receive any update
// (because there won't be any xds response, and timeout is done at a upper
// level). And when the stream is re-created, the watcher should get future
// updates.
func (s) TestV2ClientWatchWithoutStream(t *testing.T) {
	oldWatchExpiryTimeout := defaultWatchExpiryTimeout
	defaultWatchExpiryTimeout = 500 * time.Millisecond
	defer func() {
		defaultWatchExpiryTimeout = oldWatchExpiryTimeout
	}()

	fakeServer, sCleanup, err := fakeserver.StartServer()
	if err != nil {
		t.Fatalf("Failed to start fake xDS server: %v", err)
	}
	defer sCleanup()

	const scheme = "xds_client_test_whatever"
	rb := manual.NewBuilderWithScheme(scheme)
	rb.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: "no.such.server"}}})

	cc, err := grpc.Dial(scheme+":///whatever", grpc.WithInsecure(), grpc.WithResolvers(rb))
	if err != nil {
		t.Fatalf("Failed to dial ClientConn: %v", err)
	}
	defer cc.Close()

	callbackCh := testutils.NewChannel()
	v2c := newV2Client(&testUpdateReceiver{
		f: func(typeURL string, d map[string]interface{}) {
			if typeURL == ldsURL {
				if u, ok := d[goodLDSTarget1]; ok {
					t.Logf("Received LDS callback with ldsUpdate {%+v}", u)
					callbackCh.Send(u)
				}
			}
		},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	defer v2c.close()
	t.Log("Started xds v2Client...")

	// This watch is started when the xds-ClientConn is in Transient Failure,
	// and no xds stream is created.
	v2c.addWatch(ldsURL, goodLDSTarget1)

	// The watcher should receive an update, with a timeout error in it.
	if v, err := callbackCh.TimedReceive(100 * time.Millisecond); err == nil {
		t.Fatalf("Expect an timeout error from watcher, got %v", v)
	}

	// Send the real server address to the ClientConn, the stream should be
	// created, and the previous watch should be sent.
	rb.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: fakeServer.Address}},
	})

	if _, err := fakeServer.XDSRequestChan.Receive(); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS request")
	}
	t.Log("FakeServer received request...")

	fakeServer.XDSResponseChan <- &fakeserver.Response{Resp: goodLDSResponse1}
	t.Log("Good LDS response pushed to fakeServer...")

	if v, err := callbackCh.Receive(); err != nil {
		t.Fatal("Timeout when expecting LDS update")
	} else if _, ok := v.(ldsUpdate); !ok {
		t.Fatalf("Expect an LDS update from watcher, got %v", v)
	}
}
