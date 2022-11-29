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
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils/pickfirst"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/test/grpc_testing"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

const pickFirstServiceConfig = `{"loadBalancingConfig": [{"pick_first":{}}]}`

// setupPickFirst performs steps required for pick_first tests. It starts a
// bunch of backends exporting the TestService, creates a ClientConn to them
// with service config specifying the use of the pick_first LB policy.
func setupPickFirst(t *testing.T, backendCount int, opts ...grpc.DialOption) (*grpc.ClientConn, *manual.Resolver, []*stubserver.StubServer) {
	t.Helper()

	// Initialize channelz. Used to determine pending RPC count.
	czCleanup := channelz.NewChannelzStorageForTesting()
	t.Cleanup(func() { czCleanupWrapper(czCleanup, t) })

	r := manual.NewBuilderWithScheme("whatever")

	backends := make([]*stubserver.StubServer, backendCount)
	addrs := make([]resolver.Address, backendCount)
	for i := 0; i < backendCount; i++ {
		backend := &stubserver.StubServer{
			EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
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
	cc, err := grpc.Dial(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
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
	for state := cc.GetState(); state != connectivity.TransientFailure; state = cc.GetState() {
		if !cc.WaitForStateChange(ctx, state) {
			t.Fatalf("timeout waiting for state change. got %v; want %v", state, connectivity.TransientFailure)
		}
	}

	doneCh := make(chan struct{})
	client := testpb.NewTestServiceClient(cc)
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
		started := tcs[0].ChannelData.CallsStarted
		completed := tcs[0].ChannelData.CallsSucceeded + tcs[0].ChannelData.CallsFailed
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
