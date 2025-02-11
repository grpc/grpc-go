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

// Custom key to indicate name resolution delay in context
const nameResolutionDelayKey ctxKey = "nameResolutionDelay"

// gRPC server implementation
type server struct {
	testgrpc.UnimplementedTestServiceServer
}

// EmptyCall is a simple RPC that returns an empty response.
func (s *server) EmptyCall(_ context.Context, req *testgrpc.Empty) (*testgrpc.Empty, error) {
	return &testgrpc.Empty{}, nil
}

// Custom StatsHandler to verify if the delay is detected.
type testStatsHandler struct {
	isDelayed bool
}

func (h *testStatsHandler) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	// Check for the delay key in the context.
	if delayed, ok := ctx.Value(nameResolutionDelayKey).(bool); ok && delayed {
		h.isDelayed = true
		fmt.Println("StatsHandler detected name resolution delay.")
	}
	return ctx
}

func (h *testStatsHandler) HandleRPC(_ context.Context, _ stats.RPCStats) {}

func (h *testStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (h *testStatsHandler) HandleConn(_ context.Context, _ stats.ConnStats) {}

// TestNameResolutionDelayInStatsHandler tests the behavior of gRPC client and
// server to detect and handle name resolution delays.
func (s) TestNameResolutionDelayInStatsHandler(t *testing.T) {
	// Manual resolver to simulate delayed resolution.
	r := manual.NewBuilderWithScheme("test")
	t.Logf("Registered manual resolver with scheme: %s", r.Scheme())

	// Start a gRPC server.
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()

	srv := grpc.NewServer()
	testgrpc.RegisterTestServiceServer(srv, &server{})
	go srv.Serve(lis)
	defer srv.Stop()
	t.Logf("Started gRPC server at %s", lis.Addr().String())

	statsHandler := &testStatsHandler{}
	creds := &attrTransportCreds{}
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithResolvers(r),
		grpc.WithStatsHandler(statsHandler),
	}
	cc, err := grpc.NewClient(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	tc := testgrpc.NewTestServiceClient(cc)
	t.Log("Created a ClientConn...")

	// First RPC should fail because there are no addresses yet.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if _, err := tc.EmptyCall(ctx, &testgrpc.Empty{}); err == nil || status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() = _, %v, want _, DeadlineExceeded", err)
	}
	t.Log("Made an RPC which was expected to fail...")

	go func() {
		time.Sleep(2 * time.Second)
		state := resolver.State{Addresses: []resolver.Address{{Addr: lis.Addr().String()}}}
		r.UpdateState(state)
		t.Logf("Pushed resolver state update: %v", state)
	}()

	// Second RPC should succeed after the resolver state is updated.
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	ctx = context.WithValue(ctx, nameResolutionDelayKey, true)
	defer cancel()
	if _, err := tc.EmptyCall(ctx, &testgrpc.Empty{}); err != nil {
		t.Fatalf("EmptyCall() = _, %v, want _, <nil>", err)
	}
	t.Log("Made an RPC which succeeded...")

	if !statsHandler.isDelayed {
		t.Errorf("Expected StatsHandler to detect name resolution delay, but it did not")
	}
}
