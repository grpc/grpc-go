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
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/interop"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/stats"
)

// TestPeerForClientStatsHandler configures a stats handler that
// verifies that peer is sent for OutPayload, InPayload, End
// stats handlers.
func (s) TestPeerForClientStatsHandler(t *testing.T) {
	statsHandler := &peerStatsHandler{}

	// Define expected stats callouts and whether a peer object should be populated.
	// Note:
	//   * Begin stats don't have peer information as the RPC begins before peer resolution.
	//   * PickerUpdated stats don't have peer information as the picker operates without transport-level knowledge.
	expectedCallouts := map[stats.RPCStats]bool{
		&stats.OutPayload{}:    true,
		&stats.InHeader{}:      true,
		&stats.OutHeader{}:     true,
		&stats.InTrailer{}:     true,
		&stats.OutTrailer{}:    true,
		&stats.End{}:           true,
		&stats.Begin{}:         false,
		&stats.PickerUpdated{}: false,
	}

	// Start server.
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	testgrpc.RegisterTestServiceServer(s, interop.NewTestServer())
	errCh := make(chan error)
	go func() {
		errCh <- s.Serve(l)
	}()
	defer func() {
		s.Stop()
		if err := <-errCh; err != nil {
			t.Error(err)
		}
	}()

	// Create client with stats handler and do some calls.
	cc, err := grpc.NewClient(
		l.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(statsHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cc.Close(); err != nil {
			t.Error(err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client := testgrpc.NewTestServiceClient(cc)
	interop.DoClientStreaming(ctx, client)

	if len(getUniqueRPCStats(statsHandler.Args)) < len(expectedCallouts) {
		t.Errorf("Unexpected number of stats handler callouts.")
	}

	for _, callbackArgs := range statsHandler.Args {
		expectedPeer, found := expectedCallouts[callbackArgs.rpcStats]
		// In case expectation is set to false and still we got the peer,
		// then it's good to have it. So no need to assert those conditions.
		if found && expectedPeer && callbackArgs.peer != nil {
			continue
		} else if expectedPeer && callbackArgs.peer == nil {
			t.Errorf("peer not populated for: %T", callbackArgs.rpcStats)
		}
	}
}

// getUniqueRPCStats extracts a list of unique stats.RPCStats types from peer list of RPC callback.
func getUniqueRPCStats(args []peerStats) []stats.RPCStats {
	uniqueStatsTypes := make(map[stats.RPCStats]struct{})

	for _, callbackArgs := range args {
		uniqueStatsTypes[callbackArgs.rpcStats] = struct{}{}
	}

	var uniqueStatsList []stats.RPCStats
	for statsType := range uniqueStatsTypes {
		uniqueStatsList = append(uniqueStatsList, statsType)
	}

	return uniqueStatsList
}

type peerStats struct {
	rpcStats stats.RPCStats
	peer     *peer.Peer
}

type peerStatsHandler struct {
	Args []peerStats
}

func (h *peerStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	return ctx
}

func (h *peerStatsHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	p, _ := peer.FromContext(ctx)
	h.Args = append(h.Args, peerStats{rs, p})
}

func (h *peerStatsHandler) TagConn(ctx context.Context, info *stats.ConnTagInfo) context.Context {
	return ctx
}

func (h *peerStatsHandler) HandleConn(context.Context, stats.ConnStats) {}
