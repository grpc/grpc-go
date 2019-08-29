/*
 * Copyright 2019 gRPC authors.
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
 */

package edsbalancer

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/wrr"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/balancer/lrs"
	orcapb "google.golang.org/grpc/xds/internal/proto/udpa/data/orca/v1/orca_load_report"
)

type balancerConfig struct {
	// The static part of balancer group. Keeps balancerBuilders and addresses.
	// To be used when restarting balancer group.
	builder balancer.Builder
	addrs   []resolver.Address
	// The dynamic part of balancer group. Only used when balancer group is
	// started. Gets cleared when balancer group is closed.
	balancer balancer.Balancer
}

func (config *balancerConfig) start(bgcc *balancerGroupCC) {
	b := config.builder.Build(bgcc, balancer.BuildOptions{})
	config.balancer = b
	if ub, ok := b.(balancer.V2Balancer); ok {
		ub.UpdateClientConnState(balancer.ClientConnState{ResolverState: resolver.State{Addresses: config.addrs}})
	} else {
		b.HandleResolvedAddrs(config.addrs, nil)
	}
}

func (config *balancerConfig) handleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	b := config.balancer
	if b == nil {
		// Either balancer is not found or balancer group is closed.
		return
	}
	if ub, ok := b.(balancer.V2Balancer); ok {
		ub.UpdateSubConnState(sc, balancer.SubConnState{ConnectivityState: state})
	} else {
		b.HandleSubConnStateChange(sc, state)
	}
}

func (config *balancerConfig) updateAddrs(addrs []resolver.Address) {
	config.addrs = addrs
	b := config.balancer
	if b == nil {
		// bg is closed.
		return
	}
	if ub, ok := b.(balancer.V2Balancer); ok {
		ub.UpdateClientConnState(balancer.ClientConnState{ResolverState: resolver.State{Addresses: addrs}})
	} else {
		b.HandleResolvedAddrs(addrs, nil)
	}
}

func (config *balancerConfig) close() {
	config.balancer.Close()
	config.balancer = nil
}

type pickerState struct {
	weight uint32
	picker balancer.Picker
	state  connectivity.State
}

// balancerGroup takes a list of balancers, and make then into one balancer.
//
// Note that this struct doesn't implement balancer.Balancer, because it's not
// intended to be used directly as a balancer. It's expected to be used as a
// sub-balancer manager by a high level balancer.
//
// Updates from ClientConn are forwarded to sub-balancers
//  - service config update
//     - Not implemented
//  - address update
//  - subConn state change
//     - find the corresponding balancer and forward
//
// Actions from sub-balances are forwarded to parent ClientConn
//  - new/remove SubConn
//  - picker update and health states change
//     - sub-pickers are grouped into a group-picker
//     - aggregated connectivity state is the overall state of all pickers.
//  - resolveNow
//
// Sub-balancers are only built when the balancer group is started. If the
// balancer group is closed, the sub-balancers are also closed. And it's
// guaranteed that no updates will be sent to parent ClientConn from a closed
// balancer group.
type balancerGroup struct {
	cc        balancer.ClientConn
	loadStore lrs.Store

	outgoingMu         sync.Mutex
	outgoingStarted    bool
	idToBalancerConfig map[internal.Locality]*balancerConfig

	// incomingMu and pickerMu are to make sure this balancer group doesn't send
	// updates to cc after it's closed.
	//
	// We don't share the mutex to avoid deadlocks (e.g. a call to sub-balancer
	// may call back to balancer group inline. It causes deaclock if they
	// require the same mutex).

	incomingMu      sync.Mutex
	incomingStarted bool // This boolean only guards calls back to ClientConn.
	scToID          map[balancer.SubConn]internal.Locality

	pickerMu      sync.Mutex
	pickerStarted bool // This boolean only guards calls back to ClientConn.
	// All balancer IDs exist as keys in this map, even if balancer group is not
	// started.
	//
	// If an ID is not in map, it's either removed or never added.
	idToPickerState map[internal.Locality]*pickerState
}

func newBalancerGroup(cc balancer.ClientConn, loadStore lrs.Store) *balancerGroup {
	return &balancerGroup{
		cc:        cc,
		loadStore: loadStore,

		idToBalancerConfig: make(map[internal.Locality]*balancerConfig),
		scToID:             make(map[balancer.SubConn]internal.Locality),
		idToPickerState:    make(map[internal.Locality]*pickerState),
	}
}

func (bg *balancerGroup) start() {
	bg.pickerMu.Lock()
	bg.pickerStarted = true
	bg.pickerMu.Unlock()

	bg.incomingMu.Lock()
	bg.incomingStarted = true
	bg.incomingMu.Unlock()

	bg.outgoingMu.Lock()
	if bg.outgoingStarted {
		bg.outgoingMu.Unlock()
		return
	}

	for id, config := range bg.idToBalancerConfig {
		config.start(&balancerGroupCC{
			ClientConn: bg.cc,
			id:         id,
			group:      bg,
		})
	}
	bg.outgoingStarted = true
	bg.outgoingMu.Unlock()
}

// add adds a balancer built by builder to the group, with given id and weight.
//
// weight should never be zero.
func (bg *balancerGroup) add(id internal.Locality, weight uint32, builder balancer.Builder) {
	if weight == 0 {
		grpclog.Errorf("balancerGroup.add called with weight 0, locality: %v. Locality is not added to balancer group", id)
		return
	}

	// First, add things to the picker map. Do this even if pickerStarted is
	// false, because the data is static.
	bg.pickerMu.Lock()
	bg.idToPickerState[id] = &pickerState{
		weight: weight,
		// Start everything in IDLE. It's doesn't affect the overall state
		// because we don't count IDLE when aggregating (as opposite to e.g.
		// READY, 1 READY results in overall READY).
		state: connectivity.Idle,
	}
	bg.pickerMu.Unlock()

	// Store data in static map, and then check to see if bg is started.
	bg.outgoingMu.Lock()
	if _, ok := bg.idToBalancerConfig[id]; ok {
		bg.outgoingMu.Unlock()
		grpclog.Warningf("balancer group: adding a balancer with existing ID: %s", id)
		return
	}
	config := &balancerConfig{
		builder: builder,
	}
	bg.idToBalancerConfig[id] = config

	if bg.outgoingStarted {
		// Only start the balancer if bg is started.
		config.start(&balancerGroupCC{
			ClientConn: bg.cc,
			id:         id,
			group:      bg,
		})
	}
	bg.outgoingMu.Unlock()
}

// remove removes the balancer with id from the group, and closes the balancer.
//
// It also removes the picker generated from this balancer from the picker
// group. It always results in a picker update.
func (bg *balancerGroup) remove(id internal.Locality) {
	bg.outgoingMu.Lock()
	delete(bg.idToBalancerConfig, id)
	if bg.outgoingStarted {
		// Close balancer.
		if config, ok := bg.idToBalancerConfig[id]; ok {
			config.close()
		}
	}
	bg.outgoingMu.Unlock()

	bg.incomingMu.Lock()
	// Remove SubConns.
	//
	// NOTE: if NewSubConn is called by this (closed) balancer later, the
	// SubConn will be leaked. This shouldn't happen if the balancer
	// implementation is correct. To make sure this never happens, we need to
	// add another layer (balancer manager) between balancer group and the
	// sub-balancers.
	for sc, bid := range bg.scToID {
		if bid == id {
			bg.cc.RemoveSubConn(sc)
			delete(bg.scToID, sc)
		}
	}
	bg.incomingMu.Unlock()

	bg.pickerMu.Lock()
	// Remove id and picker from picker map. This also results in future updates
	// for this ID to be ignored.
	delete(bg.idToPickerState, id)
	if bg.pickerStarted {
		// Update state and picker to reflect the changes.
		bg.cc.UpdateBalancerState(buildPickerAndState(bg.idToPickerState))
	}
	bg.pickerMu.Unlock()
}

// changeWeight changes the weight of the balancer.
//
// newWeight should never be zero.
//
// NOTE: It always results in a picker update now. This probably isn't
// necessary. But it seems better to do the update because it's a change in the
// picker (which is balancer's snapshot).
func (bg *balancerGroup) changeWeight(id internal.Locality, newWeight uint32) {
	if newWeight == 0 {
		grpclog.Errorf("balancerGroup.changeWeight called with newWeight 0. Weight is not changed")
		return
	}
	bg.pickerMu.Lock()
	defer bg.pickerMu.Unlock()
	pState, ok := bg.idToPickerState[id]
	if !ok {
		return
	}
	if pState.weight == newWeight {
		return
	}
	pState.weight = newWeight
	if bg.pickerStarted {
		// Update state and picker to reflect the changes.
		bg.cc.UpdateBalancerState(buildPickerAndState(bg.idToPickerState))
	}
}

// Following are actions from the parent grpc.ClientConn, forward to sub-balancers.

// SubConn state change: find the corresponding balancer and then forward.
func (bg *balancerGroup) handleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	grpclog.Infof("balancer group: handle subconn state change: %p, %v", sc, state)
	bg.incomingMu.Lock()
	if id, ok := bg.scToID[sc]; ok {
		if state == connectivity.Shutdown {
			// Only delete sc from the map when state changed to Shutdown.
			delete(bg.scToID, sc)
		}
		bg.outgoingMu.Lock()
		config, ok := bg.idToBalancerConfig[id]
		if ok {
			config.handleSubConnStateChange(sc, state)
		}
		bg.outgoingMu.Unlock()
	}
	bg.incomingMu.Unlock()
}

// Address change: forward to balancer.
func (bg *balancerGroup) handleResolvedAddrs(id internal.Locality, addrs []resolver.Address) {
	bg.outgoingMu.Lock()
	config, ok := bg.idToBalancerConfig[id]
	if ok {
		config.updateAddrs(addrs)
	}
	bg.outgoingMu.Unlock()
}

// TODO: handleServiceConfig()
//
// For BNS address for slicer, comes from endpoint.Metadata. It will be sent
// from parent to sub-balancers as service config.

// Following are actions from sub-balancers, forward to ClientConn.

// newSubConn: forward to ClientConn, and also create a map from sc to balancer,
// so state update will find the right balancer.
//
// One note about removing SubConn: only forward to ClientConn, but not delete
// from map. Delete sc from the map only when state changes to Shutdown. Since
// it's just forwarding the action, there's no need for a removeSubConn()
// wrapper function.
func (bg *balancerGroup) newSubConn(id internal.Locality, addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	bg.incomingMu.Lock()
	defer bg.incomingMu.Unlock()
	if !bg.incomingStarted {
		return nil, fmt.Errorf("NewSubConn is called after balancer is closed")
	}
	// NOTE: if balancer with id was already removed, this should also return
	// error. But since we call balancer.close when removing the balancer, this
	// shouldn't happen.
	sc, err := bg.cc.NewSubConn(addrs, opts)
	if err != nil {
		return nil, err
	}
	bg.scToID[sc] = id
	return sc, nil
}

// updateBalancerState: create an aggregated picker and an aggregated
// connectivity state, then forward to ClientConn.
func (bg *balancerGroup) updateBalancerState(id internal.Locality, state connectivity.State, picker balancer.Picker) {
	grpclog.Infof("balancer group: update balancer state: %v, %v, %p", id, state, picker)

	bg.pickerMu.Lock()
	defer bg.pickerMu.Unlock()
	pickerSt, ok := bg.idToPickerState[id]
	if !ok {
		// All state starts in IDLE. If ID is not in map, it's either removed,
		// or never existed.
		grpclog.Infof("balancer group: pickerState not found when update picker/state")
		return
	}
	pickerSt.picker = newLoadReportPicker(picker, id, bg.loadStore)
	pickerSt.state = state
	if bg.pickerStarted {
		bg.cc.UpdateBalancerState(buildPickerAndState(bg.idToPickerState))
	}
}

func (bg *balancerGroup) close() {
	bg.pickerMu.Lock()
	if bg.pickerStarted {
		bg.pickerStarted = false
		for _, pState := range bg.idToPickerState {
			// Reset everything to IDLE but keep the entry in map (to keep the
			// weight).
			pState.picker = nil
			pState.state = connectivity.Idle
		}
	}
	bg.pickerMu.Unlock()

	bg.incomingMu.Lock()
	if bg.incomingStarted {
		bg.incomingStarted = false
		// Also remove all SubConns.
		for sc := range bg.scToID {
			bg.cc.RemoveSubConn(sc)
			delete(bg.scToID, sc)
		}
	}
	bg.incomingMu.Unlock()

	bg.outgoingMu.Lock()
	if bg.outgoingStarted {
		bg.outgoingStarted = false
		for _, config := range bg.idToBalancerConfig {
			config.close()
		}
	}
	bg.outgoingMu.Unlock()
}

func buildPickerAndState(m map[internal.Locality]*pickerState) (connectivity.State, balancer.Picker) {
	var readyN, connectingN int
	readyPickerWithWeights := make([]pickerState, 0, len(m))
	for _, ps := range m {
		switch ps.state {
		case connectivity.Ready:
			readyN++
			readyPickerWithWeights = append(readyPickerWithWeights, *ps)
		case connectivity.Connecting:
			connectingN++
		}
	}
	var aggregatedState connectivity.State
	switch {
	case readyN > 0:
		aggregatedState = connectivity.Ready
	case connectingN > 0:
		aggregatedState = connectivity.Connecting
	default:
		aggregatedState = connectivity.TransientFailure
	}
	if aggregatedState == connectivity.TransientFailure {
		return aggregatedState, base.NewErrPicker(balancer.ErrTransientFailure)
	}
	return aggregatedState, newPickerGroup(readyPickerWithWeights)
}

// RandomWRR constructor, to be modified in tests.
var newRandomWRR = wrr.NewRandom

type pickerGroup struct {
	length int
	w      wrr.WRR
}

// newPickerGroup takes pickers with weights, and group them into one picker.
//
// Note it only takes ready pickers. The map shouldn't contain non-ready
// pickers.
//
// TODO: (bg) confirm this is the expected behavior: non-ready balancers should
// be ignored when picking. Only ready balancers are picked.
func newPickerGroup(readyPickerWithWeights []pickerState) *pickerGroup {
	w := newRandomWRR()
	for _, ps := range readyPickerWithWeights {
		w.Add(ps.picker, int64(ps.weight))
	}

	return &pickerGroup{
		length: len(readyPickerWithWeights),
		w:      w,
	}
}

func (pg *pickerGroup) Pick(ctx context.Context, opts balancer.PickOptions) (conn balancer.SubConn, done func(balancer.DoneInfo), err error) {
	if pg.length <= 0 {
		return nil, nil, balancer.ErrNoSubConnAvailable
	}
	p := pg.w.Next().(balancer.Picker)
	return p.Pick(ctx, opts)
}

const (
	serverLoadCPUName    = "cpu_utilization"
	serverLoadMemoryName = "mem_utilization"
)

type loadReportPicker struct {
	balancer.Picker

	id        internal.Locality
	loadStore lrs.Store
}

func newLoadReportPicker(p balancer.Picker, id internal.Locality, loadStore lrs.Store) *loadReportPicker {
	return &loadReportPicker{
		Picker:    p,
		id:        id,
		loadStore: loadStore,
	}
}

func (lrp *loadReportPicker) Pick(ctx context.Context, opts balancer.PickOptions) (conn balancer.SubConn, done func(balancer.DoneInfo), err error) {
	conn, done, err = lrp.Picker.Pick(ctx, opts)
	if lrp.loadStore != nil && err == nil {
		lrp.loadStore.CallStarted(lrp.id)
		td := done
		done = func(info balancer.DoneInfo) {
			lrp.loadStore.CallFinished(lrp.id, info.Err)
			if load, ok := info.ServerLoad.(*orcapb.OrcaLoadReport); ok {
				lrp.loadStore.CallServerLoad(lrp.id, serverLoadCPUName, load.CpuUtilization)
				lrp.loadStore.CallServerLoad(lrp.id, serverLoadMemoryName, load.MemUtilization)
				for n, d := range load.RequestCost {
					lrp.loadStore.CallServerLoad(lrp.id, n, d)
				}
				for n, d := range load.Utilization {
					lrp.loadStore.CallServerLoad(lrp.id, n, d)
				}
			}
			if td != nil {
				td(info)
			}
		}
	}
	return
}

// balancerGroupCC implements the balancer.ClientConn API and get passed to each
// sub-balancer. It contains the sub-balancer ID, so the parent balancer can
// keep track of SubConn/pickers and the sub-balancers they belong to.
//
// Some of the actions are forwarded to the parent ClientConn with no change.
// Some are forward to balancer group with the sub-balancer ID.
type balancerGroupCC struct {
	balancer.ClientConn
	id    internal.Locality
	group *balancerGroup
}

func (bgcc *balancerGroupCC) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	return bgcc.group.newSubConn(bgcc.id, addrs, opts)
}
func (bgcc *balancerGroupCC) UpdateBalancerState(state connectivity.State, picker balancer.Picker) {
	bgcc.group.updateBalancerState(bgcc.id, state, picker)
}
