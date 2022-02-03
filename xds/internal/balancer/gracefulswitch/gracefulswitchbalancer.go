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

	// currentMu protects operations on the current balancer.
	currentMu sync.Mutex

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
		// this instant (i.e. the LB policy is connecting). This balancer can
		// send it later on the current LB exiting READY.
		gsb.pendingState = state
		// "If the channel is currently in a state other than READY, the new
		// policy will be swapped into place immediately." - Java
		// implementation.
		if state.ConnectivityState != connectivity.Connecting || !gsb.currentLbIsReady {
			gsb.swap()
		}
		return
	}
	gsb.currentLbIsReady = state.ConnectivityState == connectivity.Ready
	// In the specific case that the current lb exits ready, and there is a
	// pending lb, you can forward the pending LB's cached State up to
	// ClientConn and swap the pending into the current. "Otherwise, the
	// channel will keep using the old policy until...the old policy exits
	// READY." - Java
	if !gsb.currentLbIsReady && gsb.balancerPending != nil {
		gsb.swap()
		return
	}
	// Even if there is a pending balancer waiting to be gracefully
	// switched to, still forward current balancer updates to the Client
	// Conn. Ignoring state + picker from the current would cause
	// undefined behavior/cause the system to behave incorrectly from
	// the current LB policies perspective. Also, the current LB is
	// still being used by grpc to choose SubConns per RPC, and thus
	// should use the most updated form of the current balancer.
	gsb.cc.UpdateState(state)
}

// swap swaps out the current lb with the pending LB and updates the ClientConn.
// The caller must hold gsb.mu.
func (gsb *gracefulSwitchBalancer) swap() {
	gsb.cc.UpdateState(gsb.pendingState)
	currBalToClose := gsb.balancerCurrent
	go func() {
		gsb.currentMu.Lock()
		defer gsb.currentMu.Unlock()
		currBalToClose.Close()
		gsb.mu.Lock()
		defer gsb.mu.Unlock()
		for sc, bal := range gsb.scToSubBalancer {
			if bal == gsb.balancerCurrent {
				gsb.cc.RemoveSubConn(sc)
				delete(gsb.scToSubBalancer, sc)
			}
		}

		gsb.balancerCurrent = gsb.balancerPending
		gsb.balancerPending = nil
	}()
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
	if !gsb.balancerCurrentOrPending(bal) {
		return
	}
	delete(gsb.scToSubBalancer, sc)
	gsb.cc.RemoveSubConn(sc)
}

func (gsb *gracefulSwitchBalancer) updateAddresses(bal balancer.Balancer, sc balancer.SubConn, addrs []resolver.Address) {
	gsb.mu.Lock()
	defer gsb.mu.Unlock()
	if !gsb.balancerCurrentOrPending(bal) {
		return
	}
	gsb.cc.UpdateAddresses(sc, addrs)
}

// SwitchTo gracefully switches to the new balancer. This function must be
// called synchronously alongside the rest of the balancer.Balancer methods this
// Graceful Switch Balancer implements.
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
			gsb.cc.RemoveSubConn(sc)
			delete(gsb.scToSubBalancer, sc)
		}
	}
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
	// The reason we read to a local var balToUpdate is to prevent potential
	// deadlocks, as calling downward can lead to inline call backs back into
	// this balancer. Thus, if we update one of balancer within gsb.mu, this can
	// induce deadlock because it can callback into a function that also
	// requires gsb.mu. However, in between reading into a local var pointer and
	// updating that local var pointer, the current balancer can potentially be
	// closed in an updateState call. The reason the pending cannot also be
	// closed is we are guaranteed that grpc will call the balancer API
	// synchronously, and for pending to be deleted would require another
	// SwitchTo(), which is outside of the state space of the potential
	// scenarios within the context of this single UpdateClientConnState() call.
	// However, in the scenario that the current can possibly be deleted
	// (current + pending balancers are both populated), this Update will always
	// go to the pending. Thus, there is a guarantee that we never break the API
	// guarantee for the current balancer by closing before updating.
	balToUpdate.UpdateClientConnState(state)
	return nil
}

func (gsb *gracefulSwitchBalancer) ResolverError(err error) {
	if gsb.closed.HasFired() {
		// Logger?
		return
	}
	gsb.mu.Lock()
	// The update will be forwarded to the pending balancer only if there is a
	// pending LB present, as that is the most recent LB Config prepared by the
	// resolver, and thus is a separate concern from the current.
	balToUpdate := gsb.balancerPending
	if balToUpdate == nil {
		balToUpdate = gsb.balancerCurrent
	}
	gsb.mu.Unlock()
	// The reason we read to a local var balToUpdate is to prevent potential
	// deadlocks, as calling downward can lead to inline call backs back into
	// this balancer. Thus, if we update one of balancer within gsb.mu, this can
	// induce deadlock because it can callback into a function that also
	// requires gsb.mu. However, in between reading into a local var pointer and
	// updating that local var pointer, the current balancer can potentially be
	// closed in an updateState call. The reason the pending cannot also be
	// closed is we are guaranteed that grpc will call the balancer API
	// synchronously, and for pending to be deleted would require another
	// SwitchTo(), which is outside of the state space of the potential
	// scenarios within the context of this single UpdateClientConnState() call.
	// However, in the scenario that the current can possibly be deleted
	// (current + pending balancers are both populated), this Update will always
	// go to the pending. Thus, there is a guarantee that we never break the API
	// guarantee for the current balancer by closing before updating.
	balToUpdate.ResolverError(err)
}

func (gsb *gracefulSwitchBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	if gsb.closed.HasFired() {
		// Logger
		return
	}
	// At any given time, the current balancer may be closed in the situation
	// where there is a current balancer and a pending balancer. Thus, the
	// swap() (close() + clearing of SubCons) operation is also guarded by this
	// currentMu. Whether the current has been deleted or not will be checked
	// from reading from the scToSubBalancer map, as that will be cleared in the
	// atomic swap() operation. We are guaranteed that there can only be one
	// balancer closed within this UpdateSubConnState() call, as multiple
	// balancers closed would require another call to UpdateClientConnState()
	// which we are guaranteed won't be called concurrently.
	// UpdateSubConnState() is different to UpdateClientConnState() and
	// ResolverError(), as for those functions you don't update the current
	// balancer in the situation where the current balancer could be closed
	// (current + pending balancer populated).
	gsb.currentMu.Lock()
	defer gsb.currentMu.Unlock()
	gsb.mu.Lock()
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

func (ccw *clientConnWrapper) RemoveSubConn(sc balancer.SubConn) {
	ccw.gsb.removeSubConn(ccw.bal, sc)
}

func (ccw *clientConnWrapper) UpdateAddresses(sc balancer.SubConn, addrs []resolver.Address) {
	ccw.gsb.updateAddresses(ccw.bal, sc, addrs)
}
