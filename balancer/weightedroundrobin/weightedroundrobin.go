/*
 *
 * Copyright 2019 gRPC authors.
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

// Package weightedroundrobin defines a weighted roundrobin balancer.
package weightedroundrobin

import (
	"context"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/wrr"
	"google.golang.org/grpc/resolver"
	"sync"
)

// Name is the name of weighted_round_robin balancer.
const Name = "weighted_round_robin"

// AddrInfo will be stored inside Address metadata in order to use weighted roundrobin
// balancer.
type AddrInfo struct {
	Weight uint32
}

type wrrBuilder struct{}

// newBuilder creates a new roundrobin balancer builder.
func newBuilder() balancer.Builder {
	return &wrrBuilder{}
}

func init() {
	balancer.Register(newBuilder())
}

func (*wrrBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return &wrrBalancer{
		cc: cc,

		subConns: make(map[string]*BalancerAddrInfo),
		scStates: make(map[balancer.SubConn]connectivity.State),
		csEvltr:  &balancer.ConnectivityStateEvaluator{},
	}
}

func (*wrrBuilder) Name() string {
	return Name
}

type BalancerAddrInfo struct {
	SubConn balancer.SubConn
	Weight  uint32
}

type wrrBalancer struct {
	cc balancer.ClientConn

	csEvltr *balancer.ConnectivityStateEvaluator
	state   connectivity.State

	subConns map[string]*BalancerAddrInfo
	scStates map[balancer.SubConn]connectivity.State
	picker   balancer.Picker
}

func (*wrrBalancer) HandleSubConnStateChange(sc balancer.SubConn, state connectivity.State) {
	panic("not implemented")
}

func (*wrrBalancer) HandleResolvedAddrs([]resolver.Address, error) {
	panic("not implemented")
}

func (*wrrBalancer) Close() {
}

func (b *wrrBalancer) UpdateClientConnState(s balancer.ClientConnState) {
	grpclog.Infoln("wrr: got new ClientConn state: ", s)
	// addrWeights is the set converted from addrs, it's used for quick lookup of an address.
	addrWeights := make(map[string]uint32)
	for _, a := range s.ResolverState.Addresses {
		weight := uint32(1)
		if addrInfo, ok := a.Metadata.(*AddrInfo); ok {
			weight = addrInfo.Weight
		}
		addrWeights[a.Addr] += weight
		if _, ok := b.subConns[a.Addr]; !ok {
			// a is a new address (not existing in b.subConns).
			sc, err := b.cc.NewSubConn([]resolver.Address{a}, balancer.NewSubConnOptions{HealthCheckEnabled: true})
			if err != nil {
				grpclog.Warningf("base.baseBalancer: failed to create new SubConn: %v", err)
				continue
			}
			balancerAddrInfo := BalancerAddrInfo{SubConn: sc}
			b.subConns[a.Addr] = &balancerAddrInfo
			b.scStates[sc] = connectivity.Idle
			sc.Connect()
		}
	}
	for a, sc := range b.subConns {
		// a was removed by resolver.
		if weight, ok := addrWeights[a]; !ok {
			b.cc.RemoveSubConn(sc.SubConn)
			delete(b.subConns, a)
			// Keep the state of this sc in b.scStates until sc's state becomes Shutdown.
			// The entry will be deleted in HandleSubConnStateChange.
		} else {
			sc.Weight = weight
		}
	}
}

func (b *wrrBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	s := state.ConnectivityState
	grpclog.Infof("wrr: handle SubConn state change: %p, %v", sc, s)
	oldS, ok := b.scStates[sc]
	if !ok {
		grpclog.Infof("wrr: got state changes for an unknown SubConn: %p, %v", sc, s)
		return
	}
	b.scStates[sc] = s
	switch s {
	case connectivity.Idle:
		sc.Connect()
	case connectivity.Shutdown:
		// When an address was removed by resolver, b called RemoveSubConn but
		// kept the sc's state in scStates. Remove state for this sc here.
		delete(b.scStates, sc)
	}

	oldAggrState := b.state
	b.state = b.csEvltr.RecordTransition(oldS, s)

	// Regenerate picker when one of the following happens:
	//  - this sc became ready from not-ready
	//  - this sc became not-ready from ready
	//  - the aggregated state of balancer became TransientFailure from non-TransientFailure
	//  - the aggregated state of balancer became non-TransientFailure from TransientFailure
	if (s == connectivity.Ready) != (oldS == connectivity.Ready) ||
		(b.state == connectivity.TransientFailure) != (oldAggrState == connectivity.TransientFailure) {
		b.regeneratePicker()
	}

	b.cc.UpdateBalancerState(b.state, b.picker)
}

// regeneratePicker takes a snapshot of the balancer, and generates a picker
// from it. The picker is
//  - errPicker with ErrTransientFailure if the balancer is in TransientFailure,
//  - built by the pickerBuilder with all READY SubConns otherwise.
func (b *wrrBalancer) regeneratePicker() {
	if b.state == connectivity.TransientFailure {
		b.picker = base.NewErrPicker(balancer.ErrTransientFailure)
		return
	}
	readySCs := make(map[string]*BalancerAddrInfo)

	// Filter out all ready SCs from full subConn map.
	for addr, sc := range b.subConns {
		if st, ok := b.scStates[sc.SubConn]; ok && st == connectivity.Ready {
			readySCs[addr] = sc
		}
	}
	b.picker = buildPicker(readySCs)
}

func buildPicker(readySCs map[string]*BalancerAddrInfo) balancer.Picker {
	grpclog.Infof("wrr: buildPicker called with readySCs: %v", readySCs)
	if len(readySCs) == 0 {
		return base.NewErrPicker(balancer.ErrNoSubConnAvailable)
	}
	w := wrr.NewEDF()
	for _, sc := range readySCs {
		w.Add(sc.SubConn, int64(sc.Weight))
	}
	return &wrrPicker{wrr: w}
}

type wrrPicker struct {
	wrr wrr.WRR
	mu  sync.Mutex
}

func (p *wrrPicker) Pick(ctx context.Context, opts balancer.PickOptions) (balancer.SubConn, func(balancer.DoneInfo), error) {
	p.mu.Lock()
	sc := p.wrr.Next().(balancer.SubConn)
	p.mu.Unlock()
	return sc, nil, nil
}
