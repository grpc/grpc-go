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
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	_ "google.golang.org/grpc/xds"
)

type statsWatcherKey struct {
	startID int32
	endID   int32
}

// rpcInfo contains the rpc type and the hostname where the response is received
// from.
type rpcInfo struct {
	typ      string
	hostname string
}

type statsWatcher struct {
	rpcsByPeer    map[string]int32
	rpcsByType    map[string]map[string]int32
	numFailures   int32
	remainingRpcs int32
	chanHosts     chan *rpcInfo
}

func (watcher *statsWatcher) buildResp() *testpb.LoadBalancerStatsResponse {
	rpcsByType := make(map[string]*testpb.LoadBalancerStatsResponse_RpcsByPeer, len(watcher.rpcsByType))
	for t, rpcsByPeer := range watcher.rpcsByType {
		rpcsByType[t] = &testpb.LoadBalancerStatsResponse_RpcsByPeer{
			RpcsByPeer: rpcsByPeer,
		}
	}

	return &testpb.LoadBalancerStatsResponse{
		NumFailures:  watcher.numFailures + watcher.remainingRpcs,
		RpcsByPeer:   watcher.rpcsByPeer,
		RpcsByMethod: rpcsByType,
	}
}

var (
	failOnFailedRPC = flag.Bool("fail_on_failed_rpc", false, "Fail client if any RPCs fail after first success")
	numChannels     = flag.Int("num_channels", 1, "Num of channels")
	printResponse   = flag.Bool("print_response", false, "Write RPC response to stdout")
	qps             = flag.Int("qps", 1, "QPS per channel, for each type of RPC")
	rpc             = flag.String("rpc", "UnaryCall", "Types of RPCs to make, ',' separated string. RPCs can be EmptyCall or UnaryCall")
	rpcMetadata     = flag.String("metadata", "", "The metadata to send with RPC, in format EmptyCall:key1:value1,UnaryCall:key2:value2")
	rpcTimeout      = flag.Duration("rpc_timeout", 20*time.Second, "Per RPC timeout")
	server          = flag.String("server", "localhost:8080", "Address of server to connect to")
	statsPort       = flag.Int("stats_port", 8081, "Port to expose peer distribution stats service")

	mu               sync.Mutex
	currentRequestID int32
	watchers         = make(map[statsWatcherKey]*statsWatcher)

	// 0 or 1 representing an RPC has succeeded. Use hasRPCSucceeded and
	// setRPCSucceeded to access in a safe manner.
	rpcSucceeded uint32

	logger = grpclog.Component("interop")
)

type statsService struct {
	testpb.UnimplementedLoadBalancerStatsServiceServer
}

func hasRPCSucceeded() bool {
	return atomic.LoadUint32(&rpcSucceeded) > 0
}

func setRPCSucceeded() {
	atomic.StoreUint32(&rpcSucceeded, 1)
}

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
			rpcsByType:    make(map[string]map[string]int32),
			numFailures:   0,
			remainingRpcs: in.GetNumRpcs(),
			chanHosts:     make(chan *rpcInfo),
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
		case info := <-watcher.chanHosts:
			if info != nil {
				watcher.rpcsByPeer[info.hostname]++

				rpcsByPeerForType := watcher.rpcsByType[info.typ]
				if rpcsByPeerForType == nil {
					rpcsByPeerForType = make(map[string]int32)
					watcher.rpcsByType[info.typ] = rpcsByPeerForType
				}
				rpcsByPeerForType[info.hostname]++
			} else {
				watcher.numFailures++
			}
			watcher.remainingRpcs--
			if watcher.remainingRpcs == 0 {
				return watcher.buildResp(), nil
			}
		case <-ctx.Done():
			logger.Info("Timed out, returning partial stats")
			return watcher.buildResp(), nil
		}
	}
}

const (
	unaryCall string = "UnaryCall"
	emptyCall string = "EmptyCall"
)

func parseRPCTypes(rpcStr string) (ret []string) {
	if len(rpcStr) == 0 {
		return []string{unaryCall}
	}

	rpcs := strings.Split(rpcStr, ",")
	for _, r := range rpcs {
		switch r {
		case unaryCall, emptyCall:
			ret = append(ret, r)
		default:
			flag.PrintDefaults()
			log.Fatalf("unsupported RPC type: %v", r)
		}
	}
	return
}

type rpcConfig struct {
	typ string
	md  metadata.MD
}

// parseRPCMetadata turns EmptyCall:key1:value1 into
//   {typ: emptyCall, md: {key1:value1}}.
func parseRPCMetadata(rpcMetadataStr string, rpcs []string) []*rpcConfig {
	rpcMetadataSplit := strings.Split(rpcMetadataStr, ",")
	rpcsToMD := make(map[string][]string)
	for _, rm := range rpcMetadataSplit {
		rmSplit := strings.Split(rm, ":")
		if len(rmSplit)%2 != 1 {
			log.Fatalf("invalid metadata config %v, want EmptyCall:key1:value1", rm)
		}
		rpcsToMD[rmSplit[0]] = append(rpcsToMD[rmSplit[0]], rmSplit[1:]...)
	}
	ret := make([]*rpcConfig, 0, len(rpcs))
	for _, rpcT := range rpcs {
		rpcC := &rpcConfig{
			typ: rpcT,
		}
		if md := rpcsToMD[string(rpcT)]; len(md) > 0 {
			rpcC.md = metadata.Pairs(md...)
		}
		ret = append(ret, rpcC)
	}
	return ret
}

func main() {
	flag.Parse()
	rpcCfgs := parseRPCMetadata(*rpcMetadata, parseRPCTypes(*rpc))

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *statsPort))
	if err != nil {
		logger.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	defer s.Stop()
	testpb.RegisterLoadBalancerStatsServiceServer(s, &statsService{})
	go s.Serve(lis)

	clients := make([]testpb.TestServiceClient, *numChannels)
	for i := 0; i < *numChannels; i++ {
		conn, err := grpc.DialContext(context.Background(), *server, grpc.WithInsecure())
		if err != nil {
			logger.Fatalf("Fail to dial: %v", err)
		}
		defer conn.Close()
		clients[i] = testpb.NewTestServiceClient(conn)
	}
	ticker := time.NewTicker(time.Second / time.Duration(*qps**numChannels))
	defer ticker.Stop()
	sendRPCs(clients, rpcCfgs, ticker)
}

func makeOneRPC(c testpb.TestServiceClient, cfg *rpcConfig) (*peer.Peer, *rpcInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), *rpcTimeout)
	defer cancel()

	if len(cfg.md) != 0 {
		ctx = metadata.NewOutgoingContext(ctx, cfg.md)
	}
	info := rpcInfo{typ: cfg.typ}

	var (
		p      peer.Peer
		header metadata.MD
		err    error
	)
	switch cfg.typ {
	case unaryCall:
		var resp *testpb.SimpleResponse
		resp, err = c.UnaryCall(ctx, &testpb.SimpleRequest{FillServerId: true}, grpc.Peer(&p), grpc.Header(&header))
		// For UnaryCall, also read hostname from response, in case the server
		// isn't updated to send headers.
		if resp != nil {
			info.hostname = resp.Hostname
		}
	case emptyCall:
		_, err = c.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&p), grpc.Header(&header))
	}
	if err != nil {
		return nil, nil, err
	}

	hosts := header["hostname"]
	if len(hosts) > 0 {
		info.hostname = hosts[0]
	}
	return &p, &info, err
}

func sendRPCs(clients []testpb.TestServiceClient, cfgs []*rpcConfig, ticker *time.Ticker) {
	var i int
	for range ticker.C {
		go func(i int) {
			// Get and increment request ID, and save a list of watchers that
			// are interested in this RPC.
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

			c := clients[i]

			for _, cfg := range cfgs {
				p, info, err := makeOneRPC(c, cfg)

				for _, watcher := range savedWatchers {
					// This sends an empty string if the RPC failed.
					watcher.chanHosts <- info
				}
				if err != nil && *failOnFailedRPC && hasRPCSucceeded() {
					logger.Fatalf("RPC failed: %v", err)
				}
				if err == nil {
					setRPCSucceeded()
				}
				if *printResponse {
					if err == nil {
						if cfg.typ == unaryCall {
							// Need to keep this format, because some tests are
							// relying on stdout.
							fmt.Printf("Greeting: Hello world, this is %s, from %v\n", info.hostname, p.Addr)
						} else {
							fmt.Printf("RPC %q, from host %s, addr %v\n", cfg.typ, info.hostname, p.Addr)
						}
					} else {
						fmt.Printf("RPC %q, failed with %v\n", cfg.typ, err)
					}
				}
			}
		}(i)
		i = (i + 1) % len(clients)
	}
}
