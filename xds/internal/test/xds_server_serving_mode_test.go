//go:build !386
// +build !386

/*
 *
 * Copyright 2021 gRPC authors.
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

// Package xds_test contains e2e tests for xDS use.
package xds_test

import (
	"context"
	"net"
	"testing"
	"time"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	xdscreds "google.golang.org/grpc/credentials/xds"
	testpb "google.golang.org/grpc/test/grpc_testing"
	"google.golang.org/grpc/xds"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/e2e"
)

// TestServerSideXDS_ServingModeChanges tests the serving mode functionality in
// xDS enabled gRPC servers. It verifies that appropriate mode changes happen in
// the server, and also verifies behavior of clientConns under these modes.
func (s) TestServerSideXDS_ServingModeChanges(t *testing.T) {
	managementServer, nodeID, bootstrapContents, _, cleanup := setupManagementServer(t)
	defer cleanup()

	// Configure xDS credentials to be used on the server-side.
	creds, err := xdscreds.NewServerCredentials(xdscreds.ServerOptions{
		FallbackCreds: insecure.NewCredentials(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create two local listeners and pass it to Serve().
	lis1, err := xdstestutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	lis2, err := xdstestutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}

	// Create a couple of channels on which mode updates will be pushed.
	updateCh1 := make(chan connectivity.ServingMode, 1)
	updateCh2 := make(chan connectivity.ServingMode, 1)

	// Create a server option to get notified about serving mode changes, and
	// push the updated mode on the channels created above.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	modeChangeOpt := xds.ServingModeCallback(func(addr net.Addr, args xds.ServingModeChangeArgs) {
		t.Logf("serving mode for listener %q changed to %q, err: %v", addr.String(), args.Mode, args.Err)
		switch addr.String() {
		case lis1.Addr().String():
			updateCh1 <- args.Mode
		case lis2.Addr().String():
			updateCh2 <- args.Mode
		default:
			t.Logf("serving mode callback invoked for unknown listener address: %q", addr.String())
		}
	})

	// Initialize an xDS-enabled gRPC server and register the stubServer on it.
	server := xds.NewGRPCServer(grpc.Creds(creds), modeChangeOpt, xds.BootstrapContentsForTesting(bootstrapContents))
	defer server.Stop()
	testpb.RegisterTestServiceServer(server, &testService{})

	// Setup the management server to respond with server-side Listener
	// resources for both listeners.
	host1, port1, err := hostPortFromListener(lis1)
	if err != nil {
		t.Fatalf("failed to retrieve host and port of server: %v", err)
	}
	listener1 := e2e.DefaultServerListener(host1, port1, e2e.SecurityLevelNone)
	host2, port2, err := hostPortFromListener(lis2)
	if err != nil {
		t.Fatalf("failed to retrieve host and port of server: %v", err)
	}
	listener2 := e2e.DefaultServerListener(host2, port2, e2e.SecurityLevelNone)
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener1, listener2},
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	go func() {
		if err := server.Serve(lis1); err != nil {
			t.Errorf("Serve() failed: %v", err)
		}
	}()
	go func() {
		if err := server.Serve(lis2); err != nil {
			t.Errorf("Serve() failed: %v", err)
		}
	}()

	// Wait for both listeners to move to "serving" mode.
	select {
	case <-ctx.Done():
		t.Fatalf("timed out waiting for a mode change update: %v", err)
	case mode := <-updateCh1:
		if mode != connectivity.ServingModeServing {
			t.Errorf("listener received new mode %v, want %v", mode, connectivity.ServingModeServing)
		}
	}
	select {
	case <-ctx.Done():
		t.Fatalf("timed out waiting for a mode change update: %v", err)
	case mode := <-updateCh2:
		if mode != connectivity.ServingModeServing {
			t.Errorf("listener received new mode %v, want %v", mode, connectivity.ServingModeServing)
		}
	}

	// Create a ClientConn to the first listener and make a successful RPCs.
	cc1, err := grpc.Dial(lis1.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc1.Close()
	waitForSuccessfulRPC(ctx, t, cc1)

	// Create a ClientConn to the second listener and make a successful RPCs.
	cc2, err := grpc.Dial(lis2.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc2.Close()
	waitForSuccessfulRPC(ctx, t, cc2)

	// Update the management server to remove the second listener resource. This
	// should push only the second listener into "not-serving" mode.
	if err := managementServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener1},
	}); err != nil {
		t.Error(err)
	}

	// Wait for lis2 to move to "not-serving" mode. lis1 also receives an update
	// here even though it stays in "serving" mode.
	// See https://github.com/grpc/grpc-go/issues/4695.
	select {
	case <-ctx.Done():
		t.Fatalf("timed out waiting for a mode change update: %v", err)
	case mode := <-updateCh1:
		if mode != connectivity.ServingModeServing {
			t.Errorf("listener received new mode %v, want %v", mode, connectivity.ServingModeServing)
		}
	}
	select {
	case <-ctx.Done():
		t.Fatalf("timed out waiting for a mode change update: %v", err)
	case mode := <-updateCh2:
		if mode != connectivity.ServingModeNotServing {
			t.Errorf("listener received new mode %v, want %v", mode, connectivity.ServingModeNotServing)
		}
	}

	// Make sure RPCs succeed on cc1 and fail on cc2.
	waitForSuccessfulRPC(ctx, t, cc1)
	waitForFailedRPC(ctx, t, cc2)

	// Update the management server to remove the first listener resource as
	// well. This should push the first listener into "not-serving" mode. Second
	// listener is already in "not-serving" mode.
	if err := managementServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{},
	}); err != nil {
		t.Error(err)
	}

	// Wait for lis1 to move to "not-serving" mode. lis2 was already removed
	// from the xdsclient's resource cache. So, lis2's callback will not be
	// invoked this time around.
	select {
	case <-ctx.Done():
		t.Fatalf("timed out waiting for a mode change update: %v", err)
	case mode := <-updateCh1:
		if mode != connectivity.ServingModeNotServing {
			t.Errorf("listener received new mode %v, want %v", mode, connectivity.ServingModeNotServing)
		}
	}

	// Make sure RPCs fail on both.
	waitForFailedRPC(ctx, t, cc1)
	waitForFailedRPC(ctx, t, cc2)

	// Make sure new connection attempts to "not-serving" servers fail. We use a
	// short timeout since we expect this to fail.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := grpc.DialContext(sCtx, lis1.Addr().String(), grpc.WithBlock(), grpc.WithTransportCredentials(insecure.NewCredentials())); err == nil {
		t.Fatal("successfully created clientConn to a server in \"not-serving\" state")
	}

	// Update the management server with both listener resources.
	if err := managementServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener1, listener2},
	}); err != nil {
		t.Error(err)
	}

	// Wait for both listeners to move to "serving" mode.
	select {
	case <-ctx.Done():
		t.Fatalf("timed out waiting for a mode change update: %v", err)
	case mode := <-updateCh1:
		if mode != connectivity.ServingModeServing {
			t.Errorf("listener received new mode %v, want %v", mode, connectivity.ServingModeServing)
		}
	}
	select {
	case <-ctx.Done():
		t.Fatalf("timed out waiting for a mode change update: %v", err)
	case mode := <-updateCh2:
		if mode != connectivity.ServingModeServing {
			t.Errorf("listener received new mode %v, want %v", mode, connectivity.ServingModeServing)
		}
	}

	// The clientConns created earlier should be able to make RPCs now.
	waitForSuccessfulRPC(ctx, t, cc1)
	waitForSuccessfulRPC(ctx, t, cc2)
}

func waitForSuccessfulRPC(ctx context.Context, t *testing.T, cc *grpc.ClientConn) {
	t.Helper()

	c := testpb.NewTestServiceClient(cc)
	if _, err := c.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}

func waitForFailedRPC(ctx context.Context, t *testing.T, cc *grpc.ClientConn) {
	t.Helper()

	// Attempt one RPC before waiting for the ticker to expire.
	c := testpb.NewTestServiceClient(cc)
	if _, err := c.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		return
	}

	ticker := time.NewTimer(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("failure when waiting for RPCs to fail: %v", ctx.Err())
		case <-ticker.C:
			if _, err := c.EmptyCall(ctx, &testpb.Empty{}); err != nil {
				return
			}
		}
	}
}
