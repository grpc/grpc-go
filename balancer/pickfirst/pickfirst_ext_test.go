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

package pickfirst_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/balancer"
	pfbalancer "google.golang.org/grpc/balancer/pickfirst"
	pfinternal "google.golang.org/grpc/balancer/pickfirst/internal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/balancer/weight"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/pickfirst"
	"google.golang.org/grpc/internal/testutils/stats"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

const (
	pickFirstServiceConfig = `{"loadBalancingConfig": [{"pick_first":{}}]}`
	// Default timeout for tests in this package.
	defaultTestTimeout = 10 * time.Second
	// Default short timeout, to be used when waiting for events which are not
	// expected to happen.
	defaultTestShortTimeout  = 100 * time.Millisecond
	stateStoringBalancerName = "state_storing"
)

var (
	stateStoringServiceConfig = fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, stateStoringBalancerName)
	ignoreBalAttributesOpt    = cmp.Transformer("IgnoreBalancerAttributes", func(a resolver.Address) resolver.Address {
		a.BalancerAttributes = nil
		return a
	})
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func init() {
	channelz.TurnOn()
}

// parseServiceConfig is a test helper which uses the manual resolver to parse
// the given service config. It calls t.Fatal() if service config parsing fails.
func parseServiceConfig(t *testing.T, r *manual.Resolver, sc string) *serviceconfig.ParseResult {
	t.Helper()

	scpr := r.CC().ParseServiceConfig(sc)
	if scpr.Err != nil {
		t.Fatalf("Failed to parse service config %q: %v", sc, scpr.Err)
	}
	return scpr
}

// setupPickFirst performs steps required for pick_first tests. It starts a
// bunch of backends exporting the TestService, creates a ClientConn to them
// with service config specifying the use of the pick_first LB policy.
func setupPickFirst(t *testing.T, backendCount int, opts ...grpc.DialOption) (*grpc.ClientConn, *manual.Resolver, []*stubserver.StubServer) {
	t.Helper()

	r := manual.NewBuilderWithScheme("whatever")

	backends := make([]*stubserver.StubServer, backendCount)
	addrs := make([]resolver.Address, backendCount)
	for i := 0; i < backendCount; i++ {
		backend := &stubserver.StubServer{
			EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
				return &testpb.Empty{}, nil
			},
		}
		if err := backend.StartServer(); err != nil {
			t.Fatalf("Failed to start backend: %v", err)
		}
		t.Logf("Started TestService backend at: %q", backend.Address)
		t.Cleanup(func() { backend.Stop() })

		backends[i] = backend
		addrs[i] = resolver.Address{Addr: backend.Address}
	}

	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithDefaultServiceConfig(pickFirstServiceConfig),
	}
	dopts = append(dopts, opts...)
	cc, err := grpc.NewClient(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	// At this point, the resolver has not returned any addresses to the channel.
	// This RPC must block until the context expires.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(sCtx, &testpb.Empty{}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() = %s, want %s", status.Code(err), codes.DeadlineExceeded)
	}
	return cc, r, backends
}

// stubBackendsToResolverAddrs converts from a set of stub server backends to
// resolver addresses. Useful when pushing addresses to the manual resolver.
func stubBackendsToResolverAddrs(backends []*stubserver.StubServer) []resolver.Address {
	addrs := make([]resolver.Address, len(backends))
	for i, backend := range backends {
		addrs[i] = resolver.Address{Addr: backend.Address}
	}
	return addrs
}

// TestPickFirst_OneBackend tests the most basic scenario for pick_first. It
// brings up a single backend and verifies that all RPCs get routed to it.
func (s) TestPickFirst_OneBackend(t *testing.T) {
	cc, r, backends := setupPickFirst(t, 1)

	addrs := stubBackendsToResolverAddrs(backends)
	r.UpdateState(resolver.State{Addresses: addrs})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}
}

// TestPickFirst_MultipleBackends tests the scenario with multiple backends and
// verifies that all RPCs get routed to the first one.
func (s) TestPickFirst_MultipleBackends(t *testing.T) {
	cc, r, backends := setupPickFirst(t, 2)

	addrs := stubBackendsToResolverAddrs(backends)
	r.UpdateState(resolver.State{Addresses: addrs})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}
}

// TestPickFirst_OneServerDown tests the scenario where we have multiple
// backends and pick_first is working as expected. Verifies that RPCs get routed
// to the next backend in the list when the first one goes down.
func (s) TestPickFirst_OneServerDown(t *testing.T) {
	cc, r, backends := setupPickFirst(t, 2)

	addrs := stubBackendsToResolverAddrs(backends)
	r.UpdateState(resolver.State{Addresses: addrs})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// Stop the backend which is currently being used. RPCs should get routed to
	// the next backend in the list.
	backends[0].Stop()
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}
}

// TestPickFirst_AllServersDown tests the scenario where we have multiple
// backends and pick_first is working as expected. When all backends go down,
// the test verifies that RPCs fail with appropriate status code.
func (s) TestPickFirst_AllServersDown(t *testing.T) {
	cc, r, backends := setupPickFirst(t, 2)

	addrs := stubBackendsToResolverAddrs(backends)
	r.UpdateState(resolver.State{Addresses: addrs})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	for _, b := range backends {
		b.Stop()
	}

	client := testgrpc.NewTestServiceClient(cc)
	for {
		if ctx.Err() != nil {
			t.Fatalf("channel failed to move to Unavailable after all backends were stopped: %v", ctx.Err())
		}
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) == codes.Unavailable {
			return
		}
		time.Sleep(defaultTestShortTimeout)
	}
}

// TestPickFirst_AddressesRemoved tests the scenario where we have multiple
// backends and pick_first is working as expected. It then verifies that when
// addresses are removed by the name resolver, RPCs get routed appropriately.
func (s) TestPickFirst_AddressesRemoved(t *testing.T) {
	cc, r, backends := setupPickFirst(t, 3)

	addrs := stubBackendsToResolverAddrs(backends)
	r.UpdateState(resolver.State{Addresses: addrs})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// Remove the first backend from the list of addresses originally pushed.
	// RPCs should get routed to the first backend in the new list.
	r.UpdateState(resolver.State{Addresses: []resolver.Address{addrs[1], addrs[2]}})
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}

	// Append the backend that we just removed to the end of the list.
	// Nothing should change.
	r.UpdateState(resolver.State{Addresses: []resolver.Address{addrs[1], addrs[2], addrs[0]}})
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}

	// Remove the first backend from the existing list of addresses.
	// RPCs should get routed to the first backend in the new list.
	r.UpdateState(resolver.State{Addresses: []resolver.Address{addrs[2], addrs[0]}})
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[2]); err != nil {
		t.Fatal(err)
	}

	// Remove the first backend from the existing list of addresses.
	// RPCs should get routed to the first backend in the new list.
	r.UpdateState(resolver.State{Addresses: []resolver.Address{addrs[0]}})
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}
}

// TestPickFirst_NewAddressWhileBlocking tests the case where pick_first is
// configured on a channel, things are working as expected and then a resolver
// updates removes all addresses. An RPC attempted at this point in time will be
// blocked because there are no valid backends. This test verifies that when new
// backends are added, the RPC is able to complete.
func (s) TestPickFirst_NewAddressWhileBlocking(t *testing.T) {
	cc, r, backends := setupPickFirst(t, 2)
	addrs := stubBackendsToResolverAddrs(backends)
	r.UpdateState(resolver.State{Addresses: addrs})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// Send a resolver update with no addresses. This should push the channel into
	// TransientFailure.
	r.UpdateState(resolver.State{})
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	doneCh := make(chan struct{})
	client := testgrpc.NewTestServiceClient(cc)
	go func() {
		// The channel is currently in TransientFailure and this RPC will block
		// until the channel becomes Ready, which will only happen when we push a
		// resolver update with a valid backend address.
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
			t.Errorf("EmptyCall() = %v, want <nil>", err)
		}
		close(doneCh)
	}()

	// Make sure that there is one pending RPC on the ClientConn before attempting
	// to push new addresses through the name resolver. If we don't do this, the
	// resolver update can happen before the above goroutine gets to make the RPC.
	for {
		if err := ctx.Err(); err != nil {
			t.Fatal(err)
		}
		tcs, _ := channelz.GetTopChannels(0, 0)
		if len(tcs) != 1 {
			t.Fatalf("there should only be one top channel, not %d", len(tcs))
		}
		started := tcs[0].ChannelMetrics.CallsStarted.Load()
		completed := tcs[0].ChannelMetrics.CallsSucceeded.Load() + tcs[0].ChannelMetrics.CallsFailed.Load()
		if (started - completed) == 1 {
			break
		}
		time.Sleep(defaultTestShortTimeout)
	}

	// Send a resolver update with a valid backend to push the channel to Ready
	// and unblock the above RPC.
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: backends[0].Address}}})

	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for blocked RPC to complete")
	case <-doneCh:
	}
}

// TestPickFirst_StickyTransientFailure tests the case where pick_first is
// configured on a channel, and the backend is configured to close incoming
// connections as soon as they are accepted. The test verifies that the channel
// enters TransientFailure and stays there. The test also verifies that the
// pick_first LB policy is constantly trying to reconnect to the backend.
func (s) TestPickFirst_StickyTransientFailure(t *testing.T) {
	// Spin up a local server which closes the connection as soon as it receives
	// one. It also sends a signal on a channel whenever it received a connection.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	t.Cleanup(func() { lis.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	connCh := make(chan struct{}, 1)
	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			select {
			case connCh <- struct{}{}:
				conn.Close()
			case <-ctx.Done():
				return
			}
		}
	}()

	// Dial the above server with a ConnectParams that does a constant backoff
	// of defaultTestShortTimeout duration.
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(pickFirstServiceConfig),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  defaultTestShortTimeout,
				Multiplier: float64(0),
				Jitter:     float64(0),
				MaxDelay:   defaultTestShortTimeout,
			},
		}),
	}
	cc, err := grpc.NewClient(lis.Addr().String(), dopts...)
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}
	t.Cleanup(func() { cc.Close() })
	cc.Connect()
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	// Spawn a goroutine to ensure that the channel stays in TransientFailure.
	// The call to cc.WaitForStateChange will return false when the main
	// goroutine exits and the context is cancelled.
	go func() {
		if cc.WaitForStateChange(ctx, connectivity.TransientFailure) {
			if state := cc.GetState(); state != connectivity.Shutdown {
				t.Errorf("Unexpected state change from TransientFailure to %s", cc.GetState())
			}
		}
	}()

	// Ensures that the pick_first LB policy is constantly trying to reconnect.
	for i := 0; i < 10; i++ {
		select {
		case <-connCh:
		case <-time.After(2 * defaultTestShortTimeout):
			t.Error("Timeout when waiting for pick_first to reconnect")
		}
	}
}

// Tests the PF LB policy with shuffling enabled.
func (s) TestPickFirst_ShuffleAddressList(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.PickFirstWeightedShuffling, false)

	const serviceConfig = `{"loadBalancingConfig": [{"pick_first":{ "shuffleAddressList": true }}]}`

	// Install a shuffler that always reverses two entries.
	origShuf := pfinternal.RandShuffle
	defer func() { pfinternal.RandShuffle = origShuf }()
	pfinternal.RandShuffle = func(n int, f func(int, int)) {
		if n != 2 {
			t.Errorf("Shuffle called with n=%v; want 2", n)
			return
		}
		f(0, 1) // reverse the two addresses
	}
	// Set up our backends.
	cc, r, backends := setupPickFirst(t, 2)
	addrs := stubBackendsToResolverAddrs(backends)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Push an update with both addresses and shuffling disabled.  We should
	// connect to backend 0.
	r.UpdateState(resolver.State{Endpoints: []resolver.Endpoint{
		{Addresses: []resolver.Address{addrs[0]}},
		{Addresses: []resolver.Address{addrs[1]}},
	}})
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// Send a config with shuffling enabled.  This will reverse the addresses,
	// but the channel should still be connected to backend 0.
	shufState := resolver.State{
		ServiceConfig: parseServiceConfig(t, r, serviceConfig),
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{addrs[0]}},
			{Addresses: []resolver.Address{addrs[1]}},
		},
	}
	r.UpdateState(shufState)
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// Send a resolver update with no addresses. This should push the channel
	// into TransientFailure.
	r.UpdateState(resolver.State{})
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	// Send the same config as last time with shuffling enabled.  Since we are
	// not connected to backend 0, we should connect to backend 1.
	r.UpdateState(shufState)
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}
}

// Tests the PF LB policy with shuffling enabled. It explicitly unsets the
// Endpoints field in the resolver update to test the shuffling of the
// Addresses.
func (s) TestPickFirst_ShuffleAddressListNoEndpoints(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.PickFirstWeightedShuffling, false)

	// Install a shuffler that always reverses two entries.
	origShuf := pfinternal.RandShuffle
	defer func() { pfinternal.RandShuffle = origShuf }()
	pfinternal.RandShuffle = func(n int, f func(int, int)) {
		if n != 2 {
			t.Errorf("Shuffle called with n=%v; want 2", n)
			return
		}
		f(0, 1) // reverse the two addresses
	}

	pfBuilder := balancer.Get(pfbalancer.Name)
	shuffleConfig, err := pfBuilder.(balancer.ConfigParser).ParseConfig(json.RawMessage(`{ "shuffleAddressList": true }`))
	if err != nil {
		t.Fatal(err)
	}
	noShuffleConfig, err := pfBuilder.(balancer.ConfigParser).ParseConfig(json.RawMessage(`{ "shuffleAddressList": false }`))
	if err != nil {
		t.Fatal(err)
	}
	var activeCfg serviceconfig.LoadBalancingConfig

	bf := stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.ChildBalancer = pfBuilder.Build(bd.ClientConn, bd.BuildOptions)
		},
		Close: func(bd *stub.BalancerData) {
			bd.ChildBalancer.Close()
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			ccs.BalancerConfig = activeCfg
			ccs.ResolverState.Endpoints = nil
			return bd.ChildBalancer.UpdateClientConnState(ccs)
		},
	}

	stub.Register(t.Name(), bf)
	svcCfg := fmt.Sprintf(`{ "loadBalancingConfig": [{%q: {}}] }`, t.Name())
	// Set up our backends.
	cc, r, backends := setupPickFirst(t, 2, grpc.WithDefaultServiceConfig(svcCfg))
	addrs := stubBackendsToResolverAddrs(backends)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Push an update with both addresses and shuffling disabled.  We should
	// connect to backend 0.
	activeCfg = noShuffleConfig
	resolverState := resolver.State{Addresses: addrs}
	r.UpdateState(resolverState)
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// Send a config with shuffling enabled.  This will reverse the addresses,
	// but the channel should still be connected to backend 0.
	activeCfg = shuffleConfig
	r.UpdateState(resolverState)
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// Send a resolver update with no addresses. This should push the channel
	// into TransientFailure.
	r.UpdateState(resolver.State{})
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	// Send the same config as last time with shuffling enabled.  Since we are
	// not connected to backend 0, we should connect to backend 1.
	r.UpdateState(resolverState)
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}
}

// Tests the PF LB policy with weighted shuffling enabled.
func (s) TestPickFirst_ShuffleAddressList_WeightedShuffling(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.PickFirstWeightedShuffling, true)

	const serviceConfig = `{"loadBalancingConfig": [{"pick_first":{ "shuffleAddressList": true }}]}`

	// Install a rand func that returns a constant value. The test sets up three
	// endpoints with increasing weights. This means that in the weighted
	// shuffling algorithm, the endpoints will end up with increasing values for
	// their keys.  And since the algorithm sorts in descending order, the last
	// endpoint should be the one that would get picked.
	origRand := pfinternal.RandFloat64
	defer func() { pfinternal.RandFloat64 = origRand }()
	pfinternal.RandFloat64 = func() float64 {
		return 0.5
	}

	// Set up our backends.
	cc, r, backends := setupPickFirst(t, 3)
	addrs := stubBackendsToResolverAddrs(backends)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create endpoints for the above backends with increasing weights.
	ep1 := resolver.Endpoint{Addresses: []resolver.Address{addrs[0]}}
	ep1 = weight.Set(ep1, weight.EndpointInfo{Weight: 357913941}) // Normalized weight of 1/6
	ep2 := resolver.Endpoint{Addresses: []resolver.Address{addrs[1]}}
	ep2 = weight.Set(ep2, weight.EndpointInfo{Weight: 715827882}) // Normalized weight of 2/6
	ep3 := resolver.Endpoint{Addresses: []resolver.Address{addrs[2]}}
	ep3 = weight.Set(ep3, weight.EndpointInfo{Weight: 1073741824}) // Normalized weight of 3/6

	// Push an update with all addresses and shuffling disabled. We should
	// connect to backend 0.
	r.UpdateState(resolver.State{Endpoints: []resolver.Endpoint{ep1, ep2, ep3}})
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// Send a config with shuffling enabled. This will reverse the addresses,
	// but the channel should still be connected to backend 0.
	shufState := resolver.State{
		ServiceConfig: parseServiceConfig(t, r, serviceConfig),
		Endpoints:     []resolver.Endpoint{ep1, ep2, ep3},
	}
	r.UpdateState(shufState)
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// Send a resolver update with no addresses. This should push the channel
	// into TransientFailure.
	r.UpdateState(resolver.State{})
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	// Send the same config as last time with shuffling enabled.  Since we are
	// not connected to backend 0, we should connect to backend 2.
	r.UpdateState(shufState)
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[2]); err != nil {
		t.Fatal(err)
	}
}

// Test config parsing with the env var turned on and off for various scenarios.
func (s) TestPickFirst_ParseConfig_Success(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.PickFirstWeightedShuffling, false)

	// Install a shuffler that always reverses two entries.
	origShuf := pfinternal.RandShuffle
	defer func() { pfinternal.RandShuffle = origShuf }()
	pfinternal.RandShuffle = func(n int, f func(int, int)) {
		if n != 2 {
			t.Errorf("Shuffle called with n=%v; want 2", n)
			return
		}
		f(0, 1) // reverse the two addresses
	}

	tests := []struct {
		name          string
		serviceConfig string
		wantFirstAddr bool
	}{
		{
			name:          "empty pickfirst config",
			serviceConfig: `{"loadBalancingConfig": [{"pick_first":{}}]}`,
			wantFirstAddr: true,
		},
		{
			name:          "empty good pickfirst config",
			serviceConfig: `{"loadBalancingConfig": [{"pick_first":{ "shuffleAddressList": true }}]}`,
			wantFirstAddr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Set up our backends.
			cc, r, backends := setupPickFirst(t, 2)
			addrs := stubBackendsToResolverAddrs(backends)

			r.UpdateState(resolver.State{
				ServiceConfig: parseServiceConfig(t, r, test.serviceConfig),
				Addresses:     addrs,
			})

			// Some tests expect address shuffling to happen, and indicate that
			// by setting wantFirstAddr to false (since our shuffling function
			// defined at the top of this test, simply reverses the list of
			// addresses provided to it).
			wantAddr := addrs[0]
			if !test.wantFirstAddr {
				wantAddr = addrs[1]
			}

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if err := pickfirst.CheckRPCsToBackend(ctx, cc, wantAddr); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Test config parsing for a bad service config.
func (s) TestPickFirst_ParseConfig_Failure(t *testing.T) {
	// Service config should fail with the below config. Name resolvers are
	// expected to perform this parsing before they push the parsed service
	// config to the channel.
	const sc = `{"loadBalancingConfig": [{"pick_first":{ "shuffleAddressList": 666 }}]}`
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(sc)
	if scpr.Err == nil {
		t.Fatalf("ParseConfig() succeeded and returned %+v, when expected to fail", scpr)
	}
}

// setupPickFirstWithListenerWrapper is very similar to setupPickFirst, but uses
// a wrapped listener that the test can use to track accepted connections.
func setupPickFirstWithListenerWrapper(t *testing.T, backendCount int, opts ...grpc.DialOption) (*grpc.ClientConn, *manual.Resolver, []*stubserver.StubServer, []*testutils.ListenerWrapper) {
	t.Helper()

	backends := make([]*stubserver.StubServer, backendCount)
	addrs := make([]resolver.Address, backendCount)
	listeners := make([]*testutils.ListenerWrapper, backendCount)
	for i := 0; i < backendCount; i++ {
		lis := testutils.NewListenerWrapper(t, nil)
		backend := &stubserver.StubServer{
			Listener: lis,
			EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
				return &testpb.Empty{}, nil
			},
		}
		if err := backend.StartServer(); err != nil {
			t.Fatalf("Failed to start backend: %v", err)
		}
		t.Logf("Started TestService backend at: %q", backend.Address)
		t.Cleanup(func() { backend.Stop() })

		backends[i] = backend
		addrs[i] = resolver.Address{Addr: backend.Address}
		listeners[i] = lis
	}

	r := manual.NewBuilderWithScheme("whatever")
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithDefaultServiceConfig(pickFirstServiceConfig),
	}
	dopts = append(dopts, opts...)
	cc, err := grpc.NewClient(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	// At this point, the resolver has not returned any addresses to the channel.
	// This RPC must block until the context expires.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(sCtx, &testpb.Empty{}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() = %s, want %s", status.Code(err), codes.DeadlineExceeded)
	}
	return cc, r, backends, listeners
}

// TestPickFirst_AddressUpdateWithAttributes tests the case where an address
// update received by the pick_first LB policy differs in attributes. Addresses
// which differ in attributes are considered different from the perspective of
// subconn creation and connection establishment and the test verifies that new
// connections are created when attributes change.
func (s) TestPickFirst_AddressUpdateWithAttributes(t *testing.T) {
	cc, r, backends, listeners := setupPickFirstWithListenerWrapper(t, 2)

	// Add a set of attributes to the addresses before pushing them to the
	// pick_first LB policy through the manual resolver.
	addrs := stubBackendsToResolverAddrs(backends)
	for i := range addrs {
		addrs[i].Attributes = addrs[i].Attributes.WithValue("test-attribute-1", fmt.Sprintf("%d", i))
	}
	r.UpdateState(resolver.State{Addresses: addrs})

	// Ensure that RPCs succeed to the first backend in the list.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// Grab the wrapped connection from the listener wrapper. This will be used
	// to verify the connection is closed.
	val, err := listeners[0].NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Failed to receive new connection from wrapped listener: %v", err)
	}
	conn := val.(*testutils.ConnWrapper)

	// Add another set of attributes to the addresses, and push them to the
	// pick_first LB policy through the manual resolver. Leave the order of the
	// addresses unchanged.
	for i := range addrs {
		addrs[i].Attributes = addrs[i].Attributes.WithValue("test-attribute-2", fmt.Sprintf("%d", i))
	}
	r.UpdateState(resolver.State{Addresses: addrs})
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// A change in the address attributes results in the new address being
	// considered different to the current address. This will result in the old
	// connection being closed and a new connection to the same backend (since
	// address order is not modified).
	if _, err := conn.CloseCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout when expecting existing connection to be closed: %v", err)
	}
	val, err = listeners[0].NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Failed to receive new connection from wrapped listener: %v", err)
	}
	conn = val.(*testutils.ConnWrapper)

	// Add another set of attributes to the addresses, and push them to the
	// pick_first LB policy through the manual resolver.  Reverse of the order
	// of addresses.
	for i := range addrs {
		addrs[i].Attributes = addrs[i].Attributes.WithValue("test-attribute-3", fmt.Sprintf("%d", i))
	}
	addrs[0], addrs[1] = addrs[1], addrs[0]
	r.UpdateState(resolver.State{Addresses: addrs})
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// Ensure that the old connection is closed and a new connection is
	// established to the first address in the new list.
	if _, err := conn.CloseCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout when expecting existing connection to be closed: %v", err)
	}
	_, err = listeners[1].NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Failed to receive new connection from wrapped listener: %v", err)
	}
}

// TestPickFirst_AddressUpdateWithBalancerAttributes tests the case where an
// address update received by the pick_first LB policy differs in balancer
// attributes, which are meant only for consumption by LB policies. In this
// case, the test verifies that new connections are not created when the address
// update only changes the balancer attributes.
func (s) TestPickFirst_AddressUpdateWithBalancerAttributes(t *testing.T) {
	cc, r, backends, listeners := setupPickFirstWithListenerWrapper(t, 2)

	// Add a set of balancer attributes to the addresses before pushing them to
	// the pick_first LB policy through the manual resolver.
	addrs := stubBackendsToResolverAddrs(backends)
	for i := range addrs {
		addrs[i].BalancerAttributes = addrs[i].BalancerAttributes.WithValue("test-attribute-1", fmt.Sprintf("%d", i))
	}
	r.UpdateState(resolver.State{Addresses: addrs})

	// Ensure that RPCs succeed to the expected backend.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// Grab the wrapped connection from the listener wrapper. This will be used
	// to verify the connection is not closed.
	val, err := listeners[0].NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Failed to receive new connection from wrapped listener: %v", err)
	}
	conn := val.(*testutils.ConnWrapper)

	// Add a set of balancer attributes to the addresses before pushing them to
	// the pick_first LB policy through the manual resolver. Leave the order of
	// the addresses unchanged.
	for i := range addrs {
		addrs[i].BalancerAttributes = addrs[i].BalancerAttributes.WithValue("test-attribute-2", fmt.Sprintf("%d", i))
	}
	r.UpdateState(resolver.State{Addresses: addrs})

	// Ensure that no new connection is established, and ensure that the old
	// connection is not closed.
	for i := range listeners {
		sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
		defer sCancel()
		if _, err := listeners[i].NewConnCh.Receive(sCtx); err != context.DeadlineExceeded {
			t.Fatalf("Unexpected error when expecting no new connection: %v", err)
		}
	}
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := conn.CloseCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("Unexpected error when expecting existing connection to stay active: %v", err)
	}
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// Add a set of balancer attributes to the addresses before pushing them to
	// the pick_first LB policy through the manual resolver. Reverse of the
	// order of addresses.
	for i := range addrs {
		addrs[i].BalancerAttributes = addrs[i].BalancerAttributes.WithValue("test-attribute-3", fmt.Sprintf("%d", i))
	}
	addrs[0], addrs[1] = addrs[1], addrs[0]
	r.UpdateState(resolver.State{Addresses: addrs})

	// Ensure that no new connection is established, and ensure that the old
	// connection is not closed.
	for i := range listeners {
		sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
		defer sCancel()
		if _, err := listeners[i].NewConnCh.Receive(sCtx); err != context.DeadlineExceeded {
			t.Fatalf("Unexpected error when expecting no new connection: %v", err)
		}
	}
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := conn.CloseCh.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("Unexpected error when expecting existing connection to stay active: %v", err)
	}
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where the pick_first LB policy receives an error from the name
// resolver without previously receiving a good update. Verifies that the
// channel moves to TRANSIENT_FAILURE and that error received from the name
// resolver is propagated to the caller of an RPC.
func (s) TestPickFirst_ResolverError_NoPreviousUpdate(t *testing.T) {
	cc, r, _ := setupPickFirst(t, 0)

	nrErr := errors.New("error from name resolver")
	r.CC().ReportError(nrErr)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	client := testgrpc.NewTestServiceClient(cc)
	_, err := client.EmptyCall(ctx, &testpb.Empty{})
	if err == nil {
		t.Fatalf("EmptyCall() succeeded when expected to fail with error: %v", nrErr)
	}
	if !strings.Contains(err.Error(), nrErr.Error()) {
		t.Fatalf("EmptyCall() failed with error: %v, want error: %v", err, nrErr)
	}
}

// Tests the case where the pick_first LB policy receives an error from the name
// resolver after receiving a good update (and the channel is currently READY).
// The test verifies that the channel continues to use the previously received
// good update.
func (s) TestPickFirst_ResolverError_WithPreviousUpdate_Ready(t *testing.T) {
	cc, r, backends := setupPickFirst(t, 1)

	addrs := stubBackendsToResolverAddrs(backends)
	r.UpdateState(resolver.State{Addresses: addrs})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	nrErr := errors.New("error from name resolver")
	r.CC().ReportError(nrErr)

	// Ensure that RPCs continue to succeed for the next second.
	client := testgrpc.NewTestServiceClient(cc)
	for end := time.Now().Add(time.Second); time.Now().Before(end); <-time.After(defaultTestShortTimeout) {
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			t.Fatalf("EmptyCall() failed: %v", err)
		}
	}
}

// Tests the case where the pick_first LB policy receives an error from the name
// resolver after receiving a good update (and the channel is currently in
// CONNECTING state). The test verifies that the channel continues to use the
// previously received good update, and that RPCs don't fail with the error
// received from the name resolver.
func (s) TestPickFirst_ResolverError_WithPreviousUpdate_Connecting(t *testing.T) {
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}

	// Listen on a local port and act like a server that blocks until the
	// channel reaches CONNECTING and closes the connection without sending a
	// server preface.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForConnecting := make(chan struct{})
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			t.Errorf("Unexpected error when accepting a connection: %v", err)
		}
		defer conn.Close()

		select {
		case <-waitForConnecting:
		case <-ctx.Done():
			t.Error("Timeout when waiting for channel to move to CONNECTING state")
		}
	}()

	r := manual.NewBuilderWithScheme("whatever")
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithDefaultServiceConfig(pickFirstServiceConfig),
	}
	cc, err := grpc.NewClient(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })
	cc.Connect()
	addrs := []resolver.Address{{Addr: lis.Addr().String()}}
	r.UpdateState(resolver.State{Addresses: addrs})
	testutils.AwaitState(ctx, t, cc, connectivity.Connecting)

	nrErr := errors.New("error from name resolver")
	r.CC().ReportError(nrErr)

	// RPCs should fail with deadline exceed error as long as they are in
	// CONNECTING and not the error returned by the name resolver.
	client := testgrpc.NewTestServiceClient(cc)
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := client.EmptyCall(sCtx, &testpb.Empty{}); !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("EmptyCall() failed with error: %v, want error: %v", err, context.DeadlineExceeded)
	}

	// Closing this channel leads to closing of the connection by our listener.
	// gRPC should see this as a connection error.
	close(waitForConnecting)
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)
	checkForConnectionError(ctx, t, cc)
}

// Tests the case where the pick_first LB policy receives an error from the name
// resolver after receiving a good update. The previous good update though has
// seen the channel move to TRANSIENT_FAILURE.  The test verifies that the
// channel fails RPCs with the new error from the resolver.
func (s) TestPickFirst_ResolverError_WithPreviousUpdate_TransientFailure(t *testing.T) {
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}

	// Listen on a local port and act like a server that closes the connection
	// without sending a server preface.
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			t.Errorf("Unexpected error when accepting a connection: %v", err)
		}
		conn.Close()
	}()

	r := manual.NewBuilderWithScheme("whatever")
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithDefaultServiceConfig(pickFirstServiceConfig),
	}
	cc, err := grpc.NewClient(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })
	cc.Connect()
	addrs := []resolver.Address{{Addr: lis.Addr().String()}}
	r.UpdateState(resolver.State{Addresses: addrs})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)
	checkForConnectionError(ctx, t, cc)

	// An error from the name resolver should result in RPCs failing with that
	// error instead of the old error that caused the channel to move to
	// TRANSIENT_FAILURE in the first place.
	nrErr := errors.New("error from name resolver")
	r.CC().ReportError(nrErr)
	client := testgrpc.NewTestServiceClient(cc)
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}); strings.Contains(err.Error(), nrErr.Error()) {
			break
		}
	}
	if ctx.Err() != nil {
		t.Fatal("Timeout when waiting for RPCs to fail with error returned by the name resolver")
	}
}

func checkForConnectionError(ctx context.Context, t *testing.T, cc *grpc.ClientConn) {
	t.Helper()

	// RPCs may fail on the client side in two ways, once the fake server closes
	// the accepted connection:
	// - writing the client preface succeeds, but not reading the server preface
	// - writing the client preface fails
	// In either case, we should see it fail with UNAVAILABLE.
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("EmptyCall() failed with error: %v, want code %v", err, codes.Unavailable)
	}
}

// Tests the case where the pick_first LB policy receives an update from the
// name resolver with no addresses after receiving a good update. The test
// verifies that the channel fails RPCs with an error indicating the fact that
// the name resolver returned no addresses.
func (s) TestPickFirst_ResolverError_ZeroAddresses_WithPreviousUpdate(t *testing.T) {
	cc, r, backends := setupPickFirst(t, 1)

	addrs := stubBackendsToResolverAddrs(backends)
	r.UpdateState(resolver.State{Addresses: addrs})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	r.UpdateState(resolver.State{})
	wantErr := "produced zero addresses"
	client := testgrpc.NewTestServiceClient(cc)
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}); strings.Contains(err.Error(), wantErr) {
			break
		}
	}
	if ctx.Err() != nil {
		t.Fatal("Timeout when waiting for RPCs to fail with error returned by the name resolver")
	}
}

// testServer is a server than can be stopped and resumed without closing
// the listener. This guarantees the same port number (and address) is used
// after restart. When a server is stopped, it accepts and closes all tcp
// connections from clients.
type testServer struct {
	stubserver.StubServer
	lis *testutils.RestartableListener
}

func (s *testServer) stop() {
	s.lis.Stop()
}

func (s *testServer) resume() {
	s.lis.Restart()
}

func newTestServer(t *testing.T) *testServer {
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	rl := testutils.NewRestartableListener(l)
	ss := stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
		Listener:   rl,
	}
	return &testServer{
		StubServer: ss,
		lis:        rl,
	}
}

// setupPickFirstLeaf performs steps required for pick_first tests. It starts a
// bunch of backends exporting the TestService, and creates a ClientConn to them.
func setupPickFirstLeaf(t *testing.T, backendCount int, opts ...grpc.DialOption) (*grpc.ClientConn, *manual.Resolver, *backendManager) {
	t.Helper()
	r := manual.NewBuilderWithScheme("whatever")
	backends := make([]*testServer, backendCount)
	addrs := make([]resolver.Address, backendCount)

	for i := 0; i < backendCount; i++ {
		server := newTestServer(t)
		backend := stubserver.StartTestService(t, &server.StubServer)
		t.Cleanup(func() {
			backend.Stop()
		})
		backends[i] = server
		addrs[i] = resolver.Address{Addr: backend.Address}
	}

	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
	}
	dopts = append(dopts, opts...)
	cc, err := grpc.NewClient(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	// At this point, the resolver has not returned any addresses to the channel.
	// This RPC must block until the context expires.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(sCtx, &testpb.Empty{}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() = %s, want %s", status.Code(err), codes.DeadlineExceeded)
	}
	return cc, r, &backendManager{backends}
}

// TestPickFirstLeaf_SimpleResolverUpdate tests the behaviour of the pick first
// policy when given an list of addresses. The following steps are carried
// out in order:
//  1. A list of addresses are given through the resolver. Only one
//     of the servers is running.
//  2. RPCs are sent to verify they reach the running server.
//
// The state transitions of the ClientConn and all the SubConns created are
// verified.
func (s) TestPickFirstLeaf_SimpleResolverUpdate_FirstServerReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})

	cc, r, bm := setupPickFirstLeaf(t, 2, grpc.WithDefaultServiceConfig(stateStoringServiceConfig))
	addrs := bm.resolverAddrs()
	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

	r.UpdateState(resolver.State{Addresses: addrs})
	var bal *stateStoringBalancer
	select {
	case bal = <-balCh:
	case <-ctx.Done():
		t.Fatal("Context expired while waiting for balancer to be built")
	}
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	wantSCStates := []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Ready},
	}
	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions()); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_SimpleResolverUpdate_FirstServerUnReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})

	cc, r, bm := setupPickFirstLeaf(t, 2, grpc.WithDefaultServiceConfig(stateStoringServiceConfig))
	addrs := bm.resolverAddrs()
	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)
	bm.stopAllExcept(1)

	r.UpdateState(resolver.State{Addresses: addrs})
	var bal *stateStoringBalancer
	select {
	case bal = <-balCh:
	case <-ctx.Done():
		t.Fatal("Context expired while waiting for balancer to be built")
	}
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}

	wantSCStates := []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Ready},
	}
	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions()); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_SimpleResolverUpdate_DuplicateAddrs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})

	cc, r, bm := setupPickFirstLeaf(t, 2, grpc.WithDefaultServiceConfig(stateStoringServiceConfig))
	addrs := bm.resolverAddrs()
	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)
	bm.stopAllExcept(1)

	// Add a duplicate entry in the addresslist
	r.UpdateState(resolver.State{
		Addresses: append([]resolver.Address{addrs[0]}, addrs...),
	})
	var bal *stateStoringBalancer
	select {
	case bal = <-balCh:
	case <-ctx.Done():
		t.Fatal("Context expired while waiting for balancer to be built")
	}
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}

	wantSCStates := []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Ready},
	}
	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions()); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

// TestPickFirstLeaf_ResolverUpdates_DisjointLists tests the behaviour of the pick first
// policy when the following steps are carried out in order:
//  1. A list of addresses are given through the resolver. Only one
//     of the servers is running.
//  2. RPCs are sent to verify they reach the running server.
//  3. A second resolver update is sent. Again, only one of the servers is
//     running. This may not be the same server as before.
//  4. RPCs are sent to verify they reach the running server.
//
// The state transitions of the ClientConn and all the SubConns created are
// verified.
func (s) TestPickFirstLeaf_ResolverUpdates_DisjointLists(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	cc, r, bm := setupPickFirstLeaf(t, 4, grpc.WithDefaultServiceConfig(stateStoringServiceConfig))
	addrs := bm.resolverAddrs()
	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

	bm.backends[0].stop()
	r.UpdateState(resolver.State{Addresses: []resolver.Address{addrs[0], addrs[1]}})
	var bal *stateStoringBalancer
	select {
	case bal = <-balCh:
	case <-ctx.Done():
		t.Fatal("Context expired while waiting for balancer to be built")
	}
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}
	wantSCStates := []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	bm.backends[2].stop()
	r.UpdateState(resolver.State{Addresses: []resolver.Address{addrs[2], addrs[3]}})

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[3]); err != nil {
		t.Fatal(err)
	}
	wantSCStates = []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[2]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[3]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions()); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_ResolverUpdates_ActiveBackendInUpdatedList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	cc, r, bm := setupPickFirstLeaf(t, 3, grpc.WithDefaultServiceConfig(stateStoringServiceConfig))
	addrs := bm.resolverAddrs()
	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

	bm.backends[0].stop()
	r.UpdateState(resolver.State{Addresses: []resolver.Address{addrs[0], addrs[1]}})
	var bal *stateStoringBalancer
	select {
	case bal = <-balCh:
	case <-ctx.Done():
		t.Fatal("Context expired while waiting for balancer to be built")
	}
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}
	wantSCStates := []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	bm.backends[2].stop()
	r.UpdateState(resolver.State{Addresses: []resolver.Address{addrs[2], addrs[1]}})

	// Verify that the ClientConn stays in READY.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	testutils.AwaitNoStateChange(sCtx, t, cc, connectivity.Ready)

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}
	wantSCStates = []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions()); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_ResolverUpdates_InActiveBackendInUpdatedList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	cc, r, bm := setupPickFirstLeaf(t, 3, grpc.WithDefaultServiceConfig(stateStoringServiceConfig))
	addrs := bm.resolverAddrs()
	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

	bm.backends[0].stop()
	r.UpdateState(resolver.State{Addresses: []resolver.Address{addrs[0], addrs[1]}})
	var bal *stateStoringBalancer
	select {
	case bal = <-balCh:
	case <-ctx.Done():
		t.Fatal("Context expired while waiting for balancer to be built")
	}
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}
	wantSCStates := []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	bm.backends[2].stop()
	bm.backends[0].resume()

	r.UpdateState(resolver.State{Addresses: []resolver.Address{addrs[0], addrs[2]}})

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}
	wantSCStates = []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions()); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_ResolverUpdates_IdenticalLists(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	cc, r, bm := setupPickFirstLeaf(t, 2, grpc.WithDefaultServiceConfig(stateStoringServiceConfig))
	addrs := bm.resolverAddrs()
	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

	bm.backends[0].stop()
	r.UpdateState(resolver.State{Addresses: []resolver.Address{addrs[0], addrs[1]}})
	var bal *stateStoringBalancer
	select {
	case bal = <-balCh:
	case <-ctx.Done():
		t.Fatal("Context expired while waiting for balancer to be built")
	}
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}
	wantSCStates := []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	r.UpdateState(resolver.State{Addresses: []resolver.Address{addrs[0], addrs[1]}})

	// Verify that the ClientConn stays in READY.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	testutils.AwaitNoStateChange(sCtx, t, cc, connectivity.Ready)

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}
	wantSCStates = []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions()); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

// TestPickFirstLeaf_StopConnectedServer tests the behaviour of the pick first
// policy when the connected server is shut down. It carries out the following
// steps in order:
//  1. A list of addresses are given through the resolver. Only one
//     of the servers is running.
//  2. The running server is stopped, causing the ClientConn to enter IDLE.
//  3. A (possibly different) server is started.
//  4. RPCs are made to kick the ClientConn out of IDLE. The test verifies that
//     the RPCs reach the running server.
//
// The test verifies the ClientConn state transitions.
func (s) TestPickFirstLeaf_StopConnectedServer_FirstServerRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	cc, r, bm := setupPickFirstLeaf(t, 2, grpc.WithDefaultServiceConfig(stateStoringServiceConfig))
	addrs := bm.resolverAddrs()
	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

	// shutdown all active backends except the target.
	bm.stopAllExcept(0)

	r.UpdateState(resolver.State{Addresses: addrs})
	var bal *stateStoringBalancer
	select {
	case bal = <-balCh:
	case <-ctx.Done():
		t.Fatal("Context expired while waiting for balancer to be built")
	}
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	wantSCStates := []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	// Shut down the connected server.
	bm.backends[0].stop()
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Start the new target server.
	bm.backends[0].resume()

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Idle,
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions()); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_StopConnectedServer_SecondServerRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	cc, r, bm := setupPickFirstLeaf(t, 2, grpc.WithDefaultServiceConfig(stateStoringServiceConfig))
	addrs := bm.resolverAddrs()
	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

	// shutdown all active backends except the target.
	bm.stopAllExcept(1)

	r.UpdateState(resolver.State{Addresses: addrs})
	var bal *stateStoringBalancer
	select {
	case bal = <-balCh:
	case <-ctx.Done():
		t.Fatal("Context expired while waiting for balancer to be built")
	}
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}

	wantSCStates := []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	// Shut down the connected server.
	bm.backends[1].stop()
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Start the new target server.
	bm.backends[1].resume()

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}

	wantSCStates = []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Ready},
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Idle,
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions()); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_StopConnectedServer_SecondServerToFirst(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	cc, r, bm := setupPickFirstLeaf(t, 2, grpc.WithDefaultServiceConfig(stateStoringServiceConfig))
	addrs := bm.resolverAddrs()
	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

	// shutdown all active backends except the target.
	bm.stopAllExcept(1)

	r.UpdateState(resolver.State{Addresses: addrs})
	var bal *stateStoringBalancer
	select {
	case bal = <-balCh:
	case <-ctx.Done():
		t.Fatal("Context expired while waiting for balancer to be built")
	}
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}

	wantSCStates := []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	// Shut down the connected server.
	bm.backends[1].stop()
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Start the new target server.
	bm.backends[0].resume()

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	wantSCStates = []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Idle,
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions()); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_StopConnectedServer_FirstServerToSecond(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	cc, r, bm := setupPickFirstLeaf(t, 2, grpc.WithDefaultServiceConfig(stateStoringServiceConfig))
	addrs := bm.resolverAddrs()
	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

	// shutdown all active backends except the target.
	bm.stopAllExcept(0)

	r.UpdateState(resolver.State{Addresses: addrs})
	var bal *stateStoringBalancer
	select {
	case bal = <-balCh:
	case <-ctx.Done():
		t.Fatal("Context expired while waiting for balancer to be built")
	}
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	wantSCStates := []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	// Shut down the connected server.
	bm.backends[0].stop()
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Start the new target server.
	bm.backends[1].resume()

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}

	wantSCStates = []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates(), ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Idle,
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions()); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

// TestPickFirstLeaf_EmptyAddressList carries out the following steps in order:
// 1. Send a resolver update with one running backend.
// 2. Send an empty address list causing the balancer to enter TRANSIENT_FAILURE.
// 3. Send a resolver update with one running backend.
// The test verifies the ClientConn state transitions.
func (s) TestPickFirstLeaf_EmptyAddressList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	balChan := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balChan})
	cc, r, bm := setupPickFirstLeaf(t, 1, grpc.WithDefaultServiceConfig(stateStoringServiceConfig))
	addrs := bm.resolverAddrs()

	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

	r.UpdateState(resolver.State{Addresses: addrs})
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	r.UpdateState(resolver.State{})
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	// The balancer should have entered transient failure.
	// It should transition to CONNECTING from TRANSIENT_FAILURE as sticky TF
	// only applies when the initial TF is reported due to connection failures
	// and not bad resolver states.
	r.UpdateState(resolver.State{Addresses: addrs})
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	wantTransitions := []connectivity.State{
		// From first resolver update.
		connectivity.Connecting,
		connectivity.Ready,
		// From second update.
		connectivity.TransientFailure,
		// From third update.
		connectivity.Connecting,
		connectivity.Ready,
	}

	if diff := cmp.Diff(wantTransitions, stateSubscriber.transitions()); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

// Test verifies that pickfirst correctly detects the end of the first happy
// eyeballs pass when the timer causes pickfirst to reach the end of the address
// list and failures are reported out of order.
func (s) TestPickFirstLeaf_HappyEyeballs_TF_AfterEndOfList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	originalTimer := pfinternal.TimeAfterFunc
	defer func() {
		pfinternal.TimeAfterFunc = originalTimer
	}()
	triggerTimer, timeAfter := mockTimer()
	pfinternal.TimeAfterFunc = timeAfter

	tmr := stats.NewTestMetricsRecorder()
	dialer := testutils.NewBlockingDialer()
	opts := []grpc.DialOption{
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, pfbalancer.Name)),
		grpc.WithContextDialer(dialer.DialContext),
		grpc.WithStatsHandler(tmr),
	}
	cc, rb, bm := setupPickFirstLeaf(t, 3, opts...)
	addrs := bm.resolverAddrs()
	holds := bm.holds(dialer)
	rb.UpdateState(resolver.State{Addresses: addrs})
	cc.Connect()

	testutils.AwaitState(ctx, t, cc, connectivity.Connecting)

	// Verify that only the first server is contacted.
	if holds[0].Wait(ctx) != true {
		t.Fatalf("Timeout waiting for server %d with address %q to be contacted", 0, addrs[0])
	}
	if holds[1].IsStarted() != false {
		t.Fatalf("Server %d with address %q contacted unexpectedly", 1, addrs[1])
	}
	if holds[2].IsStarted() != false {
		t.Fatalf("Server %d with address %q contacted unexpectedly", 2, addrs[2])
	}

	// Make the happy eyeballs timer fire once and verify that the
	// second server is contacted, but the third isn't.
	triggerTimer()
	if holds[1].Wait(ctx) != true {
		t.Fatalf("Timeout waiting for server %d with address %q to be contacted", 1, addrs[1])
	}
	if holds[2].IsStarted() != false {
		t.Fatalf("Server %d with address %q contacted unexpectedly", 2, addrs[2])
	}

	// Make the happy eyeballs timer fire once more and verify that the
	// third server is contacted.
	triggerTimer()
	if holds[2].Wait(ctx) != true {
		t.Fatalf("Timeout waiting for server %d with address %q to be contacted", 2, addrs[2])
	}

	// First SubConn Fails.
	holds[0].Fail(fmt.Errorf("test error"))
	tmr.WaitForInt64CountIncr(ctx, 1)

	// No TF should be reported until the first pass is complete.
	shortCtx, shortCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer shortCancel()
	testutils.AwaitNotState(shortCtx, t, cc, connectivity.TransientFailure)

	// Third SubConn fails.
	shortCtx, shortCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer shortCancel()
	holds[2].Fail(fmt.Errorf("test error"))
	tmr.WaitForInt64CountIncr(ctx, 1)
	testutils.AwaitNotState(shortCtx, t, cc, connectivity.TransientFailure)

	// Last SubConn fails, this should result in a TF update.
	holds[1].Fail(fmt.Errorf("test error"))
	tmr.WaitForInt64CountIncr(ctx, 1)
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)
	waitForMetric(ctx, t, tmr, "grpc.subchannel.connection_attempts_failed")

	// Only connection attempt fails in this test.
	if got, _ := tmr.Metric("grpc.lb.pick_first.connection_attempts_succeeded"); got != 0 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.connection_attempts_succeeded", got, 0)
	}
	if got, _ := tmr.Metric("grpc.lb.pick_first.connection_attempts_failed"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.connection_attempts_failed", got, 1)
	}
	if got, _ := tmr.Metric("grpc.lb.pick_first.disconnections"); got != 0 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.disconnections", got, 0)
	}
	if got, _ := tmr.Metric("grpc.subchannel.connection_attempts_failed"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.subchannel.connection_attempts_failed", got, 1)
	}
	if got, _ := tmr.Metric("grpc.lb.subchannel.connection_attempts_succeeded"); got != 0 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.subchannel.connection_attempts_succeeded", got, 0)
	}
}

// Test verifies that pickfirst attempts to connect to the second backend once
// the happy eyeballs timer expires.
func (s) TestPickFirstLeaf_HappyEyeballs_TriggerConnectionDelay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	originalTimer := pfinternal.TimeAfterFunc
	defer func() {
		pfinternal.TimeAfterFunc = originalTimer
	}()
	triggerTimer, timeAfter := mockTimer()
	pfinternal.TimeAfterFunc = timeAfter

	tmr := stats.NewTestMetricsRecorder()
	dialer := testutils.NewBlockingDialer()
	opts := []grpc.DialOption{
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, pfbalancer.Name)),
		grpc.WithContextDialer(dialer.DialContext),
		grpc.WithStatsHandler(tmr),
	}
	cc, rb, bm := setupPickFirstLeaf(t, 2, opts...)
	addrs := bm.resolverAddrs()
	holds := bm.holds(dialer)
	rb.UpdateState(resolver.State{Addresses: addrs})
	cc.Connect()

	testutils.AwaitState(ctx, t, cc, connectivity.Connecting)

	// Verify that only the first server is contacted.
	if holds[0].Wait(ctx) != true {
		t.Fatalf("Timeout waiting for server %d with address %q to be contacted", 0, addrs[0])
	}
	if holds[1].IsStarted() != false {
		t.Fatalf("Server %d with address %q contacted unexpectedly", 1, addrs[1])
	}

	// Make the happy eyeballs timer fire once and verify that the
	// second server is contacted.
	triggerTimer()
	if holds[1].Wait(ctx) != true {
		t.Fatalf("Timeout waiting for server %d with address %q to be contacted", 1, addrs[1])
	}

	// Get the connection attempt to the second server to succeed and verify
	// that the channel becomes READY.
	holds[1].Resume()
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)
	waitForMetric(ctx, t, tmr, "grpc.subchannel.connection_attempts_succeeded")
	// Only connection attempt successes in this test.
	if got, _ := tmr.Metric("grpc.lb.pick_first.connection_attempts_succeeded"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.connection_attempts_succeeded", got, 1)
	}
	if got, _ := tmr.Metric("grpc.lb.pick_first.connection_attempts_failed"); got != 0 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.connection_attempts_failed", got, 0)
	}
	if got, _ := tmr.Metric("grpc.lb.pick_first.disconnections"); got != 0 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.disconnections", got, 0)
	}

	if got, _ := tmr.Metric("grpc.subchannel.connection_attempts_succeeded"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.subchannel.connection_attempts_succeeded", got, 1)
	}
	if got, _ := tmr.Metric("grpc.subchannel.connection_attempts_failed"); got != 0 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.subchannel.connection_attempts_failed", got, 0)
	}
}

func waitForMetric(ctx context.Context, t *testing.T, tmr *stats.TestMetricsRecorder, metricName string) {
	for {
		if _, ok := tmr.Metric(metricName); ok {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for metric emission: %s", metricName)
		case <-time.After(10 * time.Millisecond):
			continue
		}
	}
}

// Test tests the pickfirst balancer by causing a SubConn to fail and then
// jumping to the 3rd SubConn after the happy eyeballs timer expires.
func (s) TestPickFirstLeaf_HappyEyeballs_TF_ThenTimerFires(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	originalTimer := pfinternal.TimeAfterFunc
	defer func() {
		pfinternal.TimeAfterFunc = originalTimer
	}()
	triggerTimer, timeAfter := mockTimer()
	pfinternal.TimeAfterFunc = timeAfter

	tmr := stats.NewTestMetricsRecorder()
	dialer := testutils.NewBlockingDialer()
	opts := []grpc.DialOption{
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, pfbalancer.Name)),
		grpc.WithContextDialer(dialer.DialContext),
		grpc.WithStatsHandler(tmr),
	}
	cc, rb, bm := setupPickFirstLeaf(t, 3, opts...)
	addrs := bm.resolverAddrs()
	holds := bm.holds(dialer)
	rb.UpdateState(resolver.State{Addresses: addrs})
	cc.Connect()

	testutils.AwaitState(ctx, t, cc, connectivity.Connecting)

	// Verify that only the first server is contacted.
	if holds[0].Wait(ctx) != true {
		t.Fatalf("Timeout waiting for server %d with address %q to be contacted", 0, addrs[0])
	}
	if holds[1].IsStarted() != false {
		t.Fatalf("Server %d with address %q contacted unexpectedly", 1, addrs[1])
	}
	if holds[2].IsStarted() != false {
		t.Fatalf("Server %d with address %q contacted unexpectedly", 2, addrs[2])
	}

	// First SubConn Fails.
	holds[0].Fail(fmt.Errorf("test error"))

	// Verify that only the second server is contacted.
	if holds[1].Wait(ctx) != true {
		t.Fatalf("Timeout waiting for server %d with address %q to be contacted", 1, addrs[1])
	}
	if got, _ := tmr.Metric("grpc.lb.pick_first.connection_attempts_failed"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.connection_attempts_failed", got, 1)
	}
	if got, _ := tmr.Metric("grpc.subchannel.connection_attempts_failed"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.subchannel.connection_attempts_failed", got, 1)
	}
	if holds[2].IsStarted() != false {
		t.Fatalf("Server %d with address %q contacted unexpectedly", 2, addrs[2])
	}

	// The happy eyeballs timer expires, pickfirst should stop waiting for
	// server[1] to report a failure/success and request the creation of a third
	// SubConn.
	triggerTimer()
	if holds[2].Wait(ctx) != true {
		t.Fatalf("Timeout waiting for server %d with address %q to be contacted", 2, addrs[2])
	}

	// Get the connection attempt to the second server to succeed and verify
	// that the channel becomes READY.
	holds[1].Resume()
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)
	waitForMetric(ctx, t, tmr, "grpc.subchannel.connection_attempts_succeeded")
	if got, _ := tmr.Metric("grpc.subchannel.connection_attempts_succeeded"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.subchannel.connection_attempts_succeeded", got, 1)
	}
	if got, _ := tmr.Metric("grpc.lb.pick_first.connection_attempts_succeeded"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.connection_attempts_succeeded", got, 1)
	}
	if got, _ := tmr.Metric("grpc.lb.pick_first.disconnections"); got != 0 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.disconnections", got, 0)
	}
}

// Test verifies that when a subchannel is shut down by the LB (because another
// subchannel won) while its dial is still in-flight, it records exactly one
// successful attempt.
func (s) TestPickFirstLeaf_HappyEyeballs_Ignore_Inflight_Cancellations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	originalTimer := pfinternal.TimeAfterFunc
	defer func() {
		pfinternal.TimeAfterFunc = originalTimer
	}()
	triggerTimer, timeAfter := mockTimer()
	pfinternal.TimeAfterFunc = timeAfter

	tmr := stats.NewTestMetricsRecorder()
	dialer := testutils.NewBlockingDialer()
	opts := []grpc.DialOption{
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, pfbalancer.Name)),
		grpc.WithContextDialer(dialer.DialContext),
		grpc.WithStatsHandler(tmr),
	}

	// Setup 2 backend addresses
	cc, rb, bm := setupPickFirstLeaf(t, 2, opts...)
	addrs := bm.resolverAddrs()
	holds := bm.holds(dialer)
	rb.UpdateState(resolver.State{Addresses: addrs})
	cc.Connect()

	// Make sure we connect to second subconn
	testutils.AwaitState(ctx, t, cc, connectivity.Connecting)
	if holds[0].Wait(ctx) != true {
		t.Fatalf("Timeout waiting for server %d to be contacted", 0)
	}
	triggerTimer()
	if holds[1].Wait(ctx) != true {
		t.Fatalf("Timeout waiting for server %d to be contacted", 1)
	}
	holds[1].Resume()

	// Wait for Channel to become READY.
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	// Unblock the First SubConn.
	// Since the LB has already closed this subchannel, the context passed to Dial
	// is canceled. This will lead to an inflight attempt to be cancelled.
	// No success or failure metric should be recorded for this.
	holds[0].Resume()

	// --- Assertions ---

	// Wait for the SUCCESS metric to ensure recording logic has processed.
	waitForMetric(ctx, t, tmr, "grpc.subchannel.connection_attempts_succeeded")

	// Verify Success: Exactly 1 (The Winner).
	if got, _ := tmr.Metric("grpc.subchannel.connection_attempts_succeeded"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: 1", "grpc.subchannel.connection_attempts_succeeded", got)
	}

	// Verify Failure: Exactly 0 (The Loser was ignored).
	// We poll briefly to ensure no delayed failure metric appears.
	sCtx, sCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer sCancel()
	for ; sCtx.Err() == nil; <-time.After(time.Millisecond) {
		if got, _ := tmr.Metric("grpc.subchannel.connection_attempts_failed"); got != 0 {
			t.Fatalf("Unexpected failure recorded for shutdown subchannel, got: %v, want: 0", got)
		}
	}

	// LB Metrics Check
	if got, _ := tmr.Metric("grpc.lb.pick_first.connection_attempts_succeeded"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: 1", "grpc.lb.pick_first.connection_attempts_succeeded", got)
	}
	if got, _ := tmr.Metric("grpc.lb.pick_first.connection_attempts_failed"); got != 0 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: 0", "grpc.lb.pick_first.connection_attempts_failed", got)
	}
}

func (s) TestPickFirstLeaf_InterleavingIPV4Preferred(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc := testutils.NewBalancerClientConn(t)
	bal := balancer.Get(pfbalancer.Name).Build(cc, balancer.BuildOptions{})
	defer bal.Close()
	ccState := balancer.ClientConnState{
		ResolverState: resolver.State{
			Endpoints: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "1.1.1.1:1111"}}},
				{Addresses: []resolver.Address{{Addr: "2.2.2.2:2"}}},
				{Addresses: []resolver.Address{{Addr: "3.3.3.3:3"}}},
				// IPv4-mapped IPv6 address, considered as an IPv4 for
				// interleaving.
				{Addresses: []resolver.Address{{Addr: "[::FFFF:192.168.0.1]:2222"}}},
				{Addresses: []resolver.Address{{Addr: "[0001:0001:0001:0001:0001:0001:0001:0001]:8080"}}},
				{Addresses: []resolver.Address{{Addr: "[0002:0002:0002:0002:0002:0002:0002:0002]:8080"}}},
				{Addresses: []resolver.Address{{Addr: "[fe80::1%eth0]:3333"}}},
				{Addresses: []resolver.Address{{Addr: "grpc.io:80"}}}, // not an IP.
			},
		},
	}
	if err := bal.UpdateClientConnState(ccState); err != nil {
		t.Fatalf("UpdateClientConnState(%v) returned error: %v", ccState, err)
	}

	wantAddrs := []resolver.Address{
		{Addr: "1.1.1.1:1111"},
		{Addr: "[0001:0001:0001:0001:0001:0001:0001:0001]:8080"},
		{Addr: "grpc.io:80"},
		{Addr: "2.2.2.2:2"},
		{Addr: "[0002:0002:0002:0002:0002:0002:0002:0002]:8080"},
		{Addr: "3.3.3.3:3"},
		{Addr: "[fe80::1%eth0]:3333"},
		{Addr: "[::FFFF:192.168.0.1]:2222"},
	}

	gotAddrs, err := subConnAddresses(ctx, cc, 8)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if diff := cmp.Diff(wantAddrs, gotAddrs, ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn creation order mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_InterleavingIPv6Preferred(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc := testutils.NewBalancerClientConn(t)
	bal := balancer.Get(pfbalancer.Name).Build(cc, balancer.BuildOptions{})
	defer bal.Close()
	ccState := balancer.ClientConnState{
		ResolverState: resolver.State{
			Endpoints: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "[0001:0001:0001:0001:0001:0001:0001:0001]:8080"}}},
				{Addresses: []resolver.Address{{Addr: "[0001:0001:0001:0001:0001:0001:0001:0001]:8080"}}}, // duplicate, should be ignored.
				{Addresses: []resolver.Address{{Addr: "1.1.1.1:1111"}}},
				{Addresses: []resolver.Address{{Addr: "2.2.2.2:2"}}},
				{Addresses: []resolver.Address{{Addr: "3.3.3.3:3"}}},
				{Addresses: []resolver.Address{{Addr: "[::FFFF:192.168.0.1]:2222"}}},
				{Addresses: []resolver.Address{{Addr: "[0002:0002:0002:0002:0002:0002:0002:0002]:2222"}}},
				{Addresses: []resolver.Address{{Addr: "[fe80::1%eth0]:3333"}}},
				{Addresses: []resolver.Address{{Addr: "grpc.io:80"}}}, // not an IP.
			},
		},
	}
	if err := bal.UpdateClientConnState(ccState); err != nil {
		t.Fatalf("UpdateClientConnState(%v) returned error: %v", ccState, err)
	}

	wantAddrs := []resolver.Address{
		{Addr: "[0001:0001:0001:0001:0001:0001:0001:0001]:8080"},
		{Addr: "1.1.1.1:1111"},
		{Addr: "grpc.io:80"},
		{Addr: "[0002:0002:0002:0002:0002:0002:0002:0002]:2222"},
		{Addr: "2.2.2.2:2"},
		{Addr: "[fe80::1%eth0]:3333"},
		{Addr: "3.3.3.3:3"},
		{Addr: "[::FFFF:192.168.0.1]:2222"},
	}

	gotAddrs, err := subConnAddresses(ctx, cc, 8)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if diff := cmp.Diff(wantAddrs, gotAddrs, ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn creation order mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_InterleavingUnknownPreferred(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc := testutils.NewBalancerClientConn(t)
	bal := balancer.Get(pfbalancer.Name).Build(cc, balancer.BuildOptions{})
	defer bal.Close()
	ccState := balancer.ClientConnState{
		ResolverState: resolver.State{
			Endpoints: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "grpc.io:80"}}}, // not an IP.
				{Addresses: []resolver.Address{{Addr: "1.1.1.1:1111"}}},
				{Addresses: []resolver.Address{{Addr: "2.2.2.2:2"}}},
				{Addresses: []resolver.Address{{Addr: "3.3.3.3:3"}}},
				{Addresses: []resolver.Address{{Addr: "[::FFFF:192.168.0.1]:2222"}}},
				{Addresses: []resolver.Address{{Addr: "[0001:0001:0001:0001:0001:0001:0001:0001]:8080"}}},
				{Addresses: []resolver.Address{{Addr: "[0002:0002:0002:0002:0002:0002:0002:0002]:8080"}}},
				{Addresses: []resolver.Address{{Addr: "[fe80::1%eth0]:3333"}}},
				{Addresses: []resolver.Address{{Addr: "example.com:80"}}}, // not an IP.
			},
		},
	}
	if err := bal.UpdateClientConnState(ccState); err != nil {
		t.Fatalf("UpdateClientConnState(%v) returned error: %v", ccState, err)
	}

	wantAddrs := []resolver.Address{
		{Addr: "grpc.io:80"},
		{Addr: "1.1.1.1:1111"},
		{Addr: "[0001:0001:0001:0001:0001:0001:0001:0001]:8080"},
		{Addr: "example.com:80"},
		{Addr: "2.2.2.2:2"},
		{Addr: "[0002:0002:0002:0002:0002:0002:0002:0002]:8080"},
		{Addr: "3.3.3.3:3"},
		{Addr: "[fe80::1%eth0]:3333"},
		{Addr: "[::FFFF:192.168.0.1]:2222"},
	}

	gotAddrs, err := subConnAddresses(ctx, cc, 9)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if diff := cmp.Diff(wantAddrs, gotAddrs, ignoreBalAttributesOpt); diff != "" {
		t.Errorf("SubConn creation order mismatch (-want +got):\n%s", diff)
	}
}

// Test verifies that pickfirst balancer transitions to READY when the health
// listener is enabled. Since client side health checking is not enabled in
// the service config, the health listener will send a health update for READY
// after registering the listener.
func (s) TestPickFirstLeaf_HealthListenerEnabled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	bf := stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			bd.ChildBalancer = balancer.Get(pfbalancer.Name).Build(bd.ClientConn, bd.BuildOptions)
		},
		Close: func(bd *stub.BalancerData) {
			bd.ChildBalancer.Close()
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			ccs.ResolverState = pfbalancer.EnableHealthListener(ccs.ResolverState)
			return bd.ChildBalancer.UpdateClientConnState(ccs)
		},
	}

	stub.Register(t.Name(), bf)
	svcCfg := fmt.Sprintf(`{ "loadBalancingConfig": [{%q: {}}] }`, t.Name())
	backend := stubserver.StartTestService(t, nil)
	defer backend.Stop()
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(svcCfg),
	}
	cc, err := grpc.NewClient(backend.Address, opts...)
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", backend.Address, err)

	}
	defer cc.Close()

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, resolver.Address{Addr: backend.Address}); err != nil {
		t.Fatal(err)
	}
}

// Test verifies that a health listener is not registered when pickfirst is not
// under a petiole policy.
func (s) TestPickFirstLeaf_HealthListenerNotEnabled(t *testing.T) {
	// Wrap the clientconn to intercept NewSubConn.
	// Capture the health list by wrapping the SC.
	// Wrap the picker to unwrap the SC.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	healthListenerCh := make(chan func(balancer.SubConnState))

	bf := stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			ccw := &healthListenerCapturingCCWrapper{
				ClientConn:       bd.ClientConn,
				healthListenerCh: healthListenerCh,
				subConnStateCh:   make(chan balancer.SubConnState, 5),
			}
			bd.ChildBalancer = balancer.Get(pfbalancer.Name).Build(ccw, bd.BuildOptions)
		},
		Close: func(bd *stub.BalancerData) {
			bd.ChildBalancer.Close()
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			// Functions like a non-petiole policy by not configuring the use
			// of health listeners.
			return bd.ChildBalancer.UpdateClientConnState(ccs)
		},
	}

	stub.Register(t.Name(), bf)
	svcCfg := fmt.Sprintf(`{ "loadBalancingConfig": [{%q: {}}] }`, t.Name())
	backend := stubserver.StartTestService(t, nil)
	defer backend.Stop()
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(svcCfg),
	}
	cc, err := grpc.NewClient(backend.Address, opts...)
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", backend.Address, err)

	}
	defer cc.Close()
	cc.Connect()

	select {
	case <-healthListenerCh:
		t.Fatal("Health listener registered when not enabled.")
	case <-time.After(defaultTestShortTimeout):
	}

	testutils.AwaitState(ctx, t, cc, connectivity.Ready)
}

// Test mocks the updates sent to the health listener and verifies that the
// balancer correctly reports the health state once the SubConn's connectivity
// state becomes READY.
func (s) TestPickFirstLeaf_HealthUpdates(t *testing.T) {
	// Wrap the clientconn to intercept NewSubConn.
	// Capture the health list by wrapping the SC.
	// Wrap the picker to unwrap the SC.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	healthListenerCh := make(chan func(balancer.SubConnState))
	scConnectivityStateCh := make(chan balancer.SubConnState, 5)

	bf := stub.BalancerFuncs{
		Init: func(bd *stub.BalancerData) {
			ccw := &healthListenerCapturingCCWrapper{
				ClientConn:       bd.ClientConn,
				healthListenerCh: healthListenerCh,
				subConnStateCh:   scConnectivityStateCh,
			}
			bd.ChildBalancer = balancer.Get(pfbalancer.Name).Build(ccw, bd.BuildOptions)
		},
		Close: func(bd *stub.BalancerData) {
			bd.ChildBalancer.Close()
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			ccs.ResolverState = pfbalancer.EnableHealthListener(ccs.ResolverState)
			return bd.ChildBalancer.UpdateClientConnState(ccs)
		},
	}

	stub.Register(t.Name(), bf)
	svcCfg := fmt.Sprintf(`{ "loadBalancingConfig": [{%q: {}}] }`, t.Name())
	backend := stubserver.StartTestService(t, nil)
	defer backend.Stop()
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(svcCfg),
	}
	cc, err := grpc.NewClient(backend.Address, opts...)
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", backend.Address, err)

	}
	defer cc.Close()
	cc.Connect()

	var healthListener func(balancer.SubConnState)
	select {
	case healthListener = <-healthListenerCh:
	case <-ctx.Done():
		t.Fatal("Context timed out waiting for health listener to be registered.")
	}

	// Wait for the raw connectivity state to become READY. The LB policy should
	// wait for the health updates before transitioning the channel to READY.
	for {
		var scs balancer.SubConnState
		select {
		case scs = <-scConnectivityStateCh:
		case <-ctx.Done():
			t.Fatal("Context timed out waiting for the SubConn connectivity state to become READY.")
		}
		if scs.ConnectivityState == connectivity.Ready {
			break
		}
	}

	shortCtx, cancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer cancel()
	testutils.AwaitNoStateChange(shortCtx, t, cc, connectivity.Connecting)

	// The LB policy should update the channel state based on the health state.
	healthListener(balancer.SubConnState{
		ConnectivityState: connectivity.TransientFailure,
		ConnectionError:   fmt.Errorf("test health check failure"),
	})
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	healthListener(balancer.SubConnState{
		ConnectivityState: connectivity.Connecting,
		ConnectionError:   balancer.ErrNoSubConnAvailable,
	})
	testutils.AwaitState(ctx, t, cc, connectivity.Connecting)

	healthListener(balancer.SubConnState{
		ConnectivityState: connectivity.Ready,
	})
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, resolver.Address{Addr: backend.Address}); err != nil {
		t.Fatal(err)
	}

	// When the health check fails, the channel should transition to TF.
	healthListener(balancer.SubConnState{
		ConnectivityState: connectivity.TransientFailure,
		ConnectionError:   fmt.Errorf("test health check failure"),
	})
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)
}

// Tests the case where an address update received by the pick_first LB policy
// differs in metadata which should be ignored by the LB policy. In this case,
// the test verifies that new connections are not created when the address
// update only changes the metadata.
func (s) TestPickFirstLeaf_AddressUpdateWithMetadata(t *testing.T) {
	dialer := testutils.NewBlockingDialer()
	dopts := []grpc.DialOption{
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, pfbalancer.Name)),
		grpc.WithContextDialer(dialer.DialContext),
	}
	cc, r, backends := setupPickFirstLeaf(t, 2, dopts...)

	// Add a metadata to the addresses before pushing them to the pick_first LB
	// policy through the manual resolver.
	addrs := backends.resolverAddrs()
	for i := range addrs {
		addrs[i].Metadata = &metadata.MD{
			"test-metadata-1": []string{fmt.Sprintf("%d", i)},
		}
	}
	r.UpdateState(resolver.State{Addresses: addrs})

	// Ensure that RPCs succeed to the expected backend.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// Create holds for each backend. This will be used to verify the connection
	// is not re-established.
	holds := backends.holds(dialer)

	// Add metadata to the addresses before pushing them to the pick_first LB
	// policy through the manual resolver. Leave the order of the addresses
	// unchanged.
	for i := range addrs {
		addrs[i].Metadata = &metadata.MD{
			"test-metadata-2": []string{fmt.Sprintf("%d", i)},
		}
	}
	r.UpdateState(resolver.State{Addresses: addrs})

	// Ensure that no new connection is established.
	for i := range holds {
		sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
		defer sCancel()
		if holds[i].Wait(sCtx) {
			t.Fatalf("Unexpected connection attempt to backend: %s", addrs[i])
		}
	}

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	// Add metadata to the addresses before pushing them to the pick_first LB
	// policy through the manual resolver. Reverse of the order of addresses.
	for i := range addrs {
		addrs[i].Metadata = &metadata.MD{
			"test-metadata-3": []string{fmt.Sprintf("%d", i)},
		}
	}
	addrs[0], addrs[1] = addrs[1], addrs[0]
	r.UpdateState(resolver.State{Addresses: addrs})

	// Ensure that no new connection is established.
	for i := range holds {
		sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
		defer sCancel()
		if holds[i].Wait(sCtx) {
			t.Fatalf("Unexpected connection attempt to backend: %s", addrs[i])
		}
	}
	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}
}

// Tests the scenario where a connection is established and then breaks, leading
// to a reconnection attempt. While the reconnection is in progress, a resolver
// update with a new address is received. The test verifies that the balancer
// creates a new SubConn for the new address and that the ClientConn eventually
// becomes READY.
func (s) TestPickFirstLeaf_Reconnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc := testutils.NewBalancerClientConn(t)
	bal := balancer.Get(pfbalancer.Name).Build(cc, balancer.BuildOptions{})
	defer bal.Close()
	ccState := balancer.ClientConnState{
		ResolverState: resolver.State{
			Endpoints: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "1.1.1.1:1"}}},
			},
		},
	}
	if err := bal.UpdateClientConnState(ccState); err != nil {
		t.Fatalf("UpdateClientConnState(%v) returned error: %v", ccState, err)
	}

	select {
	case state := <-cc.NewStateCh:
		if got, want := state, connectivity.Connecting; got != want {
			t.Fatalf("Received unexpected ClientConn sate: got %v, want %v", got, want)
		}
	case <-ctx.Done():
		t.Fatal("Context timed out waiting for ClientConn state update.")
	}

	sc1 := <-cc.NewSubConnCh
	select {
	case <-sc1.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Context timed out waiting for Connect() to be called on sc1.")
	}
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})

	if err := cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
		t.Fatalf("Context timed out waiting for ClientConn to become READY.")
	}

	// Simulate a connection breakage, this should result the channel
	// transitioning to IDLE.
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Idle})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Idle); err != nil {
		t.Fatalf("Context timed out waiting for ClientConn to enter IDLE.")
	}

	// Calling the idle picker should result in the SubConn being re-connected.
	picker := <-cc.NewPickerCh
	if _, err := picker.Pick(balancer.PickInfo{}); err != balancer.ErrNoSubConnAvailable {
		t.Fatalf("picker.Pick() returned error: %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}

	select {
	case <-sc1.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Context timed out waiting for Connect() to be called on sc1.")
	}

	// Send a resolver update, removing the existing SubConn. Since the balancer
	// is connecting, it should create a new SubConn for the new backend
	// address.
	ccState = balancer.ClientConnState{
		ResolverState: resolver.State{
			Endpoints: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "2.2.2.2:2"}}},
			},
		},
	}
	if err := bal.UpdateClientConnState(ccState); err != nil {
		t.Fatalf("UpdateClientConnState(%v) returned error: %v", ccState, err)
	}

	var sc2 *testutils.TestSubConn
	select {
	case sc2 = <-cc.NewSubConnCh:
	case <-ctx.Done():
		t.Fatal("Context timed out waiting for new SubConn to be created.")
	}

	select {
	case <-sc2.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Context timed out waiting for Connect() to be called on sc2.")
	}
	sc2.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc2.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
		t.Fatalf("Context timed out waiting for ClientConn to become READY.")
	}
}

// healthListenerCapturingCCWrapper is used to capture the health listener so
// that health updates can be mocked for testing.
type healthListenerCapturingCCWrapper struct {
	balancer.ClientConn
	healthListenerCh chan func(balancer.SubConnState)
	subConnStateCh   chan balancer.SubConnState
}

func (ccw *healthListenerCapturingCCWrapper) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	oldListener := opts.StateListener
	opts.StateListener = func(scs balancer.SubConnState) {
		ccw.subConnStateCh <- scs
		if oldListener != nil {
			oldListener(scs)
		}
	}
	sc, err := ccw.ClientConn.NewSubConn(addrs, opts)
	if err != nil {
		return nil, err
	}
	return &healthListenerCapturingSCWrapper{
		SubConn:    sc,
		listenerCh: ccw.healthListenerCh,
	}, nil
}

func (ccw *healthListenerCapturingCCWrapper) UpdateState(state balancer.State) {
	state.Picker = &unwrappingPicker{state.Picker}
	ccw.ClientConn.UpdateState(state)
}

type healthListenerCapturingSCWrapper struct {
	balancer.SubConn
	listenerCh chan func(balancer.SubConnState)
}

func (scw *healthListenerCapturingSCWrapper) RegisterHealthListener(listener func(balancer.SubConnState)) {
	scw.listenerCh <- listener
}

// unwrappingPicker unwraps SubConns because the channel expects SubConns to be
// addrConns.
type unwrappingPicker struct {
	balancer.Picker
}

func (pw *unwrappingPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	pr, err := pw.Picker.Pick(info)
	if pr.SubConn != nil {
		pr.SubConn = pr.SubConn.(*healthListenerCapturingSCWrapper).SubConn
	}
	return pr, err
}

// subConnAddresses makes the pickfirst balancer create the requested number of
// SubConns by triggering transient failures. The function returns the
// addresses of the created SubConns.
func subConnAddresses(ctx context.Context, cc *testutils.BalancerClientConn, subConnCount int) ([]resolver.Address, error) {
	addresses := []resolver.Address{}
	for i := 0; i < subConnCount; i++ {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("test timed out after creating %d subchannels, want %d", i, subConnCount)
		case sc := <-cc.NewSubConnCh:
			if len(sc.Addresses) != 1 {
				return nil, fmt.Errorf("new subchannel created with %d addresses, want 1", len(sc.Addresses))
			}
			addresses = append(addresses, sc.Addresses[0])
			sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
			sc.UpdateState(balancer.SubConnState{
				ConnectivityState: connectivity.TransientFailure,
			})
		}
	}
	return addresses, nil
}

// stateStoringBalancer stores the state of the SubConns being created.
type stateStoringBalancer struct {
	balancer.Balancer
	mu       sync.Mutex
	scStates []*scState
}

func (b *stateStoringBalancer) Close() {
	b.Balancer.Close()
}

type stateStoringBalancerBuilder struct {
	balancer chan *stateStoringBalancer
}

func (b *stateStoringBalancerBuilder) Name() string {
	return stateStoringBalancerName
}

func (b *stateStoringBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	bal := &stateStoringBalancer{}
	bal.Balancer = balancer.Get(pfbalancer.Name).Build(&stateStoringCCWrapper{cc, bal}, opts)
	b.balancer <- bal
	return bal
}

func (b *stateStoringBalancer) subConnStates() []scState {
	b.mu.Lock()
	defer b.mu.Unlock()
	ret := []scState{}
	for _, s := range b.scStates {
		ret = append(ret, *s)
	}
	return ret
}

func (b *stateStoringBalancer) addSCState(state *scState) {
	b.mu.Lock()
	b.scStates = append(b.scStates, state)
	b.mu.Unlock()
}

type stateStoringCCWrapper struct {
	balancer.ClientConn
	b *stateStoringBalancer
}

func (ccw *stateStoringCCWrapper) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	oldListener := opts.StateListener
	scs := &scState{
		State: connectivity.Idle,
		Addrs: addrs,
	}
	ccw.b.addSCState(scs)
	opts.StateListener = func(s balancer.SubConnState) {
		ccw.b.mu.Lock()
		scs.State = s.ConnectivityState
		ccw.b.mu.Unlock()
		oldListener(s)
	}
	return ccw.ClientConn.NewSubConn(addrs, opts)
}

type scState struct {
	State connectivity.State
	Addrs []resolver.Address
}

type backendManager struct {
	backends []*testServer
}

func (b *backendManager) stopAllExcept(index int) {
	for idx, b := range b.backends {
		if idx != index {
			b.stop()
		}
	}
}

// resolverAddrs  returns a list of resolver addresses for the stub server
// backends. Useful when pushing addresses to the manual resolver.
func (b *backendManager) resolverAddrs() []resolver.Address {
	addrs := make([]resolver.Address, len(b.backends))
	for i, backend := range b.backends {
		addrs[i] = resolver.Address{Addr: backend.Address}
	}
	return addrs
}

func (b *backendManager) holds(dialer *testutils.BlockingDialer) []*testutils.Hold {
	holds := []*testutils.Hold{}
	for _, addr := range b.resolverAddrs() {
		holds = append(holds, dialer.Hold(addr.Addr))
	}
	return holds
}

type ccStateSubscriber struct {
	mu     sync.Mutex
	states []connectivity.State
}

// transitions returns all the states that ccStateSubscriber recorded.
// Without this a race condition occurs when the test compares the states
// and the subscriber at the same time receives a connectivity.Shutdown.
func (c *ccStateSubscriber) transitions() []connectivity.State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.states
}

func (c *ccStateSubscriber) OnMessage(msg any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.states = append(c.states, msg.(connectivity.State))
}

// mockTimer returns a fake timeAfterFunc that will not trigger automatically.
// It returns a function that can be called to manually trigger the execution
// of the scheduled callback.
func mockTimer() (triggerFunc func(), timerFunc func(_ time.Duration, f func()) func()) {
	timerCh := make(chan struct{})
	triggerFunc = func() {
		timerCh <- struct{}{}
	}
	return triggerFunc, func(_ time.Duration, f func()) func() {
		stopCh := make(chan struct{})
		go func() {
			select {
			case <-timerCh:
				f()
			case <-stopCh:
			}
		}()
		return sync.OnceFunc(func() {
			close(stopCh)
		})
	}
}
