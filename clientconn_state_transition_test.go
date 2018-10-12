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

const stateRecordingBalancerName = "state_recoding_balancer"

var testBalancer = &stateRecordingBalancer{}

func init() {
	balancer.Register(testBalancer)
}

func TestStateTransitions_SingleAddress(t *testing.T) {
	for _, test := range []struct {
		name   string
		want   []connectivity.State
		server func(net.Listener)
	}{
		// When the server returns server preface, the client enters READY.
		{
			name: "ServerEntersReadyOnPrefaceReceipt",
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
			name: "ServerEntersTransientFailureOnClose",
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
	} {
		t.Logf("Test %s", test.name)
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

	client, err := DialContext(ctx, lis.Addr().String(), WithWaitForHandshake(), WithInsecure(), WithBalancerName(stateRecordingBalancerName))
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
}

// When a READY connection is closed, the client enters TRANSIENT FAILURE before CONNECTING.
func TestStateTransition_ReadyToTransientFailure(t *testing.T) {
	defer leakcheck.Check(t)

	want := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.TransientFailure,
		connectivity.Connecting,
	}

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

	sawReady := make(chan struct{})

	// Launch the server.
	go func() {
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
		<-sawReady

		conn.Close()
	}()

	client, err := DialContext(ctx, lis.Addr().String(), WithWaitForHandshake(), WithInsecure(), WithBalancerName(stateRecordingBalancerName))
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
			if seen == connectivity.Ready {
				close(sawReady)
			}
			if seen != want[i] {
				t.Fatalf("expected to see %v at position %d in flow %v, got %v", want[i], i, want, seen)
			}
		}
	}
}

// When the first connection is closed, the client enters stays in CONNECTING until it tries the second
// address (which succeeds, and then it enters READY).
func TestStateTransitions_TriesAllAddrsBeforeTransientFailure(t *testing.T) {
	defer leakcheck.Check(t)

	want := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
	}

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

	// Launch server 1.
	go func() {
		conn, err := lis1.Accept()
		if err != nil {
			t.Error(err)
			return
		}

		conn.Close()
		close(server1Done)
	}()
	// Launch server 2.
	go func() {
		conn, err := lis2.Accept()
		if err != nil {
			t.Error(err)
			return
		}

		framer := http2.NewFramer(conn, conn)
		if err := framer.WriteSettings(http2.Setting{}); err != nil {
			t.Errorf("Error while writing settings frame. %v", err)
			return
		}
		close(server2Done)
	}()

	rb := manual.NewBuilderWithScheme("whatever")
	rb.InitialAddrs([]resolver.Address{
		{Addr: lis1.Addr().String()},
		{Addr: lis2.Addr().String()},
	})
	client, err := DialContext(ctx, "this-gets-overwritten", WithInsecure(), WithWaitForHandshake(), WithBalancerName(stateRecordingBalancerName), withResolverBuilder(rb))
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
	case <-timeout:
		t.Fatal("saw the correct state transitions, but timed out waiting for client to finish interactions with server 1")
	case <-server1Done:
	}
	select {
	case <-timeout:
		t.Fatal("saw the correct state transitions, but timed out waiting for client to finish interactions with server 2")
	case <-server2Done:
	}
}

// When there are multiple addresses, and we enter READY on one of them, a later closure should cause
// the client to enter TRANSIENT FAILURE before it re-enters CONNECTING.
func TestStateTransitions_MultipleAddrsEntersReady(t *testing.T) {
	defer leakcheck.Check(t)

	want := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.TransientFailure,
		connectivity.Connecting,
	}

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

	// Never actually gets used; we just want it to be alive so that the resolver has two addresses to target.
	lis2, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening. Err: %v", err)
	}
	defer lis2.Close()

	server1Done := make(chan struct{})
	sawReady := make(chan struct{})

	// Launch server 1.
	go func() {
		conn, err := lis1.Accept()
		if err != nil {
			t.Error(err)
			return
		}

		framer := http2.NewFramer(conn, conn)
		if err := framer.WriteSettings(http2.Setting{}); err != nil {
			t.Errorf("Error while writing settings frame. %v", err)
			return
		}

		<-sawReady

		conn.Close()

		_, err = lis1.Accept()
		if err != nil {
			t.Error(err)
			return
		}

		close(server1Done)
	}()

	rb := manual.NewBuilderWithScheme("whatever")
	rb.InitialAddrs([]resolver.Address{
		{Addr: lis1.Addr().String()},
		{Addr: lis2.Addr().String()},
	})
	client, err := DialContext(ctx, "this-gets-overwritten", WithInsecure(), WithWaitForHandshake(), WithBalancerName(stateRecordingBalancerName), withResolverBuilder(rb))
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
			if seen == connectivity.Ready {
				close(sawReady)
			}
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
}

type stateRecordingBalancer struct {
	mu       sync.Mutex
	notifier chan<- connectivity.State

	balancer.Balancer
}

func (b *stateRecordingBalancer) HandleSubConnStateChange(sc balancer.SubConn, s connectivity.State) {
	b.mu.Lock()
	b.notifier <- s
	b.mu.Unlock()

	b.Balancer.HandleSubConnStateChange(sc, s)
}

func (b *stateRecordingBalancer) ResetNotifier(r chan<- connectivity.State) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.notifier = r
}

func (b *stateRecordingBalancer) Close() {
	b.mu.Lock()
	u := b.Balancer
	b.mu.Unlock()
	u.Close()
}

func (b *stateRecordingBalancer) Name() string {
	return stateRecordingBalancerName
}

func (b *stateRecordingBalancer) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	b.mu.Lock()
	b.Balancer = balancer.Get(PickFirstBalancerName).Build(cc, opts)
	b.mu.Unlock()
	return b
}
