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
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
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
// transitions from TRANSIENT_FAILURE to CONNECTING are ignored.
//
// Whenever a child policy wrapper changes its connectivity state, the LB policy
// returns a new picker to the channel, since the channel may need to re-process
// the picks for queued RPCs.
//
// It is not safe for concurrent access.
type childPolicyWrapper struct {
	logger *internalgrpclog.PrefixLogger
	target string // RLS target corresponding to this child policy.
	refCnt int    // Reference count.

	// Balancer state reported by the child policy. The RLS LB policy maintains
	// these child policies in a BalancerGroup. The state reported by the child
	// policy is pushed to the state aggregator (which is also implemented by the
	// RLS LB policy) and cached here. See handleChildPolicyStateUpdate() for
	// details on how the state aggregation is performed.
	state balancer.State
}

// newChildPolicyWrapper creates a child policy wrapper for the given target,
// and is initialized with one reference and starts off in CONNECTING state.
func newChildPolicyWrapper(target string) *childPolicyWrapper {
	c := &childPolicyWrapper{
		target: target,
		refCnt: 1,
		state: balancer.State{
			ConnectivityState: connectivity.Connecting,
			Picker:            base.NewErrPicker(balancer.ErrNoSubConnAvailable),
		},
	}
	c.logger = internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf("[rls-child-policy-wrapper %s %p] ", c.target, c))
	c.logger.Infof("Created")
	return c
}

// acquireRef increments the reference count on the child policy wrapper.
func (c *childPolicyWrapper) acquireRef() {
	c.refCnt++
}

// releaseRef decrements the reference count on the child policy wrapper. The
// return value indicates whether the released reference was the last one.
func (c *childPolicyWrapper) releaseRef() bool {
	c.refCnt--
	return c.refCnt == 0
}

// lamify causes the child policy wrapper to return a picker which will always
// fail requests. This is used when child policy config validation fails and we
// want the wrapper to return a picker which fails all RPCs.
func (c *childPolicyWrapper) lamify(err error) {
	c.logger.Warningf("Entering lame mode: %v", err)
	c.state = balancer.State{
		ConnectivityState: connectivity.TransientFailure,
		Picker:            &lamePicker{err: err},
	}
}

type lamePicker struct {
	err error
}

func (lp *lamePicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	return balancer.PickResult{}, lp.err
}
