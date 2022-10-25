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

// Package weightedaggregator implements state aggregator for weighted_target
// balancer.
//
// This is a separate package so it can be shared by weighted_target and eds.
// The eds balancer will be refactored to use weighted_target directly. After
// that, all functions and structs in this package can be moved to package
// weightedtarget and unexported.
package weightedaggregator

import (
	"fmt"
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/wrr"
)

type weightedPickerState struct {
	weight uint32
	state  balancer.State
	// stateToAggregate is the connectivity state used only for state
	// aggregation. It could be different from state.ConnectivityState. For
	// example when a sub-balancer transitions from TransientFailure to
	// connecting, state.ConnectivityState is Connecting, but stateToAggregate
	// is still TransientFailure.
	stateToAggregate connectivity.State
}

func (s *weightedPickerState) String() string {
	return fmt.Sprintf("weight:%v,picker:%p,state:%v,stateToAggregate:%v", s.weight, s.state.Picker, s.state.ConnectivityState, s.stateToAggregate)
}

// Aggregator is the weighted balancer state aggregator.
type Aggregator struct {
	cc     balancer.ClientConn
	logger *grpclog.PrefixLogger
	newWRR func() wrr.WRR

	csEvltr *balancer.ConnectivityStateEvaluator

	mu sync.Mutex
	// If started is false, no updates should be sent to the parent cc. A closed
	// sub-balancer could still send pickers to this aggregator. This makes sure
	// that no updates will be forwarded to parent when the whole balancer group
	// and states aggregator is closed.
	started bool
	// All balancer IDs exist as keys in this map, even if balancer group is not
	// started.
	//
	// If an ID is not in map, it's either removed or never added.
	idToPickerState map[string]*weightedPickerState
	// Set when UpdateState call propagation is paused.
	pauseUpdateState bool
	// Set when UpdateState call propagation is paused and an UpdateState call
	// is suppressed.
	needUpdateStateOnResume bool
}

// New creates a new weighted balancer state aggregator.
func New(cc balancer.ClientConn, logger *grpclog.PrefixLogger, newWRR func() wrr.WRR) *Aggregator {
	return &Aggregator{
		cc:              cc,
		logger:          logger,
		newWRR:          newWRR,
		csEvltr:         &balancer.ConnectivityStateEvaluator{},
		idToPickerState: make(map[string]*weightedPickerState),
	}
}

// Start starts the aggregator. It can be called after Close to restart the
// aggretator.
func (wbsa *Aggregator) Start() {
	wbsa.mu.Lock()
	defer wbsa.mu.Unlock()
	wbsa.started = true
}

// Stop stops the aggregator. When the aggregator is closed, it won't call
// parent ClientConn to update balancer state.
func (wbsa *Aggregator) Stop() {
	wbsa.mu.Lock()
	defer wbsa.mu.Unlock()
	wbsa.started = false
	wbsa.clearStates()
}

// Add adds a sub-balancer state with weight. It adds a place holder, and waits for
// the real sub-balancer to update state.
func (wbsa *Aggregator) Add(id string, weight uint32) {
	wbsa.mu.Lock()
	defer wbsa.mu.Unlock()
	wbsa.idToPickerState[id] = &weightedPickerState{
		weight: weight,
		// Start everything in CONNECTING, so if one of the sub-balancers
		// reports TransientFailure, the RPCs will still wait for the other
		// sub-balancers.
		state: balancer.State{
			ConnectivityState: connectivity.Connecting,
			Picker:            base.NewErrPicker(balancer.ErrNoSubConnAvailable),
		},
		stateToAggregate: connectivity.Connecting,
	}
	// Record transition to connectivity.Connecting
	wbsa.csEvltr.RecordTransition(connectivity.Shutdown, connectivity.Connecting)
	// Call buildAndUpdateLocked to update picker state
	wbsa.buildAndUpdateLocked()
}

// Remove removes the sub-balancer state. Future updates from this sub-balancer,
// if any, will be ignored.
func (wbsa *Aggregator) Remove(id string) {
	wbsa.mu.Lock()
	defer wbsa.mu.Unlock()
	if _, ok := wbsa.idToPickerState[id]; !ok {
		return
	}
	// Record transition for picker to connectivity.Shutdown to decrement counter
	// for the picker's previous state
	wbsa.csEvltr.RecordTransition(wbsa.idToPickerState[id].stateToAggregate, connectivity.Shutdown)
	// Remove id and picker from picker map. This also results in future updates
	// for this ID to be ignored.
	delete(wbsa.idToPickerState, id)
	// Call buildAndUpdateLocked to update picker state
	wbsa.buildAndUpdateLocked()
}

// UpdateWeight updates the weight for the given id. Note that this doesn't
// trigger an update to the parent ClientConn. The caller should decide when
// it's necessary, and call BuildAndUpdate.
func (wbsa *Aggregator) UpdateWeight(id string, newWeight uint32) {
	wbsa.mu.Lock()
	defer wbsa.mu.Unlock()
	pState, ok := wbsa.idToPickerState[id]
	if !ok {
		return
	}
	pState.weight = newWeight
}

// PauseStateUpdates causes UpdateState calls to not propagate to the parent
// ClientConn.  The last state will be remembered and propagated when
// ResumeStateUpdates is called.
func (wbsa *Aggregator) PauseStateUpdates() {
	wbsa.mu.Lock()
	defer wbsa.mu.Unlock()
	wbsa.pauseUpdateState = true
	wbsa.needUpdateStateOnResume = false
}

// ResumeStateUpdates will resume propagating UpdateState calls to the parent,
// and call UpdateState on the parent if any UpdateState call was suppressed.
func (wbsa *Aggregator) ResumeStateUpdates() {
	wbsa.mu.Lock()
	defer wbsa.mu.Unlock()
	wbsa.pauseUpdateState = false
	if wbsa.needUpdateStateOnResume {
		wbsa.cc.UpdateState(wbsa.build())
	}
}

// UpdateState is called to report a balancer state change from sub-balancer.
// It's usually called by the balancer group.
//
// It calls parent ClientConn's UpdateState with the new aggregated state.
func (wbsa *Aggregator) UpdateState(id string, newState balancer.State) {
	wbsa.mu.Lock()
	defer wbsa.mu.Unlock()
	oldState, ok := wbsa.idToPickerState[id]
	if !ok {
		// All state starts with an entry in pickStateMap. If ID is not in map,
		// it's either removed, or never existed.
		return
	}
	// Record Transition from old state to new state
	wbsa.csEvltr.RecordTransition(oldState.stateToAggregate, newState.ConnectivityState)
	if !(oldState.state.ConnectivityState == connectivity.TransientFailure && newState.ConnectivityState == connectivity.Connecting) {
		// If old state is TransientFailure, and new state is Connecting, don't
		// update the state, to prevent the aggregated state from being always
		// CONNECTING. Otherwise, stateToAggregate is the same as
		// state.ConnectivityState.
		oldState.stateToAggregate = newState.ConnectivityState
	}
	oldState.state = newState

	if !wbsa.started {
		return
	}

	if wbsa.pauseUpdateState {
		// If updates are paused, do not call UpdateState, but remember that we
		// need to call it when they are resumed.
		wbsa.needUpdateStateOnResume = true
		return
	}

	wbsa.cc.UpdateState(wbsa.build())
}

// clearState Reset everything to init state (Connecting) but keep the entry in
// map (to keep the weight).
//
// Caller must hold wbsa.mu.
func (wbsa *Aggregator) clearStates() {
	for _, pState := range wbsa.idToPickerState {
		pState.state = balancer.State{
			ConnectivityState: connectivity.Connecting,
			Picker:            base.NewErrPicker(balancer.ErrNoSubConnAvailable),
		}
		pState.stateToAggregate = connectivity.Connecting
	}
}

// BuildAndUpdate combines the sub-state from each sub-balancer into one state,
// and update it to parent ClientConn.
func (wbsa *Aggregator) BuildAndUpdate() {
	wbsa.mu.Lock()
	defer wbsa.mu.Unlock()
	wbsa.buildAndUpdateLocked()
}

// BuildAndUpdateLocked combines the sub-state from each sub-balancer into one
// state, and update it to parent ClientConn - assuming that the lock on
// wbsa.mu is already in place
func (wbsa *Aggregator) buildAndUpdateLocked() {
	if !wbsa.started {
		return
	}
	if wbsa.pauseUpdateState {
		// If updates are paused, do not call UpdateState, but remember that we
		// need to call it when they are resumed.
		wbsa.needUpdateStateOnResume = true
		return
	}

	wbsa.cc.UpdateState(wbsa.build())
}

// build combines sub-states into one.
//
// Caller must hold wbsa.mu.
func (wbsa *Aggregator) build() balancer.State {
	wbsa.logger.Infof("Child pickers with config: %+v", wbsa.idToPickerState)

	// Make sure picker's return error is consistent with the aggregatedState.
	pickers := make([]weightedPickerState, 0, len(wbsa.idToPickerState))

	aggState := wbsa.csEvltr.CurrentState()
	switch aggState {
	case connectivity.Connecting:
		return balancer.State{
			ConnectivityState: aggState,
			Picker:            base.NewErrPicker(balancer.ErrNoSubConnAvailable)}
	case connectivity.TransientFailure:
		for _, ps := range wbsa.idToPickerState {
			if ps.stateToAggregate == connectivity.TransientFailure {
				pickers = append(pickers, *ps)
			}
		}
	default:
		for _, ps := range wbsa.idToPickerState {
			if ps.stateToAggregate == connectivity.Ready {
				pickers = append(pickers, *ps)
			}
		}
	}
	return balancer.State{ConnectivityState: aggState, Picker: newWeightedPickerGroup(pickers, wbsa.newWRR)}
}

type weightedPickerGroup struct {
	w wrr.WRR
}

// newWeightedPickerGroup takes pickers with weights, and groups them into one
// picker.
//
// Note it only takes ready pickers. The map shouldn't contain non-ready
// pickers.
func newWeightedPickerGroup(readyWeightedPickers []weightedPickerState, newWRR func() wrr.WRR) *weightedPickerGroup {
	w := newWRR()
	for _, ps := range readyWeightedPickers {
		w.Add(ps.state.Picker, int64(ps.weight))
	}

	return &weightedPickerGroup{
		w: w,
	}
}

func (pg *weightedPickerGroup) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	p, ok := pg.w.Next().(balancer.Picker)
	if !ok {
		return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
	}
	return p.Pick(info)
}
