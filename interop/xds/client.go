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

package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc"
	_ "google.golang.org/grpc/balancer/grpclb"
	"google.golang.org/grpc/grpclog"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/peer"
)

type statsWatcherKey struct {
	startId int32
	endId   int32
}

type statsWatcher struct {
	rpcsByPeer    map[string]int32
	numFailures   int32
	remainingRpcs int32
	lock          sync.Mutex
}

var (
	dialTimeout   = flag.Duration("dialTimeout", 10*time.Second, "timeout for creating grpc.ClientConn")
	numChannels   = flag.Int("num_channels", 1, "Num of channels")
	printResponse = flag.Bool("print_response", false, "Write RPC response to stdout")
	qps           = flag.Int("qps", 1, "QPS (across all channels)")
	rpcTimeout    = flag.Duration("rpc_timeout", 2*time.Second, "Per RPC timeout")
	server        = flag.String("server", "localhost:8080", "Address of server to connect to")
	statsPort     = flag.Int("stats_port", 8081, "Port to expose peer distribution stats service")

	currentRequestId int32 = 0
	lock             sync.Mutex
	watchers         = make(map[statsWatcherKey]*statsWatcher)
)

type statsService struct{}

func (s *statsService) GetClientStats(ctx context.Context, in *testpb.LoadBalancerStatsRequest) (*testpb.LoadBalancerStatsResponse, error) {
	lock.Lock()
	watcherKey := statsWatcherKey{currentRequestId, currentRequestId + in.GetNumRpcs()}
	var watcher *statsWatcher
	_, exists := watchers[watcherKey]
	if exists {
		watcher = watchers[watcherKey]
	} else {
		watcher = &statsWatcher{make(map[string]int32), 0, in.GetNumRpcs(), sync.Mutex{}}
		watchers[watcherKey] = watcher
	}
	lock.Unlock()

	ctx, cancel := context.WithTimeout(ctx, time.Duration(in.GetTimeoutSec())*time.Second)
	defer cancel()

	done := make(chan bool)
	go func() {
		for {
			select {
			default:
				watcher.lock.Lock()
				remainingRpcs := watcher.remainingRpcs
				watcher.lock.Unlock()
				if remainingRpcs == 0 {
					done <- true
					close(done)
					return
				}
			case <-ctx.Done():
				done <- false
				close(done)
				return
			}
		}
	}()

	success := <-done
	if !success {
		grpclog.Info("Timed out, returning partial stats")
	}
	lock.Lock()
	delete(watchers, watcherKey)
	lock.Unlock()
	watcher.lock.Lock()
	defer watcher.lock.Unlock()
	return &testpb.LoadBalancerStatsResponse{NumFailures: watcher.numFailures + watcher.remainingRpcs, RpcsByPeer: watcher.rpcsByPeer}, nil
}

func main() {
	flag.Parse()

	p := strconv.Itoa(*statsPort)
	lis, err := net.Listen("tcp", ":"+p)
	if err != nil {
		grpclog.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	testpb.RegisterLoadBalancerStatsServiceServer(s, &statsService{})
	go s.Serve(lis)

	ctx, cancel := context.WithTimeout(context.Background(), *dialTimeout)
	defer cancel()
	clients := make([]testpb.TestServiceClient, *numChannels)
	for i := 0; i < *numChannels; i++ {
		conn, err := grpc.DialContext(ctx, *server, grpc.WithInsecure(), grpc.WithBlock())
		if err != nil {
			grpclog.Fatalf("Fail to dial: %v", err)
		}
		defer conn.Close()
		clients[i] = testpb.NewTestServiceClient(conn)
	}
	ticker := time.NewTicker(time.Second / time.Duration(*qps))
	defer ticker.Stop()
	sendRpcs(clients, ticker)
}

func sendRpcs(clients []testpb.TestServiceClient, ticker *time.Ticker) {
	var i int
	for range ticker.C {
		go func(i int) {
			c := clients[i%len(clients)]
			ctx, cancel := context.WithTimeout(context.Background(), *rpcTimeout)
			p := new(peer.Peer)
			lock.Lock()
			savedRequestId := currentRequestId
			currentRequestId += 1
			savedWatchers := make(map[statsWatcherKey]*statsWatcher)
			for key, value := range watchers {
				savedWatchers[key] = value
			}
			lock.Unlock()
			r, err := c.UnaryCall(ctx, &testpb.SimpleRequest{FillServerId: true}, grpc.Peer(p))

			success := err == nil
			cancel()

			for key, value := range savedWatchers {
				value.lock.Lock()
				if key.startId <= savedRequestId && savedRequestId < key.endId {
					if success {
						value.rpcsByPeer[r.GetServerId()] += 1
					} else {
						value.numFailures += 1
					}
					value.remainingRpcs -= 1
				}
				value.lock.Unlock()
			}

			if success && *printResponse {
				fmt.Printf("Greeting: Hello world, this is %s, from %v\n", r.GetHostname(), p.Addr)
			}
		}(i)
		i++
	}
}
