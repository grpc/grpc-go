/*
 *
 * Copyright 2020 gRPC authors.
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

// Binary client for xDS interop tests.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/peer"
	_ "google.golang.org/grpc/xds/experimental"
)

type statsWatcherKey struct {
	startID int32
	endID   int32
}

type statsWatcher struct {
	rpcsByPeer    map[string]int32
	numFailures   int32
	remainingRpcs int32
	c             chan *testpb.SimpleResponse
}

var (
	numChannels   = flag.Int("num_channels", 1, "Num of channels")
	printResponse = flag.Bool("print_response", false, "Write RPC response to stdout")
	qps           = flag.Int("qps", 1, "QPS per channel")
	rpcTimeout    = flag.Duration("rpc_timeout", 10*time.Second, "Per RPC timeout")
	server        = flag.String("server", "localhost:8080", "Address of server to connect to")
	statsPort     = flag.Int("stats_port", 8081, "Port to expose peer distribution stats service")

	mu               sync.Mutex
	currentRequestID int32
	watchers         = make(map[statsWatcherKey]*statsWatcher)
)

type statsService struct{}

// Wait for the next LoadBalancerStatsRequest.GetNumRpcs to start and complete,
// and return the distribution of remote peers. This is essentially a clientside
// LB reporting mechanism that is designed to be queried by an external test
// driver when verifying that the client is distributing RPCs as expected.
func (s *statsService) GetClientStats(ctx context.Context, in *testpb.LoadBalancerStatsRequest) (*testpb.LoadBalancerStatsResponse, error) {
	mu.Lock()
	watcherKey := statsWatcherKey{currentRequestID, currentRequestID + in.GetNumRpcs()}
	watcher, ok := watchers[watcherKey]
	if !ok {
		watcher = &statsWatcher{
			rpcsByPeer:    make(map[string]int32),
			numFailures:   0,
			remainingRpcs: in.GetNumRpcs(),
			c:             make(chan *testpb.SimpleResponse),
		}
		watchers[watcherKey] = watcher
	}
	mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, time.Duration(in.GetTimeoutSec())*time.Second)
	defer cancel()

	defer func() {
		mu.Lock()
		delete(watchers, watcherKey)
		mu.Unlock()
	}()

	// Wait until the requested RPCs have all been recorded or timeout occurs.
	for {
		select {
		case r := <-watcher.c:
			if r != nil {
				watcher.rpcsByPeer[(*r).GetHostname()]++
			} else {
				watcher.numFailures++
			}
			watcher.remainingRpcs--
			if watcher.remainingRpcs == 0 {
				return &testpb.LoadBalancerStatsResponse{NumFailures: watcher.numFailures + watcher.remainingRpcs, RpcsByPeer: watcher.rpcsByPeer}, nil
			}
		case <-ctx.Done():
			grpclog.Info("Timed out, returning partial stats")
			return &testpb.LoadBalancerStatsResponse{NumFailures: watcher.numFailures + watcher.remainingRpcs, RpcsByPeer: watcher.rpcsByPeer}, nil
		}
	}
}

func main() {
	flag.Parse()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *statsPort))
	if err != nil {
		grpclog.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	defer s.Stop()
	testpb.RegisterLoadBalancerStatsServiceServer(s, &statsService{})
	go s.Serve(lis)

	clients := make([]testpb.TestServiceClient, *numChannels)
	for i := 0; i < *numChannels; i++ {
		conn, err := grpc.DialContext(context.Background(), *server, grpc.WithInsecure(), grpc.WithBlock())
		if err != nil {
			grpclog.Fatalf("Fail to dial: %v", err)
		}
		defer conn.Close()
		clients[i] = testpb.NewTestServiceClient(conn)
	}
	ticker := time.NewTicker(time.Second / time.Duration(*qps**numChannels))
	defer ticker.Stop()
	sendRPCs(clients, ticker)
}

func sendRPCs(clients []testpb.TestServiceClient, ticker *time.Ticker) {
	var i int
	for range ticker.C {
		go func(i int) {
			c := clients[i]
			ctx, cancel := context.WithTimeout(context.Background(), *rpcTimeout)
			p := new(peer.Peer)
			mu.Lock()
			savedRequestID := currentRequestID
			currentRequestID++
			savedWatchers := []*statsWatcher{}
			for key, value := range watchers {
				if key.startID <= savedRequestID && savedRequestID < key.endID {
					savedWatchers = append(savedWatchers, value)
				}
			}
			mu.Unlock()
			r, err := c.UnaryCall(ctx, &testpb.SimpleRequest{FillServerId: true}, grpc.Peer(p))

			success := err == nil
			cancel()

			for _, watcher := range savedWatchers {
				watcher.c <- r
			}

			if success && *printResponse {
				fmt.Printf("Greeting: Hello world, this is %s, from %v\n", r.GetHostname(), p.Addr)
			}
		}(i)
		i = (i + 1) % len(clients)
	}
}
