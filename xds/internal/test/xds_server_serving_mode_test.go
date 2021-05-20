// +build go1.13
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
	"fmt"
	"net"
	"sync"
	"testing"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	xdscreds "google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/internal/testutils"
	testpb "google.golang.org/grpc/test/grpc_testing"
	"google.golang.org/grpc/xds"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/e2e"
)

// A convenience typed used to keep track of mode changes on multiple listeners.
type modeTracker struct {
	mu       sync.Mutex
	modes    map[string]xds.ServingMode
	updateCh *testutils.Channel
}

func newModeTracker() *modeTracker {
	return &modeTracker{
		modes:    make(map[string]xds.ServingMode),
		updateCh: testutils.NewChannel(),
	}
}

func (mt *modeTracker) updateMode(ctx context.Context, addr net.Addr, mode xds.ServingMode) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	mt.modes[addr.String()] = mode
	// Sometimes we could get state updates which are not expected by the test.
	// Using `Send()` here would block in that case and cause the whole test to
	// hang and will eventually only timeout when the `-timeout` passed to `go
	// test` elapses. Using `SendContext()` here instead fails the test within a
	// reasonable timeout.
	mt.updateCh.SendContext(ctx, nil)
}

func (mt *modeTracker) getMode(addr net.Addr) xds.ServingMode {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	return mt.modes[addr.String()]
}

func (mt *modeTracker) waitForUpdate(ctx context.Context) error {
	_, err := mt.updateCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("error when waiting for a mode change update: %v", err)
	}
	return nil
}

// TestServerSideXDS_ServingModeChanges tests the serving mode functionality in
// xDS enabled gRPC servers. It verifies that appropriate mode changes happen in
// the server, and also verifies behavior of clientConns under these modes.
func (s) TestServerSideXDS_ServingModeChanges(t *testing.T) {
	// Configure xDS credentials to be used on the server-side.
	creds, err := xdscreds.NewServerCredentials(xdscreds.ServerOptions{
		FallbackCreds: insecure.NewCredentials(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a server option to get notified about serving mode changes.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	modeTracker := newModeTracker()
	modeChangeOpt := xds.ServingModeCallback(func(addr net.Addr, args xds.ServingModeChangeArgs) {
		t.Logf("serving mode for listener %q changed to %q, err: %v", addr.String(), args.Mode, args.Err)
		modeTracker.updateMode(ctx, addr, args.Mode)
	})

	// Initialize an xDS-enabled gRPC server and register the stubServer on it.
	server := xds.NewGRPCServer(grpc.Creds(creds), modeChangeOpt, xds.BootstrapContentsForTesting(bootstrapContents))
	defer server.Stop()
	testpb.RegisterTestServiceServer(server, &testService{})

	// Create two local listeners and pass it to Serve().
	lis1, err := xdstestutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	lis2, err := xdstestutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}

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
		NodeID:    xdsClientNodeID,
		Listeners: []*v3listenerpb.Listener{listener1, listener2},
	}
	if err := managementServer.Update(resources); err != nil {
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
	if err := waitForModeChange(ctx, modeTracker, lis1.Addr(), xds.ServingModeServing); err != nil {
		t.Fatal(err)
	}
	if err := waitForModeChange(ctx, modeTracker, lis2.Addr(), xds.ServingModeServing); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn to the first listener and make a successful RPCs.
	cc1, err := grpc.DialContext(ctx, lis1.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc1.Close()

	client1 := testpb.NewTestServiceClient(cc1)
	if _, err := client1.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// Create a ClientConn to the second listener and make a successful RPCs.
	cc2, err := grpc.DialContext(ctx, lis2.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc2.Close()

	client2 := testpb.NewTestServiceClient(cc2)
	if _, err := client2.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// Update the management server to remove the second listener resource. This
	// should push only the second listener into "not-serving" mode.
	if err := managementServer.Update(e2e.UpdateOptions{
		NodeID:    xdsClientNodeID,
		Listeners: []*v3listenerpb.Listener{listener1},
	}); err != nil {
		t.Error(err)
	}
	if err := waitForModeChange(ctx, modeTracker, lis2.Addr(), xds.ServingModeNotServing); err != nil {
		t.Fatal(err)
	}

	// Make sure cc1 is still in READY state, while cc2 has moved out of READY.
	if s := cc1.GetState(); s != connectivity.Ready {
		t.Fatalf("clientConn1 state is %s, want %s", s, connectivity.Ready)
	}
	if !cc2.WaitForStateChange(ctx, connectivity.Ready) {
		t.Fatal("clientConn2 failed to move out of READY")
	}

	// Make sure RPCs succeed on cc1 and fail on cc2.
	if _, err := client1.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
	if _, err := client2.EmptyCall(ctx, &testpb.Empty{}); err == nil {
		t.Fatal("rpc EmptyCall() succeeded when expected to fail")
	}

	// Update the management server to remove the first listener resource as
	// well. This should push the first listener into "not-serving" mode. Second
	// listener is already in "not-serving" mode.
	if err := managementServer.Update(e2e.UpdateOptions{
		NodeID:    xdsClientNodeID,
		Listeners: []*v3listenerpb.Listener{},
	}); err != nil {
		t.Error(err)
	}
	if err := waitForModeChange(ctx, modeTracker, lis1.Addr(), xds.ServingModeNotServing); err != nil {
		t.Fatal(err)
	}

	// Make sure cc1 has moved out of READY.
	if !cc1.WaitForStateChange(ctx, connectivity.Ready) {
		t.Fatal("clientConn1 failed to move out of READY")
	}

	// Make sure RPCs fail on both.
	if _, err := client1.EmptyCall(ctx, &testpb.Empty{}); err == nil {
		t.Fatal("rpc EmptyCall() succeeded when expected to fail")
	}
	if _, err := client2.EmptyCall(ctx, &testpb.Empty{}); err == nil {
		t.Fatal("rpc EmptyCall() succeeded when expected to fail")
	}

	// Make sure new connection attempts to "not-serving" servers fail. We use a
	// short timeout since we expect this to fail.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := grpc.DialContext(sCtx, lis1.Addr().String(), grpc.WithBlock(), grpc.WithTransportCredentials(insecure.NewCredentials())); err == nil {
		t.Fatal("successfully created clientConn to a server in \"not-serving\" state")
	}

	// Update the management server with both listener resources.
	if err := managementServer.Update(e2e.UpdateOptions{
		NodeID:    xdsClientNodeID,
		Listeners: []*v3listenerpb.Listener{listener1, listener2},
	}); err != nil {
		t.Error(err)
	}

	// Wait for both listeners to move to "serving" mode.
	if err := waitForModeChange(ctx, modeTracker, lis1.Addr(), xds.ServingModeServing); err != nil {
		t.Fatal(err)
	}
	if err := waitForModeChange(ctx, modeTracker, lis2.Addr(), xds.ServingModeServing); err != nil {
		t.Fatal(err)
	}

	// The clientConns created earlier should be able to make RPCs now.
	if _, err := client1.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
	if _, err := client2.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}

func waitForModeChange(ctx context.Context, modeTracker *modeTracker, addr net.Addr, wantMode xds.ServingMode) error {
	for {
		if gotMode := modeTracker.getMode(addr); gotMode == wantMode {
			return nil
		}
		if err := modeTracker.waitForUpdate(ctx); err != nil {
			return err
		}
	}
}
