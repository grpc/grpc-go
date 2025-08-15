/*
 *
 * Copyright 2025 gRPC authors.
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
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"
	"google.golang.org/grpc/xds"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

// Tests the case where an LDS points to an RDS which returns resource not
// found. Before getting the resource not found, the xDS Server has not received
// all configuration needed, so it should Accept and Close any new connections.
// After it has received the resource not found error (due to short watch
// expiry), the server should move to serving, successfully Accept Connections,
// and fail at the L7 level with resource not found specified.
func (s) TestServer_RouteConfiguration_ResourceNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	routeConfigNamesCh := make(chan []string, 1)
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.TypeUrl == version.V3RouteConfigURL {
				select {
				case routeConfigNamesCh <- req.GetResourceNames():
				case <-ctx.Done():
				}
			}
			return nil
		},
		AllowResourceSubset: true,
	})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)

	// Setup the management server to respond with a listener resource that
	// specifies a route name to watch, and no RDS resource corresponding to
	// this route name.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}
	const routeConfigResourceName = "routeName"
	listener := e2e.DefaultServerListenerWithRouteConfigName(host, port, e2e.SecurityLevelNone, routeConfigResourceName)
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listener},
		SkipValidation: true,
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	modeChangeHandler := newServingModeChangeHandler(t)
	modeChangeOpt := xds.ServingModeCallback(modeChangeHandler.modeChangeCallback)

	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	// Create a specific xDS client instance within that pool for the server,
	// configuring it with a short WatchExpiryTimeout.
	pool := xdsclient.NewPool(config)
	_, serverXDSClientClose, err := pool.NewClientForTesting(xdsclient.OptionsForTesting{
		Name:               xdsclient.NameForServer,
		WatchExpiryTimeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Failed to create xDS client for server: %v", err)
	}
	defer serverXDSClientClose()
	// Start an xDS-enabled gRPC server using the above client from the pool.
	createStubServer(t, lis, modeChangeOpt, xds.ClientPoolForTesting(pool))

	// Wait for the route configuration resource to be requested from the
	// management server.
	select {
	case gotNames := <-routeConfigNamesCh:
		if !cmp.Equal(gotNames, []string{routeConfigResourceName}) {
			t.Fatalf("Requested route config resource names: %v, want %v", gotNames, []string{routeConfigResourceName})
		}
	case <-ctx.Done():
		t.Fatal("Timeout waiting for route config resource to be requested")
	}

	// Do NOT send the RDS resource. The xDS client's watch expiry timer will
	// fire. After the RDS resource is deemed "not found" (due to the short
	// watch expiry), the server will transition to SERVING mode.

	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()
	// Before the watch expiry, the server is NOT_SERVING, RPCs should fail with UNAVAILABLE.
	waitForFailedRPCWithStatus(ctx, t, cc, codes.Unavailable, "", "")

	// Wait for the xDS-enabled gRPC server to go SERVING. This should happen
	// after the RDS watch expiry timer fires.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for the xDS-enabled gRPC server to go SERVING")
	case gotMode := <-modeChangeHandler.modeCh:
		if gotMode != connectivity.ServingModeServing {
			t.Fatalf("Mode changed to %v, want %v", gotMode, connectivity.ServingModeServing)
		}
	}
	// After watch expiry, the server should be SERVING, but RPCs should fail
	// at the L7 level with resource not found.
	waitForFailedRPCWithStatus(ctx, t, cc, codes.Unavailable, "error from xDS configuration for matched route configuration", nodeID)
}

// Tests the scenario where the control plane sends the same resource update. It
// verifies that the mode change callback is not invoked and client connections
// to the server are not recycled.
func (s) TestServer_RedundantUpdateSuppression(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)

	// Setup the management server to respond with the listener resources.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}
	listener := e2e.DefaultServerListener(host, port, e2e.SecurityLevelNone, "routeName")
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener},
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Start an xDS-enabled gRPC server with the above bootstrap configuration.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	modeChangeHandler := newServingModeChangeHandler(t)
	modeChangeOpt := xds.ServingModeCallback(modeChangeHandler.modeChangeCallback)
	createStubServer(t, lis, modeChangeOpt, xds.ClientPoolForTesting(pool))

	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for a mode change update: %v", err)
	case mode := <-modeChangeHandler.modeCh:
		if mode != connectivity.ServingModeServing {
			t.Fatalf("Listener received new mode %v, want %v", mode, connectivity.ServingModeServing)
		}
	}

	// Create a ClientConn and make a successful RPCs.
	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", lis.Addr(), err)
	}
	defer cc.Close()
	waitForSuccessfulRPC(ctx, t, cc)

	// Start a goroutine to make sure that we do not see any connectivity state
	// changes on the client connection. If redundant updates are not
	// suppressed, server will recycle client connections.
	errCh := make(chan error, 1)
	go func() {
		prev := connectivity.Ready // We know we are READY since we just did an RPC.
		for {
			curr := cc.GetState()
			if !(curr == connectivity.Ready || curr == connectivity.Idle) {
				errCh <- fmt.Errorf("unexpected connectivity state change {%s --> %s} on the client connection", prev, curr)
				return
			}
			if !cc.WaitForStateChange(ctx, curr) {
				// Break out of the for loop when the context has been cancelled.
				break
			}
			prev = curr
		}
		errCh <- nil
	}()

	// Update the management server with the same listener resource. This will
	// update the resource version though, and should result in a the management
	// server sending the same resource to the xDS-enabled gRPC server.
	if err := managementServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener},
	}); err != nil {
		t.Fatal(err)
	}

	// Since redundant resource updates are suppressed, we should not see the
	// mode change callback being invoked.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	select {
	case <-sCtx.Done():
	case mode := <-modeChangeHandler.modeCh:
		t.Fatalf("Unexpected mode change callback with new mode %v", mode)
	}

	// Make sure RPCs continue to succeed.
	waitForSuccessfulRPC(ctx, t, cc)

	// Cancel the context to ensure that the WaitForStateChange call exits early
	// and returns false.
	cancel()
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

// Tests the case where the route configuration contains an unsupported route
// action.  Verifies that RPCs fail with UNAVAILABLE.
func (s) TestServer_FailWithRouteActionRoute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server.
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)

	// Configure the managegement server with a listener and route configuration
	// resource for the above xDS enabled gRPC server.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to listen to local port: %v", err)
	}
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}
	const routeConfigName = "routeName"
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultServerListenerWithRouteConfigName(host, port, e2e.SecurityLevelNone, "routeName")},
		Routes:    []*v3routepb.RouteConfiguration{e2e.RouteConfigNonForwardingAction(routeConfigName)},
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Start an xDS-enabled gRPC server with the above bootstrap configuration.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	modeChangeOpt := xds.ServingModeCallback(func(addr net.Addr, args xds.ServingModeChangeArgs) {
		t.Logf("Serving mode for listener %q changed to %q, err: %v", addr.String(), args.Mode, args.Err)
	})
	createStubServer(t, lis, modeChangeOpt, xds.ClientPoolForTesting(pool))

	// Create a gRPC channel and verify that RPCs succeed.
	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", lis.Addr(), err)
	}
	defer cc.Close()
	waitForSuccessfulRPC(ctx, t, cc)

	// Update the route config resource to contain an unsupported action.
	//
	// "NonForwardingAction is expected for all Routes used on server-side; a
	// route with an inappropriate action causes RPCs matching that route to
	// fail with UNAVAILABLE." - A36
	resources.Routes = []*v3routepb.RouteConfiguration{e2e.RouteConfigFilterAction("routeName")}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	waitForFailedRPCWithStatus(ctx, t, cc, codes.Unavailable, "the incoming RPC matched to a route that was not of action type non forwarding", nodeID)
}

// Tests the case where the listener resource is removed from the management
// server. This should cause the xDS server to transition to NOT_SERVING mode,
// and the error message should contain the xDS node ID.
func (s) TestServer_ListenerResourceRemoved(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server.
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)

	// Configure the managegement server with a listener and route configuration
	// resource for the above xDS enabled gRPC server.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to listen to local port: %v", err)
	}
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}
	const routeConfigName = "routeName"
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultServerListenerWithRouteConfigName(host, port, e2e.SecurityLevelNone, "routeName")},
		Routes:         []*v3routepb.RouteConfiguration{e2e.RouteConfigNonForwardingAction(routeConfigName)},
		SkipValidation: true,
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Start an xDS-enabled gRPC server with the above bootstrap configuration.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	modeChangeHandler := newServingModeChangeHandler(t)
	modeChangeOpt := xds.ServingModeCallback(modeChangeHandler.modeChangeCallback)
	createStubServer(t, lis, modeChangeOpt, xds.ClientPoolForTesting(pool))

	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for the xDS-enabled gRPC server to go SERVING")
	case gotMode := <-modeChangeHandler.modeCh:
		if gotMode != connectivity.ServingModeServing {
			t.Fatalf("Mode changed to %v, want %v", gotMode, connectivity.ServingModeServing)
		}
	}

	// Create a gRPC channel and verify that RPCs succeed.
	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", lis.Addr(), err)
	}
	defer cc.Close()
	waitForSuccessfulRPC(ctx, t, cc)

	// Remove the listener resource from the management server. This should
	// cause the server to go NOT_SERVING, and the error message should contain
	// the xDS node ID.
	resources.Listeners = nil
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for server to go NOT_SERVING")
	case gotMode := <-modeChangeHandler.modeCh:
		if gotMode != connectivity.ServingModeNotServing {
			t.Fatalf("Mode changed to %v, want %v", gotMode, connectivity.ServingModeNotServing)
		}
		gotErr := <-modeChangeHandler.errCh
		if gotErr == nil || !strings.Contains(gotErr.Error(), nodeID) {
			t.Fatalf("Unexpected error: %v, want xDS Node id: %s", gotErr, nodeID)
		}
	}
}

// Tests the case where the listener resource points to a route configuration
// name that is NACKed. This should trigger the server to move to SERVING,
// successfully accept connections, and fail at RPCs with an expected error
// message.
func (s) TestServer_RouteConfiguration_ResourceNACK(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server.
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)

	// Configure the managegement server with a listener and route configuration
	// resource (that will be NACKed) for the above xDS enabled gRPC server.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to listen to local port: %v", err)
	}
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}
	const routeConfigName = "routeName"
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultServerListenerWithRouteConfigName(host, port, e2e.SecurityLevelNone, "routeName")},
		Routes:         []*v3routepb.RouteConfiguration{e2e.RouteConfigNoRouteMatch(routeConfigName)},
		SkipValidation: true,
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Start an xDS-enabled gRPC server with the above bootstrap configuration.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	modeChangeHandler := newServingModeChangeHandler(t)
	modeChangeOpt := xds.ServingModeCallback(modeChangeHandler.modeChangeCallback)
	createStubServer(t, lis, modeChangeOpt, xds.ClientPoolForTesting(pool))

	// Create a gRPC channel and verify that RPCs succeed.
	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", lis.Addr(), err)
	}
	defer cc.Close()

	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for the server to start serving RPCs")
	case gotMode := <-modeChangeHandler.modeCh:
		if gotMode != connectivity.ServingModeServing {
			t.Fatalf("Mode changed to %v, want %v", gotMode, connectivity.ServingModeServing)
		}
	}
	waitForFailedRPCWithStatus(ctx, t, cc, codes.Unavailable, "error from xDS configuration for matched route configuration", nodeID)
}

// Tests the case where the listener resource points to multiple route
// configuration resources.
//
//   - Initially the listener resource points to three route configuration
//     resources (A, B and C). The filter chain in the listener matches incoming
//     connections to route A, and RPCs are expected to succeed.
//   - A streaming RPC is also kept open at this point.
//   - The listener resource is then updated to point to two route configuration
//     resources (A and B). The filter chain in the listener resource does not
//     match to any of the configured routes. The default filter chain though
//     matches to route B, which contains a route action of type "Route", and this
//     is not supported on the server side. New RPCs are expected to fail, while
//     any ongoing RPCs should be allowed to complete.
//   - The listener resource is then updated to point to a single route
//     configuration (A), and the filter chain in the listener matches to route A.
//     New RPCs are expected to succeed at this point.
func (s) TestServer_MultipleRouteConfigurations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server.
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)

	// Create a listener on a local port to act as the xDS enabled gRPC server.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to listen to local port: %v", err)
	}
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}

	// Setup the management server to respond with a listener resource that
	// specifies three route names to watch, and the corresponding route
	// configuration resources.
	const routeConfigNameA = "routeName-A"
	const routeConfigNameB = "routeName-B"
	const routeConfigNameC = "routeName-C"
	ldsResource := e2e.DefaultServerListenerWithRouteConfigName(host, port, e2e.SecurityLevelNone, routeConfigNameA)
	ldsResource.FilterChains = append(ldsResource.FilterChains,
		filterChainWontMatch(t, routeConfigNameB, "1.1.1.1", []uint32{1}),
		filterChainWontMatch(t, routeConfigNameC, "2.2.2.2", []uint32{2}),
	)
	routeConfigA := e2e.RouteConfigNonForwardingAction(routeConfigNameA)
	routeConfigB := e2e.RouteConfigFilterAction(routeConfigNameB) // Unsupported route action on server.
	routeConfigC := e2e.RouteConfigFilterAction(routeConfigNameC) // Unsupported route action on server.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{ldsResource},
		Routes:         []*v3routepb.RouteConfiguration{routeConfigA, routeConfigB, routeConfigC},
		SkipValidation: true,
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Start an xDS-enabled gRPC server with the above bootstrap configuration.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	modeChangeOpt := xds.ServingModeCallback(func(addr net.Addr, args xds.ServingModeChangeArgs) {
		t.Logf("Serving mode for listener %q changed to %q, err: %v", addr.String(), args.Mode, args.Err)
	})
	createStubServer(t, lis, modeChangeOpt, xds.ClientPoolForTesting(pool))

	// Create a gRPC channel and verify that RPCs succeed.
	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", lis.Addr(), err)
	}
	defer cc.Close()
	waitForSuccessfulRPC(ctx, t, cc)

	// Start a streaming RPC and keep the stream open.
	client := testgrpc.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall failed: %v", err)
	}
	if err = stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("stream.Send() failed: %v, should continue to work due to graceful stop", err)
	}

	// Update the listener resource such that the filter chain does not match
	// incoming connections to route A. Instead a default filter chain matches
	// to route B, which contains an unsupported route action.
	ldsResource = e2e.DefaultServerListenerWithRouteConfigName(host, port, e2e.SecurityLevelNone, routeConfigNameA)
	ldsResource.FilterChains = []*v3listenerpb.FilterChain{filterChainWontMatch(t, routeConfigNameA, "1.1.1.1", []uint32{1})}
	ldsResource.DefaultFilterChain = filterChainWontMatch(t, routeConfigNameB, "2.2.2.2", []uint32{2})
	resources.Listeners = []*v3listenerpb.Listener{ldsResource}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// xDS is eventually consistent. So simply poll for the new change to be
	// reflected.
	// "NonForwardingAction is expected for all Routes used on server-side; a
	// route with an inappropriate action causes RPCs matching that route to
	// fail with UNAVAILABLE." - A36
	waitForFailedRPCWithStatus(ctx, t, cc, codes.Unavailable, "the incoming RPC matched to a route that was not of action type non forwarding", nodeID)

	// Stream should be allowed to continue on the old working configuration -
	// as it on a connection that is gracefully closed (old FCM/LDS
	// Configuration which is allowed to continue).
	if err = stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() failed: %v, should continue to work due to graceful stop", err)
	}
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("unexpected error: %v, expected an EOF error", err)
	}

	// Update the listener resource to point to a single route configuration
	// that is expected to match and verify that RPCs succeed.
	resources.Listeners = []*v3listenerpb.Listener{e2e.DefaultServerListener(host, port, e2e.SecurityLevelNone, routeConfigNameA)}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	waitForSuccessfulRPC(ctx, t, cc)
}

// filterChainWontMatch returns a filter chain that won't match if running the
// test locally.
func filterChainWontMatch(t *testing.T, routeName string, addressPrefix string, srcPorts []uint32) *v3listenerpb.FilterChain {
	hcm := &v3httppb.HttpConnectionManager{
		RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
			Rds: &v3httppb.Rds{
				ConfigSource: &v3corepb.ConfigSource{
					ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
				},
				RouteConfigName: routeName,
			},
		},
		HttpFilters: []*v3httppb.HttpFilter{e2e.HTTPFilter("router", &v3routerpb.Router{})},
	}
	return &v3listenerpb.FilterChain{
		Name: routeName + "-wont-match",
		FilterChainMatch: &v3listenerpb.FilterChainMatch{
			PrefixRanges: []*v3corepb.CidrRange{
				{
					AddressPrefix: addressPrefix,
					PrefixLen: &wrapperspb.UInt32Value{
						Value: uint32(0),
					},
				},
			},
			SourceType:  v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK,
			SourcePorts: srcPorts,
			SourcePrefixRanges: []*v3corepb.CidrRange{
				{
					AddressPrefix: addressPrefix,
					PrefixLen: &wrapperspb.UInt32Value{
						Value: uint32(0),
					},
				},
			},
		},
		Filters: []*v3listenerpb.Filter{
			{
				Name:       "filter-1",
				ConfigType: &v3listenerpb.Filter_TypedConfig{TypedConfig: testutils.MarshalAny(t, hcm)},
			},
		},
	}
}
