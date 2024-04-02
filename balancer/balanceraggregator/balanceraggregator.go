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

// Package balanceraggregator implements a BalancerAggregator helper.
package balanceraggregator

import (
	"encoding/json"
	"errors"
	"sync/atomic"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/balancer/gracefulswitch"
	"google.golang.org/grpc/internal/grpcrand"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

// ChildState is the balancer state of the child along with the
// endpoint which ID's the child balancer.
type ChildState struct {
	Endpoint resolver.Endpoint
	State    balancer.State
}

// Build returns a new BalancerAggregator.
func Build(cc balancer.ClientConn, opts balancer.BuildOptions) *BalancerAggregator {
	return &BalancerAggregator{
		cc:       cc,
		bOpts:    opts,
		children: resolver.NewEndpointMap(),
	}
}

// BalancerAggregator is a balancer that wraps child balancers. It creates a
// child balancer with child config for every Endpoint received. It updates the
// child states on any update from parent or child.
type BalancerAggregator struct {
	cc    balancer.ClientConn
	bOpts balancer.BuildOptions

	children *resolver.EndpointMap

	inhibitChildUpdates bool
}

// UpdateClientConnState creates a child for new endpoints, deletes children for
// endpoints that are no longer present. It also updates all the children, and
// sends a single synchronous update of the children's aggregate state at the
// end of the UpdateClientConnState operation. If an endpoint has no addresses,
// returns error. Otherwise returns first error found from a child, but fully
// processes the new update.
func (ba *BalancerAggregator) UpdateClientConnState(state balancer.ClientConnState) error {
	if len(state.ResolverState.Endpoints) == 0 {
		return errors.New("endpoints list is empty")
	}

	// Check/return early if any endpoints have no addresses.
	// TODO: eventually make this configurable.
	for _, endpoint := range state.ResolverState.Endpoints {
		if len(endpoint.Addresses) == 0 {
			return errors.New("endpoint has empty addresses")
		}
	}
	endpointSet := resolver.NewEndpointMap()
	ba.inhibitChildUpdates = true
	defer func() {
		ba.inhibitChildUpdates = false
		ba.updateState()
	}()
	var ret error
	// Update/Create new children.
	for _, endpoint := range state.ResolverState.Endpoints {
		endpointSet.Set(endpoint, nil)
		var bal *balancerWrapper
		if child, ok := ba.children.Get(endpoint); ok {
			bal = child.(*balancerWrapper)
		} else {
			bal = &balancerWrapper{
				childState: ChildState{Endpoint: endpoint},
				ClientConn: ba.cc,
				ba:         ba,
			}
			bal.Balancer = gracefulswitch.NewBalancer(bal, ba.bOpts)
			ba.children.Set(endpoint, bal)
		}
		if err := bal.UpdateClientConnState(balancer.ClientConnState{
			BalancerConfig: state.BalancerConfig,
			ResolverState: resolver.State{
				Endpoints:  []resolver.Endpoint{endpoint},
				Attributes: state.ResolverState.Attributes,
			},
		}); err != nil && ret == nil {
			// Return first error found, and always commit full processing of
			// updating children. If desired to process more specific errors
			// across all endpoints, caller should make these specific
			// validations, this is a current limitation for simplicities sake.
			ret = err
		}
	}
	// Delete old children that are no longer present.
	for _, e := range ba.children.Keys() {
		child, _ := ba.children.Get(e)
		bal := child.(balancer.Balancer)
		if _, ok := endpointSet.Get(e); !ok {
			bal.Close()
			ba.children.Delete(e)
		}
	}
	return ret
}

// ResolverError forwards the resolver error to all of the BalancerAggregator's
// children, sends a single synchronous update of the childStates at the end of
// the ResolverError operation.
func (ba *BalancerAggregator) ResolverError(err error) {
	ba.inhibitChildUpdates = true
	defer func() {
		ba.inhibitChildUpdates = false
		ba.updateState()
	}()
	for _, child := range ba.children.Values() {
		bal := child.(balancer.Balancer)
		bal.ResolverError(err)
	}
}

func (ba *BalancerAggregator) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	// UpdateSubConnState is deprecated.
}

func (ba *BalancerAggregator) Close() {
	for _, child := range ba.children.Values() {
		bal := child.(balancer.Balancer)
		bal.Close()
	}
}

// updateState updates this components state. It sends the aggregated state, and
// a picker with round robin behavior with all the child states present if
// needed.
func (ba *BalancerAggregator) updateState() {
	if ba.inhibitChildUpdates {
		return
	}
	readyPickers := make([]balancer.Picker, 0)
	connectingPickers := make([]balancer.Picker, 0)
	idlePickers := make([]balancer.Picker, 0)
	transientFailurePickers := make([]balancer.Picker, 0)

	childStates := make([]ChildState, 0, ba.children.Len())
	for _, child := range ba.children.Values() {
		bw := child.(*balancerWrapper)
		childStates = append(childStates, bw.childState)
		childPicker := bw.childState.State.Picker
		switch bw.childState.State.ConnectivityState {
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
		pickers = append(pickers, base.NewErrPicker(errors.New("no children to pick from")))
	} // No children (resolver error before valid update).

	p := &pickerWithChildStates{
		pickers:     pickers,
		childStates: childStates,
		next:        uint32(grpcrand.Intn(len(pickers))),
	}
	ba.cc.UpdateState(balancer.State{
		ConnectivityState: aggState,
		Picker:            p,
	})
}

// pickerWithChildStates delegates to the pickers it holds in a round robin
// fashion. It also contains the childStates of all the BalancerAggregator's
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

// ChildStatesFromPicker returns the persisted child states from the picker if
// picker is of type pickerWithChildStates.
func ChildStatesFromPicker(picker balancer.Picker) []ChildState {
	p, ok := picker.(*pickerWithChildStates)
	if !ok {
		return nil
	}
	return p.childStates
}

// balancerWrapper is a wrapper of a balancer. It ID's a child balancer by
// endpoint, and persists recent child balancer state.
type balancerWrapper struct {
	balancer.Balancer   // Simply forward balancer.Balancer operations.
	balancer.ClientConn // embed to intercept UpdateState, doesn't deal with SubConns

	ba *BalancerAggregator

	childState ChildState
}

func (bw *balancerWrapper) UpdateState(state balancer.State) {
	bw.childState.State = state
	bw.ba.updateState()
}

func ParseConfig(cfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	return gracefulswitch.ParseConfig(cfg)
}

// PickFirstConfig is a pick first config without shuffling enabled.
const PickFirstConfig = "[{\"pick_first\": {}}]"
