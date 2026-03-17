/*
 *
 * Copyright 2024 gRPC authors.
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
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils"
	"google.golang.org/grpc/internal/xds/clients/xdsclient"
	xdsclientinternal "google.golang.org/grpc/internal/xds/clients/xdsclient/internal"
	"google.golang.org/grpc/internal/xds/clients/xdsclient/internal/xdsresource"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
)

func waitForResourceWatchState(ctx context.Context, client *xdsclient.XDSClient, resourceName string, wantState xdsresource.WatchState, wantTimer bool) error {
	var lastErr error
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		err := verifyResourceWatchState(client, resourceName, wantState, wantTimer)
		if err == nil {
			break
		}
		lastErr = err
	}
	if ctx.Err() != nil {
		return fmt.Errorf("timeout when waiting for expected watch state for resource %q: %v", resourceName, lastErr)
	}
	return nil
}

func verifyResourceWatchState(client *xdsclient.XDSClient, resourceName string, wantState xdsresource.WatchState, wantTimer bool) error {
	resourceWatchStateForTesting := xdsclientinternal.ResourceWatchStateForTesting.(func(*xdsclient.XDSClient, xdsclient.ResourceType, string) (xdsresource.ResourceWatchState, error))
	gotState, err := resourceWatchStateForTesting(client, listenerType, resourceName)
	if err != nil {
		return fmt.Errorf("failed to get watch state for resource %q: %v", resourceName, err)
	}
	if gotState.State != wantState {
		return fmt.Errorf("watch state for resource %q is %v, want %v", resourceName, gotState.State, wantState)
	}
	if (gotState.ExpiryTimer != nil) != wantTimer {
		return fmt.Errorf("expiry timer for resource %q is %t, want %t", resourceName, gotState.ExpiryTimer != nil, wantTimer)
	}
	return nil
}

// Tests the state transitions of the resource specific watch state within the
// ADS stream, specifically when the stream breaks (for both resources that have
// been previously received and for resources that are yet to be received).
func (s) TestADS_WatchState_StreamBreaks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create an xDS management server with a restartable listener.
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: lis})

	// Create an xDS client pointing to the above server.
	nodeID := uuid.New().String()
	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	client := createXDSClient(t, mgmtServer.Address, nodeID, grpctransport.NewBuilder(configs))

	// Create a watch for the first listener resource and verify that the timer
	// is running and the watch state is `requested`.
	const listenerName1 = "listener1"
	ldsCancel1 := client.WatchResource(xdsresource.V3ListenerURL, listenerName1, noopListenerWatcher{})
	defer ldsCancel1()
	if err := waitForResourceWatchState(ctx, client, listenerName1, xdsresource.ResourceWatchStateRequested, true); err != nil {
		t.Fatal(err)
	}

	// Configure the first resource on the management server. This should result
	// in the resource being pushed to the xDS client and should result in the
	// timer getting stopped and the watch state moving to `received`.
	const routeConfigName = "route-config"
	listenerResource1 := e2e.DefaultClientListener(listenerName1, routeConfigName)
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listenerResource1},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if err := waitForResourceWatchState(ctx, client, listenerName1, xdsresource.ResourceWatchStateReceived, false); err != nil {
		t.Fatal(err)
	}

	// Create a watch for the second listener resource and verify that the timer
	// is running and the watch state is `requested`.
	const listenerName2 = "listener2"
	ldsCancel2 := client.WatchResource(xdsresource.V3ListenerURL, listenerName2, noopListenerWatcher{})
	defer ldsCancel2()
	if err := waitForResourceWatchState(ctx, client, listenerName2, xdsresource.ResourceWatchStateRequested, true); err != nil {
		t.Fatal(err)
	}

	// Stop the server to break the ADS stream. Since the first resource was
	// already received, this should not change anything for it. But for the
	// second resource, it should result in the timer getting stopped and the
	// watch state moving to `started`.
	lis.Stop()
	if err := waitForResourceWatchState(ctx, client, listenerName2, xdsresource.ResourceWatchStateStarted, false); err != nil {
		t.Fatal(err)
	}
	if err := verifyResourceWatchState(client, listenerName1, xdsresource.ResourceWatchStateReceived, false); err != nil {
		t.Fatal(err)
	}

	// Restart the server and verify that the timer is running and the watch
	// state is `requested`, for the second resource. For the first resource,
	// nothing should change.
	lis.Restart()
	if err := waitForResourceWatchState(ctx, client, listenerName2, xdsresource.ResourceWatchStateRequested, true); err != nil {
		t.Fatal(err)
	}
	if err := verifyResourceWatchState(client, listenerName1, xdsresource.ResourceWatchStateReceived, false); err != nil {
		t.Fatal(err)
	}

	// Configure the second resource on the management server. This should result
	// in the resource being pushed to the xDS client and should result in the
	// timer getting stopped and the watch state moving to `received`.
	listenerResource2 := e2e.DefaultClientListener(listenerName2, routeConfigName)
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listenerResource1, listenerResource2},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if err := waitForResourceWatchState(ctx, client, listenerName2, xdsresource.ResourceWatchStateReceived, false); err != nil {
		t.Fatal(err)
	}
}

// Tests the behavior of the xDS client when a resource watch timer expires and
// verifies the resource watch state transitions as expected.
func (s) TestADS_WatchState_TimerFires(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create an xDS client with bootstrap pointing to the above server, and a
	// short resource expiry timeout.
	nodeID := uuid.New().String()
	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	si := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: si}},
		Node:             clients.Node{ID: nodeID, UserAgentName: "user-agent", UserAgentVersion: "0.0.0.0"},
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
		WatchExpiryTimeout: defaultTestWatchExpiryTimeout,
	}

	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	// Create a watch for the first listener resource and verify that the timer
	// is running and the watch state is `requested`.
	const listenerName = "listener"
	ldsCancel1 := client.WatchResource(xdsresource.V3ListenerURL, listenerName, noopListenerWatcher{})
	defer ldsCancel1()
	if err := waitForResourceWatchState(ctx, client, listenerName, xdsresource.ResourceWatchStateRequested, true); err != nil {
		t.Fatal(err)
	}

	// Since the resource is not configured on the management server, the watch
	// expiry timer is expected to fire, and the watch state should move to
	// `timeout`.
	if err := waitForResourceWatchState(ctx, client, listenerName, xdsresource.ResourceWatchStateTimeout, false); err != nil {
		t.Fatal(err)
	}
}
