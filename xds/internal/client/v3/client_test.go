/*
 *
 * Copyright 2020 gRPC authors.
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

package v3

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/testutils/e2e"
	"google.golang.org/grpc/xds/internal/version"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

const (
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond

	listenerName = "lds.target.good:1111"
	routeName    = "route_configuration"
)

var (
	listener = &v3listenerpb.Listener{
		Name: listenerName,
		ApiListener: &v3listenerpb.ApiListener{
			ApiListener: &anypb.Any{
				TypeUrl: version.V3HTTPConnManagerURL,
				Value: func() []byte {
					cm := &v3httppb.HttpConnectionManager{
						RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
							Rds: &v3httppb.Rds{
								ConfigSource: &v3corepb.ConfigSource{
									ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
								},
								RouteConfigName: routeName,
							},
						},
					}
					mcm, _ := proto.Marshal(cm)
					return mcm
				}(),
			},
		},
	}
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type testUpdateReceiver struct {
	f func(rType xdsclient.ResourceType, d map[string]interface{})
}

func (t *testUpdateReceiver) NewListeners(d map[string]xdsclient.ListenerUpdate) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(xdsclient.ListenerResource, dd)
}

func (t *testUpdateReceiver) NewRouteConfigs(d map[string]xdsclient.RouteConfigUpdate) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(xdsclient.RouteConfigResource, dd)
}

func (t *testUpdateReceiver) NewClusters(d map[string]xdsclient.ClusterUpdate) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(xdsclient.ClusterResource, dd)
}

func (t *testUpdateReceiver) NewEndpoints(d map[string]xdsclient.EndpointsUpdate) {
	dd := make(map[string]interface{})
	for k, v := range d {
		dd[k] = v
	}
	t.newUpdate(xdsclient.EndpointsResource, dd)
}

func (t *testUpdateReceiver) newUpdate(rType xdsclient.ResourceType, d map[string]interface{}) {
	t.f(rType, d)
}

func newV3Client(p *testUpdateReceiver, cc *grpc.ClientConn, n *v3corepb.Node, b func(int) time.Duration) (*client, error) {
	c, err := newClient(cc, xdsclient.BuildOptions{
		Parent:    p,
		NodeProto: n,
		Backoff:   b,
	})
	if err != nil {
		return nil, err
	}
	return c.(*client), nil
}

// TestClientWatchWithoutStream tests the case where a watch is started on the
// version specific xdsClient before the connection to the management server is
// healthy. The test verifies that the watcher does not receive any update
// (because there won't be any response, and timeout is done at a upper level).
// And when connection to the management server becomes healthy and the ADS
// stream is created, the watcher should get the appropriate update.
func (s) TestClientWatchWithoutStream(t *testing.T) {
	// Start the xDS management server.
	mgmtServer, err := e2e.StartManagementServer(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer mgmtServer.Stop()

	// Configure the management server with a listener resource.
	nodeID := uuid.New().String()
	if err := mgmtServer.Update(e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener},
	}); err != nil {
		t.Fatal(err)
	}

	// Create a manual resolver which returns a bad address.
	const scheme = "xds_client_test_whatever"
	rb := manual.NewBuilderWithScheme(scheme)
	rb.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: "no.such.server"}}})

	// Dial the management server with the manual resolver created above. Since
	// the address returned by the manual resolver is a bad one, this ClientConn
	// be in transient failure.
	cc, err := grpc.Dial(scheme+":///whatever", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(rb))
	if err != nil {
		t.Fatalf("Failed to dial management server: %v", err)
	}
	defer cc.Close()

	// Create a v3Client with a fake top-level client which simply pushes the
	// received update on a channel for this test to inspect. Also pass it the
	// ClientConn created above.
	callbackCh := testutils.NewChannel()
	v3c, err := newV3Client(&testUpdateReceiver{
		f: func(rType xdsclient.ResourceType, d map[string]interface{}) {
			u, ok := d[listenerName]
			if !ok {
				t.Errorf("received unexpected updated: %+v", d)
			}
			callbackCh.Send(u)
		},
	}, cc, &v3corepb.Node{Id: nodeID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v3c.Close()
	t.Log("Started xds v3Client...")

	// Register a watch for a Listener resource. The underlying connection to
	// the management server is still in transient failure, so no ADS stream
	// will be created.
	v3c.AddWatch(xdsclient.ListenerResource, listenerName)

	// The watcher should not receive an update. Timeout is done at the
	// top-level xds client (which we have faked out).
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if v, err := callbackCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("Received an update from watcher when none was expected, got %v", v)
	}

	// Push the real server address to the manual resolver. This should move the
	// ClientConn to ready state, triggering the creation of an ADS stream on
	// which a discovery request corresponding to the previously registered
	// watch should be sent.
	rb.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: mgmtServer.Address}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := waitForListenerUpdate(ctx, callbackCh, xdsclient.ListenerUpdate{RouteConfigName: routeName}); err != nil {
		t.Fatal(err)
	}
}

// TestClientRetriesAfterBrokenStream tests the case where a stream
// encountered a Recv() error, and is expected to send out xDS requests for
// registered watchers once it comes back up again.
func (s) TestClientRetriesAfterBrokenStream(t *testing.T) {
	// Create a manual resolver which returns a bad address.
	const scheme = "xds_client_test_whatever"
	rb := manual.NewBuilderWithScheme(scheme)
	rb.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: "no.such.server"}}})

	// Dial the management server with the manual resolver created above. Since
	// the address returned by the manual resolver is a bad one, this ClientConn
	// be in transient failure.
	cc, err := grpc.Dial(scheme+":///whatever", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(rb))
	if err != nil {
		t.Fatalf("Failed to dial management server: %v", err)
	}
	defer cc.Close()

	// Create a v3Client with a fake top-level client which simply pushes the
	// received update on a channel for this test to inspect. Also pass it the
	// ClientConn created above.
	nodeID := uuid.New().String()
	callbackCh := testutils.NewChannel()
	v3c, err := newV3Client(&testUpdateReceiver{
		f: func(rType xdsclient.ResourceType, d map[string]interface{}) {
			u, ok := d[listenerName]
			if !ok {
				t.Errorf("received unexpected updated: %+v", d)
			}
			callbackCh.Send(u)
		},
	}, cc, &v3corepb.Node{Id: nodeID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer v3c.Close()
	t.Log("Started xds v3Client...")

	const streamCloseResourceName = "stream-close-resource-name"

	// Start the xDS management server.
	streamOpenCh := testutils.NewChannel()
	streamCloseCh := testutils.NewChannel()
	cb := &e2e.CallbackFuncs{
		StreamOpenFunc: func(context.Context, int64, string) error {
			streamOpenCh.Send(nil)
			return nil
		},
		StreamClosedFunc: func(int64) {
			// The `streamCloseResourceName` causes the management server to
			// return an error, which causes the stream to close. This callback
			// is invoked when the stream is closed, and we cancel the watch for
			// the `streamCloseResourceName` resource name here. So when the
			// xdsClient retries after stream close, it does not include this
			// resource name.
			v3c.RemoveWatch(xdsclient.ListenerResource, streamCloseResourceName)
			streamCloseCh.Send(nil)
		},
		StreamRequestFunc: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			// We configure the management server to return an error when the
			// `streamCloseResourceName` is requested.
			for _, name := range req.GetResourceNames() {
				if name == streamCloseResourceName {
					return errors.New("closing stream")
				}
			}
			return nil
		},
	}
	mgmtServer, err := e2e.StartManagementServer(cb)
	if err != nil {
		t.Fatal(err)
	}
	defer mgmtServer.Stop()

	// Configure the management server with a listener resource.
	if err := mgmtServer.Update(e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener},
	}); err != nil {
		t.Fatal(err)
	}

	// Push the real server address to the manual resolver. This should move the
	// ClientConn to ready state.
	rb.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: mgmtServer.Address}},
	})

	// Make sure an ADS stream is opened to the management server.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := streamOpenCh.Receive(ctx); err != nil {
		t.Fatalf("Failed to open ADS stream to management server: %v", err)
	}

	// Register a watch for a good Listener resource.
	v3c.AddWatch(xdsclient.ListenerResource, listenerName)
	if err := waitForListenerUpdate(ctx, callbackCh, xdsclient.ListenerUpdate{RouteConfigName: routeName}); err != nil {
		t.Fatal(err)
	}

	// Register a watch for a resource which causes the stream to be closed.
	v3c.AddWatch(xdsclient.ListenerResource, streamCloseResourceName)
	if _, err := streamCloseCh.Receive(ctx); err != nil {
		t.Fatalf("Failed to open ADS stream to management server: %v", err)
	}

	// Make sure a new ADS stream is opened to the management server and we
	// receive the update for the registered watch.
	if _, err := streamOpenCh.Receive(ctx); err != nil {
		t.Fatalf("Failed to open ADS stream to management server: %v", err)
	}
	if err := waitForListenerUpdate(ctx, callbackCh, xdsclient.ListenerUpdate{RouteConfigName: routeName}); err != nil {
		t.Fatal(err)
	}
}

// TestClientBackoffAfterRecvError tests the case where a stream encountered a
// Recv() error, and is expected to backoff and send out xDS requests for
// registered watchers once it comes back up again.
func (s) TestClientBackoffAfterRecvError(t *testing.T) {
	// Start the xDS management server.
	reqNum := 0
	cb := &e2e.CallbackFuncs{
		StreamRequestFunc: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			// We return an error the first time. This should cause the
			// xdsClient to backoff and retry.
			defer func() { reqNum++ }()
			if reqNum == 0 {
				return errors.New("stream error")
			}
			return nil
		},
	}
	mgmtServer, err := e2e.StartManagementServer(cb)
	if err != nil {
		t.Fatal(err)
	}
	defer mgmtServer.Stop()

	// Configure the management server with a listener resource.
	nodeID := uuid.New().String()
	if err := mgmtServer.Update(e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener},
	}); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.Dial(mgmtServer.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial management server: %v", err)
	}
	defer cc.Close()

	// Create a v3Client with the following:
	// - a fake top-level client which simply pushes the received update on a channel for this test to inspect.
	// - ClientConn to the management server created above.
	// - a backoff function which pushes on a channel
	callbackCh := testutils.NewChannel()
	boCh := testutils.NewChannel()
	v3c, err := newV3Client(&testUpdateReceiver{
		f: func(rType xdsclient.ResourceType, d map[string]interface{}) {
			u, ok := d[listenerName]
			if !ok {
				t.Errorf("received unexpected updated: %+v", d)
			}
			callbackCh.Send(u)
		},
	}, cc, &v3corepb.Node{Id: nodeID}, func(int) time.Duration {
		boCh.Send(nil)
		return time.Duration(0)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer v3c.Close()
	t.Log("Started xds v3Client...")

	// Register a watch for a Listener resource and make sure the xdsClient
	// backsoff and receives an update.
	v3c.AddWatch(xdsclient.ListenerResource, listenerName)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := boCh.Receive(ctx); err != nil {
		t.Fatalf("timeout when waiting for xdsClient to backoff: %v", err)
	}
	if err := waitForListenerUpdate(ctx, callbackCh, xdsclient.ListenerUpdate{RouteConfigName: routeName}); err != nil {
		t.Fatal(err)
	}
}

func waitForListenerUpdate(ctx context.Context, updateCh *testutils.Channel, wantUpdate xdsclient.ListenerUpdate) error {
	v, err := updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("failed to receive an LDS update: %v", err)
	}
	gotUpdate, ok := v.(xdsclient.ListenerUpdate)
	if !ok {
		return fmt.Errorf("expected an LDS update from watcher, got %v", v)
	}
	if !cmp.Equal(gotUpdate, wantUpdate) {
		return fmt.Errorf("got LDS update %v, want %v", gotUpdate, wantUpdate)
	}
	return nil
}
