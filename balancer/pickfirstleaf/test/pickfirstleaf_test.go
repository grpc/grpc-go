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
	"slices"
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
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
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
func setupPickFirstLeaf(t *testing.T, backendCount int, opts ...grpc.DialOption) (*grpc.ClientConn, *manual.Resolver, *backendManager) {
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
	return cc, r, &backendManager{backends}
}

// TestPickFirstLeaf_SimpleResolverUpdate tests the behaviour of the pick first
// policy when when given an list of addresses. The following steps are carried
// out in order:
//  1. A list of addresses are given through the resolver. Only one
//     of the servers is running.
//  2. RPCs are sent to verify they reach the running server.
//
// The state transitions of the ClientConn and all the subconns created are
// verified.
func (s) TestPickFirstLeaf_SimpleResolverUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	balChan := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balChan})
	tests := []struct {
		name                     string
		backendCount             int
		backendIndexes           []int
		runningBackendIndex      int
		wantSCStates             []scStateExpectation
		wantConnStateTransitions []connectivity.State
	}{
		{
			name:                "two_server_first_ready",
			backendIndexes:      []int{0, 1},
			runningBackendIndex: 0,
			wantSCStates: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Ready},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
		{
			name:                "two_server_second_ready",
			backendIndexes:      []int{0, 1},
			runningBackendIndex: 1,
			wantSCStates: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Shutdown},
				{ServerIdx: 1, State: connectivity.Ready},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
		{
			name:                "duplicate_address",
			backendIndexes:      []int{0, 0, 1},
			runningBackendIndex: 1,
			wantSCStates: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Shutdown},
				{ServerIdx: 1, State: connectivity.Ready},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cc, r, bm := setupPickFirstLeaf(t, 2)
			addrs := stubBackendsToResolverAddrs(bm.backends)
			stateSubscriber := &ccStateSubscriber{transitions: []connectivity.State{}}
			internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

			activeAddrs := []resolver.Address{}
			for _, idx := range tc.backendIndexes {
				activeAddrs = append(activeAddrs, addrs[idx])
			}

			bm.stopAllExcept(tc.runningBackendIndex)
			runningAddr := addrs[tc.runningBackendIndex]

			r.UpdateState(resolver.State{Addresses: activeAddrs})
			bal := <-balChan
			testutils.AwaitState(ctx, t, cc, connectivity.Ready)

			if err := pickfirst.CheckRPCsToBackend(ctx, cc, runningAddr); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(scStateExpectationToSCState(tc.wantSCStates, addrs), bal.subConns()); diff != "" {
				t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.wantConnStateTransitions, stateSubscriber.transitions); diff != "" {
				t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPickFirstLeaf_ResolverUpdate tests the behaviour of the pick first
// policy when the following steps are carried out in order:
//  1. A list of addresses are given through the resolver. Only one
//     of the servers is running.
//  2. RPCs are sent to verify they reach the running server.
//  3. A second resolver update is sent. Again, only one of the servers is
//     running. This may not be the same server as before.
//  4. RPCs are sent to verify they reach the running server.
//
// The state transitions of the ClientConn and all the subconns created are
// verified.
func (s) TestPickFirstLeaf_MultipleResolverUpdates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	tests := []struct {
		name                     string
		backendCount             int
		backendIndexes1          []int
		runningBackendIndex1     int
		wantSCStates1            []scStateExpectation
		backendIndexes2          []int
		runningBackendIndex2     int
		wantSCStates2            []scStateExpectation
		wantConnStateTransitions []connectivity.State
	}{
		{
			name:                 "disjoint_updated_addresses",
			backendCount:         4,
			backendIndexes1:      []int{0, 1},
			runningBackendIndex1: 1,
			wantSCStates1: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Shutdown},
				{ServerIdx: 1, State: connectivity.Ready},
			},
			backendIndexes2:      []int{2, 3},
			runningBackendIndex2: 3,
			wantSCStates2: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Shutdown},
				{ServerIdx: 1, State: connectivity.Shutdown},
				{ServerIdx: 2, State: connectivity.Shutdown},
				{ServerIdx: 3, State: connectivity.Ready},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
		{
			name:                 "active_backend_in_updated_list",
			backendCount:         3,
			backendIndexes1:      []int{0, 1},
			runningBackendIndex1: 1,
			wantSCStates1: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Shutdown},
				{ServerIdx: 1, State: connectivity.Ready},
			},
			backendIndexes2:      []int{1, 2},
			runningBackendIndex2: 1,
			wantSCStates2: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Shutdown},
				{ServerIdx: 1, State: connectivity.Ready},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
		{
			name:                 "inactive_backend_in_updated_list",
			backendCount:         3,
			backendIndexes1:      []int{0, 1},
			runningBackendIndex1: 1,
			wantSCStates1: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Shutdown},
				{ServerIdx: 1, State: connectivity.Ready},
			},
			backendIndexes2:      []int{0, 2},
			runningBackendIndex2: 0,
			wantSCStates2: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Shutdown},
				{ServerIdx: 1, State: connectivity.Shutdown},
				{ServerIdx: 0, State: connectivity.Ready},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
		{
			name:                 "identical_list",
			backendCount:         2,
			backendIndexes1:      []int{0, 1},
			runningBackendIndex1: 1,
			wantSCStates1: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Shutdown},
				{ServerIdx: 1, State: connectivity.Ready},
			},
			backendIndexes2:      []int{0, 1},
			runningBackendIndex2: 1,
			wantSCStates2: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Shutdown},
				{ServerIdx: 1, State: connectivity.Ready},
			},
			wantConnStateTransitions: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cc, r, bm := setupPickFirstLeaf(t, tc.backendCount)
			addrs := stubBackendsToResolverAddrs(bm.backends)
			stateSubscriber := &ccStateSubscriber{transitions: []connectivity.State{}}
			internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

			resolverAddrs := []resolver.Address{}
			for _, idx := range tc.backendIndexes1 {
				resolverAddrs = append(resolverAddrs, addrs[idx])
			}

			bm.stopAllExcept(tc.runningBackendIndex1)
			targetAddr := addrs[tc.runningBackendIndex1]

			r.UpdateState(resolver.State{Addresses: resolverAddrs})
			bal := <-balCh
			testutils.AwaitState(ctx, t, cc, connectivity.Ready)

			if err := pickfirst.CheckRPCsToBackend(ctx, cc, targetAddr); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(scStateExpectationToSCState(tc.wantSCStates1, addrs), bal.subConns()); diff != "" {
				t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
			}

			// Start the backend that needs to be connected to next.
			if tc.runningBackendIndex1 != tc.runningBackendIndex2 {
				if err := bm.backends[tc.runningBackendIndex2].StartServer(); err != nil {
					t.Fatalf("Failed to re-start test backend: %v", err)
				}
			}

			resolverAddrs = []resolver.Address{}
			for _, idx := range tc.backendIndexes2 {
				resolverAddrs = append(resolverAddrs, addrs[idx])
			}
			r.UpdateState(resolver.State{Addresses: resolverAddrs})

			if slices.Contains(tc.backendIndexes2, tc.runningBackendIndex1) {
				// Verify that the ClientConn stays in READY.
				sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
				defer sCancel()
				testutils.AwaitNoStateChange(sCtx, t, cc, connectivity.Ready)
			}
			// We don't shut down the previous target server if it's different
			// from the current target server as it can cause the ClientConn to
			// go into IDLE if it has not yet processed the resolver update.
			targetAddr = addrs[tc.runningBackendIndex2]

			if err := pickfirst.CheckRPCsToBackend(ctx, cc, targetAddr); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(scStateExpectationToSCState(tc.wantSCStates2, addrs), bal.subConns()); diff != "" {
				t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.wantConnStateTransitions, stateSubscriber.transitions); diff != "" {
				t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
			}
		})
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
func (s) TestPickFirstLeaf_StopConnectedServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	balChan := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balChan})
	tests := []struct {
		name                     string
		backendCount             int
		runningServerIndex       int
		wantSCStates1            []scStateExpectation
		runningServerIndex2      int
		wantSCStates2            []scStateExpectation
		wantConnStateTransitions []connectivity.State
	}{
		{
			name:               "first_connected_idle_reconnect_first",
			backendCount:       2,
			runningServerIndex: 0,
			wantSCStates1: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Ready},
			},
			runningServerIndex2: 0,
			wantSCStates2: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Ready},
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
			name:               "second_connected_idle_reconnect_second",
			backendCount:       2,
			runningServerIndex: 1,
			wantSCStates1: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Shutdown},
				{ServerIdx: 1, State: connectivity.Ready},
			},
			runningServerIndex2: 1,
			wantSCStates2: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Shutdown},
				{ServerIdx: 1, State: connectivity.Ready},
				{ServerIdx: 0, State: connectivity.Shutdown},
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
			name:               "second_connected_idle_reconnect_first",
			backendCount:       2,
			runningServerIndex: 1,
			wantSCStates1: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Shutdown},
				{ServerIdx: 1, State: connectivity.Ready},
			},
			runningServerIndex2: 0,
			wantSCStates2: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Shutdown},
				{ServerIdx: 1, State: connectivity.Shutdown},
				{ServerIdx: 0, State: connectivity.Ready},
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
			name:               "first_connected_idle_reconnect_second",
			backendCount:       2,
			runningServerIndex: 0,
			wantSCStates1: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Ready},
			},
			runningServerIndex2: 1,
			wantSCStates2: []scStateExpectation{
				{ServerIdx: 0, State: connectivity.Shutdown},
				{ServerIdx: 1, State: connectivity.Ready},
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
			cc, r, bm := setupPickFirstLeaf(t, tc.backendCount)
			addrs := stubBackendsToResolverAddrs(bm.backends)
			stateSubscriber := &ccStateSubscriber{transitions: []connectivity.State{}}
			internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

			// shutdown all active backends except the target.
			bm.stopAllExcept(tc.runningServerIndex)
			targetAddr := addrs[tc.runningServerIndex]

			r.UpdateState(resolver.State{Addresses: addrs})
			bal := <-balChan
			testutils.AwaitState(ctx, t, cc, connectivity.Ready)

			if err := pickfirst.CheckRPCsToBackend(ctx, cc, targetAddr); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(scStateExpectationToSCState(tc.wantSCStates1, addrs), bal.subConns()); diff != "" {
				t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
			}

			// Shut down the connected server.
			bm.backends[tc.runningServerIndex].S.Stop()
			testutils.AwaitState(ctx, t, cc, connectivity.Idle)

			// Start the new target server.
			if err := bm.backends[tc.runningServerIndex2].StartServer(); err != nil {
				t.Fatalf("Failed to start server: %v", err)
			}

			targetAddr = addrs[tc.runningServerIndex2]
			if err := pickfirst.CheckRPCsToBackend(ctx, cc, targetAddr); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(scStateExpectationToSCState(tc.wantSCStates2, addrs), bal.subConns()); diff != "" {
				t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.wantConnStateTransitions, stateSubscriber.transitions); diff != "" {
				t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
			}
		})
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
	cc, r, bm := setupPickFirstLeaf(t, 1)
	addrs := stubBackendsToResolverAddrs(bm.backends)

	stateSubscriber := &ccStateSubscriber{transitions: []connectivity.State{}}
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

	if diff := cmp.Diff(wantTransitions, stateSubscriber.transitions); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

// scStateExpectation stores the expected state for a subconn created for the
// specified server. Since the servers use random ports, the server is identified
// using using its index in the list of backends.
type scStateExpectation struct {
	State     connectivity.State
	ServerIdx int
}

// scStateExpectationToSCState converts scStateExpectationToSCState to scState
// using the server addresses. This is required since the servers use random
// ports which are known only once the test runs.
func scStateExpectationToSCState(in []scStateExpectation, serverAddrs []resolver.Address) []scState {
	out := []scState{}
	for _, exp := range in {
		out = append(out, scState{
			State: exp.State,
			Addrs: []resolver.Address{serverAddrs[exp.ServerIdx]},
		})
	}
	return out
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
	balancer chan *stateStoringBalancer
}

func (b *stateStoringBalancerBuilder) Name() string {
	return stateStoringBalancerName
}

func (b *stateStoringBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	bal := &stateStoringBalancer{}
	bal.Balancer = balancer.Get(pickfirstleaf.Name).Build(&stateStoringCCWrapper{cc, bal}, opts)
	b.balancer <- bal
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
	backends []*stubserver.StubServer
}

func (b *backendManager) stopAllExcept(index int) {
	for idx, b := range b.backends {
		if idx != index {
			b.S.Stop()
		}
	}
}

type ccStateSubscriber struct {
	transitions []connectivity.State
}

func (c *ccStateSubscriber) OnMessage(msg any) {
	c.transitions = append(c.transitions, msg.(connectivity.State))
}
