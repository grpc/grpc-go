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
	"net"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils/e2e"
	"google.golang.org/grpc/internal/xds/clients/xdsclient"
	"google.golang.org/grpc/internal/xds/clients/xdsclient/internal/xdsresource"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
)

const (
	testAuthority1 = "test-authority1"
	testAuthority2 = "test-authority2"
	testAuthority3 = "test-authority3"
)

var (
	// These two resources use `testAuthority1`, which contains an empty server
	// config and therefore will use the default management server.
	authorityTestResourceName11 = buildResourceName(listenerResourceTypeName, testAuthority1, ldsName, nil)
	authorityTestResourceName12 = buildResourceName(listenerResourceTypeName, testAuthority1, ldsName+"2", nil)
	// This resource uses `testAuthority2`, which contains an empty server
	// config and therefore will use the default management server.
	authorityTestResourceName2 = buildResourceName(listenerResourceTypeName, testAuthority2, ldsName+"3", nil)
	// This resource uses `testAuthority3`, which contains a non-empty server
	// config, and therefore will use the non-default management server.
	authorityTestResourceName3 = buildResourceName(listenerResourceTypeName, testAuthority3, ldsName+"3", nil)
)

// setupForAuthorityTests spins up two management servers, one to act as the
// default and the other to act as the non-default. It also creates a
// xDS client configuration with three authorities (the first two pointing to
// the default and the third one pointing to the non-default).
//
// Returns two listeners used by the default and non-default management servers
// respectively, and the xDS client.
func setupForAuthorityTests(ctx context.Context, t *testing.T) (*testutils.ListenerWrapper, *testutils.ListenerWrapper, *xdsclient.XDSClient) {
	// Create listener wrappers which notify on to a channel whenever a new
	// connection is accepted. We use this to track the number of transports
	// used by the xDS client.
	lisDefault := testutils.NewListenerWrapper(t, nil)
	lisNonDefault := testutils.NewListenerWrapper(t, nil)

	// Start a management server to act as the default authority.
	defaultAuthorityServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: lisDefault})

	// Start a management server to act as the non-default authority.
	nonDefaultAuthorityServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: lisNonDefault})

	// Create a bootstrap configuration with two non-default authorities which
	// have empty server configs, and therefore end up using the default server
	// config, which points to the above management server.
	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	ext := grpctransport.ServerIdentifierExtension{ConfigName: "insecure"}
	siDefault := clients.ServerIdentifier{
		ServerURI:  defaultAuthorityServer.Address,
		Extensions: ext,
	}
	siNonDefault := clients.ServerIdentifier{
		ServerURI:  nonDefaultAuthorityServer.Address,
		Extensions: ext,
	}

	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: siDefault}},
		Node:             clients.Node{ID: nodeID},
		TransportBuilder: grpctransport.NewBuilder(configs),
		ResourceTypes:    resourceTypes,
		// Xdstp style resource names used in this test use a slash removed
		// version of t.Name as their authority, and the empty config
		// results in the top-level xds server configuration being used for
		// this authority.
		Authorities: map[string]xdsclient.Authority{
			testAuthority1: {XDSServers: []xdsclient.ServerConfig{}},
			testAuthority2: {XDSServers: []xdsclient.ServerConfig{}},
			testAuthority3: {XDSServers: []xdsclient.ServerConfig{{ServerIdentifier: siNonDefault}}},
		},
		WatchExpiryTimeout: defaultTestWatchExpiryTimeout,
	}

	// Create an xDS client with the above config.
	client, err := xdsclient.New(xdsClientConfig)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}

	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Listeners: []*v3listenerpb.Listener{
			e2e.DefaultClientListener(authorityTestResourceName11, rdsName),
			e2e.DefaultClientListener(authorityTestResourceName12, rdsName),
			e2e.DefaultClientListener(authorityTestResourceName2, rdsName),
			e2e.DefaultClientListener(authorityTestResourceName3, rdsName),
		},
		SkipValidation: true,
	}
	if err := defaultAuthorityServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}
	return lisDefault, lisNonDefault, client
}

// Tests the xdsChannel sharing logic among authorities. The test verifies the
// following scenarios:
//   - A watch for a resource name with an authority matching an existing watch
//     should not result in a new transport being created.
//   - A watch for a resource name with different authority name but same
//     authority config as an existing watch should not result in a new transport
//     being created.
func (s) TestAuthority_XDSChannelSharing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	lis, _, client := setupForAuthorityTests(ctx, t)
	defer client.Close()

	// Verify that no connection is established to the management server at this
	// point. A transport is created only when a resource (which belongs to that
	// authority) is requested.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := lis.NewConnCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Unexpected new transport created to management server")
	}

	// Request the first resource. Verify that a new transport is created.
	watcher := noopListenerWatcher{}
	ldsCancel1 := client.WatchResource(xdsresource.V3ListenerURL, authorityTestResourceName11, watcher)
	defer ldsCancel1()
	if _, err := lis.NewConnCh.Receive(ctx); err != nil {
		t.Fatalf("Timed out when waiting for a new transport to be created to the management server: %v", err)
	}

	// Request the second resource. Verify that no new transport is created.
	ldsCancel2 := client.WatchResource(xdsresource.V3ListenerURL, authorityTestResourceName12, watcher)
	defer ldsCancel2()
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := lis.NewConnCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Unexpected new transport created to management server")
	}

	// Request the third resource. Verify that no new transport is created.
	ldsCancel3 := client.WatchResource(xdsresource.V3ListenerURL, authorityTestResourceName2, watcher)
	defer ldsCancel3()
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := lis.NewConnCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Unexpected new transport created to management server")
	}
}

// Test the xdsChannel close logic. The test verifies that the xDS client
// closes an xdsChannel immediately after the last watch is canceled.
func (s) TestAuthority_XDSChannelClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	lis, _, client := setupForAuthorityTests(ctx, t)
	defer client.Close()

	// Request the first resource. Verify that a new transport is created.
	watcher := noopListenerWatcher{}
	ldsCancel1 := client.WatchResource(xdsresource.V3ListenerURL, authorityTestResourceName11, watcher)
	val, err := lis.NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timed out when waiting for a new transport to be created to the management server: %v", err)
	}
	conn := val.(*testutils.ConnWrapper)

	// Request the second resource. Verify that no new transport is created.
	ldsCancel2 := client.WatchResource(xdsresource.V3ListenerURL, authorityTestResourceName12, watcher)
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := lis.NewConnCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Unexpected new transport created to management server")
	}

	// Cancel both watches, and verify that the connection to the management
	// server is closed.
	ldsCancel1()
	ldsCancel2()
	if _, err := conn.CloseCh.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for connection to management server to be closed")
	}
}

// Tests the scenario where the primary management server is unavailable at
// startup and the xDS client falls back to the secondary.  The test verifies
// that the resource watcher is not notified of the connectivity failure until
// all servers have failed.
func (s) TestAuthority_Fallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create primary and secondary management servers with restartable
	// listeners.
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	primaryLis := testutils.NewRestartableListener(l)
	primaryMgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: primaryLis})
	l, err = net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	secondaryLis := testutils.NewRestartableListener(l)
	secondaryMgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: secondaryLis})

	nodeID := uuid.New().String()

	resourceTypes := map[string]xdsclient.ResourceType{xdsresource.V3ListenerURL: listenerType}
	psi := clients.ServerIdentifier{
		ServerURI:  primaryMgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}
	ssi := clients.ServerIdentifier{
		ServerURI:  secondaryMgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}

	// Create config with the above primary and fallback management servers,
	// and an xDS client with that configuration.
	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	xdsClientConfig := xdsclient.Config{
		Servers:          []xdsclient.ServerConfig{{ServerIdentifier: psi}, {ServerIdentifier: ssi}},
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

	const listenerName = "listener"
	const rdsPrimaryName = "rds-primary"
	const rdsSecondaryName = "rds-secondary"

	// Create a Cluster resource on the primary.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerName, rdsPrimaryName)},
		SkipValidation: true,
	}
	if err := primaryMgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update primary management server with resources: %v, err: %v", resources, err)
	}

	// Create a Cluster resource on the secondary .
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerName, rdsSecondaryName)},
		SkipValidation: true,
	}
	if err := secondaryMgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update primary management server with resources: %v, err: %v", resources, err)
	}

	// Stop the primary.
	primaryLis.Close()

	// Register a watch.
	watcher := newListenerWatcher()
	ldsCancel := client.WatchResource(xdsresource.V3ListenerURL, listenerName, watcher)
	defer ldsCancel()

	// Ensure that the connectivity error callback is not called. Since, this
	// is the first watch without cached resource, it checks for resourceErrCh
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if v, err := watcher.resourceErrCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("Resource error callback on the watcher with error:  %v", v.(error))
	}

	// Ensure that the resource update callback is invoked.
	wantUpdate := listenerUpdateErrTuple{
		update: listenerUpdate{
			RouteConfigName: rdsSecondaryName,
		},
	}
	if err := verifyListenerUpdate(ctx, watcher.updateCh, wantUpdate); err != nil {
		t.Fatal(err)
	}

	// Stop the secondary.
	secondaryLis.Close()

	// Ensure that the connectivity error callback is called as ambient error
	// since cached resource exist.
	if _, err := watcher.ambientErrCh.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for ambient error callback on the watcher")
	}
}
