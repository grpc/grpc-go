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

package pickfirstleaf_test

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/net/http2"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	pfinternal "google.golang.org/grpc/balancer/pickfirst/internal"
	"google.golang.org/grpc/balancer/pickfirst/pickfirstleaf"
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
// with service config specifying the use of the state_storing LB policy.
func setupPickFirstLeaf(t *testing.T, backendCount int, opts ...grpc.DialOption) (*grpc.ClientConn, *manual.Resolver, *backendManager) {
	t.Helper()
	r := manual.NewBuilderWithScheme("whatever")
	backends := make([]*stubserver.StubServer, backendCount)
	addrs := make([]resolver.Address, backendCount)

	for i := 0; i < backendCount; i++ {
		backend := stubserver.StartTestService(t, nil)
		t.Cleanup(func() {
			backend.Stop()
		})
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
// policy when given an list of addresses. The following steps are carried
// out in order:
//  1. A list of addresses are given through the resolver. Only one
//     of the servers is running.
//  2. RPCs are sent to verify they reach the running server.
//
// The state transitions of the ClientConn and all the subconns created are
// verified.
func (s) TestPickFirstLeaf_SimpleResolverUpdate_FirstServerReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})

	cc, r, bm := setupPickFirstLeaf(t, 2)
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
	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_SimpleResolverUpdate_FirstServerUnReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})

	cc, r, bm := setupPickFirstLeaf(t, 2)
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
	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_SimpleResolverUpdate_DuplicateAddrs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})

	cc, r, bm := setupPickFirstLeaf(t, 2)
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
	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions); diff != "" {
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
// The state transitions of the ClientConn and all the subconns created are
// verified.
func (s) TestPickFirstLeaf_ResolverUpdates_DisjointLists(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	cc, r, bm := setupPickFirstLeaf(t, 4)
	addrs := bm.resolverAddrs()
	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

	bm.backends[0].S.Stop()
	bm.backends[0].S = nil
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

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	bm.backends[2].S.Stop()
	bm.backends[2].S = nil
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

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_ResolverUpdates_ActiveBackendInUpdatedList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	cc, r, bm := setupPickFirstLeaf(t, 3)
	addrs := bm.resolverAddrs()
	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

	bm.backends[0].S.Stop()
	bm.backends[0].S = nil
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

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	bm.backends[2].S.Stop()
	bm.backends[2].S = nil
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

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_ResolverUpdates_InActiveBackendInUpdatedList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	cc, r, bm := setupPickFirstLeaf(t, 3)
	addrs := bm.resolverAddrs()
	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

	bm.backends[0].S.Stop()
	bm.backends[0].S = nil
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

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	bm.backends[2].S.Stop()
	bm.backends[2].S = nil
	if err := bm.backends[0].StartServer(); err != nil {
		t.Fatalf("Failed to re-start test backend: %v", err)
	}
	r.UpdateState(resolver.State{Addresses: []resolver.Address{addrs[0], addrs[2]}})

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}
	wantSCStates = []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_ResolverUpdates_IdenticalLists(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	cc, r, bm := setupPickFirstLeaf(t, 2)
	addrs := bm.resolverAddrs()
	stateSubscriber := &ccStateSubscriber{}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, stateSubscriber)

	bm.backends[0].S.Stop()
	bm.backends[0].S = nil
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

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
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

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions); diff != "" {
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
	cc, r, bm := setupPickFirstLeaf(t, 2)
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

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	// Shut down the connected server.
	bm.backends[0].S.Stop()
	bm.backends[0].S = nil
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Start the new target server.
	if err := bm.backends[0].StartServer(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Idle,
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_StopConnectedServer_SecondServerRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	cc, r, bm := setupPickFirstLeaf(t, 2)
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

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	// Shut down the connected server.
	bm.backends[1].S.Stop()
	bm.backends[1].S = nil
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Start the new target server.
	if err := bm.backends[1].StartServer(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}

	wantSCStates = []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Ready},
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Idle,
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_StopConnectedServer_SecondServerToFirst(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	cc, r, bm := setupPickFirstLeaf(t, 2)
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

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	// Shut down the connected server.
	bm.backends[1].S.Stop()
	bm.backends[1].S = nil
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Start the new target server.
	if err := bm.backends[0].StartServer(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[0]); err != nil {
		t.Fatal(err)
	}

	wantSCStates = []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Idle,
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestPickFirstLeaf_StopConnectedServer_FirstServerToSecond(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	balCh := make(chan *stateStoringBalancer, 1)
	balancer.Register(&stateStoringBalancerBuilder{balancer: balCh})
	cc, r, bm := setupPickFirstLeaf(t, 2)
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

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	// Shut down the connected server.
	bm.backends[0].S.Stop()
	bm.backends[0].S = nil
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Start the new target server.
	if err := bm.backends[1].StartServer(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	if err := pickfirst.CheckRPCsToBackend(ctx, cc, addrs[1]); err != nil {
		t.Fatal(err)
	}

	wantSCStates = []scState{
		{Addrs: []resolver.Address{addrs[0]}, State: connectivity.Shutdown},
		{Addrs: []resolver.Address{addrs[1]}, State: connectivity.Ready},
	}

	if diff := cmp.Diff(wantSCStates, bal.subConnStates()); diff != "" {
		t.Errorf("subconn states mismatch (-want +got):\n%s", diff)
	}

	wantConnStateTransitions := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Idle,
		connectivity.Connecting,
		connectivity.Ready,
	}
	if diff := cmp.Diff(wantConnStateTransitions, stateSubscriber.transitions); diff != "" {
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
	cc, r, bm := setupPickFirstLeaf(t, 1)
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

	if diff := cmp.Diff(wantTransitions, stateSubscriber.transitions); diff != "" {
		t.Errorf("ClientConn states mismatch (-want +got):\n%s", diff)
	}
}

// TestPickFirstLeaf_HappyEyeballs_TFAfterEndOfList verifies that pickfirst
// correctly detects the end of the first happy eyeballs pass when the timer
// causes pickfirst to reach the end of the address list and failures are
// reported out of order.
func (s) TestPickFirstLeaf_HappyEyeballs_TFAfterEndOfList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	timerCh := make(chan struct{})
	originalTimer := pfinternal.TimeAfterFunc
	pfinternal.TimeAfterFunc = func(_ time.Duration, f func()) *time.Timer {
		// Set a really long expiration to prevent it from triggering
		// automatically.
		ret := time.AfterFunc(time.Hour, f)
		go func() {
			select {
			case <-ctx.Done():
			case <-timerCh:
			}
			ret.Reset(0)
		}()
		return ret
	}

	defer func() {
		pfinternal.TimeAfterFunc = originalTimer
	}()

	defer func() {
		pfinternal.TimeAfterFunc = originalTimer
	}()

	servers := newHangingServerGroup(t, 3)
	defer servers.close()
	rb := manual.NewBuilderWithScheme("whatever")
	rb.InitialState(resolver.State{Addresses: servers.addrs})
	cc, err := grpc.NewClient("whatever:///this-gets-overwritten",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, pickfirstleaf.Name)),
		grpc.WithResolvers(rb))

	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	cc.Connect()

	testutils.AwaitState(ctx, t, cc, connectivity.Connecting)

	// Verify that only the first server is contacted.
	if err := servers.awaitContacted(ctx, 0); err != nil {
		t.Fatalf("Server with address %q not contacted: %v", servers.addrs[0], err)
	}

	// Ensure no other servers are contacted.
	if got, want := servers.isContacted(1), false; got != want {
		t.Fatalf("Servers.isContacted(%q) = %t, want %t", servers.addrs[1], got, want)
	}
	if got, want := servers.isContacted(2), false; got != want {
		t.Fatalf("Servers.isContacted(%q) = %t, want %t", servers.addrs[2], got, want)
	}

	// Make the happy eyeballs timer fire twice so that pickfirst reaches the
	// last address in the list.
	timerCh <- struct{}{}

	// Verify that the second server is contacted and 3rd isn't.
	if err := servers.awaitContacted(ctx, 1); err != nil {
		t.Fatalf("Server with address %q not contacted: %v", servers.addrs[1], err)
	}

	if got, want := servers.isContacted(2), false; got != want {
		t.Fatalf("Servers.isContacted(%q) = %t, want %t", servers.addrs[2], got, want)
	}
	timerCh <- struct{}{}
	if err := servers.awaitContacted(ctx, 2); err != nil {
		t.Fatalf("Server with address %q not contacted: %v", servers.addrs[2], err)
	}

	// First SubConn Fails.
	servers.closeConn(0)

	// No TF should be reported until the first pass is complete.
	shortCtx, shortCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer shortCancel()

	testutils.AwaitNotState(shortCtx, t, cc, connectivity.TransientFailure)

	// Move off the end of the list, pickfirst should still be waiting for TFs
	// to be reported.
	timerCh <- struct{}{}

	// Third SubConn fails.
	servers.closeConn(2)

	testutils.AwaitNotState(shortCtx, t, cc, connectivity.TransientFailure)

	// Last SubConn fails, this should result in a TF update.
	servers.closeConn(1)
	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)
}

// TestPickFirstLeaf_HappyEyeballs_TriggerConnectionDelay verifies that
// pickfirst attempts to connect to the second backend once the happy eyeballs
// timer expires.
func (s) TestPickFirstLeaf_HappyEyeballs_TriggerConnectionDelay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	timerCh := make(chan struct{})
	originalTimer := pfinternal.TimeAfterFunc
	pfinternal.TimeAfterFunc = func(_ time.Duration, f func()) *time.Timer {
		// Set a really long expiration to prevent it from triggering
		// automatically.
		ret := time.AfterFunc(time.Hour, f)
		go func() {
			select {
			case <-ctx.Done():
			case <-timerCh:
			}
			ret.Reset(0)
		}()
		return ret
	}

	defer func() {
		pfinternal.TimeAfterFunc = originalTimer
	}()

	defer func() {
		pfinternal.TimeAfterFunc = originalTimer
	}()

	servers := newHangingServerGroup(t, 2)
	defer servers.close()
	rb := manual.NewBuilderWithScheme("whatever")
	rb.InitialState(resolver.State{Addresses: servers.addrs})
	cc, err := grpc.NewClient("whatever:///this-gets-overwritten",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, pickfirstleaf.Name)),
		grpc.WithResolvers(rb))

	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	cc.Connect()

	testutils.AwaitState(ctx, t, cc, connectivity.Connecting)

	// Verify that the first server is contacted.
	if err := servers.awaitContacted(ctx, 0); err != nil {
		t.Fatalf("Server with address %q not contacted: %v", servers.addrs[0], err)
	}

	if got, want := servers.isContacted(1), false; got != want {
		t.Fatalf("Servers.isContacted(%q) = %t, want %t", servers.addrs[1], got, want)
	}

	timerCh <- struct{}{}

	// Second connection attempt is successful.
	if err := servers.awaitContacted(ctx, 1); err != nil {
		t.Fatalf("Server with address %q not contacted: %v", servers.addrs[1], err)
	}
	servers.enterReady(1)
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)
}

// TestPickFirstLeaf_HappyEyeballs_TFThenTimerFires tests the pickfirst balancer
// by causing a SubConn to fail and then jumping to the 3rd SubConn after the
// happy eyeballs timer expires.
func (s) TestPickFirstLeaf_HappyEyeballs_TFThenTimerFires(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	timerMu := sync.Mutex{}
	timerCh := make(chan struct{})
	originalTimer := pfinternal.TimeAfterFunc
	pfinternal.TimeAfterFunc = func(_ time.Duration, f func()) *time.Timer {
		// Set a really long expiration to prevent it from triggering
		// automatically.
		ret := time.AfterFunc(time.Hour, f)
		go func() {
			timerMu.Lock()
			ch := timerCh
			timerMu.Unlock()
			select {
			case <-ctx.Done():
			case <-ch:
			}
			ret.Reset(0)
		}()
		return ret
	}

	defer func() {
		pfinternal.TimeAfterFunc = originalTimer
	}()

	servers := newHangingServerGroup(t, 3)
	defer servers.close()
	rb := manual.NewBuilderWithScheme("whatever")
	rb.InitialState(resolver.State{Addresses: servers.addrs})
	cc, err := grpc.NewClient("whatever:///this-gets-overwritten",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, pickfirstleaf.Name)),
		grpc.WithResolvers(rb))

	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	cc.Connect()

	testutils.AwaitState(ctx, t, cc, connectivity.Connecting)

	// Verify that only the first server is contacted.
	if err := servers.awaitContacted(ctx, 0); err != nil {
		t.Fatalf("Server with address %q not contacted: %v", servers.addrs[0], err)
	}

	// Ensure no other servers are contacted.
	if got, want := servers.isContacted(1), false; got != want {
		t.Fatalf("Servers.isContacted(%q) = %t, want %t", servers.addrs[1], got, want)
	}
	if got, want := servers.isContacted(2), false; got != want {
		t.Fatalf("Servers.isContacted(%q) = %t, want %t", servers.addrs[2], got, want)
	}

	// First SubConn Fails.
	// Replace the timer channel so that the old timers don't attempt to read
	// messages pushed next.
	timerMu.Lock()
	timerCh = make(chan struct{})
	timerMu.Unlock()
	servers.closeConn(0)

	// The second server is contacted.
	// Verify that only the first server is contacted.
	if err := servers.awaitContacted(ctx, 1); err != nil {
		t.Fatalf("Server with address %q not contacted: %v", servers.addrs[1], err)
	}

	// Ensure no other servers are contacted.
	if got, want := servers.isContacted(2), false; got != want {
		t.Fatalf("Servers.isContacted(%q) = %t, want %t", servers.addrs[2], got, want)
	}

	// The happy eyeballs timer expires, skipping server[1] and requesting the creation
	// of a third SubConn.
	timerCh <- struct{}{}

	if err := servers.awaitContacted(ctx, 2); err != nil {
		t.Fatalf("Server with address %q not contacted: %v", servers.addrs[2], err)
	}

	// Second SubConn connects.
	servers.enterReady(1)
	testutils.AwaitState(ctx, t, cc, connectivity.Connecting)
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
	backends []*stubserver.StubServer
}

func (b *backendManager) stopAllExcept(index int) {
	for idx, b := range b.backends {
		if idx != index {
			b.S.Stop()
			b.S = nil
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

type ccStateSubscriber struct {
	transitions []connectivity.State
}

func (c *ccStateSubscriber) OnMessage(msg any) {
	c.transitions = append(c.transitions, msg.(connectivity.State))
}

// handingServerGroup is a group of servers that accept a TCP connection and
// remain idle until asked to close the connection. They can be used to control
// how long it takes for a subchannel to report a TRANSIENT_FAILURE in tests.
type handingServerGroup struct {
	addrs                []resolver.Address
	listeners            []net.Listener
	serverConnCloseFuncs []func()
	serverContacted      []*grpcsync.Event
	readyChans           []chan struct{}
}

func newHangingServerGroup(t *testing.T, count int) *handingServerGroup {
	listeners := []net.Listener{}
	closeFns := []func(){}
	addrs := []resolver.Address{}
	contactedEvents := []*grpcsync.Event{}
	readyChans := []chan struct{}{}

	for i := 0; i < count; i++ {
		lis, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			t.Fatalf("Error while listening. Err: %v", err)
		}
		listeners = append(listeners, lis)
		addrs = append(addrs, resolver.Address{Addr: lis.Addr().String()})
		closeChan := make(chan struct{})
		closeFns = append(closeFns, sync.OnceFunc(func() { close(closeChan) }))
		contacted := grpcsync.NewEvent()
		contactedEvents = append(contactedEvents, contacted)
		readyChan := make(chan struct{})
		readyChans = append(readyChans, readyChan)

		go func() {
			conn, err := lis.Accept()
			defer conn.Close()
			if err != nil {
				t.Error(err)
				return
			}

			contacted.Fire()

			for {
				select {
				case <-closeChan:
					return
				case <-readyChan:
					framer := http2.NewFramer(conn, conn)
					if err := framer.WriteSettings(http2.Setting{}); err != nil {
						t.Fatalf("Error while writing settings frame. %v", err)
						return
					}

				}
			}
		}()
	}

	return &handingServerGroup{
		addrs:                addrs,
		listeners:            listeners,
		serverConnCloseFuncs: closeFns,
		serverContacted:      contactedEvents,
		readyChans:           readyChans,
	}
}

func (hg *handingServerGroup) close() {
	for _, fn := range hg.serverConnCloseFuncs {
		fn()
	}
	for _, l := range hg.listeners {
		l.Close()
	}
}

func (hg *handingServerGroup) closeConn(serverIdx int) {
	hg.serverConnCloseFuncs[serverIdx]()
}

func (hg *handingServerGroup) isContacted(serverIdx int) bool {
	return hg.serverContacted[serverIdx].HasFired()
}

func (hg *handingServerGroup) enterReady(serverIdx int) {
	hg.readyChans[serverIdx] <- struct{}{}
}

func (hg *handingServerGroup) awaitContacted(ctx context.Context, serverIdx int) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-hg.serverContacted[serverIdx].Done():
	}
	return nil
}
