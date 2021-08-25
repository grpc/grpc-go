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
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	grpctestutils "google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/testutils"
)

const resolveNowBalancerName = "test-resolve-now-balancer"

var resolveNowBalancerCCCh = grpctestutils.NewChannel()

type resolveNowBalancerBuilder struct {
	balancer.Builder
}

func (r *resolveNowBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	resolveNowBalancerCCCh.Send(cc)
	return r.Builder.Build(cc, opts)
}

func (r *resolveNowBalancerBuilder) Name() string {
	return resolveNowBalancerName
}

func init() {
	balancer.Register(&resolveNowBalancerBuilder{
		Builder: balancer.Get(roundrobin.Name),
	})
}

func (s) TestIgnoreResolveNowBalancerBuilder(t *testing.T) {
	resolveNowBB := balancer.Get(resolveNowBalancerName)
	// Create a build wrapper, but will not ignore ResolveNow().
	ignoreResolveNowBB := newIgnoreResolveNowBalancerBuilder(resolveNowBB, false)

	cc := testutils.NewTestClientConn(t)
	tb := ignoreResolveNowBB.Build(cc, balancer.BuildOptions{})
	defer tb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// This is the balancer.ClientConn that the inner resolverNowBalancer is
	// built with.
	balancerCCI, err := resolveNowBalancerCCCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout waiting for ClientConn from balancer builder")
	}
	balancerCC := balancerCCI.(balancer.ClientConn)

	// Call ResolveNow() on the CC, it should be forwarded.
	balancerCC.ResolveNow(resolver.ResolveNowOptions{})
	select {
	case <-cc.ResolveNowCh:
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for ResolveNow()")
	}

	// Update ignoreResolveNow to true, call ResolveNow() on the CC, they should
	// all be ignored.
	ignoreResolveNowBB.updateIgnoreResolveNow(true)
	for i := 0; i < 5; i++ {
		balancerCC.ResolveNow(resolver.ResolveNowOptions{})
	}
	select {
	case <-cc.ResolveNowCh:
		t.Fatalf("got unexpected ResolveNow() call")
	case <-time.After(time.Millisecond * 100):
	}

	// Update ignoreResolveNow to false, new ResolveNow() calls should be
	// forwarded.
	ignoreResolveNowBB.updateIgnoreResolveNow(false)
	balancerCC.ResolveNow(resolver.ResolveNowOptions{})
	select {
	case <-cc.ResolveNowCh:
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for ResolveNow()")
	}
}
