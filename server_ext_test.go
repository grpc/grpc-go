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

package grpc_test

import (
	"context"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/stubserver"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

// TestServer_MaxHandlers ensures that no more than MaxConcurrentStreams server
// handlers are active at one time.
func (s) TestServer_MaxHandlers(t *testing.T) {
	started := make(chan struct{})
	blockCalls := grpcsync.NewEvent()

	// This stub server does not properly respect the stream context, so it will
	// not exit when the context is canceled.
	ss := stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			started <- struct{}{}
			<-blockCalls.Done()
			return nil
		},
	}
	if err := ss.Start([]grpc.ServerOption{grpc.MaxConcurrentStreams(1)}); err != nil {
		t.Fatal("Error starting server:", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start one RPC to the server.
	ctx1, cancel1 := context.WithCancel(ctx)
	_, err := ss.Client.FullDuplexCall(ctx1)
	if err != nil {
		t.Fatal("Error staring call:", err)
	}

	// Wait for the handler to be invoked.
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for RPC to start on server.")
	}

	// Cancel it on the client.  The server handler will still be running.
	cancel1()

	ctx2, cancel2 := context.WithCancel(ctx)
	defer cancel2()
	s, err := ss.Client.FullDuplexCall(ctx2)
	if err != nil {
		t.Fatal("Error staring call:", err)
	}

	// After 100ms, allow the first call to unblock.  That should allow the
	// second RPC to run and finish.
	select {
	case <-started:
		blockCalls.Fire()
		t.Fatalf("RPC started unexpectedly.")
	case <-time.After(100 * time.Millisecond):
		blockCalls.Fire()
	}

	select {
	case <-started:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for second RPC to start on server.")
	}
	if _, err := s.Recv(); err != io.EOF {
		t.Fatal("Received unexpected RPC error:", err)
	}
}
