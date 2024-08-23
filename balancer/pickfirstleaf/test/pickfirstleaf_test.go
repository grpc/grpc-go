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
	"time"

	"github.com/google/go-cmp/cmp"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/pickfirstleaf"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils/pickfirst"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

const (
	// Default timeout for tests in this package.
	defaultTestTimeout = 10 * time.Second
	// Default short timeout, to be used when waiting for events which are not
	// expected to happen.
	defaultTestShortTimeout  = 100 * time.Millisecond
	stateStoringBalancerName = "state_storing"
)

var stateStoringServiceConfig = fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, stateStoringBalancerName)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

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

type scStateExpectation struct {
	State     connectivity.State
	ServerIdx int
}

func scStateExpectationToScState(in []scStateExpectation, serverAddrs []resolver.Address) []scState {
	out := []scState{}
	for _, exp := range in {
		out = append(out, scState{
			State: exp.State,
			Addrs: []resolver.Address{serverAddrs[exp.ServerIdx]},
		})
	}
	return out

}

// TestPickFirstLeaf_ResolverUpdate tests the behaviour of the new pick first
// policy when servers are brought down and resolver updates are received.
func (s) TestPickFirstLeaf_ResolverUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	balChan := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancerChan: balChan})
	tests := []struct {
		name                         string
		backendCount                 int
		preUpdateBackendIndexes      []int
		preUpdateTargetBackendIndex  int
		wantPreUpdateScStates        []scStateExpectation
		updatedBackendIndexes        []int
		postUpdateTargetBackendIndex int
		wantPostUpdateScStates       []scStateExpectation
		restartConnected             bool
		wantConnStateTransitions     []connectivity.State
	}{
		{
			name:                        "two_server_first_ready",
			backendCount:                2,
			preUpdateBackendIndexes:     []int{0, 1},
			preUpdateTargetBackendIndex: 0,
			wantPreUpdateScStates: []scStateExpectation{
				{State: connectivity.Ready, ServerIdx: 0},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
		{
			name:                        "two_server_second_ready",
			backendCount:                2,
			preUpdateBackendIndexes:     []int{0, 1},
			preUpdateTargetBackendIndex: 1,
			wantPreUpdateScStates: []scStateExpectation{
				{State: connectivity.Shutdown, ServerIdx: 0},
				{State: connectivity.Ready, ServerIdx: 1},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
		{
			name:                        "duplicate_address",
			backendCount:                2,
			preUpdateBackendIndexes:     []int{0, 0, 1},
			preUpdateTargetBackendIndex: 1,
			wantPreUpdateScStates: []scStateExpectation{
				{State: connectivity.Shutdown, ServerIdx: 0},
				{State: connectivity.Ready, ServerIdx: 1},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
		{
			name:                        "disjoint_updated_addresses",
			backendCount:                4,
			preUpdateBackendIndexes:     []int{0, 1},
			preUpdateTargetBackendIndex: 1,
			wantPreUpdateScStates: []scStateExpectation{
				{State: connectivity.Shutdown, ServerIdx: 0},
				{State: connectivity.Ready, ServerIdx: 1},
			},
			updatedBackendIndexes:        []int{2, 3},
			postUpdateTargetBackendIndex: 3,
			wantPostUpdateScStates: []scStateExpectation{
				{State: connectivity.Shutdown, ServerIdx: 0},
				{State: connectivity.Shutdown, ServerIdx: 1},
				{State: connectivity.Shutdown, ServerIdx: 2},
				{State: connectivity.Ready, ServerIdx: 3},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
		{
			name:                        "active_backend_in_updated_list",
			backendCount:                3,
			preUpdateBackendIndexes:     []int{0, 1},
			preUpdateTargetBackendIndex: 1,
			wantPreUpdateScStates: []scStateExpectation{
				{State: connectivity.Shutdown, ServerIdx: 0},
				{State: connectivity.Ready, ServerIdx: 1},
			},
			updatedBackendIndexes:        []int{1, 2},
			postUpdateTargetBackendIndex: 1,
			wantPostUpdateScStates: []scStateExpectation{
				{State: connectivity.Shutdown, ServerIdx: 0},
				{State: connectivity.Ready, ServerIdx: 1},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
		{
			name:                        "inactive_backend_in_updated_list",
			backendCount:                3,
			preUpdateBackendIndexes:     []int{0, 1},
			preUpdateTargetBackendIndex: 1,
			wantPreUpdateScStates: []scStateExpectation{
				{State: connectivity.Shutdown, ServerIdx: 0},
				{State: connectivity.Ready, ServerIdx: 1},
			},
			updatedBackendIndexes:        []int{0, 2},
			postUpdateTargetBackendIndex: 0,
			wantPostUpdateScStates: []scStateExpectation{
				{State: connectivity.Shutdown, ServerIdx: 0},
				{State: connectivity.Shutdown, ServerIdx: 1},
				{State: connectivity.Ready, ServerIdx: 0},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
		{
			name:                        "identical_list",
			backendCount:                2,
			preUpdateBackendIndexes:     []int{0, 1},
			preUpdateTargetBackendIndex: 1,
			wantPreUpdateScStates: []scStateExpectation{
				{State: connectivity.Shutdown, ServerIdx: 0},
				{State: connectivity.Ready, ServerIdx: 1},
			},
			updatedBackendIndexes:        []int{0, 1},
			postUpdateTargetBackendIndex: 1,
			wantPostUpdateScStates: []scStateExpectation{
				{State: connectivity.Shutdown, ServerIdx: 0},
				{State: connectivity.Ready, ServerIdx: 1},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
		{
			name:                        "first_connected_idle_reconnect",
			backendCount:                2,
			preUpdateBackendIndexes:     []int{0, 1},
			preUpdateTargetBackendIndex: 0,
			restartConnected:            true,
			wantPreUpdateScStates: []scStateExpectation{
				{State: connectivity.Ready, ServerIdx: 0},
			},
			updatedBackendIndexes:        []int{0, 1},
			postUpdateTargetBackendIndex: 0,
			wantPostUpdateScStates: []scStateExpectation{
				{State: connectivity.Ready, ServerIdx: 0},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
				connectivity.Idle,
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
		{
			name:                        "second_connected_idle_reconnect",
			backendCount:                2,
			preUpdateBackendIndexes:     []int{0, 1},
			preUpdateTargetBackendIndex: 1,
			restartConnected:            true,
			wantPreUpdateScStates: []scStateExpectation{
				{State: connectivity.Shutdown, ServerIdx: 0},
				{State: connectivity.Ready, ServerIdx: 1},
			},
			updatedBackendIndexes:        []int{0, 1},
			postUpdateTargetBackendIndex: 1,
			wantPostUpdateScStates: []scStateExpectation{
				{State: connectivity.Shutdown, ServerIdx: 0},
				{State: connectivity.Ready, ServerIdx: 1},
				{State: connectivity.Shutdown, ServerIdx: 0},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
				connectivity.Idle,
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
		{
			name:                        "second_connected_idle_reconnect_first",
			backendCount:                2,
			preUpdateBackendIndexes:     []int{0, 1},
			preUpdateTargetBackendIndex: 1,
			restartConnected:            true,
			wantPreUpdateScStates: []scStateExpectation{
				{State: connectivity.Shutdown, ServerIdx: 0},
				{State: connectivity.Ready, ServerIdx: 1},
			},
			updatedBackendIndexes:        []int{0, 1},
			postUpdateTargetBackendIndex: 0,
			wantPostUpdateScStates: []scStateExpectation{
				{State: connectivity.Shutdown, ServerIdx: 0},
				{State: connectivity.Shutdown, ServerIdx: 1},
				{State: connectivity.Ready, ServerIdx: 0},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
				connectivity.Idle,
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
		{
			name:                        "first_connected_idle_reconnect_second",
			backendCount:                2,
			preUpdateBackendIndexes:     []int{0, 1},
			preUpdateTargetBackendIndex: 0,
			restartConnected:            true,
			wantPreUpdateScStates: []scStateExpectation{
				{State: connectivity.Ready, ServerIdx: 0},
			},
			updatedBackendIndexes:        []int{0, 1},
			postUpdateTargetBackendIndex: 1,
			wantPostUpdateScStates: []scStateExpectation{
				{State: connectivity.Shutdown, ServerIdx: 0},
				{State: connectivity.Ready, ServerIdx: 1},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
				connectivity.Idle,
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cc, r, backends := setupPickFirstLeaf(t, tc.backendCount)
			addrs := stubBackendsToResolverAddrs(backends)

			activeAddrs := []resolver.Address{}
			for _, idx := range tc.preUpdateBackendIndexes {
				activeAddrs = append(activeAddrs, addrs[idx])
			}
			r.UpdateState(resolver.State{Addresses: activeAddrs})
			bal := <-balChan
			select {
			case <-bal.resolverUpdateSeen:
			case <-ctx.Done():
				t.Fatalf("Context timed out waiting for resolve update to be processed")
			}

			// shutdown all active backends except the target.
			var targetAddr resolver.Address
			for idxI, idx := range tc.preUpdateBackendIndexes {
				if idx == tc.preUpdateTargetBackendIndex {
					targetAddr = activeAddrs[idxI]
					continue
				}
				backends[idx].S.Stop()
			}

			if err := pickfirst.CheckRPCsToBackend(ctx, cc, targetAddr); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(scStateExpectationToScState(tc.wantPreUpdateScStates, addrs), bal.subConns()); diff != "" {
				t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
			}

			if len(tc.updatedBackendIndexes) == 0 {
				if diff := cmp.Diff(tc.wantConnStateTransitions, bal.connStateTransitions()); diff != "" {
					t.Errorf("balancer states mismatch (-want +got):\n%s", diff)
				}
				return
			}

			// Restart all the backends.
			for i, s := range backends {
				if !tc.restartConnected && i == tc.preUpdateTargetBackendIndex {
					continue
				}
				s.S.Stop()
				if err := s.StartServer(); err != nil {
					t.Fatalf("Failed to re-start test backend: %v", err)
				}
			}

			activeAddrs = []resolver.Address{}
			for _, idx := range tc.updatedBackendIndexes {
				activeAddrs = append(activeAddrs, addrs[idx])
			}

			r.UpdateState(resolver.State{Addresses: activeAddrs})
			select {
			case <-bal.resolverUpdateSeen:
			case <-ctx.Done():
				t.Fatalf("Context timed out waiting for resolve update to be processed")
			}

			// shutdown all active backends except the target.
			for idxI, idx := range tc.updatedBackendIndexes {
				if idx == tc.postUpdateTargetBackendIndex {
					targetAddr = activeAddrs[idxI]
					continue
				}
				backends[idx].S.Stop()
			}

			if err := pickfirst.CheckRPCsToBackend(ctx, cc, targetAddr); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(scStateExpectationToScState(tc.wantPostUpdateScStates, addrs), bal.subConns()); diff != "" {
				t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.wantConnStateTransitions, bal.connStateTransitions()); diff != "" {
				t.Errorf("balancer states mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func (s) TestPickFirstLeaf_EmptyAddressList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	balChan := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancerChan: balChan})
	cc, r, backends := setupPickFirstLeaf(t, 1)
	addrs := stubBackendsToResolverAddrs(backends)

	r.UpdateState(resolver.State{Addresses: addrs})
	bal := <-balChan
	select {
	case <-bal.resolverUpdateSeen:
	case <-ctx.Done():
		t.Fatalf("Context timed out waiting for resolve update to be processed")
	}

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	r.UpdateState(resolver.State{})
	select {
	case <-bal.resolverUpdateSeen:
	case <-ctx.Done():
		t.Fatalf("Context timed out waiting for resolve update to be processed")
	}

	// The balancer should have entered transient failure.
	// It should transition to CONNECTING from TRANSIENT_FAILURE as sticky TF
	// only applies when the initial TF is reported due to connection failures
	// and not bad resolver states.
	r.UpdateState(resolver.State{Addresses: addrs})
	select {
	case <-bal.resolverUpdateSeen:
	case <-ctx.Done():
		t.Fatalf("Context timed out waiting for resolve update to be processed")
	}

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

	if diff := cmp.Diff(wantTransitions, bal.connStateTransitions()); diff != "" {
		t.Errorf("balancer states mismatch (-want +got):\n%s", diff)
	}
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

// stateStoringBalancer stores the state of the subconns being created.
type stateStoringBalancer struct {
	balancer.Balancer
	mu                 sync.Mutex
	scStates           []*scState
	stateTransitions   []connectivity.State
	resolverUpdateSeen chan struct{}
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
	bal := &stateStoringBalancer{
		resolverUpdateSeen: make(chan struct{}, 1),
	}
	bal.Balancer = balancer.Get(pickfirstleaf.Name).Build(&stateStoringCCWrapper{cc, bal}, opts)
	b.balancerChan <- bal
	return bal
}

func (b *stateStoringBalancer) UpdateClientConnState(state balancer.ClientConnState) error {
	err := b.Balancer.UpdateClientConnState(state)
	b.resolverUpdateSeen <- struct{}{}
	return err
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

func (b *stateStoringBalancer) addConnState(state connectivity.State) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.stateTransitions) == 0 || state != b.stateTransitions[len(b.stateTransitions)-1] {
		b.stateTransitions = append(b.stateTransitions, state)
	}
}

func (b *stateStoringBalancer) connStateTransitions() []connectivity.State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]connectivity.State{}, b.stateTransitions...)
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
	ccw.b.addScState(scs)
	opts.StateListener = func(s balancer.SubConnState) {
		ccw.b.mu.Lock()
		scs.State = s.ConnectivityState
		ccw.b.mu.Unlock()
		oldListener(s)
	}
	return ccw.ClientConn.NewSubConn(addrs, opts)
}

func (ccw *stateStoringCCWrapper) UpdateState(state balancer.State) {
	ccw.b.addConnState(state.ConnectivityState)
	ccw.ClientConn.UpdateState(state)
}

type scState struct {
	State connectivity.State
	Addrs []resolver.Address
}
