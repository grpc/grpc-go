/*
 *
 * Copyright 2021 gRPC authors.
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

package priority

import (
	"sync/atomic"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
)

type atomicBool struct {
	v uint32
}

func newAtomicBool(b bool) *atomicBool {
	ret := &atomicBool{}
	if b {
		ret.v = 1
	}
	return ret
}

func (ab *atomicBool) set(b bool) {
	if b {
		atomic.StoreUint32(&(ab.v), 1)
		return
	}
	atomic.StoreUint32(&(ab.v), 0)
}

func (ab *atomicBool) get() bool {
	return atomic.LoadUint32(&(ab.v)) != 0
}

type ignoreResolveNowBalancerBuilder struct {
	balancer.Builder
	ignoreResolveNow *atomicBool
}

// If `ignore` is true, all `ResolveNow()` from the balancer built from this
// builder will be ignored.
//
// `ignore` can be updated later by `updateIgnoreResolveNow`, and the update
// will be propagated to all the old and new balancers built with this.
func newIgnoreResolveNowBalancerBuilder(bb balancer.Builder, ignore bool) *ignoreResolveNowBalancerBuilder {
	return &ignoreResolveNowBalancerBuilder{
		Builder:          bb,
		ignoreResolveNow: newAtomicBool(ignore),
	}
}

func (irnbb *ignoreResolveNowBalancerBuilder) updateIgnoreResolveNow(b bool) {
	irnbb.ignoreResolveNow.set(b)
}

func (irnbb *ignoreResolveNowBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return irnbb.Builder.Build(&ignoreResolveNowClientConn{
		ClientConn:       cc,
		ignoreResolveNow: irnbb.ignoreResolveNow,
	}, opts)
}

type ignoreResolveNowClientConn struct {
	balancer.ClientConn
	ignoreResolveNow *atomicBool
}

func (i ignoreResolveNowClientConn) ResolveNow(o resolver.ResolveNowOptions) {
	if i.ignoreResolveNow.get() {
		return
	}
	i.ClientConn.ResolveNow(o)
}
