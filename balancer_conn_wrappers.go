/*
 *
 * Copyright 2017 gRPC authors.
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

package grpc

import (
	"fmt"
	"strings"
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/balancer/gracefulswitch"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/resolver"
)

// ccBalancerWrapper is a wrapper on top of cc for balancers. It ensures that
// method invocations on the underlying balancer happen synchronously and in the
// same order in which they were received from grpc.
//
// It uses the gracefulswitch.Balancer internally to ensure that balancer
// switches happen in a graceful manner.
//
// It implements balancer.ClientConn interface.
type ccBalancerWrapper struct {
	cc *ClientConn

	// balancerMu protects access to the following fields. Any calls on the
	// underlying balancer must be made with the mutex held. This ensures that we
	// never call the underlying balancer methods concurrently.
	balancerMu      sync.Mutex
	balancer        *gracefulswitch.Balancer
	curBalancerName string

	updateCh *buffer.Unbounded // Updates written on this channel are processed by watcher().
	resultCh *buffer.Unbounded // Results of calls to UpdateClientConnState() are pushed here.
	closed   *grpcsync.Event   // Indicates if close has been called.
	done     *grpcsync.Event   // Indicates if close has completed its work.

	mu       sync.Mutex
	subConns map[*acBalancerWrapper]struct{}
}

// newCCBalancerWrapper creates a new balancer wrapper. The underlying balancer
// is not created until the switchTo() method is invoked.
func newCCBalancerWrapper(cc *ClientConn, bopts balancer.BuildOptions) *ccBalancerWrapper {
	ccb := &ccBalancerWrapper{
		cc:       cc,
		updateCh: buffer.NewUnbounded(),
		resultCh: buffer.NewUnbounded(),
		closed:   grpcsync.NewEvent(),
		done:     grpcsync.NewEvent(),
		subConns: make(map[*acBalancerWrapper]struct{}),
	}
	go ccb.watcher()
	ccb.balancer = gracefulswitch.NewBalancer(ccb, bopts)
	return ccb
}

// updateType indicates the type of update pushed to the watcher goroutine.
type updateType int

const (
	updateTypeClientConnState updateType = iota // clientConn state change from grpc
	updateTypeSubConnState                      // subConn state change from grpc
	updateTypeExitIdle                          // exitIdle from grpc
	updateTypeResolverError                     // resolver error from grpc
	updateTypeSwitchTo                          // balancer switch update from grpc
	updateTypeSubConn                           // removeSubConn from the balancer
)

// watcherUpdate wraps the actual update to be passed to the watcher goroutine
// with a type indicating the kind of update being wrapped.
type watcherUpdate struct {
	typ    updateType
	update interface{}
}

// scStateUpdate contains the subConn and the new state it changed to.
type scStateUpdate struct {
	sc    balancer.SubConn
	state connectivity.State
	err   error
}

// watcher is a long-running goroutine which reads updates from a channel and
// invokes corresponding methods on the underlying balancer. It ensures that
// these methods are invoked in a synchronous fashion. It also ensures that
// these methods are invoked in the order in which the updates were received.
func (ccb *ccBalancerWrapper) watcher() {
	for {
		select {
		case u := <-ccb.updateCh.Get():
			ccb.updateCh.Load()
			if ccb.closed.HasFired() {
				break
			}
			update := u.(watcherUpdate)
			switch update.typ {
			case updateTypeClientConnState:
				ccb.handleClientConnStateChange(update.update.(*balancer.ClientConnState))
			case updateTypeSubConnState:
				ccb.handleSubConnStateChange(update.update.(*scStateUpdate))
			case updateTypeExitIdle:
				ccb.handleExitIdle()
			case updateTypeResolverError:
				ccb.handleResolverError(update.update.(error))
			case updateTypeSwitchTo:
				ccb.handleSwitchTo(update.update.(string))
			case updateTypeSubConn:
				ccb.handleRemoveSubConn(update.update.(*acBalancerWrapper))
			default:
				logger.Errorf("ccBalancerWrapper.watcher: unknown update %+v, type %T", update, update)
			}
		case <-ccb.closed.Done():
		}

		if ccb.closed.HasFired() {
			ccb.handleClose()
			return
		}
	}
}

// updateClientConnState is invoked by grpc to push a ClientConnState update to
// the underlying balancer.
//
// Unlike other methods invoked by grpc to push updates to the underlying
// balancer, this method cannot simply push the update onto the update channel
// and return. It needs to return the error returned by the underlying balancer
// back to grpc which propagates that to the resolver.
func (ccb *ccBalancerWrapper) updateClientConnState(ccs *balancer.ClientConnState) error {
	if ccb == nil {
		return nil
	}
	ccb.updateCh.Put(watcherUpdate{typ: updateTypeClientConnState, update: ccs})
	res := <-ccb.resultCh.Get()
	ccb.resultCh.Load()
	// If the returned error is nil, attempting to type assert to error leads to
	// panic. So, this needs to handled separately.
	if res == nil {
		return nil
	}
	return res.(error)
}

// handleClientConnStateChange handles a ClientConnState update from the update
// channel and invokes the appropriate method on the underlying balancer.
func (ccb *ccBalancerWrapper) handleClientConnStateChange(ccs *balancer.ClientConnState) {
	ccb.balancerMu.Lock()
	defer ccb.balancerMu.Unlock()

	if ccb.curBalancerName != grpclbName {
		// Filter any grpclb addresses since we don't have the grpclb balancer.
		var addrs []resolver.Address
		for _, addr := range ccs.ResolverState.Addresses {
			if addr.Type == resolver.GRPCLB {
				continue
			}
			addrs = append(addrs, addr)
		}
		ccs.ResolverState.Addresses = addrs
	}
	ccb.resultCh.Put(ccb.balancer.UpdateClientConnState(*ccs))
}

// updateSubConnState is invoked by grpc to push a subConn state update to the
// underlying balancer.
func (ccb *ccBalancerWrapper) updateSubConnState(sc balancer.SubConn, s connectivity.State, err error) {
	// When updating addresses for a SubConn, if the address in use is not in
	// the new addresses, the old ac will be tearDown() and a new ac will be
	// created. tearDown() generates a state change with Shutdown state, we
	// don't want the balancer to receive this state change. So before
	// tearDown() on the old ac, ac.acbw (acWrapper) will be set to nil, and
	// this function will be called with (nil, Shutdown). We don't need to call
	// balancer method in this case.
	if sc == nil {
		return
	}
	ccb.updateCh.Put(watcherUpdate{typ: updateTypeSubConnState, update: &scStateUpdate{
		sc:    sc,
		state: s,
		err:   err,
	}})
}

// handleSubConnStateChange handles a SubConnState update from the update
// channel and invokes the appropriate method on the underlying balancer.
func (ccb *ccBalancerWrapper) handleSubConnStateChange(update *scStateUpdate) {
	ccb.balancerMu.Lock()
	ccb.balancer.UpdateSubConnState(update.sc, balancer.SubConnState{ConnectivityState: update.state, ConnectionError: update.err})
	ccb.balancerMu.Unlock()
}

func (ccb *ccBalancerWrapper) exitIdle() {
	if ccb == nil || ccb.cc.GetState() != connectivity.Idle {
		return
	}
	ccb.updateCh.Put(watcherUpdate{typ: updateTypeExitIdle})
}

func (ccb *ccBalancerWrapper) handleExitIdle() {
	ccb.balancerMu.Lock()
	ccb.balancer.ExitIdle()
	ccb.balancerMu.Unlock()
}

func (ccb *ccBalancerWrapper) resolverError(err error) {
	if ccb == nil {
		return
	}
	ccb.updateCh.Put(watcherUpdate{typ: updateTypeResolverError, update: err})
}

func (ccb *ccBalancerWrapper) handleResolverError(err error) {
	ccb.balancerMu.Lock()
	ccb.balancer.ResolverError(err)
	ccb.balancerMu.Unlock()
}

// switchTo is invoked by grpc to instruct the balancer wrapper to switch to the
// LB policy identified by name.
//
// ClientConn calls newCCBalancerWrapper() at creation time. Upon receipt of the
// first good update from the name resolver, it determines the LB policy to use
// and invokes the switchTo() method. Upon receipt of every subsequent update
// from the name resolver, it invokes this method.
//
// the ccBalancerWrapper keeps track of the current LB policy name, and skips
// the graceful balancer switching process if the name does not change.
func (ccb *ccBalancerWrapper) switchTo(name string) {
	if ccb == nil {
		return
	}
	ccb.updateCh.Put(watcherUpdate{typ: updateTypeSwitchTo, update: name})
}

// handleSwitchTo handles a balancer switch update from the update channel. It
// calls the SwitchTo() method on the gracefulswitch.Balancer with a
// balancer.Builder corresponding to name. If no balancer.Builder is registered
// for the given name, it uses the default LB policy which is "pick_first".
func (ccb *ccBalancerWrapper) handleSwitchTo(name string) {
	ccb.balancerMu.Lock()
	defer ccb.balancerMu.Unlock()

	if strings.EqualFold(ccb.curBalancerName, name) {
		return
	}

	channelz.Infof(logger, ccb.cc.channelzID, "ClientConn switching balancer to %q", name)
	builder := balancer.Get(name)
	if builder == nil {
		channelz.Warningf(logger, ccb.cc.channelzID, "Channel switches to new LB policy %q due to fallback from invalid balancer name", PickFirstBalancerName)
		channelz.Infof(logger, ccb.cc.channelzID, "failed to get balancer builder for: %v, using pick_first instead", name)
		builder = newPickfirstBuilder()
	} else {
		channelz.Infof(logger, ccb.cc.channelzID, "Channel switches to new LB policy %q", name)
	}

	if err := ccb.balancer.SwitchTo(builder); err != nil {
		channelz.Errorf(logger, ccb.cc.channelzID, "Channel failed to build new LB policy %q: %v", name, err)
		return
	}
	ccb.curBalancerName = builder.Name()
}

// handleRemoveSucConn handles a request from the underlying balancer to remove
// a subConn.
//
// See comments in RemoveSubConn() for more details.
func (ccb *ccBalancerWrapper) handleRemoveSubConn(acbw *acBalancerWrapper) {
	ccb.mu.Lock()
	if ccb.subConns != nil {
		delete(ccb.subConns, acbw)
		ccb.cc.removeAddrConn(acbw.getAddrConn(), errConnDrain)
	}
	ccb.mu.Unlock()
}

func (ccb *ccBalancerWrapper) close() {
	if ccb == nil {
		return
	}
	ccb.closed.Fire()
	<-ccb.done.Done()
}

func (ccb *ccBalancerWrapper) handleClose() {
	ccb.balancerMu.Lock()
	ccb.balancer.Close()
	ccb.balancerMu.Unlock()

	ccb.mu.Lock()
	scs := ccb.subConns
	ccb.subConns = nil
	ccb.mu.Unlock()

	ccb.UpdateState(balancer.State{ConnectivityState: connectivity.Connecting, Picker: nil})
	ccb.done.Fire()
	// Fire done before removing the addr conns.  We can safely unblock
	// ccb.close and allow the removeAddrConns to happen
	// asynchronously.
	for acbw := range scs {
		ccb.cc.removeAddrConn(acbw.getAddrConn(), errConnDrain)
	}
	return
}

func (ccb *ccBalancerWrapper) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	if len(addrs) <= 0 {
		return nil, fmt.Errorf("grpc: cannot create SubConn with empty address list")
	}
	ccb.mu.Lock()
	defer ccb.mu.Unlock()
	if ccb.subConns == nil {
		return nil, fmt.Errorf("grpc: ClientConn balancer wrapper was closed")
	}
	ac, err := ccb.cc.newAddrConn(addrs, opts)
	if err != nil {
		channelz.Warningf(logger, ccb.cc.channelzID, "acBalancerWrapper: NewSubConn: failed to newAddrConn: %v", err)
		return nil, err
	}
	acbw := &acBalancerWrapper{ac: ac}
	acbw.ac.mu.Lock()
	ac.acbw = acbw
	acbw.ac.mu.Unlock()
	ccb.subConns[acbw] = struct{}{}
	return acbw, nil
}

func (ccb *ccBalancerWrapper) RemoveSubConn(sc balancer.SubConn) {
	// Before we switched the ccBalancerWrapper to use gracefulswitch.Balancer, it
	// was required to handle the RemoveSubConn() method asynchronously by pushing
	// the update onto the update channel. This was done to avoid a deadlock as
	// switchBalancer() was holding cc.mu when calling Close() on the old
	// balancer, which would in turn call RemoveSubConn().
	//
	// With the use of gracefulswitch.Balancer in ccBalancerWrapper, handling this
	// asynchronously is probably not required anymore since the switchTo() method
	// handles the balancer switch by pushing the update onto the channel.
	// TODO(easwars): Handle this inline.
	ccb.updateCh.Put(watcherUpdate{typ: updateTypeSubConn, update: sc})
}

func (ccb *ccBalancerWrapper) UpdateAddresses(sc balancer.SubConn, addrs []resolver.Address) {
	acbw, ok := sc.(*acBalancerWrapper)
	if !ok {
		return
	}
	acbw.UpdateAddresses(addrs)
}

func (ccb *ccBalancerWrapper) UpdateState(s balancer.State) {
	ccb.mu.Lock()
	defer ccb.mu.Unlock()
	if ccb.subConns == nil {
		return
	}
	// Update picker before updating state.  Even though the ordering here does
	// not matter, it can lead to multiple calls of Pick in the common start-up
	// case where we wait for ready and then perform an RPC.  If the picker is
	// updated later, we could call the "connecting" picker when the state is
	// updated, and then call the "ready" picker after the picker gets updated.
	ccb.cc.blockingpicker.updatePicker(s.Picker)
	ccb.cc.csMgr.updateState(s.ConnectivityState)
}

func (ccb *ccBalancerWrapper) ResolveNow(o resolver.ResolveNowOptions) {
	ccb.cc.resolveNow(o)
}

func (ccb *ccBalancerWrapper) Target() string {
	return ccb.cc.target
}

// acBalancerWrapper is a wrapper on top of ac for balancers.
// It implements balancer.SubConn interface.
type acBalancerWrapper struct {
	mu sync.Mutex
	ac *addrConn
}

func (acbw *acBalancerWrapper) UpdateAddresses(addrs []resolver.Address) {
	acbw.mu.Lock()
	defer acbw.mu.Unlock()
	if len(addrs) <= 0 {
		acbw.ac.cc.removeAddrConn(acbw.ac, errConnDrain)
		return
	}
	if !acbw.ac.tryUpdateAddrs(addrs) {
		cc := acbw.ac.cc
		opts := acbw.ac.scopts
		acbw.ac.mu.Lock()
		// Set old ac.acbw to nil so the Shutdown state update will be ignored
		// by balancer.
		//
		// TODO(bar) the state transition could be wrong when tearDown() old ac
		// and creating new ac, fix the transition.
		acbw.ac.acbw = nil
		acbw.ac.mu.Unlock()
		acState := acbw.ac.getState()
		acbw.ac.cc.removeAddrConn(acbw.ac, errConnDrain)

		if acState == connectivity.Shutdown {
			return
		}

		newAC, err := cc.newAddrConn(addrs, opts)
		if err != nil {
			channelz.Warningf(logger, acbw.ac.channelzID, "acBalancerWrapper: UpdateAddresses: failed to newAddrConn: %v", err)
			return
		}
		acbw.ac = newAC
		newAC.mu.Lock()
		newAC.acbw = acbw
		newAC.mu.Unlock()
		if acState != connectivity.Idle {
			go newAC.connect()
		}
	}
}

func (acbw *acBalancerWrapper) Connect() {
	acbw.mu.Lock()
	defer acbw.mu.Unlock()
	go acbw.ac.connect()
}

func (acbw *acBalancerWrapper) getAddrConn() *addrConn {
	acbw.mu.Lock()
	defer acbw.mu.Unlock()
	return acbw.ac
}
