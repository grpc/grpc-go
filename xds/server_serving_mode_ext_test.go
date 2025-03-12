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
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds"
	"google.golang.org/grpc/xds/internal/xdsclient"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

// Tests the Server's logic as it transitions from NOT_SERVING to SERVING, then
// to NOT_SERVING again. Before it goes to SERVING, connections should be
// accepted and closed. After it goes SERVING, RPC's should proceed as normal
// according to matched route configuration. After it transitions back into
// NOT_SERVING, (through an explicit LDS Resource Not Found), previously running
// RPC's should be gracefully closed and still work, and new RPC's should fail.
func (s) TestServer_ServingModeChanges_SingleServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)

	// Setup the management server to respond with a listener resource that
	// specifies a route name to watch. Due to not having received the full
	// configuration, this should cause the server to be in mode NOT_SERVING.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}
	listener := e2e.DefaultServerListenerWithRouteConfigName(host, port, e2e.SecurityLevelNone, "routeName")
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listener},
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

	// Start a gRPC channel to the above server. The server is yet to receive
	// route configuration, and therefore RPCs must fail at this time.
	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	defer cc.Close()
	waitForFailedRPCWithStatus(ctx, t, cc, codes.Unavailable, "", "")

	// Setup the route configuration resource on the management server. This
	// should cause the xDS-enabled gRPC server to move to SERVING mode.
	routeConfig := e2e.RouteConfigNonForwardingAction("routeName")
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{listener},
		Routes:         []*v3routepb.RouteConfiguration{routeConfig},
		SkipValidation: true,
	}
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for the xDS-enabled gRPC server to go SERVING")
	case gotMode := <-modeChangeHandler.modeCh:
		if gotMode != connectivity.ServingModeServing {
			t.Fatalf("Mode changed to %v, want %v", gotMode, connectivity.ServingModeServing)
		}
	}
	waitForSuccessfulRPC(ctx, t, cc)

	// Start a stream before switching the server to not serving. Due to the
	// stream being created before the graceful stop of the underlying
	// connection, it should be able to continue even after the server switches
	// to not serving.
	c := testgrpc.NewTestServiceClient(cc)
	stream, err := c.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("cc.FullDuplexCall failed: %f", err)
	}

	// Remove the listener resource from the management server.
	resources.Listeners = nil
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Ensure the server is in NOT_SERVING mode.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for the xDS-enabled gRPC server to go NOT_SERVING")
	case gotMode := <-modeChangeHandler.modeCh:
		if gotMode != connectivity.ServingModeNotServing {
			t.Fatalf("Mode changed to %v, want %v", gotMode, connectivity.ServingModeNotServing)
		}
		gotErr := <-modeChangeHandler.errCh
		if gotErr == nil || !strings.Contains(gotErr.Error(), nodeID) {
			t.Fatalf("Unexpected error: %v, want xDS Node id: %s", gotErr, nodeID)
		}
	}

	// Due to graceful stop, any started streams continue to work.
	if err = stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("stream.Send() failed: %v, should continue to work due to graceful stop", err)
	}
	if err = stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend() failed: %v, should continue to work due to graceful stop", err)
	}
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv() failed with %v, want io.EOF", err)
	}

	// New RPCs on that connection should eventually start failing.
	waitForFailedRPCWithStatus(ctx, t, cc, codes.Unavailable, "", "")
}

// Tests the serving mode functionality with multiple xDS enabled gRPC servers.
func (s) TestServer_ServingModeChanges_MultipleServers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	managementServer, nodeID, bootstrapContents, _ := setup.ManagementServerAndResolver(t)

	// Create two local listeners and pass it to Serve().
	lis1, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	lis2, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}

	// Create a server option to get notified about serving mode changes.
	modeChangeHandler1 := newServingModeChangeHandler(t)
	modeChangeOpt1 := xds.ServingModeCallback(modeChangeHandler1.modeChangeCallback)
	modeChangeHandler2 := newServingModeChangeHandler(t)
	modeChangeOpt2 := xds.ServingModeCallback(modeChangeHandler2.modeChangeCallback)

	// Start two xDS-enabled gRPC servers with the above bootstrap configuration.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bootstrapContents), err)
	}
	pool := xdsclient.NewPool(config)
	createStubServer(t, lis1, modeChangeOpt1, xds.ClientPoolForTesting(pool))
	createStubServer(t, lis2, modeChangeOpt2, xds.ClientPoolForTesting(pool))

	// Setup the management server to respond with server-side Listener
	// resources for both listeners.
	host1, port1, err := hostPortFromListener(lis1)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}
	listener1 := e2e.DefaultServerListener(host1, port1, e2e.SecurityLevelNone, "routeName")
	host2, port2, err := hostPortFromListener(lis2)
	if err != nil {
		t.Fatalf("Failed to retrieve host and port of server: %v", err)
	}
	listener2 := e2e.DefaultServerListener(host2, port2, e2e.SecurityLevelNone, "routeName")
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener1, listener2},
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for both listeners to move to "serving" mode.
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for a mode change update: %v", err)
	case mode := <-modeChangeHandler1.modeCh:
		if mode != connectivity.ServingModeServing {
			t.Fatalf("Listener 1 received new mode %v, want %v", mode, connectivity.ServingModeServing)
		}
	}
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for a mode change update: %v", err)
	case mode := <-modeChangeHandler2.modeCh:
		if mode != connectivity.ServingModeServing {
			t.Fatalf("Listener 2 received new mode %v, want %v", mode, connectivity.ServingModeServing)
		}
	}

	// Create a ClientConn to the first listener and make a successful RPCs.
	cc1, err := grpc.NewClient(lis1.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer cc1.Close()
	waitForSuccessfulRPC(ctx, t, cc1)

	// Create a ClientConn to the second listener and make a successful RPCs.
	cc2, err := grpc.NewClient(lis2.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer cc2.Close()
	waitForSuccessfulRPC(ctx, t, cc2)

	// Update the management server to remove the second listener resource. This
	// should push only the second listener into "not-serving" mode.
	if err := managementServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener1},
	}); err != nil {
		t.Fatal(err)
	}

	// Wait for lis2 to move to "not-serving" mode.
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for a mode change update: %v", err)
	case mode := <-modeChangeHandler2.modeCh:
		if mode != connectivity.ServingModeNotServing {
			t.Fatalf("Listener received new mode %v, want %v", mode, connectivity.ServingModeNotServing)
		}
		gotErr := <-modeChangeHandler2.errCh
		if gotErr == nil || !strings.Contains(gotErr.Error(), nodeID) {
			t.Fatalf("Unexpected error: %v, want xDS Node id: %s", gotErr, nodeID)
		}
	}

	// Make sure RPCs succeed on cc1 and fail on cc2.
	waitForSuccessfulRPC(ctx, t, cc1)
	waitForFailedRPCWithStatus(ctx, t, cc2, codes.Unavailable, "", "")

	// Update the management server to remove the first listener resource as
	// well. This should push the first listener into "not-serving" mode. Second
	// listener is already in "not-serving" mode.
	if err := managementServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{},
	}); err != nil {
		t.Fatal(err)
	}

	// Wait for lis1 to move to "not-serving" mode. lis2 was already removed
	// from the xdsclient's resource cache. So, lis2's callback will not be
	// invoked this time around.
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for a mode change update: %v", err)
	case mode := <-modeChangeHandler1.modeCh:
		if mode != connectivity.ServingModeNotServing {
			t.Fatalf("Listener received new mode %v, want %v", mode, connectivity.ServingModeNotServing)
		}
		gotErr := <-modeChangeHandler1.errCh
		if gotErr == nil || !strings.Contains(gotErr.Error(), nodeID) {
			t.Fatalf("Unexpected error: %v, want xDS Node id: %s", gotErr, nodeID)
		}
	}

	// Make sure RPCs fail on both.
	waitForFailedRPCWithStatus(ctx, t, cc1, codes.Unavailable, "", "")
	waitForFailedRPCWithStatus(ctx, t, cc2, codes.Unavailable, "", "")

	// Make sure new connection attempts to "not-serving" servers fail.
	if cc1, err = grpc.NewClient(lis1.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials())); err != nil {
		t.Fatal("Failed to create clientConn to a server in \"not-serving\" state")
	}
	defer cc1.Close()
	if _, err := testgrpc.NewTestServiceClient(cc1).FullDuplexCall(ctx); status.Code(err) != codes.Unavailable {
		t.Fatalf("FullDuplexCall failed with status code: %v, want: Unavailable", status.Code(err))
	}

	// Update the management server with both listener resources.
	if err := managementServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener1, listener2},
	}); err != nil {
		t.Fatal(err)
	}

	// Wait for both listeners to move to "serving" mode.
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for a mode change update: %v", err)
	case mode := <-modeChangeHandler1.modeCh:
		if mode != connectivity.ServingModeServing {
			t.Fatalf("Listener received new mode %v, want %v", mode, connectivity.ServingModeServing)
		}
	}
	select {
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for a mode change update: %v", err)
	case mode := <-modeChangeHandler2.modeCh:
		if mode != connectivity.ServingModeServing {
			t.Fatalf("Listener received new mode %v, want %v", mode, connectivity.ServingModeServing)
		}
	}

	// The clientConns created earlier should be able to make RPCs now.
	waitForSuccessfulRPC(ctx, t, cc1)
	waitForSuccessfulRPC(ctx, t, cc2)
}
