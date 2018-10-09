/*
 *
 * Copyright 2018 gRPC authors.
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
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"
	"golang.org/x/net/http2"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/leakcheck"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
)

var testBalancer = &stateRecordingBalancer{}

func init() {
	balancer.Register(&stateRecordingBalancerBuilder{name: "state_recording_balancer", b: testBalancer})
}

func TestStateTransitions_SingleAddress(t *testing.T) {
	for i, test := range []struct {
		want   []connectivity.State
		server func(net.Listener)
	}{
		// When the server returns server preface, the client enters READY.
		{
			want: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
			},
			server: func(lis net.Listener) {
				conn, err := lis.Accept()
				if err != nil {
					t.Error(err)
					return
				}

				framer := http2.NewFramer(conn, conn)
				if err := framer.WriteSettings(http2.Setting{}); err != nil {
					t.Errorf("Error while writing settings frame. %v", err)
					return
				}
			},
		},
		// When the connection is closed, the client enters TRANSIENT FAILURE.
		{
			want: []connectivity.State{
				connectivity.Connecting,
				connectivity.TransientFailure,
			},
			server: func(lis net.Listener) {
				conn, err := lis.Accept()
				if err != nil {
					t.Error(err)
					return
				}

				conn.Close()
			},
		},
		// When a READY connection is closed, the client enters TRANSIENT FAILURE before CONNECTING.
		{
			want: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
				connectivity.TransientFailure,
				connectivity.Connecting,
			},
			server: func(lis net.Listener) {
				conn, err := lis.Accept()
				if err != nil {
					t.Error(err)
					return
				}

				framer := http2.NewFramer(conn, conn)
				if err := framer.WriteSettings(http2.Setting{}); err != nil {
					t.Errorf("Error while writing settings frame. %v", err)
					return
				}

				// Prevents race between onPrefaceReceipt and onClose.
				time.Sleep(50 * time.Millisecond)

				conn.Close()
			},
		},
	} {
		t.Logf("Test %d", i)
		testStateTransitionSingleAddress(t, test.want, test.server)
	}
}

func testStateTransitionSingleAddress(t *testing.T, want []connectivity.State, server func(net.Listener)) {
	defer leakcheck.Check(t)

	stateNotifications := make(chan connectivity.State, len(want))
	testBalancer.ResetNotifier(stateNotifications)
	defer close(stateNotifications)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening. Err: %v", err)
	}
	defer lis.Close()

	// Launch the server.
	go server(lis)

	client, err := DialContext(ctx, lis.Addr().String(), WithWaitForHandshake(), WithInsecure(), WithBalancerName("state_recording_balancer"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	timeout := time.After(5 * time.Second)

	for i := 0; i < len(want); i++ {
		select {
		case <-timeout:
			t.Fatalf("timed out waiting for state %d (%v) in flow %v", i, want[i], want)
		case seen := <-stateNotifications:
			if seen != want[i] {
				t.Fatalf("expected to see %v at position %d in flow %v, got %v", want[i], i, want, seen)
			}
		}
	}
	select {
	case seen := <-stateNotifications:
		t.Fatalf("unexpectedly saw extra state %v after flow %v", seen, want)
	default:
	}
}

func TestStateTransitions_TwoAddresses(t *testing.T) {
	for i, test := range []struct {
		want    []connectivity.State
		server1 func(net.Listener, chan struct{})
		server2 func(net.Listener, chan struct{})
	}{
		// When the first connection is closed, the client enters stays in CONNECTING until it tries the second
		// address before transitioning to TRANSIENT FAILURE.
		{
			want: []connectivity.State{
				connectivity.Connecting,
				connectivity.TransientFailure,
			},
			server1: func(lis net.Listener, done chan struct{}) {
				conn, err := lis.Accept()
				if err != nil {
					t.Error(err)
					return
				}

				conn.Close()
				close(done)
			},
			server2: func(lis net.Listener, done chan struct{}) {
				conn, err := lis.Accept()
				if err != nil {
					t.Error(err)
					return
				}

				conn.Close()
				close(done)
			},
		},
	} {
		t.Logf("Test %d", i)
		testStateTransitionTwoAddresses(t, test.want, test.server1, test.server2)
	}
}

func testStateTransitionTwoAddresses(t *testing.T, want []connectivity.State, server1, server2 func(net.Listener, chan struct{})) {
	defer leakcheck.Check(t)

	stateNotifications := make(chan connectivity.State, len(want))
	testBalancer.ResetNotifier(stateNotifications)
	defer close(stateNotifications)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	lis1, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening. Err: %v", err)
	}
	defer lis1.Close()

	lis2, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening. Err: %v", err)
	}
	defer lis2.Close()

	server1Done := make(chan struct{})
	server2Done := make(chan struct{})

	// Launch the server.
	go server1(lis1, server1Done)
	go server2(lis2, server2Done)

	rb := manual.NewBuilderWithScheme("whatever")
	rb.InitialAddrs([]resolver.Address{
		{Addr: lis1.Addr().String()},
		{Addr: lis2.Addr().String()},
	})
	client, err := DialContext(ctx, "this-gets-overwritten", WithInsecure(), WithWaitForHandshake(), WithBalancerName("state_recording_balancer"), withResolverBuilder(rb))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	timeout := time.After(2 * time.Second)

	for i := 0; i < len(want); i++ {
		select {
		case <-timeout:
			t.Fatalf("timed out waiting for state %d (%v) in flow %v", i, want[i], want)
		case seen := <-stateNotifications:
			if seen != want[i] {
				t.Fatalf("expected to see %v at position %d in flow %v, got %v", want[i], i, want, seen)
			}
		}
	}
	select {
	case <-timeout:
		t.Fatal("saw the correct state transitions, but timed out waiting for client to finish interactions with server 1")
	case <-server1Done:
	}
	select {
	case <-timeout:
		t.Fatal("saw the correct state transitions, but timed out waiting for client to finish interactions with server 2")
	case <-server2Done:
	}
	select {
	case seen := <-stateNotifications:
		t.Fatalf("unexpectedly saw extra state %v after flow %v", seen, want)
	default:
	}
}

type stateRecordingBalancer struct {
	mu       sync.Mutex
	notifier chan<- connectivity.State

	underlyingBalancer balancer.Balancer
}

func (b *stateRecordingBalancer) HandleResolvedAddrs(addrs []resolver.Address, err error) {
	b.underlyingBalancer.HandleResolvedAddrs(addrs, err)
}

func (b *stateRecordingBalancer) HandleSubConnStateChange(sc balancer.SubConn, s connectivity.State) {
	b.mu.Lock()
	b.notifier <- s
	b.mu.Unlock()

	b.underlyingBalancer.HandleSubConnStateChange(sc, s)
}

func (b *stateRecordingBalancer) ResetNotifier(r chan<- connectivity.State) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.notifier = r
}

func (b *stateRecordingBalancer) Close() {
	b.mu.Lock()
	u := b.underlyingBalancer
	b.mu.Unlock()
	u.Close()
}

type stateRecordingBalancerBuilder struct {
	name string
	b    *stateRecordingBalancer
}

func (bb *stateRecordingBalancerBuilder) Name() string {
	return bb.name
}

func (bb *stateRecordingBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	bb.b.mu.Lock()
	bb.b.underlyingBalancer = balancer.Get(PickFirstBalancerName).Build(cc, opts)
	bb.b.mu.Unlock()
	return bb.b
}
