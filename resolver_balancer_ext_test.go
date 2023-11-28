/*
 *
 * Copyright 2014 gRPC authors.
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

package grpc_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
)

// TestResolverBalancerInteraction tests:
// 1. resolver.Builder.Build()
// 2. resolver.ClientConn.UpdateState()
// 3. balancer.Balancer.UpdateClientConnState()
// 4. balancer.ClientConn.ResolveNow()
// 5. resolver.Resolver.ResolveNow()
func (s) TestResolverBalancerInteraction(t *testing.T) {
	const name = "testrbi"
	bf := stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			bd.ClientConn.ResolveNow(resolver.ResolveNowOptions{})
			return nil
		},
	}
	stub.Register(name, bf)

	rb := manual.NewBuilderWithScheme(name)
	rb.BuildCallback = func(_ resolver.Target, cc resolver.ClientConn, _ resolver.BuildOptions) {
		sc := cc.ParseServiceConfig(`{"loadBalancingConfig": [{"` + name + `":{}}]}`)
		cc.UpdateState(resolver.State{
			Addresses:     []resolver.Address{{Addr: "test"}},
			ServiceConfig: sc,
		})
	}
	rnCh := make(chan struct{})
	rb.ResolveNowCallback = func(resolver.ResolveNowOptions) { close(rnCh) }
	resolver.Register(rb)

	cc, err := grpc.Dial(name+":///", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial error: %v", err)
	}
	defer cc.Close()
	select {
	case <-rnCh:
	case <-time.After(defaultTestTimeout):
		t.Fatalf("timed out waiting for resolver.ResolveNow")
	}
}

type resolverBuilderWithErr struct {
	resolver.Resolver
	errCh  <-chan error
	scheme string
}

func (b *resolverBuilderWithErr) Build(_ resolver.Target, cc resolver.ClientConn, _ resolver.BuildOptions) (resolver.Resolver, error) {
	if err := <-b.errCh; err != nil {
		return nil, err
	}
	return b, nil
}

func (b *resolverBuilderWithErr) Scheme() string {
	return b.scheme
}

func (b *resolverBuilderWithErr) Close() {}

// TestResolverBuildFailure tests:
// 1. resolver.Builder.Build() passes.
// 2. Channel enters idle mode.
// 3. An RPC happens.
// 4. resolver.Builder.Build() fails.
func (s) TestResolverBuildFailure(t *testing.T) {
	enterIdle := internal.EnterIdleModeForTesting.(func(*grpc.ClientConn))
	const name = "trbf"
	resErrCh := make(chan error, 1)
	resolver.Register(&resolverBuilderWithErr{errCh: resErrCh, scheme: name})

	resErrCh <- nil
	cc, err := grpc.Dial(name+":///", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial error: %v", err)
	}
	defer cc.Close()
	enterIdle(cc)
	const errStr = "test error from resolver builder"
	t.Log("pushing res err")
	resErrCh <- errors.New(errStr)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := cc.Invoke(ctx, "/a/b", nil, nil); err == nil || !strings.Contains(err.Error(), errStr) {
		t.Fatalf("Invoke = %v; want %v", err, errStr)
	}
}
