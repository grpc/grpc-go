/*
 *
 * Copyright 2024 gRPC authors.
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
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/interop"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/stats"
)

// TestPeerForClientStatsHandler tests the scenario where stats handler
// (having peer as part of its struct) has peer enriched as part of
// stream context.
func (s) TestPeerForClientStatsHandler(t *testing.T) {
	spy := &handlerSpy{}

	// Start server.
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	testgrpc.RegisterTestServiceServer(grpcServer, interop.NewTestServer())
	errCh := make(chan error)
	go func() {
		errCh <- grpcServer.Serve(l)
	}()
	t.Cleanup(func() {
		grpcServer.Stop()
		if err := <-errCh; err != nil {
			t.Error(err)
		}
	})

	// Create client with stats handler and do some calls.
	conn, err := grpc.NewClient(
		l.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(spy))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client := testgrpc.NewTestServiceClient(conn)
	interop.DoEmptyUnaryCall(ctx, client)
	interop.DoLargeUnaryCall(ctx, client)
	interop.DoClientStreaming(ctx, client)
	interop.DoServerStreaming(ctx, client)
	interop.DoPingPong(ctx, client)

	// Assert if peer is populated for each stats type.
	for _, callbackArgs := range spy.Args {
		if callbackArgs.Peer == nil {
			switch callbackArgs.RPCStats.(type) {
			case *stats.Begin:
				continue
			case *stats.PickerUpdated:
				continue
			default:
			}
			t.Errorf("peer not populated for: %T", callbackArgs.RPCStats)
		}
	}
}

type peerStats struct {
	RPCStats stats.RPCStats
	Peer     *peer.Peer
}

type handlerSpy struct {
	Args []peerStats
	mu   sync.Mutex
}

func (h *handlerSpy) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	return ctx
}

func (h *handlerSpy) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	h.mu.Lock()
	p, _ := peer.FromContext(ctx)
	h.Args = append(h.Args, peerStats{rs, p})
	h.mu.Unlock()
}

func (h *handlerSpy) TagConn(ctx context.Context, info *stats.ConnTagInfo) context.Context {
	return ctx
}

func (h *handlerSpy) HandleConn(context.Context, stats.ConnStats) {}
