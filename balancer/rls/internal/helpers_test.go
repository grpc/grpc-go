/*
 *
 * Copyright 2021 gRPC authors.
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

package rls

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer/rls/internal/test/e2e"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancergroup"
	"google.golang.org/grpc/internal/grpctest"
	rlspb "google.golang.org/grpc/internal/proto/grpc_lookup_v1"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"
	testgrpc "google.golang.org/grpc/test/grpc_testing"
	testpb "google.golang.org/grpc/test/grpc_testing"
	"google.golang.org/protobuf/types/known/durationpb"
)

// TODO(easwars): Remove this once all RLS code is merged.
//lint:file-ignore U1000 Ignore all unused code, not all code is merged yet.

const (
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 100 * time.Millisecond
)

func init() {
	balancergroup.DefaultSubBalancerCloseTimeout = time.Millisecond
}

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// connWrapper wraps a net.Conn and pushes on a channel when closed.
type connWrapper struct {
	net.Conn
	closeCh *testutils.Channel
}

func (cw *connWrapper) Close() error {
	err := cw.Conn.Close()
	cw.closeCh.Replace(nil)
	return err
}

// listenerWrapper wraps a net.Listener and the returned net.Conn.
//
// It pushes on a channel whenever it accepts a new connection.
type listenerWrapper struct {
	net.Listener
	newConnCh *testutils.Channel
}

func (l *listenerWrapper) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	closeCh := testutils.NewChannel()
	conn := &connWrapper{Conn: c, closeCh: closeCh}
	l.newConnCh.Send(conn)
	return conn, nil
}

func newListenerWrapper(t *testing.T, lis net.Listener) *listenerWrapper {
	if lis == nil {
		var err error
		lis, err = testutils.LocalTCPListener()
		if err != nil {
			t.Fatal(err)
		}
	}

	return &listenerWrapper{
		Listener:  lis,
		newConnCh: testutils.NewChannel(),
	}
}

// fakeBackoffStrategy is a fake implementation of the backoff.Strategy
// interface, for tests to inject the backoff duration.
type fakeBackoffStrategy struct {
	backoff time.Duration
}

func (f *fakeBackoffStrategy) Backoff(retries int) time.Duration {
	return f.backoff
}

// fakeThrottler is a fake implementation of the adaptiveThrottler interface.
type fakeThrottler struct {
	throttleFunc func() bool
}

func (f *fakeThrottler) ShouldThrottle() bool         { return f.throttleFunc() }
func (f *fakeThrottler) RegisterBackendResponse(bool) {}

// alwaysThrottlingThrottler returns a fake throttler which always throttles.
func alwaysThrottlingThrottler() *fakeThrottler {
	return &fakeThrottler{throttleFunc: func() bool { return true }}
}

// neverThrottlingThrottler returns a fake throttler which never throttles.
func neverThrottlingThrottler() *fakeThrottler {
	return &fakeThrottler{throttleFunc: func() bool { return false }}
}

// oneTimeAllowingThrottler returns a fake throttler which does not throttle the
// first request, but throttles everything that comes after. This is useful for
// tests which need to set up a valid cache entry before testing other cases.
func oneTimeAllowingThrottler() *fakeThrottler {
	var once sync.Once
	return &fakeThrottler{
		throttleFunc: func() bool {
			throttle := true
			once.Do(func() { throttle = false })
			return throttle
		},
	}
}

func overrideAdaptiveThrottler(t *testing.T, f *fakeThrottler) {
	origAdaptiveThrottler := newAdaptiveThrottler
	newAdaptiveThrottler = func() adaptiveThrottler { return f }
	t.Cleanup(func() { newAdaptiveThrottler = origAdaptiveThrottler })
}

// setupFakeRLSServer starts and returns a fake RouteLookupService server
// listening on the given listener or on a random local port. Also returns a
// channel for tests to get notified whenever the RouteLookup RPC is invoked on
// the fake server.
//
// This function sets up the fake server to respond with an empty response for
// the RouteLookup RPCs. Tests can override this by calling the
// SetResponseCallback() method on the returned fake server.
func setupFakeRLSServer(t *testing.T, lis net.Listener, opts ...grpc.ServerOption) (*e2e.FakeRouteLookupServer, chan struct{}) {
	s, cancel := e2e.StartFakeRouteLookupServer(t, lis, opts...)
	t.Logf("Started fake RLS server at %q", s.Address)

	ch := make(chan struct{}, 1)
	s.SetRequestCallback(func(request *rlspb.RouteLookupRequest) {
		select {
		case ch <- struct{}{}:
		default:
		}
	})
	t.Cleanup(cancel)
	return s, ch
}

// buildBasicRLSConfig constructs a basic service config for the RLS LB policy
// which header matching rules. This expects the passed child policy name to
// have been registered by the caller.
func buildBasicRLSConfig(childPolicyName, rlsServerAddress string) *e2e.RLSConfig {
	return &e2e.RLSConfig{
		RouteLookupConfig: &rlspb.RouteLookupConfig{
			GrpcKeybuilders: []*rlspb.GrpcKeyBuilder{
				{
					Names: []*rlspb.GrpcKeyBuilder_Name{{Service: "grpc.testing.TestService"}},
					Headers: []*rlspb.NameMatcher{
						{Key: "k1", Names: []string{"n1"}},
						{Key: "k2", Names: []string{"n2"}},
					},
				},
			},
			LookupService:        rlsServerAddress,
			LookupServiceTimeout: durationpb.New(defaultTestTimeout),
			CacheSizeBytes:       1024,
		},
		ChildPolicy:                      &internalserviceconfig.BalancerConfig{Name: childPolicyName},
		ChildPolicyConfigTargetFieldName: e2e.RLSChildPolicyTargetNameField,
	}
}

// buildBasicRLSConfigWithChildPolicy constructs a very basic service config for
// the RLS LB policy. It also registers a test LB policy which is capable of
// being a child of the RLS LB policy.
func buildBasicRLSConfigWithChildPolicy(t *testing.T, childPolicyName, rlsServerAddress string) *e2e.RLSConfig {
	childPolicyName = "test-child-policy" + childPolicyName
	e2e.RegisterRLSChildPolicy(childPolicyName, nil)
	t.Logf("Registered child policy with name %q", childPolicyName)

	return &e2e.RLSConfig{
		RouteLookupConfig: &rlspb.RouteLookupConfig{
			GrpcKeybuilders:      []*rlspb.GrpcKeyBuilder{{Names: []*rlspb.GrpcKeyBuilder_Name{{Service: "grpc.testing.TestService"}}}},
			LookupService:        rlsServerAddress,
			LookupServiceTimeout: durationpb.New(defaultTestTimeout),
			CacheSizeBytes:       1024,
		},
		ChildPolicy:                      &internalserviceconfig.BalancerConfig{Name: childPolicyName},
		ChildPolicyConfigTargetFieldName: e2e.RLSChildPolicyTargetNameField,
	}
}

// startBackend starts a backend implementing the TestService on a local port.
// It returns a channel for tests to get notified whenever an RPC is invoked on
// the backend. This allows tests to ensure that RPCs reach expected backends.
// Also returns the address of the backend.
func startBackend(t *testing.T, sopts ...grpc.ServerOption) (rpcCh chan struct{}, address string) {
	t.Helper()

	rpcCh = make(chan struct{}, 1)
	backend := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			select {
			case rpcCh <- struct{}{}:
			default:
			}
			return &testpb.Empty{}, nil
		},
	}
	if err := backend.StartServer(sopts...); err != nil {
		t.Fatalf("Failed to start backend: %v", err)
	}
	t.Logf("Started TestService backend at: %q", backend.Address)
	t.Cleanup(func() { backend.Stop() })
	return rpcCh, backend.Address
}

// startManualResolverWithConfig registers and returns a manual resolver which
// pushes the RLS LB policy's service config on the channel.
func startManualResolverWithConfig(t *testing.T, rlsConfig *e2e.RLSConfig) *manual.Resolver {
	t.Helper()

	scJSON, err := rlsConfig.ServiceConfigJSON()
	if err != nil {
		t.Fatal(err)
	}

	sc := internal.ParseServiceConfigForTesting.(func(string) *serviceconfig.ParseResult)(scJSON)
	r := manual.NewBuilderWithScheme("rls-e2e")
	r.InitialState(resolver.State{ServiceConfig: sc})
	t.Cleanup(r.Close)
	return r
}

// makeTestRPCAndExpectItToReachBackend is a test helper function which makes
// the EmptyCall RPC on the given ClientConn and verifies that it reaches a
// backend. The latter is accomplished by listening on the provided channel
// which gets pushed to whenever the backend in question gets an RPC.
func makeTestRPCAndExpectItToReachBackend(ctx context.Context, t *testing.T, cc *grpc.ClientConn, ch chan struct{}) {
	t.Helper()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("TestService/EmptyCall() failed with error: %v", err)
	}
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout when waiting for backend to receive RPC")
	case <-ch:
	}
}

// makeTestRPCAndVerifyError is a test helper function which makes the EmptyCall
// RPC on the given ClientConn and verifies that the RPC fails with the given
// status code and error.
func makeTestRPCAndVerifyError(ctx context.Context, t *testing.T, cc *grpc.ClientConn, wantCode codes.Code, wantErr error) {
	t.Helper()

	client := testgrpc.NewTestServiceClient(cc)
	_, err := client.EmptyCall(ctx, &testpb.Empty{})
	if err == nil {
		t.Fatal("TestService/EmptyCall() succeeded when expected to fail")
	}
	if code := status.Code(err); code != wantCode {
		t.Fatalf("TestService/EmptyCall() returned code: %v, want: %v", code, wantCode)
	}
	if wantErr != nil && !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("TestService/EmptyCall() returned err: %v, want: %v", err, wantErr)
	}
}

// verifyRLSRequest is a test helper which listens on a channel to see if an RLS
// request was received by the fake RLS server. Based on whether the test
// expects a request to be sent out or not, it uses a different timeout.
func verifyRLSRequest(t *testing.T, ch chan struct{}, wantRequest bool) {
	t.Helper()

	if wantRequest {
		select {
		case <-time.After(defaultTestTimeout):
			t.Fatalf("Timeout when waiting for an RLS request to be sent out")
		case <-ch:
		}
	} else {
		select {
		case <-time.After(defaultTestShortTimeout):
		case <-ch:
			t.Fatalf("RLS request sent out when not expecting one")
		}
	}
}
