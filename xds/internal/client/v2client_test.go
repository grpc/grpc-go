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
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/xds/internal/client/fakexds"

	discoverypb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	ldspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	basepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	httppb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	adsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	anypb "github.com/golang/protobuf/ptypes/any"
	structpb "github.com/golang/protobuf/ptypes/struct"
)

const (
	defaultTestTimeout = 2 * time.Second
	goodLDSTarget      = "GoodListener"
	goodRouteName      = "GoodRouteConfig"
	httpConnManagerURL = "type.googleapis.com/envoy.config.filter.network.http_connection_manager.v2.HttpConnectionManager"
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
	goodHTTPConnManager = &httppb.HttpConnectionManager{
		RouteSpecifier: &httppb.HttpConnectionManager_Rds{
			Rds: &httppb.Rds{
				RouteConfigName: goodRouteName,
			},
		},
	}
	marshaledConnMgr, _ = proto.Marshal(goodHTTPConnManager)
	goodListener        = &ldspb.Listener{
		Name: goodLDSTarget,
		ApiListener: &listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: httpConnManagerURL,
				Value:   marshaledConnMgr,
			},
		},
	}
	marshaledListener, _ = proto.Marshal(goodListener)
	goodLDSResponse      = &discoverypb.DiscoveryResponse{
		Resources: []*anypb.Any{
			{
				TypeUrl: listenerURL,
				Value:   marshaledListener,
			},
		},
		TypeUrl: listenerURL,
	}
)

func setupClientAndServer(t *testing.T) (*fakexds.Server, *grpc.ClientConn, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}

	server := grpc.NewServer()
	fakeServer := fakexds.New(t, nil)
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

// TODO:
// test the case when the stream starts off after an error and resends all the watches
// test the case where the server replies with multiple entries
// test backoff when stream fails with no responses.
// test the case where server sends multiple messages .. so call back should be invoked twice
// test the case where server sends response after watcher is cancelled. make sure callback is not invoked.

type testConfig struct {
	fakeServer  *fakexds.Server
	v2c         *v2Client
	backoffFunc func(int) time.Duration
	ops         chan interface{}
}

type ldsTestOp struct {
	target         string
	wantUpdate     *ldsUpdate
	wantRequest    *fakexds.Request
	responseToSend *fakexds.Response
}
type rdsTestOp struct {
}

func testLDSnRDS(t *testing.T, tc *testConfig, errCh chan error) {
	for op := range tc.ops {
		switch testOp := op.(type) {
		case *ldsTestOp:
			ldsOp := testOp
			updateCh := make(chan struct{})
			if ldsOp.target != "" {
				tc.v2c.watchLDS(ldsOp.target, func(u ldsUpdate, err error) {
					t.Logf("Received ldsUpdate in callback: %+v", u)
					if err != nil {
						errCh <- fmt.Errorf("received error in lds callback: %v", err)
						return
					}
					if !reflect.DeepEqual(u, *ldsOp.wantUpdate) {
						errCh <- fmt.Errorf("got LDS update : %+v, want %+v", u, ldsOp.wantUpdate)
						return
					}
					close(updateCh)
				})
				t.Logf("Registered a watcher for LDS target: %v...", ldsOp.target)
			}
			if ldsOp.wantRequest != nil {
				got := <-tc.fakeServer.RequestChan
				if !proto.Equal(got.Req, ldsOp.wantRequest.Req) {
					errCh <- fmt.Errorf("got LDS request: %+v, want: %+v", got.Req, ldsOp.wantRequest.Req)
					return
				}
				if got.Err != ldsOp.wantRequest.Err {
					errCh <- fmt.Errorf("got error while processing LDS request: %v, want: %v", got.Err, ldsOp.wantRequest.Err)
					return
				}
			}
			if ldsOp.responseToSend != nil {
				tc.fakeServer.ResponseChan <- ldsOp.responseToSend
			}
			if ldsOp.wantUpdate != nil {
				<-updateCh
			}
		case *rdsTestOp:
		default:
		}
	}
	errCh <- nil
}

// test bad messages. make sure the client backsoff and retries
//  - recv returns err
//  - length of resources array in response is zero
//  - ptype any unmarshal error
//  - listener resource of unexpected type
//  - listener not interesting
//  - no API listener field in response
//  - api listener unmarshal error
//  - no http connection manager in response
//  - no route specifier in response
//  - RDS config inline
func TestLDSBadResponses(t *testing.T) {
	fakeServer, client, cleanup := setupClientAndServer(t)
	defer cleanup()

	boCh := make(chan int)
	clientBackoff := func(v int) time.Duration {
		boCh <- v
		return 0
	}

	v2c := newV2Client(client, goodNodeProto, clientBackoff)
	defer v2c.close()
	t.Log("Started xds v2Client...")

	tc := &testConfig{
		fakeServer: fakeServer,
		v2c:        v2c,
		ops:        make(chan interface{}),
	}

	errCh := make(chan error, 1)
	go testLDSnRDS(t, tc, errCh)
	tc.ops <- &ldsTestOp{
		target:     goodLDSTarget,
		wantUpdate: nil,
		wantRequest: &fakexds.Request{
			Req: &discoverypb.DiscoveryRequest{
				Node:          goodNodeProto,
				TypeUrl:       listenerURL,
				ResourceNames: []string{goodLDSTarget},
			},
		},
		responseToSend: &fakexds.Response{
			Err: errors.New("RPC error"),
		},
	}
	close(tc.ops)

	<-boCh

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

func TestLDSOneGoodResponse(t *testing.T) {
	fakeServer, client, cleanup := setupClientAndServer(t)
	defer cleanup()

	clientBackoff := func(v int) time.Duration {
		return 0
	}

	v2c := newV2Client(client, goodNodeProto, clientBackoff)
	defer v2c.close()
	t.Log("Started xds v2Client...")

	tc := &testConfig{
		fakeServer: fakeServer,
		v2c:        v2c,
		ops:        make(chan interface{}),
	}

	errCh := make(chan error, 1)
	go testLDSnRDS(t, tc, errCh)
	tc.ops <- &ldsTestOp{
		target:     goodLDSTarget,
		wantUpdate: &ldsUpdate{routeName: goodRouteName},
		wantRequest: &fakexds.Request{
			Req: &discoverypb.DiscoveryRequest{
				Node:          goodNodeProto,
				TypeUrl:       listenerURL,
				ResourceNames: []string{goodLDSTarget},
			},
		},
		responseToSend: &fakexds.Response{
			Resp: goodLDSResponse,
		},
	}
	close(tc.ops)

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
