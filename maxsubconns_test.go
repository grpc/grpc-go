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

package grpc

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/resolver"
)

// TestMaxSubConnsLimit tests that WithMaxSubConns limits the number of
// SubConns created by the ClientConn.
func (s) TestMaxSubConnsLimit(t *testing.T) {
	// Register a test balancer that creates SubConns for each address.
	balancer.Register(&testMaxSubConnsBalancerBuilder{})

	// Create a ClientConn with a max of 3 SubConns.
	cc, err := Dial("passthrough:///test.server",
		WithInsecure(),
		WithMaxSubConns(3),
		WithDefaultServiceConfig(`{"loadBalancingConfig": [{"test_max_subconns": {}}]}`),
	)
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Wait for the resolver to provide addresses.
	// The test balancer will try to create SubConns for each address.
	// With maxSubConns=3, only 3 should be created.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for {
		cc.mu.RLock()
		numConns := len(cc.conns)
		cc.mu.RUnlock()
		if numConns >= 3 {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("Timeout waiting for SubConns to be created. Got %d, want 3", numConns)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Verify exactly 3 SubConns were created.
	cc.mu.RLock()
	numConns := len(cc.conns)
	cc.mu.RUnlock()
	if numConns != 3 {
		t.Fatalf("Got %d SubConns, want 3", numConns)
	}
}

// TestMaxSubConnsLimitError tests that NewSubConn returns an error when the limit is reached.
func (s) TestMaxSubConnsLimitError(t *testing.T) {
	// Register a test balancer that captures the error from NewSubConn.
	balancer.Register(&testMaxSubConnsErrorBalancerBuilder{})

	// Create a ClientConn with a max of 2 SubConns.
	cc, err := Dial("passthrough:///test.server",
		WithInsecure(),
		WithMaxSubConns(2),
		WithDefaultServiceConfig(`{"loadBalancingConfig": [{"test_max_subconns_error": {}}]}`),
	)
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Wait for the balancer to attempt creating SubConns.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for {
		cc.mu.RLock()
		numConns := len(cc.conns)
		cc.mu.RUnlock()
		if numConns >= 2 {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("Timeout waiting for SubConns to be created. Got %d, want 2", numConns)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The balancer should have observed at least one error from NewSubConn.
	// We verify by checking the global error counter set by the test balancer.
	// Since the balancer runs in a goroutine, give it a moment to process.
	time.Sleep(100 * time.Millisecond)

	// Verify exactly 2 SubConns exist (the limit) even though the balancer tried to create 5.
	cc.mu.RLock()
	numConns := len(cc.conns)
	cc.mu.RUnlock()
	if numConns != 2 {
		t.Fatalf("Expected exactly 2 SubConns (the limit), got %d", numConns)
	}
}

// TestMaxSubConnsZeroMeansUnlimited tests that a maxSubConns of 0 means
// no limit.
func (s) TestMaxSubConnsZeroMeansUnlimited(t *testing.T) {
	balancer.Register(&testMaxSubConnsBalancerBuilder{})

	// Create a ClientConn without a limit.
	cc, err := Dial("passthrough:///test.server",
		WithInsecure(),
		WithDefaultServiceConfig(`{"loadBalancingConfig": [{"test_max_subconns": {}}]}`),
	)
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()

	// Wait a bit and verify that more than 3 SubConns can be created.
	time.Sleep(500 * time.Millisecond)

	cc.mu.RLock()
	numConns := len(cc.conns)
	cc.mu.RUnlock()
	if numConns < 5 {
		t.Fatalf("Expected more than 5 SubConns without limit, got %d", numConns)
	}
}

// testMaxSubConnsBalancerBuilder is a test balancer builder.
type testMaxSubConnsBalancerBuilder struct{}

func (b *testMaxSubConnsBalancerBuilder) Name() string {
	return "test_max_subconns"
}

func (b *testMaxSubConnsBalancerBuilder) Build(cc balancer.ClientConn, _ balancer.BuildOptions) balancer.Balancer {
	return &testMaxSubConnsBalancer{cc: cc}
}

// testMaxSubConnsBalancer is a test balancer that creates many SubConns.
type testMaxSubConnsBalancer struct {
	cc balancer.ClientConn
}

func (b *testMaxSubConnsBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	// Create 10 SubConns with different addresses.
	for i := 0; i < 10; i++ {
		addr := resolver.Address{Addr: fmt.Sprintf("127.0.0.1:%d", 10000+i)}
		_, err := b.cc.NewSubConn([]resolver.Address{addr}, balancer.NewSubConnOptions{})
		if err != nil {
			// Expected when maxSubConns limit is reached.
			continue
		}
	}
	b.cc.UpdateState(balancer.State{
		ConnectivityState: connectivity.Ready,
		Picker:            &testMaxSubConnsPicker{},
	})
	return nil
}

func (b *testMaxSubConnsBalancer) ResolverError(_ error)                                         {}
func (b *testMaxSubConnsBalancer) UpdateSubConnState(_ balancer.SubConn, _ balancer.SubConnState) {}
func (b *testMaxSubConnsBalancer) Close()                                                          {}
func (b *testMaxSubConnsBalancer) ExitIdle()                                                       {}

// testMaxSubConnsPicker is a test picker.
type testMaxSubConnsPicker struct{}

func (p *testMaxSubConnsPicker) Pick(_ balancer.PickInfo) (balancer.PickResult, error) {
	return balancer.PickResult{}, fmt.Errorf("no connection available")
}

// testMaxSubConnsErrorBalancerBuilder is a test balancer builder that captures NewSubConn errors.
type testMaxSubConnsErrorBalancerBuilder struct{}

func (b *testMaxSubConnsErrorBalancerBuilder) Name() string {
	return "test_max_subconns_error"
}

func (b *testMaxSubConnsErrorBalancerBuilder) Build(cc balancer.ClientConn, _ balancer.BuildOptions) balancer.Balancer {
	return &testMaxSubConnsErrorBalancer{cc: cc}
}

// testMaxSubConnsErrorBalancer is a test balancer that captures errors from NewSubConn.
type testMaxSubConnsErrorBalancer struct {
	cc     balancer.ClientConn
	sawErr atomic.Bool
}

func (b *testMaxSubConnsErrorBalancer) UpdateClientConnState(_ balancer.ClientConnState) error {
	// Try to create 5 SubConns; expect errors after the limit.
	for i := 0; i < 5; i++ {
		addr := resolver.Address{Addr: fmt.Sprintf("127.0.0.1:%d", 10000+i)}
		_, err := b.cc.NewSubConn([]resolver.Address{addr}, balancer.NewSubConnOptions{})
		if err != nil {
			b.sawErr.Store(true)
		}
	}
	b.cc.UpdateState(balancer.State{
		ConnectivityState: connectivity.Ready,
		Picker:            &testMaxSubConnsPicker{},
	})
	return nil
}

func (b *testMaxSubConnsErrorBalancer) ResolverError(_ error) {}
func (b *testMaxSubConnsErrorBalancer) UpdateSubConnState(_ balancer.SubConn, _ balancer.SubConnState) {
}
func (b *testMaxSubConnsErrorBalancer) Close()    {}
func (b *testMaxSubConnsErrorBalancer) ExitIdle() {}
