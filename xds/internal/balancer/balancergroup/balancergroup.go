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

// Package balancergroup implements a utility struct to bind multiple balancers
// into one balancer.
package balancergroup

import (
	"fmt"
	"sync"
	"time"

	orcapb "github.com/cncf/udpa/go/udpa/data/orca/v1"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/cache"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/wrr"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/balancer/lrs"
)

// subBalancerWrapper is used to keep the configurations that will be used to start
// the underlying balancer. It can be called to start/stop the underlying
// balancer.
//
// When the config changes, it will pass the update to the underlying balancer
// if it exists.
//
// TODO: move to a separate file?
type subBalancerWrapper struct {
	// subBalancerWrapper is passed to the sub-balancer as a ClientConn
	// wrapper, only to keep the state and picker.  When sub-balancer is
	// restarted while in cache, the picker needs to be resent.
	//
	// It also contains the sub-balancer ID, so the parent balancer group can
	// keep track of SubConn/pickers and the sub-balancers they belong to. Some
	// of the actions are forwarded to the parent ClientConn with no change.
	// Some are forward to balancer group with the sub-balancer ID.
	balancer.ClientConn
	id    internal.LocalityID
	group *BalancerGroup

	mu    sync.Mutex
	state balancer.State

	// The static part of sub-balancer. Keeps balancerBuilders and addresses.
	// To be used when restarting sub-balancer.
	builder balancer.Builder
	// ccState is a cache of the addresses/balancer config, so when the balancer
	// is restarted after close, it will get the previous update. It's a pointer
	// and is set to nil at init, so when the balancer is built for the first
	// time (not a restart), it won't receive an empty update. Note that this
	// isn't reset to nil when the underlying balancer is closed.
	ccState *balancer.ClientConnState
	// The dynamic part of sub-balancer. Only used when balancer group is
	// started. Gets cleared when sub-balancer is closed.
	balancer balancer.Balancer
}

// UpdateState overrides balancer.ClientConn, to keep state and picker.
func (sbc *subBalancerWrapper) UpdateState(state balancer.State) {
	sbc.mu.Lock()
	sbc.state = state
	sbc.group.updateBalancerState(sbc.id, state)
	sbc.mu.Unlock()
}

// NewSubConn overrides balancer.ClientConn, so balancer group can keep track of
// the relation between subconns and sub-balancers.
func (sbc *subBalancerWrapper) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	return sbc.group.newSubConn(sbc, addrs, opts)
}

func (sbc *subBalancerWrapper) updateBalancerStateWithCachedPicker() {
	sbc.mu.Lock()
	if sbc.state.Picker != nil {
		sbc.group.updateBalancerState(sbc.id, sbc.state)
	}
	sbc.mu.Unlock()
}

func (sbc *subBalancerWrapper) startBalancer() {
	b := sbc.builder.Build(sbc, balancer.BuildOptions{})
	sbc.group.logger.Infof("Created child policy %p of type %v", b, sbc.builder.Name())
	sbc.balancer = b
	if sbc.ccState != nil {
		b.UpdateClientConnState(*sbc.ccState)
	}
}

func (sbc *subBalancerWrapper) updateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	b := sbc.balancer
	if b == nil {
		// This sub-balancer was closed. This can happen when EDS removes a
		// locality. The balancer for this locality was already closed, and the
		// SubConns are being deleted. But SubConn state change can still
		// happen.
		return
	}
	b.UpdateSubConnState(sc, state)
}

func (sbc *subBalancerWrapper) updateClientConnState(s balancer.ClientConnState) error {
	sbc.ccState = &s
	b := sbc.balancer
	if b == nil {
		// This sub-balancer was closed. This should never happen because
		// sub-balancers are closed when the locality is removed from EDS, or
		// the balancer group is closed. There should be no further address
		// updates when either of this happened.
		//
		// This will be a common case with priority support, because a
		// sub-balancer (and the whole balancer group) could be closed because
		// it's the lower priority, but it can still get address updates.
		return nil
	}
	return b.UpdateClientConnState(s)
}

func (sbc *subBalancerWrapper) resolverError(err error) {
	b := sbc.balancer
	if b == nil {
		// This sub-balancer was closed. This should never happen because
		// sub-balancers are closed when the locality is removed from EDS, or
		// the balancer group is closed. There should be no further address
		// updates when either of this happened.
		//
		// This will be a common case with priority support, because a
		// sub-balancer (and the whole balancer group) could be closed because
		// it's the lower priority, but it can still get address updates.
		return
	}
	b.ResolverError(err)
}

func (sbc *subBalancerWrapper) stopBalancer() {
	sbc.balancer.Close()
	sbc.balancer = nil
}

type pickerState struct {
	weight uint32
	state  balancer.State
	// stateToAggregate is the connectivity state used only for state
	// aggregation. It could be different from state.ConnectivityState. For
	// example when a sub-balancer transitions from TransientFailure to
	// connecting, state.ConnectivityState is Connecting, but stateToAggregate
	// is still TransientFailure.
	stateToAggregate connectivity.State
}

func (s *pickerState) String() string {
	return fmt.Sprintf("weight:%v,picker:%p,state:%v,stateToAggregate:%v", s.weight, s.state.Picker, s.state.ConnectivityState, s.stateToAggregate)
}

// BalancerGroup takes a list of balancers, and make them into one balancer.
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
type BalancerGroup struct {
	cc        balancer.ClientConn
	logger    *grpclog.PrefixLogger
	loadStore lrs.Store

	// outgoingMu guards all operations in the direction:
	// ClientConn-->Sub-balancer. Including start, stop, resolver updates and
	// SubConn state changes.
	//
	// The corresponding boolean outgoingStarted is used to stop further updates
	// to sub-balancers after they are closed.
	outgoingMu         sync.Mutex
	outgoingStarted    bool
	idToBalancerConfig map[internal.LocalityID]*subBalancerWrapper
	// Cache for sub-balancers when they are removed.
	balancerCache *cache.TimeoutCache

	// incomingMu and pickerMu are to make sure this balancer group doesn't send
	// updates to cc after it's closed.
	//
	// We don't share the mutex to avoid deadlocks (e.g. a call to sub-balancer
	// may call back to balancer group inline. It causes deaclock if they
	// require the same mutex).
	//
	// We should never need to hold multiple locks at the same time in this
	// struct. The case where two locks are held can only happen when the
	// underlying balancer calls back into balancer group inline. So there's an
	// implicit lock acquisition order that outgoingMu is locked before either
	// incomingMu or pickerMu.

	// incomingMu guards all operations in the direction:
	// Sub-balancer-->ClientConn. Including NewSubConn, RemoveSubConn, and
	// updatePicker. It also guards the map from SubConn to balancer ID, so
	// updateSubConnState needs to hold it shortly to find the
	// sub-balancer to forward the update.
	//
	// The corresponding boolean incomingStarted is used to stop further updates
	// from sub-balancers after they are closed.
	incomingMu      sync.Mutex
	incomingStarted bool // This boolean only guards calls back to ClientConn.
	scToSubBalancer map[balancer.SubConn]*subBalancerWrapper
	// All balancer IDs exist as keys in this map, even if balancer group is not
	// started.
	//
	// If an ID is not in map, it's either removed or never added.
	idToPickerState map[internal.LocalityID]*pickerState
}

// DefaultSubBalancerCloseTimeout is defined as a variable instead of const for
// testing.
//
// TODO: make it a parameter for New().
var DefaultSubBalancerCloseTimeout = 15 * time.Minute

// New creates a new BalancerGroup. Note that the BalancerGroup
// needs to be started to work.
func New(cc balancer.ClientConn, loadStore lrs.Store, logger *grpclog.PrefixLogger) *BalancerGroup {
	return &BalancerGroup{
		cc:        cc,
		logger:    logger,
		loadStore: loadStore,

		idToBalancerConfig: make(map[internal.LocalityID]*subBalancerWrapper),
		balancerCache:      cache.NewTimeoutCache(DefaultSubBalancerCloseTimeout),
		scToSubBalancer:    make(map[balancer.SubConn]*subBalancerWrapper),
		idToPickerState:    make(map[internal.LocalityID]*pickerState),
	}
}

// Start starts the balancer group, including building all the sub-balancers,
// and send the existing addresses to them.
//
// A BalancerGroup can be closed and started later. When a BalancerGroup is
// closed, it can still receive address updates, which will be applied when
// restarted.
func (bg *BalancerGroup) Start() {
	bg.incomingMu.Lock()
	bg.incomingStarted = true
	bg.incomingMu.Unlock()

	bg.outgoingMu.Lock()
	if bg.outgoingStarted {
		bg.outgoingMu.Unlock()
		return
	}

	for _, config := range bg.idToBalancerConfig {
		config.startBalancer()
	}
	bg.outgoingStarted = true
	bg.outgoingMu.Unlock()
}

// Add adds a balancer built by builder to the group, with given id and weight.
//
// weight should never be zero.
func (bg *BalancerGroup) Add(id internal.LocalityID, weight uint32, builder balancer.Builder) {
	if weight == 0 {
		bg.logger.Errorf("BalancerGroup.add called with weight 0, locality: %v. Locality is not added to balancer group", id)
		return
	}

	// First, add things to the picker map. Do this even if incomingStarted is
	// false, because the data is static.
	bg.incomingMu.Lock()
	bg.idToPickerState[id] = &pickerState{
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
	bg.incomingMu.Unlock()

	// Store data in static map, and then check to see if bg is started.
	bg.outgoingMu.Lock()
	var sbc *subBalancerWrapper
	// If outgoingStarted is true, search in the cache. Otherwise, cache is
	// guaranteed to be empty, searching is unnecessary.
	if bg.outgoingStarted {
		if old, ok := bg.balancerCache.Remove(id); ok {
			sbc, _ = old.(*subBalancerWrapper)
			if sbc != nil && sbc.builder != builder {
				// If the sub-balancer in cache was built with a different
				// balancer builder, don't use it, cleanup this old-balancer,
				// and behave as sub-balancer is not found in cache.
				//
				// NOTE that this will also drop the cached addresses for this
				// sub-balancer, which seems to be reasonable.
				sbc.stopBalancer()
				// cleanupSubConns must be done before the new balancer starts,
				// otherwise new SubConns created by the new balancer might be
				// removed by mistake.
				bg.cleanupSubConns(sbc)
				sbc = nil
			}
		}
	}
	if sbc == nil {
		sbc = &subBalancerWrapper{
			ClientConn: bg.cc,
			id:         id,
			group:      bg,
			builder:    builder,
		}
		if bg.outgoingStarted {
			// Only start the balancer if bg is started. Otherwise, we only keep the
			// static data.
			sbc.startBalancer()
		}
	} else {
		// When brining back a sub-balancer from cache, re-send the cached
		// picker and state.
		sbc.updateBalancerStateWithCachedPicker()
	}
	bg.idToBalancerConfig[id] = sbc
	bg.outgoingMu.Unlock()
}

// Remove removes the balancer with id from the group.
//
// But doesn't close the balancer. The balancer is kept in a cache, and will be
// closed after timeout. Cleanup work (closing sub-balancer and removing
// subconns) will be done after timeout.
//
// It also removes the picker generated from this balancer from the picker
// group. It always results in a picker update.
func (bg *BalancerGroup) Remove(id internal.LocalityID) {
	bg.outgoingMu.Lock()
	if sbToRemove, ok := bg.idToBalancerConfig[id]; ok {
		if bg.outgoingStarted {
			bg.balancerCache.Add(id, sbToRemove, func() {
				// After timeout, when sub-balancer is removed from cache, need
				// to close the underlying sub-balancer, and remove all its
				// subconns.
				bg.outgoingMu.Lock()
				if bg.outgoingStarted {
					sbToRemove.stopBalancer()
				}
				bg.outgoingMu.Unlock()
				bg.cleanupSubConns(sbToRemove)
			})
		}
		delete(bg.idToBalancerConfig, id)
	} else {
		bg.logger.Infof("balancer group: trying to remove a non-existing locality from balancer group: %v", id)
	}
	bg.outgoingMu.Unlock()

	bg.incomingMu.Lock()
	// Remove id and picker from picker map. This also results in future updates
	// for this ID to be ignored.
	delete(bg.idToPickerState, id)
	if bg.incomingStarted {
		// Normally picker update is triggered by SubConn state change. But we
		// want to update state and picker to reflect the changes, too. Because
		// we don't want `ClientConn` to pick this sub-balancer anymore.
		bg.cc.UpdateState(buildPickerAndState(bg.idToPickerState))
	}
	bg.incomingMu.Unlock()
}

// bg.remove(id) doesn't do cleanup for the sub-balancer. This function does
// cleanup after the timeout.
func (bg *BalancerGroup) cleanupSubConns(config *subBalancerWrapper) {
	bg.incomingMu.Lock()
	// Remove SubConns. This is only done after the balancer is
	// actually closed.
	//
	// NOTE: if NewSubConn is called by this (closed) balancer later, the
	// SubConn will be leaked. This shouldn't happen if the balancer
	// implementation is correct. To make sure this never happens, we need to
	// add another layer (balancer manager) between balancer group and the
	// sub-balancers.
	for sc, b := range bg.scToSubBalancer {
		if b == config {
			bg.cc.RemoveSubConn(sc)
			delete(bg.scToSubBalancer, sc)
		}
	}
	bg.incomingMu.Unlock()
}

// ChangeWeight changes the weight of the balancer.
//
// newWeight should never be zero.
//
// NOTE: It always results in a picker update now. This probably isn't
// necessary. But it seems better to do the update because it's a change in the
// picker (which is balancer's snapshot).
func (bg *BalancerGroup) ChangeWeight(id internal.LocalityID, newWeight uint32) {
	if newWeight == 0 {
		bg.logger.Errorf("BalancerGroup.changeWeight called with newWeight 0. Weight is not changed")
		return
	}
	bg.incomingMu.Lock()
	defer bg.incomingMu.Unlock()
	pState, ok := bg.idToPickerState[id]
	if !ok {
		return
	}
	if pState.weight == newWeight {
		return
	}
	pState.weight = newWeight
	if bg.incomingStarted {
		// Normally picker update is triggered by SubConn state change. But we
		// want to update state and picker to reflect the changes, too. Because
		// `ClientConn` should do pick with the new weights now.
		bg.cc.UpdateState(buildPickerAndState(bg.idToPickerState))
	}
}

// Following are actions from the parent grpc.ClientConn, forward to sub-balancers.

// UpdateSubConnState handles the state for the subconn. It finds the
// corresponding balancer and forwards the update.
func (bg *BalancerGroup) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	bg.incomingMu.Lock()
	config, ok := bg.scToSubBalancer[sc]
	if !ok {
		bg.incomingMu.Unlock()
		return
	}
	if state.ConnectivityState == connectivity.Shutdown {
		// Only delete sc from the map when state changed to Shutdown.
		delete(bg.scToSubBalancer, sc)
	}
	bg.incomingMu.Unlock()

	bg.outgoingMu.Lock()
	config.updateSubConnState(sc, state)
	bg.outgoingMu.Unlock()
}

// UpdateClientConnState handles ClientState (including balancer config and
// addresses) from resolver. It finds the balancer and forwards the update.
func (bg *BalancerGroup) UpdateClientConnState(id internal.LocalityID, s balancer.ClientConnState) error {
	bg.outgoingMu.Lock()
	defer bg.outgoingMu.Unlock()
	if config, ok := bg.idToBalancerConfig[id]; ok {
		return config.updateClientConnState(s)
	}
	return nil
}

// ResolverError forwards resolver errors to all sub-balancers.
func (bg *BalancerGroup) ResolverError(err error) {
	bg.outgoingMu.Lock()
	for _, config := range bg.idToBalancerConfig {
		config.resolverError(err)
	}
	bg.outgoingMu.Unlock()
}

// Following are actions from sub-balancers, forward to ClientConn.

// newSubConn: forward to ClientConn, and also create a map from sc to balancer,
// so state update will find the right balancer.
//
// One note about removing SubConn: only forward to ClientConn, but not delete
// from map. Delete sc from the map only when state changes to Shutdown. Since
// it's just forwarding the action, there's no need for a removeSubConn()
// wrapper function.
func (bg *BalancerGroup) newSubConn(config *subBalancerWrapper, addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	// NOTE: if balancer with id was already removed, this should also return
	// error. But since we call balancer.stopBalancer when removing the balancer, this
	// shouldn't happen.
	bg.incomingMu.Lock()
	if !bg.incomingStarted {
		bg.incomingMu.Unlock()
		return nil, fmt.Errorf("NewSubConn is called after balancer group is closed")
	}
	sc, err := bg.cc.NewSubConn(addrs, opts)
	if err != nil {
		bg.incomingMu.Unlock()
		return nil, err
	}
	bg.scToSubBalancer[sc] = config
	bg.incomingMu.Unlock()
	return sc, nil
}

// updateBalancerState: create an aggregated picker and an aggregated
// connectivity state, then forward to ClientConn.
func (bg *BalancerGroup) updateBalancerState(id internal.LocalityID, state balancer.State) {
	bg.logger.Infof("Balancer state update from locality %v, new state: %+v", id, state)

	bg.incomingMu.Lock()
	defer bg.incomingMu.Unlock()
	pickerSt, ok := bg.idToPickerState[id]
	if !ok {
		// All state starts in IDLE. If ID is not in map, it's either removed,
		// or never existed.
		bg.logger.Warningf("balancer group: pickerState for %v not found when update picker/state", id)
		return
	}
	if bg.loadStore != nil {
		// Only wrap the picker to do load reporting if loadStore was set.
		state.Picker = newLoadReportPicker(state.Picker, id, bg.loadStore)
	}
	if !(pickerSt.state.ConnectivityState == connectivity.TransientFailure && state.ConnectivityState == connectivity.Connecting) {
		// If old state is TransientFailure, and new state is Connecting, don't
		// update the state, to prevent the aggregated state from being always
		// CONNECTING. Otherwise, stateToAggregate is the same as
		// state.ConnectivityState.
		pickerSt.stateToAggregate = state.ConnectivityState
	}
	pickerSt.state = state
	if bg.incomingStarted {
		bg.logger.Infof("Child pickers with weight: %+v", bg.idToPickerState)
		bg.cc.UpdateState(buildPickerAndState(bg.idToPickerState))
	}
}

// Close closes the balancer. It stops sub-balancers, and removes the subconns.
// The BalancerGroup can be restarted later.
func (bg *BalancerGroup) Close() {
	bg.incomingMu.Lock()
	if bg.incomingStarted {
		bg.incomingStarted = false

		for _, pState := range bg.idToPickerState {
			// Reset everything to init state (Connecting) but keep the entry in
			// map (to keep the weight).
			pState.state = balancer.State{
				ConnectivityState: connectivity.Connecting,
				Picker:            base.NewErrPicker(balancer.ErrNoSubConnAvailable),
			}
			pState.stateToAggregate = connectivity.Connecting
		}

		// Also remove all SubConns.
		for sc := range bg.scToSubBalancer {
			bg.cc.RemoveSubConn(sc)
			delete(bg.scToSubBalancer, sc)
		}
	}
	bg.incomingMu.Unlock()

	bg.outgoingMu.Lock()
	if bg.outgoingStarted {
		bg.outgoingStarted = false
		for _, config := range bg.idToBalancerConfig {
			config.stopBalancer()
		}
	}
	bg.outgoingMu.Unlock()
	// Clear(true) runs clear function to close sub-balancers in cache. It
	// must be called out of outgoing mutex.
	bg.balancerCache.Clear(true)
}

func buildPickerAndState(m map[internal.LocalityID]*pickerState) balancer.State {
	var readyN, connectingN int
	readyPickerWithWeights := make([]pickerState, 0, len(m))
	for _, ps := range m {
		switch ps.stateToAggregate {
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

	// Make sure picker's return error is consistent with the aggregatedState.
	//
	// TODO: This is true for balancers like weighted_target, but not for
	// routing. For routing, we want to always build picker with all sub-pickers
	// (not even ready sub-pickers), so even if the overall state is Ready, pick
	// for certain RPCs can behave like Connecting or TransientFailure.
	var picker balancer.Picker
	switch aggregatedState {
	case connectivity.TransientFailure:
		picker = base.NewErrPicker(balancer.ErrTransientFailure)
	case connectivity.Connecting:
		picker = base.NewErrPicker(balancer.ErrNoSubConnAvailable)
	default:
		picker = newPickerGroup(readyPickerWithWeights)
	}
	return balancer.State{ConnectivityState: aggregatedState, Picker: picker}
}

// NewRandomWRR is the WRR constructor used to pick sub-pickers from
// sub-balancers. It's to be modified in tests.
var NewRandomWRR = wrr.NewRandom

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
	w := NewRandomWRR()
	for _, ps := range readyPickerWithWeights {
		w.Add(ps.state.Picker, int64(ps.weight))
	}

	return &pickerGroup{
		length: len(readyPickerWithWeights),
		w:      w,
	}
}

func (pg *pickerGroup) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	if pg.length <= 0 {
		return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
	}
	p := pg.w.Next().(balancer.Picker)
	return p.Pick(info)
}

const (
	serverLoadCPUName    = "cpu_utilization"
	serverLoadMemoryName = "mem_utilization"
)

type loadReportPicker struct {
	p balancer.Picker

	id        internal.LocalityID
	loadStore lrs.Store
}

func newLoadReportPicker(p balancer.Picker, id internal.LocalityID, loadStore lrs.Store) *loadReportPicker {
	return &loadReportPicker{
		p:         p,
		id:        id,
		loadStore: loadStore,
	}
}

func (lrp *loadReportPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	res, err := lrp.p.Pick(info)
	if lrp.loadStore != nil && err == nil {
		lrp.loadStore.CallStarted(lrp.id)
		td := res.Done
		res.Done = func(info balancer.DoneInfo) {
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
	return res, err
}
