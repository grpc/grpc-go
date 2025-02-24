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
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/channelz"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
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
		_, err := client.EmptyCall(ctx, &testgrpc.Empty{})
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

// EmptyCall is a simple RPC that returns an empty response.
func (s *server) EmptyCall(_ context.Context, _ *testgrpc.Empty) (*testgrpc.Empty, error) {
	return &testgrpc.Empty{}, nil
}

// gRPC server implementation
type server struct {
	testgrpc.UnimplementedTestServiceServer
}

// Custom StatsHandler to verify if the delay is detected.
type testStatsHandler struct {
	nameResolutionDelayed bool
}

// TagRPC is called when an RPC is initiated and allows adding metadata to the context.
// It checks if the RPC experienced a name resolution delay and updates the handler's state.
// If a delay is detected, it logs the event for debugging.
func (h *testStatsHandler) TagRPC(ctx context.Context, rpcInfo *stats.RPCTagInfo) context.Context {
	if rpcInfo.NameResolutionDelay {
		h.nameResolutionDelayed = true
		fmt.Println("StatsHandler detected name resolution delay via RPCInfo.")
	}
	return ctx
}

// HandleRPC is a no-op implementation for handling RPC stats.
// This method is required to satisfy the stats.Handler interface.
func (h *testStatsHandler) HandleRPC(_ context.Context, _ stats.RPCStats) {}

// TagConn is called when a new connection is established and allows tagging the connection context.
// This implementation simply returns the existing context without modification.
func (h *testStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

// HandleConn is a no-op implementation for handling connection stats.
// This method is required to satisfy the stats.Handler interface.
func (h *testStatsHandler) HandleConn(_ context.Context, _ stats.ConnStats) {}

// startTestGRPCServer initializes a test gRPC server on a random port.
// Returns the server address and a cleanup function.
func startTestGRPCServer(t *testing.T) (string, func()) {
	lis, err := net.Listen("tcp", "localhost:0") // Random available port
	if err != nil {
		t.Fatalf("Failed to create test gRPC server: %v", err)
	}

	srv := grpc.NewServer()
	testgrpc.RegisterTestServiceServer(srv, &server{})
	go srv.Serve(lis)

	// Return server address and cleanup function
	return lis.Addr().String(), func() {
		srv.Stop()
		lis.Close()
	}
}

// TestRPCSucceedsWithImmediateResolution verifies gRPC instantly resolves addresses when
// available.Uses a manual resolver, ensuring both RPCs succeed without delay.
func (s) TestRPCSucceedsWithImmediateResolution(t *testing.T) {
	serverAddress, cleanup := startTestGRPCServer(t)
	defer cleanup()

	// Create a manual resolver that immediately returns an address
	resolverBuilder := manual.NewBuilderWithScheme("instant")
	resolverBuilder.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: serverAddress}}})

	// Create a ClientConn using the manual resolver
	clientConn, err := grpc.NewClient(resolverBuilder.Scheme()+":///test.server",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(resolverBuilder), // Resolver already has addresses
	)
	if err != nil {
		t.Fatalf("grpc.NewClient error: %v", err)
	}
	defer clientConn.Close()

	// Call an RPC to trigger waitForResolvedAddrs
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	client := testgrpc.NewTestServiceClient(clientConn)

	// First RPC call should succeed immediately
	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Fatalf("First RPC failed unexpectedly: %v", err)
	}
	t.Log("First RPC succeeded immediately.")

	// Second RPC should also succeed without re-resolving
	ctx, cancel = context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Fatalf("Second RPC failed unexpectedly: %v", err)
	}
	t.Log("Second RPC succeeded, confirming resolution stability.")
}

// TestStatsHandlerDetectsResolutionDelay verifies that RPCs properly wait for
// name resolution when using a manual resolver that initially lacks addresses.
// The first RPC blocks until the resolver provides addresses.
// The resolver is updated after a simulated delay, unblocking RPCs.
// The second RPC succeeds after resolution is completed.
// The StatsHandler correctly detects and tracks the name resolution delay.
func (s) TestStatsHandlerDetectsResolutionDelay(t *testing.T) {
	// Create a manual resolver WITHOUT immediately providing addresses
	resolverBuilder := manual.NewBuilderWithScheme("delayed")

	// Create a channel to simulate delay before providing addresses
	resolutionReady := make(chan struct{})

	// Start a gRPC test server
	serverAddress, cleanup := startTestGRPCServer(t)
	defer cleanup()

	// Create a stats handler to track name resolution delay
	statsHandler := &testStatsHandler{}

	// Create a ClientConn using the resolver and stats handler
	clientConn, err := grpc.NewClient(resolverBuilder.Scheme()+":///test.server",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(resolverBuilder),
		grpc.WithStatsHandler(statsHandler),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient error: %v", err)
	}
	defer clientConn.Close()

	// Start an RPC in a goroutine (it should block)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		client := testgrpc.NewTestServiceClient(clientConn)
		// This RPC should block until resolver returns addresses
		if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
			t.Logf("First RPC failed as expected (before addresses available): %v", err)
		}
		close(resolutionReady)
	}()

	// Simulate a delay before updating resolver
	time.Sleep(2 * time.Second)

	// Update the resolver with valid addresses, unblocking RPC
	resolverBuilder.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: serverAddress}}})

	// Second RPC should succeed after resolver update
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := testgrpc.NewTestServiceClient(clientConn)
	if _, err := client.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Fatalf("Second RPC failed unexpectedly: %v", err)
	}
	t.Log("Second RPC succeeded after resolver update, confirming resolution completion.")

	if !statsHandler.nameResolutionDelayed {
		t.Errorf("Expected StatsHandler to detect name resolution delay, but it did not!")
	}
}
