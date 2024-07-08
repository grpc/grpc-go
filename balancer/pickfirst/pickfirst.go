/*
 *
 * Copyright 2017 gRPC authors.
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

// Package pickfirst contains the pick_first load balancing policy.
package pickfirst

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

const (
	subConnListPending = iota
	subConnListActive
	subConnListClosed
)

func init() {
	balancer.Register(pickfirstBuilder{})
	internal.ShuffleAddressListForTesting = func(n int, swap func(i, j int)) { rand.Shuffle(n, swap) }
}

var logger = grpclog.Component("pick-first-lb")

const (
	// Name is the name of the pick_first balancer.
	Name      = "pick_first"
	logPrefix = "[pick-first-lb %p] "
)

type pickfirstBuilder struct{}

func (pickfirstBuilder) Build(cc balancer.ClientConn, opt balancer.BuildOptions) balancer.Balancer {
	b := &pickfirstBalancer{cc: cc}
	b.logger = internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf(logPrefix, b))
	return b
}

func (pickfirstBuilder) Name() string {
	return Name
}

type pfConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	// If set to true, instructs the LB policy to shuffle the order of the list
	// of endpoints received from the name resolver before attempting to
	// connect to them.
	ShuffleAddressList bool `json:"shuffleAddressList"`
}

// subConnList stores provides functions to connect to a list of addresses using
// the pick-first algorithm.
type subConnList struct {
	subConns []*scWrapper
	// The index within the subConns list for the subConn being tried.
	attemptingIndex int
	b               *pickfirstBalancer
	// The most recent failure during the initial connection attempt over the
	// entire sunConns list.
	lastFailure error
	// Whether all the subConns have reported a transient failure once.
	inTransientFailure bool
	// Use a mutex to guard the list status as it can be changed concurrently by
	// the idlePicker. Note that only calls to change the state to active are
	// triggered from the picker and can happen concurrently.
	mu     sync.RWMutex
	status int
}

// scWrapper keeps track of the current state of the subConn.
type scWrapper struct {
	subConn  balancer.SubConn
	conState connectivity.State
	addr     resolver.Address
}

func newScWrapper(b *pickfirstBalancer, addr resolver.Address, listener func(state balancer.SubConnState)) (*scWrapper, error) {
	scw := &scWrapper{
		conState: connectivity.Idle,
		addr:     addr,
	}
	sc, err := b.cc.NewSubConn([]resolver.Address{addr}, balancer.NewSubConnOptions{
		StateListener: func(scs balancer.SubConnState) {
			// Store the state and delegate.
			scw.conState = scs.ConnectivityState
			listener(scs)
		},
	})
	if err != nil {
		return nil, err
	}
	scw.subConn = sc
	return scw, nil
}

func newSubConnList(addrs []resolver.Address, b *pickfirstBalancer) *subConnList {
	sl := &subConnList{
		b:      b,
		status: subConnListPending,
	}

	for _, addr := range addrs {
		var scw *scWrapper
		scw, err := newScWrapper(b, addr, func(state balancer.SubConnState) {
			sl.stateListener(scw, state)
		})
		if err != nil {
			if b.logger.V(2) {
				b.logger.Infof("Ignoring failure, could not create a subConn for address %q due to error: %v", addr, err)
			}
			continue
		}
		if b.logger.V(2) {
			b.logger.Infof("Created a subConn for address %q", addr)
		}
		sl.subConns = append(sl.subConns, scw)
	}
	return sl
}

func (sl *subConnList) startConnectingIfNeeded() {
	if sl == nil {
		return
	}
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.status != subConnListPending {
		return
	}
	sl.status = subConnListActive
	sl.startConnectingNextSubConn()
}

func (sl *subConnList) stateListener(scw *scWrapper, state balancer.SubConnState) {
	if sl.b.logger.V(2) {
		sl.b.logger.Infof("Received SubConn state update: %p, %+v", scw, state)
	}
	if scw == sl.b.selectedSubConn {
		// As we set the selected subConn only once it's ready, the only
		// possible transitions are to IDLE and SHUTDOWN.
		switch state.ConnectivityState {
		case connectivity.Shutdown:
		case connectivity.Idle:
			sl.b.goIdle()
		default:
			sl.b.logger.Warningf("Ignoring unexpected transition of selected subConn %p to %v", &scw.subConn, state.ConnectivityState)
		}
		return
	}
	// If this list is already closed, ignore the update.
	if sl.status != subConnListActive {
		if sl.b.logger.V(2) {
			sl.b.logger.Infof("Ignoring state update for non active subConn %p to %v", &scw.subConn, state.ConnectivityState)
		}
		return
	}
	if !sl.inTransientFailure {
		// We are still trying to connect to each subConn once.
		switch state.ConnectivityState {
		case connectivity.TransientFailure:
			if sl.b.logger.V(2) {
				sl.b.logger.Infof("SubConn %p failed to connect due to error: %v", &scw.subConn, state.ConnectionError)
			}
			sl.attemptingIndex++
			sl.lastFailure = state.ConnectionError
			sl.startConnectingNextSubConn()
		case connectivity.Ready:
			// Cleanup and update the picker to use the subconn.
			sl.selectSubConn(scw)
		case connectivity.Connecting:
			// Move the channel to connecting if this is the first subConn to
			// start connecting.
			if sl.b.state == connectivity.Idle {
				sl.b.cc.UpdateState(balancer.State{
					ConnectivityState: connectivity.Connecting,
					Picker:            &picker{err: balancer.ErrNoSubConnAvailable},
				})
			}
		default:
			if sl.b.logger.V(2) {
				sl.b.logger.Infof("Ignoring update for the subConn %p to state %v", &scw.subConn, state.ConnectivityState)
			}
		}
		return
	}

	// We have attempted to connect to all the subConns once and failed.
	switch state.ConnectivityState {
	case connectivity.TransientFailure:
		if sl.b.logger.V(2) {
			sl.b.logger.Infof("SubConn %p failed to connect due to error: %v", &scw.subConn, state.ConnectionError)
		}
		// If its the initial connection attempt, try to connect to the next subConn.
		if sl.attemptingIndex < len(sl.subConns) && sl.subConns[sl.attemptingIndex] == scw {
			// Going over all the subConns and try connecting to ones that are idle.
			sl.attemptingIndex++
			sl.startConnectingNextSubConn()
		}
	case connectivity.Ready:
		// Cleanup and update the picker to use the subconn.
		sl.selectSubConn(scw)
	case connectivity.Idle:
		// Trigger re-connection.
		scw.subConn.Connect()
	default:
		if sl.b.logger.V(2) {
			sl.b.logger.Infof("Ignoring update for the subConn %p to state %v", &scw.subConn, state.ConnectivityState)
		}
	}
}

func (sl *subConnList) selectSubConn(scw *scWrapper) {
	if sl.b.logger.V(2) {
		sl.b.logger.Infof("Selected subConn %p", &scw.subConn)
	}
	sl.b.unsetSelectedSubConn()
	sl.b.selectedSubConn = scw
	sl.b.state = connectivity.Ready
	sl.inTransientFailure = false
	sl.close()
	sl.b.cc.UpdateState(balancer.State{
		ConnectivityState: connectivity.Ready,
		Picker:            &picker{result: balancer.PickResult{SubConn: scw.subConn}},
	})
}

func (sl *subConnList) close() {
	if sl == nil {
		return
	}
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.status == subConnListClosed {
		return
	}
	sl.status = subConnListClosed
	// Close all the subConns except the selected one. The selected subConn
	// will be closed by the balancer.
	for _, sc := range sl.subConns {
		if sc == sl.b.selectedSubConn {
			continue
		}
		sc.subConn.Shutdown()
	}
	sl.subConns = nil
}

func (sl *subConnList) startConnectingNextSubConn() {
	if sl.status != subConnListActive {
		return
	}
	if !sl.inTransientFailure && sl.attemptingIndex < len(sl.subConns) {
		// Try to connect to the next subConn.
		sl.subConns[sl.attemptingIndex].subConn.Connect()
		return
	}
	if !sl.inTransientFailure {
		// Failed to connect to each subConn once, enter transient failure.
		sl.b.state = connectivity.TransientFailure
		sl.inTransientFailure = true
		sl.attemptingIndex = 0
		sl.b.cc.UpdateState(balancer.State{
			ConnectivityState: connectivity.TransientFailure,
			Picker:            &picker{err: sl.lastFailure},
		})
		// Drop the existing (working) connection, if any.  This may be
		// sub-optimal, but we can't ignore what the control plane told us.
		sl.b.unsetSelectedSubConn()
	}

	// Attempt to connect to addresses that have already completed the back-off.
	for ; sl.attemptingIndex < len(sl.subConns); sl.attemptingIndex++ {
		sc := sl.subConns[sl.attemptingIndex]
		if sc.conState == connectivity.TransientFailure {
			continue
		}
		sl.subConns[sl.attemptingIndex].subConn.Connect()
		return
	}
	// Wait for the next subConn to enter idle state, then re-connect.
}

func (pickfirstBuilder) ParseConfig(js json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	var cfg pfConfig
	if err := json.Unmarshal(js, &cfg); err != nil {
		return nil, fmt.Errorf("pickfirst: unable to unmarshal LB policy config: %s, error: %v", string(js), err)
	}
	return cfg, nil
}

type pickfirstBalancer struct {
	logger *internalgrpclog.PrefixLogger
	state  connectivity.State
	cc     balancer.ClientConn
	// Pointer to the subConn list currently connecting. Always close the
	// current list before replacing it to ensure resources are freed.
	subConnList       *subConnList
	selectedSubConn   *scWrapper
	latestAddressList []resolver.Address
	shuttingDown      bool
}

func (b *pickfirstBalancer) ResolverError(err error) {
	if b.logger.V(2) {
		b.logger.Infof("Received error from the name resolver: %v", err)
	}
	if len(b.latestAddressList) == 0 {
		// The picker will not change since the balancer does not currently
		// report an error.
		b.state = connectivity.TransientFailure
	}

	if b.state != connectivity.TransientFailure {
		// The picker will not change since the balancer does not currently
		// report an error.
		return
	}
	b.cc.UpdateState(balancer.State{
		ConnectivityState: connectivity.TransientFailure,
		Picker:            &picker{err: fmt.Errorf("name resolver error: %v", err)},
	})
}

func (b *pickfirstBalancer) unsetSelectedSubConn() {
	if b.selectedSubConn != nil {
		b.selectedSubConn.subConn.Shutdown()
		b.selectedSubConn = nil
	}
}

func (b *pickfirstBalancer) goIdle() {
	b.unsetSelectedSubConn()
	b.refreshSunConnList()
	if len(b.subConnList.subConns) == 0 {
		b.state = connectivity.TransientFailure
		b.cc.UpdateState(balancer.State{
			ConnectivityState: connectivity.TransientFailure,
			Picker:            &picker{err: fmt.Errorf("empty address list")},
		})
		b.unsetSelectedSubConn()
		b.subConnList.close()
		b.cc.ResolveNow(resolver.ResolveNowOptions{})
		return
	}

	callback := func() {
		b.subConnList.startConnectingIfNeeded()
	}

	nextState := connectivity.Idle
	if b.state == connectivity.TransientFailure {
		// We stay in TransientFailure until we are Ready. See A62.
		nextState = connectivity.TransientFailure
	} else {
		b.state = connectivity.Idle
	}
	b.cc.UpdateState(balancer.State{
		ConnectivityState: nextState,
		Picker: &idlePicker{
			callback: callback,
		},
	})
}

type Shuffler interface {
	ShuffleAddressListForTesting(n int, swap func(i, j int))
}

func ShuffleAddressListForTesting(n int, swap func(i, j int)) { rand.Shuffle(n, swap) }

func (b *pickfirstBalancer) refreshSunConnList() {
	subConnList := newSubConnList(b.latestAddressList, b)

	// Reset the previous subConnList to release resources.
	if b.subConnList != nil {
		if b.logger.V(2) {
			b.logger.Infof("Closing older subConnList")
		}
		b.subConnList.close()
	}
	b.subConnList = subConnList
}

func (b *pickfirstBalancer) connectUsingLatestAddrs() error {
	b.refreshSunConnList()
	if len(b.subConnList.subConns) == 0 {
		b.state = connectivity.TransientFailure
		b.cc.UpdateState(balancer.State{
			ConnectivityState: connectivity.TransientFailure,
			Picker:            &picker{err: fmt.Errorf("empty address list")},
		})
		b.unsetSelectedSubConn()
		b.subConnList.close()
		return balancer.ErrBadResolverState
	}

	if b.state != connectivity.TransientFailure {
		// We stay in TransientFailure until we are Ready. See A62.
		b.cc.UpdateState(balancer.State{
			ConnectivityState: connectivity.Connecting,
			Picker:            &picker{err: balancer.ErrNoSubConnAvailable},
		})
	}
	b.subConnList.startConnectingIfNeeded()
	return nil
}

func (b *pickfirstBalancer) UpdateClientConnState(state balancer.ClientConnState) error {
	if len(state.ResolverState.Addresses) == 0 && len(state.ResolverState.Endpoints) == 0 {
		// The resolver reported an empty address list. Treat it like an error by
		// calling b.ResolverError.
		b.unsetSelectedSubConn()
		// Shut down the old subConnList. All addresses were removed, so it is
		// no longer valid.
		b.subConnList.close()
		b.latestAddressList = nil
		b.ResolverError(errors.New("produced zero addresses"))
		return balancer.ErrBadResolverState
	}
	// We don't have to guard this block with the env var because ParseConfig
	// already does so.
	cfg, ok := state.BalancerConfig.(pfConfig)
	if state.BalancerConfig != nil && !ok {
		return fmt.Errorf("pickfirst: received illegal BalancerConfig (type %T): %v", state.BalancerConfig, state.BalancerConfig)
	}

	if b.logger.V(2) {
		b.logger.Infof("Received new config %s, resolver state %s", pretty.ToJSON(cfg), pretty.ToJSON(state.ResolverState))
	}

	var addrs []resolver.Address
	if endpoints := state.ResolverState.Endpoints; len(endpoints) != 0 {
		// Perform the optional shuffling described in gRFC A62. The shuffling will
		// change the order of endpoints but not touch the order of the addresses
		// within each endpoint. - A61
		if cfg.ShuffleAddressList {
			endpoints = append([]resolver.Endpoint{}, endpoints...)
			internal.ShuffleAddressListForTesting.(func(int, func(int, int)))(len(endpoints), func(i, j int) { endpoints[i], endpoints[j] = endpoints[j], endpoints[i] })
		}

		// "Flatten the list by concatenating the ordered list of addresses for each
		// of the endpoints, in order." - A61
		for _, endpoint := range endpoints {
			// "In the flattened list, interleave addresses from the two address
			// families, as per RFC-8304 section 4." - A61
			// TODO: support the above language.
			addrs = append(addrs, endpoint.Addresses...)
		}
	} else {
		// Endpoints not set, process addresses until we migrate resolver
		// emissions fully to Endpoints. The top channel does wrap emitted
		// addresses with endpoints, however some balancers such as weighted
		// target do not forward the corresponding correct endpoints down/split
		// endpoints properly. Once all balancers correctly forward endpoints
		// down, can delete this else conditional.
		addrs = state.ResolverState.Addresses
		if cfg.ShuffleAddressList {
			addrs = append([]resolver.Address{}, addrs...)
			rand.Shuffle(len(addrs), func(i, j int) { addrs[i], addrs[j] = addrs[j], addrs[i] })
		}
	}

	b.latestAddressList = addrs
	// If the selected subConn's address is present in the list, don't attempt
	// to re-connect.
	if b.selectedSubConn != nil && slices.ContainsFunc(addrs, func(addr resolver.Address) bool {
		return addr.Equal(b.selectedSubConn.addr)
	}) {
		if b.logger.V(2) {
			b.logger.Infof("Not attempting to re-connect since selected address %q is present in new address list", b.selectedSubConn.addr.String())
		}
		return nil
	}
	return b.connectUsingLatestAddrs()
}

// UpdateSubConnState is unused as a StateListener is always registered when
// creating SubConns.
func (b *pickfirstBalancer) UpdateSubConnState(subConn balancer.SubConn, state balancer.SubConnState) {
	b.logger.Errorf("UpdateSubConnState(%v, %+v) called unexpectedly", subConn, state)
}

func (b *pickfirstBalancer) Close() {
	b.shuttingDown = true
	b.unsetSelectedSubConn()
	b.subConnList.close()
}

func (b *pickfirstBalancer) ExitIdle() {
	if b.shuttingDown {
		return
	}
	b.subConnList.startConnectingIfNeeded()
}

type picker struct {
	result balancer.PickResult
	err    error
}

func (p *picker) Pick(balancer.PickInfo) (balancer.PickResult, error) {
	return p.result, p.err
}

// idlePicker is used when the SubConn is IDLE and kicks the SubConn into
// CONNECTING when Pick is called.
type idlePicker struct {
	callback func()
}

func (i *idlePicker) Pick(balancer.PickInfo) (balancer.PickResult, error) {
	i.callback()
	return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
}
