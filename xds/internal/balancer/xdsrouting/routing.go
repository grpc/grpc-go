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

package xdsrouting

import (
	"encoding/json"
	"fmt"
	"regexp"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/hierarchy"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/balancergroup"
)

const xdsRoutingName = "xds_routing_experimental"

func init() {
	balancer.Register(&routingBB{})
}

type routingBB struct{}

func (rbb *routingBB) Build(cc balancer.ClientConn, _ balancer.BuildOptions) balancer.Balancer {
	b := &routingBalancer{}
	b.logger = prefixLogger(b)
	b.stateAggregator = newBalancerStateAggregator(cc, b.logger)
	b.stateAggregator.start()
	b.bg = balancergroup.New(cc, b.stateAggregator, nil, b.logger)
	b.bg.Start()
	b.logger.Infof("Created")
	return b
}

func (rbb *routingBB) Name() string {
	return xdsRoutingName
}

func (rbb *routingBB) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	return parseConfig(c)
}

type route struct {
	action string
	m      *compositeMatcher
}

func (r route) String() string {
	return r.m.String() + "->" + r.action
}

type routingBalancer struct {
	logger *grpclog.PrefixLogger

	// TODO: make this package not dependent on xds specific code. Same as for
	// weighted target balancer.
	bg              *balancergroup.BalancerGroup
	stateAggregator *balancerStateAggregator

	actions map[string]actionConfig
	routes  []route
}

func (rb *routingBalancer) updateActions(s balancer.ClientConnState, newConfig *lbConfig) (needRebuild bool) {
	addressesSplit := hierarchy.Group(s.ResolverState.Addresses)
	var rebuildStateAndPicker bool

	// Remove sub-pickers and sub-balancers that are not in the new action list.
	for name := range rb.actions {
		if _, ok := newConfig.actions[name]; !ok {
			rb.stateAggregator.remove(name)
			rb.bg.Remove(name)
			// Trigger a state/picker update, because we don't want `ClientConn`
			// to pick this sub-balancer anymore.
			rebuildStateAndPicker = true
		}
	}

	// For sub-balancers in the new action list,
	// - add to balancer group if it's new,
	// - forward the address/balancer config update.
	for name, newT := range newConfig.actions {
		if _, ok := rb.actions[name]; !ok {
			// If this is a new sub-balancer, add weights to the picker map.
			rb.stateAggregator.add(name)
			// Then add to the balancer group.
			rb.bg.Add(name, balancer.Get(newT.ChildPolicy.Name))
			// Not trigger a state/picker update. Wait for the new sub-balancer
			// to send its updates.
		}
		// Forwards all the update:
		// - Addresses are from the map after splitting with hierarchy path,
		// - Top level service config and attributes are the same,
		// - Balancer config comes from the targets map.
		//
		// TODO: handle error? How to aggregate errors and return?
		_ = rb.bg.UpdateClientConnState(name, balancer.ClientConnState{
			ResolverState: resolver.State{
				Addresses:     addressesSplit[name],
				ServiceConfig: s.ResolverState.ServiceConfig,
				Attributes:    s.ResolverState.Attributes,
			},
			BalancerConfig: newT.ChildPolicy.Config,
		})
	}

	rb.actions = newConfig.actions
	return rebuildStateAndPicker
}

func routeToMatcher(r routeConfig) (*compositeMatcher, error) {
	var pathMatcher pathMatcherInterface
	switch {
	case r.regex != "":
		re, err := regexp.Compile(r.regex)
		if err != nil {
			return nil, fmt.Errorf("failed to compile regex %q", r.regex)
		}
		pathMatcher = newPathRegexMatcher(re)
	case r.path != "":
		pathMatcher = newPathExactMatcher(r.path, r.caseInsensitive)
	default:
		pathMatcher = newPathPrefixMatcher(r.prefix, r.caseInsensitive)
	}

	var headerMatchers []headerMatcherInterface
	for _, h := range r.headers {
		var matcherT headerMatcherInterface
		switch {
		case h.exactMatch != "":
			matcherT = newHeaderExactMatcher(h.name, h.exactMatch)
		case h.regexMatch != "":
			re, err := regexp.Compile(h.regexMatch)
			if err != nil {
				return nil, fmt.Errorf("failed to compile regex %q, skipping this matcher", h.regexMatch)
			}
			matcherT = newHeaderRegexMatcher(h.name, re)
		case h.prefixMatch != "":
			matcherT = newHeaderPrefixMatcher(h.name, h.prefixMatch)
		case h.suffixMatch != "":
			matcherT = newHeaderSuffixMatcher(h.name, h.suffixMatch)
		case h.rangeMatch != nil:
			matcherT = newHeaderRangeMatcher(h.name, h.rangeMatch.start, h.rangeMatch.end)
		default:
			matcherT = newHeaderPresentMatcher(h.name, h.presentMatch)
		}
		if h.invertMatch {
			matcherT = newInvertMatcher(matcherT)
		}
		headerMatchers = append(headerMatchers, matcherT)
	}

	var fractionMatcher *fractionMatcher
	if r.fraction != nil {
		fractionMatcher = newFractionMatcher(*r.fraction)
	}
	return newCompositeMatcher(pathMatcher, headerMatchers, fractionMatcher), nil
}

func routesEqual(a, b []route) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		aa := a[i]
		bb := b[i]
		if aa.action != bb.action {
			return false
		}
		if !aa.m.equal(bb.m) {
			return false
		}
	}
	return true
}

func (rb *routingBalancer) updateRoutes(newConfig *lbConfig) (needRebuild bool, _ error) {
	var newRoutes []route
	for _, rt := range newConfig.routes {
		newMatcher, err := routeToMatcher(rt)
		if err != nil {
			return false, err
		}
		newRoutes = append(newRoutes, route{action: rt.action, m: newMatcher})
	}
	rebuildStateAndPicker := !routesEqual(newRoutes, rb.routes)
	rb.routes = newRoutes

	if rebuildStateAndPicker {
		rb.stateAggregator.updateRoutes(rb.routes)
	}
	return rebuildStateAndPicker, nil
}

func (rb *routingBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	newConfig, ok := s.BalancerConfig.(*lbConfig)
	if !ok {
		return fmt.Errorf("unexpected balancer config with type: %T", s.BalancerConfig)
	}
	rb.logger.Infof("update with config %+v, resolver state %+v", s.BalancerConfig, s.ResolverState)

	rebuildForActions := rb.updateActions(s, newConfig)
	rebuildForRoutes, err := rb.updateRoutes(newConfig)
	if err != nil {
		return fmt.Errorf("xds_routing balancer: failed to update routes: %v", err)
	}

	if rebuildForActions || rebuildForRoutes {
		rb.stateAggregator.buildAndUpdate()
	}
	return nil
}

func (rb *routingBalancer) ResolverError(err error) {
	rb.bg.ResolverError(err)
}

func (rb *routingBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	rb.bg.UpdateSubConnState(sc, state)
}

func (rb *routingBalancer) Close() {
	rb.stateAggregator.close()
	rb.bg.Close()
}
