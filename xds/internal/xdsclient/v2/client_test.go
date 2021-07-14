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

package v2

import (
	"context"
	"errors"
	"fmt"
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
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/grpc/xds/internal/xdsclient"
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
	// An place holder error. When comparing UpdateErrorMetadata, we only check
	// if error is nil, and don't compare error content.
	errPlaceHolder = fmt.Errorf("err place holder")
)

type watchHandleTestcase struct {
	rType        xdsclient.ResourceType
	resourceName string

	responseToHandle *xdspb.DiscoveryResponse
	wantHandleErr    bool
	wantUpdate       interface{}
	wantUpdateMD     xdsclient.UpdateMetadata
	wantUpdateErr    bool
}

type testUpdateReceiver struct {
	f func(rType xdsclient.ResourceType, d map[string]interface{}, md xdsclient.UpdateMetadata)
}

func (t *testUpdateReceiver) NewListeners(d map[string]xdsclient.ListenerUpdate, metadata xdsclient.UpdateMetadata) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(xdsclient.ListenerResource, dd, metadata)
}

func (t *testUpdateReceiver) NewRouteConfigs(d map[string]xdsclient.RouteConfigUpdate, metadata xdsclient.UpdateMetadata) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(xdsclient.RouteConfigResource, dd, metadata)
}

func (t *testUpdateReceiver) NewClusters(d map[string]xdsclient.ClusterUpdate, metadata xdsclient.UpdateMetadata) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(xdsclient.ClusterResource, dd, metadata)
}

func (t *testUpdateReceiver) NewEndpoints(d map[string]xdsclient.EndpointsUpdate, metadata xdsclient.UpdateMetadata) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(xdsclient.EndpointsResource, dd, metadata)
}

func (t *testUpdateReceiver) NewConnectionError(error) {}

func (t *testUpdateReceiver) newUpdate(rType xdsclient.ResourceType, d map[string]interface{}, metadata xdsclient.UpdateMetadata) {
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

	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	type updateErr struct {
		u   interface{}
		md  xdsclient.UpdateMetadata
		err error
	}
	gotUpdateCh := testutils.NewChannel()

	v2c, err := newV2Client(&testUpdateReceiver{
		f: func(rType xdsclient.ResourceType, d map[string]interface{}, md xdsclient.UpdateMetadata) {
			if rType == test.rType {
				switch test.rType {
				case xdsclient.ListenerResource:
					dd := make(map[string]xdsclient.ListenerUpdate)
					for n, u := range d {
						dd[n] = u.(xdsclient.ListenerUpdate)
					}
					gotUpdateCh.Send(updateErr{dd, md, nil})
				case xdsclient.RouteConfigResource:
					dd := make(map[string]xdsclient.RouteConfigUpdate)
					for n, u := range d {
						dd[n] = u.(xdsclient.RouteConfigUpdate)
					}
					gotUpdateCh.Send(updateErr{dd, md, nil})
				case xdsclient.ClusterResource:
					dd := make(map[string]xdsclient.ClusterUpdate)
					for n, u := range d {
						dd[n] = u.(xdsclient.ClusterUpdate)
					}
					gotUpdateCh.Send(updateErr{dd, md, nil})
				case xdsclient.EndpointsResource:
					dd := make(map[string]xdsclient.EndpointsUpdate)
					for n, u := range d {
						dd[n] = u.(xdsclient.EndpointsUpdate)
					}
					gotUpdateCh.Send(updateErr{dd, md, nil})
				}
			}
		},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
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
	var handleXDSResp func(response *xdspb.DiscoveryResponse) error
	switch test.rType {
	case xdsclient.ListenerResource:
		handleXDSResp = v2c.handleLDSResponse
	case xdsclient.RouteConfigResource:
		handleXDSResp = v2c.handleRDSResponse
	case xdsclient.ClusterResource:
		handleXDSResp = v2c.handleCDSResponse
	case xdsclient.EndpointsResource:
		handleXDSResp = v2c.handleEDSResponse
	}
	if err := handleXDSResp(test.responseToHandle); (err != nil) != test.wantHandleErr {
		t.Fatalf("v2c.handleRDSResponse() returned err: %v, wantErr: %v", err, test.wantHandleErr)
	}

	wantUpdate := test.wantUpdate
	cmpOpts := cmp.Options{
		cmpopts.EquateEmpty(), protocmp.Transform(),
		cmpopts.IgnoreFields(xdsclient.UpdateMetadata{}, "Timestamp"),
		cmpopts.IgnoreFields(xdsclient.UpdateErrorMetadata{}, "Timestamp"),
		cmp.Comparer(func(x, y error) bool { return (x == nil) == (y == nil) }),
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

// startServerAndGetCC starts a fake XDS server and also returns a ClientConn
// connected to it.
func startServerAndGetCC(t *testing.T) (*fakeserver.Server, *grpc.ClientConn, func()) {
	t.Helper()

	fs, sCleanup, err := fakeserver.StartServer()
	if err != nil {
		t.Fatalf("Failed to start fake xDS server: %v", err)
	}

	cc, ccCleanup, err := fs.XDSClientConn()
	if err != nil {
		sCleanup()
		t.Fatalf("Failed to get a clientConn to the fake xDS server: %v", err)
	}
	return fs, cc, func() {
		sCleanup()
		ccCleanup()
	}
}

func newV2Client(p xdsclient.UpdateHandler, cc *grpc.ClientConn, n *basepb.Node, b func(int) time.Duration, l *grpclog.PrefixLogger) (*client, error) {
	c, err := newClient(cc, xdsclient.BuildOptions{
		Parent:    p,
		NodeProto: n,
		Backoff:   b,
		Logger:    l,
	})
	if err != nil {
		return nil, err
	}
	return c.(*client), nil
}

// TestV2ClientBackoffAfterRecvError verifies if the v2Client backs off when it
// encounters a Recv error while receiving an LDS response.
func (s) TestV2ClientBackoffAfterRecvError(t *testing.T) {
	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	// Override the v2Client backoff function with this, so that we can verify
	// that a backoff actually was triggered.
	boCh := make(chan int, 1)
	clientBackoff := func(v int) time.Duration {
		boCh <- v
		return 0
	}

	callbackCh := make(chan struct{})
	v2c, err := newV2Client(&testUpdateReceiver{
		f: func(xdsclient.ResourceType, map[string]interface{}, xdsclient.UpdateMetadata) { close(callbackCh) },
	}, cc, goodNodeProto, clientBackoff, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v2c.Close()
	t.Log("Started xds v2Client...")

	v2c.AddWatch(xdsclient.ListenerResource, goodLDSTarget1)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := fakeServer.XDSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS request")
	}
	t.Log("FakeServer received request...")

	fakeServer.XDSResponseChan <- &fakeserver.Response{Err: errors.New("RPC error")}
	t.Log("Bad LDS response pushed to fakeServer...")

	timer := time.NewTimer(defaultTestShortTimeout)
	select {
	case <-timer.C:
		t.Fatal("Timeout when expecting LDS update")
	case <-boCh:
		timer.Stop()
		t.Log("v2Client backed off before retrying...")
	case <-callbackCh:
		t.Fatal("Received unexpected LDS callback")
	}

	if _, err := fakeServer.XDSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS request")
	}
	t.Log("FakeServer received request after backoff...")
}

// TestV2ClientRetriesAfterBrokenStream verifies the case where a stream
// encountered a Recv() error, and is expected to send out xDS requests for
// registered watchers once it comes back up again.
func (s) TestV2ClientRetriesAfterBrokenStream(t *testing.T) {
	fakeServer, cc, cleanup := startServerAndGetCC(t)
	defer cleanup()

	callbackCh := testutils.NewChannel()
	v2c, err := newV2Client(&testUpdateReceiver{
		f: func(rType xdsclient.ResourceType, d map[string]interface{}, md xdsclient.UpdateMetadata) {
			if rType == xdsclient.ListenerResource {
				if u, ok := d[goodLDSTarget1]; ok {
					t.Logf("Received LDS callback with ldsUpdate {%+v}", u)
					callbackCh.Send(struct{}{})
				}
			}
		},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v2c.Close()
	t.Log("Started xds v2Client...")

	v2c.AddWatch(xdsclient.ListenerResource, goodLDSTarget1)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := fakeServer.XDSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS request")
	}
	t.Log("FakeServer received request...")

	fakeServer.XDSResponseChan <- &fakeserver.Response{Resp: goodLDSResponse1}
	t.Log("Good LDS response pushed to fakeServer...")

	if _, err := callbackCh.Receive(ctx); err != nil {
		t.Fatal("Timeout when expecting LDS update")
	}

	// Read the ack, so the next request is sent after stream re-creation.
	if _, err := fakeServer.XDSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS ACK")
	}

	fakeServer.XDSResponseChan <- &fakeserver.Response{Err: errors.New("RPC error")}
	t.Log("Bad LDS response pushed to fakeServer...")

	val, err := fakeServer.XDSRequestChan.Receive(ctx)
	if err != nil {
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
	fakeServer, sCleanup, err := fakeserver.StartServer()
	if err != nil {
		t.Fatalf("Failed to start fake xDS server: %v", err)
	}
	defer sCleanup()

	const scheme = "xds_client_test_whatever"
	rb := manual.NewBuilderWithScheme(scheme)
	rb.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: "no.such.server"}}})

	cc, err := grpc.Dial(scheme+":///whatever", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(rb))
	if err != nil {
		t.Fatalf("Failed to dial ClientConn: %v", err)
	}
	defer cc.Close()

	callbackCh := testutils.NewChannel()
	v2c, err := newV2Client(&testUpdateReceiver{
		f: func(rType xdsclient.ResourceType, d map[string]interface{}, md xdsclient.UpdateMetadata) {
			if rType == xdsclient.ListenerResource {
				if u, ok := d[goodLDSTarget1]; ok {
					t.Logf("Received LDS callback with ldsUpdate {%+v}", u)
					callbackCh.Send(u)
				}
			}
		},
	}, cc, goodNodeProto, func(int) time.Duration { return 0 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v2c.Close()
	t.Log("Started xds v2Client...")

	// This watch is started when the xds-ClientConn is in Transient Failure,
	// and no xds stream is created.
	v2c.AddWatch(xdsclient.ListenerResource, goodLDSTarget1)

	// The watcher should receive an update, with a timeout error in it.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if v, err := callbackCh.Receive(sCtx); err == nil {
		t.Fatalf("Expect an timeout error from watcher, got %v", v)
	}

	// Send the real server address to the ClientConn, the stream should be
	// created, and the previous watch should be sent.
	rb.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: fakeServer.Address}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := fakeServer.XDSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout expired when expecting an LDS request")
	}
	t.Log("FakeServer received request...")

	fakeServer.XDSResponseChan <- &fakeserver.Response{Resp: goodLDSResponse1}
	t.Log("Good LDS response pushed to fakeServer...")

	if v, err := callbackCh.Receive(ctx); err != nil {
		t.Fatal("Timeout when expecting LDS update")
	} else if _, ok := v.(xdsclient.ListenerUpdate); !ok {
		t.Fatalf("Expect an LDS update from watcher, got %v", v)
	}
}

func newStringP(s string) *string {
	return &s
}
