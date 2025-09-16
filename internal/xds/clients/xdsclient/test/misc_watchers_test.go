/*
 *
 * Copyright 2022 gRPC authors.
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

package xdsclient_test

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils/e2e"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils/fakeserver"
	"google.golang.org/grpc/internal/xds/clients/xdsclient"
	"google.golang.org/grpc/internal/xds/clients/xdsclient/internal/xdsresource"
	"google.golang.org/protobuf/types/known/anypb"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// testLDSWatcher is a test watcher that registers two watches corresponding to
// the names passed in at creation time on a valid update.
type testLDSWatcher struct {
	client           *xdsclient.XDSClient
	name1, name2     string
	lw1, lw2         *listenerWatcher
	cancel1, cancel2 func()
	updateCh         *testutils.Channel
}

func newTestLDSWatcher(client *xdsclient.XDSClient, name1, name2 string) *testLDSWatcher {
	return &testLDSWatcher{
		client:   client,
		name1:    name1,
		name2:    name2,
		lw1:      newListenerWatcher(),
		lw2:      newListenerWatcher(),
		updateCh: testutils.NewChannelWithSize(1),
	}
}

func (lw *testLDSWatcher) ResourceChanged(update xdsclient.ResourceData, onDone func()) {
	lisData, ok := update.(*listenerResourceData)
	if !ok {
		lw.updateCh.Send(listenerUpdateErrTuple{resourceErr: fmt.Errorf("unexpected resource type: %T", update)})
		onDone()
		return
	}
	lw.updateCh.Send(listenerUpdateErrTuple{update: lisData.Resource})

	lw.cancel1 = lw.client.WatchResource(xdsresource.V3ListenerURL, lw.name1, lw.lw1)
	lw.cancel2 = lw.client.WatchResource(xdsresource.V3ListenerURL, lw.name2, lw.lw2)
	onDone()
}

func (lw *testLDSWatcher) AmbientError(err error, onDone func()) {
	// When used with a go-control-plane management server that continuously
	// resends resources which are NACKed by the xDS client, using a `Replace()`
	// here and in OnResourceDoesNotExist() simplifies tests which will have
	// access to the most recently received error.
	lw.updateCh.Replace(listenerUpdateErrTuple{ambientErr: err})
	onDone()
}

func (lw *testLDSWatcher) ResourceError(_ error, onDone func()) {
	lw.updateCh.Replace(listenerUpdateErrTuple{resourceErr: xdsresource.NewError(xdsresource.ErrorTypeResourceNotFound, "Listener not found in received response")})
	onDone()
}

func (lw *testLDSWatcher) cancel() {
	lw.cancel1()
	lw.cancel2()
}

// TestWatchCallAnotherWatch tests the scenario where a watch is registered for
// a resource, and more watches are registered from the first watch's callback.
// The test verifies that this scenario does not lead to a deadlock.
func (s) TestWatchCallAnotherWatch(t *testing.T) {
	// Start an xDS management server and set the option to allow it to respond
	// to requests which only specify a subset of the configured resources.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})

	nodeID := uuid.New().String()
	authority := makeAuthorityName(t.Name())

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		// Xdstp style resource names used in this test use a slash removed
		// version of t.Name as their authority, and the empty config
		// results in the top-level xds server configuration being used for
		// this authority.
		Authorities: map[string]xdsclient.Authority{
			authority: {XDSServers: []xdsclient.ServerConfig{}},
		},
	}

	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Configure the management server to return two listener resources,
	// corresponding to the registered watches.
	ldsNameNewStyle := makeNewStyleLDSName(authority)
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Listeners: []*v3listenerpb.Listener{
			e2e.DefaultClientListener(ldsName, rdsName),
			e2e.DefaultClientListener(ldsNameNewStyle, rdsNameNewStyle),
		},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}

	// Create a listener watcher that registers two more watches from
	// the OnUpdate callback:
	// - one for the same resource name as this watch, which would be
	//   satisfied from xdsClient cache
	// - the other for a different resource name, which would be
	//   satisfied from the server
	lw := newTestLDSWatcher(client, ldsName, ldsNameNewStyle)
	defer lw.cancel()
	ldsCancel := client.WatchResource(xdsresource.V3ListenerURL, ldsName, lw)
	defer ldsCancel()

	// Verify the contents of the received update for the all watchers.
	// Verify the contents of the received update for the all watchers. The two
	// resources returned differ only in the resource name. Therefore the
	// expected update is the same for all the watchers.
	wantUpdate12 := listenerUpdateErrTuple{
		update: listenerUpdate{
			RouteConfigName: rdsName,
		},
	}
	// Verify the contents of the received update for the all watchers. The two
	// resources returned differ only in the resource name. Therefore the
	// expected update is the same for all the watchers.
	wantUpdate3 := listenerUpdateErrTuple{
		update: listenerUpdate{
			RouteConfigName: rdsNameNewStyle,
		},
	}
	if err := verifyListenerUpdate(ctx, lw.updateCh, wantUpdate12); err != nil {
		t.Fatal(err)
	}
	if err := verifyListenerUpdate(ctx, lw.lw1.updateCh, wantUpdate12); err != nil {
		t.Fatal(err)
	}
	if err := verifyListenerUpdate(ctx, lw.lw2.updateCh, wantUpdate3); err != nil {
		t.Fatal(err)
	}
}

// TestNodeProtoSentOnlyInFirstRequest verifies that a non-empty node proto gets
// sent only on the first discovery request message on the ADS stream.
//
// It also verifies the same behavior holds after a stream restart.
func (s) TestNodeProtoSentOnlyInFirstRequest(t *testing.T) {
	// Create a restartable listener which can close existing connections.
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening. Err: %v", err)
	}
	lis := testutils.NewRestartableListener(l)

	// Start a fake xDS management server with the above restartable listener.
	//
	// We are unable to use the go-control-plane server here, because it caches
	// the node proto received in the first request message and adds it to
	// subsequent requests before invoking the OnStreamRequest() callback.
	// Therefore we cannot verify what is sent by the xDS client.
	mgmtServer, cleanup, err := fakeserver.StartServer(lis)
	if err != nil {
		t.Fatalf("Failed to start fake xDS server: %v", err)
	}
	defer cleanup()

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		// Xdstp resource names used in this test do not specify an
		// authority. These will end up looking up an entry with the
		// empty key in the authorities map. Having an entry with an
		// empty key and empty configuration, results in these
		// resources also using the top-level configuration.
		Authorities: map[string]xdsclient.Authority{
			"": {XDSServers: []xdsclient.ServerConfig{}},
		},
	}

	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	const (
		serviceName     = "my-service-client-side-xds"
		routeConfigName = "route-" + serviceName
		clusterName     = "cluster-" + serviceName
		serviceName2    = "my-service-client-side-xds-2"
	)

	// Register a watch for the Listener resource.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	watcher := newListenerWatcher()
	ldsCancel1 := client.WatchResource(xdsresource.V3ListenerURL, serviceName, watcher)
	defer ldsCancel1()

	// Ensure the watch results in a discovery request with an empty node proto.
	if err := readDiscoveryResponseAndCheckForNonEmptyNodeProto(ctx, mgmtServer.XDSRequestChan); err != nil {
		t.Fatal(err)
	}

	// Configure a listener resource on the fake xDS server.
	lisAny, err := anypb.New(e2e.DefaultClientListener(serviceName, routeConfigName))
	if err != nil {
		t.Fatalf("Failed to marshal listener resource into an Any proto: %v", err)
	}
	mgmtServer.XDSResponseChan <- &fakeserver.Response{
		Resp: &v3discoverypb.DiscoveryResponse{
			TypeUrl:     "type.googleapis.com/envoy.config.listener.v3.Listener",
			VersionInfo: "1",
			Resources:   []*anypb.Any{lisAny},
		},
	}

	// The xDS client is expected to ACK the Listener resource. The discovery
	// request corresponding to the ACK must contain a nil node proto.
	if err := readDiscoveryResponseAndCheckForEmptyNodeProto(ctx, mgmtServer.XDSRequestChan); err != nil {
		t.Fatal(err)
	}

	// Register a watch for another Listener resource.
	ldscancel2 := client.WatchResource(xdsresource.V3ListenerURL, serviceName2, watcher)
	defer ldscancel2()

	// Ensure the watch results in a discovery request with an empty node proto.
	if err := readDiscoveryResponseAndCheckForEmptyNodeProto(ctx, mgmtServer.XDSRequestChan); err != nil {
		t.Fatal(err)
	}

	// Configure the other listener resource on the fake xDS server.
	lisAny2, err := anypb.New(e2e.DefaultClientListener(serviceName2, routeConfigName))

	if err != nil {
		t.Fatalf("Failed to marshal route configuration resource into an Any proto: %v", err)
	}

	mgmtServer.XDSResponseChan <- &fakeserver.Response{
		Resp: &v3discoverypb.DiscoveryResponse{
			TypeUrl:     "type.googleapis.com/envoy.config.listener.v3.Listener",
			VersionInfo: "1",
			Resources:   []*anypb.Any{lisAny2},
		},
	}

	// Ensure the discovery request for the ACK contains an empty node proto.
	if err := readDiscoveryResponseAndCheckForEmptyNodeProto(ctx, mgmtServer.XDSRequestChan); err != nil {
		t.Fatal(err)
	}

	// Stop the management server and expect the error callback to be invoked.
	lis.Stop()
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for the connection error to be propagated to the watcher")
	case <-watcher.ambientErrCh.C:
	}
	// Restart the management server.
	lis.Restart()

	// The xDS client is expected to re-request previously requested resources.
	// Here, we expect 1 DiscoveryRequest messages with both the listener resources.
	// The message should contain a non-nil node proto (since its the first
	// request after restart).
	//
	// And since we don't push any responses on the response channel of the fake
	// server, we do not expect any ACKs here.
	if err := readDiscoveryResponseAndCheckForNonEmptyNodeProto(ctx, mgmtServer.XDSRequestChan); err != nil {
		t.Fatal(err)
	}
}

// readDiscoveryResponseAndCheckForEmptyNodeProto reads a discovery request
// message out of the provided reqCh. It returns an error if it fails to read a
// message before the context deadline expires, or if the read message contains
// a non-empty node proto.
func readDiscoveryResponseAndCheckForEmptyNodeProto(ctx context.Context, reqCh *testutils.Channel) error {
	v, err := reqCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("Timeout when waiting for a DiscoveryRequest message")
	}
	req := v.(*fakeserver.Request).Req.(*v3discoverypb.DiscoveryRequest)
	if node := req.GetNode(); node != nil {
		return fmt.Errorf("Node proto received in DiscoveryRequest message is %v, want empty node proto", node)
	}
	return nil
}

// readDiscoveryResponseAndCheckForNonEmptyNodeProto reads a discovery request
// message out of the provided reqCh. It returns an error if it fails to read a
// message before the context deadline expires, or if the read message contains
// an empty node proto.
func readDiscoveryResponseAndCheckForNonEmptyNodeProto(ctx context.Context, reqCh *testutils.Channel) error {
	v, err := reqCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("Timeout when waiting for a DiscoveryRequest message")
	}
	req := v.(*fakeserver.Request).Req.(*v3discoverypb.DiscoveryRequest)
	if node := req.GetNode(); node == nil {
		return fmt.Errorf("Empty node proto received in DiscoveryRequest message, want non-empty node proto")
	}
	return nil
}

// Tests that the errors returned by the xDS client when watching a resource
// contain the node ID that was used to create the client. This test covers two
// scenarios:
//
//  1. When a watch is registered for an already registered resource type, but
//     a new watch is registered with a type url which is not present in
//     provided resource types.
//  2. When a watch is registered for a resource name whose authority is not
//     found in the xDS client config.
func (s) TestWatchErrorsContainNodeID(t *testing.T) {
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		// Xdstp resource names used in this test do not specify an
		// authority. These will end up looking up an entry with the
		// empty key in the authorities map. Having an entry with an
		// empty key and empty configuration, results in these
		// resources also using the top-level configuration.
		Authorities: map[string]xdsclient.Authority{
			"": {XDSServers: []xdsclient.ServerConfig{}},
		},
	}

	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	t.Run("Right_Wrong_ResourceType_Implementations", func(t *testing.T) {
		const listenerName = "listener-name"
		watcher := newListenerWatcher()
		client.WatchResource(xdsresource.V3ListenerURL, listenerName, watcher)

		sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
		defer sCancel()
		select {
		case <-sCtx.Done():
		case <-watcher.updateCh.C:
			t.Fatal("Unexpected resource update")
		case <-watcher.resourceErrCh.C:
			t.Fatal("Unexpected resource error")
		}

		client.WatchResource("non-existent-type-url", listenerName, watcher)
		select {
		case <-ctx.Done():
			t.Fatal("Timeout when waiting for error callback to be invoked")
		case u, ok := <-watcher.resourceErrCh.C:
			if !ok {
				t.Fatalf("got no update, wanted listener resource error from the management server")
			}
			gotErr := u.(listenerUpdateErrTuple).resourceErr
			if !strings.Contains(gotErr.Error(), nodeID) {
				t.Fatalf("Unexpected error: %v, want error with node ID: %q", err, nodeID)
			}
		}
	})

	t.Run("Missing_Authority", func(t *testing.T) {
		const listenerName = "xdstp://nonexistant-authority/envoy.config.listener.v3.Listener/listener-name"
		watcher := newListenerWatcher()
		client.WatchResource(xdsresource.V3ListenerURL, listenerName, watcher)

		select {
		case <-ctx.Done():
			t.Fatal("Timeout when waiting for error callback to be invoked")
		case u, ok := <-watcher.resourceErrCh.C:
			if !ok {
				t.Fatalf("got no update, wanted listener resource error from the management server")
			}
			gotErr := u.(listenerUpdateErrTuple).resourceErr
			if !strings.Contains(gotErr.Error(), nodeID) {
				t.Fatalf("Unexpected error: %v, want error with node ID: %q", err, nodeID)
			}
		}
	})
}

// erroringTransportBuilder is a transport builder which always returns a nil
// transport along with an error.
type erroringTransportBuilder struct{}

func (*erroringTransportBuilder) Build(_ clients.ServerIdentifier) (clients.Transport, error) {
	return nil, fmt.Errorf("failed to create transport")
}

// Tests that the errors returned by the xDS client when watching a resource
// contain the node ID when channel creation to the management server fails.
func (s) TestWatchErrorsContainNodeID_ChannelCreationFailure(t *testing.T) {
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: &erroringTransportBuilder{},
		ResourceTypes:    resourceTypes,
		// Xdstp resource names used in this test do not specify an
		// authority. These will end up looking up an entry with the
		// empty key in the authorities map. Having an entry with an
		// empty key and empty configuration, results in these
		// resources also using the top-level configuration.
		Authorities: map[string]xdsclient.Authority{
			"": {XDSServers: []xdsclient.ServerConfig{}},
		},
	}

	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	const listenerName = "listener-name"
	watcher := newListenerWatcher()
	client.WatchResource(xdsresource.V3ListenerURL, listenerName, watcher)

	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for error callback to be invoked")
	case u, ok := <-watcher.resourceErrCh.C:
		if !ok {
			t.Fatalf("got no update, wanted listener resource error from the management server")
		}
		gotErr := u.(listenerUpdateErrTuple).resourceErr
		if !strings.Contains(gotErr.Error(), nodeID) {
			t.Fatalf("Unexpected error: %v, want error with node ID: %q", err, nodeID)
		}
	}
}
