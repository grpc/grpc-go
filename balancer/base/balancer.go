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

package base

import (
	"context"
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
)

type baseBuilder struct {
	name   string
	config Config
}

func (bb *baseBuilder) Build(cc balancer.ClientConn, opt balancer.BuildOptions) balancer.Balancer {
	pb := bb.config.PickerBuilderFactory()
	pbv2, _ := pb.(PickerBuilderV2)
	result := &baseBalancer{
		cc:              cc,
		pickerBuilder:   pb,
		pickerBuilderV2: pbv2,

		subConns: make(map[balancer.SubConn]subConnInfo),
		csEvltr:  &balancer.ConnectivityStateEvaluator{},
		// Initialize picker to a picker that always return
		// ErrNoSubConnAvailable, because when state of a SubConn changes, we
		// may call UpdateBalancerState with this picker.
		picker: NewErrPicker(balancer.ErrNoSubConnAvailable),
		config: bb.config,
	}
	scm := subConnManager{bb: result}
	result.cm = bb.config.ConnectionManagerFactory(scm, bb.config)
	return result
}

func (bb *baseBuilder) Name() string {
	return bb.name
}

var _ balancer.V2Balancer = (*baseBalancer)(nil) // Assert that we implement V2Balancer

type subConnInfo struct {
	sc    *SubConn
	state connectivity.State
}

type baseBalancer struct {
	cc              balancer.ClientConn
	pickerBuilder   PickerBuilder
	pickerBuilderV2 PickerBuilderV2
	cm              ConnectionManager

	csEvltr *balancer.ConnectivityStateEvaluator
	state   connectivity.State

	picker balancer.Picker
	config Config

	// it is possible that a ConnectionManager could arrange for connections
	// to be created from other goroutines, which means we need a mutex to
	// protect access to subConns
	subConnsMu sync.Mutex
	subConns   map[balancer.SubConn]subConnInfo
}

func (b *baseBalancer) HandleResolvedAddrs(addrs []resolver.Address, err error) {
	panic("not implemented")
}

func (b *baseBalancer) ResolverError(error) {
	// Ignore
}

func (b *baseBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	return b.cm.UpdateResolverState(s.ResolverState)
}

// regeneratePicker takes a snapshot of the balancer, and generates a picker
// from it. The picker is
//  - errPicker with ErrTransientFailure if the balancer is in TransientFailure,
//  - built by the pickerBuilder with all READY SubConns otherwise.
func (b *baseBalancer) regeneratePicker() {
	if b.state == connectivity.TransientFailure {
		b.picker = NewErrPicker(balancer.ErrTransientFailure)
		return
	}
	if b.pickerBuilderV2 != nil {
		// v2 picker:
		readySCs := b.readySubConnSlice()
		b.picker = pickerV2Adapter{p: b.pickerBuilderV2.Build(readySCs)}
		return
	}

	// v1 picker:
	readySCs := b.readySubConnMap()
	b.picker = b.pickerBuilder.Build(readySCs)
}

func (b *baseBalancer) readySubConnSlice() []*SubConn {
	b.subConnsMu.Lock()
	b.subConnsMu.Unlock()

	readySCs := make([]*SubConn, 0, len(b.subConns))
	// filter out all ready SCs from full subConn map.
	for _, info := range b.subConns {
		if info.state == connectivity.Ready {
			readySCs = append(readySCs, info.sc)
		}
	}
	return readySCs
}

func (b *baseBalancer) readySubConnMap() map[resolver.Address]balancer.SubConn {
	b.subConnsMu.Lock()
	b.subConnsMu.Unlock()

	readySCs := make(map[resolver.Address]balancer.SubConn, len(b.subConns))
	// filter out all ready SCs from full subConn map.
	for sc, info := range b.subConns {
		if info.state == connectivity.Ready {
			readySCs[info.sc.addr] = sc
		}
	}
	return readySCs
}

func (b *baseBalancer) addSubConn(sc *SubConn) {
	b.subConnsMu.Lock()
	defer b.subConnsMu.Unlock()
	b.subConns[sc.subConn] = subConnInfo{
		sc:    sc,
		state: connectivity.Idle,
	}
}

func (b *baseBalancer) HandleSubConnStateChange(sc balancer.SubConn, s connectivity.State) {
	panic("not implemented")
}

func (b *baseBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	oldS := b.state
	needNewPicker := b.updateSubConnState(sc, state.ConnectivityState)
	if needNewPicker {
		b.regeneratePicker()
	}

	if needNewPicker || b.state != oldS {
		// if picker or state updated, must inform cc
		b.cc.UpdateBalancerState(b.state, b.picker)
	}
}

func (b *baseBalancer) updateSubConnState(sc balancer.SubConn, s connectivity.State) bool {
	if grpclog.V(2) {
		grpclog.Infof("base.baseBalancer: handle SubConn state change: %p, %v", sc, s)
	}

	b.subConnsMu.Lock()
	defer b.subConnsMu.Unlock()

	scInfo, ok := b.subConns[sc]
	if !ok {
		if grpclog.V(2) {
			grpclog.Infof("base.baseBalancer: got state changes for an unknown SubConn: %p, %v", sc, s)
		}
		return false
	}
	oldS := scInfo.state
	b.subConns[sc] = subConnInfo{sc: scInfo.sc, state: s}
	switch s {
	case connectivity.Idle:
		sc.Connect()
	case connectivity.Shutdown:
		// When an address was removed by resolver, b called RemoveSubConn but
		// kept the sc's state in scStates. Remove state for this sc here.
		delete(b.subConns, sc)
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
		return true
	}

	return false
}

func (b *baseBalancer) Close() {
	b.cm.Close()
}

// NewErrPicker returns a picker that always returns err on Pick().
func NewErrPicker(err error) balancer.Picker {
	return &errPicker{err: err}
}

type errPicker struct {
	err error // Pick() always returns this err.
}

func (p *errPicker) Pick(ctx context.Context, opts balancer.PickOptions) (balancer.SubConn, func(balancer.DoneInfo), error) {
	return nil, nil, p.err
}
