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
 */

package pickfirstleaf_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	pfinternal "google.golang.org/grpc/balancer/pickfirst/internal"
	"google.golang.org/grpc/balancer/pickfirst/pickfirstleaf"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/pickfirst"
	"google.golang.org/grpc/internal/testutils/stats"
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
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, pickfirstleaf.Name)),
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
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, pickfirstleaf.Name)),
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
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, pickfirstleaf.Name)),
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

	if got, _ := tmr.Metric("grpc.lb.pick_first.connection_attempts_succeeded"); got != 1 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.connection_attempts_succeeded", got, 1)
	}
	if got, _ := tmr.Metric("grpc.lb.pick_first.disconnections"); got != 0 {
		t.Errorf("Unexpected data for metric %v, got: %v, want: %v", "grpc.lb.pick_first.disconnections", got, 0)
	}
}

func (s) TestPickFirstLeaf_InterleavingIPV4Preffered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc := testutils.NewBalancerClientConn(t)
	bal := balancer.Get(pickfirstleaf.Name).Build(cc, balancer.BuildOptions{})
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

func (s) TestPickFirstLeaf_InterleavingIPv6Preffered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc := testutils.NewBalancerClientConn(t)
	bal := balancer.Get(pickfirstleaf.Name).Build(cc, balancer.BuildOptions{})
	defer bal.Close()
	ccState := balancer.ClientConnState{
		ResolverState: resolver.State{
			Endpoints: []resolver.Endpoint{
				{Addresses: []resolver.Address{{Addr: "[0001:0001:0001:0001:0001:0001:0001:0001]:8080"}}},
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

func (s) TestPickFirstLeaf_InterleavingUnknownPreffered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc := testutils.NewBalancerClientConn(t)
	bal := balancer.Get(pickfirstleaf.Name).Build(cc, balancer.BuildOptions{})
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
			bd.Data = balancer.Get(pickfirstleaf.Name).Build(bd.ClientConn, bd.BuildOptions)
		},
		Close: func(bd *stub.BalancerData) {
			bd.Data.(balancer.Balancer).Close()
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			ccs.ResolverState = pickfirstleaf.EnableHealthListener(ccs.ResolverState)
			return bd.Data.(balancer.Balancer).UpdateClientConnState(ccs)
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
			bd.Data = balancer.Get(pickfirstleaf.Name).Build(ccw, bd.BuildOptions)
		},
		Close: func(bd *stub.BalancerData) {
			bd.Data.(balancer.Balancer).Close()
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			// Functions like a non-petiole policy by not configuring the use
			// of health listeners.
			return bd.Data.(balancer.Balancer).UpdateClientConnState(ccs)
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
			bd.Data = balancer.Get(pickfirstleaf.Name).Build(ccw, bd.BuildOptions)
		},
		Close: func(bd *stub.BalancerData) {
			bd.Data.(balancer.Balancer).Close()
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			ccs.ResolverState = pickfirstleaf.EnableHealthListener(ccs.ResolverState)
			return bd.Data.(balancer.Balancer).UpdateClientConnState(ccs)
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
