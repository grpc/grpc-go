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
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/autosharding"
	"google.golang.org/grpc/balancer/autosharding/internal"
	"google.golang.org/grpc/balancer/autosharding/internal/sharding"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/experimental/balancer/hostname"
	"google.golang.org/grpc/experimental/resolver/locality"
	"google.golang.org/grpc/internal/grpcsync"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
)

const (
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
	errorTolerance          = .05 // For tests that rely on statistical significance.
)

// Tests scenarios where an update from the name resolver is invalid and
// verifies that the balancer transitions to TransientFailure with an
// appropriate error picker, closing any existing gRPC channel and autosharding
// client. A subsequent valid update should create a new channel and client and
// transition the channel back to Idle with a queueing picker.
func (s) TestUpdateClientConnState_ResolverError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	defaultTestCfg := &autosharding.LBConfig{
		ChannelFactoryKey:  "test-factory-key",
		AutoShardingTarget: "test-target-%s",
		KeyHeaderName:      "test-header-name",
	}
	badKeyCfg := &autosharding.LBConfig{
		ChannelFactoryKey:  "invalid-factory-key",
		AutoShardingTarget: "test-target-%s",
		KeyHeaderName:      "test-header-name",
	}
	providerErr := errors.New("channel factory error")

	tests := []struct {
		name               string
		endpoints          []resolver.Endpoint
		clientConnProvider func(string) (grpc.ClientConnInterface, func(), error)
		invalidCfg         *autosharding.LBConfig
		wantPickerErr      error
	}{
		{
			name:      "empty-endpoints",
			endpoints: nil,
			clientConnProvider: func(key string) (grpc.ClientConnInterface, func(), error) {
				return defaultTestClientConnProvider()(key)
			},
			invalidCfg:    defaultTestCfg,
			wantPickerErr: errors.New("autosharding: no endpoints from resolver"),
		},
		{
			name:      "endpoints-with-no-addresses",
			endpoints: []resolver.Endpoint{{Addresses: nil}},
			clientConnProvider: func(key string) (grpc.ClientConnInterface, func(), error) {
				return defaultTestClientConnProvider()(key)
			},
			invalidCfg:    defaultTestCfg,
			wantPickerErr: errors.New("autosharding: no endpoints from resolver"),
		},
		{
			name:               "missing-channel-factory",
			endpoints:          []resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")},
			clientConnProvider: nil,
			invalidCfg:         defaultTestCfg,
			wantPickerErr:      errors.New("autosharding: no channel factory found in resolver state"),
		},
		{
			name:      "channel-factory-returns-error",
			endpoints: []resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")},
			clientConnProvider: func(string) (grpc.ClientConnInterface, func(), error) {
				return nil, nil, providerErr
			},
			invalidCfg:    badKeyCfg,
			wantPickerErr: fmt.Errorf("autosharding: failed to create gRPC channel for key %q: %v", badKeyCfg.ChannelFactoryKey, providerErr),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
			cc := testutils.NewBalancerClientConn(t)
			b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
			defer b.Close()

			// 1. Initial invalid resolver state should return ErrBadResolverState
			// and transition the channel to TransientFailure with an error picker.
			err := b.UpdateClientConnState(balancer.ClientConnState{
				ResolverState:  resolverStateWithProviderAndEndpoints(tc.clientConnProvider, tc.endpoints),
				BalancerConfig: tc.invalidCfg,
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

			// 2. Valid resolver state should create a gRPC channel and autosharding
			// client, and transition the channel to Idle with a queueing picker.
			provider, testClientConnCh := testClientConnProvider()
			err = b.UpdateClientConnState(balancer.ClientConnState{
				ResolverState:  resolverStateWithProviderAndEndpoints(provider, []resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")}),
				BalancerConfig: defaultTestCfg,
			})
			if err != nil {
				t.Fatalf("UpdateClientConnState() unexpected error: %v", err)
			}
			var tcc1 *testClientConn
			select {
			case tcc1 = <-testClientConnCh:
			case <-ctx.Done():
				t.Fatal("Timeout waiting for initial gRPC channel creation")
			}
			var tac1 *testAutoshardingClient
			select {
			case tac1 = <-testAutoshardingClientCh:
			case <-ctx.Done():
				t.Fatal("Timeout waiting for initial autosharding client creation")
			}
			if err := cc.WaitForConnectivityState(ctx, connectivity.Idle); err != nil {
				t.Fatalf("WaitForConnectivityState(Idle) failed: %v", err)
			}
			if err := cc.WaitForPickerWithErr(ctx, balancer.ErrNoSubConnAvailable); err != nil {
				t.Fatalf("WaitForPickerWithErr(ErrNoSubConnAvailable) failed: %v", err)
			}

			// Inject an assignment so we can also verify that a subsequent
			// invalid update clears b.assignment.
			tac1.onAssignmentUpdate(&sharding.Assignment{
				EndpointNames: []string{"host-0"},
				Slices:        []sharding.Slice{{StartKey: []byte(""), Endpoints: []int{0}}},
				Generation:    1,
			})
			_ = waitForPicker(ctx, t, cc)

			// 3. A subsequent invalid update should transition the channel back
			// to TransientFailure and close the existing client and gRPC channel.
			err = b.UpdateClientConnState(balancer.ClientConnState{
				ResolverState:  resolverStateWithProviderAndEndpoints(tc.clientConnProvider, tc.endpoints),
				BalancerConfig: tc.invalidCfg,
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
			select {
			case <-tac1.closeCalled.Done():
			case <-ctx.Done():
				t.Fatal("Timeout waiting for autosharding client to be closed on invalid update")
			}
			select {
			case <-tcc1.closeCalled.Done():
			case <-ctx.Done():
				t.Fatal("Timeout waiting for gRPC channel to be closed on invalid update")
			}

			// 4. A subsequent valid update with the same config should create a
			// new gRPC channel and autosharding client, and transition back to
			// Idle with a queueing picker (since the previous assignment was
			// cleared).
			err = b.UpdateClientConnState(balancer.ClientConnState{
				ResolverState:  resolverStateWithProviderAndEndpoints(provider, []resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")}),
				BalancerConfig: defaultTestCfg,
			})
			if err != nil {
				t.Fatalf("UpdateClientConnState() unexpected error: %v", err)
			}
			select {
			case <-testClientConnCh:
			case <-ctx.Done():
				t.Fatal("Timeout waiting for new gRPC channel creation after recovery")
			}
			select {
			case <-testAutoshardingClientCh:
			case <-ctx.Done():
				t.Fatal("Timeout waiting for new autosharding client creation after recovery")
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

// Tests that ResolverError transitions the channel to TransientFailure when no
// valid endpoints have been received yet, and preserves existing state when a
// valid update has already been processed.
func (s) TestResolverError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	// ResolverError before any valid resolver update should put the channel in
	// TransientFailure and surface the resolver error from the picker.
	resolverErr := errors.New("initial resolver failure")
	b.ResolverError(resolverErr)
	if err := cc.WaitForConnectivityState(ctx, connectivity.TransientFailure); err != nil {
		t.Fatalf("WaitForConnectivityState(TransientFailure) failed: %v", err)
	}
	if err := cc.WaitForPickerWithErr(ctx, resolverErr); err != nil {
		t.Fatalf("WaitForPickerWithErr(%v) failed: %v", resolverErr, err)
	}

	// A valid resolver update should clear lastResolverErr and move the channel
	// to Idle while waiting for an initial assignment.
	defaultTestCfg := &autosharding.LBConfig{
		ChannelFactoryKey:  "test-factory-key",
		AutoShardingTarget: "test-target-%s",
		KeyHeaderName:      "test-header-name",
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), []resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")}),
		BalancerConfig: defaultTestCfg,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() unexpected error: %v", err)
	}
	if err := cc.WaitForConnectivityState(ctx, connectivity.Idle); err != nil {
		t.Fatalf("WaitForConnectivityState(Idle) failed: %v", err)
	}
	if err := cc.WaitForPickerWithErr(ctx, balancer.ErrNoSubConnAvailable); err != nil {
		t.Fatalf("WaitForPickerWithErr(ErrNoSubConnAvailable) failed: %v", err)
	}

	// ResolverError after a valid resolver update should not overwrite
	// lastResolverErr when endpointMap is non-empty and children are in Idle.
	b.ResolverError(errors.New("subsequent resolver failure"))
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if err := cc.WaitForConnectivityState(sCtx, connectivity.TransientFailure); err == nil {
		t.Fatal("Channel unexpectedly transitioned to TransientFailure after ResolverError with valid endpoints")
	}

	// If a subsequent update has valid endpoints but is rejected due to invalid
	// configuration (closing the autosharding client), a following ResolverError
	// should overwrite lastResolverErr and be surfaced by the picker.
	err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolverStateWithProviderAndEndpoints(nil, []resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")}),
		BalancerConfig: defaultTestCfg,
	})
	if !errors.Is(err, balancer.ErrBadResolverState) {
		t.Fatalf("UpdateClientConnState() error = %v, want %v", err, balancer.ErrBadResolverState)
	}
	if err := cc.WaitForConnectivityState(ctx, connectivity.TransientFailure); err != nil {
		t.Fatalf("WaitForConnectivityState(TransientFailure) failed: %v", err)
	}
	resolverErr2 := errors.New("resolver failure after invalid config update")
	b.ResolverError(resolverErr2)
	if err := cc.WaitForPickerWithErr(ctx, resolverErr2); err != nil {
		t.Fatalf("WaitForPickerWithErr(%v) failed: %v", resolverErr2, err)
	}
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

	// Send a valid resolver update with one endpoint.
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), []resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")}),
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
		ResolverState:  resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), nil),
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
	if err := cc.WaitForPickerWithErr(ctx, internal.ErrNoEndpointsFromNR); err != nil {
		t.Fatalf("WaitForPickerWithErr(%v) failed: %v", internal.ErrNoEndpointsFromNR, err)
	}
}

// Tests that an identical update from the name resolver does not create a new
// gRPC channel or autosharding client, and does not close the existing channel
// or client.
func (s) TestUpdateClientConnState_IdenticalUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	// Initial configuration with locality "us-east1" and "%s" in target.
	provider, testClientConnCh := testClientConnProvider()
	baseState := resolverStateWithProviderAndEndpoints(provider, []resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")})
	lbConfig := &autosharding.LBConfig{
		ChannelFactoryKey:        "key-1",
		AutoShardingTarget:       "service-%s-shard",
		KeyHeaderName:            "test-header-name",
		InitialAssignmentTimeout: iserviceconfig.Duration(60 * time.Second),
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  locality.Set(baseState, "us-east1"),
		BalancerConfig: lbConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() unexpected error: %v", err)
	}

	// Verify that the gRPC channel is created with the expected key.
	var tcc1 *testClientConn
	select {
	case tcc1 = <-testClientConnCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for initial gRPC channel creation")
	}
	if tcc1.key != "key-1" {
		t.Fatalf("Channel created with key %q, want %q", tcc1.key, "key-1")
	}

	// Verify that the AutoshardingClient is created with the expected options.
	var tac1 *testAutoshardingClient
	select {
	case tac1 = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for initial autosharding client creation")
	}
	if tac1.opts.CC != tcc1 {
		t.Errorf("Autosharding client created with CC %v, want %v", tac1.opts.CC, tcc1)
	}
	if want := "service-us-east1-shard"; tac1.opts.AutoshardingTarget != want {
		t.Errorf("Autosharding client created with AutoshardingTarget = %q, want %q", tac1.opts.AutoshardingTarget, want)
	}
	if tac1.opts.UUID == "" {
		t.Error("Autosharding client created with empty UUID, want non-empty UUID")
	}
	if want := 60 * time.Second; tac1.opts.InitialAssignmentTimeout != want {
		t.Errorf("Autosharding client created with InitialAssignmentTimeout = %v, want %v", tac1.opts.InitialAssignmentTimeout, want)
	}

	// Re-sending the same config should not create a new channel or client, and
	// should not close the existing channel and client.
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  locality.Set(baseState, "us-east1"),
		BalancerConfig: lbConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() unexpected error: %v", err)
	}
	select {
	case ch := <-testClientConnCh:
		t.Fatalf("Unexpected new gRPC channel created for key %q", ch.key)
	case c := <-testAutoshardingClientCh:
		t.Fatalf("Unexpected new autosharding client created for target %q", c.opts.AutoshardingTarget)
	case <-tcc1.closeCalled.Done():
		t.Fatal("gRPC channel was unexpectedly closed")
	case <-tac1.closeCalled.Done():
		t.Fatal("AutoshardingClient was unexpectedly closed")
	case <-time.After(defaultTestShortTimeout):
	}
}

// Tests that changing the locality in the resolver update (with "%s" in the
// autosharding target) creates a new autosharding client with the updated
// locality, but reuses the existing gRPC channel if the channel factory key is
// unchanged.
func (s) TestUpdateClientConnState_AutoshardingTargetChange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	// Initial configuration with locality "us-east1" and "%s" in target.
	provider, testClientConnCh := testClientConnProvider()
	baseState := resolverStateWithProviderAndEndpoints(provider, []resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")})
	lbConfig := &autosharding.LBConfig{
		ChannelFactoryKey:        "key-1",
		AutoShardingTarget:       "service-%s-shard",
		KeyHeaderName:            "test-header-name",
		InitialAssignmentTimeout: iserviceconfig.Duration(60 * time.Second),
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  locality.Set(baseState, "us-east1"),
		BalancerConfig: lbConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() unexpected error: %v", err)
	}

	// Verify that the gRPC channel is created with the expected key.
	var tcc1 *testClientConn
	select {
	case tcc1 = <-testClientConnCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for initial gRPC channel creation")
	}
	if tcc1.key != "key-1" {
		t.Fatalf("Channel created with key %q, want %q", tcc1.key, "key-1")
	}

	// Verify that the AutoshardingClient is created with the expected options.
	var tac1 *testAutoshardingClient
	select {
	case tac1 = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for initial autosharding client creation")
	}
	if tac1.opts.CC != tcc1 {
		t.Errorf("Autosharding client created with CC %v, want %v", tac1.opts.CC, tcc1)
	}
	if want := "service-us-east1-shard"; tac1.opts.AutoshardingTarget != want {
		t.Errorf("Autosharding client created with AutoshardingTarget = %q, want %q", tac1.opts.AutoshardingTarget, want)
	}
	if tac1.opts.UUID == "" {
		t.Error("Autosharding client created with empty UUID, want non-empty UUID")
	}
	if want := 60 * time.Second; tac1.opts.InitialAssignmentTimeout != want {
		t.Errorf("Autosharding client created with InitialAssignmentTimeout = %v, want %v", tac1.opts.InitialAssignmentTimeout, want)
	}

	// Changing locality (with "%s" in target) and keeping the channel
	// factory key unchanged should reuse the gRPC channel, but create a new
	// AutoshardingClient.
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  locality.Set(baseState, "eu-west1"),
		BalancerConfig: lbConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() unexpected error: %v", err)
	}

	// Verify the options passed to the new autosharding client.
	var tac2 *testAutoshardingClient
	select {
	case tac2 = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for new autosharding client creation on locality change")
	}
	if tac2.opts.CC != tcc1 {
		t.Errorf("New autosharding client does not reuse existing gRPC channel, got %v, want %v", tac2.opts.CC, tcc1)
	}
	if want := "service-eu-west1-shard"; tac2.opts.AutoshardingTarget != want {
		t.Errorf("New autosharding client has AutoshardingTarget = %q, want %q", tac2.opts.AutoshardingTarget, want)
	}
	if tac2.opts.UUID != tac1.opts.UUID {
		t.Errorf("New autosharding client has UUID = %q, want reused balancer UUID %q", tac2.opts.UUID, tac1.opts.UUID)
	}

	// Verify that the previous autosharding client is closed.
	select {
	case <-tac1.closeCalled.Done():
	case <-ctx.Done():
		t.Fatal("Timeout waiting for previous autosharding client to be closed on locality change")
	}

	// Verify that the gRPC channel is not closed.
	select {
	case <-tcc1.closeCalled.Done():
		t.Fatalf("Existing gRPC channel was unexpectedly closed on locality change")
	case <-time.After(defaultTestShortTimeout):
	}
}

// Tests that if the locality attribute is missing in the resolver update, and
// the autosharding target contains "%s", the balancer replaces "%s" with an
// empty string "" when creating the new autosharding client.
func (s) TestUpdateClientConnState_MissingLocality(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	// Missing locality attribute when target contains "%s" replaces "%s" with
	// empty string "".
	provider, testClientConnCh := testClientConnProvider()
	state := resolverStateWithProviderAndEndpoints(provider, []resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")})
	lbConfig := &autosharding.LBConfig{
		ChannelFactoryKey:        "key-1",
		AutoShardingTarget:       "service-%s-shard",
		KeyHeaderName:            "test-header-name",
		InitialAssignmentTimeout: iserviceconfig.Duration(60 * time.Second),
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  state,
		BalancerConfig: lbConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() unexpected error: %v", err)
	}

	// Verify that the gRPC channel is created with the expected key.
	var tcc1 *testClientConn
	select {
	case tcc1 = <-testClientConnCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for initial gRPC channel creation")
	}
	if tcc1.key != "key-1" {
		t.Fatalf("Channel created with key %q, want %q", tcc1.key, "key-1")
	}

	// Verify the options passed to the new autosharding client.
	var tac1 *testAutoshardingClient
	select {
	case tac1 = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for new autosharding client creation")
	}
	if tac1.opts.CC != tcc1 {
		t.Errorf("Autosharding client created with CC %v, want %v", tac1.opts.CC, tcc1)
	}
	if want := "service--shard"; tac1.opts.AutoshardingTarget != want {
		t.Errorf("New autosharding client has AutoshardingTarget = %q, want %q", tac1.opts.AutoshardingTarget, want)
	}
	if tac1.opts.UUID == "" {
		t.Error("Autosharding client created with empty UUID, want non-empty UUID")
	}
}

// Tests that changing the channelFactoryKey in the resolver update creates a new
// gRPC channel and a new autosharding client, and closes the old ones.
func (s) TestUpdateClientConnState_ChannelFactoryKeyChange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	// Initial configuration with locality "us-east1" and "%s" in target.
	provider, testClientConnCh := testClientConnProvider()
	baseState := resolverStateWithProviderAndEndpoints(provider, []resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")})
	lbConfig := &autosharding.LBConfig{
		ChannelFactoryKey:        "key-1",
		AutoShardingTarget:       "service-%s-shard",
		KeyHeaderName:            "test-header-name",
		InitialAssignmentTimeout: iserviceconfig.Duration(60 * time.Second),
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  locality.Set(baseState, "us-east1"),
		BalancerConfig: lbConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() unexpected error: %v", err)
	}

	// Verify that the gRPC channel is created with the expected key.
	var tcc1 *testClientConn
	select {
	case tcc1 = <-testClientConnCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for initial gRPC channel creation")
	}
	if tcc1.key != "key-1" {
		t.Fatalf("Channel created with key %q, want %q", tcc1.key, "key-1")
	}

	// Verify that the AutoshardingClient is created with the expected options.
	var tac1 *testAutoshardingClient
	select {
	case tac1 = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for initial autosharding client creation")
	}
	if tac1.opts.CC != tcc1 {
		t.Errorf("Autosharding client created with CC %v, want %v", tac1.opts.CC, tcc1)
	}
	if want := "service-us-east1-shard"; tac1.opts.AutoshardingTarget != want {
		t.Errorf("Autosharding client created with AutoshardingTarget = %q, want %q", tac1.opts.AutoshardingTarget, want)
	}
	if tac1.opts.UUID == "" {
		t.Error("Autosharding client created with empty UUID, want non-empty UUID")
	}
	if want := 60 * time.Second; tac1.opts.InitialAssignmentTimeout != want {
		t.Errorf("Autosharding client created with InitialAssignmentTimeout = %v, want %v", tac1.opts.InitialAssignmentTimeout, want)
	}

	// Changing channelFactoryKey creates a new gRPC channel and a new
	// autoshardingClient, and closes the old ones.
	config := &autosharding.LBConfig{
		ChannelFactoryKey:  "key-2",
		AutoShardingTarget: "service-%s-shard",
		KeyHeaderName:      "test-header-name",
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  locality.Set(baseState, "us-east1"),
		BalancerConfig: config,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() unexpected error: %v", err)
	}

	// Verify that a new gRPC channel is created.
	var tcc2 *testClientConn
	select {
	case tcc2 = <-testClientConnCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for the second gRPC channel creation")
	}
	if tcc2.key != "key-2" {
		t.Fatalf("Channel created with key %q, want %q", tcc2.key, "key-2")
	}

	// Verify that a new autosharding client is created.
	var tac2 *testAutoshardingClient
	select {
	case tac2 = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for second autosharding client creation")
	}
	if tac2.opts.CC != tcc2 {
		t.Fatalf("Autosharding client created with CC %v, want %v", tac2.opts.CC, tcc2)
	}

	// Wait for the previous autosharding client and gRPC channel to be
	// closed.
	select {
	case <-tac1.closeCalled.Done():
	case <-ctx.Done():
		t.Fatal("Timeout waiting for the previous autosharding client to be closed")
	}
	select {
	case <-tcc1.closeCalled.Done():
	case <-ctx.Done():
		t.Fatal("Timeout waiting for the previous gRPC channel to be closed")
	}
}

// Tests that closing the balancer closes the autosharding client and the gRPC
// channel.
func (s) TestUpdateClientConnState_BalancerClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	// Initial configuration with locality "us-east1" and "%s" in target.
	provider, testClientConnCh := testClientConnProvider()
	baseState := resolverStateWithProviderAndEndpoints(provider, []resolver.Endpoint{newTestEndpoint("1.1.1.1:1", "host-0")})
	lbConfig := &autosharding.LBConfig{
		ChannelFactoryKey:        "key-1",
		AutoShardingTarget:       "service-%s-shard",
		KeyHeaderName:            "test-header-name",
		InitialAssignmentTimeout: iserviceconfig.Duration(60 * time.Second),
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  locality.Set(baseState, "us-east1"),
		BalancerConfig: lbConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() unexpected error: %v", err)
	}

	// Verify that the gRPC channel is created with the expected key.
	var tcc1 *testClientConn
	select {
	case tcc1 = <-testClientConnCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for initial gRPC channel creation")
	}
	if tcc1.key != "key-1" {
		t.Fatalf("Channel created with key %q, want %q", tcc1.key, "key-1")
	}

	// Verify that the AutoshardingClient is created with the expected options.
	var tac1 *testAutoshardingClient
	select {
	case tac1 = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for initial autosharding client creation")
	}
	if tac1.opts.CC != tcc1 {
		t.Errorf("Autosharding client created with CC %v, want %v", tac1.opts.CC, tcc1)
	}
	if want := "service-us-east1-shard"; tac1.opts.AutoshardingTarget != want {
		t.Errorf("Autosharding client created with AutoshardingTarget = %q, want %q", tac1.opts.AutoshardingTarget, want)
	}
	if tac1.opts.UUID == "" {
		t.Error("Autosharding client created with empty UUID, want non-empty UUID")
	}
	if want := 60 * time.Second; tac1.opts.InitialAssignmentTimeout != want {
		t.Errorf("Autosharding client created with InitialAssignmentTimeout = %v, want %v", tac1.opts.InitialAssignmentTimeout, want)
	}

	b.Close()
	select {
	case <-tac1.closeCalled.Done():
	case <-ctx.Done():
		t.Fatal("Timeout waiting for autosharding client to be closed")
	}
	select {
	case <-tcc1.closeCalled.Done():
	case <-ctx.Done():
		t.Fatal("Timeout waiting for gRPC channel to be closed")
	}
}

// Tests that endpoints with no addresses are ignored, duplicate endpoints with
// the same hostname or the same address set are ignored, and endpoints without
// an explicit hostname attribute fall back to using their first address as the
// hostname.
func (s) TestEndpointFilteringAndDeduplication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	provider, _ := testClientConnProvider()
	epNoAddr := resolver.Endpoint{Addresses: nil}
	ep1 := newTestEndpoint("10.0.0.1:8080", "host-a")
	ep1DupHostname := newTestEndpoint("10.0.0.2:8080", "host-a")
	ep1DupAddr := newTestEndpoint("10.0.0.1:8080", "host-b")
	ep2NoHostname := newTestEndpoint("10.0.0.3:8080", "")
	ep2DupAddr := newTestEndpoint("10.0.0.3:8080", "")
	state := resolverStateWithProviderAndEndpoints(provider, []resolver.Endpoint{
		epNoAddr, ep1, ep1DupHostname, ep1DupAddr, ep2NoHostname, ep2DupAddr,
	})
	defaultTestCfg := &autosharding.LBConfig{
		ChannelFactoryKey:  "test-factory-key",
		AutoShardingTarget: "test-target",
		KeyHeaderName:      "test-header-name",
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  state,
		BalancerConfig: defaultTestCfg,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() failed: %v", err)
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
	// ["", "m") -> "host-a", ["m", "t") -> "10.0.0.3:8080", and ["t", inf) -> "host-b".
	testClient.onAssignmentUpdate(&sharding.Assignment{
		EndpointNames: []string{"host-a", "10.0.0.3:8080", "host-b"},
		Slices: []sharding.Slice{
			{StartKey: []byte(""), Endpoints: []int{0}},
			{StartKey: []byte("m"), Endpoints: []int{1}},
			{StartKey: []byte("t"), Endpoints: []int{2}},
		},
		Generation: 1,
	})

	// Picking key "a" should trigger connection to ep1 ("10.0.0.1:8080"), and
	// not ep1DupHostname ("10.0.0.2:8080").
	p := waitForPicker(ctx, t, cc)
	if _, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick(\"a\") error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	sc1 := waitForSubConn(ctx, t, cc)
	if got, want := sc1.Addresses[0].Addr, "10.0.0.1:8080"; got != want {
		t.Fatalf("SubConn for host-a created with addr %q, want %q", got, want)
	}
	select {
	case <-sc1.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for SubConn.Connect()")
	}
	if err := cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("WaitForConnectivityState(Connecting) failed: %v", err)
	}

	// Picking key "n" should trigger connection to ep2NoHostname ("10.0.0.3:8080").
	if _, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "n")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick(\"n\") error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	sc2 := waitForSubConn(ctx, t, cc)
	if got, want := sc2.Addresses[0].Addr, "10.0.0.3:8080"; got != want {
		t.Fatalf("SubConn for 10.0.0.3:8080 created with addr %q, want %q", got, want)
	}
	select {
	case <-sc2.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for SubConn.Connect()")
	}

	// Picking key "z" (assigned to "host-b") should fail because ep1DupAddr
	// ("host-b") shared the same address set as ep1 ("host-a") and was ignored.
	const wantErr = "autosharding: matching slice has no available endpoints"
	if _, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "z")}); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("Pick(\"z\") error = %v, want error containing %q", err, wantErr)
	}
}

// Tests that before any assignment or error is reported by the autosharding client,
// the balancer's picker queues RPCs.
func (s) TestAutoshardingClient_NoAssignmentOrError_QueueingPicker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	tests := []struct {
		name            string
		fallbackEnabled bool
	}{
		{
			name:            "fallback enabled",
			fallbackEnabled: true,
		},
		{
			name:            "fallback disabled",
			fallbackEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
			cc := testutils.NewBalancerClientConn(t)
			b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
			defer b.Close()

			// Initial state with two endpoints and a valid LBConfig.
			state := resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), []resolver.Endpoint{
				newTestEndpoint("10.0.0.1:8080", "host-a"),
				newTestEndpoint("10.0.0.2:8080", "host-b"),
			})
			lbConfig := &autosharding.LBConfig{
				ChannelFactoryKey:  "test-factory-key",
				AutoShardingTarget: "test-target",
				KeyHeaderName:      "test-header-name",
				EnableFallback:     tt.fallbackEnabled,
			}
			if err := b.UpdateClientConnState(balancer.ClientConnState{
				ResolverState:  state,
				BalancerConfig: lbConfig,
			}); err != nil {
				t.Fatalf("UpdateClientConnState() failed: %v", err)
			}

			select {
			case <-testAutoshardingClientCh:
			case <-ctx.Done():
				t.Fatal("Timeout waiting for creation of autosharding client")
			}

			// Ensure that before any assignment or error from the autosharding client,
			// channel is Idle and queues RPCs.
			if err := cc.WaitForConnectivityState(ctx, connectivity.Idle); err != nil {
				t.Fatalf("WaitForConnectivityState(Idle) failed: %v", err)
			}
			if err := cc.WaitForPickerWithErr(ctx, balancer.ErrNoSubConnAvailable); err != nil {
				t.Fatalf("WaitForPickerWithErr(ErrNoSubConnAvailable) failed: %v", err)
			}
		})
	}
}

// Tests that when the autosharding client reports a valid assignment, the
// balancer creates the expected SubConns when RPCs are made and routes RPCs to
// the expected backends.
func (s) TestAutoshardingClient_ValidAssignment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	// Initial state with two endpoints and a valid LBConfig.
	state := resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), []resolver.Endpoint{
		newTestEndpoint("10.0.0.1:8080", "host-a"),
		newTestEndpoint("10.0.0.2:8080", "host-b"),
	})
	lbConfig := &autosharding.LBConfig{
		ChannelFactoryKey:  "test-factory-key",
		AutoShardingTarget: "test-target",
		KeyHeaderName:      "test-header-name",
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  state,
		BalancerConfig: lbConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() failed: %v", err)
	}
	_ = waitForPicker(ctx, t, cc) // Wait for the initial picker to be created.

	// Inject a valid assignment.
	// ["", "m") -> host-a, ["m", inf) -> host-b.
	//
	// And verify that the LB policy is still reporting Idle state.
	var tac *testAutoshardingClient
	select {
	case tac = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for creation of autosharding client")
	}
	tac.onAssignmentUpdate(&sharding.Assignment{
		EndpointNames: []string{"host-a", "host-b"},
		Slices: []sharding.Slice{
			{StartKey: []byte(""), Endpoints: []int{0}},
			{StartKey: []byte("m"), Endpoints: []int{1}},
		},
		Generation: 1,
	})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Idle); err != nil {
		t.Fatalf("WaitForConnectivityState(Idle) failed: %v", err)
	}

	// The first call to Pick must get queued and should result in a connection
	// attempt to the backend A, moving the channel to Connecting.
	picker := waitForPicker(ctx, t, cc)
	if _, err := picker.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick() error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	scA := waitForSubConn(ctx, t, cc)
	if want, got := "10.0.0.1:8080", scA.Addresses[0].Addr; got != want {
		t.Fatalf("SubConn created with addr %q, want %q", got, want)
	}
	select {
	case <-scA.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for SubConn.Connect()")
	}
	if err := cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("WaitForConnectivityState(Connecting) failed: %v", err)
	}

	// Move subconnA to Ready and verify that the channel moves to Ready.
	scA.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	scA.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
		t.Fatalf("WaitForConnectivityState(Ready) failed: %v", err)
	}

	// Grab the ready picker and make the next Pick that should get routed to
	// backend B, which should trigger a connection attempt to backend B.
	picker = waitForPicker(ctx, t, cc)
	if _, err := picker.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "m")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick() error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	scB := waitForSubConn(ctx, t, cc)
	if want, got := "10.0.0.2:8080", scB.Addresses[0].Addr; got != want {
		t.Fatalf("SubConn created with addr %q, want %q", got, want)
	}
	select {
	case <-scB.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for SubConn.Connect()")
	}

	// Move subconnB to Ready.
	scB.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	scB.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Verify that the picker routes keys to the expected backends.
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		resA, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")})
		if err != nil || resA.SubConn != scA {
			return fmt.Errorf("Pick(\"a\") = (%v, %v), want SubConn %v", resA, err, scA)
		}
		resM, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "m")})
		if err != nil || resM.SubConn != scB {
			return fmt.Errorf("Pick(\"m\") = (%v, %v), want SubConn %v", resM, err, scB)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Inject another valid assignment that changes how keys are routed.
	// ["", "z") -> host-a, ["z", inf) -> host-b.
	tac.onAssignmentUpdate(&sharding.Assignment{
		EndpointNames: []string{"host-a", "host-b"},
		Slices: []sharding.Slice{
			{StartKey: []byte(""), Endpoints: []int{0}},
			{StartKey: []byte("z"), Endpoints: []int{1}},
		},
		Generation: 2,
	})
	// Verify that the picker routes keys to the expected backends.
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		resA, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")})
		if err != nil || resA.SubConn != scA {
			return fmt.Errorf("Pick(\"a\") = (%v, %v), want SubConn %v", resA, err, scA)
		}
		resM, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "m")})
		if err != nil || resM.SubConn != scA {
			return fmt.Errorf("Pick(\"m\") = (%v, %v), want SubConn %v", resM, err, scA)
		}
		resZ, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "z")})
		if err != nil || resZ.SubConn != scB {
			return fmt.Errorf("Pick(\"z\") = (%v, %v), want SubConn %v", resZ, err, scB)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Tests that when the autosharding client reports an error and fallback is
// disabled, the balancer's picker fails RPCs, and surfaces the error reported
// by the autosharding client. A subsequent valid assignment from the
// autosharding client should restore the balancer to a working state.
func (s) TestAutoshardingClient_Error_FallbackDisabled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	// Initial state with two endpoints and a valid LBConfig.
	state := resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), []resolver.Endpoint{
		newTestEndpoint("10.0.0.1:8080", "host-a"),
		newTestEndpoint("10.0.0.2:8080", "host-b"),
	})
	lbConfig := &autosharding.LBConfig{
		ChannelFactoryKey:  "test-factory-key",
		AutoShardingTarget: "test-target",
		KeyHeaderName:      "test-header-name",
		EnableFallback:     false,
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  state,
		BalancerConfig: lbConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() failed: %v", err)
	}
	_ = waitForPicker(ctx, t, cc) // Wait for the initial picker to be created.

	// Inject an error from the autosharding client.
	var tac *testAutoshardingClient
	select {
	case tac = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for creation of autosharding client")
	}
	autoshardingClientErr := errors.New("autosharding client stream error")
	tac.onAssignmentError(autoshardingClientErr)

	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		_, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "doesn't-matter")})
		if err == nil || !strings.Contains(err.Error(), autoshardingClientErr.Error()) {
			return fmt.Errorf("Pick() error = %v, want error containing %q", err, autoshardingClientErr.Error())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Inject a valid assignment.
	// ["", "m") -> host-a, ["m", inf) -> host-b.
	//
	// And verify that the LB policy is still reporting Idle state.
	tac.onAssignmentUpdate(&sharding.Assignment{
		EndpointNames: []string{"host-a", "host-b"},
		Slices: []sharding.Slice{
			{StartKey: []byte(""), Endpoints: []int{0}},
			{StartKey: []byte("m"), Endpoints: []int{1}},
		},
		Generation: 1,
	})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Idle); err != nil {
		t.Fatalf("WaitForConnectivityState(Idle) failed: %v", err)
	}

	// The first call to Pick must get queued and should result in a connection
	// attempt to the backend A, moving the channel to Connecting.
	picker := waitForPicker(ctx, t, cc)
	if _, err := picker.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick() error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	scA := waitForSubConn(ctx, t, cc)
	if want, got := "10.0.0.1:8080", scA.Addresses[0].Addr; got != want {
		t.Fatalf("SubConn created with addr %q, want %q", got, want)
	}
	select {
	case <-scA.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for SubConn.Connect()")
	}
	if err := cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("WaitForConnectivityState(Connecting) failed: %v", err)
	}

	// Move subconnA to Ready and verify that the channel moves to Ready.
	scA.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	scA.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
		t.Fatalf("WaitForConnectivityState(Ready) failed: %v", err)
	}

	// Grab the ready picker and make the next Pick that should get routed to
	// backend B, which should trigger a connection attempt to backend B.
	picker = waitForPicker(ctx, t, cc)
	if _, err := picker.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "m")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick() error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	scB := waitForSubConn(ctx, t, cc)
	if want, got := "10.0.0.2:8080", scB.Addresses[0].Addr; got != want {
		t.Fatalf("SubConn created with addr %q, want %q", got, want)
	}
	select {
	case <-scB.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for SubConn.Connect()")
	}

	// Move subconnB to Ready.
	scB.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	scB.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Verify that the picker routes keys to the expected backends.
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		resA, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")})
		if err != nil || resA.SubConn != scA {
			return fmt.Errorf("Pick(\"a\") = (%v, %v), want SubConn %v", resA, err, scA)
		}
		resM, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "m")})
		if err != nil || resM.SubConn != scB {
			return fmt.Errorf("Pick(\"m\") = (%v, %v), want SubConn %v", resM, err, scB)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Tests that when the autosharding client reports an error and fallback is
// enabled, the balancer's picker routes RPCs to the fallback pool.
func (s) TestAutoshardingClient_Error_FallbackEnabled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	// Initial state with two endpoints and a valid LBConfig.
	state := resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), []resolver.Endpoint{
		newTestEndpoint("10.0.0.1:8080", "host-a"),
		newTestEndpoint("10.0.0.2:8080", "host-b"),
	})
	lbConfig := &autosharding.LBConfig{
		ChannelFactoryKey:  "test-factory-key",
		AutoShardingTarget: "test-target",
		KeyHeaderName:      "test-header-name",
		EnableFallback:     true,
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  state,
		BalancerConfig: lbConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() failed: %v", err)
	}
	_ = waitForPicker(ctx, t, cc) // Wait for the initial picker to be created.

	// Inject an error from the autosharding client.
	var tac *testAutoshardingClient
	select {
	case tac = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for creation of autosharding client")
	}
	autoshardingClientErr := errors.New("autosharding client stream error")
	tac.onAssignmentError(autoshardingClientErr)

	// Make picks until both backends are picked.
	p := waitForPicker(ctx, t, cc)
	var scA, scB *testutils.TestSubConn
	var seenA, seenB bool
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "doesn't-matter")})

		// A non-blocking select is used here as the random pick might not have
		// created a new SubConn if it picked the same one as a previous pick.
		var sc *testutils.TestSubConn
		select {
		case sc = <-cc.NewSubConnCh:
		default:
		}
		if sc == nil {
			continue
		}

		switch sc.Addresses[0].Addr {
		case "10.0.0.1:8080":
			scA = sc
			seenA = true
		case "10.0.0.2:8080":
			scB = sc
			seenB = true
		}
		if seenA && seenB {
			break
		}
	}
	if ctx.Err() != nil {
		t.Fatal("Timeout waiting for both SubConns to be created")
	}

	// Move both subconnA and subconnB to Ready.
	scA.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	scA.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	scB.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	scB.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Wait for the balancer to report Ready state.
	if err := cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
		t.Fatalf("WaitForConnectivityState(Ready) failed: %v", err)
	}

	// Make a large number of picks and verify that they are routed to both
	// backends in roughly equal proportion.
	p = waitForPicker(ctx, t, cc)
	numPicks := computeIdealNumberOfRPCs(t, .5, errorTolerance)
	gotPerBackend := checkPicksOK(ctx, t, p, numPicks)
	for _, backend := range []string{"10.0.0.1:8080", "10.0.0.2:8080"} {
		got := float64(gotPerBackend[backend]) / float64(numPicks)
		want := .5
		if !cmp.Equal(got, want, cmpopts.EquateApprox(0, errorTolerance)) {
			t.Errorf("Fraction of Picks to backend %s: got %v, want %v (margin: +-%v)", backend, got, want, errorTolerance)
		}
	}
}

// Tests that when the autosharding client reports an error and fallback is
// disabled, and then fallback is enabled, the balancer's picker routes RPCs to
// the fallback pool.
func (s) TestAutoshardingClient_Error_FallbackDisabledToEnabled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	// Initial state with two endpoints and a valid LBConfig.
	state := resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), []resolver.Endpoint{
		newTestEndpoint("10.0.0.1:8080", "host-a"),
		newTestEndpoint("10.0.0.2:8080", "host-b"),
	})
	lbConfig := &autosharding.LBConfig{
		ChannelFactoryKey:  "test-factory-key",
		AutoShardingTarget: "test-target",
		KeyHeaderName:      "test-header-name",
		EnableFallback:     false,
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  state,
		BalancerConfig: lbConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() failed: %v", err)
	}
	_ = waitForPicker(ctx, t, cc) // Wait for the initial picker to be created.

	// Inject an error from the autosharding client.
	var tac *testAutoshardingClient
	select {
	case tac = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for creation of autosharding client")
	}
	autoshardingClientErr := errors.New("autosharding client stream error")
	tac.onAssignmentError(autoshardingClientErr)

	// Ensure that the channel moves to TransientFailure and that the picker
	// returns the error reported by the autosharding client.
	if err := cc.WaitForConnectivityState(ctx, connectivity.TransientFailure); err != nil {
		t.Fatalf("WaitForConnectivityState(TransientFailure) failed: %v", err)
	}
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		_, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "doesn't-matter")})
		if err == nil || !strings.Contains(err.Error(), autoshardingClientErr.Error()) {
			return fmt.Errorf("Pick() error = %v, want error containing %q", err, autoshardingClientErr.Error())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Update state to enable fallback.
	lbConfig.EnableFallback = true
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  state,
		BalancerConfig: lbConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() failed: %v", err)
	}

	// Ensure that the channel moves to Idle.
	if err := cc.WaitForConnectivityState(ctx, connectivity.Idle); err != nil {
		t.Fatalf("WaitForConnectivityState(Idle) failed: %v", err)
	}

	// A pick should now be routed to the fallback pool, which should trigger a
	// connection attempt.
	p := waitForPicker(ctx, t, cc)
	if _, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "m")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick() error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	_ = waitForSubConn(ctx, t, cc)
}

// Tests the case where the autosharding target changes causing a autosharding
// client to be created. Verified that the previous assignment is used by the
// balancer until the new client returns one.
func (s) TestAutoshardingClient_TargetChange_PreviousAssignmentInUse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	// Initial state with two endpoints and a valid LBConfig.
	state := resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), []resolver.Endpoint{
		newTestEndpoint("10.0.0.1:8080", "host-a"),
		newTestEndpoint("10.0.0.2:8080", "host-b"),
	})
	lbConfig := &autosharding.LBConfig{
		ChannelFactoryKey:  "test-factory-key",
		AutoShardingTarget: "test-target",
		KeyHeaderName:      "test-header-name",
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  state,
		BalancerConfig: lbConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() failed: %v", err)
	}
	_ = waitForPicker(ctx, t, cc) // Wait for the initial picker to be created.

	// Inject a valid assignment.
	// ["", "m") -> host-a, ["m", inf) -> host-b.
	//
	// And verify that the LB policy is still reporting Idle state.
	var tac *testAutoshardingClient
	select {
	case tac = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for creation of autosharding client")
	}
	tac.onAssignmentUpdate(&sharding.Assignment{
		EndpointNames: []string{"host-a", "host-b"},
		Slices: []sharding.Slice{
			{StartKey: []byte(""), Endpoints: []int{0}},
			{StartKey: []byte("m"), Endpoints: []int{1}},
		},
		Generation: 1,
	})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Idle); err != nil {
		t.Fatalf("WaitForConnectivityState(Idle) failed: %v", err)
	}

	// The first call to Pick must get queued and should result in a connection
	// attempt to the backend A, moving the channel to Connecting.
	picker := waitForPicker(ctx, t, cc)
	if _, err := picker.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick() error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	scA := waitForSubConn(ctx, t, cc)
	if want, got := "10.0.0.1:8080", scA.Addresses[0].Addr; got != want {
		t.Fatalf("SubConn created with addr %q, want %q", got, want)
	}
	select {
	case <-scA.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for SubConn.Connect()")
	}
	if err := cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("WaitForConnectivityState(Connecting) failed: %v", err)
	}

	// Move subconnA to Ready and verify that the channel moves to Ready.
	scA.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	scA.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
		t.Fatalf("WaitForConnectivityState(Ready) failed: %v", err)
	}

	// Grab the ready picker and make the next Pick that should get routed to
	// backend B, which should trigger a connection attempt to backend B.
	picker = waitForPicker(ctx, t, cc)
	if _, err := picker.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "m")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick() error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	scB := waitForSubConn(ctx, t, cc)
	if want, got := "10.0.0.2:8080", scB.Addresses[0].Addr; got != want {
		t.Fatalf("SubConn created with addr %q, want %q", got, want)
	}
	select {
	case <-scB.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for SubConn.Connect()")
	}

	// Move subconnB to Ready.
	scB.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	scB.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Verify that the picker routes keys to the expected backends.
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		resA, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")})
		if err != nil || resA.SubConn != scA {
			return fmt.Errorf("Pick(\"a\") = (%v, %v), want SubConn %v", resA, err, scA)
		}
		resZ, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "z")})
		if err != nil || resZ.SubConn != scB {
			return fmt.Errorf("Pick(\"z\") = (%v, %v), want SubConn %v", resZ, err, scB)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Change autosharding_target causing a new client (client2) to be created.
	lbConfig.AutoShardingTarget = "new-target"
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  state,
		BalancerConfig: lbConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() failed: %v", err)
	}
	var tac2 *testAutoshardingClient
	select {
	case tac2 = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for new autosharding client to be created")
	}

	// Verify that the picker continues routing keys to the expected backends
	// from the previous assignment.
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		resA, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")})
		if err != nil || resA.SubConn != scA {
			return fmt.Errorf("Pick(\"a\") = (%v, %v), want SubConn %v", resA, err, scA)
		}
		resZ, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "z")})
		if err != nil || resZ.SubConn != scB {
			return fmt.Errorf("Pick(\"z\") = (%v, %v), want SubConn %v", resZ, err, scB)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Inject a new assignment swapping the ranges:
	// ["", "m") -> host-b and ["m", inf) -> host-a
	tac2.onAssignmentUpdate(&sharding.Assignment{
		EndpointNames: []string{"host-a", "host-b"},
		Slices: []sharding.Slice{
			{StartKey: []byte(""), Endpoints: []int{1}},
			{StartKey: []byte("m"), Endpoints: []int{0}},
		},
		Generation: 2,
	})
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		resA, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")})
		if err != nil || resA.SubConn != scB {
			return fmt.Errorf("Pick(\"a\") = (%v, %v), want SubConn %v", resA, err, scB)
		}
		resZ, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "z")})
		if err != nil || resZ.SubConn != scA {
			return fmt.Errorf("Pick(\"z\") = (%v, %v), want SubConn %v", resZ, err, scA)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Tests that when the autosharding client reports a valid assignment, the
// balancer creates the expected SubConns when RPCs are made and routes RPCs to
// the expected backends. When fallback is enabled and all endpoints in a slice
// move to TF, the test verifies that the balancer uses the fallback pool for
// RPCs matching that slice.
func (s) TestAutoshardingClient_ValidAssignment_PerSliceFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	// Initial state with two endpoints and a valid LBConfig.
	state := resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), []resolver.Endpoint{
		newTestEndpoint("10.0.0.1:8080", "host-a"),
		newTestEndpoint("10.0.0.2:8080", "host-b"),
	})
	lbConfig := &autosharding.LBConfig{
		ChannelFactoryKey:  "test-factory-key",
		AutoShardingTarget: "test-target",
		KeyHeaderName:      "test-header-name",
		EnableFallback:     true,
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  state,
		BalancerConfig: lbConfig,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() failed: %v", err)
	}
	_ = waitForPicker(ctx, t, cc) // Wait for the initial picker to be created.

	// Inject a valid assignment.
	// ["", "m") -> host-a, ["m", inf) -> host-b.
	//
	// And verify that the LB policy is still reporting Idle state.
	var tac *testAutoshardingClient
	select {
	case tac = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for creation of autosharding client")
	}
	tac.onAssignmentUpdate(&sharding.Assignment{
		EndpointNames: []string{"host-a", "host-b"},
		Slices: []sharding.Slice{
			{StartKey: []byte(""), Endpoints: []int{0}},
			{StartKey: []byte("m"), Endpoints: []int{1}},
		},
		Generation: 1,
	})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Idle); err != nil {
		t.Fatalf("WaitForConnectivityState(Idle) failed: %v", err)
	}

	// The first call to Pick must get queued and should result in a connection
	// attempt to the backend A, moving the channel to Connecting.
	picker := waitForPicker(ctx, t, cc)
	if _, err := picker.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick() error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	scA := waitForSubConn(ctx, t, cc)
	if want, got := "10.0.0.1:8080", scA.Addresses[0].Addr; got != want {
		t.Fatalf("SubConn created with addr %q, want %q", got, want)
	}
	select {
	case <-scA.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for SubConn.Connect()")
	}
	if err := cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("WaitForConnectivityState(Connecting) failed: %v", err)
	}

	// Move subconnA to Ready and verify that the channel moves to Ready.
	scA.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	scA.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
		t.Fatalf("WaitForConnectivityState(Ready) failed: %v", err)
	}

	// Grab the ready picker and make the next Pick that should get routed to
	// backend B, which should trigger a connection attempt to backend B.
	picker = waitForPicker(ctx, t, cc)
	if _, err := picker.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "m")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick() error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	scB := waitForSubConn(ctx, t, cc)
	if want, got := "10.0.0.2:8080", scB.Addresses[0].Addr; got != want {
		t.Fatalf("SubConn created with addr %q, want %q", got, want)
	}
	select {
	case <-scB.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for SubConn.Connect()")
	}

	// Move subconnB to Ready.
	scB.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	scB.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Verify that the picker routes keys to the expected backends.
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		resA, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")})
		if err != nil || resA.SubConn != scA {
			return fmt.Errorf("Pick(\"a\") = (%v, %v), want SubConn %v", resA, err, scA)
		}
		resM, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "m")})
		if err != nil || resM.SubConn != scB {
			return fmt.Errorf("Pick(\"m\") = (%v, %v), want SubConn %v", resM, err, scB)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Move subconnB to TransientFailure and verify that the picker routes keys in the
	// ["m", inf) slice to the fallback pool.
	scB.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	// Verify that the picker routes keys to the expected backends.
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		resA, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")})
		if err != nil || resA.SubConn != scA {
			return fmt.Errorf("Pick(\"a\") = (%v, %v), want SubConn %v", resA, err, scA)
		}
		resM, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "m")})
		if err != nil || resM.SubConn != scA {
			return fmt.Errorf("Pick(\"m\") = (%v, %v), want SubConn %v", resM, err, scA)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Tests that when the name resolver reorders or removes endpoints while an
// assignment is active, the slice map is regenerated so keys continue routing
// to the correct endpoints by hostname.
func (s) TestDynamicEndpointUpdates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	epA := newTestEndpoint("10.0.0.1:8080", "host-a")
	epB := newTestEndpoint("10.0.0.2:8080", "host-b")
	cfg := &autosharding.LBConfig{
		ChannelFactoryKey:  "test-factory-key",
		AutoShardingTarget: "test-target",
		KeyHeaderName:      "test-header-name",
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), []resolver.Endpoint{epA, epB}),
		BalancerConfig: cfg,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() unexpected error: %v", err)
	}
	_ = waitForPicker(ctx, t, cc) // Wait for the initial picker to be created.

	// Inject a valid assignment.
	// ["", "m") -> host-a, ["m", inf) -> host-b.
	//
	// And verify that the LB policy is still reporting Idle state.
	var tac *testAutoshardingClient
	select {
	case tac = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for creation of autosharding client")
	}
	tac.onAssignmentUpdate(&sharding.Assignment{
		EndpointNames: []string{"host-a", "host-b"},
		Slices: []sharding.Slice{
			{StartKey: []byte(""), Endpoints: []int{0}},
			{StartKey: []byte("m"), Endpoints: []int{1}},
		},
		Generation: 1,
	})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Idle); err != nil {
		t.Fatalf("WaitForConnectivityState(Idle) failed: %v", err)
	}

	// The first call to Pick must get queued and should result in a connection
	// attempt to the backend A, moving the channel to Connecting.
	picker := waitForPicker(ctx, t, cc)
	if _, err := picker.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick() error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	scA := waitForSubConn(ctx, t, cc)
	if want, got := "10.0.0.1:8080", scA.Addresses[0].Addr; got != want {
		t.Fatalf("SubConn created with addr %q, want %q", got, want)
	}
	select {
	case <-scA.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for SubConn.Connect()")
	}
	if err := cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("WaitForConnectivityState(Connecting) failed: %v", err)
	}

	// Move subconnA to Ready and verify that the channel moves to Ready.
	scA.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	scA.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
		t.Fatalf("WaitForConnectivityState(Ready) failed: %v", err)
	}

	// Grab the ready picker and make the next Pick that should get routed to
	// backend B, which should trigger a connection attempt to backend B.
	picker = waitForPicker(ctx, t, cc)
	if _, err := picker.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "m")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick() error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	scB := waitForSubConn(ctx, t, cc)
	if want, got := "10.0.0.2:8080", scB.Addresses[0].Addr; got != want {
		t.Fatalf("SubConn created with addr %q, want %q", got, want)
	}
	select {
	case <-scB.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for SubConn.Connect()")
	}

	// Move subconnB to Ready.
	scB.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	scB.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})

	// Verify that the picker routes keys to the expected backends.
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		resA, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")})
		if err != nil || resA.SubConn != scA {
			return fmt.Errorf("Pick(\"a\") = (%v, %v), want SubConn %v", resA, err, scA)
		}
		resZ, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "z")})
		if err != nil || resZ.SubConn != scB {
			return fmt.Errorf("Pick(\"z\") = (%v, %v), want SubConn %v", resZ, err, scB)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Reorder endpoints in resolver update: [epB, epA]. Because the endpoint
	// indices swap (host-b is now 0, host-a is now 1), the balancer must
	// regenerate the sliceMap so "a" still routes to scA and "z" still routes
	// to scB.
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), []resolver.Endpoint{epB, epA}),
		BalancerConfig: cfg,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() with reordered endpoints failed: %v", err)
	}
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		resA, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")})
		if err != nil || resA.SubConn != scA {
			return fmt.Errorf("after reorder, Pick(\"a\") = (%v, %v), want SubConn %v", resA, err, scA)
		}
		resZ, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "z")})
		if err != nil || resZ.SubConn != scB {
			return fmt.Errorf("after reorder, Pick(\"z\") = (%v, %v), want SubConn %v", resZ, err, scB)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Remove epB from resolver endpoints. Slice ["m", inf) now has no valid
	// endpoints in endpointMap, so picking "z" with fallback disabled must fail
	// with "matching slice has no available endpoints".
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), []resolver.Endpoint{epA}),
		BalancerConfig: cfg,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() with removed endpoint failed: %v", err)
	}
	if err := cc.WaitForPicker(ctx, func(p balancer.Picker) error {
		resA, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")})
		if err != nil || resA.SubConn != scA {
			return fmt.Errorf("after removing host-b, Pick(\"a\") = (%v, %v), want SubConn %v", resA, err, scA)
		}
		_, err = p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "z")})
		const wantErr = "autosharding: matching slice has no available endpoints"
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			return fmt.Errorf("after removing host-b, Pick(\"z\") error = %v, want error containing %q", err, wantErr)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Tests the connectivity state aggregation rules and automatic connection
// triggering on IDLE endpoints when the aggregated state is CONNECTING or
// TRANSIENT_FAILURE and no endpoint is currently in CONNECTING.
func (s) TestAggregatedConnectivityStateAndAutoConnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testAutoshardingClientCh := overrideNewAutoshardingClientForTesting(t)
	cc := testutils.NewBalancerClientConn(t)
	b := balancer.Get(autosharding.Name).Build(cc, balancer.BuildOptions{})
	defer b.Close()

	ep1 := newTestEndpoint("10.0.0.1:8080", "host-1")
	ep2 := newTestEndpoint("10.0.0.2:8080", "host-2")
	ep3 := newTestEndpoint("10.0.0.3:8080", "host-3")
	ep4 := newTestEndpoint("10.0.0.4:8080", "host-4")
	cfg := &autosharding.LBConfig{
		ChannelFactoryKey:  "test-factory-key",
		AutoShardingTarget: "test-target",
		KeyHeaderName:      "test-header-name",
	}
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), []resolver.Endpoint{ep1, ep2, ep3, ep4}),
		BalancerConfig: cfg,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() unexpected error: %v", err)
	}
	_ = waitForPicker(ctx, t, cc) // Wait for the initial picker to be created.

	// Inject an assignment so the balancer computes aggregated connectivity
	// state from its child endpoints.
	var tac *testAutoshardingClient
	select {
	case tac = <-testAutoshardingClientCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for creation of autosharding client")
	}
	tac.onAssignmentUpdate(&sharding.Assignment{
		EndpointNames: []string{"host-1", "host-2", "host-3", "host-4"},
		Slices: []sharding.Slice{
			{StartKey: []byte(""), Endpoints: []int{0}},
			{StartKey: []byte("m"), Endpoints: []int{1}},
			{StartKey: []byte("t"), Endpoints: []int{2}},
			{StartKey: []byte("x"), Endpoints: []int{3}},
		},
		Generation: 1,
	})

	// All 4 endpoints are in IDLE, therefore aggregated state must be IDLE.
	if err := cc.WaitForConnectivityState(ctx, connectivity.Idle); err != nil {
		t.Fatalf("WaitForConnectivityState(Idle) failed: %v", err)
	}
	p := waitForPicker(ctx, t, cc)

	// Trigger a connection on host-1 via Pick("a").
	if _, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "a")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick(\"a\") error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	sc1 := waitForSubConn(ctx, t, cc)
	select {
	case <-sc1.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for sc1.Connect()")
	}

	// Moving sc1 to CONNECTING should leave us with three endpoints in IDLE and
	// one in CONNECTING, therefore aggregated state must be CONNECTING.
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Connecting); err != nil {
		t.Fatalf("WaitForConnectivityState(Connecting) failed: %v", err)
	}

	// Verify that no more subconns are created while an endpoint is already in
	// CONNECTING. This is because the balancer only automatically triggers
	// ExitIdle on an IDLE endpoint when the aggregated state is CONNECTING or
	// TF and no endpoint is currently in CONNECTING.
	select {
	case sc := <-cc.NewSubConnCh:
		t.Fatalf("Unexpected SubConn created while an endpoint is already CONNECTING: %v", sc)
	case <-time.After(defaultTestShortTimeout):
	}

	// Moving sc1 to TF should leave us with one endpoint in TF and three in IDLE.
	// Because 1 endpoint is in TF and len(endpoints) > 1, aggregated state must
	// be CONNECTING. And because no endpoint is in CONNECTING, the balancer
	// must automatically trigger ExitIdle on the lowest-index IDLE endpoint
	// (host-2, "10.0.0.2:8080").
	sc1.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	sc2 := waitForSubConn(ctx, t, cc)
	if got, want := sc2.Addresses[0].Addr, "10.0.0.2:8080"; got != want {
		t.Fatalf("Auto-ExitIdle created SubConn for %q, want lowest-index IDLE endpoint %q", got, want)
	}
	select {
	case <-sc2.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for sc2.Connect()")
	}

	// Moving sc2 to CONNECTING then TF should leave us with two endpoints in TF
	// and two in IDLE, and this should result in the aggregated state being TF.
	// And because no endpoint is in CONNECTING, the balancer automatically
	// triggers ExitIdle on the lowest-index IDLE endpoint (host-3,
	// "10.0.0.3:8080").
	sc2.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc2.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.TransientFailure})
	if err := cc.WaitForConnectivityState(ctx, connectivity.TransientFailure); err != nil {
		t.Fatalf("WaitForConnectivityState(TransientFailure) with 2 TF endpoints failed: %v", err)
	}
	sc3 := waitForSubConn(ctx, t, cc)
	if got, want := sc3.Addresses[0].Addr, "10.0.0.3:8080"; got != want {
		t.Fatalf("Auto-ExitIdle created SubConn for %q, want %q", got, want)
	}
	select {
	case <-sc3.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for sc3.Connect()")
	}

	// Moving sc3 to CONNECTING should leave us with two endpoints in TF, one
	// endpoint in CONNECTING and one in IDLE which should result in the
	// aggregated state being TF. And this should not trigger any more subconns
	// to be created because an endpoint is already in CONNECTING.
	sc3.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	if err := cc.WaitForConnectivityState(ctx, connectivity.TransientFailure); err != nil {
		t.Fatalf("WaitForConnectivityState(TransientFailure) with 2 TF endpoints failed: %v", err)
	}
	select {
	case sc := <-cc.NewSubConnCh:
		t.Fatalf("Unexpected SubConn created while an endpoint is already CONNECTING: %v", sc)
	case <-time.After(defaultTestShortTimeout):
	}

	// Trigger a connection on host-4 via Pick("x").
	if _, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "x")}); !errors.Is(err, balancer.ErrNoSubConnAvailable) {
		t.Fatalf("Pick(\"x\") error = %v, want %v", err, balancer.ErrNoSubConnAvailable)
	}
	sc4 := waitForSubConn(ctx, t, cc)
	select {
	case <-sc4.ConnectCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for sc4.Connect()")
	}

	// Moving sc4 to CONNECTING and READY should leave us with one endpoint in
	// READY and therefore aggregated state must be READY.
	sc4.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Connecting})
	sc4.UpdateState(balancer.SubConnState{ConnectivityState: connectivity.Ready})
	if err := cc.WaitForConnectivityState(ctx, connectivity.Ready); err != nil {
		t.Fatalf("WaitForConnectivityState(Ready) failed: %v", err)
	}

	// Update resolver to only have ep1 (which is in TRANSIENT_FAILURE). Having
	// a single endpoint and it being in TF should result in the aggregated
	// state being TF.
	if err := b.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  resolverStateWithProviderAndEndpoints(defaultTestClientConnProvider(), []resolver.Endpoint{ep1}),
		BalancerConfig: cfg,
	}); err != nil {
		t.Fatalf("UpdateClientConnState() with single TF endpoint failed: %v", err)
	}
	if err := cc.WaitForConnectivityState(ctx, connectivity.TransientFailure); err != nil {
		t.Fatalf("WaitForConnectivityState(TransientFailure) with single TF endpoint failed: %v", err)
	}
}

// newTestEndpoint returns a resolver.Endpoint with the given address and
// hostname attribute (if non-empty).
func newTestEndpoint(addr, host string) resolver.Endpoint {
	ep := resolver.Endpoint{Addresses: []resolver.Address{{Addr: addr}}}
	if host != "" {
		ep = hostname.Set(ep, host)
	}
	return ep
}

// testClientConn is a test implementation of grpc.ClientConnInterface that
// records the key passed to the provider and signals when Close() is called.
type testClientConn struct {
	grpc.ClientConnInterface
	key         string
	closeCalled *grpcsync.Event
}

// testClientConnProvider returns a provider function that creates a new
// testClientConn for each key and sends it on a channel. The channel is also
// returned so that the caller can receive the created testClientConn.
func testClientConnProvider() (func(string) (grpc.ClientConnInterface, func(), error), chan *testClientConn) {
	tccCh := make(chan *testClientConn, 1)
	provider := func(key string) (grpc.ClientConnInterface, func(), error) {
		tcc := &testClientConn{
			key:         key,
			closeCalled: grpcsync.NewEvent(),
		}
		tccCh <- tcc
		return tcc, func() { tcc.closeCalled.Fire() }, nil
	}
	return provider, tccCh
}

// defaultTestClientConnProvider returns a provider function that creates a new
// testClientConn for each key.
//
// Use testClientConnProvider() if the test needs to capture the created
// testClientConn.
func defaultTestClientConnProvider() func(string) (grpc.ClientConnInterface, func(), error) {
	return func(key string) (grpc.ClientConnInterface, func(), error) {
		tcc := &testClientConn{
			key:         key,
			closeCalled: grpcsync.NewEvent(),
		}
		return tcc, func() { tcc.closeCalled.Fire() }, nil
	}
}

// resolverStateWithProviderAndEndpoints returns a resolver.State with the given
// provider (set in attributes) and endpoints.
func resolverStateWithProviderAndEndpoints(provider func(string) (grpc.ClientConnInterface, func(), error), endpoints []resolver.Endpoint) resolver.State {
	return grpc.SetClientConnProvider(resolver.State{Endpoints: endpoints}, provider)
}

// testAutoshardingClient is a test implementation of sharding.Client that
// records the ClientOptions passed to it and signals when Close() is called. It
// also allows the test to inject assignment updates and errors.
type testAutoshardingClient struct {
	opts        sharding.ClientOptions
	closeCalled *grpcsync.Event
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
	c.closeCalled.Fire()
}

// overrideNewAutoshardingClientForTesting overrides the
// internal.NewAutoshardingClient function to return a testAutoshardingClient
// and returns a channel that will receive the created testAutoshardingClient.
// The override is reverted when the test ends.
func overrideNewAutoshardingClientForTesting(t *testing.T) chan *testAutoshardingClient {
	t.Helper()

	ch := make(chan *testAutoshardingClient, 1)
	orig := internal.NewAutoshardingClient
	t.Cleanup(func() { internal.NewAutoshardingClient = orig })
	internal.NewAutoshardingClient = func(opts sharding.ClientOptions) func() {
		client := &testAutoshardingClient{
			opts:        opts,
			closeCalled: grpcsync.NewEvent(),
		}
		ch <- client
		return client.close
	}
	return ch
}

// waitForPicker waits for a new picker to be created and returns it. Aborts the
// test if no picker is created before the context deadline.
func waitForPicker(ctx context.Context, t *testing.T, cc *testutils.BalancerClientConn) balancer.Picker {
	t.Helper()
	select {
	case p := <-cc.NewPickerCh:
		return p
	case <-ctx.Done():
		t.Fatal("Timeout waiting for picker")
		return nil
	}
}

// waitForSubConn waits for a new SubConn to be created and returns it. Aborts
// the test if no SubConn is created before the context deadline.
func waitForSubConn(ctx context.Context, t *testing.T, cc *testutils.BalancerClientConn) *testutils.TestSubConn {
	t.Helper()
	select {
	case sc := <-cc.NewSubConnCh:
		return sc
	case <-ctx.Done():
		t.Fatal("Timeout waiting for subconn")
		return nil
	}
}

// newContextWithShardingKey returns a new context with the given key and value
// added to the outgoing metadata.
func newContextWithShardingKey(ctx context.Context, key, value string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, key, value)
}

// checkPicksOK makes num Picks. Returns a map of backend addresses as keys and
// number of RPCs sent to it as value. Aborts the test if any pick fails.
func checkPicksOK(ctx context.Context, t *testing.T, p balancer.Picker, num int) map[string]int {
	t.Helper()
	backendCount := make(map[string]int)
	for i := 0; i < num; i++ {
		res, err := p.Pick(balancer.PickInfo{Ctx: newContextWithShardingKey(ctx, "test-header-name", "doesn't-matter")})
		if err != nil {
			t.Fatalf("Pick() (%d/%d) failed: %v", i, num, err)
		}
		backendCount[res.SubConn.(*testutils.TestSubConn).Addresses[0].Addr]++
	}
	return backendCount
}

// computeIdealNumberOfRPCs computes the ideal number of RPCs to send so that
// we can observe an event happening with probability p, and the result will
// have value p with the given error tolerance.
//
// See https://github.com/grpc/grpc/blob/4f6e13bdda9e8c26d6027af97db4b368ca2b3069/test/cpp/end2end/xds/xds_end2end_test_lib.h#L941
// for an explanation of the formula.
func computeIdealNumberOfRPCs(t *testing.T, p, errorTolerance float64) int {
	t.Helper()
	if p < 0 || p > 1 {
		t.Fatal("p must be in (0, 1)")
	}
	numRPCs := math.Ceil(p * (1 - p) * 5. * 5. / errorTolerance / errorTolerance)
	return int(numRPCs + 1000.) // add 1k as a buffer to avoid flakiness.
}
