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

// Package leastrequest implements a least request load balancer.
package leastrequest

import (
	"encoding/json"
	"fmt"
	rand "math/rand/v2"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/endpointsharding"
	"google.golang.org/grpc/balancer/pickfirst/pickfirstleaf"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

// Name is the name of the least request balancer.
const Name = "least_request_experimental"

var (
	// randuint32 is a global to stub out in tests.
	randuint32               = rand.Uint32
	endpointShardingLBConfig serviceconfig.LoadBalancingConfig
	logger                   = grpclog.Component("least-request")
)

func init() {
	var err error
	endpointShardingLBConfig, err = endpointsharding.ParseConfig(json.RawMessage(endpointsharding.PickFirstConfig))
	if err != nil {
		logger.Fatal(err)
	}
	balancer.Register(bb{})
}

// LBConfig is the balancer config for least_request_experimental balancer.
type LBConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	// ChoiceCount is the number of random SubConns to sample to find the one
	// with the fewest outstanding requests. If unset, defaults to 2. If set to
	// < 2, the config will be rejected, and if set to > 10, will become 10.
	ChoiceCount uint32 `json:"choiceCount,omitempty"`
}

type bb struct{}

func (bb) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	lbConfig := &LBConfig{
		ChoiceCount: 2,
	}
	if err := json.Unmarshal(s, lbConfig); err != nil {
		return nil, fmt.Errorf("least-request: unable to unmarshal LBConfig: %v", err)
	}
	// "If `choice_count < 2`, the config will be rejected." - A48
	if lbConfig.ChoiceCount < 2 { // sweet
		return nil, fmt.Errorf("least-request: lbConfig.choiceCount: %v, must be >= 2", lbConfig.ChoiceCount)
	}
	// "If a LeastRequestLoadBalancingConfig with a choice_count > 10 is
	// received, the least_request_experimental policy will set choice_count =
	// 10." - A48
	if lbConfig.ChoiceCount > 10 {
		lbConfig.ChoiceCount = 10
	}
	return lbConfig, nil
}

func (bb) Name() string {
	return Name
}

func (bb) Build(cc balancer.ClientConn, bOpts balancer.BuildOptions) balancer.Balancer {
	b := &leastRequestBalancer{
		ClientConn:        cc,
		endpointRPCCounts: resolver.NewEndpointMap(),
		choiceCount:       2,
	}
	b.child = endpointsharding.NewBalancer(b, bOpts)
	b.logger = internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf("[%p] ", b))
	b.logger.Infof("Created")
	return b
}

type leastRequestBalancer struct {
	// Embeds balancer.ClientConn because needs to intercept UpdateState calls
	// from the child balancer.
	balancer.ClientConn
	child  balancer.Balancer
	logger *internalgrpclog.PrefixLogger

	mu          sync.Mutex
	choiceCount uint32
	// endpointRPCCounts holds  RPC counts to keep track for subsequent picker
	// updates.
	endpointRPCCounts *resolver.EndpointMap // endpoint -> *atomic.Int32
}

// Close implements balancer.Balancer.
func (lrb *leastRequestBalancer) Close() {
	lrb.child.Close()
	lrb.endpointRPCCounts = nil
}

// ResolverError implements balancer.Balancer.
func (lrb *leastRequestBalancer) ResolverError(err error) {
	lrb.child.ResolverError(err)
}

// UpdateSubConnState implements balancer.Balancer.
func (lrb *leastRequestBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	lrb.logger.Errorf("UpdateSubConnState(%v, %+v) called unexpectedly", sc, state)
}

func (lrb *leastRequestBalancer) UpdateClientConnState(ccs balancer.ClientConnState) error {
	lrCfg, ok := ccs.BalancerConfig.(*LBConfig)
	if !ok {
		logger.Errorf("least-request: received config with unexpected type %T: %v", ccs.BalancerConfig, ccs.BalancerConfig)
		return balancer.ErrBadResolverState
	}

	lrb.mu.Lock()
	lrb.choiceCount = lrCfg.ChoiceCount
	lrb.mu.Unlock()
	// Enable the health listener in pickfirst children for client side health
	// checks and outlier detection, if configured.
	ccs.ResolverState = pickfirstleaf.EnableHealthListener(ccs.ResolverState)
	ccs.BalancerConfig = endpointShardingLBConfig
	return lrb.child.UpdateClientConnState(ccs)
}

type endpointState struct {
	picker  balancer.Picker
	numRPCs *atomic.Int32
}

func (lrb *leastRequestBalancer) UpdateState(state balancer.State) {
	var readyEndpoints []endpointsharding.ChildState
	for _, child := range endpointsharding.ChildStatesFromPicker(state.Picker) {
		if child.State.ConnectivityState == connectivity.Ready {
			readyEndpoints = append(readyEndpoints, child)
		}
	}

	// If no ready pickers are present, simply defer to the round robin picker
	// from endpoint sharding, which will round robin across the most relevant
	// pick first children in the highest precedence connectivity state.
	if len(readyEndpoints) == 0 {
		lrb.ClientConn.UpdateState(state)
		return
	}

	lrb.mu.Lock()
	defer lrb.mu.Unlock()

	if logger.V(2) {
		lrb.logger.Infof("UpdateState called with ready endpoints: %v", readyEndpoints)
	}

	// Reconcile endpoints.
	newEndpoints := resolver.NewEndpointMap() // endpoint -> nil
	for _, child := range readyEndpoints {
		newEndpoints.Set(child.Endpoint, nil)
	}

	// If endpoints are no longer ready, no need to count their active RPCs.
	for _, endpoint := range lrb.endpointRPCCounts.Keys() {
		if _, ok := newEndpoints.Get(endpoint); !ok {
			lrb.endpointRPCCounts.Delete(endpoint)
		}
	}

	// Copy refs to counters into picker.
	endpointStates := make([]endpointState, 0, len(readyEndpoints))
	for _, child := range readyEndpoints {
		var counter *atomic.Int32
		if val, ok := lrb.endpointRPCCounts.Get(child.Endpoint); !ok {
			// Create new counts if needed.
			counter = new(atomic.Int32)
			lrb.endpointRPCCounts.Set(child.Endpoint, counter)
		} else {
			counter = val.(*atomic.Int32)
		}
		endpointStates = append(endpointStates, endpointState{
			picker:  child.State.Picker,
			numRPCs: counter,
		})
	}

	lrb.ClientConn.UpdateState(balancer.State{
		Picker: &picker{
			choiceCount:    lrb.choiceCount,
			endpointStates: endpointStates,
		},
		ConnectivityState: connectivity.Ready,
	})
}

type picker struct {
	// choiceCount is the number of random SubConns to find the one with
	// the least request.
	choiceCount uint32
	// Built out when receives list of ready child pickers.
	endpointStates []endpointState
}

func (p *picker) Pick(pInfo balancer.PickInfo) (balancer.PickResult, error) {
	var pickedEndpointState *endpointState
	var pickedEndpointNumRPCs int32
	for i := 0; i < int(p.choiceCount); i++ {
		index := randuint32() % uint32(len(p.endpointStates))
		endpointState := p.endpointStates[index]
		n := endpointState.numRPCs.Load()
		if pickedEndpointState == nil || n < pickedEndpointNumRPCs {
			pickedEndpointState = &endpointState
			pickedEndpointNumRPCs = n
		}
	}
	result, err := pickedEndpointState.picker.Pick(pInfo)
	if err != nil {
		return result, err
	}
	// "The counter for a subchannel should be atomically incremented by one
	// after it has been successfully picked by the picker." - A48
	pickedEndpointState.numRPCs.Add(1)
	// "the picker should add a callback for atomically decrementing the
	// subchannel counter once the RPC finishes (regardless of Status code)." -
	// A48.
	originalDone := result.Done
	result.Done = func(info balancer.DoneInfo) {
		pickedEndpointState.numRPCs.Add(-1)
		if originalDone != nil {
			originalDone(info)
		}
	}
	return result, nil
}
