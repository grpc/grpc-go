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

// Package weightedtarget implements the weighted_target balancer.
package weightedtarget

import (
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/hierarchy"
	"google.golang.org/grpc/internal/wrr"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/balancergroup"
	"google.golang.org/grpc/xds/internal/balancer/weightedtarget/weightedaggregator"
)

const weightedTargetName = "weighted_target_experimental"

// newRandomWRR is the WRR constructor used to pick sub-pickers from
// sub-balancers. It's to be modified in tests.
var newRandomWRR = wrr.NewRandom

func init() {
	balancer.Register(&weightedTargetBB{})
}

type weightedTargetBB struct{}

func (wt *weightedTargetBB) Build(cc balancer.ClientConn, _ balancer.BuildOptions) balancer.Balancer {
	b := &weightedTargetBalancer{}
	b.logger = prefixLogger(b)
	b.stateAggregator = weightedaggregator.New(cc, b.logger, newRandomWRR)
	b.stateAggregator.Start()
	b.bg = balancergroup.New(cc, b.stateAggregator, nil, b.logger)
	b.bg.Start()
	b.logger.Infof("Created")
	return b
}

func (wt *weightedTargetBB) Name() string {
	return weightedTargetName
}

func (wt *weightedTargetBB) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	return parseConfig(c)
}

type weightedTargetBalancer struct {
	logger *grpclog.PrefixLogger

	// TODO: Make this package not dependent on any xds specific code.
	// BalancerGroup uses xdsinternal.LocalityID as the key in the map of child
	// policies that it maintains and reports load using LRS. Once these two
	// dependencies are removed from the balancerGroup, this package will not
	// have any dependencies on xds code.
	bg              *balancergroup.BalancerGroup
	stateAggregator *weightedaggregator.Aggregator

	targets map[string]target
}

// UpdateClientConnState takes the new targets in balancer group,
// creates/deletes sub-balancers and sends them update. Addresses are split into
// groups based on hierarchy path.
func (w *weightedTargetBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	newConfig, ok := s.BalancerConfig.(*lbConfig)
	if !ok {
		return fmt.Errorf("unexpected balancer config with type: %T", s.BalancerConfig)
	}
	addressesSplit := hierarchy.Group(s.ResolverState.Addresses)

	var rebuildStateAndPicker bool

	// Remove sub-pickers and sub-balancers that are not in the new config.
	for name := range w.targets {
		if _, ok := newConfig.Targets[name]; !ok {
			w.stateAggregator.Remove(name)
			w.bg.Remove(name)
			// Trigger a state/picker update, because we don't want `ClientConn`
			// to pick this sub-balancer anymore.
			rebuildStateAndPicker = true
		}
	}

	// For sub-balancers in the new config
	// - if it's new. add to balancer group,
	// - if it's old, but has a new weight, update weight in balancer group.
	//
	// For all sub-balancers, forward the address/balancer config update.
	for name, newT := range newConfig.Targets {
		oldT, ok := w.targets[name]
		if !ok {
			// If this is a new sub-balancer, add weights to the picker map.
			w.stateAggregator.Add(name, newT.Weight)
			// Then add to the balancer group.
			w.bg.Add(name, balancer.Get(newT.ChildPolicy.Name))
			// Not trigger a state/picker update. Wait for the new sub-balancer
			// to send its updates.
		} else if newT.Weight != oldT.Weight {
			// If this is an existing sub-balancer, update weight if necessary.
			w.stateAggregator.UpdateWeight(name, newT.Weight)
			// Trigger a state/picker update, because we don't want `ClientConn`
			// should do picks with the new weights now.
			rebuildStateAndPicker = true
		}

		// Forwards all the update:
		// - Addresses are from the map after splitting with hierarchy path,
		// - Top level service config and attributes are the same,
		// - Balancer config comes from the targets map.
		//
		// TODO: handle error? How to aggregate errors and return?
		_ = w.bg.UpdateClientConnState(name, balancer.ClientConnState{
			ResolverState: resolver.State{
				Addresses:     addressesSplit[name],
				ServiceConfig: s.ResolverState.ServiceConfig,
				Attributes:    s.ResolverState.Attributes,
			},
			BalancerConfig: newT.ChildPolicy.Config,
		})
	}

	w.targets = newConfig.Targets

	if rebuildStateAndPicker {
		w.stateAggregator.BuildAndUpdate()
	}
	return nil
}

func (w *weightedTargetBalancer) ResolverError(err error) {
	w.bg.ResolverError(err)
}

func (w *weightedTargetBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	w.bg.UpdateSubConnState(sc, state)
}

func (w *weightedTargetBalancer) Close() {
	w.stateAggregator.Stop()
	w.bg.Close()
}
