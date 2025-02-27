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
	t.Helper()
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
	t.Helper()
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

// TestClientConnRPC_WithoutNameResolutionDelay verify that if the resolution
// has already happened once before at the time of making RPC, the name
// resolution flag is not set indicating there was no delay in name resolution.
func (s) TestClientConnRPC_WithoutNameResolutionDelay(t *testing.T) {
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
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client := testgrpc.NewTestServiceClient(cc)
	// Verify that the RPC succeeds.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("First RPC failed unexpectedly: %v", err)
	}
	// Verify that name resolution did not happen.
	if statsHandler.nameResolutionDelayed {
		t.Fatalf("Expected no name resolution delay, but it was detected.")
	}
}

// TestStatsHandlerDetectsResolutionDelay verifies that if this is the
// first time resolution is happening at the time of making RPC,
// nameResolutionDelayed flag was set indicating there was a delay in name
// resolution waiting for resolver to return addresses.
func (s) TestClientConnRPC_WithNameResolutionDelay(t *testing.T) {
	stub, cleanup := startStubServer(t)
	defer cleanup()

	statsHandler := &testStatsHandler{}
	cc, rb := createTestClient(t, "delayed", statsHandler)
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	rpcError := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_, err := client.EmptyCall(ctx, &testpb.Empty{})
		rpcError <- err
	}()

	timer := time.AfterFunc(100*time.Millisecond, func() {
		rb.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: stub.Address}}})
		t.Log("Resolver state updated, unblocking RPC.")
	})
	defer timer.Stop()

	select {
	case err := <-rpcError:
		if err != nil {
			t.Fatalf("RPC failed after resolution: %v", err)
		}
		if !statsHandler.nameResolutionDelayed {
			t.Fatalf("Expected statsHandler.nameResolutionDelayed to be true, but got %v", statsHandler.nameResolutionDelayed)
		}
	case <-ctx.Done():
		t.Fatal("Test setup timed out unexpectedly.")
	}
}
