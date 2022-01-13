/*
 *
 * Copyright 2022 gRPC authors.
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

// Package gracefulswitch implements a graceful switch load balancer.
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

const gracefulSwitchBalancerName = "graceful_switch_load_balancer"

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
	return gracefulSwitchBalancerName
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

	recentConfig *lbConfig
	balancerCurrent balancer.Balancer
	balancerPending balancer.Balancer
	// One mutex...protecting incoming (updateState happening atomically,
	// scToSubBalancer reads/writes, and also reads from
	// balancerCurrent/Pending...which will put into a local variable to call
	// downward into, thus making it so that no lock is held when call downward).
	mu sync.Mutex
	scToSubBalancer map[balancer.SubConn]balancer.Balancer
	pendingState balancer.State
	currentLbIsReady bool

	closed *grpcsync.Event
}

func (gsb *gracefulSwitchBalancer) updateState(bal balancer.Balancer, state balancer.State) {
	if gsb.closed.HasFired() {
		return
	}

	// I want this to not intersplice with other updateState() calls and for
	// this to happen atomically (i.e. if this update isn't atomic writing to
	// pending state concurrently and sending wrong one out for one
	// updateState() call).
	gsb.mu.Lock()
	defer gsb.mu.Unlock()

	if bal == gsb.balancerPending {
		// Cache the pending state and picker if you don't need to send at this instant (i.e. the LB policy is not ready)
		// you can send it later on an event like current LB exits READY.
		gsb.pendingState = state
		// "If the channel is currently in a state other than READY, the new policy will be swapped into place immediately."
		if state.ConnectivityState == connectivity.Ready || !gsb.currentLbIsReady { // "Otherwise, the channel will keep using the old policy until the new policy reports READY" - Java
			gsb.swap()
		}
		// Note: no-op if comes from balancer that is already deleted
	} else if bal == gsb.balancerCurrent { // Make a note that this copies Java behavior on swapping on exiting ready and also forwarding current updates to Client Conn even if there is pending lb present (me and Doug discussed this - if ignoring state + picker from current would cause undefined behavior/cause the system to behave incorrectly from the LB policies perspective)
		// specific case that the current lb exits ready, and there is a pending
		// lb, can forward it up to ClientConn. "Otherwise, the channel will
		// keep using the old policy until...the old policy exits READY." - Java
		gsb.currentLbIsReady = state.ConnectivityState == connectivity.Ready
		if !gsb.currentLbIsReady && gsb.balancerPending != nil {
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
			// Make a note that this copies Java behavior on swapping on exiting
			// ready and also forwarding current updates to Client Conn even if
			// there is pending lb present (me and Doug discussed this - if
			// ignoring state + picker from current would cause undefined
			// behavior/cause the system to behave incorrectly from the LB
			// policies perspective)
		}
	}
}

// Swap swaps out the current lb with the pending LB and updates the ClientConn.
func (gsb *gracefulSwitchBalancer) swap() {
	gsb.cc.UpdateState(gsb.pendingState)
	gsb.balancerCurrent.Close()
	for sc, bal := range gsb.scToSubBalancer {
		if bal == gsb.balancerCurrent {
			gsb.cc.RemoveSubConn(sc)
			delete(gsb.scToSubBalancer, sc)
		}
	}

	gsb.balancerCurrent = gsb.balancerPending
	gsb.balancerPending = nil
}

func (gsb *gracefulSwitchBalancer) newSubConn(bal balancer.Balancer, addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	if gsb.closed.HasFired() {
		// logger?
		return nil, errBalancerClosed
	}
	gsb.mu.Lock()
	if bal != gsb.balancerCurrent || bal != gsb.balancerPending { // Update came from a balancer no longer present.
		return nil, errors.New("balancer that called NewSubConn is deleted")
	}
	sc, err := gsb.cc.NewSubConn(addrs, opts)
	if err != nil {
		gsb.mu.Unlock()
		return nil, err
	}
	gsb.scToSubBalancer[sc] = bal
	gsb.mu.Unlock()
	return sc, nil
}

// Eat newSubConn calls from current if there is a pending?

// Eat NewAddresses calls from current if there is a pending? (Allowing ClientConn to do work that doesn't need)

func (gsb *gracefulSwitchBalancer) resolveNow(bal balancer.Balancer, opts resolver.ResolveNowOptions) {
	// If the resolver specifies an update with a whole new type of policy,
	// there is no reason to forward a ResolveNow call from the balancer being
	// switched from, as the most recent update from the Resolver does not
	// concern the balancer being switched from.
	gsb.mu.Lock()
	if bal == gsb.balancerCurrent && gsb.balancerPending != nil {
		gsb.mu.Unlock()
		return
	}
	gsb.mu.Unlock()
	gsb.cc.ResolveNow(opts)
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
		print("wtf")
		// b.logger.Warningf("xds: unexpected LoadBalancingConfig type: %T", state.BalancerConfig)
		return balancer.ErrBadResolverState
	}
	gsb.mu.Lock()
	buildPolicy := gsb.balancerCurrent == nil || gsb.recentConfig.ChildBalancerType != lbCfg.ChildBalancerType
	gsb.recentConfig = lbCfg
	var balToUpdate balancer.Balancer // Hold a local variable pointer to the balancer, as current/pending pointers can be changed synchronously.
	if buildPolicy {
		builder := balancer.Get(lbCfg.ChildBalancerType)
		if builder == nil {
			gsb.mu.Unlock()
			return balancer.ErrBadResolverState
			// return fmt.Errorf("balancer of type %v not supported", lbCfg.ChildBalancerType) // Maybe make this ErrBadResolverState as well?
		}
		if gsb.balancerCurrent == nil {
			balToUpdate = gsb.balancerCurrent
		} else {
			balToUpdate = gsb.balancerPending
		}
		ccw := &clientConnWrapper{
			ClientConn: gsb.cc,
			gsb:        gsb, // BalToUpdate will get 0, 0, and be done, need to write to this later // Type and value, so I think this is right
		}
		balToUpdate = builder.Build(ccw, gsb.bOpts)
		ccw.bal = balToUpdate
		print("wow")
		// i.e. * = *, or does it write to the memory this pointer points to
		//Are we sure this doesn't just write to a local var? Learn inherent go struct pointer and how that works
		print(balToUpdate)
		print(gsb.balancerCurrent)
		if gsb.balancerCurrent == nil {
			gsb.balancerCurrent = balToUpdate
		} else {
			// Clear out pendingState when updating balancerPending...this is
			// logically equivalent to swapping without having received an
			// UpdateState() call, which is valid. Also clear out
			// scToSubbalancer that corresponds to pendingBalancer.
			gsb.pendingState = balancer.State{}
			for sc, sb := range gsb.scToSubBalancer {
				if sb == gsb.balancerPending {
					delete(gsb.scToSubBalancer, sc)
				}
			}
			gsb.balancerPending = balToUpdate
		}
	} else {
		if gsb.balancerPending != nil {
			balToUpdate = gsb.balancerPending
		} else {
			balToUpdate = gsb.balancerCurrent
		}
	}
	gsb.mu.Unlock()
	balToUpdate.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: state.ResolverState,
		BalancerConfig: lbCfg.Config,
	})
	return nil
}

func (gsb *gracefulSwitchBalancer) ResolverError(err error) {
	if gsb.closed.HasFired() {
		// Logger?
		return
	}
	gsb.mu.Lock()
	currentBalancerToUpdate := gsb.balancerCurrent
	pendingBalancerToUpdate := gsb.balancerPending
	gsb.mu.Unlock()
	if currentBalancerToUpdate != nil {
		currentBalancerToUpdate.ResolverError(err)
	}
	if pendingBalancerToUpdate != nil {
		pendingBalancerToUpdate.ResolverError(err)
	}
}

func (gsb *gracefulSwitchBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	if gsb.closed.HasFired() {
		// Logger
		return
	}
	gsb.mu.Lock()
	bal, ok := gsb.scToSubBalancer[sc]
	if !ok {
		gsb.mu.Unlock()
		return
	}
	if state.ConnectivityState == connectivity.Shutdown {
		delete(gsb.scToSubBalancer, sc)
	}
	gsb.mu.Unlock()
	bal.UpdateSubConnState(sc, state)
}

func (gsb *gracefulSwitchBalancer) Close() {
	gsb.closed.Fire()
	gsb.mu.Lock()
	for sc := range gsb.scToSubBalancer {
		gsb.cc.RemoveSubConn(sc)
		delete(gsb.scToSubBalancer, sc)
	}
	currentBalancerToUpdate := gsb.balancerCurrent
	if gsb.balancerCurrent != nil {
		gsb.balancerCurrent = nil
	}
	pendingBalancerToUpdate := gsb.balancerPending
	if gsb.balancerPending != nil {
		gsb.balancerPending.Close()
		gsb.balancerPending = nil
	}
	gsb.mu.Unlock()

	if currentBalancerToUpdate != nil {
		currentBalancerToUpdate.Close()
	}
	if pendingBalancerToUpdate != nil {
		pendingBalancerToUpdate.Close()
	}
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

func (ccw *clientConnWrapper) ResolveNow(opts resolver.ResolveNowOptions) {
	ccw.gsb.resolveNow(ccw.bal, opts)
}