/*
 *
 * Copyright 2023 gRPC authors.
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

package idle_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

func init() {
	channelz.TurnOn()
}

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestTimeout          = 10 * time.Second
	defaultTestShortTimeout     = 100 * time.Millisecond
	defaultTestShortIdleTimeout = 500 * time.Millisecond
)

// channelzTraceEventFound looks up the top-channels in channelz (expects a
// single one), and checks if there is a trace event on the channel matching the
// provided description string.
func channelzTraceEventFound(ctx context.Context, wantDesc string) error {
	for ctx.Err() == nil {
		tcs, _ := channelz.GetTopChannels(0, 0)
		if l := len(tcs); l != 1 {
			return fmt.Errorf("when looking for channelz trace event with description %q, found %d top-level channels, want 1", wantDesc, l)
		}
		if tcs[0].Trace == nil {
			return fmt.Errorf("when looking for channelz trace event with description %q, no trace events found for top-level channel", wantDesc)
		}

		for _, e := range tcs[0].Trace.Events {
			if strings.Contains(e.Desc, wantDesc) {
				return nil
			}
		}
	}
	return fmt.Errorf("when looking for channelz trace event with description %q, %w", wantDesc, ctx.Err())
}

// channelzTraceEventNotFound looks up the top-channels in channelz (expects a
// single one), and verifies that there is no trace event on the channel
// matching the provided description string.
func channelzTraceEventNotFound(ctx context.Context, wantDesc string) error {
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()

	err := channelzTraceEventFound(sCtx, wantDesc)
	if err == nil {
		return fmt.Errorf("found channelz trace event with description %q, when expected not to", wantDesc)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

// Tests the case where channel idleness is disabled by passing an idle_timeout
// of 0. Verifies that a READY channel with no RPCs does not move to IDLE.
func (s) TestChannelIdleness_Disabled_NoActivity(t *testing.T) {
	// Create a ClientConn with idle_timeout set to 0.
	r := manual.NewBuilderWithScheme("whatever")
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithIdleTimeout(0), // Disable idleness.
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`),
	}
	cc, err := grpc.Dial(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	// Start a test backend and push an address update via the resolver.
	backend := stubserver.StartTestService(t, nil)
	t.Cleanup(backend.Stop)
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: backend.Address}}})

	// Verify that the ClientConn moves to READY.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	// Verify that the ClientConn stay in READY.
	sCtx, sCancel := context.WithTimeout(ctx, 3*defaultTestShortIdleTimeout)
	defer sCancel()
	testutils.AwaitNoStateChange(sCtx, t, cc, connectivity.Ready)

	// Verify that there are no idleness related channelz events.
	if err := channelzTraceEventNotFound(ctx, "entering idle mode"); err != nil {
		t.Fatal(err)
	}
	if err := channelzTraceEventNotFound(ctx, "exiting idle mode"); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where channel idleness is enabled by passing a small value for
// idle_timeout. Verifies that a READY channel with no RPCs moves to IDLE, and
// the connection to the backend is closed.
func (s) TestChannelIdleness_Enabled_NoActivity(t *testing.T) {
	// Create a ClientConn with a short idle_timeout.
	r := manual.NewBuilderWithScheme("whatever")
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithIdleTimeout(defaultTestShortIdleTimeout),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`),
	}
	cc, err := grpc.Dial(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	// Start a test backend and push an address update via the resolver.
	lis := testutils.NewListenerWrapper(t, nil)
	backend := stubserver.StartTestService(t, &stubserver.StubServer{Listener: lis})
	t.Cleanup(backend.Stop)
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: backend.Address}}})

	// Verify that the ClientConn moves to READY.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	// Retrieve the wrapped conn from the listener.
	v, err := lis.NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Failed to retrieve conn from test listener: %v", err)
	}
	conn := v.(*testutils.ConnWrapper)

	// Verify that the ClientConn moves to IDLE as there is no activity.
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Verify idleness related channelz events.
	if err := channelzTraceEventFound(ctx, "entering idle mode"); err != nil {
		t.Fatal(err)
	}

	// Verify that the previously open connection is closed.
	if _, err := conn.CloseCh.Receive(ctx); err != nil {
		t.Fatalf("Failed when waiting for connection to be closed after channel entered IDLE: %v", err)
	}
}

// Tests the case where channel idleness is enabled by passing a small value for
// idle_timeout. Verifies that a READY channel with an ongoing RPC stays READY.
func (s) TestChannelIdleness_Enabled_OngoingCall(t *testing.T) {
	tests := []struct {
		name    string
		makeRPC func(ctx context.Context, client testgrpc.TestServiceClient) error
	}{
		{
			name: "unary",
			makeRPC: func(ctx context.Context, client testgrpc.TestServiceClient) error {
				if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
					return fmt.Errorf("EmptyCall RPC failed: %v", err)
				}
				return nil
			},
		},
		{
			name: "streaming",
			makeRPC: func(ctx context.Context, client testgrpc.TestServiceClient) error {
				stream, err := client.FullDuplexCall(ctx)
				if err != nil {
					t.Fatalf("FullDuplexCall RPC failed: %v", err)
				}
				if _, err := stream.Recv(); err != nil && err != io.EOF {
					t.Fatalf("stream.Recv() failed: %v", err)
				}
				return nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Create a ClientConn with a short idle_timeout.
			r := manual.NewBuilderWithScheme("whatever")
			dopts := []grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithResolvers(r),
				grpc.WithIdleTimeout(defaultTestShortIdleTimeout),
				grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`),
			}
			cc, err := grpc.Dial(r.Scheme()+":///test.server", dopts...)
			if err != nil {
				t.Fatalf("grpc.Dial() failed: %v", err)
			}
			t.Cleanup(func() { cc.Close() })

			// Start a test backend which keeps a unary RPC call active by blocking on a
			// channel that is closed by the test later on. Also push an address update
			// via the resolver.
			blockCh := make(chan struct{})
			backend := &stubserver.StubServer{
				EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
					<-blockCh
					return &testpb.Empty{}, nil
				},
				FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
					<-blockCh
					return nil
				},
			}
			if err := backend.StartServer(); err != nil {
				t.Fatalf("Failed to start backend: %v", err)
			}
			t.Cleanup(backend.Stop)
			r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: backend.Address}}})

			// Verify that the ClientConn moves to READY.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			testutils.AwaitState(ctx, t, cc, connectivity.Ready)

			// Spawn a goroutine which checks expected state transitions and idleness
			// channelz trace events.
			errCh := make(chan error, 1)
			go func() {
				defer close(blockCh)

				// Verify that the ClientConn stays in READY.
				sCtx, sCancel := context.WithTimeout(ctx, 3*defaultTestShortIdleTimeout)
				defer sCancel()
				if cc.WaitForStateChange(sCtx, connectivity.Ready) {
					errCh <- fmt.Errorf("state changed from %q to %q when no state change was expected", connectivity.Ready, cc.GetState())
					return
				}

				// Verify that there are no idleness related channelz events.
				//
				// TODO: Improve the checks here. If these log strings are
				// changed in the code, these checks will continue to pass.
				if err := channelzTraceEventNotFound(ctx, "entering idle mode"); err != nil {
					errCh <- err
					return
				}
				errCh <- channelzTraceEventNotFound(ctx, "exiting idle mode")
			}()

			if err := test.makeRPC(ctx, testgrpc.NewTestServiceClient(cc)); err != nil {
				t.Fatalf("%s rpc failed: %v", test.name, err)
			}

			select {
			case err := <-errCh:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatalf("Timeout when trying to verify that an active RPC keeps channel from moving to IDLE")
			}
		})
	}
}

// Tests the case where channel idleness is enabled by passing a small value for
// idle_timeout. Verifies that activity on a READY channel (frequent and short
// RPCs) keeps it from moving to IDLE.
func (s) TestChannelIdleness_Enabled_ActiveSinceLastCheck(t *testing.T) {
	// Create a ClientConn with a short idle_timeout.
	r := manual.NewBuilderWithScheme("whatever")
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithIdleTimeout(defaultTestShortIdleTimeout),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`),
	}
	cc, err := grpc.Dial(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	// Start a test backend and push an address update via the resolver.
	backend := stubserver.StartTestService(t, nil)
	t.Cleanup(backend.Stop)
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: backend.Address}}})

	// Verify that the ClientConn moves to READY.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	// For a duration of three times the configured idle timeout, making RPCs
	// every now and then and ensure that the channel does not move out of
	// READY.
	sCtx, sCancel := context.WithTimeout(ctx, 3*defaultTestShortIdleTimeout)
	defer sCancel()
	go func() {
		for ; sCtx.Err() == nil; <-time.After(defaultTestShortIdleTimeout / 4) {
			client := testgrpc.NewTestServiceClient(cc)
			if _, err := client.EmptyCall(sCtx, &testpb.Empty{}); err != nil {
				// While iterating through this for loop, at some point in time,
				// the context deadline will expire. It is safe to ignore that
				// error code.
				if status.Code(err) != codes.DeadlineExceeded {
					t.Errorf("EmptyCall RPC failed: %v", err)
					return
				}
			}
		}
	}()

	// Verify that the ClientConn stay in READY.
	testutils.AwaitNoStateChange(sCtx, t, cc, connectivity.Ready)

	// Verify that there are no idleness related channelz events.
	if err := channelzTraceEventNotFound(ctx, "entering idle mode"); err != nil {
		t.Fatal(err)
	}
	if err := channelzTraceEventNotFound(ctx, "exiting idle mode"); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where channel idleness is enabled by passing a small value for
// idle_timeout. Verifies that a READY channel with no RPCs moves to IDLE. Also
// verifies that a subsequent RPC on the IDLE channel kicks it out of IDLE.
func (s) TestChannelIdleness_Enabled_ExitIdleOnRPC(t *testing.T) {
	// Start a test backend and set the bootstrap state of the resolver to
	// include this address. This will ensure that when the resolver is
	// restarted when exiting idle, it will push the same address to grpc again.
	r := manual.NewBuilderWithScheme("whatever")
	backend := stubserver.StartTestService(t, nil)
	t.Cleanup(backend.Stop)
	r.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backend.Address}}})

	// Create a ClientConn with a short idle_timeout.
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithIdleTimeout(defaultTestShortIdleTimeout),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`),
	}
	cc, err := grpc.Dial(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	// Verify that the ClientConn moves to READY.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	// Verify that the ClientConn moves to IDLE as there is no activity.
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Verify idleness related channelz events.
	if err := channelzTraceEventFound(ctx, "entering idle mode"); err != nil {
		t.Fatal(err)
	}

	// Make an RPC and ensure that it succeeds and moves the channel back to
	// READY.
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall RPC failed: %v", err)
	}
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)
	if err := channelzTraceEventFound(ctx, "exiting idle mode"); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where channel idleness is enabled by passing a small value for
// idle_timeout. Simulates a race between the idle timer firing and RPCs being
// initiated, after a period of inactivity on the channel.
//
// After a period of inactivity (for the configured idle timeout duration), when
// RPCs are started, there are two possibilities:
//   - the idle timer wins the race and puts the channel in idle. The RPCs then
//     kick it out of idle.
//   - the RPCs win the race, and therefore the channel never moves to idle.
//
// In either of these cases, all RPCs must succeed.
func (s) TestChannelIdleness_Enabled_IdleTimeoutRacesWithRPCs(t *testing.T) {
	// Start a test backend and set the bootstrap state of the resolver to
	// include this address. This will ensure that when the resolver is
	// restarted when exiting idle, it will push the same address to grpc again.
	r := manual.NewBuilderWithScheme("whatever")
	backend := stubserver.StartTestService(t, nil)
	t.Cleanup(backend.Stop)
	r.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backend.Address}}})

	// Create a ClientConn with a short idle_timeout.
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithIdleTimeout(defaultTestShortTimeout),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`),
	}
	cc, err := grpc.Dial(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	// Verify that the ClientConn moves to READY.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Errorf("EmptyCall RPC failed: %v", err)
	}

	// Make an RPC every defaultTestShortTimeout duration so as to race with the
	// idle timeout. Whether the idle timeout wins the race or the RPC wins the
	// race, RPCs must succeed.
	for i := 0; i < 20; i++ {
		<-time.After(defaultTestShortTimeout)
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			t.Fatalf("EmptyCall RPC failed: %v", err)
		}
		t.Logf("Iteration %d succeeded", i)
	}
}

// Tests the case where the channel is IDLE and we call cc.Connect.
func (s) TestChannelIdleness_Connect(t *testing.T) {
	// Start a test backend and set the bootstrap state of the resolver to
	// include this address. This will ensure that when the resolver is
	// restarted when exiting idle, it will push the same address to grpc again.
	r := manual.NewBuilderWithScheme("whatever")
	backend := stubserver.StartTestService(t, nil)
	t.Cleanup(backend.Stop)
	r.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backend.Address}}})

	// Create a ClientConn with a short idle_timeout.
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithIdleTimeout(defaultTestShortIdleTimeout),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`),
	}
	cc, err := grpc.Dial(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })

	// Verify that the ClientConn moves to IDLE.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Connect should exit channel idleness.
	cc.Connect()

	// Verify that the ClientConn moves back to READY.
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)
}
