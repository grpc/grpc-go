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

package test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/stubserver"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

// TestClientConnClose_WithPendingRPC tests the scenario where the channel has
// not yet received any update from the name resolver and hence RPCs are
// blocking. The test verifies that closing the ClientConn unblocks the RPC with
// the expected error code.
func (s) TestClientConnClose_WithPendingRPC(t *testing.T) {
	r := manual.NewBuilderWithScheme("whatever")
	cc, err := grpc.NewClient(r.Scheme()+":///test.server", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	client := testgrpc.NewTestServiceClient(cc)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	doneErrCh := make(chan error, 1)
	go func() {
		// This RPC would block until the ClientConn is closed, because the
		// resolver has not provided its first update yet.
		_, err := client.EmptyCall(ctx, &testpb.Empty{})
		if status.Code(err) != codes.Canceled || !strings.Contains(err.Error(), "client connection is closing") {
			doneErrCh <- fmt.Errorf("EmptyCall() = %v, want %s", err, codes.Canceled)
		}
		doneErrCh <- nil
	}()

	// Make sure that there is one pending RPC on the ClientConn before attempting
	// to close it. If we don't do this, cc.Close() can happen before the above
	// goroutine gets to make the RPC.
	for {
		if err := ctx.Err(); err != nil {
			t.Fatal(err)
		}
		tcs, _ := channelz.GetTopChannels(0, 0)
		if len(tcs) != 1 {
			t.Fatalf("there should only be one top channel, not %d", len(tcs))
		}
		started := tcs[0].ChannelMetrics.CallsStarted.Load()
		completed := tcs[0].ChannelMetrics.CallsSucceeded.Load() + tcs[0].ChannelMetrics.CallsFailed.Load()
		if (started - completed) == 1 {
			break
		}
		time.Sleep(defaultTestShortTimeout)
	}
	cc.Close()
	if err := <-doneErrCh; err != nil {
		t.Fatal(err)
	}
}

// Custom StatsHandler to verify if the delay is detected.
type testStatsHandler struct {
	nameResolutionDelayed bool
}

// TagRPC is called when an RPC is initiated and allows adding metadata to the
// context. It checks if the RPC experienced a name resolution delay and updates
// the handler's state.
func (h *testStatsHandler) TagRPC(ctx context.Context, rpcInfo *stats.RPCTagInfo) context.Context {
	h.nameResolutionDelayed = rpcInfo.NameResolutionDelay
	return ctx
}

// HandleRPC is a no-op implementation for handling RPC stats.
// This method is required to satisfy the stats.Handler interface.
func (h *testStatsHandler) HandleRPC(_ context.Context, _ stats.RPCStats) {}

// TagConn exists to satisfy stats.Handler.
func (h *testStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

// HandleConn exists to satisfy stats.Handler.
func (h *testStatsHandler) HandleConn(_ context.Context, _ stats.ConnStats) {}

// startStubServer initializes a stub gRPC server and returns its address and cleanup function.
func startStubServer(t *testing.T) (*stubserver.StubServer, func()) {
	stub := &stubserver.StubServer{
		EmptyCallF: func(_ context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			t.Log("EmptyCall received and processed")
			return &testpb.Empty{}, nil
		},
	}
	if err := stub.Start(nil); err != nil {
		t.Fatalf("Failed to start StubServer: %v", err)
	}
	return stub, func() { stub.Stop() }
}

// createTestClient sets up a gRPC client connection with a manual resolver.
func createTestClient(t *testing.T, scheme string, statsHandler *testStatsHandler) (*grpc.ClientConn, *manual.Resolver) {
	rb := manual.NewBuilderWithScheme(scheme)
	cc, err := grpc.NewClient(
		scheme+":///test.server",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(rb),
		grpc.WithStatsHandler(statsHandler),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	return cc, rb
}

// TestRPCSucceedsWithImmediateResolution ensures that when a resolver
// provides addresses immediately, RPC calls proceed without delay.
func (s) TestRPCSucceedsWithImmediateResolution(t *testing.T) {
	stub, cleanup := startStubServer(t)
	defer cleanup()

	statsHandler := &testStatsHandler{}
	rb := manual.NewBuilderWithScheme("instant")
	rb.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: stub.Address}}})
	cc, err := grpc.NewClient(rb.Scheme()+":///test.server",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(rb),
		grpc.WithStatsHandler(statsHandler),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient error: %v", err)
	}
	defer cc.Close()

	// Call an RPC to trigger waitForResolvedAddrs.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	client := testgrpc.NewTestServiceClient(cc)
	// First RPC call should succeed immediately.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("First RPC failed unexpectedly: %v", err)
	}
	// Ensure that resolution was not delayed.
	if statsHandler.nameResolutionDelayed {
		t.Fatalf("Expected no name resolution delay, but it was detected.")
	}
}

// TestStatsHandlerDetectsResolutionDelay verifies that the StatsHandler
// detects delays in name resolution when using a manual resolver.
// Ensures that once resolution is updated, the RPC completes successfully.
// Checks that the StatsHandler correctly detects the name resolution delay.
func (s) TestStatsHandlerDetectsResolutionDelay(t *testing.T) {
	stub, cleanup := startStubServer(t)
	defer cleanup()

	statsHandler := &testStatsHandler{}
	cc, rb := createTestClient(t, "delayed", statsHandler)
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	resolutionReady := make(chan struct{})
	rpcCompleted := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_, err := client.EmptyCall(ctx, &testpb.Empty{})
		rpcCompleted <- err
	}()
	// Simulate delayed resolution and unblock it via resolutionReady
	go func() {
		<-resolutionReady
		rb.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: stub.Address}}})
		t.Log("Resolver state updated, unblocking RPC.")
	}()
	// Block until we’re ready to test the resolution delay
	select {
	case <-time.After(100 * time.Millisecond):
		t.Log("Initial delay passed, signaling resolution to proceed.")
		close(resolutionReady)
	case <-rpcCompleted:
		t.Fatal("RPC completed prematurely before resolution was updated!")
	case <-ctx.Done():
		t.Fatal("Test setup timed out unexpectedly.")
	}

	// Wait for the RPC to complete after resolution
	select {
	case err := <-rpcCompleted:
		if err != nil {
			t.Fatalf("RPC failed after resolution: %v", err)
		}
		t.Log("RPC completed successfully after resolution.")
	case <-ctx.Done():
		t.Fatal("RPC did not complete within timeout after resolver update.")
	}

	// Verify StatsHandler detected the name resolution delay
	if !statsHandler.nameResolutionDelayed {
		t.Errorf("Expected StatsHandler to detect name resolution delay, but it did not!")
	}
}
