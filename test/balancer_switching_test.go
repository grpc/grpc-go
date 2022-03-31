/*
 *
 * Copyright 2022 gRPC authors.
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
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils/fakegrpclb"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

const (
	loadBalancedServiceName = "foo.bar.service"
	loadBalancedServicePort = 443
	wantGRPCLBTraceDesc     = `Channel switches to new LB policy "grpclb"`
	wantRoundRobinTraceDesc = `Channel switches to new LB policy "round_robin"`
	wantPickFirstTraceDesc  = `Channel switches to new LB policy "pick_first"`
)

// setupBackendsAndFakeGRPCLB sets up the stub server backends and a fake grpclb
// server for tests which exercise balancer switch scenarios involving grpclb.
// Returns a cleanup function to be invoked by the caller.
func setupBackendsAndFakeGRPCLB(t *testing.T) ([]*stubserver.StubServer, *fakegrpclb.Server, func()) {
	czCleanup := channelz.NewChannelzStorageForTesting()
	backends, backendsCleanup := startBackendsForBalancerSwitch(t)
	rawAddrs := stubBackendsToRawAddrs(backends)

	lbServer, err := fakegrpclb.NewServer(fakegrpclb.ServerParams{
		LoadBalancedServiceName: loadBalancedServiceName,
		LoadBalancedServicePort: loadBalancedServicePort,
		BackendAddresses:        rawAddrs,
	})
	if err != nil {
		t.Fatalf("failed to create fake grpclb server: %v", err)
	}
	go func() {
		if err := lbServer.Serve(); err != nil {
			t.Errorf("fake grpclb Serve() failed: %v", err)
		}
	}()

	return backends, lbServer, func() {
		backendsCleanup()
		lbServer.Stop()
		czCleanupWrapper(czCleanup, t)
	}
}

// startBackendsForBalancerSwitch spins up a bunch of stub server backends
// exposing the TestService. Returns a cleanup function to be invoked by the
// caller.
func startBackendsForBalancerSwitch(t *testing.T) ([]*stubserver.StubServer, func()) {
	t.Helper()

	const backendCount = 3
	backends := make([]*stubserver.StubServer, backendCount)
	for i := 0; i < backendCount; i++ {
		backend := &stubserver.StubServer{
			EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
		}
		if err := backend.StartServer(); err != nil {
			t.Fatalf("Failed to start backend: %v", err)
		}
		t.Logf("Started TestService backend at: %q", backend.Address)
		backends[i] = backend
	}
	return backends, func() {
		for _, b := range backends {
			b.Stop()
		}
	}
}

// stubBackendsToRawAddrs converts from a set of stub server backends to raw
// address strings. Useful when pushing addresses to the fake grpclb server.
func stubBackendsToRawAddrs(backends []*stubserver.StubServer) []string {
	addrs := make([]string, len(backends))
	for i, backend := range backends {
		addrs[i] = backend.Address
	}
	return addrs
}

// checkForTraceEvent looks for a trace event in the top level channel matching
// the given description. Events before since are ignored. Returns nil error if
// such an event is found.
func checkForTraceEvent(ctx context.Context, wantDesc string, since time.Time) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		tcs, _ := channelz.GetTopChannels(0, 0)
		if len(tcs) != 1 {
			return fmt.Errorf("channelz returned %d top channels, want 1", len(tcs))
		}
		for _, event := range tcs[0].Trace.Events {
			if event.Timestamp.Before(since) {
				continue
			}
			if strings.Contains(event.Desc, wantDesc) {
				return nil
			}
		}
		time.Sleep(defaultTestShortTimeout)
	}
}

// TestBalancerSwitch_Basic tests the basic scenario of switching from one LB
// policy to another, as specified in the service config.
func (s) TestBalancerSwitch_Basic(t *testing.T) {
	backends, cleanup := startBackendsForBalancerSwitch(t)
	defer cleanup()
	addrs := stubBackendsToResolverAddrs(backends)

	r := manual.NewBuilderWithScheme("whatever")
	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Push a resolver update without an LB policy in the service config. The
	// channel should pick the default LB policy, which is pick_first.
	r.UpdateState(resolver.State{Addresses: addrs})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := checkPickFirst(ctx, cc, addrs[0].Addr); err != nil {
		t.Fatal(err)
	}

	// Push a resolver update with the service config specifying "round_robin".
	r.UpdateState(resolver.State{
		Addresses:     addrs,
		ServiceConfig: parseServiceConfig(t, r, rrServiceConfig),
	})
	if err := checkRoundRobin(ctx, cc, addrs); err != nil {
		t.Fatal(err)
	}

	// Push a resolver update with the service config specifying "pick_first".
	r.UpdateState(resolver.State{
		Addresses:     addrs,
		ServiceConfig: parseServiceConfig(t, r, pickFirstServiceConfig),
	})
	if err := checkPickFirst(ctx, cc, addrs[0].Addr); err != nil {
		t.Fatal(err)
	}
}

// TestBalancerSwitch_grpclbToPickFirst tests the scenario where the channel
// starts off "grpclb", switches to "pick_first" and back.
func (s) TestBalancerSwitch_grpclbToPickFirst(t *testing.T) {
	backends, lbServer, cleanup := setupBackendsAndFakeGRPCLB(t)
	defer cleanup()

	addrs := stubBackendsToResolverAddrs(backends)
	r := manual.NewBuilderWithScheme("whatever")
	target := fmt.Sprintf("%s:///%s", r.Scheme(), loadBalancedServiceName)
	cc, err := grpc.Dial(target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Push a resolver update with no service config and a single address pointing
	// to the grpclb server we created above. This will cause the channel to
	// switch to the "grpclb" balancer, and will equally distribute RPCs across
	// the backends as the fake grpclb server does not support load reporting from
	// the clients.
	now := time.Now()
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: lbServer.Address(), Type: resolver.GRPCLB}}})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := checkRoundRobin(ctx, cc, addrs); err != nil {
		t.Fatal(err)
	}
	if err := checkForTraceEvent(ctx, wantGRPCLBTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantGRPCLBTraceDesc, err)
	}

	// Push a resolver update containing a non-existent grpclb server address.
	// This should not lead to a balancer switch.
	now = time.Now()
	const nonExistentServer = "non-existent-grpclb-server-address"
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: nonExistentServer, Type: resolver.GRPCLB}}})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	wantDesc := fmt.Sprintf("Channel switches to new LB policy %q", nonExistentServer)
	if err := checkForTraceEvent(sCtx, wantDesc, now); err == nil {
		t.Fatal("channel switched balancers when expected not to")
	}

	// Push a resolver update containing no grpclb server address. This should
	// lead to the channel using the default LB policy which is pick_first.
	now = time.Now()
	r.UpdateState(resolver.State{Addresses: addrs})
	if err := checkPickFirst(ctx, cc, addrs[0].Addr); err != nil {
		t.Fatal(err)
	}
	if err := checkForTraceEvent(ctx, wantPickFirstTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantPickFirstTraceDesc, err)
	}
}

// TestBalancerSwitch_pickFirstToGRPCLB tests the scenario where the channel
// starts off with "pick_first", switches to "grpclb" and back.
func (s) TestBalancerSwitch_pickFirstToGRPCLB(t *testing.T) {
	backends, lbServer, cleanup := setupBackendsAndFakeGRPCLB(t)
	defer cleanup()

	addrs := stubBackendsToResolverAddrs(backends)
	r := manual.NewBuilderWithScheme("whatever")
	target := fmt.Sprintf("%s:///%s", r.Scheme(), loadBalancedServiceName)
	cc, err := grpc.Dial(target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Push a resolver update containing no grpclb server address. This should
	// lead to the channel using the default LB policy which is pick_first.
	now := time.Now()
	r.UpdateState(resolver.State{Addresses: addrs})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := checkForTraceEvent(ctx, wantPickFirstTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantPickFirstTraceDesc, err)
	}
	if err := checkPickFirst(ctx, cc, addrs[0].Addr); err != nil {
		t.Fatal(err)
	}

	// Push a resolver update with no service config and a single address pointing
	// to the grpclb server we created above. This will cause the channel to
	// switch to the "grpclb" balancer, and will equally distribute RPCs across
	// the backends as the fake grpclb server does not support load reporting from
	// the clients.
	now = time.Now()
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: lbServer.Address(), Type: resolver.GRPCLB}}})
	if err := checkForTraceEvent(ctx, wantGRPCLBTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantGRPCLBTraceDesc, err)
	}

	// Push a resolver update containing a non-existent grpclb server address.
	// This should not lead to a balancer switch.
	now = time.Now()
	const nonExistentServer = "non-existent-grpclb-server-address"
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: nonExistentServer, Type: resolver.GRPCLB}}})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	wantDesc := fmt.Sprintf("Channel switches to new LB policy %q", nonExistentServer)
	if err := checkForTraceEvent(sCtx, wantDesc, now); err == nil {
		t.Fatal("channel switched balancers when expected not to")
	}

	// Switch to "pick_first" again by sending no grpclb server addresses.
	now = time.Now()
	r.UpdateState(resolver.State{Addresses: addrs})
	if err := checkForTraceEvent(ctx, wantPickFirstTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantPickFirstTraceDesc, err)
	}
	if err := checkPickFirst(ctx, cc, addrs[0].Addr); err != nil {
		t.Fatal(err)
	}
}

// TestBalancerSwitch_RoundRobinToGRPCLB tests the scenario where the channel
// starts off with "round_robin", switches to "grpclb" and back.
//
// Note that this test uses the deprecated `loadBalancingPolicy` field in the
// service config.
func (s) TestBalancerSwitch_RoundRobinToGRPCLB(t *testing.T) {
	backends, lbServer, cleanup := setupBackendsAndFakeGRPCLB(t)
	defer cleanup()

	addrs := stubBackendsToResolverAddrs(backends)
	r := manual.NewBuilderWithScheme("whatever")
	target := fmt.Sprintf("%s:///%s", r.Scheme(), loadBalancedServiceName)
	cc, err := grpc.Dial(target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Note the use of the deprecated `loadBalancingPolicy` field here instead
	// of the now recommended `loadBalancingConfig` field. The logic in the
	// ClientConn which decides which balancer to switch to looks at the
	// following places in the given order of preference:
	// - `loadBalancingConfig` field
	// - addresses of type grpclb
	// - `loadBalancingPolicy` field
	// If we use the `loadBalancingPolicy` field, the switch to "grpclb" later on
	// in the test will not happen as the ClientConn will continue to use the LB
	// policy received in the first update.
	scpr := parseServiceConfig(t, r, `{"loadBalancingPolicy": "round_robin"}`)

	// Push a resolver update with the service config specifying "round_robin".
	now := time.Now()
	r.UpdateState(resolver.State{
		Addresses:     addrs,
		ServiceConfig: scpr,
	})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := checkForTraceEvent(ctx, wantRoundRobinTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantRoundRobinTraceDesc, err)
	}
	if err := checkRoundRobin(ctx, cc, addrs); err != nil {
		t.Fatal(err)
	}

	// Push a resolver update with no service config and a single address pointing
	// to the grpclb server we created above. This will cause the channel to
	// switch to the "grpclb" balancer, and will equally distribute RPCs across
	// the backends as the fake grpclb server does not support load reporting from
	// the clients.
	now = time.Now()
	r.UpdateState(resolver.State{
		Addresses:     []resolver.Address{{Addr: lbServer.Address(), Type: resolver.GRPCLB}},
		ServiceConfig: scpr,
	})
	if err := checkForTraceEvent(ctx, wantGRPCLBTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantGRPCLBTraceDesc, err)
	}

	// Switch back to "round_robin".
	now = time.Now()
	r.UpdateState(resolver.State{
		Addresses:     addrs,
		ServiceConfig: scpr,
	})
	if err := checkForTraceEvent(ctx, wantRoundRobinTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantRoundRobinTraceDesc, err)
	}
	if err := checkRoundRobin(ctx, cc, addrs); err != nil {
		t.Fatal(err)
	}
}

// TestBalancerSwitch_grpclbNotRegistered tests the scenario where the grpclb
// balancer is not registered. Verifies that the ClientConn fallbacks to the
// default LB policy or the LB policy specified in the service config, and that
// addresses of type "grpclb" are filtered out.
func (s) TestBalancerSwitch_grpclbNotRegistered(t *testing.T) {
	// Unregister the grpclb balancer builder for the duration of this test.
	grpclbBuilder := balancer.Get("grpclb")
	internal.BalancerUnregister(grpclbBuilder.Name())
	defer balancer.Register(grpclbBuilder)

	backends, cleanup := startBackendsForBalancerSwitch(t)
	defer cleanup()
	addrs := stubBackendsToResolverAddrs(backends)

	r := manual.NewBuilderWithScheme("whatever")
	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Push a resolver update which contains a bunch of stub server backends and a
	// grpclb server address. The latter should get the ClientConn to try and
	// apply the grpclb policy. But since grpclb is not registered, it should
	// fallback to the default LB policy which is pick_first. The ClientConn is
	// also expected to filter out the grpclb address when sending the addresses
	// list fo pick_first.
	grpclbAddr := []resolver.Address{{Addr: "non-existent-grpclb-server-address", Type: resolver.GRPCLB}}
	addrs = append(grpclbAddr, addrs...)
	now := time.Now()
	r.UpdateState(resolver.State{Addresses: addrs})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := checkPickFirst(ctx, cc, addrs[1].Addr); err != nil {
		t.Fatal(err)
	}
	if err := checkForTraceEvent(ctx, wantPickFirstTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantPickFirstTraceDesc, err)
	}

	// Push a resolver update with the same addresses, but with a service config
	// specifying "round_robin". The ClientConn is expected to filter out the
	// grpclb address when sending the addresses list to round_robin.
	r.UpdateState(resolver.State{
		Addresses:     addrs,
		ServiceConfig: parseServiceConfig(t, r, rrServiceConfig),
	})
	if err := checkRoundRobin(ctx, cc, addrs[1:]); err != nil {
		t.Fatal(err)
	}
}

// TestBalancerSwitch_grpclbAddressOverridesLoadBalancingPolicy verifies that
// if the resolver update contains any addresses of type "grpclb", it overrides
// the LB policy specifies in the deprecated `loadBalancingPolicy` field of the
// service config.
func (s) TestBalancerSwitch_grpclbAddressOverridesLoadBalancingPolicy(t *testing.T) {
	backends, lbServer, cleanup := setupBackendsAndFakeGRPCLB(t)
	defer cleanup()

	addrs := stubBackendsToResolverAddrs(backends)
	r := manual.NewBuilderWithScheme("whatever")
	target := fmt.Sprintf("%s:///%s", r.Scheme(), loadBalancedServiceName)
	cc, err := grpc.Dial(target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Push a resolver update containing no grpclb server address. This should
	// lead to the channel using the default LB policy which is pick_first.
	now := time.Now()
	r.UpdateState(resolver.State{Addresses: addrs})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := checkForTraceEvent(ctx, wantPickFirstTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantPickFirstTraceDesc, err)
	}
	if err := checkPickFirst(ctx, cc, addrs[0].Addr); err != nil {
		t.Fatal(err)
	}

	// Push a resolver update with no service config. The addresses list contains
	// the stub backend addresses and a single address pointing to the grpclb
	// server we created above. This will cause the channel to switch to the
	// "grpclb" balancer, and will equally distribute RPCs across the backends.
	now = time.Now()
	r.UpdateState(resolver.State{
		Addresses: append(addrs, resolver.Address{Addr: lbServer.Address(), Type: resolver.GRPCLB}),
	})
	if err := checkForTraceEvent(ctx, wantGRPCLBTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantGRPCLBTraceDesc, err)
	}

	// Push a resolver update with a service config using the deprecated
	// `loadBalancingPolicy` field pointing to round_robin. The addresses list
	// contains an address of type "grpclb". This should be preferred and hence
	// there should be no balancer switch.
	scpr := parseServiceConfig(t, r, `{"loadBalancingPolicy": "round_robin"}`)
	now = time.Now()
	r.UpdateState(resolver.State{
		Addresses:     append(addrs, resolver.Address{Addr: lbServer.Address(), Type: resolver.GRPCLB}),
		ServiceConfig: scpr,
	})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if err := checkForTraceEvent(sCtx, wantRoundRobinTraceDesc, now); err == nil {
		t.Fatal("channel switched balancers when expected not to")
	}
	if err := checkRoundRobin(ctx, cc, addrs); err != nil {
		t.Fatal(err)
	}

	// Switch to "round_robin" by removing the address of type "grpclb".
	now = time.Now()
	r.UpdateState(resolver.State{Addresses: addrs})
	if err := checkRoundRobin(ctx, cc, addrs); err != nil {
		t.Fatal(err)
	}
	if err := checkForTraceEvent(ctx, wantRoundRobinTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantRoundRobinTraceDesc, err)
	}
}

// TestBalancerSwitch_LoadBalancingConfigTrumps verifies that the
// `loadBalancingConfig` field in the service config trumps over addresses of
// type "grpclb" when it comes to deciding which LB policy is applied on the
// channel.
func (s) TestBalancerSwitch_LoadBalancingConfigTrumps(t *testing.T) {
	backends, lbServer, cleanup := setupBackendsAndFakeGRPCLB(t)
	defer cleanup()

	addrs := stubBackendsToResolverAddrs(backends)
	r := manual.NewBuilderWithScheme("whatever")
	target := fmt.Sprintf("%s:///%s", r.Scheme(), loadBalancedServiceName)
	cc, err := grpc.Dial(target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Push a resolver update with no service config and a single address pointing
	// to the grpclb server we created above. This will cause the channel to
	// switch to the "grpclb" balancer, and will equally distribute RPCs across
	// the backends as the fake grpclb server does not support load reporting from
	// the clients.
	now := time.Now()
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: lbServer.Address(), Type: resolver.GRPCLB}}})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := checkRoundRobin(ctx, cc, addrs); err != nil {
		t.Fatal(err)
	}
	if err := checkForTraceEvent(ctx, wantGRPCLBTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantGRPCLBTraceDesc, err)
	}

	// Push a resolver update with the service config specifying "round_robin"
	// through the recommended `loadBalancingConfig` field.
	now = time.Now()
	r.UpdateState(resolver.State{
		Addresses:     addrs,
		ServiceConfig: parseServiceConfig(t, r, rrServiceConfig),
	})
	if err := checkForTraceEvent(ctx, wantRoundRobinTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantRoundRobinTraceDesc, err)
	}
	if err := checkRoundRobin(ctx, cc, addrs); err != nil {
		t.Fatal(err)
	}

	// Push a resolver update with no service config and an address of type
	// "grpclb". The ClientConn should continue to use the service config received
	// earlier, which specified the use of "round_robin" through the
	// `loadBalancingConfig` field, and therefore the balancer should not be
	// switched. And because the `loadBalancingConfig` field trumps everything
	// else, the address of type "grpclb" should be ignored.
	grpclbAddr := resolver.Address{Addr: "non-existent-grpclb-server-address", Type: resolver.GRPCLB}
	now = time.Now()
	r.UpdateState(resolver.State{Addresses: append(addrs, grpclbAddr)})
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if err := checkForTraceEvent(sCtx, wantRoundRobinTraceDesc, now); err == nil {
		t.Fatal("channel switched balancers when expected not to")
	}
	if err := checkRoundRobin(ctx, cc, addrs); err != nil {
		t.Fatal(err)
	}
}

// TestBalancerSwitch_OldBalancerCallsRemoveSubConnInClose tests the scenario
// where the balancer being switched out calls RemoveSubConn() in its Close()
// method. Verifies that this sequence of calls doesn't lead to a deadlock.
func (s) TestBalancerSwitch_OldBalancerCallsRemoveSubConnInClose(t *testing.T) {
	// Register a stub balancer which calls RemoveSubConn() from its Close().
	scChan := make(chan balancer.SubConn, 1)
	uccsCalled := make(chan struct{}, 1)
	stub.Register(t.Name(), stub.BalancerFuncs{
		UpdateClientConnState: func(data *stub.BalancerData, ccs balancer.ClientConnState) error {
			sc, err := data.ClientConn.NewSubConn(ccs.ResolverState.Addresses, balancer.NewSubConnOptions{})
			if err != nil {
				t.Errorf("failed to create subConn: %v", err)
			}
			scChan <- sc
			close(uccsCalled)
			return nil
		},
		Close: func(data *stub.BalancerData) {
			data.ClientConn.RemoveSubConn(<-scChan)
		},
	})

	r := manual.NewBuilderWithScheme("whatever")
	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Push a resolver update specifying our stub balancer as the LB policy.
	scpr := parseServiceConfig(t, r, fmt.Sprintf(`{"loadBalancingPolicy": "%v"}`, t.Name()))
	r.UpdateState(resolver.State{
		Addresses:     []resolver.Address{{Addr: "dummy-address"}},
		ServiceConfig: scpr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatalf("timeout waiting for UpdateClientConnState to be called: %v", ctx.Err())
	case <-uccsCalled:
	}

	// The following service config update will switch balancer from our stub
	// balancer to pick_first. The former will be closed, which will call
	// cc.RemoveSubConn() inline (this RemoveSubConn is not required by the API,
	// but some balancers might do it).
	//
	// This is to make sure the cc.RemoveSubConn() from Close() doesn't cause a
	// deadlock (e.g. trying to grab a mutex while it's already locked).
	//
	// Do it in a goroutine so this test will fail with a helpful message
	// (though the goroutine will still leak).
	done := make(chan struct{})
	go func() {
		r.UpdateState(resolver.State{
			Addresses:     []resolver.Address{{Addr: "dummy-address"}},
			ServiceConfig: parseServiceConfig(t, r, pickFirstServiceConfig),
		})
		close(done)
	}()

	select {
	case <-ctx.Done():
		t.Fatalf("timeout waiting for resolver.UpdateState to finish: %v", ctx.Err())
	case <-done:
	}
}

// TestBalancerSwitch_Graceful tests the graceful switching of LB policies. It
// starts off by configuring "round_robin" on the channel and ensures that RPCs
// are successful. Then, it switches to a stub balancer which does not report a
// picker until instructed by the test do to so. At this point, the test
// verifies that RPCs are still successful using the old balancer. Then the test
// asks the new balancer to report a healthy picker and the test verifies that
// the RPCs get routed using the picker reported by the new balancer.
func (s) TestBalancerSwitch_Graceful(t *testing.T) {
	backends, cleanup := startBackendsForBalancerSwitch(t)
	defer cleanup()
	addrs := stubBackendsToResolverAddrs(backends)

	r := manual.NewBuilderWithScheme("whatever")
	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Push a resolver update with the service config specifying "round_robin".
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	r.UpdateState(resolver.State{
		Addresses:     addrs,
		ServiceConfig: parseServiceConfig(t, r, rrServiceConfig),
	})
	if err := checkForTraceEvent(ctx, wantRoundRobinTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantRoundRobinTraceDesc, err)
	}
	if err := checkRoundRobin(ctx, cc, addrs); err != nil {
		t.Fatal(err)
	}

	// Register a stub balancer which uses a "pick_first" balancer underneath and
	// signals on a channel when it receives ClientConn updates. But it does not
	// forward the ccUpdate to the underlying "pick_first" balancer until the test
	// asks it to do so. This allows us to test the graceful switch functionality.
	// Until the test asks the stub balancer to forward the ccUpdate, RPCs should
	// get routed to the old balancer. And once the test gives the go ahead, RPCs
	// should get routed to the new balancer.
	ccUpdateCh := make(chan struct{})
	waitToProceed := make(chan struct{})
	stub.Register(t.Name(), stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			pf := balancer.Get(grpc.PickFirstBalancerName)
			bd.Data = pf.Build(bd.ClientConn, bd.BuildOptions)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			bal := bd.Data.(balancer.Balancer)
			close(ccUpdateCh)
			go func() {
				<-waitToProceed
				bal.UpdateClientConnState(ccs)
			}()
			return nil
		},
		UpdateSubConnState: func(bd *stub.BalancerData, sc balancer.SubConn, state balancer.SubConnState) {
			bal := bd.Data.(balancer.Balancer)
			bal.UpdateSubConnState(sc, state)
		},
	})

	// Push a resolver update with the service config specifying our stub
	// balancer. We should see a trace event for this balancer switch. But RPCs
	// should still be routed to the old balancer since our stub balancer does not
	// report a ready picker until we ask it to do so.
	now = time.Now()
	r.UpdateState(resolver.State{
		Addresses:     addrs,
		ServiceConfig: r.CC.ParseServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%v": {}}]}`, t.Name())),
	})
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for a ClientConnState update on the new balancer")
	case <-ccUpdateCh:
	}
	wantTraceDesc := fmt.Sprintf("Channel switches to new LB policy %q", t.Name())
	if err := checkForTraceEvent(ctx, wantTraceDesc, now); err != nil {
		t.Fatalf("timeout when waiting for a trace event: %s, err: %v", wantTraceDesc, err)
	}
	if err := checkRoundRobin(ctx, cc, addrs); err != nil {
		t.Fatal(err)
	}

	// Ask our stub balancer to forward the earlier received ccUpdate to the
	// underlying "pick_first" balancer which will result in a healthy picker
	// being reported to the channel. RPCs should start using the new balancer.
	close(waitToProceed)
	if err := checkPickFirst(ctx, cc, addrs[0].Addr); err != nil {
		t.Fatal(err)
	}
}
