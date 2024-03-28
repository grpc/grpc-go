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

package customroundrobin

import (
	"encoding/json"
	"fmt"
	"sync/atomic"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/balancer/balanceraggregator"
	"google.golang.org/grpc/serviceconfig"
)

func init() {
	balancer.Register(customRoundRobinBuilder{})
}

const customRRName = "custom_round_robin"

type customRRConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	// ChildPolicy is the child policy of this balancer. This will be hardcoded
	// to a graceful switch config which wraps a pick first with no shuffling
	// enabled.
	ChildPolicy serviceconfig.LoadBalancingConfig `json:"childPolicy"`

	// ChooseSecond represents how often pick iterations choose the second
	// SubConn in the list. Defaults to 3. If 0 never choose the second SubConn.
	ChooseSecond uint32 `json:"chooseSecond,omitempty"`
}

type customRoundRobinBuilder struct{}

func (customRoundRobinBuilder) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	// Hardcode a pick first with no shuffling, since this is a petiole, and
	// that is what petiole policies will interact with.
	gspf, err := balanceraggregator.ParseConfig(json.RawMessage(balanceraggregator.PickFirstConfig))
	if err != nil {
		return nil, fmt.Errorf("error parsing hardcoded pick_first config: %v", err)
	}
	lbConfig := &customRRConfig{
		ChooseSecond: 3,
		ChildPolicy:  gspf,
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
	crr.balancerAggregator = balanceraggregator.Build(crr, bOpts)
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
	balancer.ClientConn
	bOpts balancer.BuildOptions

	// hardcoded child config, graceful switch that wraps a pick_first with no
	// shuffling enabled.
	balancerAggregator balancer.Balancer

	cfg *customRRConfig

	// InhibitPickerUpdates determines whether picker updates from the child
	// forward to cc or not.
	inhibitPickerUpdates bool
}

func (crr *customRoundRobin) UpdateClientConnState(state balancer.ClientConnState) error {
	crrCfg, ok := state.BalancerConfig.(*customRRConfig)
	if !ok {
		return balancer.ErrBadResolverState
	}
	crr.cfg = crrCfg
	return crr.balancerAggregator.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: crrCfg.ChildPolicy,
		ResolverState:  state.ResolverState,
	})
}

func (crr *customRoundRobin) ResolverError(err error) {
	crr.balancerAggregator.ResolverError(err)
}

// This function is deprecated. SubConn state updates now come through listener
// callbacks. This balancer does not deal with SubConns directly and has no need
// to intercept listener callbacks.
func (crr *customRoundRobin) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	logger.Errorf("custom_round_robin: UpdateSubConnState(%v, %+v) called unexpectedly", sc, state)
}

func (crr *customRoundRobin) Close() {
	crr.balancerAggregator.Close()
}

// regeneratePicker generates a picker if both child balancers are READY and
// forwards it upward.
func (crr *customRoundRobin) UpdateChildState(childStates []balanceraggregator.ChildState) {
	var readyPickers []balancer.Picker
	for _, childState := range childStates {
		if childState.State.ConnectivityState == connectivity.Ready {
			readyPickers = append(readyPickers, childState.State.Picker)
		}
	}

	// For determinism, this balancer only updates it's picker when both
	// backends of the example are ready. Thus, no need to keep track of
	// aggregated state and can simply specify this balancer is READY once it
	// has two ready children. Other balancers can keep track of aggregated
	// state and interact with errors as part of picker.
	if len(readyPickers) != 2 {
		return
	}
	picker := &customRoundRobinPicker{
		pickers:      readyPickers,
		chooseSecond: crr.cfg.ChooseSecond,
		next:         0,
	}
	crr.ClientConn.UpdateState(balancer.State{
		ConnectivityState: connectivity.Ready,
		Picker:            picker,
	})
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
