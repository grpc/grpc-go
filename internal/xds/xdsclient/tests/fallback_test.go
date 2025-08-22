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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/status"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

// Give the fallback tests additional time to complete because they need to
// first identify failed connections before establishing new ones.
const defaultFallbackTestTimeout = 2 * defaultTestTimeout

func waitForRPCsToReachBackend(ctx context.Context, client testgrpc.TestServiceClient, backend string) error {
	var lastErr error
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		var peer peer.Peer
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer)); err != nil {
			lastErr = err
			continue
		}
		// Veirfy the peer when the RPC succeeds.
		if peer.Addr.String() == backend {
			break
		}
	}
	if ctx.Err() != nil {
		return fmt.Errorf("timeout when waiting for RPCs to reach expected backend. Last error: %v", lastErr)
	}
	return nil
}

// Tests fallback on startup where the xDS client is unable to establish a
// connection to the primary server. The test verifies that the xDS client falls
// back to the secondary server, and when the primary comes back up, it reverts
// to it. The test also verifies that when all requested resources are cached
// from the primary, fallback is not triggered when the connection goes down.
func (s) TestFallback_OnStartup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultFallbackTestTimeout)
	defer cancel()

	// Create two listeners for the two management servers. The test can
	// start/stop these listeners and can also get notified when the listener
	// receives a connection request.
	primaryWrappedLis := testutils.NewListenerWrapper(t, nil)
	primaryLis := testutils.NewRestartableListener(primaryWrappedLis)
	fallbackWrappedLis := testutils.NewListenerWrapper(t, nil)
	fallbackLis := testutils.NewRestartableListener(fallbackWrappedLis)

	// Start two management servers, primary and fallback, with the above
	// listeners.
	primaryManagementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: primaryLis})
	fallbackManagementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: fallbackLis})

	// Start two test service backends.
	backend1 := stubserver.StartTestService(t, nil)
	defer backend1.Stop()
	backend2 := stubserver.StartTestService(t, nil)
	defer backend2.Stop()

	// Configure xDS resource on the primary management server, with a cluster
	// resource that contains an endpoint for backend1.
	nodeID := uuid.New().String()
	const serviceName = "my-service-fallback-xds"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, backend1.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	if err := primaryManagementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Configure xDS resource on the secondary management server, with a cluster
	// resource that contains an endpoint for backend2. Only the listener
	// resource has the same name on both servers.
	fallbackRouteConfigName := "fallback-route-" + serviceName
	fallbackClusterName := "fallback-cluster-" + serviceName
	fallbackEndpointsName := "fallback-endpoints-" + serviceName
	resources = e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, fallbackRouteConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(fallbackRouteConfigName, serviceName, fallbackClusterName)},
		Clusters:  []*v3clusterpb.Cluster{e2e.DefaultCluster(fallbackClusterName, fallbackEndpointsName, e2e.SecurityLevelNone)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(fallbackEndpointsName, "localhost", []uint32{testutils.ParsePort(t, backend2.Address)})},
	}
	if err := fallbackManagementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Shut both management servers down before starting the gRPC client to
	// trigger fallback on startup.
	primaryLis.Stop()
	fallbackLis.Stop()

	// Generate bootstrap configuration with the above two servers.
	bootstrapContents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: []byte(fmt.Sprintf(`[
		{
			"server_uri": %q,
			"channel_creds": [{"type": "insecure"}]
		},
		{
			"server_uri": %q,
			"channel_creds": [{"type": "insecure"}]
		}]`, primaryManagementServer.Address, fallbackManagementServer.Address)),
		Node: []byte(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap file: %v", err)
	}

	// Create an xDS client with the above bootstrap configuration.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}

	// Get the xDS resolver to use the above xDS client.
	resolverBuilder := internal.NewXDSResolverWithPoolForTesting.(func(*xdsclient.Pool) (resolver.Builder, error))
	resolver, err := resolverBuilder(pool)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a gRPC client that uses the above xDS resolver.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("Failed to create gRPC client: %v", err)
	}
	defer cc.Close()
	cc.Connect()

	// Ensure that a connection is attempted to the primary.
	if _, err := primaryWrappedLis.NewConnCh.Receive(ctx); err != nil {
		t.Fatalf("Failure when waiting for a connection to be opened to the primary management server: %v", err)
	}

	// Ensure that a connection is attempted to the fallback.
	if _, err := fallbackWrappedLis.NewConnCh.Receive(ctx); err != nil {
		t.Fatalf("Failure when waiting for a connection to be opened to the primary management server: %v", err)
	}

	// Make an RPC with a shortish deadline and expect it to fail.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(sCtx, &testpb.Empty{}, grpc.WaitForReady(true)); err == nil || status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() = %v, want DeadlineExceeded", err)
	}

	// Start the fallback server. Ensure that an RPC can succeed, and that it
	// reaches backend2.
	fallbackLis.Restart()
	if err := waitForRPCsToReachBackend(ctx, client, backend2.Address); err != nil {
		t.Fatal(err)
	}

	// Start the primary server. It can take a while before the xDS client
	// notices this, since the ADS stream implementation uses a backoff before
	// retrying the stream.
	primaryLis.Restart()

	// Wait for the connection to the secondary to be closed and ensure that an
	// RPC can succeed, and that it reaches backend1.
	c, err := fallbackWrappedLis.NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Failure when retrieving the most recent connection to the fallback management server: %v", err)
	}
	conn := c.(*testutils.ConnWrapper)
	if _, err := conn.CloseCh.Receive(ctx); err != nil {
		t.Fatalf("Connection to fallback server not closed once primary becomes ready: %v", err)
	}
	if err := waitForRPCsToReachBackend(ctx, client, backend1.Address); err != nil {
		t.Fatal(err)
	}

	// Stop the primary servers. Since all xDS resources were received from the
	// primary (and RPCs were succeeding to the clusters returned by the
	// primary), we will not trigger fallback.
	primaryLis.Stop()
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := fallbackWrappedLis.NewConnCh.Receive(sCtx); err == nil {
		t.Fatalf("Fallback attempted when not expected to. There are no uncached resources from the primary server at this point.")
	}

	// Ensure that RPCs still succeed, and that they use the configuration
	// received from the primary.
	if err := waitForRPCsToReachBackend(ctx, client, backend1.Address); err != nil {
		t.Fatal(err)
	}
}

// Tests fallback when the primary management server fails during an update.
func (s) TestFallback_MidUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultFallbackTestTimeout)
	defer cancel()

	// Create two listeners for the two management servers. The test can
	// start/stop these listeners and can also get notified when the listener
	// receives a connection request.
	primaryWrappedLis := testutils.NewListenerWrapper(t, nil)
	primaryLis := testutils.NewRestartableListener(primaryWrappedLis)
	fallbackWrappedLis := testutils.NewListenerWrapper(t, nil)
	fallbackLis := testutils.NewRestartableListener(fallbackWrappedLis)

	// This boolean helps with triggering fallback mid update. When this boolean
	// is set and the below defined cluster resource is requested, the primary
	// management server shuts down the connection, forcing the client to
	// fallback to the secondary server.
	var closeConnOnMidUpdateClusterResource atomic.Bool
	const (
		serviceName              = "my-service-fallback-xds"
		routeConfigName          = "route-" + serviceName
		clusterName              = "cluster-" + serviceName
		endpointsName            = "endpoints-" + serviceName
		midUpdateRouteConfigName = "mid-update-route-" + serviceName
		midUpdateClusterName     = "mid-update-cluster-" + serviceName
		midUpdateEndpointsName   = "mid-update-endpoints-" + serviceName
	)

	// Start two management servers, primary and fallback, with the above
	// listeners.
	primaryManagementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener: primaryLis,
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if closeConnOnMidUpdateClusterResource.Load() == false {
				return nil
			}
			if req.GetTypeUrl() != version.V3ClusterURL {
				return nil
			}
			for _, name := range req.GetResourceNames() {
				if name == midUpdateClusterName {
					primaryLis.Stop()
					return fmt.Errorf("closing ADS stream because %q resource was requested", midUpdateClusterName)
				}
			}
			return nil
		},
		AllowResourceSubset: true,
	})
	fallbackManagementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: fallbackLis})

	// Start three test service backends.
	backend1 := stubserver.StartTestService(t, nil)
	defer backend1.Stop()
	backend2 := stubserver.StartTestService(t, nil)
	defer backend2.Stop()
	backend3 := stubserver.StartTestService(t, nil)
	defer backend3.Stop()

	// Configure xDS resource on the primary management server, with a cluster
	// resource that contains an endpoint for backend1.
	nodeID := uuid.New().String()
	primaryResources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, serviceName, clusterName)},
		Clusters:  []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, endpointsName, e2e.SecurityLevelNone)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(endpointsName, "localhost", []uint32{testutils.ParsePort(t, backend1.Address)})},
	}
	if err := primaryManagementServer.Update(ctx, primaryResources); err != nil {
		t.Fatal(err)
	}

	// Configure xDS resource on the secondary management server, with a cluster
	// resource that contains an endpoint for backend2. Only the listener
	// resource has the same name on both servers.
	const (
		fallbackRouteConfigName = "fallback-route-" + serviceName
		fallbackClusterName     = "fallback-cluster-" + serviceName
		fallbackEndpointsName   = "fallback-endpoints-" + serviceName
	)
	fallbackResources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, fallbackRouteConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(fallbackRouteConfigName, serviceName, fallbackClusterName)},
		Clusters:  []*v3clusterpb.Cluster{e2e.DefaultCluster(fallbackClusterName, fallbackEndpointsName, e2e.SecurityLevelNone)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(fallbackEndpointsName, "localhost", []uint32{testutils.ParsePort(t, backend2.Address)})},
	}
	if err := fallbackManagementServer.Update(ctx, fallbackResources); err != nil {
		t.Fatal(err)
	}

	// Generate bootstrap configuration with the above two servers.
	bootstrapContents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: []byte(fmt.Sprintf(`[
		{
			"server_uri": %q,
			"channel_creds": [{"type": "insecure"}]
		},
		{
			"server_uri": %q,
			"channel_creds": [{"type": "insecure"}]
		}]`, primaryManagementServer.Address, fallbackManagementServer.Address)),
		Node: []byte(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap file: %v", err)
	}

	// Create an xDS client with the above bootstrap configuration.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}

	// Get the xDS resolver to use the above xDS client.
	resolverBuilder := internal.NewXDSResolverWithPoolForTesting.(func(*xdsclient.Pool) (resolver.Builder, error))
	resolver, err := resolverBuilder(pool)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a gRPC client that uses the above xDS resolver.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("Failed to create gRPC client: %v", err)
	}
	defer cc.Close()
	cc.Connect()

	// Ensure that RPCs reach the cluster specified by the primary server and
	// that no connection is attempted to the fallback server.
	client := testgrpc.NewTestServiceClient(cc)
	if err := waitForRPCsToReachBackend(ctx, client, backend1.Address); err != nil {
		t.Fatal(err)
	}
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := fallbackWrappedLis.NewConnCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("Connection attempt made to fallback server when none expected: %v", err)
	}

	// Instruct the primary server to close the connection if below defined
	// cluster resource is requested.
	closeConnOnMidUpdateClusterResource.Store(true)

	// Update the listener resource on the primary server to point to a new
	// route configuration that points to a new cluster that points to a new
	// endpoints resource that contains backend3.
	primaryResources = e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, midUpdateRouteConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(midUpdateRouteConfigName, serviceName, midUpdateClusterName)},
		Clusters:  []*v3clusterpb.Cluster{e2e.DefaultCluster(midUpdateClusterName, midUpdateEndpointsName, e2e.SecurityLevelNone)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(midUpdateEndpointsName, "localhost", []uint32{testutils.ParsePort(t, backend3.Address)})},
	}
	if err := primaryManagementServer.Update(ctx, primaryResources); err != nil {
		t.Fatal(err)
	}

	// Ensure that a connection is attempted to the fallback (because both
	// conditions mentioned for fallback in A71 are satisfied: connectivity
	// failure and a watcher for an uncached resource), and that RPCs are
	// routed to the cluster returned by the fallback server.
	c, err := fallbackWrappedLis.NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Failure when waiting for a connection to be opened to the fallback management server: %v", err)
	}
	fallbackConn := c.(*testutils.ConnWrapper)
	if err := waitForRPCsToReachBackend(ctx, client, backend2.Address); err != nil {
		t.Fatal(err)
	}

	// Set the primary management server to not close the connection anymore if
	// the mid-update cluster resource is requested, and get it to start serving
	// again.
	closeConnOnMidUpdateClusterResource.Store(false)
	primaryLis.Restart()

	// A new snapshot, with the same resources, is pushed to the management
	// server to get it to respond for already requested resource names.
	if err := primaryManagementServer.Update(ctx, primaryResources); err != nil {
		t.Fatal(err)
	}

	// Ensure that RPCs reach the backend pointed to by the new cluster.
	if err := waitForRPCsToReachBackend(ctx, client, backend3.Address); err != nil {
		t.Fatal(err)
	}

	// Wait for the connection to the secondary to be closed since we have
	// reverted back to the primary.
	if _, err := fallbackConn.CloseCh.Receive(ctx); err != nil {
		t.Fatalf("Connection to fallback server not closed once primary becomes ready: %v", err)
	}
}

// Tests fallback when the primary management server fails during startup.
func (s) TestFallback_MidStartup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultFallbackTestTimeout)
	defer cancel()

	// Create two listeners for the two management servers. The test can
	// start/stop these listeners and can also get notified when the listener
	// receives a connection request.
	primaryWrappedLis := testutils.NewListenerWrapper(t, nil)
	primaryLis := testutils.NewRestartableListener(primaryWrappedLis)
	fallbackWrappedLis := testutils.NewListenerWrapper(t, nil)
	fallbackLis := testutils.NewRestartableListener(fallbackWrappedLis)

	// This boolean helps with triggering fallback during startup. When this
	// boolean is set and a cluster resource is requested, the primary
	// management server shuts down the connection, forcing the client to
	// fallback to the secondary server.
	var closeConnOnClusterResource atomic.Bool
	closeConnOnClusterResource.Store(true)

	// Start two management servers, primary and fallback, with the above
	// listeners.
	primaryManagementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener: primaryLis,
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if closeConnOnClusterResource.Load() == false {
				return nil
			}
			if req.GetTypeUrl() != version.V3ClusterURL {
				return nil
			}
			primaryLis.Stop()
			return fmt.Errorf("closing ADS stream because cluster resource was requested")
		},
		AllowResourceSubset: true,
	})
	fallbackManagementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: fallbackLis})

	// Start two test service backends.
	backend1 := stubserver.StartTestService(t, nil)
	defer backend1.Stop()
	backend2 := stubserver.StartTestService(t, nil)
	defer backend2.Stop()

	// Configure xDS resource on the primary management server, with a cluster
	// resource that contains an endpoint for backend1.
	nodeID := uuid.New().String()
	const serviceName = "my-service-fallback-xds"
	primaryResources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, backend1.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	if err := primaryManagementServer.Update(ctx, primaryResources); err != nil {
		t.Fatal(err)
	}

	// Configure xDS resource on the secondary management server, with a cluster
	// resource that contains an endpoint for backend2. Only the listener
	// resource has the same name on both servers.
	fallbackRouteConfigName := "fallback-route-" + serviceName
	fallbackClusterName := "fallback-cluster-" + serviceName
	fallbackEndpointsName := "fallback-endpoints-" + serviceName
	fallbackResources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, fallbackRouteConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(fallbackRouteConfigName, serviceName, fallbackClusterName)},
		Clusters:  []*v3clusterpb.Cluster{e2e.DefaultCluster(fallbackClusterName, fallbackEndpointsName, e2e.SecurityLevelNone)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(fallbackEndpointsName, "localhost", []uint32{testutils.ParsePort(t, backend2.Address)})},
	}
	if err := fallbackManagementServer.Update(ctx, fallbackResources); err != nil {
		t.Fatal(err)
	}

	// Generate bootstrap configuration with the above two servers.
	bootstrapContents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: []byte(fmt.Sprintf(`[
		{
			"server_uri": %q,
			"channel_creds": [{"type": "insecure"}]
		},
		{
			"server_uri": %q,
			"channel_creds": [{"type": "insecure"}]
		}]`, primaryManagementServer.Address, fallbackManagementServer.Address)),
		Node: []byte(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap file: %v", err)
	}

	// Create an xDS client with the above bootstrap configuration.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}

	// Get the xDS resolver to use the above xDS client.
	resolverBuilder := internal.NewXDSResolverWithPoolForTesting.(func(*xdsclient.Pool) (resolver.Builder, error))
	resolver, err := resolverBuilder(pool)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a gRPC client that uses the above xDS resolver.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("Failed to create gRPC client: %v", err)
	}
	defer cc.Close()
	cc.Connect()

	// Ensure that a connection is attempted to the primary.
	if _, err := primaryWrappedLis.NewConnCh.Receive(ctx); err != nil {
		t.Fatalf("Failure when waiting for a connection to be opened to the primary management server: %v", err)
	}

	// Ensure that a connection is attempted to the fallback.
	c, err := fallbackWrappedLis.NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Failure when waiting for a connection to be opened to the secondary management server: %v", err)
	}
	fallbackConn := c.(*testutils.ConnWrapper)

	// Ensure that RPCs are routed to the cluster returned by the fallback
	// management server.
	client := testgrpc.NewTestServiceClient(cc)
	if err := waitForRPCsToReachBackend(ctx, client, backend2.Address); err != nil {
		t.Fatal(err)
	}

	// Get the primary management server to no longer close the connection when
	// the cluster resource is requested.
	closeConnOnClusterResource.Store(false)
	primaryLis.Restart()

	// A new snapshot, with the same resources, is pushed to the management
	// server to get it to respond for already requested resource names.
	if err := primaryManagementServer.Update(ctx, primaryResources); err != nil {
		t.Fatal(err)
	}

	// Ensure that RPCs are routed to the cluster returned by the primary
	// management server.
	if err := waitForRPCsToReachBackend(ctx, client, backend1.Address); err != nil {
		t.Fatal(err)
	}

	// Wait for the connection to the secondary to be closed since we have
	// reverted back to the primary.
	if _, err := fallbackConn.CloseCh.Receive(ctx); err != nil {
		t.Fatalf("Connection to fallback server not closed once primary becomes ready: %v", err)
	}
}

// Tests that RPCs succeed at startup when the primary management server is
// down, but the secondary is available.
func (s) TestFallback_OnStartup_RPCSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultFallbackTestTimeout)
	defer cancel()

	// Create two listeners for the two management servers. The test can
	// start/stop these listeners.
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	primaryLis := testutils.NewRestartableListener(l)
	l, err = testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	fallbackLis := testutils.NewRestartableListener(l)

	// Start two management servers, primary and fallback, with the above
	// listeners.
	primaryManagementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: primaryLis})
	fallbackManagementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: fallbackLis})

	// Start two test service backends.
	backend1 := stubserver.StartTestService(t, nil)
	defer backend1.Stop()
	backend2 := stubserver.StartTestService(t, nil)
	defer backend2.Stop()

	// Configure xDS resource on the primary management server, with a cluster
	// resource that contains an endpoint for backend1.
	nodeID := uuid.New().String()
	const serviceName = "my-service-fallback-xds"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, backend1.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	if err := primaryManagementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Configure xDS resource on the secondary management server, with a cluster
	// resource that contains an endpoint for backend2. Only the listener
	// resource has the same name on both servers.
	fallbackRouteConfigName := "fallback-route-" + serviceName
	fallbackClusterName := "fallback-cluster-" + serviceName
	fallbackEndpointsName := "fallback-endpoints-" + serviceName
	resources = e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, fallbackRouteConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(fallbackRouteConfigName, serviceName, fallbackClusterName)},
		Clusters:  []*v3clusterpb.Cluster{e2e.DefaultCluster(fallbackClusterName, fallbackEndpointsName, e2e.SecurityLevelNone)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(fallbackEndpointsName, "localhost", []uint32{testutils.ParsePort(t, backend2.Address)})},
	}
	if err := fallbackManagementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Shutdown the primary management server before starting the gRPC client to
	// trigger fallback on startup.
	primaryLis.Stop()

	// Generate bootstrap configuration with the above two servers.
	bootstrapContents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: []byte(fmt.Sprintf(`[
		{
			"server_uri": %q,
			"channel_creds": [{"type": "insecure"}]
		},
		{
			"server_uri": %q,
			"channel_creds": [{"type": "insecure"}]
		}]`, primaryManagementServer.Address, fallbackManagementServer.Address)),
		Node: []byte(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap file: %v", err)
	}

	// Create an xDS client with the above bootstrap configuration.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}

	// Get the xDS resolver to use the above xDS client.
	resolverBuilder := internal.NewXDSResolverWithPoolForTesting.(func(*xdsclient.Pool) (resolver.Builder, error))
	resolver, err := resolverBuilder(pool)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a gRPC client that uses the above xDS resolver.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("Failed to create gRPC client: %v", err)
	}
	defer cc.Close()

	// Make an RPC (without the `wait_for_ready` call option) and expect it to
	// succeed since the fallback management server is up and running.
	client := testgrpc.NewTestServiceClient(cc)
	var peer peer.Peer
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if got, want := peer.Addr.String(), backend2.Address; got != want {
		t.Fatalf("Unexpected peer address: got %q, want %q", got, want)
	}

	// Start the primary server. It can take a while before the xDS client
	// notices this, since the ADS stream implementation uses a backoff before
	// retrying the stream.
	primaryLis.Restart()
	if err := waitForRPCsToReachBackend(ctx, client, backend1.Address); err != nil {
		t.Fatal(err)
	}
}

// TestXDSFallback_ThreeServerPromotion verifies that when the primary
// management server is unavailable, the system attempts to connect to the
// first fallback server, and if that is also down, to the second fallback
// server. It also ensures that the system switches back to the first fallback
// server once it becomes available again, and eventually returns to the
// primary server when it comes back online, closing connections to the
// fallback servers accordingly.
func (s) TestXDSFallback_ThreeServerPromotion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultFallbackTestTimeout)
	defer cancel()

	// Create three listener wrappers for three management servers.
	primaryWrappedLis := testutils.NewListenerWrapper(t, nil)
	primaryLis := testutils.NewRestartableListener(primaryWrappedLis)

	secondaryWrappedLis := testutils.NewListenerWrapper(t, nil)
	secondaryLis := testutils.NewRestartableListener(secondaryWrappedLis)

	tertiaryWrappedLis := testutils.NewListenerWrapper(t, nil)
	tertiaryLis := testutils.NewRestartableListener(tertiaryWrappedLis)

	// Start the three management servers.
	primaryManagementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: primaryLis})
	secondaryManagementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: secondaryLis})
	tertiaryManagementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: tertiaryLis})

	// Start three test service backends.
	backend1 := stubserver.StartTestService(t, nil)
	defer backend1.Stop()
	backend2 := stubserver.StartTestService(t, nil)
	defer backend2.Stop()
	backend3 := stubserver.StartTestService(t, nil)
	defer backend3.Stop()

	nodeID := uuid.New().String()
	const serviceName = "my-service-fallback-xds"

	// Configure partial resources on the primary and secondary
	// management servers.
	primaryManagementServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, "route-p")},
	})
	secondaryManagementServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, "route-s1")},
	})

	// Configure full resources on tertiary management server.
	tertiaryManagementServer.Update(ctx, e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, backend3.Address),
		SecLevel:   e2e.SecurityLevelNone,
	}))

	// Create bootstrap configuration for all three management servers.
	bootstrapContents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: []byte(fmt.Sprintf(`[
            {
                "server_uri": %q,
                "channel_creds": [{"type": "insecure"}]
            },
            {
                "server_uri": %q,
                "channel_creds": [{"type": "insecure"}]
            },
             {
                "server_uri": %q,
                "channel_creds": [{"type": "insecure"}]
            }
        ]`, primaryManagementServer.Address, secondaryManagementServer.Address, tertiaryManagementServer.Address)),
		Node: []byte(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap file: %v", err)
	}

	// Create an xDS client with the above bootstrap configuration.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %v", err)
	}
	pool := xdsclient.NewPool(config)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}

	// Get the xDS resolver to use the above xDS client.
	resolverBuilder := internal.NewXDSResolverWithPoolForTesting.(func(*xdsclient.Pool) (resolver.Builder, error))
	resolver, err := resolverBuilder(pool)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a gRPC client that uses the above xDS resolver.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("Failed to create gRPC client: %v", err)
	}
	defer cc.Close()
	cc.Connect()
	client := testgrpc.NewTestServiceClient(cc)

	// Verify that connection attempts were made to primaryWrappedLis and
	// secondaryWrappedLis, before using tertiaryWrappedLis to make
	// successful RPCs to backend3.
	if _, err := primaryWrappedLis.NewConnCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for connection to primary: %v", err)
	}

	// Stop primary, client should connect to secondary.
	primaryLis.Stop()
	if _, err := secondaryWrappedLis.NewConnCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for connection to secondary after primary stopped: %v", err)
	}

	// Stop secondary, client should connect to tertiary.
	secondaryLis.Stop()
	tertiaryConn, err := tertiaryWrappedLis.NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout when waiting for connection to tertiary after secondary stopped: %v", err)
	}

	// Tertiary has all resources, RPCs should succeed to backend3.
	if err := waitForRPCsToReachBackend(ctx, client, backend3.Address); err != nil {
		t.Fatal(err)
	}

	// Secondary1 becomes available, RPCs go to backend2.
	secondaryLis.Restart()
	secondaryManagementServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, "route-s1")},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig("route-s1", serviceName, "cluster-s1")},
		Clusters:  []*v3clusterpb.Cluster{e2e.DefaultCluster("cluster-s1", "endpoints-s1", e2e.SecurityLevelNone)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{
			e2e.DefaultEndpoint("endpoints-s1", "localhost", []uint32{testutils.ParsePort(t, backend2.Address)}),
		},
	})

	secondaryConn, err := secondaryWrappedLis.NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout when waiting for new connection to secondary: %v", err)
	}
	if _, err := tertiaryConn.(*testutils.ConnWrapper).CloseCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for connection to the tertiary to be closed after promotion to secondary: %v", err)
	}
	if err := waitForRPCsToReachBackend(ctx, client, backend2.Address); err != nil {
		t.Fatal(err)
	}

	// Primary becomes available, RPCs go to backend1.
	primaryLis.Restart()
	primaryManagementServer.Update(ctx, e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, backend1.Address),
		SecLevel:   e2e.SecurityLevelNone,
	}))

	if _, err := primaryWrappedLis.NewConnCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for new connection to primary: %v", err)
	}
	if _, err := secondaryConn.(*testutils.ConnWrapper).CloseCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for connection to the secondary to be closed after promotion to primary: %v", err)
	}
	if err := waitForRPCsToReachBackend(ctx, client, backend1.Address); err != nil {
		t.Fatal(err)
	}
}
