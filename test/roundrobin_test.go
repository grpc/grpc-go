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

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/channelz"
	imetadata "google.golang.org/grpc/internal/metadata"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"
	testgrpc "google.golang.org/grpc/test/grpc_testing"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

const rrServiceConfig = `{"loadBalancingConfig": [{"round_robin":{}}]}`

func checkRoundRobin(ctx context.Context, cc *grpc.ClientConn, addrs []resolver.Address) error {
	client := testgrpc.NewTestServiceClient(cc)
	// Make sure connections to all backends are up. We need to do this two
	// times (to be sure that round_robin has kicked in) because the channel
	// could have been configured with a different LB policy before the switch
	// to round_robin. And the previous LB policy could be sharing backends with
	// round_robin, and therefore in the first iteration of this loop, RPCs
	// could land on backends owned by the previous LB policy.
	backendCount := len(addrs)
	for j := 0; j < 2; j++ {
		for i := 0; i < backendCount; i++ {
			for {
				time.Sleep(time.Millisecond)
				if ctx.Err() != nil {
					return fmt.Errorf("timeout waiting for connection to %q to be up", addrs[i].Addr)
				}
				var peer peer.Peer
				if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer)); err != nil {
					// Some tests remove backends and check if round robin is happening
					// across the remaining backends. In such cases, RPCs can initially fail
					// on the connection using the removed backend. Just keep retrying and
					// eventually the connection using the removed backend will shutdown and
					// will be removed.
					continue
				}
				if peer.Addr.String() == addrs[i].Addr {
					break
				}
			}
		}
	}
	// Perform 3 iterations and ensure they are all equal.
	var iterations [][]string
	for i := 0; i < 3; i++ {
		iteration := make([]string, backendCount)
		for c := 0; c < backendCount; c++ {
			var peer peer.Peer
			if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer)); err != nil {
				return fmt.Errorf("EmptyCall() = %v, want <nil>", err)
			}
			iteration[c] = peer.Addr.String()
		}
		iterations = append(iterations, iteration)
	}
	if !cmp.Equal(iterations[0], iterations[1]) || !cmp.Equal(iterations[0], iterations[2]) {
		return fmt.Errorf("non-roundrobin, first iter: %v, second iter: %v, third iter: %v", iterations[0], iterations[1], iterations[2])
	}
	return nil
}

func testRoundRobinBasic(ctx context.Context, t *testing.T, opts ...grpc.DialOption) (*grpc.ClientConn, *manual.Resolver, []*stubserver.StubServer) {
	t.Helper()

	// Initialize channelz. Used to determine pending RPC count.
	czCleanup := channelz.NewChannelzStorageForTesting()
	t.Cleanup(func() { czCleanupWrapper(czCleanup, t) })

	r := manual.NewBuilderWithScheme("whatever")

	const backendCount = 5
	backends := make([]*stubserver.StubServer, backendCount)
	addrs := make([]resolver.Address, backendCount)
	for i := 0; i < backendCount; i++ {
		backend := &stubserver.StubServer{
			EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
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
		grpc.WithDefaultServiceConfig(rrServiceConfig),
	}
	dopts = append(dopts, opts...)
	cc, err := grpc.Dial(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })
	client := testpb.NewTestServiceClient(cc)

	// At this point, the resolver has not returned any addresses to the channel.
	// This RPC must block until the context expires.
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := client.EmptyCall(sCtx, &testpb.Empty{}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() = %s, want %s", status.Code(err), codes.DeadlineExceeded)
	}

	r.UpdateState(resolver.State{Addresses: addrs})
	if err := checkRoundRobin(ctx, cc, addrs); err != nil {
		t.Fatal(err)
	}
	return cc, r, backends
}

// TestRoundRobin_Basic tests the most basic scenario for round_robin. It brings
// up a bunch of backends and verifies that RPCs are getting round robin-ed
// across these backends.
func (s) TestRoundRobin_Basic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testRoundRobinBasic(ctx, t)
}

// TestRoundRobin_AddressesRemoved tests the scenario where a bunch of backends
// are brought up, and round_robin is configured as the LB policy and RPCs are
// being correctly round robin-ed across these backends. We then send a resolver
// update with no addresses and verify that the channel enters TransientFailure
// and RPCs fail with an expected error message.
func (s) TestRoundRobin_AddressesRemoved(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc, r, _ := testRoundRobinBasic(ctx, t)

	// Send a resolver update with no addresses. This should push the channel into
	// TransientFailure.
	r.UpdateState(resolver.State{Addresses: []resolver.Address{}})
	for state := cc.GetState(); state != connectivity.TransientFailure; state = cc.GetState() {
		if !cc.WaitForStateChange(ctx, state) {
			t.Fatalf("timeout waiting for state change. got %v; want %v", state, connectivity.TransientFailure)
		}
	}

	const msgWant = "produced zero addresses"
	client := testpb.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); !strings.Contains(status.Convert(err).Message(), msgWant) {
		t.Fatalf("EmptyCall() = %v, want Contains(Message(), %q)", err, msgWant)
	}
}

// TestRoundRobin_NewAddressWhileBlocking tests the case where round_robin is
// configured on a channel, things are working as expected and then a resolver
// updates removes all addresses. An RPC attempted at this point in time will be
// blocked because there are no valid backends. This test verifies that when new
// backends are added, the RPC is able to complete.
func (s) TestRoundRobin_NewAddressWhileBlocking(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc, r, backends := testRoundRobinBasic(ctx, t)

	// Send a resolver update with no addresses. This should push the channel into
	// TransientFailure.
	r.UpdateState(resolver.State{Addresses: []resolver.Address{}})
	for state := cc.GetState(); state != connectivity.TransientFailure; state = cc.GetState() {
		if !cc.WaitForStateChange(ctx, state) {
			t.Fatalf("timeout waiting for state change. got %v; want %v", state, connectivity.TransientFailure)
		}
	}

	client := testpb.NewTestServiceClient(cc)
	doneCh := make(chan struct{})
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

// TestRoundRobin_OneServerDown tests the scenario where a channel is configured
// to round robin across a set of backends, and things are working correctly.
// One backend goes down. The test verifies that going forward, RPCs are round
// robin-ed across the remaining set of backends.
func (s) TestRoundRobin_OneServerDown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc, _, backends := testRoundRobinBasic(ctx, t)

	// Stop one backend. RPCs should round robin across the remaining backends.
	backends[len(backends)-1].Stop()

	addrs := make([]resolver.Address, len(backends)-1)
	for i := 0; i < len(backends)-1; i++ {
		addrs[i] = resolver.Address{Addr: backends[i].Address}
	}
	if err := checkRoundRobin(ctx, cc, addrs); err != nil {
		t.Fatalf("RPCs are not being round robined across remaining servers: %v", err)
	}
}

// TestRoundRobin_AllServersDown tests the scenario where a channel is
// configured to round robin across a set of backends, and things are working
// correctly. Then, all backends go down. The test verifies that the channel
// moves to TransientFailure and failfast RPCs fail with Unavailable.
func (s) TestRoundRobin_AllServersDown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc, _, backends := testRoundRobinBasic(ctx, t)

	// Stop all backends.
	for _, b := range backends {
		b.Stop()
	}

	// Wait for TransientFailure.
	for state := cc.GetState(); state != connectivity.TransientFailure; state = cc.GetState() {
		if !cc.WaitForStateChange(ctx, state) {
			t.Fatalf("timeout waiting for state change. got %v; want %v", state, connectivity.TransientFailure)
		}
	}

	// Failfast RPCs should fail with Unavailable.
	client := testpb.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(context.Background(), &testpb.Empty{}); status.Code(err) == codes.Unavailable {
		return
	}
}

// TestRoundRobin_UpdateAddressAttributes tests the scenario where the addresses
// returned by the resolver contain attributes. The test verifies that the
// attributes contained in the addresses show up as RPC metadata in the backend.
func (s) TestRoundRobin_UpdateAddressAttributes(t *testing.T) {
	const (
		testMDKey   = "test-md"
		testMDValue = "test-md-value"
	)
	r := manual.NewBuilderWithScheme("whatever")

	// Spin up a StubServer to serve as a backend. The implementation verifies
	// that the expected metadata is received.
	testMDChan := make(chan []string, 1)
	backend := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if ok {
				select {
				case testMDChan <- md[testMDKey]:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return &testpb.Empty{}, nil
		},
	}
	if err := backend.StartServer(); err != nil {
		t.Fatalf("Failed to start backend: %v", err)
	}
	t.Logf("Started TestService backend at: %q", backend.Address)
	t.Cleanup(func() { backend.Stop() })

	// Dial the backend with round_robin as the LB policy.
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithDefaultServiceConfig(rrServiceConfig),
	}
	cc, err := grpc.Dial(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	// Send a resolver update with no address attributes.
	addr := resolver.Address{Addr: backend.Address}
	r.UpdateState(resolver.State{Addresses: []resolver.Address{addr}})

	// Make an RPC and ensure it does not contain the metadata we are looking for.
	client := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() = %v, want <nil>", err)
	}
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout when waiting for metadata received in RPC")
	case md := <-testMDChan:
		if len(md) != 0 {
			t.Fatalf("received metadata %v, want nil", md)
		}
	}

	// Send a resolver update with address attributes.
	addrWithAttributes := imetadata.Set(addr, metadata.Pairs(testMDKey, testMDValue))
	r.UpdateState(resolver.State{Addresses: []resolver.Address{addrWithAttributes}})

	// Make an RPC and ensure it contains the metadata we are looking for. The
	// resolver update isn't processed synchronously, so we wait some time before
	// failing if some RPCs do not contain it.
Done:
	for {
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			t.Fatalf("EmptyCall() = %v, want <nil>", err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout when waiting for metadata received in RPC")
		case md := <-testMDChan:
			if len(md) == 1 && md[0] == testMDValue {
				break Done
			}
		}
		time.Sleep(defaultTestShortTimeout)
	}
}
