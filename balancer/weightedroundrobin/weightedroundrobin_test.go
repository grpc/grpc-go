/*
 *
 * Copyright 2017 gRPC authors.
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

package weightedroundrobin_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/go-cmp/cmp"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer/weightedroundrobin"
	"google.golang.org/grpc/codes"
	_ "google.golang.org/grpc/grpclog/glogger"
	"google.golang.org/grpc/internal/leakcheck"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

func equalApproximate(a, b float64) error {
	opt := cmp.Comparer(func(x, y float64) bool {
		delta := math.Abs(x - y)
		mean := math.Abs(x+y) / 2.0
		return delta/mean < 0.05
	})
	if !cmp.Equal(a, b, opt) {
		return errors.New(cmp.Diff(a, b))
	}
	return nil
}

type testServer struct {
	testpb.TestServiceServer
}

func (s *testServer) EmptyCall(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
	return &testpb.Empty{}, nil
}

func (s *testServer) FullDuplexCall(stream testpb.TestService_FullDuplexCallServer) error {
	return nil
}

type test struct {
	servers   []*grpc.Server
	addresses []string
}

func (t *test) cleanup() {
	for _, s := range t.servers {
		s.Stop()
	}
}

func startTestServers(count int) (_ *test, err error) {
	t := &test{}

	defer func() {
		if err != nil {
			t.cleanup()
		}
	}()
	for i := 0; i < count; i++ {
		lis, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			return nil, fmt.Errorf("failed to listen %v", err)
		}

		s := grpc.NewServer()
		testpb.RegisterTestServiceServer(s, &testServer{})
		t.servers = append(t.servers, s)
		t.addresses = append(t.addresses, lis.Addr().String())

		go func(s *grpc.Server, l net.Listener) {
			s.Serve(l)
		}(s, lis)
	}

	return t, nil
}

func TestOneBackend(t *testing.T) {
	defer leakcheck.Check(t)
	r, cleanup := manual.GenerateAndRegisterManualResolver()
	defer cleanup()

	test, err := startTestServers(1)
	if err != nil {
		t.Fatalf("failed to start servers: %v", err)
	}
	defer test.cleanup()

	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName(weightedroundrobin.Name))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()
	testc := testpb.NewTestServiceClient(cc)
	// The first RPC should fail because there's no address.
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := testc.EmptyCall(ctx, &testpb.Empty{}); err == nil || status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() = _, %v, want _, DeadlineExceeded", err)
	}

	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: test.addresses[0]}}})
	// The second RPC should succeed.
	if _, err := testc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() = _, %v, want _, <nil>", err)
	}
}

func TestBackendsWeightedRoundRobin(t *testing.T) {
	defer leakcheck.Check(t)
	r, cleanup := manual.GenerateAndRegisterManualResolver()
	defer cleanup()

	backendCount := 5
	test, err := startTestServers(backendCount)
	if err != nil {
		t.Fatalf("failed to start servers: %v", err)
	}
	defer test.cleanup()

	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName(weightedroundrobin.Name))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()
	testc := testpb.NewTestServiceClient(cc)
	// The first RPC should fail because there's no address.
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := testc.EmptyCall(ctx, &testpb.Empty{}); err == nil || status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() = _, %v, want _, DeadlineExceeded", err)
	}

	var resolvedAddrs []resolver.Address
	for i := 0; i < backendCount; i++ {
		resolvedAddrs = append(resolvedAddrs,
			resolver.Address{
				Addr:     test.addresses[i],
				Metadata: &weightedroundrobin.AddrInfo{Weight: uint32(i + 1)}})
	}
	// Backend can be listed several times.
	resolvedAddrs = append(resolvedAddrs,
		resolver.Address{
			Addr:     test.addresses[backendCount-1],
			Metadata: &weightedroundrobin.AddrInfo{Weight: uint32(1)}})

	r.UpdateState(resolver.State{Addresses: resolvedAddrs})
	var p peer.Peer
	// Make sure connections to all servers are up.
	for si := 0; si < backendCount; si++ {
		var connected bool
		for i := 0; i < 1000; i++ {
			if _, err := testc.EmptyCall(context.Background(), &testpb.Empty{}, grpc.Peer(&p)); err != nil {
				t.Fatalf("EmptyCall() = _, %v, want _, <nil>", err)
			}
			if p.Addr.String() == test.addresses[si] {
				connected = true
				break
			}
			time.Sleep(time.Millisecond)
		}
		if !connected {
			t.Fatalf("Connection to %v was not up after more than 1 second", test.addresses[si])
		}
	}

	checkWeighedRoundRobin(t, testc, resolvedAddrs)
}

func checkWeighedRoundRobin(t *testing.T, testc testpb.TestServiceClient, addrs []resolver.Address) {
	var sumOfWeights uint32
	for _, addr := range addrs {
		sumOfWeights += addr.Metadata.(*weightedroundrobin.AddrInfo).Weight
	}

	results := make(map[string]uint32)

	iterCount := 10000
	var p peer.Peer
	for i := 0; i < iterCount; i++ {
		if _, err := testc.EmptyCall(context.Background(), &testpb.Empty{}, grpc.Peer(&p)); err != nil {
			t.Fatalf("EmptyCall() = _, %v, want _, <nil>", err)
		}
		results[p.Addr.String()]++
	}

	wantRatio := make(map[string]float64)
	for _, addr := range addrs {
		wantRatio[addr.Addr] += float64(addr.Metadata.(*weightedroundrobin.AddrInfo).Weight) / float64(sumOfWeights)
	}
	gotRatio := make(map[string]float64)
	for key, count := range results {
		gotRatio[key] = float64(count) / float64(iterCount)
	}

	for key := range wantRatio {
		if err := equalApproximate(gotRatio[key], wantRatio[key]); err != nil {
			t.Errorf("%v not equal %v", key, err)
		}
	}
}

func TestAddressesRemoved(t *testing.T) {
	defer leakcheck.Check(t)
	r, cleanup := manual.GenerateAndRegisterManualResolver()
	defer cleanup()

	test, err := startTestServers(1)
	if err != nil {
		t.Fatalf("failed to start servers: %v", err)
	}
	defer test.cleanup()

	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName(weightedroundrobin.Name))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()
	testc := testpb.NewTestServiceClient(cc)
	// The first RPC should fail because there's no address.
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := testc.EmptyCall(ctx, &testpb.Empty{}); err == nil || status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() = _, %v, want _, DeadlineExceeded", err)
	}

	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: test.addresses[0]}}})
	// The second RPC should succeed.
	if _, err := testc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() = _, %v, want _, <nil>", err)
	}

	r.UpdateState(resolver.State{Addresses: []resolver.Address{}})
	for i := 0; i < 1000; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, err := testc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); status.Code(err) == codes.DeadlineExceeded {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("No RPC failed after removing all addresses, want RPC to fail with DeadlineExceeded")
}

func TestCloseWithPendingRPC(t *testing.T) {
	defer leakcheck.Check(t)
	r, cleanup := manual.GenerateAndRegisterManualResolver()
	defer cleanup()

	test, err := startTestServers(1)
	if err != nil {
		t.Fatalf("failed to start servers: %v", err)
	}
	defer test.cleanup()

	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName(weightedroundrobin.Name))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	testc := testpb.NewTestServiceClient(cc)

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// This RPC blocks until cc is closed.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if _, err := testc.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) == codes.DeadlineExceeded {
				t.Errorf("RPC failed because of deadline after cc is closed; want error the client connection is closing")
			}
			cancel()
		}()
	}
	cc.Close()
	wg.Wait()
}

func TestNewAddressWhileBlocking(t *testing.T) {
	defer leakcheck.Check(t)
	r, cleanup := manual.GenerateAndRegisterManualResolver()
	defer cleanup()

	test, err := startTestServers(1)
	if err != nil {
		t.Fatalf("failed to start servers: %v", err)
	}
	defer test.cleanup()

	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName(weightedroundrobin.Name))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()
	testc := testpb.NewTestServiceClient(cc)
	// The first RPC should fail because there's no address.
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := testc.EmptyCall(ctx, &testpb.Empty{}); err == nil || status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() = _, %v, want _, DeadlineExceeded", err)
	}

	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: test.addresses[0]}}})
	// The second RPC should succeed.
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := testc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() = _, %v, want _, nil", err)
	}

	r.UpdateState(resolver.State{Addresses: []resolver.Address{}})

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// This RPC blocks until NewAddress is called.
			testc.EmptyCall(context.Background(), &testpb.Empty{})
		}()
	}
	time.Sleep(50 * time.Millisecond)
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: test.addresses[0]}}})
	wg.Wait()
}

func TestOneServerDown(t *testing.T) {
	defer leakcheck.Check(t)
	r, cleanup := manual.GenerateAndRegisterManualResolver()
	defer cleanup()

	backendCount := 3
	test, err := startTestServers(backendCount)
	if err != nil {
		t.Fatalf("failed to start servers: %v", err)
	}
	defer test.cleanup()

	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName(weightedroundrobin.Name))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()
	testc := testpb.NewTestServiceClient(cc)
	// The first RPC should fail because there's no address.
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := testc.EmptyCall(ctx, &testpb.Empty{}); err == nil || status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() = _, %v, want _, DeadlineExceeded", err)
	}

	var resolvedAddrs []resolver.Address
	for i := 0; i < backendCount; i++ {
		resolvedAddrs = append(resolvedAddrs,
			resolver.Address{Addr: test.addresses[i], Metadata: &weightedroundrobin.AddrInfo{Weight: uint32(i + 1)}})
	}

	r.UpdateState(resolver.State{Addresses: resolvedAddrs})
	var p peer.Peer
	// Make sure connections to all servers are up.
	for si := 0; si < backendCount; si++ {
		var connected bool
		for i := 0; i < 1000; i++ {
			if _, err := testc.EmptyCall(context.Background(), &testpb.Empty{}, grpc.Peer(&p)); err != nil {
				t.Fatalf("EmptyCall() = _, %v, want _, <nil>", err)
			}
			if p.Addr.String() == test.addresses[si] {
				connected = true
				break
			}
			time.Sleep(time.Millisecond)
		}
		if !connected {
			t.Fatalf("Connection to %v was not up after more than 1 second", test.addresses[si])
		}
	}

	checkWeighedRoundRobin(t, testc, resolvedAddrs)

	// Stop one server, RPCs should roundrobin among the remaining servers.
	backendCount--
	test.servers[backendCount].Stop()

	checkWeighedRoundRobin(t, testc, resolvedAddrs[0:2])
}

func TestAllServersDown(t *testing.T) {
	defer leakcheck.Check(t)
	r, cleanup := manual.GenerateAndRegisterManualResolver()
	defer cleanup()

	backendCount := 3
	test, err := startTestServers(backendCount)
	if err != nil {
		t.Fatalf("failed to start servers: %v", err)
	}
	defer test.cleanup()

	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName(weightedroundrobin.Name))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()
	testc := testpb.NewTestServiceClient(cc)
	// The first RPC should fail because there's no address.
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := testc.EmptyCall(ctx, &testpb.Empty{}); err == nil || status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() = _, %v, want _, DeadlineExceeded", err)
	}

	var resolvedAddrs []resolver.Address
	for i := 0; i < backendCount; i++ {
		resolvedAddrs = append(resolvedAddrs,
			resolver.Address{Addr: test.addresses[i], Metadata: &weightedroundrobin.AddrInfo{Weight: uint32(i + 1)}})
	}

	r.UpdateState(resolver.State{Addresses: resolvedAddrs})
	var p peer.Peer
	// Make sure connections to all servers are up.
	for si := 0; si < backendCount; si++ {
		var connected bool
		for i := 0; i < 1000; i++ {
			if _, err := testc.EmptyCall(context.Background(), &testpb.Empty{}, grpc.Peer(&p)); err != nil {
				t.Fatalf("EmptyCall() = _, %v, want _, <nil>", err)
			}
			if p.Addr.String() == test.addresses[si] {
				connected = true
				break
			}
			time.Sleep(time.Millisecond)
		}
		if !connected {
			t.Fatalf("Connection to %v was not up after more than 1 second", test.addresses[si])
		}
	}

	checkWeighedRoundRobin(t, testc, resolvedAddrs)

	// All servers are stopped, failfast RPC should fail with unavailable.
	for i := 0; i < backendCount; i++ {
		test.servers[i].Stop()
	}
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 1000; i++ {
		if _, err := testc.EmptyCall(context.Background(), &testpb.Empty{}); status.Code(err) == codes.Unavailable {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Failfast RPCs didn't fail with Unavailable after all servers are stopped")
}
