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

// Package roundrobin defines a roundrobin balancer.
package roundrobin

import (
	"sync"

	"golang.org/x/net/context"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
)

// NewBuilder creates a new roundrobin balancer builder.
func NewBuilder() balancer.Builder {
	return &roundrobinBuilder{}
}

type roundrobinBuilder struct{}

func (*roundrobinBuilder) Build(cc balancer.ClientConn, opt balancer.BuildOptions) balancer.Balancer {
	b := &roundrobinBalancer{
		cc:       cc,
		subConns: make(map[resolver.Address]balancer.SubConn),
		scStates: make(map[balancer.SubConn]connectivity.State),
		csEvltr:  &connectivityStateEvaluator{},
		// Initialize picker to a picker that always return
		// ErrNoSubConnAvailable, because when state of a SubConn changes, we
		// may call UpdateBalancerState with this picker.
		picker: newPicker([]balancer.SubConn{}, nil),
	}
	return b
}

func (*roundrobinBuilder) Name() string {
	return "roundrobin"
}

type roundrobinBalancer struct {
	cc balancer.ClientConn

	csEvltr *connectivityStateEvaluator
	state   connectivity.State

	subConns map[resolver.Address]balancer.SubConn
	scStates map[balancer.SubConn]connectivity.State
	picker   *picker
}

func (b *roundrobinBalancer) HandleResolvedAddrs(addrs []resolver.Address, err error) {
	if err != nil {
		grpclog.Infof("roundrobinBalancer: HandleResolvedAddrs called with error %v", err)
		return
	}
	grpclog.Infoln("roundrobinBalancer: got new resolved addresses: ", addrs)
	// addrsSet is the set converted from addrs, it's used to quick lookup for an address.
	addrsSet := make(map[resolver.Address]struct{})
	for _, a := range addrs {
		addrsSet[a] = struct{}{}
		if _, ok := b.subConns[a]; !ok {
			// a is a new address (not existing in b.subConns).
			sc, err := b.cc.NewSubConn([]resolver.Address{a}, balancer.NewSubConnOptions{})
			if err == nil {
				b.subConns[a] = sc
				b.scStates[sc] = connectivity.Idle
				sc.Connect()
			} else {
				grpclog.Warningf("roundrobinBalancer: failed to create new SubConn: %v", err)
			}
		}
	}
	for a, sc := range b.subConns {
		// a was removed by resolver.
		if _, ok := addrsSet[a]; !ok {
			b.cc.RemoveSubConn(sc)
			delete(b.subConns, a)
			// Keep the state of this sc in b.scStates until sc's state becomes Shutdown.
			// The entry will be deleted in HandleSubConnStateChange.
		}
	}
}

// regeneratePicker takes a snapshot of the balancer, and generate a picker from
// it. The picker
//  - always return ErrTransientFailure if the balancer is in TransientFailure,
//  - do roundrobin on existing subConns otherwise.
func (b *roundrobinBalancer) regeneratePicker() {
	if b.state == connectivity.TransientFailure {
		b.picker = newPicker(nil, balancer.ErrTransientFailure)
		return
	}
	var readySCs []balancer.SubConn
	for sc, st := range b.scStates {
		if st == connectivity.Ready {
			readySCs = append(readySCs, sc)
		}
	}
	b.picker = newPicker(readySCs, nil)
	return
}

func (b *roundrobinBalancer) HandleSubConnStateChange(sc balancer.SubConn, s connectivity.State) {
	grpclog.Infof("roundrobinBalancer: handle SubConn state change: %p, %v", sc, s)
	oldS, ok := b.scStates[sc]
	if !ok {
		return
	}
	if s == connectivity.Idle {
		sc.Connect()
	}
	b.scStates[sc] = s

	oldAggrState := b.state
	b.state = b.csEvltr.recordTransition(oldS, s)

	// Regenerate picker when one of the following happens:
	//  - this sc became ready from not-ready
	//  - this sc became not-ready from ready
	//  - the aggregated state of balancer became TransientFailure from non-TransientFailure
	//  - the aggregated state of balancer became non-TransientFailure from TransientFailure
	if oldS != connectivity.Ready && s == connectivity.Ready ||
		oldS == connectivity.Ready && s != connectivity.Ready ||
		b.state != connectivity.TransientFailure && oldAggrState == connectivity.TransientFailure ||
		b.state == connectivity.TransientFailure && oldAggrState != connectivity.TransientFailure {
		b.regeneratePicker()
	}

	b.cc.UpdateBalancerState(b.state, b.picker)
	if s == connectivity.Shutdown {
		// When an address was removed by resolver, b called RemoveSubConn but
		// kept the sc's state in scStates. Remove state for this sc here.
		delete(b.scStates, sc)
	}
	return
}

func (b *roundrobinBalancer) Close() {
	for _, sc := range b.subConns {
		b.cc.RemoveSubConn(sc)
	}
}

type picker struct {
	// If err is not nil, Pick always returns this err.
	err error

	mu       sync.Mutex
	next     int
	size     int
	subConns []balancer.SubConn
}

func newPicker(scs []balancer.SubConn, err error) *picker {
	grpclog.Infof("roundrobinPicker: newPicker called with scs: %v, %v", scs, err)
	if err != nil {
		return &picker{err: err}
	}
	return &picker{
		size:     len(scs),
		subConns: scs,
	}
}

func (p *picker) Pick(ctx context.Context, opts balancer.PickOptions) (conn balancer.SubConn, put func(balancer.DoneInfo), err error) {
	if p.err != nil {
		return nil, nil, p.err
	}
	if p.size <= 0 {
		return nil, nil, balancer.ErrNoSubConnAvailable
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	sc := p.subConns[p.next]
	p.next = (p.next + 1) % p.size
	return sc, nil, nil
}

// connectivityStateEvaluator gets updated by addrConns when their
// states transition, based on which it evaluates the state of
// ClientConn.
type connectivityStateEvaluator struct {
	mu                  sync.Mutex
	numReady            uint64 // Number of addrConns in ready state.
	numConnecting       uint64 // Number of addrConns in connecting state.
	numTransientFailure uint64 // Number of addrConns in transientFailure.
}

// recordTransition records state change happening in every subConn and based on
// that it evaluates what aggregated state should be.
// It can only transition between Ready, Connecting and TransientFailure. Other states,
// Idle and Shutdown are transitioned into by ClientConn; in the begining of the connection
// before any subConn is created ClientConn is in idle state. In the end when ClientConn
// closes it is in Shutdown state.
func (cse *connectivityStateEvaluator) recordTransition(oldState, newState connectivity.State) connectivity.State {
	cse.mu.Lock()
	defer cse.mu.Unlock()

	// Update counters.
	for idx, state := range []connectivity.State{oldState, newState} {
		updateVal := 2*uint64(idx) - 1 // -1 for oldState and +1 for new.
		switch state {
		case connectivity.Ready:
			cse.numReady += updateVal
		case connectivity.Connecting:
			cse.numConnecting += updateVal
		case connectivity.TransientFailure:
			cse.numTransientFailure += updateVal
		}
	}

	// Evaluate.
	if cse.numReady > 0 {
		return connectivity.Ready
	}
	if cse.numConnecting > 0 {
		return connectivity.Connecting
	}
	return connectivity.TransientFailure
}
