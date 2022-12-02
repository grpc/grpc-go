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

package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

const (
	testAuthority1 = "test-authority1"
	testAuthority2 = "test-authority2"
	testAuthority3 = "test-authority3"
)

var (
	// These two resources use `testAuthority1`, which contains an empty server
	// config in the bootstrap file, and therefore will use the default
	// management server.
	authorityTestResourceName11 = xdstestutils.BuildResourceName(xdsresource.ClusterResource, testAuthority1, cdsName+"1", nil)
	authorityTestResourceName12 = xdstestutils.BuildResourceName(xdsresource.ClusterResource, testAuthority1, cdsName+"2", nil)
	// This resource uses `testAuthority2`, which contains an empty server
	// config in the bootstrap file, and therefore will use the default
	// management server.
	authorityTestResourceName2 = xdstestutils.BuildResourceName(xdsresource.ClusterResource, testAuthority2, cdsName+"3", nil)
	// This resource uses `testAuthority3`, which contains a non-empty server
	// config in the bootstrap file, and therefore will use the non-default
	// management server.
	authorityTestResourceName3 = xdstestutils.BuildResourceName(xdsresource.ClusterResource, testAuthority3, cdsName+"3", nil)
)

// setupForAuthorityTests spins up two management servers, one to act as the
// default and the other to act as the non-default. It also generates a
// bootstrap configuration with three authorities (the first two pointing to the
// default and the third one pointing to the non-default).
//
// Returns two listeners used by the default and non-default management servers
// respectively, and the xDS client.
func setupForAuthorityTests(ctx context.Context, t *testing.T, idleTimeout time.Duration) (*testutils.ListenerWrapper, *testutils.ListenerWrapper, xdsclient.XDSClient) {
	overrideFedEnvVar(t)

	// Create listener wrappers which notify on to a channel whenever a new
	// connection is accepted. We use this to track the number of transports
	// used by the xDS client.
	lisDefault := testutils.NewListenerWrapper(t, nil)
	lisNonDefault := testutils.NewListenerWrapper(t, nil)

	// Start a management server to act as the default authority.
	defaultAuthorityServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{Listener: lisDefault})
	if err != nil {
		t.Fatalf("Failed to spin up the xDS management server: %v", err)
	}
	t.Cleanup(func() { defaultAuthorityServer.Stop() })

	// Start a management server to act as the non-default authority.
	nonDefaultAuthorityServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{Listener: lisNonDefault})
	if err != nil {
		t.Fatalf("Failed to spin up the xDS management server: %v", err)
	}
	t.Cleanup(func() { nonDefaultAuthorityServer.Stop() })

	// Create a bootstrap configuration with two non-default authorities which
	// have empty server configs, and therefore end up using the default server
	// config, which points to the above management server.
	nodeID := uuid.New().String()
	client, err := xdsclient.NewWithConfigForTesting(&bootstrap.Config{
		XDSServer: &bootstrap.ServerConfig{
			ServerURI:    defaultAuthorityServer.Address,
			Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
			TransportAPI: version.TransportV3,
			NodeProto:    &v3corepb.Node{Id: nodeID},
		},
		Authorities: map[string]*bootstrap.Authority{
			testAuthority1: {},
			testAuthority2: {},
			testAuthority3: {
				XDSServer: &bootstrap.ServerConfig{
					ServerURI:    nonDefaultAuthorityServer.Address,
					Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
					TransportAPI: version.TransportV3,
					NodeProto:    &v3corepb.Node{Id: nodeID},
				},
			},
		},
	}, defaultTestWatchExpiryTimeout, idleTimeout)
	if err != nil {
		t.Fatalf("failed to create xds client: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Clusters: []*v3clusterpb.Cluster{
			e2e.DefaultCluster(authorityTestResourceName11, edsName, e2e.SecurityLevelNone),
			e2e.DefaultCluster(authorityTestResourceName12, edsName, e2e.SecurityLevelNone),
			e2e.DefaultCluster(authorityTestResourceName2, edsName, e2e.SecurityLevelNone),
			e2e.DefaultCluster(authorityTestResourceName3, edsName, e2e.SecurityLevelNone),
		},
		SkipValidation: true,
	}
	if err := defaultAuthorityServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server with resources: %v, err: %v", resources, err)
	}
	return lisDefault, lisNonDefault, client
}

// TestAuthorityShare tests the authority sharing logic. The test verifies the
// following scenarios:
//   - A watch for a resource name with an authority matching an existing watch
//     should not result in a new transport being created.
//   - A watch for a resource name with different authority name but same
//     authority config as an existing watch should not result in a new transport
//     being created.
func (s) TestAuthorityShare(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	lis, _, client := setupForAuthorityTests(ctx, t, time.Duration(0))

	// Verify that no connection is established to the management server at this
	// point. A transport is created only when a resource (which belongs to that
	// authority) is requested.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := lis.NewConnCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Unexpected new transport created to management server")
	}

	// Request the first resource. Verify that a new transport is created.
	cdsCancel1 := client.WatchCluster(authorityTestResourceName11, func(u xdsresource.ClusterUpdate, err error) {})
	defer cdsCancel1()
	if _, err := lis.NewConnCh.Receive(ctx); err != nil {
		t.Fatalf("Timed out when waiting for a new transport to be created to the management server: %v", err)
	}

	// Request the second resource. Verify that no new transport is created.
	cdsCancel2 := client.WatchCluster(authorityTestResourceName12, func(u xdsresource.ClusterUpdate, err error) {})
	defer cdsCancel2()
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := lis.NewConnCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Unexpected new transport created to management server")
	}

	// Request the third resource. Verify that no new transport is created.
	cdsCancel3 := client.WatchCluster(authorityTestResourceName2, func(u xdsresource.ClusterUpdate, err error) {})
	defer cdsCancel3()
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := lis.NewConnCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Unexpected new transport created to management server")
	}
}

// TestAuthorityIdle test the authority idle timeout logic. The test verifies
// that the xDS client does not close authorities immediately after the last
// watch is canceled, but waits for the configured idle timeout to expire before
// closing them.
func (s) TestAuthorityIdleTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	lis, _, client := setupForAuthorityTests(ctx, t, defaultTestIdleAuthorityTimeout)

	// Request the first resource. Verify that a new transport is created.
	cdsCancel1 := client.WatchCluster(authorityTestResourceName11, func(u xdsresource.ClusterUpdate, err error) {})
	val, err := lis.NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timed out when waiting for a new transport to be created to the management server: %v", err)
	}
	conn := val.(*testutils.ConnWrapper)

	// Request the second resource. Verify that no new transport is created.
	cdsCancel2 := client.WatchCluster(authorityTestResourceName12, func(u xdsresource.ClusterUpdate, err error) {})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := lis.NewConnCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Unexpected new transport created to management server")
	}

	// Cancel both watches, and verify that the connection to the management
	// server is not closed immediately.
	cdsCancel1()
	cdsCancel2()
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := conn.CloseCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Connection to management server closed unexpectedly")
	}

	// Wait for the authority idle timeout to fire.
	time.Sleep(2 * defaultTestIdleAuthorityTimeout)
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := conn.CloseCh.Receive(sCtx); err != nil {
		t.Fatal("Connection to management server not closed after idle timeout expiry")
	}
}

// TestAuthorityClientClose verifies that authorities in use and in the idle
// cache are all closed when the client is closed.
func (s) TestAuthorityClientClose(t *testing.T) {
	// Set the authority idle timeout to twice the defaultTestTimeout. This will
	// ensure that idle authorities stay in the cache for the duration of this
	// test, until explicitly closed.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	lisDefault, lisNonDefault, client := setupForAuthorityTests(ctx, t, time.Duration(2*defaultTestTimeout))

	// Request the first resource. Verify that a new transport is created to the
	// default management server.
	cdsCancel1 := client.WatchCluster(authorityTestResourceName11, func(u xdsresource.ClusterUpdate, err error) {})
	val, err := lisDefault.NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timed out when waiting for a new transport to be created to the management server: %v", err)
	}
	connDefault := val.(*testutils.ConnWrapper)

	// Request another resource which is served by the non-default authority.
	// Verify that a new transport is created to the non-default management
	// server.
	client.WatchCluster(authorityTestResourceName3, func(u xdsresource.ClusterUpdate, err error) {})
	val, err = lisNonDefault.NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timed out when waiting for a new transport to be created to the management server: %v", err)
	}
	connNonDefault := val.(*testutils.ConnWrapper)

	// Cancel the first watch. This should move the default authority to the
	// idle cache, but the connection should not be closed yet, because the idle
	// timeout would not have fired.
	cdsCancel1()
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := connDefault.CloseCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Connection to management server closed unexpectedly")
	}

	// Closing the xDS client should close the connection to both management
	// servers, even though we have an open watch to one of them.
	client.Close()
	if _, err := connDefault.CloseCh.Receive(ctx); err != nil {
		t.Fatal("Connection to management server not closed after client close")
	}
	if _, err := connNonDefault.CloseCh.Receive(ctx); err != nil {
		t.Fatal("Connection to management server not closed after client close")
	}
}

// TestAuthorityRevive verifies that an authority in the idle cache is revived
// when a new watch is started on this authority.
func (s) TestAuthorityRevive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	lis, _, client := setupForAuthorityTests(ctx, t, defaultTestIdleAuthorityTimeout)

	// Request the first resource. Verify that a new transport is created.
	cdsCancel1 := client.WatchCluster(authorityTestResourceName11, func(u xdsresource.ClusterUpdate, err error) {})
	val, err := lis.NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timed out when waiting for a new transport to be created to the management server: %v", err)
	}
	conn := val.(*testutils.ConnWrapper)

	// Cancel the above watch. This should move the authority to the idle cache.
	cdsCancel1()

	// Request the second resource. Verify that no new transport is created.
	// This should move the authority out of the idle cache.
	cdsCancel2 := client.WatchCluster(authorityTestResourceName12, func(u xdsresource.ClusterUpdate, err error) {})
	defer cdsCancel2()
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := lis.NewConnCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Unexpected new transport created to management server")
	}

	// Wait for double the idle timeout, and the connection to the management
	// server should not be closed, since it was revived from the idle cache.
	time.Sleep(2 * defaultTestIdleAuthorityTimeout)
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := conn.CloseCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("Connection to management server closed unexpectedly")
	}
}
