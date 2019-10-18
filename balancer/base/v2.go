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

package base

// This file contains the types and interfaces that support "v2" of the
// base balancer. V2 provides an alternate PickerBuilder interface and
// also exposes a separate ConnectionManager interface, allowing the
// concerns of deciding what connections to establish be separated from
// the concerns of actually picking a connection for an RPC.

import (
	"context"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
)

// PickerBuilder creates a balancer.Picker. If a PickerBuilder implements this
// interface, its method will be used instead.
type PickerBuilderV2 interface {
	// Build takes a slice of ready SubConns and returns a picker that will be
	// used by gRPC to pick a SubConn.
	Build(readySCs []*SubConn) Picker
}

// Picker is an alternative to balancer.Picker that is used by PickerBuilderV2.
// It returns a *base.SubConn instead of a balancer.SubConn.
type Picker interface {
	Pick(ctx context.Context, opts balancer.PickOptions) (conn *SubConn, done func(balancer.DoneInfo), err error)
}

// SubConnManager is an interface that allows a ConnectionManager to interact
// with a balancer.ClientConn.
type SubConnManager interface {
	NewSubConn(resolver.Address, balancer.NewSubConnOptions) (*SubConn, error)
	RemoveSubConn(*SubConn)
}

// ConnectionManager decides what connections to establish, maintain, or
// tear down, based on updates from a resolver.
type ConnectionManager interface {
	// UpdateResolverState provides new information about the state of a service.
	// The ConnectionManager may then choose to use its SubConnManager to create
	// or destroy sub-connections to resolved addresses.
	UpdateResolverState(resolver.State) error

	// Close closes the connection manager. The manager is not required to call
	// SubConnManager.RemoveSubConn for its existing SubConns.
	Close()
}

// SubConn represents a sub-conn to a single address. It exposes only the
// operations needed by a PickerBuilderV2 and ConnectionManager.
type SubConn struct {
	subConn balancer.SubConn
	addr    resolver.Address
}

func (sc *SubConn) Connect() {
	sc.subConn.Connect()
}

func (sc *SubConn) Address() resolver.Address {
	return sc.addr
}

type subConnManager struct {
	bb *baseBalancer
}

func (scm subConnManager) NewSubConn(addr resolver.Address, opts balancer.NewSubConnOptions) (*SubConn, error) {
	sc, err := scm.bb.cc.NewSubConn([]resolver.Address{addr}, opts)
	if sc != nil {
		s := &SubConn{subConn: sc, addr: addr}
		scm.bb.addSubConn(s)
		return s, err
	}
	return nil, err
}

func (scm subConnManager) RemoveSubConn(sc *SubConn) {
	scm.bb.cc.RemoveSubConn(sc.subConn)
}

type pickerV2Adapter struct {
	p Picker
}

func (a pickerV2Adapter) Pick(ctx context.Context, opts balancer.PickOptions) (conn balancer.SubConn, done func(balancer.DoneInfo), err error) {
	c, done, err := a.p.Pick(ctx, opts)
	if conn != nil {
		return c.subConn, done, err
	}
	return nil, done, err
}
