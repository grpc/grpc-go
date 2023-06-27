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
	"google.golang.org/grpc/admin"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	_ "google.golang.org/grpc/xds"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	_ "google.golang.org/grpc/interop/xds" // to register Custom LB.
)

func init() {
	rpcCfgs.Store([]*rpcConfig{{typ: unaryCall}})
}

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
	remainingRPCs int32
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
		NumFailures:  watcher.numFailures + watcher.remainingRPCs,
		RpcsByPeer:   watcher.rpcsByPeer,
		RpcsByMethod: rpcsByType,
	}
}

type accumulatedStats struct {
	mu                       sync.Mutex
	numRPCsStartedByMethod   map[string]int32
	numRPCsSucceededByMethod map[string]int32
	numRPCsFailedByMethod    map[string]int32
	rpcStatusByMethod        map[string]map[int32]int32
}

func convertRPCName(in string) string {
	switch in {
	case unaryCall:
		return testpb.ClientConfigureRequest_UNARY_CALL.String()
	case emptyCall:
		return testpb.ClientConfigureRequest_EMPTY_CALL.String()
	}
	logger.Warningf("unrecognized rpc type: %s", in)
	return in
}

// copyStatsMap makes a copy of the map.
func copyStatsMap(originalMap map[string]int32) map[string]int32 {
	newMap := make(map[string]int32, len(originalMap))
	for k, v := range originalMap {
		newMap[k] = v
	}
	return newMap
}

// copyStatsIntMap makes a copy of the map.
func copyStatsIntMap(originalMap map[int32]int32) map[int32]int32 {
	newMap := make(map[int32]int32, len(originalMap))
	for k, v := range originalMap {
		newMap[k] = v
	}
	return newMap
}

func (as *accumulatedStats) makeStatsMap() map[string]*testpb.LoadBalancerAccumulatedStatsResponse_MethodStats {
	m := make(map[string]*testpb.LoadBalancerAccumulatedStatsResponse_MethodStats)
	for k, v := range as.numRPCsStartedByMethod {
		m[k] = &testpb.LoadBalancerAccumulatedStatsResponse_MethodStats{RpcsStarted: v}
	}
	for k, v := range as.rpcStatusByMethod {
		if m[k] == nil {
			m[k] = &testpb.LoadBalancerAccumulatedStatsResponse_MethodStats{}
		}
		m[k].Result = copyStatsIntMap(v)
	}
	return m
}

func (as *accumulatedStats) buildResp() *testpb.LoadBalancerAccumulatedStatsResponse {
	as.mu.Lock()
	defer as.mu.Unlock()
	return &testpb.LoadBalancerAccumulatedStatsResponse{
		NumRpcsStartedByMethod:   copyStatsMap(as.numRPCsStartedByMethod),
		NumRpcsSucceededByMethod: copyStatsMap(as.numRPCsSucceededByMethod),
		NumRpcsFailedByMethod:    copyStatsMap(as.numRPCsFailedByMethod),
		StatsPerMethod:           as.makeStatsMap(),
	}
}

func (as *accumulatedStats) startRPC(rpcType string) {
	as.mu.Lock()
	defer as.mu.Unlock()
	as.numRPCsStartedByMethod[convertRPCName(rpcType)]++
}

func (as *accumulatedStats) finishRPC(rpcType string, err error) {
	as.mu.Lock()
	defer as.mu.Unlock()
	name := convertRPCName(rpcType)
	if as.rpcStatusByMethod[name] == nil {
		as.rpcStatusByMethod[name] = make(map[int32]int32)
	}
	as.rpcStatusByMethod[name][int32(status.Convert(err).Code())]++
	if err != nil {
		as.numRPCsFailedByMethod[name]++
		return
	}
	as.numRPCsSucceededByMethod[name]++
}

var (
	failOnFailedRPC = flag.Bool("fail_on_failed_rpc", false, "Fail client if any RPCs fail after first success")
	numChannels     = flag.Int("num_channels", 1, "Num of channels")
	printResponse   = flag.Bool("print_response", false, "Write RPC response to stdout")
	qps             = flag.Int("qps", 1, "QPS per channel, for each type of RPC")
	rpc             = flag.String("rpc", "UnaryCall", "Types of RPCs to make, ',' separated string. RPCs can be EmptyCall or UnaryCall. Deprecated: Use Configure RPC to XdsUpdateClientConfigureServiceServer instead.")
	rpcMetadata     = flag.String("metadata", "", "The metadata to send with RPC, in format EmptyCall:key1:value1,UnaryCall:key2:value2. Deprecated: Use Configure RPC to XdsUpdateClientConfigureServiceServer instead.")
	rpcTimeout      = flag.Duration("rpc_timeout", 20*time.Second, "Per RPC timeout")
	server          = flag.String("server", "localhost:8080", "Address of server to connect to")
	statsPort       = flag.Int("stats_port", 8081, "Port to expose peer distribution stats service")
	secureMode      = flag.Bool("secure_mode", false, "If true, retrieve security configuration from the management server. Else, use insecure credentials.")

	rpcCfgs atomic.Value

	mu               sync.Mutex
	currentRequestID int32
	watchers         = make(map[statsWatcherKey]*statsWatcher)

	accStats = accumulatedStats{
		numRPCsStartedByMethod:   make(map[string]int32),
		numRPCsSucceededByMethod: make(map[string]int32),
		numRPCsFailedByMethod:    make(map[string]int32),
		rpcStatusByMethod:        make(map[string]map[int32]int32),
	}

	// 0 or 1 representing an RPC has succeeded. Use hasRPCSucceeded and
	// setRPCSucceeded to access in a safe manner.
	rpcSucceeded uint32

	logger = grpclog.Component("interop")
)

type statsService struct {
	testgrpc.UnimplementedLoadBalancerStatsServiceServer
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
			remainingRPCs: in.GetNumRpcs(),
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
			watcher.remainingRPCs--
			if watcher.remainingRPCs == 0 {
				return watcher.buildResp(), nil
			}
		case <-ctx.Done():
			logger.Info("Timed out, returning partial stats")
			return watcher.buildResp(), nil
		}
	}
}

func (s *statsService) GetClientAccumulatedStats(ctx context.Context, in *testpb.LoadBalancerAccumulatedStatsRequest) (*testpb.LoadBalancerAccumulatedStatsResponse, error) {
	return accStats.buildResp(), nil
}

type configureService struct {
	testgrpc.UnimplementedXdsUpdateClientConfigureServiceServer
}

func (s *configureService) Configure(ctx context.Context, in *testpb.ClientConfigureRequest) (*testpb.ClientConfigureResponse, error) {
	rpcsToMD := make(map[testpb.ClientConfigureRequest_RpcType][]string)
	for _, typ := range in.GetTypes() {
		rpcsToMD[typ] = nil
	}
	for _, md := range in.GetMetadata() {
		typ := md.GetType()
		strs, ok := rpcsToMD[typ]
		if !ok {
			continue
		}
		rpcsToMD[typ] = append(strs, md.GetKey(), md.GetValue())
	}
	cfgs := make([]*rpcConfig, 0, len(rpcsToMD))
	for typ, md := range rpcsToMD {
		var rpcType string
		switch typ {
		case testpb.ClientConfigureRequest_UNARY_CALL:
			rpcType = unaryCall
		case testpb.ClientConfigureRequest_EMPTY_CALL:
			rpcType = emptyCall
		default:
			return nil, fmt.Errorf("unsupported RPC type: %v", typ)
		}
		cfgs = append(cfgs, &rpcConfig{
			typ:     rpcType,
			md:      metadata.Pairs(md...),
			timeout: in.GetTimeoutSec(),
		})
	}
	rpcCfgs.Store(cfgs)
	return &testpb.ClientConfigureResponse{}, nil
}

const (
	unaryCall string = "UnaryCall"
	emptyCall string = "EmptyCall"
)

func parseRPCTypes(rpcStr string) []string {
	if len(rpcStr) == 0 {
		return []string{unaryCall}
	}

	rpcs := strings.Split(rpcStr, ",")
	ret := make([]string, 0, len(rpcStr))
	for _, r := range rpcs {
		switch r {
		case unaryCall, emptyCall:
			ret = append(ret, r)
		default:
			flag.PrintDefaults()
			log.Fatalf("unsupported RPC type: %v", r)
		}
	}
	return ret
}

type rpcConfig struct {
	typ     string
	md      metadata.MD
	timeout int32
}

// parseRPCMetadata turns EmptyCall:key1:value1 into
//
//	{typ: emptyCall, md: {key1:value1}}.
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
	rpcCfgs.Store(parseRPCMetadata(*rpcMetadata, parseRPCTypes(*rpc)))

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *statsPort))
	if err != nil {
		logger.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	defer s.Stop()
	testgrpc.RegisterLoadBalancerStatsServiceServer(s, &statsService{})
	testgrpc.RegisterXdsUpdateClientConfigureServiceServer(s, &configureService{})
	reflection.Register(s)
	cleanup, err := admin.Register(s)
	if err != nil {
		logger.Fatalf("Failed to register admin: %v", err)
	}
	defer cleanup()
	go s.Serve(lis)

	creds := insecure.NewCredentials()
	if *secureMode {
		var err error
		creds, err = xds.NewClientCredentials(xds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
		if err != nil {
			logger.Fatalf("Failed to create xDS credentials: %v", err)
		}
	}

	clients := make([]testgrpc.TestServiceClient, *numChannels)
	for i := 0; i < *numChannels; i++ {
		conn, err := grpc.Dial(*server, grpc.WithTransportCredentials(creds))
		if err != nil {
			logger.Fatalf("Fail to dial: %v", err)
		}
		defer conn.Close()
		clients[i] = testgrpc.NewTestServiceClient(conn)
	}
	ticker := time.NewTicker(time.Second / time.Duration(*qps**numChannels))
	defer ticker.Stop()
	sendRPCs(clients, ticker)
}

func makeOneRPC(c testgrpc.TestServiceClient, cfg *rpcConfig) (*peer.Peer, *rpcInfo, error) {
	timeout := *rpcTimeout
	if cfg.timeout != 0 {
		timeout = time.Duration(cfg.timeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
	accStats.startRPC(cfg.typ)
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
	accStats.finishRPC(cfg.typ, err)
	if err != nil {
		return nil, nil, err
	}

	hosts := header["hostname"]
	if len(hosts) > 0 {
		info.hostname = hosts[0]
	}
	return &p, &info, err
}

func sendRPCs(clients []testgrpc.TestServiceClient, ticker *time.Ticker) {
	var i int
	for range ticker.C {
		// Get and increment request ID, and save a list of watchers that are
		// interested in this RPC.
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

		// Get the RPC metadata configurations from the Configure RPC.
		cfgs := rpcCfgs.Load().([]*rpcConfig)

		c := clients[i]
		for _, cfg := range cfgs {
			go func(cfg *rpcConfig) {
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
			}(cfg)
		}
		i = (i + 1) % len(clients)
	}
}
