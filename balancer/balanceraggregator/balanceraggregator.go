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
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/balancer/gracefulswitch"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

// ChildState is the balancer state of the child along with the
// endpoint which ID's the child balancer.
type ChildState struct {
	Endpoint resolver.Endpoint
	State    balancer.State
}

// Parent is a balancer.ClientConn that can also receive
// child state updates.
type Parent interface {
	balancer.ClientConn
	UpdateChildState(childStates []ChildState)
}

// Build returns a new BalancerAggregator.
func Build(parent Parent, opts balancer.BuildOptions) *BalancerAggregator {
	return &BalancerAggregator{
		parent:   parent,
		bOpts:    opts,
		children: resolver.NewEndpointMap(),
	}
}

// BalancerAggregator is a balancer that wraps child balancers. It creates a
// child balancer with child config for every Endpoint received. It updates the
// child states on any update from parent or child.
type BalancerAggregator struct {
	parent Parent
	bOpts  balancer.BuildOptions

	children *resolver.EndpointMap

	inhibitChildUpdates bool
}

// UpdateClientConnState creates a child for new endpoints, deletes children for
// endpoints that are no longer present. It also updates all the children, and
// sends a single synchronous update of the childStates at the end of
// the UpdateClientConnState operation.
func (ba *BalancerAggregator) UpdateClientConnState(state balancer.ClientConnState) error {
	endpointSet := resolver.NewEndpointMap()
	ba.inhibitChildUpdates = true
	defer func() {
		ba.inhibitChildUpdates = false
		ba.updateChildStates()
	}()
	// Update/Create new children.
	for _, endpoint := range state.ResolverState.Endpoints {
		endpointSet.Set(endpoint, nil)
		var bal *balancerWrapper
		if child, ok := ba.children.Get(endpoint); ok {
			bal = child.(*balancerWrapper)
		} else {
			bal = &balancerWrapper{
				childState: ChildState{Endpoint: endpoint},
				ClientConn: ba.parent,
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
		}); err != nil {
			return fmt.Errorf("error updating child balancer: %v", err)
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
	return nil
}

// ResolverError forwards the resolver error to all of the BalancerAggregator's
// children, sends a single synchronous update of the childStates at the end of
// the ResolverError operation.
func (ba *BalancerAggregator) ResolverError(err error) {
	ba.inhibitChildUpdates = true
	defer func() {
		ba.inhibitChildUpdates = false
		ba.updateChildStates()
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

// updateChildStates updates the parents with all of the childStates for all of
// the BalancerAggregator's children.
func (ba *BalancerAggregator) updateChildStates() {
	if ba.inhibitChildUpdates {
		return
	}

	childStates := make([]ChildState, 0, ba.children.Len())
	for _, child := range ba.children.Values() {
		bw := child.(*balancerWrapper)
		childStates = append(childStates, bw.childState)
	}
	ba.parent.UpdateChildState(childStates)
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
	bw.ba.updateChildStates()
}

func ParseConfig(cfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	return gracefulswitch.ParseConfig(cfg)
}

// PickFirstConfig is a pick first config without shuffling enabled.
const PickFirstConfig = "[{\"pick_first\": {}}]"
