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
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/hierarchy"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/balancer/balancergroup"
)

const weightedTargetName = "weighted_target_experimental"

func init() {
	balancer.Register(&weightedTargetBB{})
}

type weightedTargetBB struct{}

func (wt *weightedTargetBB) Build(cc balancer.ClientConn, _ balancer.BuildOptions) balancer.Balancer {
	b := &weightedTargetBalancer{}
	b.logger = grpclog.NewPrefixLogger(loggingPrefix(b))
	b.bg = balancergroup.New(cc, nil, b.logger)
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
	bg *balancergroup.BalancerGroup

	targets map[string]target
}

// TODO: remove this and use strings directly as keys for balancer group.
func makeLocalityFromName(name string) internal.LocalityID {
	return internal.LocalityID{Region: name}
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

	// Remove sub-balancers that are not in the new config.
	for name := range w.targets {
		if _, ok := newConfig.Targets[name]; !ok {
			w.bg.Remove(makeLocalityFromName(name))
		}
	}

	// For sub-balancers in the new config
	// - if it's new. add to balancer group,
	// - if it's old, but has a new weight, update weight in balancer group.
	//
	// For all sub-balancers, forward the address/balancer config update.
	for name, newT := range newConfig.Targets {
		l := makeLocalityFromName(name)

		oldT, ok := w.targets[name]
		if !ok {
			// If this is a new sub-balancer, add it.
			w.bg.Add(l, newT.Weight, balancer.Get(newT.ChildPolicy.Name))
		} else if newT.Weight != oldT.Weight {
			// If this is an existing sub-balancer, update weight if necessary.
			w.bg.ChangeWeight(l, newT.Weight)
		}

		// Forwards all the update:
		// - Addresses are from the map after splitting with hierarchy path,
		// - Top level service config and attributes are the same,
		// - Balancer config comes from the targets map.
		//
		// TODO: handle error? How to aggregate errors and return?
		_ = w.bg.UpdateClientConnState(l, balancer.ClientConnState{
			ResolverState: resolver.State{
				Addresses:     addressesSplit[name],
				ServiceConfig: s.ResolverState.ServiceConfig,
				Attributes:    s.ResolverState.Attributes,
			},
			BalancerConfig: newT.ChildPolicy.Config,
		})
	}

	w.targets = newConfig.Targets
	return nil
}

func (w *weightedTargetBalancer) ResolverError(err error) {
	w.bg.ResolverError(err)
}

func (w *weightedTargetBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	w.bg.UpdateSubConnState(sc, state)
}

func (w *weightedTargetBalancer) Close() {
	w.bg.Close()
}

func (w *weightedTargetBalancer) HandleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	w.logger.Errorf("UpdateSubConnState should be called instead of HandleSubConnStateChange")
}

func (w *weightedTargetBalancer) HandleResolvedAddrs([]resolver.Address, error) {
	w.logger.Errorf("UpdateClientConnState should be called instead of HandleResolvedAddrs")
}
