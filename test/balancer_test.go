/*
 *
 * Copyright 2018 gRPC authors.
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

package test

import (
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/leakcheck"
	"google.golang.org/grpc/resolver"
	testpb "google.golang.org/grpc/test/grpc_testing"
	"google.golang.org/grpc/testdata"
)

const testBalancerName = "testbalancer"

// testBalancer creates one subconn with the first address from resolved
// addresses.
//
// It's used to test options for NewSubConn are applies correctly.
type testBalancer struct {
	cc balancer.ClientConn
	sc balancer.SubConn

	newSubConnOptions balancer.NewSubConnOptions
}

func (b *testBalancer) Build(cc balancer.ClientConn, opt balancer.BuildOptions) balancer.Balancer {
	b.cc = cc
	return b
}

func (*testBalancer) Name() string {
	return testBalancerName
}

func (b *testBalancer) HandleResolvedAddrs(addrs []resolver.Address, err error) {
	// Only create a subconn at the first time.
	if err == nil && b.sc == nil {
		b.sc, err = b.cc.NewSubConn(addrs, b.newSubConnOptions)
		if err != nil {
			grpclog.Errorf("testBalancer: failed to NewSubConn: %v", err)
			return
		}
		b.cc.UpdateBalancerState(connectivity.Idle, &picker{sc: b.sc})
		b.sc.Connect()
	}
}

func (b *testBalancer) HandleSubConnStateChange(sc balancer.SubConn, s connectivity.State) {
	grpclog.Infof("testBalancer: HandleSubConnStateChange: %p, %v", sc, s)
	if b.sc != sc {
		grpclog.Infof("testBalancer: ignored state change because sc is not recognized")
		return
	}
	if s == connectivity.Shutdown {
		b.sc = nil
		return
	}

	switch s {
	case connectivity.Ready, connectivity.Idle:
		b.cc.UpdateBalancerState(s, &picker{sc: sc})
	case connectivity.Connecting:
		b.cc.UpdateBalancerState(s, &picker{err: balancer.ErrNoSubConnAvailable})
	case connectivity.TransientFailure:
		b.cc.UpdateBalancerState(s, &picker{err: balancer.ErrTransientFailure})
	}
}

func (b *testBalancer) Close() {
}

type picker struct {
	err error
	sc  balancer.SubConn
}

func (p *picker) Pick(ctx context.Context, opts balancer.PickOptions) (balancer.SubConn, func(balancer.DoneInfo), error) {
	if p.err != nil {
		return nil, nil, p.err
	}
	return p.sc, nil, nil
}

func TestCredsBundleFromBalancer(t *testing.T) {
	balancer.Register(&testBalancer{
		newSubConnOptions: balancer.NewSubConnOptions{
			CredsBundle: &testCredsBundle{},
		},
	})
	defer leakcheck.Check(t)
	te := newTest(t, env{name: "creds-bundle", network: "tcp", balancer: ""})
	te.tapHandle = authHandle
	te.customDialOptions = []grpc.DialOption{
		grpc.WithBalancerName(testBalancerName),
	}
	creds, err := credentials.NewServerTLSFromFile(testdata.Path("server1.pem"), testdata.Path("server1.key"))
	if err != nil {
		t.Fatalf("Failed to generate credentials %v", err)
	}
	te.customServerOptions = []grpc.ServerOption{
		grpc.Creds(creds),
	}
	te.startServer(&testServer{})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
		t.Fatalf("Test failed. Reason: %v", err)
	}
}
