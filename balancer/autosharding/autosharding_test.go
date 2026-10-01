/*
 *
 * Copyright 2026 gRPC authors.
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

package autosharding_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/autosharding"
	"google.golang.org/grpc/balancer/autosharding/internal"
	"google.golang.org/grpc/balancer/autosharding/internal/sharding"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/experimental/balancer/hostname"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
)

const (
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

// newTestEndpoint returns a resolver.Endpoint with the given address and
// hostname attribute (if non-empty).
func newTestEndpoint(addr, host string) resolver.Endpoint {
	ep := resolver.Endpoint{Addresses: []resolver.Address{{Addr: addr}}}
	if host != "" {
		ep = hostname.Set(ep, host)
	}
	return ep
}

type testClientConn struct {
	grpc.ClientConnInterface
	key string
}

func defaultClientConnProvider(key string) (grpc.ClientConnInterface, func(), error) {
	return &testClientConn{key: key}, func() {}, nil
}

// resolverStateWithChannelFactoryAndEndpoints returns a resolver.State with the
// given endpoints and the defaultClientConnProvider set as the
// ClientConnProvider.
func resolverStateWithChannelFactoryAndEndpoints(endpoints []resolver.Endpoint) resolver.State {
	return grpc.SetClientConnProvider(resolver.State{Endpoints: endpoints}, defaultClientConnProvider)
}

// Tests scenarios where an update from the name resolver is invalid and
// verifies that the balancer transitions to TransientFailure with an
// appropriate error picker. A subsequent valid update should transition the
// channel back to Idle with a queueing picker.
func (s) TestUpdateClientConnState_ResolverError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	defaultTestCfg := &autosharding.LBConfig{
		ChannelFactoryKey:  "test-factory-key",
		AutoShardingTarget: "test-target-%s",
		KeyHeaderName:      "test-header-name",
	}
	providerErr := errors.New("channel factory error")

	tests := []struct {
		name          string
		resolverState resolver.State
		wantPickerErr error
	}{
		{
			name:          "empty-endpoints",
			resolverState: resolverStateWithChannelFactoryAndEndpoints(nil),
			wantPickerErr: errors.New("autosharding: no endpoints from resolver"),
		},
		{
			name:          "endpoints-with-no-addresses",
			resolverState: resolverStateWithChannelFactoryAndEndpoints([]resolver.Endpoint{{Addresses: nil}}),
			wantPickerErr: errors.New("autosharding: no endpoints from resolver"),
		},
		{
			name:          "missing-channel-factory",
			resolverState: resolver.State{Endpoints: []resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")}},
			wantPickerErr: errors.New("autosharding: no channel factory found in resolver state"),
		},
		{
			name: "channel-factory-returns-error",
			resolverState: grpc.SetClientConnProvider(
				resolver.State{Endpoints: []resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")}},
				func(string) (grpc.ClientConnInterface, func(), error) {
					return nil, nil, providerErr
				},
			),
			wantPickerErr: fmt.Errorf("autosharding: failed to create gRPC channel for key %q: %v", defaultTestCfg.ChannelFactoryKey, providerErr),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cc := testutils.NewBalancerClientConn(t)
			b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
			defer b.Close()

			// Invalid resolver state should return ErrBadResolverState and
			// transition the channel to TransientFailure with an error picker.
			err := b.UpdateClientConnState(balancer.ClientConnState{
				ResolverState:  tc.resolverState,
				BalancerConfig: defaultTestCfg,
			})
			if !errors.Is(err, balancer.ErrBadResolverState) {
				t.Fatalf("UpdateClientConnState() error = %v, want %v", err, balancer.ErrBadResolverState)
			}
			if err := cc.WaitForConnectivityState(ctx, connectivity.TransientFailure); err != nil {
				t.Fatalf("WaitForConnectivityState(TransientFailure) failed: %v", err)
			}
			if err := cc.WaitForPickerWithErr(ctx, tc.wantPickerErr); err != nil {
				t.Fatalf("WaitForPickerWithErr(%v) failed: %v", tc.wantPickerErr, err)
			}

			// Valid resolver state should transition the channel back to Idle
			// with a queueing picker.
			err = b.UpdateClientConnState(balancer.ClientConnState{
				ResolverState:  resolverStateWithChannelFactoryAndEndpoints([]resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")}),
				BalancerConfig: defaultTestCfg,
			})
			if err != nil {
				t.Fatalf("UpdateClientConnState() unexpected error: %v", err)
			}
			if err := cc.WaitForConnectivityState(ctx, connectivity.Idle); err != nil {
				t.Fatalf("WaitForConnectivityState(Idle) failed: %v", err)
			}
			if err := cc.WaitForPickerWithErr(ctx, balancer.ErrNoSubConnAvailable); err != nil {
				t.Fatalf("WaitForPickerWithErr(ErrNoSubConnAvailable) failed: %v", err)
			}
		})
	}
}

type testAutoshardingClient struct {
	opts        sharding.ClientOptions
	closeCalled bool
}

func (c *testAutoshardingClient) onAssignmentUpdate(a *sharding.Assignment) {
	if c.opts.OnAssignmentUpdate != nil {
		c.opts.OnAssignmentUpdate(a)
	}
}

func (c *testAutoshardingClient) onAssignmentError(err error) {
	if c.opts.OnAssignmentError != nil {
		c.opts.OnAssignmentError(err)
	}
}

func (c *testAutoshardingClient) close() {
	c.closeCalled = true
}

func overrideNewAutoshardingClientForTesting(t *testing.T) chan *testAutoshardingClient {
	t.Helper()

	ch := make(chan *testAutoshardingClient, 1)
	orig := internal.NewAutoshardingClient
	t.Cleanup(func() { internal.NewAutoshardingClient = orig })
	internal.NewAutoshardingClient = func(opts sharding.ClientOptions) func() {
		client := &testAutoshardingClient{opts: opts}
		ch <- client
		return client.close
	}
	return ch
}

func waitForPicker(ctx context.Context, t *testing.T, cc *testutils.BalancerClientConn) balancer.Picker {
	select {
	case p := <-cc.NewPickerCh:
		return p
	case <-ctx.Done():
		t.Fatal("Timeout waiting for picker")
		return nil
	}
}

func waitForSubConn(ctx context.Context, t *testing.T, cc *testutils.BalancerClientConn) *testutils.TestSubConn {
	select {
	case sc := <-cc.NewSubConnCh:
		return sc
	case <-ctx.Done():
		t.Fatal("Timeout waiting for subconn")
		return nil
	}
}

func newContextWithShardingKey(ctx context.Context, key, value string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, key, value)
}

// Tests the scenario where the LB policy receives a valid update from the name
// resolver and a subsequent RPC results in a connection being established to
// the backend, and the grpc channel moves to Ready. The LB policy then receives
// another update from the name resolver with an empty endpoint list. The test
// verifies that the connected subchannel is shut down and the channel
// transitions to TransientFailure with an appropriate error picker.
func (s) TestUpdateClientConnState_EmptyEndpointsClosesChildSubConns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	defaultTestCfg := &autosharding.LBConfig{
		ChannelFactoryKey:  "test-factory-key",
		AutoShardingTarget: "test-target-%s",
		KeyHeaderName:      "test-header-name",
	}

	// Send a valid resolver update with one endpoint .
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolverStateWithChannelFactoryAndEndpoints([]resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")}),
		BalancerConfig: defaultTestCfg,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() unexpected error: %v", err)
	}

	// Verify that the channel is in Idle and with a queueing picker.
	if err := cc.WaitForConnectivityState(ctx, connectivity.Idle); err != nil {
		t.Fatalf("WaitForConnectivityState(Idle) failed: %v", err)
	}
	if err := cc.WaitForPickerWithErr(ctx, balancer.ErrNoSubConnAvailable); err != nil {
		t.Fatalf("WaitForPickerWithErr(%v) failed: %v", balancer.ErrNoSubConnAvailable, err)
	}

	// Wait for the autosharding client to be created.
	var testClient *testAutoshardingClient
	select {
	case testClient = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for autosharding client creation")
	}

	// Inject a valid assignment.
	testClient.onAssignmentUpdate(&sharding.Assignment{
		EndpointNames: []string{"host-0"},
		Slices:        []sharding.Slice{{StartKey: []byte(""), Endpoints: []int{0}}},
		Generation:    1,
	})

	// The first call to Pick must get queued and should result in a connection
	// attempt to the backend, moving the channel to Connecting.
	p := waitForPicker(ctx, t, cc)
	if _, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick() error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	sc := waitForSubConn(ctx, t, cc)
	select {
	case <-sc.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for SubConn.Connect()")
	}
	if err := cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("WaitForConnectivityState(Connecting) failed: %v", err)
	}

	// Move the SubConn to Ready and verify that the channel moves to Ready.
	sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
		t.Fatalf("WaitForConnectivityState(Ready) failed: %v", err)
	}
	// A subsequent call to Pick should succeed and return a SubConn.
	p = waitForPicker(ctx, t, cc)
	result, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")})
	if err != nil {
		t.Fatalf("Pick() failed: %v", err)
	}
	if result.SubConn != sc {
		t.Errorf("Pick() returned SubConn = %v, want %v", result.SubConn, sc)
	}

	// Send an empty endpoint list and verify that the SubConn is shut down and
	// the channel reports TransientFailure with errNoEndpointsFromNR.
	err = b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolverStateWithChannelFactoryAndEndpoints(nil),
		BalancerConfig: defaultTestCfg,
	})
	if !errors.Is(err, balancer.ErrBadResolverState) {
		t.Fatalf("UpdateClientConnState() error = %v, want %v", err, balancer.ErrBadResolverState)
	}
	select {
	case shutDownSC := <-cc.ShutdownSubConnCh:
		if shutDownSC != sc {
			t.Errorf("ShutdownSubConn = %v, want %v", shutDownSC, sc)
		}
	case <-ctx.Done():
		t.Fatal("Timeout waiting for SubConn shutdown")
	}
	if err := cc.WaitForConnectivityState(ctx, connectivity.TransientFailure); err != nil {
		t.Fatalf("WaitForConnectivityState(TransientFailure) failed: %v", err)
	}
	if err := cc.WaitForPickerWithErr(ctx, autosharding.ErrNoEndpointsFromNR); err != nil {
		t.Fatalf("WaitForPickerWithErr(%v) failed: %v", autosharding.ErrNoEndpointsFromNR, err)
	}
}
