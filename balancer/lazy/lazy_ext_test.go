/*
 *
 * Copyright 2025 gRPC authors.
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

package lazy_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/lazy"
	"google.golang.org/grpc/balancer/pickfirst/pickfirstleaf"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

const (
	// Default timeout for tests in this package.
	defaultTestTimeout = 10 * time.Second
	// Default short timeout, to be used when waiting for events which are not
	// expected to happen.
	defaultTestShortTimeout = 100 * time.Millisecond
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestExitIdle creates a lazy balancer than manages a pickfirst child. The test
// calls Connect() on the channel which in turn calls ExitIdle on the lazy
// balancer. The test verifies that the channel enters READY.
func (s) TestExitIdle(t *testing.T) {
	backend1 := stubserver.StartTestService(t, nil)
	defer backend1.Stop()

	mr := manual.NewBuilderWithScheme("e2e-test")
	defer mr.Close()

	mr.InitialState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: backend1.Address}}},
		},
	})

	bf := stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.Data = lazy.NewBalancer(bd.ClientConn, bd.BuildOptions, balancer.Get(pickfirstleaf.Name).Build)
		},
		ExitIdle: func(bd *stub.BalancerData) {
			bd.Data.(balancer.ExitIdler).ExitIdle()
		},
		ResolverError: func(bd *stub.BalancerData, err error) {
			bd.Data.(balancer.Balancer).ResolverError(err)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			return bd.Data.(balancer.Balancer).UpdateClientConnState(ccs)
		},
		Close: func(bd *stub.BalancerData) {
			bd.Data.(balancer.Balancer).Close()
		},
	}
	stub.Register(t.Name(), bf)
	json := fmt.Sprintf(`{"loadBalancingConfig": [{"%s": {}}]}`, t.Name())
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(json),
		grpc.WithResolvers(mr),
	}
	cc, err := grpc.NewClient(mr.Scheme()+":///", opts...)
	if err != nil {
		t.Fatalf("grpc.NewClient(_) failed: %v", err)
	}
	defer cc.Close()

	cc.Connect()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	// Send a resolver update to verify that the resolver state is correctly
	// passed through to the leaf pickfirst balancer.
	backend2 := stubserver.StartTestService(t, nil)
	defer backend2.Stop()

	mr.UpdateState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: backend2.Address}}},
		},
	})

	var peer peer.Peer
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer)); err != nil {
		t.Errorf("client.EmptyCall() returned unexpected error: %v", err)
	}
	if got, want := peer.Addr.String(), backend2.Address; got != want {
		t.Errorf("EmptyCall() went to unexpected backend: got %q, want %q", got, want)
	}
}

// TestPicker creates a lazy balancer under a stub balancer which block all
// calls to ExitIdle. This ensures the only way to trigger lazy to exit idle is
// through the picker. The test makes an RPC and ensures it succeeds.
func (s) TestPicker(t *testing.T) {
	backend := stubserver.StartTestService(t, nil)
	defer backend.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	bf := stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.Data = lazy.NewBalancer(bd.ClientConn, bd.BuildOptions, balancer.Get(pickfirstleaf.Name).Build)
		},
		ExitIdle: func(bd *stub.BalancerData) {
			t.Log("Ignoring call to ExitIdle, calling the picker should make the lazy balancer exit IDLE state.")
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			return bd.Data.(balancer.Balancer).UpdateClientConnState(ccs)
		},
		Close: func(bd *stub.BalancerData) {
			bd.Data.(balancer.Balancer).Close()
		},
	}

	name := strings.ReplaceAll(strings.ToLower(t.Name()), "/", "")
	stub.Register(name, bf)
	json := fmt.Sprintf(`{"loadBalancingConfig": [{%q: {}}]}`, name)

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(json),
	}
	cc, err := grpc.NewClient(backend.Address, opts...)
	if err != nil {
		t.Fatalf("grpc.NewClient(_) failed: %v", err)
	}
	defer cc.Close()

	// The channel should remain in IDLE as the ExitIdle calls are not
	// propagated to the lazy balancer from the stub balancer.
	cc.Connect()
	shortCtx, shortCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer shortCancel()
	testutils.AwaitNoStateChange(shortCtx, t, cc, connectivity.Idle)

	// The picker from the lazy balancer should be send to the channel when the
	// first resolver update is received by lazy. Making an RPC should trigger
	// child creation.
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Errorf("client.EmptyCall() returned unexpected error: %v", err)
	}
}

// Tests the scenario when a resolver produces a good state followed by a
// resolver error. The test verifies that the child balancer receives the good
// update followed by the error.
func (s) TestGoodUpdateThenResolverError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	backend := stubserver.StartTestService(t, nil)
	defer backend.Stop()
	resolverStateReceived := false
	resolverErrorReceived := grpcsync.NewEvent()

	childBF := stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.Data = balancer.Get(pickfirstleaf.Name).Build(bd.ClientConn, bd.BuildOptions)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			if resolverErrorReceived.HasFired() {
				t.Error("Received resolver error before resolver state.")
			}
			resolverStateReceived = true
			return bd.Data.(balancer.Balancer).UpdateClientConnState(ccs)
		},
		ResolverError: func(bd *stub.BalancerData, err error) {
			if !resolverStateReceived {
				t.Error("Received resolver error before resolver state.")
			}
			resolverErrorReceived.Fire()
			bd.Data.(balancer.Balancer).ResolverError(err)
		},
		Close: func(bd *stub.BalancerData) {
			bd.Data.(balancer.Balancer).Close()
		},
	}

	childBalName := strings.ReplaceAll(strings.ToLower(t.Name())+"_child", "/", "")
	stub.Register(childBalName, childBF)

	topLevelBF := stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.Data = lazy.NewBalancer(bd.ClientConn, bd.BuildOptions, balancer.Get(childBalName).Build)
		},
		ExitIdle: func(bd *stub.BalancerData) {
			t.Log("Ignoring call to ExitIdle to delay lazy child creation until RPC time.")
		},
		ResolverError: func(bd *stub.BalancerData, err error) {
			bd.Data.(balancer.Balancer).ResolverError(err)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			return bd.Data.(balancer.Balancer).UpdateClientConnState(ccs)
		},
		Close: func(bd *stub.BalancerData) {
			bd.Data.(balancer.Balancer).Close()
		},
	}

	topLevelBalName := strings.ReplaceAll(strings.ToLower(t.Name())+"_top_level", "/", "")
	stub.Register(topLevelBalName, topLevelBF)

	json := fmt.Sprintf(`{"loadBalancingConfig": [{%q: {}}]}`, topLevelBalName)

	mr := manual.NewBuilderWithScheme("e2e-test")
	defer mr.Close()

	mr.InitialState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: backend.Address}}},
		},
	})

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(mr),
		grpc.WithDefaultServiceConfig(json),
	}
	cc, err := grpc.NewClient(mr.Scheme()+":///whatever", opts...)
	if err != nil {
		t.Fatalf("grpc.NewClient(_) failed: %v", err)

	}

	defer cc.Close()
	cc.Connect()

	mr.CC().ReportError(errors.New("test error"))
	// The channel should remain in IDLE as the ExitIdle calls are not
	// propagated to the lazy balancer from the stub balancer.
	shortCtx, shortCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer shortCancel()
	testutils.AwaitNoStateChange(shortCtx, t, cc, connectivity.Idle)

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Errorf("client.EmptyCall() returned unexpected error: %v", err)
	}

	if !resolverStateReceived {
		t.Fatalf("Child balancer did not receive resolver state.")
	}

	select {
	case <-resolverErrorReceived.Done():
	case <-ctx.Done():
		t.Fatal("Context timed out waiting for resolver error to be delivered to child balancer.")
	}
}

// Tests the scenario when a resolver produces a list of endpoints followed by
// a resolver error. The test verifies that the child balancer receives only the
// good update.
func (s) TestResolverErrorThenGoodUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	backend := stubserver.StartTestService(t, nil)
	defer backend.Stop()

	childBF := stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.Data = balancer.Get(pickfirstleaf.Name).Build(bd.ClientConn, bd.BuildOptions)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			return bd.Data.(balancer.Balancer).UpdateClientConnState(ccs)
		},
		ResolverError: func(bd *stub.BalancerData, err error) {
			t.Error("Received unexpected resolver error.")
			bd.Data.(balancer.Balancer).ResolverError(err)
		},
		Close: func(bd *stub.BalancerData) {
			bd.Data.(balancer.Balancer).Close()
		},
	}

	childBalName := strings.ReplaceAll(strings.ToLower(t.Name())+"_child", "/", "")
	stub.Register(childBalName, childBF)

	topLevelBF := stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.Data = lazy.NewBalancer(bd.ClientConn, bd.BuildOptions, balancer.Get(childBalName).Build)
		},
		ExitIdle: func(bd *stub.BalancerData) {
			t.Log("Ignoring call to ExitIdle to delay lazy child creation until RPC time.")
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			return bd.Data.(balancer.Balancer).UpdateClientConnState(ccs)
		},
		Close: func(bd *stub.BalancerData) {
			bd.Data.(balancer.Balancer).Close()
		},
	}

	topLevelBalName := strings.ReplaceAll(strings.ToLower(t.Name())+"_top_level", "/", "")
	stub.Register(topLevelBalName, topLevelBF)

	json := fmt.Sprintf(`{"loadBalancingConfig": [{%q: {}}]}`, topLevelBalName)

	mr := manual.NewBuilderWithScheme("e2e-test")
	defer mr.Close()

	mr.InitialState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: backend.Address}}},
		},
	})

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(mr),
		grpc.WithDefaultServiceConfig(json),
	}
	cc, err := grpc.NewClient(mr.Scheme()+":///whatever", opts...)
	if err != nil {
		t.Fatalf("grpc.NewClient(_) failed: %v", err)

	}

	defer cc.Close()
	cc.Connect()

	// Send an error followed by a good update.
	mr.CC().ReportError(errors.New("test error"))
	mr.UpdateState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: backend.Address}}},
		},
	})

	// The channel should remain in IDLE as the ExitIdle calls are not
	// propagated to the lazy balancer from the stub balancer.
	shortCtx, shortCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer shortCancel()
	testutils.AwaitNoStateChange(shortCtx, t, cc, connectivity.Idle)

	// An RPC would succeed only if the leaf pickfirst receives the endpoint
	// list.
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Errorf("client.EmptyCall() returned unexpected error: %v", err)
	}
}

// Tests that ExitIdle calls are correctly passed through to the child balancer.
// It starts a backend and ensures the channel connects to it. The test then
// stops the backend, making the channel enter IDLE. The test calls Connect on
// the channel and verifies that the child balancer exits idle.
func (s) TestExitIdlePassthrough(t *testing.T) {
	backend1 := stubserver.StartTestService(t, nil)
	defer backend1.Stop()

	mr := manual.NewBuilderWithScheme("e2e-test")
	defer mr.Close()

	mr.InitialState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: backend1.Address}}},
		},
	})

	bf := stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.Data = lazy.NewBalancer(bd.ClientConn, bd.BuildOptions, balancer.Get(pickfirstleaf.Name).Build)
		},
		ExitIdle: func(bd *stub.BalancerData) {
			bd.Data.(balancer.ExitIdler).ExitIdle()
		},
		ResolverError: func(bd *stub.BalancerData, err error) {
			bd.Data.(balancer.Balancer).ResolverError(err)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			return bd.Data.(balancer.Balancer).UpdateClientConnState(ccs)
		},
		Close: func(bd *stub.BalancerData) {
			bd.Data.(balancer.Balancer).Close()
		},
	}
	stub.Register(t.Name(), bf)
	json := fmt.Sprintf(`{"loadBalancingConfig": [{"%s": {}}]}`, t.Name())
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(json),
		grpc.WithResolvers(mr),
	}
	cc, err := grpc.NewClient(mr.Scheme()+":///", opts...)
	if err != nil {
		t.Fatalf("grpc.NewClient(_) failed: %v", err)

	}
	defer cc.Close()

	cc.Connect()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	// Stopping the active backend should put the channel in IDLE.
	backend1.Stop()
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Sending a new backend address should not kick the channel out of IDLE.
	// On calling cc.Connect(), the channel should call ExitIdle on the lazy
	// balancer which passes through the call to the leaf pickfirst.
	backend2 := stubserver.StartTestService(t, nil)
	defer backend2.Stop()

	mr.UpdateState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: backend2.Address}}},
		},
	})

	shortCtx, shortCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer shortCancel()
	testutils.AwaitNoStateChange(shortCtx, t, cc, connectivity.Idle)

	cc.Connect()
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)
}
