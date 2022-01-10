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

// Package graceful switch implements a graceful switch load balancer.
package gracefulswitch

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

var errBalancerClosed = errors.New("gracefulSwitchBalancer is closed")

const balancerName = "graceful_switch_load_balancer"

func init() {
	balancer.Register(bb{})
}

type bb struct{}

func (bb) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return &gracefulSwitchBalancer{
		cc: cc,
		bOpts: opts,
		scToSubBalancer: make(map[balancer.SubConn]balancer.Balancer),
		closed: grpcsync.NewEvent(),
	}
}

func (bb) Name() string {
	return balancerName
}

type lbConfig struct {
	serviceconfig.LoadBalancingConfig
	ChildBalancerType string
	Config serviceconfig.LoadBalancingConfig
}

type intermediateConfig struct {
	serviceconfig.LoadBalancingConfig
	ChildBalancerType string
	ChildConfigJSON json.RawMessage
}

func (bb) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	var intermediateCfg intermediateConfig
	if err := json.Unmarshal(c, &intermediateCfg); err != nil {
		return nil, fmt.Errorf("graceful switch: unable to unmarshal lbconfig: %s, error: %v", string(c), err)
	}
	builder := balancer.Get(intermediateCfg.ChildBalancerType)
	if builder == nil {
		return nil, fmt.Errorf("balancer of type %v not supported", intermediateCfg.ChildBalancerType)
	}
	parsedChildCfg, err := builder.(balancer.ConfigParser).ParseConfig(intermediateCfg.ChildConfigJSON)
	if err != nil {
		return nil, fmt.Errorf("graceful switch: unable to unmarshal lbconfig: %s of type %v, error: %v", string(intermediateCfg.ChildConfigJSON), intermediateCfg.ChildBalancerType, err)
	}
	return &lbConfig{
		ChildBalancerType: intermediateCfg.ChildBalancerType,
		Config: parsedChildCfg,
	}, nil
}

type gracefulSwitchBalancer struct {
	bOpts          balancer.BuildOptions
	cc balancer.ClientConn

	outgoingMu sync.Mutex
	recentConfig *lbConfig
	balancerCurrent balancer.Balancer
	balancerPending balancer.Balancer

	incomingMu sync.Mutex
	scToSubBalancer map[balancer.SubConn]balancer.Balancer
	pendingState balancer.State

	closed *grpcsync.Event
}

func (gsb *gracefulSwitchBalancer) updateState(bal balancer.Balancer, state balancer.State) {
	if gsb.closed.HasFired() {
		return
	}

	// I want this to not intersplice with other updateState() calls and for
	// this to happen atomically (i.e. writing to pending state concurrently and
	// sending wrong one out). Can any codepaths call updateState() while
	// holding either of these mutexes?
	gsb.incomingMu.Lock()
	defer gsb.incomingMu.Unlock()

	gsb.outgoingMu.Lock()
	if bal == gsb.balancerPending {
		gsb.outgoingMu.Unlock()
		// TODO: Java has a case here: If the channel is currently in a state other than READY, the new policy will be swapped into place immediately.

		// Cache the pending state and picker if you don't need to send at this instant (i.e. the LB policy is not ready)
		// you can send it later on an event like current LB exits READY.
		gsb.pendingState = state
		if state.ConnectivityState == connectivity.Ready { // "Otherwise, the channel will keep using the old policy until the new policy reports READY" - Java
			// Close() and also "remove subconns it created", loop through map?
			// Swap the pending to current
			// Def update the Client Conn so that the Client Conn can use the new picker
			// All of the logic is in this function
			gsb.swap()
		}
		// Note: no-op if comes from balancer that is already deleted
	} else if bal == gsb.balancerCurrent { // Make a note that this copies Java behavior on swapping on exiting ready and also forwarding current updates to Client Conn even if there is pending lb present
		gsb.outgoingMu.Unlock()
		// specific case that the current lb exits ready, and there is a pending
		// lb, can forward it up to ClientConn. "Otherwise, the channel will
		// keep using the old policy until...the old policy exits READY." - Java
		if state.ConnectivityState != connectivity.Ready && gsb.balancerPending != nil {
			gsb.swap()
		} else {
			// Java forwards the current balancer's update to the Client Conn
			// even if there is a pending balancer waiting to be gracefully
			// switched to, whereas c-core ignores updates from the current
			// balancer. I agree with the Java more, as the current LB is still
			// being used by RPC's until the pending balancer gets gracefully
			// switched to, and thus should use the most updated form of the
			// current balancer (UpdateClientConnState seems to subscribe to
			// this philosophy too - maybe make it consistent?)
			gsb.cc.UpdateState(state)
		}

	}
}

// Swap swaps out the current lb with the pending LB and updates the ClientConn.
func (gsb *gracefulSwitchBalancer) swap() {
	gsb.cc.UpdateState(gsb.pendingState)
	gsb.outgoingMu.Lock()
	gsb.balancerCurrent.Close()
	for sc, bal := range gsb.scToSubBalancer {
		if bal == gsb.balancerCurrent {
			gsb.cc.RemoveSubConn(sc)
			delete(gsb.scToSubBalancer, sc)
		}
	}

	gsb.balancerCurrent = gsb.balancerPending
	gsb.balancerPending = nil
	gsb.outgoingMu.Unlock()
}

func (gsb *gracefulSwitchBalancer) newSubConn(bal balancer.Balancer, addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	if gsb.closed.HasFired() {
		// logger?
		return nil, errBalancerClosed
	}
	if bal != gsb.balancerCurrent || bal != gsb.balancerPending { // Update came from a balancer no longer present.
		return nil, errors.New("balancer that called NewSubConn is deleted")
	}
	sc, err := gsb.cc.NewSubConn(addrs, opts)
	if err != nil {
		return nil, err
	}
	gsb.incomingMu.Lock() // Do we need to move this ^^^ before ClientConn read NewSubConn call?
	gsb.scToSubBalancer[sc] = bal
	gsb.incomingMu.Unlock()
	return sc, nil
}

func (gsb *gracefulSwitchBalancer) UpdateClientConnState(state balancer.ClientConnState) error {
	if gsb.closed.HasFired() {
		// logger?
		return errBalancerClosed
	}

	// First case described in c-core: we have no existing child policy, in this
	// case, create a new child policy and store it in current child policy.


	// Second case described in c-core:
	// Existing Child Policy (i.e. BalancerConfig == nil) but no pending

	// a. If config is same type...simply pass update down to existing policy

	// b. If config is different type...create a new policy and store in pending
	// child policy. This will be swapped into child policy once the new child
	// transitions into state READY.

	// Third case described in c-core:
	// We have an existing child policy nad a pending child policy from a previous update (i.e. Pending not transitioned into state READY)

	// a. If going from current config to new config does not require a new policy, update existing pending child policy.

	// b. If going from current to new does require a new, create a new policy, and replace the pending policy.

	lbCfg, ok := state.BalancerConfig.(*lbConfig)
	if !ok {
		// b.logger.Warningf("xds: unexpected LoadBalancingConfig type: %T", state.BalancerConfig)
		return balancer.ErrBadResolverState
	}
	gsb.outgoingMu.Lock()
	buildPolicy := gsb.balancerCurrent == nil || gsb.recentConfig.ChildBalancerType != lbCfg.ChildBalancerType
	gsb.recentConfig = lbCfg
	var balToUpdate balancer.Balancer // Hold a local variable pointer to the balancer, as current/pending pointers can be changed synchronously.
	if buildPolicy {
		builder := balancer.Get(lbCfg.ChildBalancerType)
		if builder == nil {
			gsb.outgoingMu.Unlock()
			return fmt.Errorf("balancer of type %v not supported", lbCfg.ChildBalancerType)
		}
		if gsb.balancerCurrent == nil {
			balToUpdate = gsb.balancerCurrent
		} else {
			balToUpdate = gsb.balancerPending
		}
		bal := builder.Build(&clientConnWrapper{
			ClientConn: gsb.cc,
			gsb:        gsb,
			bal:        balToUpdate,
		}, gsb.bOpts)
		balToUpdate = bal
	} else {
		if gsb.balancerPending != nil {
			balToUpdate = gsb.balancerPending // TODO: Should we update current with new resolver state as well? Doug mentioned so, would keep it consistent with Java's way of not ignoring current on UpdateState() like C-core.
		} else {
			balToUpdate = gsb.balancerCurrent
		}
	}
	gsb.outgoingMu.Unlock() // Unlock before calling downward to prevent deadlock in Callback to UpdateState().
	balToUpdate.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: state.ResolverState,
		BalancerConfig: lbCfg.LoadBalancingConfig,
	})
	return nil
}

func (gsb *gracefulSwitchBalancer) ResolverError(err error) {
	if gsb.closed.HasFired() {
		// Logger?
		return
	}
	gsb.outgoingMu.Lock()
	if gsb.balancerCurrent != nil { // Can this cause deadlock (i.e. can this call back into UpdateState() inline)? If so do it like UpdateClientConnState where you read to a local variable
		gsb.balancerCurrent.ResolverError(err)
	}
	if gsb.balancerPending != nil {
		gsb.balancerPending.ResolverError(err)
	}
	gsb.outgoingMu.Unlock()
}

func (gsb *gracefulSwitchBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	if gsb.closed.HasFired() {
		// Logger
		return
	}
	gsb.incomingMu.Lock()
	bal, ok := gsb.scToSubBalancer[sc]
	if !ok {
		gsb.incomingMu.Unlock()
		return
	}
	if state.ConnectivityState == connectivity.Shutdown {
		delete(gsb.scToSubBalancer, sc)
	}
	gsb.incomingMu.Unlock()
	gsb.outgoingMu.Lock() // Do we need this? Now, will this cause deadlock (call back into update state)
	bal.UpdateSubConnState(sc, state)
	gsb.outgoingMu.Unlock()
}

func (gsb *gracefulSwitchBalancer) Close() {
	gsb.closed.Fire()

	gsb.incomingMu.Lock()
	for sc := range gsb.scToSubBalancer {
		gsb.cc.RemoveSubConn(sc)
		delete(gsb.scToSubBalancer, sc)
	}
	gsb.incomingMu.Unlock()

	gsb.outgoingMu.Lock()
	if gsb.balancerCurrent != nil {
		gsb.balancerCurrent.Close() // Can this cause deadlock (i.e. can this call back into UpdateState() inline or just Add/RemoveSubconns())? If so do it like UpdateClientConnState where you read to a local variable
		gsb.balancerCurrent = nil
	}
	if gsb.balancerPending != nil {
		gsb.balancerPending.Close()
		gsb.balancerPending = nil
	}
	gsb.outgoingMu.Unlock()
}

type clientConnWrapper struct {
	balancer.ClientConn

	gsb *gracefulSwitchBalancer

	bal balancer.Balancer
}

func (ccw *clientConnWrapper) UpdateState(state balancer.State) {
	ccw.gsb.updateState(ccw.bal, state)
}

func (ccw *clientConnWrapper) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	return ccw.gsb.newSubConn(ccw.bal, addrs, opts)
}
