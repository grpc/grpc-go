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

package test

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

// Keep reading until something causes the connection to die (EOF, server
// closed, etc). Useful as a tool for mindlessly keeping the connection
// healthy, since the client will error if things like client prefaces are not
// accepted in a timely fashion.
func keepReading(conn net.Conn) {
	io.Copy(io.Discard, conn)
}

type funcConnectivityStateSubscriber struct {
	onMsg func(connectivity.State)
}

func (f *funcConnectivityStateSubscriber) OnMessage(msg any) {
	f.onMsg(msg.(connectivity.State))
}

func waitForState(ctx context.Context, t *testing.T, stateCh <-chan connectivity.State, want connectivity.State) {
	t.Helper()
	select {
	case gotState := <-stateCh:
		if gotState != want {
			t.Fatalf("State is %s; want %s", gotState, want)
		}
		t.Logf("State is %s as expected", gotState)
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for state update: %s", want)
	}
}

// Tests for state transitions in various scenarios with a single address.
func (s) TestStateTransitions_SingleAddress(t *testing.T) {
	for _, test := range []struct {
		desc       string
		wantStates []connectivity.State
		server     func(net.Listener) net.Conn
	}{
		{
			desc: "ServerSendsPreface",
			wantStates: []connectivity.State{
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
			desc: "ConnectionClosesBeforeServerPreface",
			wantStates: []connectivity.State{
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
			desc: "ConnectionClosesBeforeClientPreface",
			wantStates: []connectivity.State{
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
			desc: "ServerNeverSendsPreface",
			wantStates: []connectivity.State{
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
		t.Run(test.desc, func(t *testing.T) {
			testStateTransitionSingleAddress(t, test.wantStates, test.server)
		})
	}
}

func testStateTransitionSingleAddress(t *testing.T, wantStates []connectivity.State, server func(net.Listener) net.Conn) {
	pl := testutils.NewPipeListener()
	defer pl.Close()

	// Launch the server.
	var conn net.Conn
	var connMu sync.Mutex
	go func() {
		connMu.Lock()
		conn = server(pl)
		connMu.Unlock()
	}()

	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDialer(pl.Dialer()),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff:           backoff.Config{},
			MinConnectTimeout: 100 * time.Millisecond,
		}),
	}
	cc, err := grpc.NewClient("passthrough:///", dopts...)
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()

	// Ensure that the client is in IDLE before connecting.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Subscribe to state updates.
	stateCh := make(chan connectivity.State, 1)
	s := &funcConnectivityStateSubscriber{
		onMsg: func(s connectivity.State) {
			select {
			case stateCh <- s:
			case <-ctx.Done():
			}
		},
	}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, s)

	cc.Connect()
	for _, wantState := range wantStates {
		waitForState(ctx, t, stateCh, wantState)
	}

	connMu.Lock()
	defer connMu.Unlock()
	if conn != nil {
		err = conn.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

// Tests for state transitions when the READY connection is closed.
func (s) TestStateTransitions_ReadyToConnecting(t *testing.T) {
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

	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()

	// Ensure that the client is in IDLE before connecting.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Subscribe to state updates.
	stateCh := make(chan connectivity.State, 1)
	s := &funcConnectivityStateSubscriber{
		onMsg: func(s connectivity.State) {
			select {
			case stateCh <- s:
			case <-ctx.Done():
			}
		},
	}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, s)

	cc.Connect()
	wantStates := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Idle,
		connectivity.Connecting,
	}
	for _, wantState := range wantStates {
		waitForState(ctx, t, stateCh, wantState)
		if wantState == connectivity.Ready {
			sawReady <- struct{}{}
		}
		if wantState == connectivity.Idle {
			cc.Connect()
		}
	}
}

// Tests for state transitions when there are multiple addresses and all the
// addresses fail.
func (s) TestStateTransitions_TriesAllAddrsBeforeTransientFailure(t *testing.T) {
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

		conn.Close()
		close(server2Done)
	}()

	rb := manual.NewBuilderWithScheme("whatever")
	rb.InitialState(resolver.State{Addresses: []resolver.Address{
		{Addr: lis1.Addr().String()},
		{Addr: lis2.Addr().String()},
	}})

	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(rb),
		grpc.WithConnectParams(grpc.ConnectParams{
			// Set a really long back-off delay to ensure the subchannels stay
			// in TRANSIENT_FAILURE and not enter IDLE.
			Backoff: backoff.Config{BaseDelay: 1 * time.Hour},
		}),
	}
	cc, err := grpc.NewClient("whatever:///this-gets-overwritten", dopts...)
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()

	// Ensure that the client is in IDLE before connecting.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Subscribe to state updates.
	stateCh := make(chan connectivity.State, 1)
	s := &funcConnectivityStateSubscriber{
		onMsg: func(s connectivity.State) {
			select {
			case stateCh <- s:
			case <-ctx.Done():
			}
		},
	}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, s)

	cc.Connect()
	wantStates := []connectivity.State{
		connectivity.Connecting,
		connectivity.TransientFailure,
	}
	for _, wantState := range wantStates {
		waitForState(ctx, t, stateCh, wantState)
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

// Tests for state transitions with multiple addresses when the READY connection
// is closed.
func (s) TestStateTransitions_MultipleAddrsEntersReady(t *testing.T) {
	lis1, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening. Err: %v", err)
	}
	defer lis1.Close()

	// Never actually gets used; we just want it to be alive so that the
	// resolver has two addresses to target.
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
	cc, err := grpc.NewClient("whatever:///this-gets-overwritten", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(rb))
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()

	// Ensure that the client is in IDLE before connecting.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Subscribe to state updates.
	stateCh := make(chan connectivity.State, 1)
	s := &funcConnectivityStateSubscriber{
		onMsg: func(s connectivity.State) {
			select {
			case stateCh <- s:
			case <-ctx.Done():
			}
		},
	}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, s)

	cc.Connect()
	wantStates := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
		connectivity.Idle,
		connectivity.Connecting,
	}
	for _, wantState := range wantStates {
		waitForState(ctx, t, stateCh, wantState)
		if wantState == connectivity.Ready {
			sawReady <- struct{}{}
		}
		if wantState == connectivity.Idle {
			cc.Connect()
		}
	}
	select {
	case <-ctx.Done():
		t.Fatal("saw the correct state transitions, but timed out waiting for client to finish interactions with server 1")
	case <-server1Done:
	}
}

// TestConnectivityStateSubscriber confirms updates sent by the balancer in
// rapid succession are not missed by the subscriber.
func (s) TestConnectivityStateSubscriber(t *testing.T) {
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
	cc, err := grpc.NewClient(testResName+":///",
		grpc.WithResolvers(rb),
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, testBalName)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	cc.Connect()
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

// Test verifies that a channel starts off in IDLE and transitions to CONNECTING
// when Connect() is called, and stays there when there are no resolver updates.
func (s) TestStateTransitions_WithConnect_NoResolverUpdate(t *testing.T) {
	backend := stubserver.StartTestService(t, nil)
	defer backend.Stop()

	mr := manual.NewBuilderWithScheme("e2e-test")
	defer mr.Close()

	cc, err := grpc.NewClient(mr.Scheme()+":///", grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}
	defer cc.Close()

	if state := cc.GetState(); state != connectivity.Idle {
		t.Fatalf("Expected initial state to be IDLE, got %v", state)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// The channel should transition to CONNECTING automatically when Connect()
	// is called.
	cc.Connect()
	testutils.AwaitState(ctx, t, cc, connectivity.Connecting)

	// Verify that the channel remains in CONNECTING state for a short time.
	shortCtx, shortCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer shortCancel()
	testutils.AwaitNoStateChange(shortCtx, t, cc, connectivity.Connecting)
}

// Test verifies that a channel starts off in IDLE and transitions to CONNECTING
// when Connect() is called, and stays there when there are no resolver updates.
func (s) TestStateTransitions_WithRPC_NoResolverUpdate(t *testing.T) {
	backend := stubserver.StartTestService(t, nil)
	defer backend.Stop()

	mr := manual.NewBuilderWithScheme("e2e-test")
	defer mr.Close()

	cc, err := grpc.NewClient(mr.Scheme()+":///", grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}
	defer cc.Close()

	if state := cc.GetState(); state != connectivity.Idle {
		t.Fatalf("Expected initial state to be IDLE, got %v", state)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Make an RPC call to transition the channel to CONNECTING.
	go func() {
		if _, err := testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{}); err == nil {
			t.Errorf("Expected RPC to fail, but it succeeded")
		}
	}()

	// The channel should transition to CONNECTING automatically when an RPC
	// is made.
	testutils.AwaitState(ctx, t, cc, connectivity.Connecting)

	// The channel remains in CONNECTING state for a short time.
	shortCtx, shortCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer shortCancel()
	testutils.AwaitNoStateChange(shortCtx, t, cc, connectivity.Connecting)
}

const testResolverBuildFailureScheme = "test-resolver-build-failure"

// testResolverBuilder is a resolver builder that fails the first time its
// Build method is called, and succeeds thereafter.
type testResolverBuilder struct {
	logger interface {
		Logf(format string, args ...any)
	}
	buildCalled bool
	manualR     *manual.Resolver
}

func (b *testResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	b.logger.Logf("testResolverBuilder: Build called with target: %v", target)
	if !b.buildCalled {
		b.buildCalled = true
		b.logger.Logf("testResolverBuilder: returning build failure")
		return nil, fmt.Errorf("simulated resolver build failure")
	}
	return b.manualR.Build(target, cc, opts)
}

func (b *testResolverBuilder) Scheme() string {
	return testResolverBuildFailureScheme
}

// Tests for state transitions when the resolver initially fails to build.
func (s) TestStateTransitions_ResolverBuildFailure(t *testing.T) {
	tests := []struct {
		name            string
		exitIdleWithRPC bool
	}{
		{
			name:            "exitIdleByConnecting",
			exitIdleWithRPC: false,
		},
		{
			name:            "exitIdleByRPC",
			exitIdleWithRPC: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := manual.NewBuilderWithScheme("whatever" + tt.name)
			backend := stubserver.StartTestService(t, nil)
			defer backend.Stop()
			mr.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backend.Address}}})

			dopts := []grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithResolvers(&testResolverBuilder{logger: t, manualR: mr}),
			}

			cc, err := grpc.NewClient(testResolverBuildFailureScheme+":///", dopts...)
			if err != nil {
				t.Fatalf("Failed to create new client: %v", err)
			}
			defer cc.Close()

			// Ensure that the client is in IDLE before connecting.
			if state := cc.GetState(); state != connectivity.Idle {
				t.Fatalf("Expected initial state to be IDLE, got %v", state)
			}

			// Subscribe to state updates.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			stateCh := make(chan connectivity.State, 1)
			s := &funcConnectivityStateSubscriber{
				onMsg: func(s connectivity.State) {
					select {
					case stateCh <- s:
					case <-ctx.Done():
					}
				},
			}
			internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, s)

			if tt.exitIdleWithRPC {
				// The first attempt to kick the channel is expected to return
				// the resolver build error to the RPC.
				const wantErr = "simulated resolver build failure"
				for range 2 {
					_, err := testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{})
					if code := status.Code(err); code != codes.Unavailable {
						t.Fatalf("EmptyCall RPC failed with code %v, want %v", err, codes.Unavailable)
					}
					if err == nil || !strings.Contains(err.Error(), wantErr) {
						t.Fatalf("EmptyCall RPC failed with error: %q, want %q", err, wantErr)
					}
				}
			} else {
				cc.Connect()
			}

			wantStates := []connectivity.State{
				connectivity.Connecting,       // When channel exits IDLE for the first time.
				connectivity.TransientFailure, // Resolver build failure.
				connectivity.Idle,             // After idle timeout.
				connectivity.Connecting,       // When channel exits IDLE again.
				connectivity.Ready,            // Successful resolver build and connection to backend.
			}
			for _, wantState := range wantStates {
				waitForState(ctx, t, stateCh, wantState)
				switch wantState {
				case connectivity.TransientFailure:
					internal.EnterIdleModeForTesting.(func(*grpc.ClientConn))(cc)
				case connectivity.Idle:
					if tt.exitIdleWithRPC {
						if _, err := testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{}); err != nil {
							t.Fatalf("EmptyCall RPC failed: %v", err)
						}
					} else {
						cc.Connect()
					}
				}
			}
		})
	}
}

// Tests for state transitions when the resolver reports no addresses.
func (s) TestStateTransitions_WithRPC_ResolverUpdateContainsNoAddresses(t *testing.T) {
	mr := manual.NewBuilderWithScheme("e2e-test")
	mr.InitialState(resolver.State{})
	defer mr.Close()

	cc, err := grpc.NewClient(mr.Scheme()+":///", grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}
	defer cc.Close()

	if state := cc.GetState(); state != connectivity.Idle {
		t.Fatalf("Expected initial state to be IDLE, got %v", state)
	}

	// Subscribe to state updates.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	stateCh := make(chan connectivity.State, 1)
	s := &funcConnectivityStateSubscriber{
		onMsg: func(s connectivity.State) {
			select {
			case stateCh <- s:
			case <-ctx.Done():
			}
		},
	}
	internal.SubscribeToConnectivityStateChanges.(func(cc *grpc.ClientConn, s grpcsync.Subscriber) func())(cc, s)

	// Make an RPC call to transition the channel to CONNECTING.
	const wantErr = "name resolver error: produced zero addresses"
	for range 2 {
		_, err := testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{})
		if code := status.Code(err); code != codes.Unavailable {
			t.Errorf("EmptyCall RPC failed with code %v, want %v", err, codes.Unavailable)
		}
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("EmptyCall RPC failed with error: %q, want %q", err, wantErr)
		}
	}

	wantStates := []connectivity.State{
		connectivity.Connecting,       // When channel exits IDLE for the first time.
		connectivity.TransientFailure, // No endpoints from the resolver
		connectivity.Idle,             // After idle timeout.
	}
	for _, wantState := range wantStates {
		waitForState(ctx, t, stateCh, wantState)
		if wantState == connectivity.TransientFailure {
			internal.EnterIdleModeForTesting.(func(*grpc.ClientConn))(cc)
		}
	}
}
