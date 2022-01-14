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

/*
func (bb) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return &gracefulSwitchBalancer{ // Will need to just construct this inline by user/tests
		cc:              cc,
		bOpts:           opts,
		scToSubBalancer: make(map[balancer.SubConn]balancer.Balancer),
		closed:          grpcsync.NewEvent(),
	}
}
*/

type gracefulSwitchBalancer struct {
	bOpts          balancer.BuildOptions
	cc balancer.ClientConn

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
		if state.ConnectivityState != connectivity.Connecting /*switch to a state other than CONNECTING*/ || !gsb.currentLbIsReady { // "Otherwise, the channel will keep using the old policy until the new policy reports READY" - Java
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

func (gsb *gracefulSwitchBalancer) SwitchTo(childType string) error {
	if gsb.closed.HasFired() {
		// logger?
		return errBalancerClosed
	}

	// Now, the question is whether we compare to previous type here or always
	// switch to no matter what? If so you don't even need to persist
	// recentChildType Used to be nested in the if buildPolicy block of
	// UpdateClientConnState(). If you want to compare, have this be in that if
	// conditional (compared to a persisted recentChildType), if not, that if
	// conditional is determined by the caller.

	builder := balancer.Get(childType)
	if builder == nil {
		return balancer.ErrBadResolverState
		// return fmt.Errorf("balancer of type %v not supported", lbCfg.ChildBalancerType) // Maybe make this ErrBadResolverState as well?
	}
	gsb.mu.Lock()
	ccw := &clientConnWrapper{
		ClientConn: gsb.cc,
		gsb:        gsb,
	}
	newBalancer := builder.Build(ccw, gsb.bOpts)
	ccw.bal = newBalancer
	if gsb.balancerCurrent == nil {
		gsb.balancerCurrent = newBalancer
	} else {
		gsb.pendingState = balancer.State{}
		// Clean up resources here that are from a previous pending lb.
		for sc, sb := range gsb.scToSubBalancer {
			if sb == gsb.balancerPending {
				delete(gsb.scToSubBalancer, sc)
			}
		}
		gsb.balancerPending = newBalancer
	}
	gsb.mu.Unlock()
	return nil
}

// "Simply forwarding the update to one of the children"
func (gsb *gracefulSwitchBalancer) UpdateClientConnState(state balancer.ClientConnState) error {
	if gsb.closed.HasFired() {
		// logger?
		return errBalancerClosed
	}
	gsb.mu.Lock()
	var balToUpdate balancer.Balancer
	if gsb.balancerPending != nil {
		balToUpdate = gsb.balancerPending
	} else {
		balToUpdate = gsb.balancerCurrent
	}
	gsb.mu.Unlock()
	balToUpdate.UpdateClientConnState(state)
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

// ExitIdle()?

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