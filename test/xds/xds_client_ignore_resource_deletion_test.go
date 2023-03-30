/*
 *
 * Copyright 2023 gRPC authors.
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

package xds_test

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/bootstrap"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds"

	clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	testServicepb "google.golang.org/grpc/test/grpc_testing"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

const (
	serviceName = "my-service-xds"
	rdsName     = "route-" + serviceName
	cdsName1    = "cluster1-" + serviceName
	cdsName2    = "cluster2-" + serviceName
	edsName1    = "eds1-" + serviceName
	edsName2    = "eds2-" + serviceName
)

var (
	resolverBuilder                 = internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))
	nodeID                          = uuid.New().String()
	defaultRouteConfigWithTwoRoutes = &routepb.RouteConfiguration{
		Name: rdsName,
		VirtualHosts: []*routepb.VirtualHost{{
			Domains: []string{serviceName},
			Routes: []*routepb.Route{
				{
					Match: &routepb.RouteMatch{PathSpecifier: &routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"}},
					Action: &routepb.Route_Route{Route: &routepb.RouteAction{
						ClusterSpecifier: &routepb.RouteAction_Cluster{Cluster: cdsName1},
					}},
				},
				{
					Match: &routepb.RouteMatch{PathSpecifier: &routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/UnaryCall"}},
					Action: &routepb.Route_Route{Route: &routepb.RouteAction{
						ClusterSpecifier: &routepb.RouteAction_Cluster{Cluster: cdsName2},
					}},
				},
			},
		}},
	}
)

// This test runs subtest each for a Listener resource and a Cluster resource deletion
// in the response from the server for the following cases:
//   - testResourceDeletionIgnored: When ignore_resource_deletion is set, the
//     xDSClient should not delete the resource.
//   - testResourceDeletionNotIgnored: When ignore_resource_deletion is unset,
//     the xDSClient should delete the resource.
//
// Resource deletion is only applicable to Listener and Cluster resources.
func (s) TestIgnoreResourceDeletionOnClient(t *testing.T) {
	port1, cleanup := startTestService(t, nil)
	t.Cleanup(cleanup)

	port2, cleanup := startTestService(t, nil)
	t.Cleanup(cleanup)

	initialResourceOnServer := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*listenerpb.Listener{e2e.DefaultClientListener(serviceName, rdsName)},
		Routes:    []*routepb.RouteConfiguration{defaultRouteConfigWithTwoRoutes},
		Clusters: []*clusterpb.Cluster{
			defaultClientCluster(cdsName1, edsName1),
			defaultClientCluster(cdsName2, edsName2),
		},
		Endpoints: []*endpointpb.ClusterLoadAssignment{
			e2e.DefaultEndpoint(edsName1, "localhost", []uint32{port1}),
			e2e.DefaultEndpoint(edsName2, "localhost", []uint32{port2}),
		},
		SkipValidation: true,
	}

	tests := []struct {
		name           string
		updateResource func(r *e2e.UpdateOptions)
	}{
		{
			name: "listener",
			updateResource: func(r *e2e.UpdateOptions) {
				r.Listeners = nil
			},
		},
		{
			name: "cluster",
			updateResource: func(r *e2e.UpdateOptions) {
				r.Clusters = nil
			},
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s resource deletion ignored", test.name), func(t *testing.T) {
			testResourceDeletionIgnored(t, initialResourceOnServer, test.updateResource)
		})
		t.Run(fmt.Sprintf("%s resource deletion not ignored", test.name), func(t *testing.T) {
			testResourceDeletionNotIgnored(t, initialResourceOnServer, test.updateResource)
		})
	}
}

// This subtest tests the scenario where the bootstrap config has "ignore_resource_deletion"
// set in "server_features" field. This subtest verifies that the resource was
// not deleted by the xDSClient when a resource is missing the xDS response and
// RPCs continue to succeed.
func testResourceDeletionIgnored(t *testing.T, resources e2e.UpdateOptions, u func(r *e2e.UpdateOptions)) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	t.Cleanup(cancel)
	ms := startManagementServer(t)
	bootstrapContent := generateBootstrapContents(t, ms.Address, true)

	// Update the management server with initial resources setup.
	if err := ms.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cResolver := generateCustomResolver(t, bootstrapContent)
	cc, err := grpc.Dial(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(cResolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	verifyRPCtoAllEndpoints(t, cc)

	// Mutate resource and update on the server.
	u(&resources)
	if err := ms.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Make RPCs for every 50ms for the next 500ms.
	timer := time.NewTimer(500 * time.Millisecond)
	ticker := time.NewTicker(50 * time.Millisecond)
	t.Cleanup(ticker.Stop)
	for {
		verifyRPCtoAllEndpoints(t, cc)
		select {
		case <-timer.C:
			return
		case <-ticker.C:
		}
	}
}

// This subtest tests the scenario where the bootstrap config has "ignore_resource_deletion"
// not set in "server_features" field. This subtest verifies that the resource was
// deleted by the xDSClient when a resource is missing the xDS response and subsequent
// RPCs fail.
func testResourceDeletionNotIgnored(t *testing.T, resources e2e.UpdateOptions, u func(r *e2e.UpdateOptions)) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	t.Cleanup(cancel)
	ms := startManagementServer(t)
	bootstrapContent := generateBootstrapContents(t, ms.Address, true)

	// Update the management server with initial resources setup.
	if err := ms.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cResolver := generateCustomResolver(t, bootstrapContent)
	cc, err := grpc.Dial(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(cResolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	verifyRPCtoAllEndpoints(t, cc)

	// Mutate resource and update on the server.
	u(&resources)
	if err := ms.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Spin up go routines to verify RPCs fail after the update.
	client := testServicepb.NewTestServiceClient(cc)
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		for ctx.Err() == nil {
			if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
				wg.Done()
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	go func() {
		for ctx.Err() == nil {
			if _, err := client.UnaryCall(ctx, &testpb.SimpleRequest{}); err != nil {
				wg.Done()
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	wg.Wait()
	if ctx.Err() != nil {
		t.Fatal("Context expired before RPCs failed.")
	}
}

// This helper creates a management server for the test.
func startManagementServer(t *testing.T) *e2e.ManagementServer {
	t.Helper()
	ms, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		t.Fatalf("Failed to start management server: %v", err)
	}
	t.Cleanup(ms.Stop)
	return ms
}

// This helper generates a custom bootstrap config for the test.
func generateBootstrapContents(t *testing.T, serverURI string, ignoreResourceDeletion bool) []byte {
	t.Helper()
	bootstrapContents, err := bootstrap.Contents(bootstrap.Options{
		NodeID:                             nodeID,
		ServerURI:                          serverURI,
		ServerListenerResourceNameTemplate: e2e.ServerListenerResourceNameTemplate,
		IgnoreResourceDeletion:             ignoreResourceDeletion,
	})
	if err != nil {
		t.Fatal(err)
	}
	return bootstrapContents
}

// This helper generates a custom bootstrap config for the test.
func generateCustomResolver(t *testing.T, bootstrapContents []byte) resolver.Builder {
	cResolver, err := resolverBuilder(bootstrapContents)
	if err != nil {
		t.Fatalf("Creating xDS resolver for testing: %v", err)
	}
	return cResolver
}

// This helper creates an xDS-enabled gRPC server using the listener and the
// bootstrap config passed. This helper then registers the server to testService
// and returns a func to accept requests.
func setupGRPCServerWithModeChangeChannel(t *testing.T, bootstrapContents []byte, lis net.Listener) (chan connectivity.ServingMode, func()) {
	t.Helper()
	updateCh := make(chan connectivity.ServingMode, 1)

	// Create a server option to get notified about serving mode changes.
	modeChangeOpt := xds.ServingModeCallback(func(addr net.Addr, args xds.ServingModeChangeArgs) {
		t.Logf("serving mode for listener %q changed to %q, err: %v", addr.String(), args.Mode, args.Err)
		updateCh <- args.Mode
	})

	server := xds.NewGRPCServer(grpc.Creds(insecure.NewCredentials()), modeChangeOpt, xds.BootstrapContentsForTesting(bootstrapContents))
	t.Cleanup(server.Stop)
	testpb.RegisterTestServiceServer(server, &testService{})

	return updateCh, func() {
		if err := server.Serve(lis); err != nil {
			t.Errorf("Serve() failed: %v", err)
		}
	}
}

// This helper creates a listener resource with using a custom listener created.
// The test uses this listener to serve the gRPC server that is set up.
func resourceWithListenerForGRPCServer(t *testing.T) (e2e.UpdateOptions, net.Listener) {
	t.Helper()
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	t.Cleanup(func() { lis.Close() })

	// Setup the management server to respond with the listener resources.
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("failed to retrieve host and port of server: %v", err)
	}
	listener := e2e.DefaultServerListener(host, port, e2e.SecurityLevelNone)
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*listenerpb.Listener{listener},
	}
	return resources, lis
}

// This test creates a xds-enabled gRPC server with a listener resource and this
// server features "ignore_resource_deletion". The test then verifies successful
// RPCs to server. The test then removes the listener resource from the server
// and verifies that server continues to server requests.
func (s) TestListenerResourceDeletionOnServerIgnored(t *testing.T) {
	ms := startManagementServer(t)
	bootstrapContents := generateBootstrapContents(t, ms.Address, true)
	resources, lis := resourceWithListenerForGRPCServer(t)
	updateCh, serve := setupGRPCServerWithModeChangeChannel(t, bootstrapContents, lis)

	go serve()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := ms.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for the server to update to "serving" mode.
	select {
	case <-ctx.Done():
		t.Fatal("Test timed out waiting for a mode change update.")
	case mode := <-updateCh:
		if mode != connectivity.ServingModeServing {
			t.Fatalf("listener received new mode %v, want %v", mode, connectivity.ServingModeServing)
		}
	}

	cResolver := generateCustomResolver(t, bootstrapContents)

	// Create a ClientConn and make a successful RPCs.
	cc, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(cResolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	verifyRPCtoAllEndpoints(t, cc)

	// Update without a listener resource.
	if err := ms.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*listenerpb.Listener{},
	}); err != nil {
		t.Fatal(err)
	}

	// Make RPCs every 100 ms for 1s and verify that the serving mode does
	// not change on server.
	timer := time.NewTimer(500 * time.Millisecond)
	ticker := time.NewTicker(50 * time.Millisecond)
	t.Cleanup(ticker.Stop)
	for {
		verifyRPCtoAllEndpoints(t, cc)
		select {
		case <-timer.C:
			return
		case mode := <-updateCh:
			t.Fatalf("Listener received new mode: %v", mode)
		case <-ticker.C:
		}
	}
}

// This test creates a xds-enabled gRPC server with a listener resource and this
// server does not feature "ignore_resource_deletion". The test verifies RPCs are
// working as expected. The test then removes the listener resource from the server
// and verifies that server should enter "non_serving" mode as expected.
func (s) TestListenerResourceDeletionOnServerNotIgnored(t *testing.T) {
	ms := startManagementServer(t)
	bootstrapContents := generateBootstrapContents(t, ms.Address, false)
	resources, lis := resourceWithListenerForGRPCServer(t)
	updateCh, serve := setupGRPCServerWithModeChangeChannel(t, bootstrapContents, lis)

	go serve()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := ms.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for the listener to move to "serving" mode.
	select {
	case <-ctx.Done():
		t.Fatal("Test timed out waiting for a mode change update.")
	case mode := <-updateCh:
		if mode != connectivity.ServingModeServing {
			t.Fatalf("Listener received new mode %v, want %v", mode, connectivity.ServingModeServing)
		}
	}

	// Create a ClientConn and make a successful RPCs.
	cResolver := generateCustomResolver(t, bootstrapContents)
	cc, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(cResolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()
	verifyRPCtoAllEndpoints(t, cc)

	if err := ms.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*listenerpb.Listener{}, // empty listener resource
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ctx.Done():
		t.Fatalf("timed out waiting for a mode change update: %v", err)
	case mode := <-updateCh:
		if mode != connectivity.ServingModeNotServing {
			t.Fatalf("listener received new mode %v, want %v", mode, connectivity.ServingModeNotServing)
		}
	}
}

// This helper makes both UnaryCall and EmptyCall RPCs using the ClientConn that
// is passed to this function. This helper panics for any failed RPCs.
func verifyRPCtoAllEndpoints(t *testing.T, cc grpc.ClientConnInterface) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client := testServicepb.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
	if _, err := client.UnaryCall(ctx, &testpb.SimpleRequest{}); err != nil {
		t.Fatalf("rpc UnaryCall() failed: %v", err)
	}
}

func defaultClientCluster(cdsName string, edsName string) *clusterpb.Cluster {
	return &clusterpb.Cluster{
		Name:                 cdsName,
		ClusterDiscoveryType: &clusterpb.Cluster_Type{Type: clusterpb.Cluster_EDS},
		EdsClusterConfig: &clusterpb.Cluster_EdsClusterConfig{
			EdsConfig: &corepb.ConfigSource{
				ConfigSourceSpecifier: &corepb.ConfigSource_Ads{
					Ads: &corepb.AggregatedConfigSource{},
				},
			},
			ServiceName: edsName,
		},
		LbPolicy: clusterpb.Cluster_ROUND_ROBIN,
	}
}
