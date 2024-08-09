/*
 *
 * Copyright 2024 gRPC authors.
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
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	pickfirstleaf "google.golang.org/grpc/balancer/pickfirst_leaf"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils/pickfirst"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

var stateStoringServiceConfig = fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, stateStoringBalancerName)

const stateStoringBalancerName = "state_storing"

// setupPickFirstLeaf performs steps required for pick_first tests. It starts a
// bunch of backends exporting the TestService, creates a ClientConn to them
// with service config specifying the use of the pick_first LB policy.
func setupPickFirstLeaf(t *testing.T, backendCount int, opts ...grpc.DialOption) (*grpc.ClientConn, *manual.Resolver, []*stubserver.StubServer) {
	t.Helper()

	r := manual.NewBuilderWithScheme("whatever")

	backends := make([]*stubserver.StubServer, backendCount)
	addrs := make([]resolver.Address, backendCount)
	for i := 0; i < backendCount; i++ {
		backend := &stubserver.StubServer{
			EmptyCallF: func(_ context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
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
		grpc.WithDefaultServiceConfig(stateStoringServiceConfig),
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

// TestPickFirstLeaf_ResolverUpdate tests the behaviour of the new pick first
// policy when servers are brought down and resolver updates are received.
func (s) TestPickFirstLeaf_ResolverUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	balChan := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancerChan: balChan})
	tests := []struct {
		name                      string
		backendCount              int
		initialBackendIndexes     []int
		initialTargetBackendIndex int
		wantScStates              []connectivity.State
		updatedBackendIndexes     []int
		updatedTargetBackendIndex int
		wantScStatesPostUpdate    []connectivity.State
		restartConnected          bool
	}{
		{
			name:                      "two_server_first_ready",
			backendCount:              2,
			initialBackendIndexes:     []int{0, 1},
			initialTargetBackendIndex: 0,
			wantScStates:              []connectivity.State{connectivity.Ready},
		},
		{
			name:                      "two_server_second_ready",
			backendCount:              2,
			initialBackendIndexes:     []int{0, 1},
			initialTargetBackendIndex: 1,
			wantScStates:              []connectivity.State{connectivity.Shutdown, connectivity.Ready},
		},
		{
			name:                      "duplicate_address",
			backendCount:              2,
			initialBackendIndexes:     []int{0, 0, 1},
			initialTargetBackendIndex: 1,
			wantScStates:              []connectivity.State{connectivity.Shutdown, connectivity.Ready},
		},
		{
			name:                      "disjoint_updated_addresses",
			backendCount:              4,
			initialBackendIndexes:     []int{0, 1},
			initialTargetBackendIndex: 1,
			wantScStates:              []connectivity.State{connectivity.Shutdown, connectivity.Ready},
			updatedBackendIndexes:     []int{2, 3},
			updatedTargetBackendIndex: 3,
			wantScStatesPostUpdate:    []connectivity.State{connectivity.Shutdown, connectivity.Shutdown, connectivity.Shutdown, connectivity.Ready},
		},
		{
			name:                      "active_backend_in_updated_list",
			backendCount:              3,
			initialBackendIndexes:     []int{0, 1},
			initialTargetBackendIndex: 1,
			wantScStates:              []connectivity.State{connectivity.Shutdown, connectivity.Ready},
			updatedBackendIndexes:     []int{1, 2},
			updatedTargetBackendIndex: 1,
			wantScStatesPostUpdate:    []connectivity.State{connectivity.Shutdown, connectivity.Ready},
		},
		{
			name:                      "inactive_backend_in_updated_list",
			backendCount:              3,
			initialBackendIndexes:     []int{0, 1},
			initialTargetBackendIndex: 1,
			wantScStates:              []connectivity.State{connectivity.Shutdown, connectivity.Ready},
			updatedBackendIndexes:     []int{0, 2},
			updatedTargetBackendIndex: 0,
			wantScStatesPostUpdate:    []connectivity.State{connectivity.Shutdown, connectivity.Shutdown, connectivity.Ready},
		},
		{
			name:                      "identical_list",
			backendCount:              2,
			initialBackendIndexes:     []int{0, 1},
			initialTargetBackendIndex: 1,
			wantScStates:              []connectivity.State{connectivity.Shutdown, connectivity.Ready},
			updatedBackendIndexes:     []int{0, 1},
			updatedTargetBackendIndex: 1,
			wantScStatesPostUpdate:    []connectivity.State{connectivity.Shutdown, connectivity.Ready},
		},
		{
			name:                      "first_connected_idle_reconnect",
			backendCount:              2,
			initialBackendIndexes:     []int{0, 1},
			initialTargetBackendIndex: 0,
			restartConnected:          true,
			wantScStates:              []connectivity.State{connectivity.Ready},
			updatedBackendIndexes:     []int{0, 1},
			updatedTargetBackendIndex: 0,
			wantScStatesPostUpdate:    []connectivity.State{connectivity.Ready},
		},
		{
			name:                      "second_connected_idle_reconnect",
			backendCount:              2,
			initialBackendIndexes:     []int{0, 1},
			initialTargetBackendIndex: 1,
			restartConnected:          true,
			wantScStates:              []connectivity.State{connectivity.Shutdown, connectivity.Ready},
			updatedBackendIndexes:     []int{0, 1},
			updatedTargetBackendIndex: 1,
			wantScStatesPostUpdate:    []connectivity.State{connectivity.Shutdown, connectivity.Ready, connectivity.Shutdown},
		},
		{
			name:                      "second_connected_idle_reconnect_first",
			backendCount:              2,
			initialBackendIndexes:     []int{0, 1},
			initialTargetBackendIndex: 1,
			restartConnected:          true,
			wantScStates:              []connectivity.State{connectivity.Shutdown, connectivity.Ready},
			updatedBackendIndexes:     []int{0, 1},
			updatedTargetBackendIndex: 0,
			wantScStatesPostUpdate:    []connectivity.State{connectivity.Shutdown, connectivity.Shutdown, connectivity.Ready},
		},
		{
			name:                      "first_connected_idle_reconnect_second",
			backendCount:              2,
			initialBackendIndexes:     []int{0, 1},
			initialTargetBackendIndex: 0,
			restartConnected:          true,
			wantScStates:              []connectivity.State{connectivity.Ready},
			updatedBackendIndexes:     []int{0, 1},
			updatedTargetBackendIndex: 1,
			wantScStatesPostUpdate:    []connectivity.State{connectivity.Shutdown, connectivity.Ready},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cc, r, backends := setupPickFirstLeaf(t, tc.backendCount)

			activeBackends := []*stubserver.StubServer{}
			for _, idx := range tc.initialBackendIndexes {
				activeBackends = append(activeBackends, backends[idx])
			}
			addrs := stubBackendsToResolverAddrs(activeBackends)
			r.UpdateState(resolver.State{Addresses: addrs})

			// shutdown all active backends except the target.
			var targetAddr resolver.Address
			for idxI, idx := range tc.initialBackendIndexes {
				if idx == tc.initialTargetBackendIndex {
					targetAddr = addrs[idxI]
					continue
				}
				backends[idx].S.Stop()
			}

			if err := pickfirst.CheckRPCsToBackend(ctx, cc, targetAddr); err != nil {
				t.Fatal(err)
			}
			bal := <-balChan
			scs := bal.subConns()

			if got, want := len(scs), len(tc.wantScStates); got != want {
				t.Fatalf("len(subconns) = %d, want %d", got, want)
			}

			for idx := range scs {
				if got, want := scs[idx].state, tc.wantScStates[idx]; got != want {
					t.Errorf("subconn[%d].state = %v, want = %v", idx, got, want)
				}
			}

			if len(tc.updatedBackendIndexes) == 0 {
				return
			}

			// Restart all the backends.
			for i, s := range backends {
				if !tc.restartConnected && i == tc.initialTargetBackendIndex {
					continue
				}
				s.S.Stop()
				if err := s.StartServer(); err != nil {
					t.Fatalf("Failed to re-start test backend: %v", err)
				}
			}

			activeBackends = []*stubserver.StubServer{}
			for _, idx := range tc.updatedBackendIndexes {
				activeBackends = append(activeBackends, backends[idx])
			}
			addrs = stubBackendsToResolverAddrs(activeBackends)
			r.UpdateState(resolver.State{Addresses: addrs})

			// shutdown all active backends except the target.
			for idxI, idx := range tc.updatedBackendIndexes {
				if idx == tc.updatedTargetBackendIndex {
					targetAddr = addrs[idxI]
					continue
				}
				backends[idx].S.Stop()
			}

			if err := pickfirst.CheckRPCsToBackend(ctx, cc, targetAddr); err != nil {
				t.Fatal(err)
			}
			scs = bal.subConns()

			if got, want := len(scs), len(tc.wantScStatesPostUpdate); got != want {
				t.Fatalf("len(subconns) = %d, want %d", got, want)
			}

			for idx := range scs {
				if got, want := scs[idx].state, tc.wantScStatesPostUpdate[idx]; got != want {
					t.Errorf("subconn[%d].state = %v, want = %v", idx, got, want)
				}
			}

		})
	}
}

// stateStoringBalancer stores the state of the subconns being created.
type stateStoringBalancer struct {
	balancer.Balancer
	mu       sync.Mutex
	scStates []*scState
}

func (b *stateStoringBalancer) Close() {
	b.Balancer.Close()
}

func (b *stateStoringBalancer) ExitIdle() {
	if ib, ok := b.Balancer.(balancer.ExitIdler); ok {
		ib.ExitIdle()
	}
}

type stateStoringBalancerBuilder struct {
	balancerChan chan *stateStoringBalancer
}

func (b *stateStoringBalancerBuilder) Name() string {
	return stateStoringBalancerName
}

func (b *stateStoringBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	bal := &stateStoringBalancer{}
	bal.Balancer = balancer.Get(pickfirstleaf.PickFirstLeafName).Build(&stateStoringCCWrapper{cc, bal}, opts)
	b.balancerChan <- bal
	return bal
}

func (b *stateStoringBalancer) subConns() []scState {
	b.mu.Lock()
	defer b.mu.Unlock()
	ret := []scState{}
	for _, s := range b.scStates {
		ret = append(ret, *s)
	}
	return ret
}

func (b *stateStoringBalancer) addScState(state *scState) {
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
		state: connectivity.Idle,
		addrs: addrs,
	}
	ccw.b.addScState(scs)
	opts.StateListener = func(s balancer.SubConnState) {
		ccw.b.mu.Lock()
		scs.state = s.ConnectivityState
		ccw.b.mu.Unlock()
		oldListener(s)
	}
	return ccw.ClientConn.NewSubConn(addrs, opts)
}

type scState struct {
	state connectivity.State
	addrs []resolver.Address
}
