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
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
)

// scStateChangeTuple contains the subConn and the new state it changed to.
type scStateChangeTuple struct {
	sc    balancer.SubConn
	state connectivity.State
}

// tupleBuffer is an unbounded channel for scStateChangeTuple.
type tupleBuffer struct {
	c       chan *scStateChangeTuple
	mu      sync.Mutex
	backlog []*scStateChangeTuple
}

func newTupleBuffer() *tupleBuffer {
	return &tupleBuffer{
		c: make(chan *scStateChangeTuple, 1),
	}
}

func (b *tupleBuffer) put(t *scStateChangeTuple) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.backlog) == 0 {
		select {
		case b.c <- t:
			return
		default:
		}
	}
	b.backlog = append(b.backlog, t)
}

func (b *tupleBuffer) load() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.backlog) > 0 {
		select {
		case b.c <- b.backlog[0]:
			b.backlog[0] = nil
			b.backlog = b.backlog[1:]
		default:
		}
	}
}

// get returns the channel that receives a recvMsg in the buffer.
//
// Upon receiving, the caller should call load to send another
// scStateChangeTuple onto the channel if there is any.
func (b *tupleBuffer) get() <-chan *scStateChangeTuple {
	return b.c
}

// resolverChangeTuple contains the new resolved addresses or error if there's
// any.
type resolverChangeTuple struct {
	addrs []resolver.Address
	err   error
}

// ccBalancerWrapper is a wrapper on top of cc for balancers.
// It implements balancer.ClientConn interface.
type ccBalancerWrapper struct {
	cc               *ClientConn
	balancer         balancer.Balancer
	stateChangeQueue *tupleBuffer
	resolverAddrCh   chan *resolverChangeTuple
	done             chan struct{}
}

func newCCBalancerWrapper(cc *ClientConn, b balancer.Builder, bopts balancer.BuildOptions) *ccBalancerWrapper {
	ccb := &ccBalancerWrapper{
		cc:               cc,
		stateChangeQueue: newTupleBuffer(),
		resolverAddrCh:   make(chan *resolverChangeTuple, 1),
		done:             make(chan struct{}),
	}
	go ccb.watcher()
	ccb.balancer = b.Build(ccb, bopts)
	return ccb
}

// watcher balancer functions sequencially, so the balancer can be implemeneted
// lock-free.
func (ccb *ccBalancerWrapper) watcher() {
	for {
		select {
		case <-ccb.done:
			ccb.balancer.Close()
			return
		default:
		}

		select {
		case t := <-ccb.stateChangeQueue.get():
			ccb.stateChangeQueue.load()
			ccb.balancer.HandleSubConnStateChange(t.sc, t.state)
		case t := <-ccb.resolverAddrCh:
			ccb.balancer.HandleResolvedAddrs(t.addrs, t.err)
		case <-ccb.done:
			ccb.balancer.Close()
			return
		}
	}
}

func (ccb *ccBalancerWrapper) close() {
	close(ccb.done)
}

func (ccb *ccBalancerWrapper) handleSubConnStateChange(sc balancer.SubConn, s connectivity.State) {
	if sc == nil {
		return
	}
	ccb.stateChangeQueue.put(&scStateChangeTuple{
		sc:    sc,
		state: s,
	})
}

func (ccb *ccBalancerWrapper) handleResolvedAddrs(addrs []resolver.Address, err error) {
	select {
	case <-ccb.resolverAddrCh:
	default:
	}
	ccb.resolverAddrCh <- &resolverChangeTuple{
		addrs: addrs,
		err:   err,
	}
}

func (ccb *ccBalancerWrapper) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	grpclog.Infof("ccBalancerWrapper: new subconn: %v", addrs)
	ac, err := ccb.cc.newAddrConn(addrs)
	if err != nil {
		return nil, err
	}
	acbw := &acBalancerWrapper{ac: ac}
	ac.acbw = acbw
	return acbw, nil
}

func (ccb *ccBalancerWrapper) RemoveSubConn(sc balancer.SubConn) {
	grpclog.Infof("ccBalancerWrapper: removing subconn")
	acbw, ok := sc.(*acBalancerWrapper)
	if !ok {
		return
	}
	ccb.cc.removeAddrConn(acbw.getAddrConn(), errConnDrain)
}

func (ccb *ccBalancerWrapper) UpdateBalancerState(s connectivity.State, p balancer.Picker) {
	grpclog.Infof("ccBalancerWrapper: updating state and picker called by balancer: %v, %p", s, p)
	ccb.cc.csMgr.updateState(s)
	ccb.cc.blockingpicker.updatePicker(p)
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
	grpclog.Infof("acBalancerWrapper: UpdateAddresses called with %v", addrs)
	acbw.mu.Lock()
	defer acbw.mu.Unlock()
	if !acbw.ac.tryUpdateAddrs(addrs) {
		cc := acbw.ac.cc
		acbw.ac.mu.Lock()
		// Set old ac.acbw to nil so the states update will be ignored by balancer.
		acbw.ac.acbw = nil
		acbw.ac.mu.Unlock()
		acState := acbw.ac.getState()
		acbw.ac.tearDown(errConnDrain)

		if acState == connectivity.Shutdown {
			return
		}

		ac, err := cc.newAddrConn(addrs)
		if err != nil {
			grpclog.Warningf("acBalancerWrapper: UpdateAddresses: failed to newAddrConn: %v", err)
			return
		}
		acbw.ac = ac
		ac.acbw = acbw
		if acState != connectivity.Idle {
			ac.connect(false)
		}
	}
}

func (acbw *acBalancerWrapper) Connect() {
	acbw.mu.Lock()
	defer acbw.mu.Unlock()
	acbw.ac.connect(false)
}

func (acbw *acBalancerWrapper) getAddrConn() *addrConn {
	acbw.mu.Lock()
	defer acbw.mu.Unlock()
	return acbw.ac
}
