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
	"errors"
	"fmt"
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/resolver"
)

var errBalancerClosed = errors.New("gracefulSwitchBalancer is closed")

func newGracefulSwitchBalancer(cc balancer.ClientConn, opts balancer.BuildOptions) *gracefulSwitchBalancer {
	return &gracefulSwitchBalancer{
		cc:              cc,
		bOpts:           opts,
		scToSubBalancer: make(map[balancer.SubConn]balancer.Balancer),
		closed:          grpcsync.NewEvent(),
		pendingState: balancer.State{
			ConnectivityState: connectivity.Connecting,
			Picker:            base.NewErrPicker(balancer.ErrNoSubConnAvailable),
		},
	}
}

type gracefulSwitchBalancer struct {
	bOpts balancer.BuildOptions
	cc    balancer.ClientConn

	mu               sync.Mutex
	balancerCurrent  balancer.Balancer
	balancerPending  balancer.Balancer
	scToSubBalancer  map[balancer.SubConn]balancer.Balancer
	pendingState     balancer.State
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

	if !gsb.balancerCurrentOrPending(bal) {
		return
	}

	if bal == gsb.balancerPending {
		// Cache the pending state and picker if you don't need to send it at
		// this instant (i.e. the LB policy is not ready) you can send it later
		// on an event like current LB exits READY.
		gsb.pendingState = state
		// "If the channel is currently in a state other than READY, the new
		// policy will be swapped into place immediately." - Java
		// implementation.
		if state.ConnectivityState != connectivity.Connecting || !gsb.currentLbIsReady {
			gsb.swap()
		}
	} else { // Make a note that this copies Java behavior on swapping on exiting ready and also forwarding current updates to Client Conn even if there is pending lb present (me and Doug discussed this - if ignoring state + picker from current would cause undefined behavior/cause the system to behave incorrectly from the LB policies perspective)
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

// swap swaps out the current lb with the pending LB and updates the ClientConn.
// The caller must hold gsb.mu.
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

// Helper function that checks if the balancer passed in is current or pending.
// The caller must hold gsb.mu.
func (gsb *gracefulSwitchBalancer) balancerCurrentOrPending(bal balancer.Balancer) bool {
	return bal == gsb.balancerCurrent || bal == gsb.balancerPending
}

func (gsb *gracefulSwitchBalancer) newSubConn(bal balancer.Balancer, addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	if gsb.closed.HasFired() {
		// logger?
		return nil, errBalancerClosed
	}
	gsb.mu.Lock()
	defer gsb.mu.Unlock()
	if !gsb.balancerCurrentOrPending(bal) {
		return nil, fmt.Errorf("%T at address %p that called NewSubConn is deleted", bal, bal)
	}
	sc, err := gsb.cc.NewSubConn(addrs, opts)
	if err != nil {
		return nil, err
	}
	gsb.scToSubBalancer[sc] = bal
	return sc, nil
}

// Eat newSubConn calls from current if there is a pending?

// Eat NewAddresses calls from current if there is a pending? (Allowing ClientConn to do work that doesn't need)
// close guard for the three functions below?
func (gsb *gracefulSwitchBalancer) resolveNow(bal balancer.Balancer, opts resolver.ResolveNowOptions) {
	gsb.mu.Lock()
	defer gsb.mu.Unlock()
	if !gsb.balancerCurrentOrPending(bal) {
		return
	}

	// If the resolver specifies an update with a whole new type of policy,
	// there is no reason to forward a ResolveNow call from the balancer being
	// switched from, as the most recent update from the Resolver does not
	// concern the balancer being switched from.
	if bal == gsb.balancerCurrent && gsb.balancerPending != nil {
		return
	}
	gsb.cc.ResolveNow(opts)
}

func (gsb *gracefulSwitchBalancer) removeSubConn(bal balancer.Balancer, sc balancer.SubConn) {
	gsb.mu.Lock()
	defer gsb.mu.Unlock()
	if bal != gsb.balancerCurrent && bal != gsb.balancerPending {
		return
	}
	gsb.cc.RemoveSubConn(sc) // Is this right? Should we really be intercepting this and checking if current or pending? I feel like this will be incorrect on Close() calls
}

func (gsb *gracefulSwitchBalancer) updateAddresses(bal balancer.Balancer, sc balancer.SubConn, addrs []resolver.Address) {
	gsb.mu.Lock()
	// "Call comes from balancer which is neither current or pending"
	if bal != gsb.balancerCurrent && bal != gsb.balancerPending {
		gsb.mu.Unlock()
		return
	}
	gsb.mu.Unlock()
	// close() and clearing from swap() (and thus making this update come from a
	// balancer neither current or pending) can get interspliced here before
	// UpdateAddresses() - however, doesn't seem to be a problem with regard to
	// correctness.
	gsb.cc.UpdateAddresses(sc, addrs) // Can this call back inline?
}

func (gsb *gracefulSwitchBalancer) SwitchTo(builder balancer.Builder) error {
	if gsb.closed.HasFired() {
		// logger?
		return errBalancerClosed
	}
	gsb.mu.Lock()
	ccw := &clientConnWrapper{
		ClientConn: gsb.cc,
		gsb:        gsb,
	}
	newBalancer := builder.Build(ccw, gsb.bOpts)
	if newBalancer == nil {
		// Can this even ever happen?
		return balancer.ErrBadResolverState
	}
	ccw.bal = newBalancer
	if gsb.balancerCurrent == nil {
		gsb.balancerCurrent = newBalancer
		gsb.mu.Unlock()
		return nil
	}
	var balToClose balancer.Balancer
	gsb.pendingState = balancer.State{
		ConnectivityState: connectivity.Connecting,
		Picker:            base.NewErrPicker(balancer.ErrNoSubConnAvailable),
	}
	// Clean up resources here that are from a previous pending lb.
	for sc, sb := range gsb.scToSubBalancer {
		if sb == gsb.balancerPending {
			delete(gsb.scToSubBalancer, sc)
		}
	}
	// Need to test this by verifying somehow.
	balToClose = gsb.balancerPending
	gsb.balancerPending = newBalancer
	gsb.mu.Unlock()
	if balToClose != nil {
		balToClose.Close()
	}
	return nil
}

// UpdateClientConnState() simply forwards the update to one of it's children.
func (gsb *gracefulSwitchBalancer) UpdateClientConnState(state balancer.ClientConnState) error {
	if gsb.closed.HasFired() {
		// logger?
		return errBalancerClosed
	}
	gsb.mu.Lock()
	balToUpdate := gsb.balancerPending
	if balToUpdate == nil {
		balToUpdate = gsb.balancerCurrent
	}
	gsb.mu.Unlock()
	balToUpdate.UpdateClientConnState(state)
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
	// The update will be forwarded to the pending balancer only if there is a
	// pending LB present, as that is the most recent LB Config prepared by the
	// resolver, and thus is a separate concern from the current.
	if currentBalancerToUpdate != nil && pendingBalancerToUpdate == nil {
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
