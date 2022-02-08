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

var errBalancerClosed = errors.New("gracefulSwitchBalancer is closed")

func newGracefulSwitchBalancer(cc balancer.ClientConn, opts balancer.BuildOptions) *gracefulSwitchBalancer {
	return &gracefulSwitchBalancer{
		cc:              cc,
		bOpts:           opts,
		scToSubBalancer: make(map[balancer.SubConn]balancer.Balancer),
	}
}

type gracefulSwitchBalancer struct {
	bOpts balancer.BuildOptions
	cc    balancer.ClientConn

	mu              sync.Mutex
	balancerCurrent balancer.Balancer
	balancerPending balancer.Balancer
	scToSubBalancer map[balancer.SubConn]balancer.Balancer
	// pendingState is the state of the pending balancer, and is used to update
	// the Client Conn in the scenario the pending balancer gets swapped to.
	pendingState     balancer.State
	currentLBIsReady bool
	closed           bool // set to true when this balancer is closed

	// currentMu must be locked before mu.
	currentMu sync.Mutex // protects operations on the balancerCurrent
}

func (gsb *gracefulSwitchBalancer) updateState(ccw *clientConnWrapper, state balancer.State) {
	// Hold the mutex for this entire call to ensure it cannot occur
	// concurrently with other updateState() calls. This causes updates to
	// pendingState and calls to cc.UpdateState to happen atomically.
	gsb.mu.Lock()
	defer gsb.mu.Unlock()

	if !gsb.balancerCurrentOrPending(ccw.bal) {
		return
	}

	// Nil and nil in the case of an inline UpdateState call and no current balancer.
	// Otherwise, gsb.balancerCurrent written to and will only hit if the pointers match
	// up, any other permutation of check (i.e. ccw.bal is (nil || pointer) and doesn't
	// equal the current, then you know the update is for pending.
	if ccw.bal == gsb.balancerCurrent {
		gsb.currentLBIsReady = state.ConnectivityState == connectivity.Ready
		// In the case that the current balancer exits READY, and there is a pending
		// balancer, you can forward the pending balancers cached State up to
		// ClientConn and swap the pending into the current. This is because there
		// is no reason to gracefully switch from and keep using the old policy as
		// the ClientConn is not connected to any backends.
		if !gsb.currentLBIsReady && gsb.balancerPending != nil {
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
	// Cache the pending state and picker if you don't need to send it at
	// this instant (i.e. the LB policy is connecting). This balancer can
	// send it later on the current LB exiting READY.
	gsb.pendingState = state
	// If the current balancer is currently in a state other than READY, the
	// new policy can be swapped into place immediately. This is because
	// there is no reason to gracefully switch from and keep using the old
	// policy as the ClientConn is not connected to any backends.
	if state.ConnectivityState != connectivity.Connecting || !gsb.currentLBIsReady {
		gsb.swap()
	}
}

// swap swaps out the current lb with the pending LB and updates the ClientConn.
// The caller must hold gsb.mu.
func (gsb *gracefulSwitchBalancer) swap() {
	gsb.cc.UpdateState(gsb.pendingState)
	currBalToClose := gsb.balancerCurrent
	gsb.balancerCurrent = gsb.balancerPending
	gsb.balancerPending = nil
	for sc, bal := range gsb.scToSubBalancer {
		if bal == currBalToClose {
			gsb.cc.RemoveSubConn(sc)
			delete(gsb.scToSubBalancer, sc)
		}
	}
	go func() {
		gsb.currentMu.Lock()
		defer gsb.currentMu.Unlock()
		currBalToClose.Close()
	}()
}

// Helper function that checks if the balancer passed in is current or pending.
// The caller must hold gsb.mu.
func (gsb *gracefulSwitchBalancer) balancerCurrentOrPending(bal balancer.Balancer) bool {
	return bal == gsb.balancerCurrent || bal == gsb.balancerPending
}

func (gsb *gracefulSwitchBalancer) newSubConn(ccw *clientConnWrapper, addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	gsb.mu.Lock()
	defer gsb.mu.Unlock()
	if !gsb.balancerCurrentOrPending(ccw.bal) {
		return nil, fmt.Errorf("%T at address %p that called NewSubConn is deleted", bal, bal)
	}
	sc, err := gsb.cc.NewSubConn(addrs, opts)
	if err != nil {
		return nil, err
	}
	gsb.scToSubBalancer[sc] = ccw.bal
	return sc, nil
}

func (gsb *gracefulSwitchBalancer) resolveNow(ccw *clientConnWrapper, opts resolver.ResolveNowOptions) {
	gsb.mu.Lock()
	defer gsb.mu.Unlock()
	if !gsb.balancerCurrentOrPending(ccw.bal) {
		return
	}

	// If the resolver specifies an update with a whole new type of policy,
	// there is no reason to forward a ResolveNow call from the balancer being
	// switched from, as the most recent update from the Resolver does not
	// concern the balancer being switched from.
	if ccw.bal == gsb.balancerCurrent && gsb.balancerPending != nil {
		return
	}
	gsb.cc.ResolveNow(opts)
}

func (gsb *gracefulSwitchBalancer) removeSubConn(ccw *clientConnWrapper, sc balancer.SubConn) {
	gsb.mu.Lock()
	defer gsb.mu.Unlock()
	if !gsb.balancerCurrentOrPending(ccw.bal) {
		return
	}
	delete(gsb.scToSubBalancer, sc)
	gsb.cc.RemoveSubConn(sc)
}

func (gsb *gracefulSwitchBalancer) updateAddresses(ccw *clientConnWrapper, sc balancer.SubConn, addrs []resolver.Address) {
	gsb.mu.Lock()
	defer gsb.mu.Unlock()
	if !gsb.balancerCurrentOrPending(ccw.bal) {
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
	ccw := &clientConnWrapper{
		gsb: gsb,
	}
	gsb.mu.Unlock()
	newBalancer := builder.Build(ccw, gsb.bOpts)
	if newBalancer == nil {
		// Can this even ever happen?
		return balancer.ErrBadResolverState
	}
	gsb.mu.Lock()
	ccw.bal = newBalancer
	for sc := range ccw.scsB {
		gsb.scToSubBalancer[sc] = ccw.bal
	}
	gsb.pendingState = balancer.State{
		ConnectivityState: connectivity.Connecting,
		Picker:            base.NewErrPicker(balancer.ErrNoSubConnAvailable),
	}
	if gsb.balancerCurrent == nil {
		gsb.balancerCurrent = newBalancer
		gsb.mu.Unlock()
		return nil
	}
	// Clean up resources here that are from a previous pending lb.
	for sc, sb := range gsb.scToSubBalancer {
		if sb == gsb.balancerPending {
			gsb.cc.RemoveSubConn(sc)
			delete(gsb.scToSubBalancer, sc)
		}
	}
	balToClose := gsb.balancerPending
	gsb.balancerPending = newBalancer
	gsb.mu.Unlock()
	if balToClose != nil {
		balToClose.Close()
	}
	return nil
}

// caller must hold gsb.mu.
func (gsb *gracefulSwitchBalancer) latestBalancer() balancer.Balancer {
	if gsb.balancerPending != nil {
		return gsb.balancerPending
	}
	return gsb.balancerCurrent
}

// UpdateClientConnState simply forwards the update to one of it's children.
func (gsb *gracefulSwitchBalancer) UpdateClientConnState(state balancer.ClientConnState) error {
	gsb.mu.Lock()
	if gsb.closed {
		gsb.mu.Unlock()
		return errBalancerClosed
	}
	balToUpdate := gsb.latestBalancer()
	gsb.mu.Unlock()

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
	gsb.mu.Lock()
	if gsb.closed {
		gsb.mu.Unlock()
		return
	}
	// The update will be forwarded to the pending balancer only if there is a
	// pending LB present, as that is the most recent LB Config prepared by the
	// resolver, and thus is a separate concern from the current.
	balToUpdate := gsb.latestBalancer()
	gsb.mu.Unlock()
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
	balToUpdate, ok := gsb.scToSubBalancer[sc]
	if !ok {
		gsb.mu.Unlock()
		return
	}
	if state.ConnectivityState == connectivity.Shutdown {
		delete(gsb.scToSubBalancer, sc)
	}
	gsb.mu.Unlock()
	balToUpdate.UpdateSubConnState(sc, state)
}

func (gsb *gracefulSwitchBalancer) Close() {
	gsb.mu.Lock()
	gsb.closed = true
	for sc := range gsb.scToSubBalancer {
		gsb.cc.RemoveSubConn(sc)
		delete(gsb.scToSubBalancer, sc)
	}
	currentBalancerToUpdate := gsb.balancerCurrent
	gsb.balancerCurrent = nil
	pendingBalancerToUpdate := gsb.balancerPending
	gsb.balancerPending = nil
	gsb.mu.Unlock()

	if currentBalancerToUpdate != nil {
		currentBalancerToUpdate.Close()
	}
	if pendingBalancerToUpdate != nil {
		pendingBalancerToUpdate.Close()
	}
}

func (gsb *gracefulSwitchBalancer) ExitIdle() {
	gsb.mu.Lock()
	defer gsb.mu.Unlock()
	if gsb.balancerCurrent != nil {
		if ei, ok := gsb.balancerCurrent.(balancer.ExitIdler); ok {
			ei.ExitIdle()
		}
	}
	if gsb.balancerPending != nil {
		if ei, ok := gsb.balancerPending.(balancer.ExitIdler); ok {
			ei.ExitIdle()
		}
	}
}

type clientConnWrapper struct {
	gsb *gracefulSwitchBalancer

	bal balancer.Balancer

	scsB map[balancer.SubConn]bool // subconns created during build
}

func (ccw *clientConnWrapper) UpdateState(state balancer.State) {
	ccw.gsb.updateState(ccw, state)
}

func (ccw *clientConnWrapper) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	sc, err := ccw.gsb.newSubConn(ccw, addrs, opts)
	if err != nil {
		return nil, err
	}
	ccw.gsb.mu.Lock()
	if ccw.bal == nil {
		ccw.scsB[sc] = true
	}
	ccw.gsb.mu.Unlock()
	return ccw.gsb.newSubConn(ccw, addrs, opts)
}

func (ccw *clientConnWrapper) ResolveNow(opts resolver.ResolveNowOptions) {
	ccw.gsb.resolveNow(ccw, opts)
}

func (ccw *clientConnWrapper) RemoveSubConn(sc balancer.SubConn) {
	ccw.gsb.mu.Lock()
	if ccw.bal == nil {
		delete(ccw.scsB, sc)
	}
	ccw.gsb.mu.Unlock()
	ccw.gsb.removeSubConn(ccw, sc)
}

func (ccw *clientConnWrapper) UpdateAddresses(sc balancer.SubConn, addrs []resolver.Address) {
	ccw.gsb.updateAddresses(ccw, sc, addrs)
}

func (ccw *clientConnWrapper) Target() string {
	return ccw.gsb.bOpts.Target.URL.String()
}
