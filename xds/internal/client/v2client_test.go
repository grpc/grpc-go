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
	"time"

	"github.com/golang/protobuf/proto"

	discoverypb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	ldspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	rdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	basepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	httppb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	anypb "github.com/golang/protobuf/ptypes/any"
	structpb "github.com/golang/protobuf/ptypes/struct"
)

const (
	defaultTestTimeout     = 5 * time.Second
	goodLDSTarget1         = "GoodListener1"
	goodLDSTarget2         = "GoodListener2"
	uninterestingLDSTarget = "UninterestingListener"
	goodRouteName1         = "GoodRouteConfig1"
	goodRouteName2         = "GoodRouteConfig2"
	httpConnManagerURL     = "type.googleapis.com/envoy.config.filter.network.http_connection_manager.v2.HttpConnectionManager"
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
	goodLDSRequest = &discoverypb.DiscoveryRequest{
		Node:          goodNodeProto,
		TypeUrl:       listenerURL,
		ResourceNames: []string{goodLDSTarget1},
	}
	goodHTTPConnManager1 = &httppb.HttpConnectionManager{
		RouteSpecifier: &httppb.HttpConnectionManager_Rds{
			Rds: &httppb.Rds{
				RouteConfigName: goodRouteName1,
			},
		},
	}
	marshaledConnMgr1, _ = proto.Marshal(goodHTTPConnManager1)
	goodHTTPConnManager2 = &httppb.HttpConnectionManager{
		RouteSpecifier: &httppb.HttpConnectionManager_Rds{
			Rds: &httppb.Rds{
				RouteConfigName: goodRouteName2,
			},
		},
	}
	marshaledConnMgr2, _ = proto.Marshal(goodHTTPConnManager2)
	emptyHTTPConnManager = &httppb.HttpConnectionManager{
		RouteSpecifier: &httppb.HttpConnectionManager_Rds{
			Rds: &httppb.Rds{},
		},
	}
	emptyMarshaledConnMgr, _     = proto.Marshal(emptyHTTPConnManager)
	connMgrWithInlineRouteConfig = &httppb.HttpConnectionManager{
		RouteSpecifier: &httppb.HttpConnectionManager_RouteConfig{
			RouteConfig: &rdspb.RouteConfiguration{
				Name: goodRouteName1,
			},
		},
	}
	marshaledConnMgrWithInlineRouteConfig, _ = proto.Marshal(connMgrWithInlineRouteConfig)
	goodListener1                            = &ldspb.Listener{
		Name: goodLDSTarget1,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgr1,
			},
		},
	}
	marshaledListener1, _ = proto.Marshal(goodListener1)
	goodListener2         = &ldspb.Listener{
		Name: goodLDSTarget2,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgr1,
			},
		},
	}
	marshaledListener2, _ = proto.Marshal(goodListener2)
	otherGoodListener2    = &ldspb.Listener{
		Name: goodLDSTarget1,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgr2,
			},
		},
	}
	otherMarshaledListener2, _ = proto.Marshal(otherGoodListener2)
	uninterestingListener      = &ldspb.Listener{
		Name: uninterestingLDSTarget,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgr1,
			},
		},
	}
	uninterestingMarshaledListener, _ = proto.Marshal(uninterestingListener)
	noAPIListener                     = &ldspb.Listener{Name: goodLDSTarget1}
	marshaledNoAPIListener, _         = proto.Marshal(noAPIListener)
	badAPIListener1                   = &ldspb.Listener{
		Name: goodLDSTarget1,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   []byte{1, 2, 3, 4},
			},
		},
	}
	badlyMarshaledAPIListener1, _ = proto.Marshal(badAPIListener1)
	badAPIListener2               = &ldspb.Listener{
		Name: goodLDSTarget2,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   []byte{1, 2, 3, 4},
			},
		},
	}
	badlyMarshaledAPIListener2, _ = proto.Marshal(badAPIListener2)
	badResourceListener           = &ldspb.Listener{
		Name: goodLDSTarget1,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: listenerURL,
				Value:   marshaledListener1,
			},
		},
	}
	marshaledBadResourceListener, _ = proto.Marshal(badResourceListener)
	listenerWithEmptyHttpConnMgr    = &ldspb.Listener{
		Name: goodLDSTarget1,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   emptyMarshaledConnMgr,
			},
		},
	}
	marshaledListenerWithEmptyHttpConnMgr, _ = proto.Marshal(listenerWithEmptyHttpConnMgr)
	listenerWithInlineRouteConfig            = &ldspb.Listener{
		Name: goodLDSTarget1,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgrWithInlineRouteConfig,
			},
		},
	}
	marshaledListenerWithInlineRouteConfig, _ = proto.Marshal(listenerWithInlineRouteConfig)
	goodLDSResponse1                          = &discoverypb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: listenerURL,
				Value:   marshaledListener1,
			},
		},
		TypeUrl: listenerURL,
	}
	otherGoodLDSResponse1 = &discoverypb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: listenerURL,
				Value:   otherMarshaledListener2,
			},
		},
		TypeUrl: listenerURL,
	}
	goodLDSResponse2 = &discoverypb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: listenerURL,
				Value:   marshaledListener2,
			},
		},
		TypeUrl: listenerURL,
	}
	emptyLDSResponse          = &discoverypb.DiscoveryResponse{TypeUrl: listenerURL}
	badlyMarshaledLDSResponse = &discoverypb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: listenerURL,
				Value:   []byte{1, 2, 3, 4},
			},
		},
		TypeUrl: listenerURL,
	}
	badResourceTypeInLDSResponse = &discoverypb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: listenerURL,
				Value:   marshaledConnMgr1,
			},
		},
		TypeUrl: listenerURL,
	}
	badResourceTypeInAPIListenerInLDSResponse = &discoverypb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: listenerURL,
				Value:   marshaledBadResourceListener,
			},
		},
		TypeUrl: listenerURL,
	}
	uninterestingLDSResponse = &discoverypb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: listenerURL,
				Value:   uninterestingMarshaledListener,
			},
		},
		TypeUrl: listenerURL,
	}
	ldsResponseWithMultipleResources = &discoverypb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: listenerURL,
				Value:   marshaledListener2,
			},
			{
				TypeUrl: listenerURL,
				Value:   marshaledListener1,
			},
		},
		TypeUrl: listenerURL,
	}
	noAPIListenerLDSResponse = &discoverypb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: listenerURL,
				Value:   marshaledNoAPIListener,
			},
		},
		TypeUrl: listenerURL,
	}
	badlyMarshaledAPIListenerInLDSResponse = &discoverypb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: listenerURL,
				Value:   badlyMarshaledAPIListener1,
			},
		},
		TypeUrl: listenerURL,
	}
	ldsResponseWithEmptyHttpConnMgr = &discoverypb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: listenerURL,
				Value:   marshaledListenerWithEmptyHttpConnMgr,
			},
		},
		TypeUrl: listenerURL,
	}
	ldsResponseWithInlineRouteConfig = &discoverypb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: listenerURL,
				Value:   marshaledListenerWithInlineRouteConfig,
			},
		},
		TypeUrl: listenerURL,
	}
	goodBadUglyLDSResponse = &discoverypb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: listenerURL,
				Value:   marshaledListener2,
			},
			{
				TypeUrl: listenerURL,
				Value:   marshaledListener1,
			},
			{
				TypeUrl: listenerURL,
				Value:   badlyMarshaledAPIListener2,
			},
		},
		TypeUrl: listenerURL,
	}
)

/*
// ldsTestOp contains all data related to one particular test operation related
// to LDS watch.
type ldsTestOp struct {
	// target is the resource name to watch for.
	target string
	// wantUpdate is the expected ldsUpdate received in the ldsCallback.
	wantUpdate *ldsUpdate
	// wantUpdateErr specfies whether or not the ldsCallback returns an error.
	wantUpdateErr bool
	// wantRetry specifies whether or not the client is expected to kill the
	// stream because of an error, and expected to backoff and retry.
	wantRetry bool
	// wantRequest is the LDS request expected to be sent by the client.
	wantRequest *fakexds.Request
	// responseToSend is the LDS response that the fake server will send.
	responseToSend *fakexds.Response
}

// setupClientAndServer starts a fakexds.Server and creates a ClientConn
// talking to it. The returned cleanup function should be invoked by the caller
// once the test is done.
func setupClientAndServer(t *testing.T) (*fakexds.Server, *grpc.ClientConn, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}

	server := grpc.NewServer()
	fakeServer := fakexds.New(nil)
	adsgrpc.RegisterAggregatedDiscoveryServiceServer(server, fakeServer)
	go server.Serve(lis)
	t.Logf("Starting fake xDS server at %v...", lis.Addr().String())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := grpc.DialContext(ctx, lis.Addr().String(), grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		t.Fatalf("grpc.DialContext(%s) failed: %v", lis.Addr().String(), err)
	}
	t.Log("Started xDS gRPC client...")

	return fakeServer, client, func() {
		server.Stop()
		lis.Close()
	}
}

// testLDS creates a v2Client object talking to a fakexds.Server and reads the
// ops channel for test operations to be performed.
func testLDS(t *testing.T, ldsOps chan ldsTestOp) {
	t.Helper()

	fakeServer, client, cleanup := setupClientAndServer(t)
	defer cleanup()

	// Override the v2Client backoff function with this, so that we can verify
	// that a backoff actually was triggerred.
	boCh := make(chan int, 1)
	clientBackoff := func(v int) time.Duration {
		boCh <- v
		return 0
	}

	v2c := newV2Client(client, goodNodeProto, clientBackoff)
	defer v2c.close()
	t.Log("Started xds v2Client...")

	errCh := make(chan error, 1)
	go func() {
		cbUpdate := make(chan ldsUpdate, 1)
		cbErr := make(chan error, 1)
		for ldsOp := range ldsOps {
			// Register a watcher if required, and use a channel to signal the
			// successful invocation of the callback.
			if ldsOp.target != "" {
				v2c.watchLDS(ldsOp.target, func(u ldsUpdate, err error) {
					t.Logf("Received callback with ldsUpdate {%+v} and error {%v}", u, err)
					cbUpdate <- u
					cbErr <- err
				})
				t.Logf("Registered a watcher for LDS target: %v...", ldsOp.target)
			}

			// Make sure the request received at the fakeserver matches the
			// expected one.
			if ldsOp.wantRequest != nil {
				got := <-fakeServer.RequestChan
				if !proto.Equal(got.Req, ldsOp.wantRequest.Req) {
					errCh <- fmt.Errorf("got LDS request: %+v, want: %+v", got.Req, ldsOp.wantRequest.Req)
					return
				}
				if got.Err != ldsOp.wantRequest.Err {
					errCh <- fmt.Errorf("got error while processing LDS request: %v, want: %v", got.Err, ldsOp.wantRequest.Err)
					return
				}
				t.Log("FakeServer received expected request...")
			}

			// if a response is specified in the testOp, push it to the
			// fakeserver.
			if ldsOp.responseToSend != nil {
				fakeServer.ResponseChan <- ldsOp.responseToSend
				t.Log("Response pushed to fakeServer...")
			}

			// Make sure the update callback was invoked, if specified in the
			// testOp.
			if ldsOp.wantUpdate != nil {
				u := <-cbUpdate
				if !reflect.DeepEqual(u, *ldsOp.wantUpdate) {
					errCh <- fmt.Errorf("got LDS update : %+v, want %+v", u, ldsOp.wantUpdate)
					return
				}
				err := <-cbErr
				if (err != nil) != ldsOp.wantUpdateErr {
					errCh <- fmt.Errorf("received error {%v} in lds callback, wantErr: %v", err, ldsOp.wantUpdateErr)
					return
				}
				t.Log("LDS watch callback received expected update...")
			}

			// Make sure the stream was retried, if specified in the testOp.
			if ldsOp.wantRetry {
				<-boCh
				t.Log("v2Client backed off before retrying...")
			}
		}
		t.Log("Completed all test ops successfully...")
		errCh <- nil
	}()

	timer := time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		t.Fatal("time out when expecting LDS update")
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	}
}

// test bad messages. make sure the client backsoff and retries
//  - api listener unmarshal error
//  - no http connection manager in response
//  - no route specifier in response
//  - RDS config inline
func TestLDSBadResponses(t *testing.T) {
	responses := []fakexds.Response{
			{Err: errors.New("RPC error")},
			{Resp: emptyLDSResponse},
			{Resp: badlyMarshaledLDSResponse},
			{Resp: badResourceTypeInLDSResponse},
			{Resp: noAPIListenerLDSResponse},
		{Resp: badlyMarshaledAPIListenerInLDSResponse},
	}

	for _, resp := range responses {
		opCh := make(chan ldsTestOp, 1)
		opCh <- ldsTestOp{
			target:         goodLDSTarget1,
			wantRetry:      true,
			wantUpdate:     nil,
			wantRequest:    &fakexds.Request{Req: goodLDSRequest},
			responseToSend: &resp,
		}
		close(opCh)
		testLDS(t, opCh)
	}
}

func TestLDSUninterestingListener(t *testing.T) {
	opCh := make(chan ldsTestOp, 1)
	opCh <- ldsTestOp{
		target:         goodLDSTarget1,
		wantRetry:      true,
		wantUpdate:     &ldsUpdate{routeName: ""},
		wantUpdateErr:  true,
		wantRequest:    &fakexds.Request{Req: goodLDSRequest},
		responseToSend: &fakexds.Response{Resp: goodLDSResponse2},
	}
	close(opCh)
	testLDS(t, opCh)
}

func TestLDSOneGoodResponse(t *testing.T) {
	opCh := make(chan ldsTestOp, 1)
	opCh <- ldsTestOp{
		target:         goodLDSTarget1,
		wantUpdate:     &ldsUpdate{routeName: goodRouteName1},
		wantRequest:    &fakexds.Request{Req: goodLDSRequest},
		responseToSend: &fakexds.Response{Resp: goodLDSResponse1},
	}
	close(opCh)
	testLDS(t, opCh)
}

func TestLDSResponseWithMultipleResources(t *testing.T) {
	opCh := make(chan ldsTestOp, 1)
	opCh <- ldsTestOp{
		target:         goodLDSTarget1,
		wantUpdate:     &ldsUpdate{routeName: goodRouteName1},
		wantRequest:    &fakexds.Request{Req: goodLDSRequest},
		responseToSend: &fakexds.Response{Resp: ldsResponseWithMultipleResources},
	}
	close(opCh)
	testLDS(t, opCh)
}

func TestLDSMultipleGoodResponses(t *testing.T) {
	opCh := make(chan ldsTestOp, 2)
	opCh <- ldsTestOp{
		target:         goodLDSTarget1,
		wantUpdate:     &ldsUpdate{routeName: goodRouteName1},
		wantRequest:    &fakexds.Request{Req: goodLDSRequest},
		responseToSend: &fakexds.Response{Resp: goodLDSResponse1},
	}
	opCh <- ldsTestOp{
		wantUpdate:     &ldsUpdate{routeName: goodRouteName2},
		responseToSend: &fakexds.Response{Resp: otherGoodLDSResponse1},
	}
	close(opCh)
	testLDS(t, opCh)
}
*/
// TODO:
// test the case when the stream starts off after an error and resends all the watches
// test the case where server sends response after watcher is cancelled. make sure callback is not invoked.
