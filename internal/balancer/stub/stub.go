/*
 *
 * Copyright 2020 gRPC authors.
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

// Package stub implements a balancer for testing purposes.
package stub

import "google.golang.org/grpc/balancer"

// BalancerFuncs contains all balancer.Balancer functions with a preceding
// *BalancerData parameter for passing additional instance information.  Any
// nil functions will never be called.
type BalancerFuncs struct {
	// Init is called after ClientConn and BuildOptions are set in
	// BalancerData.  It may be used to initialize BalancerData.Data.
	Init func(*BalancerData)

	UpdateClientConnState func(*BalancerData, balancer.ClientConnState) error
	ResolverError         func(*BalancerData, error)
	UpdateSubConnState    func(*BalancerData, balancer.SubConn, balancer.SubConnState)
	Close                 func(*BalancerData)
}

// BalancerData contains data relevant to a stub balancer.
type BalancerData struct {
	// ClientConn is set by the builder.
	ClientConn balancer.ClientConn
	// BuildOptions is set by the builder.
	BuildOptions balancer.BuildOptions
	// Data may be used to store arbitrary user data.
	Data interface{}
}

type bal struct {
	bf BalancerFuncs
	bd *BalancerData
}

func (b *bal) UpdateClientConnState(c balancer.ClientConnState) error {
	if b.bf.UpdateClientConnState != nil {
		return b.bf.UpdateClientConnState(b.bd, c)
	}
	return nil
}

func (b *bal) ResolverError(e error) {
	if b.bf.ResolverError != nil {
		b.bf.ResolverError(b.bd, e)
	}
}

func (b *bal) UpdateSubConnState(sc balancer.SubConn, scs balancer.SubConnState) {
	if b.bf.UpdateSubConnState != nil {
		b.bf.UpdateSubConnState(b.bd, sc, scs)
	}
}

func (b *bal) Close() {
	if b.bf.Close != nil {
		b.bf.Close(b.bd)
	}
}

type bb struct {
	name string
	bf   BalancerFuncs
}

func (bb bb) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	b := &bal{bf: bb.bf, bd: &BalancerData{ClientConn: cc, BuildOptions: opts}}
	if b.bf.Init != nil {
		b.bf.Init(b.bd)
	}
	return b
}

func (bb bb) Name() string { return bb.name }

// Register registers a stub balancer builder which will call the provided
// functions.  The name used should be unique.
func Register(name string, bf BalancerFuncs) {
	balancer.Register(bb{name: name, bf: bf})
}
