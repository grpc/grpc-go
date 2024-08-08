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
	"net"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/balancer"
	pickfirstleaf "google.golang.org/grpc/balancer/pickfirst_leaf"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
)

const stateRecordingPickFirstLeafBalancerName = "state_recording_pick_first_leaf_balancer"

var testPickFirstLeafBalancerBuilder = newStateRecordingBalancerBuilder(stateRecordingPickFirstLeafBalancerName, pickfirstleaf.Name)

func init() {
	balancer.Register(testPickFirstLeafBalancerBuilder)
}

// These tests use a pipeListener. This listener is similar to net.Listener
// except that it is unbuffered, so each read and write will wait for the other
// side's corresponding write or read.
func (s) TestPickFirstLeafStateTransitions_SingleAddress(t *testing.T) {
	for _, test := range []struct {
		desc   string
		want   []connectivity.State
		server func(net.Listener) net.Conn
	}{
		{
			desc: "When the server returns server preface, the client enters READY.",
			want: []connectivity.State{
				connectivity.Connecting,
				connectivity.Ready,
			},
			server: func(lis net.Listener) net.Conn {
				conn, err := lis.Accept()
				if err != nil {
					t.Error(err)
					return nil
				}

				go keepReading(conn)

				framer := http2.NewFramer(conn, conn)
				if err := framer.WriteSettings(http2.Setting{}); err != nil {
					t.Errorf("Error while writing settings frame. %v", err)
					return nil
				}

				return conn
			},
		},
		{
			desc: "When the connection is closed before the preface is sent, the client enters TRANSIENT FAILURE.",
			want: []connectivity.State{
				connectivity.Connecting,
				connectivity.TransientFailure,
			},
			server: func(lis net.Listener) net.Conn {
				conn, err := lis.Accept()
				if err != nil {
					t.Error(err)
					return nil
				}

				conn.Close()
				return nil
			},
		},
		{
			desc: `When the server sends its connection preface, but the connection dies before the client can write its
connection preface, the client enters TRANSIENT FAILURE.`,
			want: []connectivity.State{
				connectivity.Connecting,
				connectivity.TransientFailure,
			},
			server: func(lis net.Listener) net.Conn {
				conn, err := lis.Accept()
				if err != nil {
					t.Error(err)
					return nil
				}

				framer := http2.NewFramer(conn, conn)
				if err := framer.WriteSettings(http2.Setting{}); err != nil {
					t.Errorf("Error while writing settings frame. %v", err)
					return nil
				}

				conn.Close()
				return nil
			},
		},
		{
			desc: `When the server reads the client connection preface but does not send its connection preface, the
client enters TRANSIENT FAILURE.`,
			want: []connectivity.State{
				connectivity.Connecting,
				connectivity.TransientFailure,
			},
			server: func(lis net.Listener) net.Conn {
				conn, err := lis.Accept()
				if err != nil {
					t.Error(err)
					return nil
				}

				go keepReading(conn)

				return conn
			},
		},
	} {
		t.Log(test.desc)
		testStateTransitionSingleAddress(t, test.want, test.server, testPickFirstLeafBalancerBuilder)
	}
}

// When a READY connection is closed, the client enters IDLE then CONNECTING.
func (s) TestPickFirstLeafStateTransitions_ReadyToConnecting(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening. Err: %v", err)
	}
	defer lis.Close()

	sawReady := make(chan struct{}, 1)
	defer close(sawReady)

	// Launch the server.
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			t.Error(err)
			return
		}

		go keepReading(conn)

		framer := http2.NewFramer(conn, conn)
		if err := framer.WriteSettings(http2.Setting{}); err != nil {
			t.Errorf("Error while writing settings frame. %v", err)
			return
		}

		// Prevents race between onPrefaceReceipt and onClose.
		<-sawReady

		conn.Close()
	}()

	client, err := grpc.Dial(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, stateRecordingPickFirstLeafBalancerName)))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	go testutils.StayConnected(ctx, client)

	stateNotifications := testPickFirstLeafBalancerBuilder.nextStateNotifier()

	want := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Idle,
		connectivity.Shutdown,
		connectivity.Connecting,
	}
	for i := 0; i < len(want); i++ {
		select {
		case <-time.After(defaultTestTimeout):
			t.Fatalf("timed out waiting for state %d (%v) in flow %v", i, want[i], want)
		case seen := <-stateNotifications:
			if seen == connectivity.Ready {
				sawReady <- struct{}{}
			}
			if seen != want[i] {
				t.Fatalf("expected to see %v at position %d in flow %v, got %v", want[i], i, want, seen)
			}
		}
	}
}

// When the first connection is closed, the client stays in CONNECTING until it
// tries the second address (which succeeds, and then it enters READY).
func (s) TestPickFirstLeafStateTransitions_TriesAllAddrsBeforeTransientFailure(t *testing.T) {
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

		go keepReading(conn)

		framer := http2.NewFramer(conn, conn)
		if err := framer.WriteSettings(http2.Setting{}); err != nil {
			t.Errorf("Error while writing settings frame. %v", err)
			return
		}

		close(server2Done)
	}()

	rb := manual.NewBuilderWithScheme("whatever")
	rb.InitialState(resolver.State{Addresses: []resolver.Address{
		{Addr: lis1.Addr().String()},
		{Addr: lis2.Addr().String()},
	}})
	client, err := grpc.Dial("whatever:///this-gets-overwritten",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, stateRecordingPickFirstLeafBalancerName)),
		grpc.WithResolvers(rb),
		grpc.WithConnectParams(grpc.ConnectParams{
			// Set a really long back-off delay to ensure the first subConn does
			// not enter ready before the second subConn connects.
			Backoff: backoff.Config{
				BaseDelay: 1 * time.Hour,
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	stateNotifications := testPickFirstLeafBalancerBuilder.nextStateNotifier()
	want := []connectivity.State{
		connectivity.Connecting,
		connectivity.TransientFailure,
		connectivity.Connecting,
		connectivity.Ready,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for i := 0; i < len(want); i++ {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for state %d (%v) in flow %v", i, want[i], want)
		case seen := <-stateNotifications:
			if seen != want[i] {
				t.Fatalf("expected to see %v at position %d in flow %v, got %v", want[i], i, want, seen)
			}
		}
	}
	select {
	case <-ctx.Done():
		t.Fatal("saw the correct state transitions, but timed out waiting for client to finish interactions with server 1")
	case <-server1Done:
	}
	select {
	case <-ctx.Done():
		t.Fatal("saw the correct state transitions, but timed out waiting for client to finish interactions with server 2")
	case <-server2Done:
	}
}

// When there are multiple addresses, and we enter READY on one of them, a
// later closure should cause the client to enter CONNECTING
func (s) TestPickFirstLeafStateTransitions_MultipleAddrsEntersReady(t *testing.T) {
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
	sawReady := make(chan struct{}, 1)
	defer close(sawReady)

	// Launch server 1.
	go func() {
		conn, err := lis1.Accept()
		if err != nil {
			t.Error(err)
			return
		}

		go keepReading(conn)

		framer := http2.NewFramer(conn, conn)
		if err := framer.WriteSettings(http2.Setting{}); err != nil {
			t.Errorf("Error while writing settings frame. %v", err)
			return
		}

		<-sawReady

		conn.Close()

		close(server1Done)
	}()

	rb := manual.NewBuilderWithScheme("whatever")
	rb.InitialState(resolver.State{Addresses: []resolver.Address{
		{Addr: lis1.Addr().String()},
		{Addr: lis2.Addr().String()},
	}})
	client, err := grpc.Dial("whatever:///this-gets-overwritten",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, stateRecordingPickFirstLeafBalancerName)),
		grpc.WithResolvers(rb))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	go testutils.StayConnected(ctx, client)

	stateNotifications := testPickFirstLeafBalancerBuilder.nextStateNotifier()
	want := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Shutdown, // The second subConn is closed once the first one connects.
		connectivity.Idle,
		connectivity.Shutdown, // The subConn will be closed and pickfirst will run on the latest address list.
		connectivity.Connecting,
	}
	for i := 0; i < len(want); i++ {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for state %d (%v) in flow %v", i, want[i], want)
		case seen := <-stateNotifications:
			if seen == connectivity.Ready {
				sawReady <- struct{}{}
			}
			if seen != want[i] {
				t.Fatalf("expected to see %v at position %d in flow %v, got %v", want[i], i, want, seen)
			}
		}
	}
	select {
	case <-ctx.Done():
		t.Fatal("saw the correct state transitions, but timed out waiting for client to finish interactions with server 1")
	case <-server1Done:
	}
}

// TestPickFirstLeafConnectivityStateSubscriber confirms updates sent by the balancer in
// rapid succession are not missed by the subscriber.
func (s) TestPickFirstLeafConnectivityStateSubscriber(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	sendStates := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Idle,
		connectivity.Connecting,
		connectivity.Idle,
		connectivity.Connecting,
		connectivity.Ready,
	}
	wantStates := append(sendStates, connectivity.Shutdown)

	const testBalName = "any"
	bf := stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, _ balancer.ClientConnState) error {
			// Send the expected states in rapid succession.
			for _, s := range sendStates {
				t.Logf("Sending state update %s", s)
				bd.ClientConn.UpdateState(balancer.State{ConnectivityState: s})
			}
			return nil
		},
	}
	stub.Register(testBalName, bf)

	// Create the ClientConn.
	const testResName = "any"
	rb := manual.NewBuilderWithScheme(testResName)
	cc, err := grpc.Dial(testResName+":///",
		grpc.WithResolvers(rb),
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, testBalName)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Unexpected error from grpc.Dial: %v", err)
	}

	// Subscribe to state updates.  Use a buffer size of 1 to allow the
	// Shutdown state to go into the channel when Close()ing.
	connCh := make(chan connectivity.State, 1)
	s := &funcConnectivityStateSubscriber{
		onMsg: func(s connectivity.State) {
			select {
			case connCh <- s:
			case <-ctx.Done():
			}
			if s == connectivity.Shutdown {
				close(connCh)
			}
		},
	}

	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, s)

	// Send an update from the resolver that will trigger the LB policy's UpdateClientConnState.
	go rb.UpdateState(resolver.State{})

	// Verify the resulting states.
	for i, want := range wantStates {
		if i == len(sendStates) {
			// Trigger Shutdown to be sent by the channel.  Use a goroutine to
			// ensure the operation does not block.
			cc.Close()
		}
		select {
		case got := <-connCh:
			if got != want {
				t.Errorf("Update %v was %s; want %s", i, got, want)
			} else {
				t.Logf("Update %v was %s as expected", i, got)
			}
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for state update %v: %s", i, want)
		}
	}
}
