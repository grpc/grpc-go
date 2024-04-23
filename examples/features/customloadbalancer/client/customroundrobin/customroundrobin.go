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

// Package customroundrobin provides an example for the custom roundrobin balancer.
package customroundrobin

import (
	"encoding/json"
	"fmt"
	"sync/atomic"

	_ "google.golang.org/grpc" // to register pick_first
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/endpointsharding"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/serviceconfig"
)

var gracefulSwitchPickFirst serviceconfig.LoadBalancingConfig

func init() {
	balancer.Register(customRoundRobinBuilder{})
	var err error
	gracefulSwitchPickFirst, err = endpointsharding.ParseConfig(json.RawMessage(endpointsharding.PickFirstConfig))
	if err != nil {
		logger.Fatal(err)
	}
}

const customRRName = "custom_round_robin"

type customRRConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	// ChooseSecond represents how often pick iterations choose the second
	// SubConn in the list. Defaults to 3. If 0 never choose the second SubConn.
	ChooseSecond uint32 `json:"chooseSecond,omitempty"`
}

type customRoundRobinBuilder struct{}

func (customRoundRobinBuilder) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	lbConfig := &customRRConfig{
		ChooseSecond: 3,
	}

	if err := json.Unmarshal(s, lbConfig); err != nil {
		return nil, fmt.Errorf("custom-round-robin: unable to unmarshal customRRConfig: %v", err)
	}
	return lbConfig, nil
}

func (customRoundRobinBuilder) Name() string {
	return customRRName
}

func (customRoundRobinBuilder) Build(cc balancer.ClientConn, bOpts balancer.BuildOptions) balancer.Balancer {
	crr := &customRoundRobin{
		ClientConn: cc,
		bOpts:      bOpts,
	}
	crr.Balancer = endpointsharding.NewBalancer(crr, bOpts)
	return crr
}

var logger = grpclog.Component("example")

type customRoundRobin struct {
	// All state and operations on this balancer are either initialized at build
	// time and read only after, or are only accessed as part of its
	// balancer.Balancer API (UpdateState from children only comes in from
	// balancer.Balancer calls as well, and children are called one at a time),
	// in which calls are guaranteed to come synchronously. Thus, no extra
	// synchronization is required in this balancer.
	balancer.Balancer
	balancer.ClientConn
	bOpts balancer.BuildOptions

	cfg atomic.Pointer[customRRConfig]
}

func (crr *customRoundRobin) UpdateClientConnState(state balancer.ClientConnState) error {
	crrCfg, ok := state.BalancerConfig.(*customRRConfig)
	if !ok {
		return balancer.ErrBadResolverState
	}
	if el := state.ResolverState.Endpoints; len(el) != 2 {
		return fmt.Errorf("UpdateClientConnState wants two endpoints, got: %v", el)
	}
	crr.cfg.Store(crrCfg)
	// A call to UpdateClientConnState should always produce a new Picker.  That
	// is guaranteed to happen since the aggregator will always call
	// UpdateChildState in its UpdateClientConnState.
	return crr.Balancer.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: gracefulSwitchPickFirst,
		ResolverState:  state.ResolverState,
	})
}

func (crr *customRoundRobin) UpdateState(state balancer.State) {
	if state.ConnectivityState == connectivity.Ready {
		childStates := endpointsharding.ChildStatesFromPicker(state.Picker)
		var readyPickers []balancer.Picker
		for _, childState := range childStates {
			if childState.State.ConnectivityState == connectivity.Ready {
				readyPickers = append(readyPickers, childState.State.Picker)
			}
		}
		// If both children are ready, pick using the custom round robin
		// algorithm.
		if len(readyPickers) == 2 {
			picker := &customRoundRobinPicker{
				pickers:      readyPickers,
				chooseSecond: crr.cfg.Load().ChooseSecond,
				next:         0,
			}
			crr.ClientConn.UpdateState(balancer.State{
				ConnectivityState: connectivity.Ready,
				Picker:            picker,
			})
			return
		}
	}
	// Delegate to default behavior/picker from below.
	crr.ClientConn.UpdateState(state)
}

type customRoundRobinPicker struct {
	pickers      []balancer.Picker
	chooseSecond uint32
	next         uint32
}

func (crrp *customRoundRobinPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	next := atomic.AddUint32(&crrp.next, 1)
	index := 0
	if next != 0 && next%crrp.chooseSecond == 0 {
		index = 1
	}
	childPicker := crrp.pickers[index%len(crrp.pickers)]
	return childPicker.Pick(info)
}
