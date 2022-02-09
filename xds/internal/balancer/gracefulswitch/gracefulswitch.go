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
	"google.golang.org/grpc/resolver"
)

var (
	errBalancerClosed = errors.New("gracefulSwitchBalancer is closed")
	defaultState      = balancer.State{
		ConnectivityState: connectivity.Connecting,
		Picker:            base.NewErrPicker(balancer.ErrNoSubConnAvailable),
	}
)

func newGracefulSwitchBalancer(cc balancer.ClientConn, opts balancer.BuildOptions) *gracefulSwitchBalancer {
	return &gracefulSwitchBalancer{
		cc:    cc,
		bOpts: opts,
	}
}

type gracefulSwitchBalancer struct {
	bOpts balancer.BuildOptions
	cc    balancer.ClientConn

	mu              sync.Mutex // also guards all fields within balancerCurrent and balancerPending
	balancerCurrent *balancerWrapper
	balancerPending *balancerWrapper
	closed          bool // set to true when this balancer is closed

	// currentMu must be locked before mu.
	currentMu sync.Mutex // protects operations on the balancerCurrent
}

// caller must hold gsb.mu.
func (gsb *gracefulSwitchBalancer) updateState(bw *balancerWrapper, state balancer.State) {
	if !gsb.balancerCurrentOrPending(bw) {
		return
	}

	// Nil and nil in the case of an inline UpdateState call and no current
	// balancer. Otherwise, gsb.balancerCurrent written to and will only hit if
	// the pointers match up, any other permutation of check (i.e. bw.Balancer
	// is (nil || pointer) and doesn't equal the current, then you know the
	// update is for pending.
	if bw == gsb.balancerCurrent {
		// In the case that the current balancer exits READY, and there is a pending
		// balancer, you can forward the pending balancers cached State up to
		// ClientConn and swap the pending into the current. This is because there
		// is no reason to gracefully switch from and keep using the old policy as
		// the ClientConn is not connected to any backends.
		if state.ConnectivityState != connectivity.Ready && gsb.balancerPending != nil {
			gsb.swap()
			return
		}
		// Even if there is a pending balancer waiting to be gracefully switched to,
		// continue to forward current balancer updates to the Client Conn. Ignoring
		// state + picker from the current would cause undefined behavior/cause the
		// system to behave incorrectly from the current LB policies perspective.
		// Also, the current LB is still being used by grpc to choose SubConns per
		// RPC, and thus should use the most updated form of the current balancer.
		gsb.cc.UpdateState(state)
		return
	}
	// If the current balancer is currently in a state other than READY, the
	// new policy can be swapped into place immediately. This is because
	// there is no reason to gracefully switch from and keep using the old
	// policy as the ClientConn is not connected to any backends.
	if state.ConnectivityState != connectivity.Connecting || gsb.balancerCurrent.lastState.ConnectivityState != connectivity.Ready {
		gsb.swap()
	}
}

// swap swaps out the current lb with the pending LB and updates the ClientConn.
// The caller must hold gsb.mu.
func (gsb *gracefulSwitchBalancer) swap() {
	gsb.cc.UpdateState(gsb.balancerPending.lastState)
	cur := gsb.balancerCurrent
	gsb.balancerCurrent = gsb.balancerPending
	gsb.balancerPending = nil
	for sc := range cur.subconns {
		gsb.cc.RemoveSubConn(sc)
	}
	go func() {
		gsb.currentMu.Lock()
		defer gsb.currentMu.Unlock()
		cur.Close()
	}()
}

// Helper function that checks if the balancer passed in is current or pending.
// The caller must hold gsb.mu.
func (gsb *gracefulSwitchBalancer) balancerCurrentOrPending(bw *balancerWrapper) bool {
	return bw == gsb.balancerCurrent || bw == gsb.balancerPending
}

// caller must hold gsb.mu.
func (gsb *gracefulSwitchBalancer) newSubConn(bw *balancerWrapper, addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	if !gsb.balancerCurrentOrPending(bw) {
		return nil, fmt.Errorf("%T at address %p that called NewSubConn is deleted", bw.Balancer, bw.Balancer)
	}
	sc, err := gsb.cc.NewSubConn(addrs, opts)
	if err != nil {
		return nil, err
	}
	return sc, nil
}

func (gsb *gracefulSwitchBalancer) resolveNow(bw *balancerWrapper, opts resolver.ResolveNowOptions) {
	// If the resolver specifies an update with a whole new type of policy,
	// there is no reason to forward a ResolveNow call from the balancer being
	// switched from, as the most recent update from the Resolver does not
	// concern the balancer being switched from.
	if bw == gsb.balancerPending || (bw == gsb.balancerCurrent && gsb.balancerPending == nil) {
		gsb.cc.ResolveNow(opts)
	}
}

// caller must hold gsb.mu.
func (gsb *gracefulSwitchBalancer) removeSubConn(bw *balancerWrapper, sc balancer.SubConn) {
	if !gsb.balancerCurrentOrPending(bw) {
		return
	}
	gsb.cc.RemoveSubConn(sc)
}

func (gsb *gracefulSwitchBalancer) updateAddresses(bw *balancerWrapper, sc balancer.SubConn, addrs []resolver.Address) {
	gsb.mu.Lock()
	defer gsb.mu.Unlock()
	if !gsb.balancerCurrentOrPending(bw) {
		return
	}
	gsb.cc.UpdateAddresses(sc, addrs)
}

// SwitchTo gracefully switches to the new balancer. This function must be
// called synchronously alongside the rest of the balancer.Balancer methods this
// Graceful Switch Balancer implements.
func (gsb *gracefulSwitchBalancer) SwitchTo(builder balancer.Builder) error {
	gsb.mu.Lock()
	if gsb.closed {
		gsb.mu.Unlock()
		return errBalancerClosed
	}
	bw := &balancerWrapper{
		gsb:       gsb,
		lastState: defaultState,
		subconns:  make(map[balancer.SubConn]bool),
	}
	var balToClose balancer.Balancer
	if gsb.balancerCurrent == nil {
		gsb.balancerCurrent = bw
	} else {
		// Clean up resources here that are from a previous pending lb.
		if gsb.balancerPending != nil {
			for sc := range gsb.balancerPending.subconns {
				gsb.cc.RemoveSubConn(sc)
			}
		}
		if gsb.balancerPending != nil {
			balToClose = gsb.balancerPending
		}
		gsb.balancerPending = bw
	}
	gsb.mu.Unlock()

	newBalancer := builder.Build(bw, gsb.bOpts)
	if newBalancer == nil {
		if balToClose != nil {
			balToClose.Close()
		}
		if gsb.balancerPending != nil {
			gsb.balancerPending = nil
		} else {
			gsb.balancerCurrent = nil
		}
		return balancer.ErrBadResolverState
	}
	bw.Balancer = newBalancer
	if balToClose != nil {
		balToClose.Close()
	}
	return nil
}

// Returns nil if the graceful switch balancer is closed.
func (gsb *gracefulSwitchBalancer) latestBalancer() balancer.Balancer {
	gsb.mu.Lock()
	defer gsb.mu.Unlock()
	if gsb.closed {
		return nil
	}
	if gsb.balancerPending != nil {
		return gsb.balancerPending
	}
	return gsb.balancerCurrent
}

// UpdateClientConnState simply forwards the update to one of it's children.
func (gsb *gracefulSwitchBalancer) UpdateClientConnState(state balancer.ClientConnState) error {
	balToUpdate := gsb.latestBalancer()
	if balToUpdate == nil {
		return errBalancerClosed
	}

	// Forwarding the update outside of holding gsb.mu prevents deadlock
	// scenarios, as the update can induce an inline callback which requires
	// gsb.mu. However, the current balancer can be closed at any time after
	// giving up gsb.mu. The pending balancer cannot be closed, as we are
	// guaranteed grpc will call the balancer API synchronously, and for the
	// pending to also be closed would require another update from grpc. The
	// current being closed is not a problem for this UpdateClientConnState()
	// call, as in the scenario that the current balancer can be closed (current
	// + pending balancers are both populated), this update will always be
	// forwarded to the pending. Thus, there is a guarantee that this will not
	// break the balancer API of the balancer by updating after closing.
	balToUpdate.UpdateClientConnState(state)
	return nil
}

func (gsb *gracefulSwitchBalancer) ResolverError(err error) {
	// The update will be forwarded to the pending balancer only if there is a
	// pending LB present, as that is the most recent LB Config prepared by the
	// resolver, and thus is a separate concern from the current.
	balToUpdate := gsb.latestBalancer()
	if balToUpdate == nil {
		return
	}
	// Forwarding the update outside of holding gsb.mu prevents deadlock
	// scenarios, as the update can induce an inline callback which requires
	// gsb.mu. However, the current balancer can be closed at any time after
	// giving up gsb.mu. The pending balancer cannot be closed, as we are
	// guaranteed grpc will call the balancer API synchronously, and for the
	// pending to also be closed would require another update from grpc. The
	// current being closed is not a problem for this ResolverError() call, as
	// in the scenario that the current balancer can be closed (current +
	// pending balancers are both populated), this error will always be
	// forwarded to the pending. Thus, there is a guarantee that this will not
	// break the balancer API of the balancer by updating after closing.
	balToUpdate.ResolverError(err)
}

func (gsb *gracefulSwitchBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	// At any given time, the current balancer may be closed in the situation
	// where there is a current balancer and a pending balancer. Thus, the
	// swap() (close() + clearing of SubCons) operation is also guarded by this
	// currentMu. Whether the current has been deleted or not will be checked
	// from reading from the scToSubBalancer map, as that will be cleared in the
	// atomic swap() operation. We are guaranteed that there can only be one
	// balancer closed within this UpdateSubConnState() call, as multiple
	// balancers closed would require another call to SwitchTo() which we are
	// guaranteed won't be called concurrently. UpdateSubConnState() is
	// different to UpdateClientConnState() and ResolverError(), as for those
	// functions you don't update the current balancer in the situation where
	// the current balancer could be closed (current + pending balancer
	// populated).
	gsb.currentMu.Lock()
	defer gsb.currentMu.Unlock()
	gsb.mu.Lock()
	if gsb.closed {
		gsb.mu.Unlock()
		return
	}
	// This SubConn update will forward to the current balancer even if there is
	// a pending present. This is because if this balancer does not forward the
	// update, the picker from the current will not be updated with SubConns,
	// leading to the possibility that the ClientConn constantly picks bad
	// SubConns in the Graceful Switch period.
	var balToUpdate balancer.Balancer
	if gsb.balancerCurrent != nil {
		if gsb.balancerCurrent.subconns[sc] {
			balToUpdate = gsb.balancerCurrent
		}
	}
	if gsb.balancerPending != nil {
		if gsb.balancerPending.subconns[sc] {
			balToUpdate = gsb.balancerPending
		}
	}
	if balToUpdate == nil {
		gsb.mu.Unlock()
		return
	}
	gsb.mu.Unlock()
	balToUpdate.UpdateSubConnState(sc, state)
}

func (gsb *gracefulSwitchBalancer) Close() {
	gsb.mu.Lock()
	gsb.closed = true
	if gsb.balancerCurrent != nil {
		for sc := range gsb.balancerCurrent.subconns {
			gsb.cc.RemoveSubConn(sc)
		}
	}
	if gsb.balancerPending != nil {
		for sc := range gsb.balancerPending.subconns {
			gsb.cc.RemoveSubConn(sc)
		}
	}
	currentBalancerToClose := gsb.balancerCurrent
	gsb.balancerCurrent = nil
	pendingBalancerToClose := gsb.balancerPending
	gsb.balancerPending = nil
	gsb.mu.Unlock()

	if currentBalancerToClose != nil {
		currentBalancerToClose.Close()
	}
	if pendingBalancerToClose != nil {
		pendingBalancerToClose.Close()
	}
}

func (gsb *gracefulSwitchBalancer) ExitIdle() {
	gsb.mu.Lock()
	defer gsb.mu.Unlock()
	if gsb.balancerCurrent != nil {
		if ei, ok := gsb.balancerCurrent.Balancer.(balancer.ExitIdler); ok {
			ei.ExitIdle()
		}
	}
	if gsb.balancerPending != nil {
		if ei, ok := gsb.balancerPending.Balancer.(balancer.ExitIdler); ok {
			ei.ExitIdle()
		}
	}
}

type balancerWrapper struct {
	balancer.Balancer
	gsb *gracefulSwitchBalancer

	lastState balancer.State
	subconns  map[balancer.SubConn]bool // subconns created by this balancer
}

func (bw *balancerWrapper) UpdateState(state balancer.State) {
	// Hold the mutex for this entire call to ensure it cannot occur
	// concurrently with other updateState() calls. This causes updates to
	// lastState and calls to cc.UpdateState to happen atomically.
	bw.gsb.mu.Lock()
	defer bw.gsb.mu.Unlock()
	bw.lastState = state
	bw.gsb.updateState(bw, state)
}

func (bw *balancerWrapper) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	bw.gsb.mu.Lock()
	defer bw.gsb.mu.Unlock()
	sc, err := bw.gsb.newSubConn(bw, addrs, opts)
	if err != nil {
		return nil, err
	}
	bw.subconns[sc] = true
	return sc, err
}

func (bw *balancerWrapper) ResolveNow(opts resolver.ResolveNowOptions) {
	bw.gsb.resolveNow(bw, opts)
}

func (bw *balancerWrapper) RemoveSubConn(sc balancer.SubConn) {
	bw.gsb.mu.Lock()
	defer bw.gsb.mu.Unlock()
	delete(bw.subconns, sc)
	bw.gsb.removeSubConn(bw, sc)
}

func (bw *balancerWrapper) UpdateAddresses(sc balancer.SubConn, addrs []resolver.Address) {
	bw.gsb.updateAddresses(bw, sc, addrs)
}

func (bw *balancerWrapper) Target() string {
	return bw.gsb.cc.Target()
}
