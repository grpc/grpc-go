/*
 *
 * Copyright 2024 gRPC authors.
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

// Package endpointsharding implements a load balancing policy that manages
// homogeneous child policies each owning a single endpoint.
//
// # Experimental
//
// Notice: This package is EXPERIMENTAL and may be changed or removed in a
// later release.
package endpointsharding

import (
	"errors"
	rand "math/rand/v2"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/resolver"
)

var randIntN = rand.IntN

// ChildState is the state of a child balancer.
type ChildState struct {
	Endpoint resolver.Endpoint // Endpoint of the child balancer.
	State    balancer.State    // State of the child balancer.
	ExitIdle func()            // Function to exit the child balancer from IDLE state.
}

// Options configure the behaviour of the endpointsharding balancer.
type Options struct {
	// DisableAutoReconnect allows the balancer to keep child balancer in the
	// IDLE state until they are explicitly triggered to exit using the
	// ChildState obtained from the endpointsharding picker. When set to false,
	// the endpointsharding balancer will automatically call ExitIdle on child
	// connections that report IDLE.
	DisableAutoReconnect bool
}

// ChildBuilderFunc creates a new balancer with the ClientConn. It has the same
// type as the balancer.Builder.Build method.
type ChildBuilderFunc func(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer

// NewBalancer returns a load balancing policy that manages homogeneous child
// policies each owning a single endpoint. The endpointsharding balancer
// forwards the LoadBalancingConfig in ClientConn state updates to its children.
func NewBalancer(cc balancer.ClientConn, opts balancer.BuildOptions, childBuilder ChildBuilderFunc, esOpts Options) balancer.Balancer {
	return &endpointSharding{
		cc:           cc,
		bOpts:        opts,
		esOpts:       esOpts,
		childBuilder: childBuilder,
		endpoints:    resolver.NewEndpointMap[*endpointState](),
	}
}

// endpointSharding is a balancer that wraps child balancers. It creates a child
// balancer with child config for every unique Endpoint received. It updates the
// child states on any update from parent or child.
type endpointSharding struct {
	cc           balancer.ClientConn
	bOpts        balancer.BuildOptions
	esOpts       Options
	childBuilder ChildBuilderFunc

	// mu is used to guarantee mutual exclusion between top-down methods (like
	// UpdateClientConnState, ResolverError etc, which are already serialized) and
	// calls from child balancers (like UpdateState) that can be called
	// concurrently. This directly means that we should never call any methods on
	// the child balancers while holding the mutex, because they can call
	// UpdateState inline, leading to a deadlock.
	mu                  sync.Mutex
	endpoints           *resolver.EndpointMap[*endpointState]
	inhibitChildUpdates bool
}

// rotateEndpoints returns a slice of all the input endpoints rotated a random
// amount.
func rotateEndpoints(es []resolver.Endpoint) []resolver.Endpoint {
	n := len(es)
	if n == 0 {
		return es
	}
	r := randIntN(n)

	// Make a copy to avoid mutating data beyond the end of es.
	ret := make([]resolver.Endpoint, n)
	copy(ret, es[r:])
	copy(ret[n-r:], es[:r])
	return ret
}

// UpdateClientConnState creates a child for new endpoints and deletes children
// for endpoints that are no longer present. It also updates all the children,
// and sends a single synchronous update of the childrens' aggregated state at
// the end of the UpdateClientConnState operation.
//
// Returns the first error found from a child, but fully processes the update.
func (es *endpointSharding) UpdateClientConnState(state balancer.ClientConnState) error {
	es.mu.Lock()
	es.inhibitChildUpdates = true
	es.mu.Unlock()

	// Update/create child balancers for each endpoint in the update. Note that we
	// don't hold the mutex here, but this is fine because inhibitChildUpdates is
	// true, and therefore UpdateState will not access es.endpoints.
	var retErr error
	newEndpoints := resolver.NewEndpointMap[*endpointState]()
	for _, endpoint := range rotateEndpoints(state.ResolverState.Endpoints) {
		if _, ok := newEndpoints.Get(endpoint); ok {
			// Skip duplicate endpoints.
			continue
		}
		epState, ok := es.endpoints.Get(endpoint)
		if ok {
			// Endpoint child already exists, update the stored endpoint.
			epState.endpoint = endpoint
		} else {
			// Endpoint child does not exist, create a new one.
			epState = &endpointState{
				ClientConn: es.cc,
				es:         es,
				endpoint:   endpoint,
			}
			epState.childLB = es.childBuilder(epState, es.bOpts)
		}
		// Update the endpoint state for the endpoint.
		newEndpoints.Set(endpoint, epState)

		if err := epState.childLB.UpdateClientConnState(balancer.ClientConnState{
			BalancerConfig: state.BalancerConfig,
			ResolverState: resolver.State{
				Endpoints:  []resolver.Endpoint{endpoint},
				Attributes: state.ResolverState.Attributes,
			},
		}); err != nil && retErr == nil {
			// Keep the first error found from any child.
			retErr = err
		}
	}

	// Delete old children that are no longer present.
	for e, child := range es.endpoints.All() {
		if _, ok := newEndpoints.Get(e); !ok {
			child.childLB.Close()
		}
	}

	// Update the endpoints to the new endpoints.
	es.endpoints = newEndpoints
	if es.endpoints.Len() == 0 {
		retErr = balancer.ErrBadResolverState
	}

	es.mu.Lock()
	es.inhibitChildUpdates = false
	es.updateStateLocked()
	es.mu.Unlock()

	return retErr
}

// ResolverError forwards the resolver error to all of the endpointSharding's
// children and sends a single synchronous update of the childStates at the end
// of the ResolverError operation.
func (es *endpointSharding) ResolverError(err error) {
	es.mu.Lock()
	es.inhibitChildUpdates = true
	es.mu.Unlock()

	for _, child := range es.endpoints.All() {
		child.childLB.ResolverError(err)
	}

	es.mu.Lock()
	es.inhibitChildUpdates = false
	es.updateStateLocked()
	es.mu.Unlock()
}

func (es *endpointSharding) UpdateSubConnState(balancer.SubConn, balancer.SubConnState) {
	// UpdateSubConnState is deprecated.
}

func (es *endpointSharding) Close() {
	for _, child := range es.endpoints.All() {
		child.childLB.Close()
	}
}

func (es *endpointSharding) ExitIdle() {
	es.mu.Lock()
	es.inhibitChildUpdates = true
	es.mu.Unlock()

	for _, child := range es.endpoints.All() {
		child.childLB.ExitIdle()
	}

	es.mu.Lock()
	es.inhibitChildUpdates = false
	es.updateStateLocked()
	es.mu.Unlock()
}

// updateStateLocked updates this component's state. It sends the aggregated
// state, and a picker with round robin behavior with all the child states
// present if needed.
//
// Caller must hold es.mu.
func (es *endpointSharding) updateStateLocked() {
	var readyPickers, connectingPickers, idlePickers, transientFailurePickers []balancer.Picker

	childStates := make([]ChildState, 0, es.endpoints.Len())
	for _, epState := range es.endpoints.All() {
		childState := ChildState{
			Endpoint: epState.endpoint,
			State:    epState.state,
			ExitIdle: func() { go epState.childLB.ExitIdle() },
		}
		childStates = append(childStates, childState)
		childPicker := childState.State.Picker
		switch childState.State.ConnectivityState {
		case connectivity.Ready:
			readyPickers = append(readyPickers, childPicker)
		case connectivity.Connecting:
			connectingPickers = append(connectingPickers, childPicker)
		case connectivity.Idle:
			idlePickers = append(idlePickers, childPicker)
		case connectivity.TransientFailure:
			transientFailurePickers = append(transientFailurePickers, childPicker)
			// connectivity.Shutdown shouldn't appear.
		}
	}

	// Construct the round robin picker based off the aggregated state. Whatever
	// the aggregated state, use the pickers present that are currently in that
	// state only.
	var aggState connectivity.State
	var pickers []balancer.Picker
	if len(readyPickers) >= 1 {
		aggState = connectivity.Ready
		pickers = readyPickers
	} else if len(connectingPickers) >= 1 {
		aggState = connectivity.Connecting
		pickers = connectingPickers
	} else if len(idlePickers) >= 1 {
		aggState = connectivity.Idle
		pickers = idlePickers
	} else if len(transientFailurePickers) >= 1 {
		aggState = connectivity.TransientFailure
		pickers = transientFailurePickers
	} else {
		aggState = connectivity.TransientFailure
		pickers = []balancer.Picker{base.NewErrPicker(errors.New("no children to pick from"))}
	} // No children (resolver error before valid update).

	es.cc.UpdateState(balancer.State{
		ConnectivityState: aggState,
		Picker: &pickerWithChildStates{
			pickers:     pickers,
			childStates: childStates,
			next:        uint32(randIntN(len(pickers))),
		},
	})
}

// pickerWithChildStates delegates to the pickers it holds in a round robin
// fashion. It also contains the childStates of all the endpointSharding's
// children.
type pickerWithChildStates struct {
	pickers     []balancer.Picker
	childStates []ChildState
	next        uint32
}

func (p *pickerWithChildStates) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	nextIndex := atomic.AddUint32(&p.next, 1)
	picker := p.pickers[nextIndex%uint32(len(p.pickers))]
	return picker.Pick(info)
}

// ChildStatesFromPicker returns the state of all the children managed by the
// endpoint sharding balancer that created this picker.
func ChildStatesFromPicker(picker balancer.Picker) []ChildState {
	p, ok := picker.(*pickerWithChildStates)
	if !ok {
		return nil
	}
	return p.childStates
}

// endpointState is the internal state maintained for each endpoint.
type endpointState struct {
	balancer.ClientConn // Embedded to intercept UpdateState

	es       *endpointSharding // Parent endpointsharding balancer.
	childLB  balancer.Balancer // Child balancer.
	endpoint resolver.Endpoint // Endpoint of the child balancer.
	state    balancer.State    // State of the child balancer.
}

func (es *endpointState) UpdateState(state balancer.State) {
	es.es.mu.Lock()
	es.state = state
	if !es.es.inhibitChildUpdates {
		es.es.updateStateLocked()
	}
	es.es.mu.Unlock()

	if state.ConnectivityState == connectivity.Idle && !es.es.esOpts.DisableAutoReconnect {
		go es.childLB.ExitIdle()
	}
}
