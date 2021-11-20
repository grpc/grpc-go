/*
 *
 * Copyright 2021 gRPC authors.
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

package rls

import (
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/resolver"
)

// childPolicyWrapper is a reference counted wrapper around a child policy.
//
// The LB policy maintains a map of these wrappers keyed by the target returned
// by RLS. When a target is seen for the first time, a child policy wrapper is
// created for it and the wrapper is added to the child policy map. Each entry
// in the data cache holds references to the corresponding child policy
// wrappers. The LB policy also holds a reference to the child policy wrapper
// for the default target specified in the LB Policy Configuration
//
// When a cache entry is evicted, it releases references to the child policy
// wrappers that it contains. When all references have been released, the
// wrapper is removed from the child policy map and is destroyed.
//
// The child policy wrapper also caches the connectivity state and most recent
// picker from the child policy. Once the child policy wrapper reports
// TRANSIENT_FAILURE, it will continue reporting that state until it goes READY;
// transitions from TRANSIENT_FAILURE to CONNECTING are be ignored.
//
// Whenever a child policy wrapper changes its connectivity state, the LB policy
// returns a new picker to the channel, since the channel may need to re-process
// the picks for queued RPCs.
//
// It is not safe for concurrent access.
type childPolicyWrapper struct {
	bg     balancerGroup // BalancerGroup to which this child policy belongs.
	logger *internalgrpclog.PrefixLogger

	target        string                     // RLS target corresponding to this child policy.
	targetField   string                     // Child policy config field to plug in the above target.
	config        map[string]json.RawMessage // Child policy config.
	builder       balancer.Builder           // Child policy builder to be passed to the balancer group.
	parser        balancer.ConfigParser      // Child policy config parser for building the config.
	resolverState resolver.State             // Resolver state as reported to the RLS LB policy.

	refCnt           int                // Reference count.
	state            balancer.State     // Balancer state reported by the child policy.
	stateToAggregate connectivity.State // Child policy state reported for aggregation.
}

// childPolicyWrapperArgs is simply a collection of the arguments to be passed
// to construct or update a child policy wrapper.
type childPolicyWrapperArgs struct {
	policyName    string
	target        string
	targetField   string
	config        map[string]json.RawMessage
	resolverState resolver.State
	bg            balancerGroup
}

// newChildPolicyWrapper creates a new child policy wrapper for the given
// arguments. The wrapper is initialized with no references and starts off in
// CONNECTING state.
//
// It is important to note that the child policy is not added to the balancer
// group at this point. It is added only when the reference to the child policy
// is taken.
func newChildPolicyWrapper(args childPolicyWrapperArgs) *childPolicyWrapper {
	// Config parsing ensures that the child policy is registered.
	builder := balancer.Get(args.policyName)
	parser, _ := builder.(balancer.ConfigParser)

	c := &childPolicyWrapper{
		target:        args.target,
		targetField:   args.targetField,
		config:        args.config,
		builder:       builder,
		parser:        parser,
		resolverState: args.resolverState,
		bg:            args.bg,

		refCnt: 0,
		state: balancer.State{
			ConnectivityState: connectivity.Connecting,
			Picker:            base.NewErrPicker(balancer.ErrNoSubConnAvailable),
		},
		stateToAggregate: connectivity.Connecting,
	}
	c.logger = internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf("[rls-child-policy-wrapper %s %p] ", c.target, c))
	c.logger.Infof("Created")
	return c
}

// handleNewConfigs handles updates to child policy configuration.
func (c *childPolicyWrapper) handleNewConfigs(args childPolicyWrapperArgs) {
	if c.builder.Name() != args.policyName {
		// If the child policy has changed, we need to remove the old policy
		// from the balancer group and add a new one. The balancer group takes
		// care of closing the old one in this case.
		c.builder = balancer.Get(args.policyName)
		c.parser, _ = c.builder.(balancer.ConfigParser)
		c.bg.Remove(c.target)
		c.bg.Add(c.target, c.builder)
	}
	c.targetField = args.targetField
	c.config = args.config
	c.resolverState = args.resolverState
	c.buildAndPushChildPolicyConfigs()
}

// buildAndPushChildPolicyConfigs builds the child policy configuration by
// adding the `targetField` to the provided configuration with the value set to
// `target`. It then pushes the new configuration to the child policy through
// the balancer group.
func (c *childPolicyWrapper) buildAndPushChildPolicyConfigs() (err error) {
	defer func() {
		if err != nil {
			c.lamify(err)
		}
	}()

	jsonTarget, err := json.Marshal(c.target)
	if err != nil {
		return fmt.Errorf("failed to marshal child policy target %q: %v", c.target, err)
	}
	c.config[c.targetField] = jsonTarget
	jsonCfg, err := json.Marshal(c.config)
	if err != nil {
		return fmt.Errorf("failed to marshal child policy config %+v: %v", c.config, err)
	}
	parsedCfg, err := c.parser.ParseConfig(jsonCfg)
	if err != nil {
		return fmt.Errorf("childPolicy config parsing failed: %v", err)
	}

	state := balancer.ClientConnState{ResolverState: c.resolverState, BalancerConfig: parsedCfg}
	c.logger.Infof("Pushing new state to child policy: %+v", state)
	if err := c.bg.UpdateClientConnState(c.target, state); err != nil {
		c.logger.Warningf("UpdateClientConnState(%q, %+v) failed : %v", c.target, state, err)
	}
	return nil
}

// acquireRef takes a reference to the child policy wrapper. It this is the
// first reference to the wrapper, the underlying child policy is added to the
// balancer group, and configs are pushed to it.
func (c *childPolicyWrapper) acquireRef() {
	c.refCnt++
	if c.refCnt != 1 {
		return
	}
	c.bg.Add(c.target, c.builder)
	c.logger.Infof("Added to balancergroup")
	c.buildAndPushChildPolicyConfigs()
}

// releaseRef releases a reference to the child policy wrapper. If this was the
// last reference to the wrapper, the underlying child policy is removed from
// the balancer group.
func (c *childPolicyWrapper) releaseRef() bool {
	c.refCnt--
	if c.refCnt != 0 {
		return false
	}
	c.bg.Remove(c.target)
	c.logger.Infof("Removed from balancergroup")
	return true
}

// lamify causes the child policy wrapper to return a picker which will always
// fail requests. This is used when the wrapper runs into errors when trying to
// build and parse the child policy configuration.
func (c *childPolicyWrapper) lamify(err error) {
	c.logger.Warningf("Entering lame mode: %v", err)
	c.state = balancer.State{
		ConnectivityState: connectivity.TransientFailure,
		Picker:            &lamePicker{err: err},
	}
	c.stateToAggregate = connectivity.TransientFailure
}

type lamePicker struct {
	err error
}

func (lp *lamePicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	return balancer.PickResult{}, lp.err
}

// An interface for balancer group functionality to enable unit testing.
type balancerGroup interface {
	Add(string, balancer.Builder)
	Remove(string)
	UpdateClientConnState(string, balancer.ClientConnState) error
}
