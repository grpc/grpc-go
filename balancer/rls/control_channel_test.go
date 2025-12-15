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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpcsync"
	rlspb "google.golang.org/grpc/internal/proto/grpc_lookup_v1"
	"google.golang.org/grpc/internal/testutils"
	rlstest "google.golang.org/grpc/internal/testutils/rls"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/testdata"
	"google.golang.org/protobuf/proto"
)

// TestControlChannelThrottled tests the case where the adaptive throttler
// indicates that the control channel needs to be throttled.
func (s) TestControlChannelThrottled(t *testing.T) {
	// Start an RLS server and set the throttler to always throttle requests.
	rlsServer, rlsReqCh := rlstest.SetupFakeRLSServer(t, nil)
	overrideAdaptiveThrottler(t, alwaysThrottlingThrottler())

	// Create a control channel to the fake RLS server.
	ctrlCh, err := newControlChannel(rlsServer.Address, "", defaultTestTimeout, balancer.BuildOptions{}, nil)
	if err != nil {
		t.Fatalf("Failed to create control channel to RLS server: %v", err)
	}
	defer ctrlCh.close()

	// Perform the lookup and expect the attempt to be throttled.
	ctrlCh.lookup(nil, rlspb.RouteLookupRequest_REASON_MISS, staleHeaderData, nil)

	select {
	case <-rlsReqCh:
		t.Fatal("RouteLookup RPC invoked when control channel is throttled")
	case <-time.After(defaultTestShortTimeout):
	}
}

// TestLookupFailure tests the case where the RLS server responds with an error.
func (s) TestLookupFailure(t *testing.T) {
	// Start an RLS server and set the throttler to never throttle requests.
	rlsServer, _ := rlstest.SetupFakeRLSServer(t, nil)
	overrideAdaptiveThrottler(t, neverThrottlingThrottler())

	// Setup the RLS server to respond with errors.
	rlsServer.SetResponseCallback(func(context.Context, *rlspb.RouteLookupRequest) *rlstest.RouteLookupResponse {
		return &rlstest.RouteLookupResponse{Err: errors.New("rls failure")}
	})

	// Create a control channel to the fake RLS server.
	ctrlCh, err := newControlChannel(rlsServer.Address, "", defaultTestTimeout, balancer.BuildOptions{}, nil)
	if err != nil {
		t.Fatalf("Failed to create control channel to RLS server: %v", err)
	}
	defer ctrlCh.close()

	// Perform the lookup and expect the callback to be invoked with an error.
	errCh := make(chan error, 1)
	ctrlCh.lookup(nil, rlspb.RouteLookupRequest_REASON_MISS, staleHeaderData, func(_ []string, _ string, err error) {
		if err == nil {
			errCh <- errors.New("rlsClient.lookup() succeeded, should have failed")
			return
		}
		errCh <- nil
	})

	select {
	case <-time.After(defaultTestTimeout):
		t.Fatal("timeout when waiting for lookup callback to be invoked")
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestLookupDeadlineExceeded tests the case where the RLS server does not
// respond within the configured rpc timeout.
func (s) TestLookupDeadlineExceeded(t *testing.T) {
	// A unary interceptor which returns a status error with DeadlineExceeded.
	interceptor := func(context.Context, any, *grpc.UnaryServerInfo, grpc.UnaryHandler) (resp any, err error) {
		return nil, status.Error(codes.DeadlineExceeded, "deadline exceeded")
	}

	// Start an RLS server and set the throttler to never throttle.
	rlsServer, _ := rlstest.SetupFakeRLSServer(t, nil, grpc.UnaryInterceptor(interceptor))
	overrideAdaptiveThrottler(t, neverThrottlingThrottler())

	// Create a control channel with a small deadline.
	ctrlCh, err := newControlChannel(rlsServer.Address, "", defaultTestShortTimeout, balancer.BuildOptions{}, nil)
	if err != nil {
		t.Fatalf("Failed to create control channel to RLS server: %v", err)
	}
	defer ctrlCh.close()

	// Perform the lookup and expect the callback to be invoked with an error.
	errCh := make(chan error)
	ctrlCh.lookup(nil, rlspb.RouteLookupRequest_REASON_MISS, staleHeaderData, func(_ []string, _ string, err error) {
		if st, ok := status.FromError(err); !ok || st.Code() != codes.DeadlineExceeded {
			errCh <- fmt.Errorf("rlsClient.lookup() returned error: %v, want %v", err, codes.DeadlineExceeded)
			return
		}
		errCh <- nil
	})

	select {
	case <-time.After(defaultTestTimeout):
		t.Fatal("timeout when waiting for lookup callback to be invoked")
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	}
}

// testCredsBundle wraps a test call creds and real transport creds.
type testCredsBundle struct {
	transportCreds credentials.TransportCredentials
	callCreds      credentials.PerRPCCredentials
}

func (f *testCredsBundle) TransportCredentials() credentials.TransportCredentials {
	return f.transportCreds
}

func (f *testCredsBundle) PerRPCCredentials() credentials.PerRPCCredentials {
	return f.callCreds
}

func (f *testCredsBundle) NewWithMode(mode string) (credentials.Bundle, error) {
	if mode != internal.CredsBundleModeFallback {
		return nil, fmt.Errorf("unsupported mode: %v", mode)
	}
	return &testCredsBundle{
		transportCreds: f.transportCreds,
		callCreds:      f.callCreds,
	}, nil
}

var (
	// Call creds sent by the testPerRPCCredentials on the client, and verified
	// by an interceptor on the server.
	perRPCCredsData = map[string]string{
		"test-key":     "test-value",
		"test-key-bin": string([]byte{1, 2, 3}),
	}
)

type testPerRPCCredentials struct {
	callCreds map[string]string
}

func (f *testPerRPCCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return f.callCreds, nil
}

func (f *testPerRPCCredentials) RequireTransportSecurity() bool {
	return true
}

// Unary server interceptor which validates if the RPC contains call credentials
// which match `perRPCCredsData
func callCredsValidatingServerInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.PermissionDenied, "didn't find metadata in context")
	}
	for k, want := range perRPCCredsData {
		got, ok := md[k]
		if !ok {
			return ctx, status.Errorf(codes.PermissionDenied, "didn't find call creds key %v in context", k)
		}
		if got[0] != want {
			return ctx, status.Errorf(codes.PermissionDenied, "for key %v, got value %v, want %v", k, got, want)
		}
	}
	return handler(ctx, req)
}

// makeTLSCreds is a test helper which creates a TLS based transport credentials
// from files specified in the arguments.
func makeTLSCreds(t *testing.T, certPath, keyPath, rootsPath string) credentials.TransportCredentials {
	cert, err := tls.LoadX509KeyPair(testdata.Path(certPath), testdata.Path(keyPath))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(%q, %q) failed: %v", certPath, keyPath, err)
	}
	b, err := os.ReadFile(testdata.Path(rootsPath))
	if err != nil {
		t.Fatalf("os.ReadFile(%q) failed: %v", rootsPath, err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(b) {
		t.Fatal("failed to append certificates")
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      roots,
	})
}

const (
	wantHeaderData  = "headerData"
	staleHeaderData = "staleHeaderData"
)

var (
	keyMap = map[string]string{
		"k1": "v1",
		"k2": "v2",
	}
	wantTargets   = []string{"us_east_1.firestore.googleapis.com"}
	lookupRequest = &rlspb.RouteLookupRequest{
		TargetType:      "grpc",
		KeyMap:          keyMap,
		Reason:          rlspb.RouteLookupRequest_REASON_MISS,
		StaleHeaderData: staleHeaderData,
	}
	lookupResponse = &rlstest.RouteLookupResponse{
		Resp: &rlspb.RouteLookupResponse{
			Targets:    wantTargets,
			HeaderData: wantHeaderData,
		},
	}
)

func testControlChannelCredsSuccess(t *testing.T, sopts []grpc.ServerOption, bopts balancer.BuildOptions) {
	// Start an RLS server and set the throttler to never throttle requests.
	rlsServer, _ := rlstest.SetupFakeRLSServer(t, nil, sopts...)
	overrideAdaptiveThrottler(t, neverThrottlingThrottler())

	// Setup the RLS server to respond with a valid response.
	rlsServer.SetResponseCallback(func(context.Context, *rlspb.RouteLookupRequest) *rlstest.RouteLookupResponse {
		return lookupResponse
	})

	// Verify that the request received by the RLS matches the expected one.
	rlsServer.SetRequestCallback(func(got *rlspb.RouteLookupRequest) {
		if diff := cmp.Diff(lookupRequest, got, cmp.Comparer(proto.Equal)); diff != "" {
			t.Errorf("RouteLookupRequest diff (-want, +got):\n%s", diff)
		}
	})

	// Create a control channel to the fake server.
	ctrlCh, err := newControlChannel(rlsServer.Address, "", defaultTestTimeout, bopts, nil)
	if err != nil {
		t.Fatalf("Failed to create control channel to RLS server: %v", err)
	}
	defer ctrlCh.close()

	// Perform the lookup and expect a successful callback invocation.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	errCh := make(chan error, 1)
	ctrlCh.lookup(keyMap, rlspb.RouteLookupRequest_REASON_MISS, staleHeaderData, func(targets []string, headerData string, err error) {
		if err != nil {
			errCh <- fmt.Errorf("rlsClient.lookup() failed with err: %v", err)
			return
		}
		if !cmp.Equal(targets, wantTargets) || headerData != wantHeaderData {
			errCh <- fmt.Errorf("rlsClient.lookup() = (%v, %s), want (%v, %s)", targets, headerData, wantTargets, wantHeaderData)
			return
		}
		errCh <- nil
	})

	select {
	case <-ctx.Done():
		t.Fatal("timeout when waiting for lookup callback to be invoked")
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestControlChannelCredsSuccess tests creation of the control channel with
// different credentials, which are expected to succeed.
func (s) TestControlChannelCredsSuccess(t *testing.T) {
	serverCreds := makeTLSCreds(t, "x509/server1_cert.pem", "x509/server1_key.pem", "x509/client_ca_cert.pem")
	clientCreds := makeTLSCreds(t, "x509/client1_cert.pem", "x509/client1_key.pem", "x509/server_ca_cert.pem")

	tests := []struct {
		name  string
		sopts []grpc.ServerOption
		bopts balancer.BuildOptions
	}{
		{
			name:  "insecure",
			sopts: nil,
			bopts: balancer.BuildOptions{},
		},
		{
			name:  "transport creds only",
			sopts: []grpc.ServerOption{grpc.Creds(serverCreds)},
			bopts: balancer.BuildOptions{
				DialCreds: clientCreds,
				Authority: "x.test.example.com",
			},
		},
		{
			name: "creds bundle",
			sopts: []grpc.ServerOption{
				grpc.Creds(serverCreds),
				grpc.UnaryInterceptor(callCredsValidatingServerInterceptor),
			},
			bopts: balancer.BuildOptions{
				CredsBundle: &testCredsBundle{
					transportCreds: clientCreds,
					callCreds:      &testPerRPCCredentials{callCreds: perRPCCredsData},
				},
				Authority: "x.test.example.com",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testControlChannelCredsSuccess(t, test.sopts, test.bopts)
		})
	}
}

func testControlChannelCredsFailure(t *testing.T, sopts []grpc.ServerOption, bopts balancer.BuildOptions, wantCode codes.Code, wantErrRegex *regexp.Regexp) {
	// StartFakeRouteLookupServer a fake server.
	//
	// Start an RLS server and set the throttler to never throttle requests. The
	// creds failures happen before the RPC handler on the server is invoked.
	// So, there is need to setup the request and responses on the fake server.
	rlsServer, _ := rlstest.SetupFakeRLSServer(t, nil, sopts...)
	overrideAdaptiveThrottler(t, neverThrottlingThrottler())

	// Create the control channel to the fake server.
	ctrlCh, err := newControlChannel(rlsServer.Address, "", defaultTestTimeout, bopts, nil)
	if err != nil {
		t.Fatalf("Failed to create control channel to RLS server: %v", err)
	}
	defer ctrlCh.close()

	// Perform the lookup and expect the callback to be invoked with an error.
	errCh := make(chan error)
	ctrlCh.lookup(nil, rlspb.RouteLookupRequest_REASON_MISS, staleHeaderData, func(_ []string, _ string, err error) {
		if st, ok := status.FromError(err); !ok || st.Code() != wantCode || !wantErrRegex.MatchString(st.String()) {
			errCh <- fmt.Errorf("rlsClient.lookup() returned error: %v, wantCode: %v, wantErr: %s", err, wantCode, wantErrRegex.String())
			return
		}
		errCh <- nil
	})

	select {
	case <-time.After(defaultTestTimeout):
		t.Fatal("timeout when waiting for lookup callback to be invoked")
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestControlChannelCredsFailure tests creation of the control channel with
// different credentials, which are expected to fail.
func (s) TestControlChannelCredsFailure(t *testing.T) {
	serverCreds := makeTLSCreds(t, "x509/server1_cert.pem", "x509/server1_key.pem", "x509/client_ca_cert.pem")
	clientCreds := makeTLSCreds(t, "x509/client1_cert.pem", "x509/client1_key.pem", "x509/server_ca_cert.pem")

	tests := []struct {
		name         string
		sopts        []grpc.ServerOption
		bopts        balancer.BuildOptions
		wantCode     codes.Code
		wantErrRegex *regexp.Regexp
	}{
		{
			name:  "transport creds authority mismatch",
			sopts: []grpc.ServerOption{grpc.Creds(serverCreds)},
			bopts: balancer.BuildOptions{
				DialCreds: clientCreds,
				Authority: "authority-mismatch",
			},
			wantCode:     codes.Unavailable,
			wantErrRegex: regexp.MustCompile(`transport: authentication handshake failed: .* \*\.test\.example\.com.*authority-mismatch`),
		},
		{
			name:  "transport creds handshake failure",
			sopts: nil, // server expects insecure connection
			bopts: balancer.BuildOptions{
				DialCreds: clientCreds,
				Authority: "x.test.example.com",
			},
			wantCode:     codes.Unavailable,
			wantErrRegex: regexp.MustCompile("transport: authentication handshake failed: .*"),
		},
		{
			name: "call creds mismatch",
			sopts: []grpc.ServerOption{
				grpc.Creds(serverCreds),
				grpc.UnaryInterceptor(callCredsValidatingServerInterceptor), // server expects call creds
			},
			bopts: balancer.BuildOptions{
				CredsBundle: &testCredsBundle{
					transportCreds: clientCreds,
					callCreds:      &testPerRPCCredentials{}, // sends no call creds
				},
				Authority: "x.test.example.com",
			},
			wantCode:     codes.PermissionDenied,
			wantErrRegex: regexp.MustCompile("didn't find call creds"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testControlChannelCredsFailure(t, test.sopts, test.bopts, test.wantCode, test.wantErrRegex)
		})
	}
}

type unsupportedCredsBundle struct {
	credentials.Bundle
}

func (*unsupportedCredsBundle) NewWithMode(mode string) (credentials.Bundle, error) {
	return nil, fmt.Errorf("unsupported mode: %v", mode)
}

// TestNewControlChannelUnsupportedCredsBundle tests the case where the control
// channel is configured with a bundle which does not support the mode we use.
func (s) TestNewControlChannelUnsupportedCredsBundle(t *testing.T) {
	rlsServer, _ := rlstest.SetupFakeRLSServer(t, nil)

	// Create the control channel to the fake server.
	ctrlCh, err := newControlChannel(rlsServer.Address, "", defaultTestTimeout, balancer.BuildOptions{CredsBundle: &unsupportedCredsBundle{}}, nil)
	if err == nil {
		ctrlCh.close()
		t.Fatal("newControlChannel succeeded when expected to fail")
	}
}

// wrappingConnectivityStateSubscriber wraps a connectivity state subscriber
// and exposes state changes to tests via a channel.
type wrappingConnectivityStateSubscriber struct {
	delegate    grpcsync.Subscriber
	connStateCh chan connectivity.State
}

func (w *wrappingConnectivityStateSubscriber) OnMessage(msg any) {
	w.delegate.OnMessage(msg)
	w.connStateCh <- msg.(connectivity.State)
}

// TestControlChannelConnectivityStateTransitions_TransientFailure verifies that
// the control channel resets backoff when recovering from TRANSIENT_FAILURE.
// It stops the RLS server to trigger TRANSIENT_FAILURE, then restarts it and
// verifies that backoff is reset when the channel becomes READY again.
func (s) TestControlChannelConnectivityStateTransitions_TransientFailure(t *testing.T) {
	// Create a restartable listener for the RLS server.
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)

	// Start an RLS server with the restartable listener.
	rlsServer, _ := rlstest.SetupFakeRLSServer(t, lis)

	// Override the connectivity state subscriber to wrap it for testing.
	wrappedSubscriber := &wrappingConnectivityStateSubscriber{connStateCh: make(chan connectivity.State, 10)}
	origConnectivityStateSubscriber := newConnectivityStateSubscriber
	newConnectivityStateSubscriber = func(delegate grpcsync.Subscriber) grpcsync.Subscriber {
		wrappedSubscriber.delegate = delegate
		return wrappedSubscriber
	}
	defer func() { newConnectivityStateSubscriber = origConnectivityStateSubscriber }()

	// Setup callback to track invocations.
	var mu sync.Mutex
	var callbackCount int
	callback := func() {
		mu.Lock()
		callbackCount++
		mu.Unlock()
	}

	// Create control channel.
	ctrlCh, err := newControlChannel(rlsServer.Address, "", defaultTestTimeout, balancer.BuildOptions{}, callback)
	if err != nil {
		t.Fatalf("Failed to create control channel: %v", err)
	}
	defer ctrlCh.close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Verify that the control channel moves to READY.
	wantStates := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
	}
	for _, wantState := range wantStates {
		select {
		case gotState := <-wrappedSubscriber.connStateCh:
			if gotState != wantState {
				t.Fatalf("Unexpected connectivity state: got %v, want %v", gotState, wantState)
			}
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for RLS control channel to become %q", wantState)
		}
	}

	// Verify no callbacks have been invoked yet (initial READY doesn't trigger callback).
	mu.Lock()
	if callbackCount != 0 {
		mu.Unlock()
		t.Fatalf("Got %d callback invocations for initial READY, want 0", callbackCount)
	}
	mu.Unlock()

	// Stop the RLS server to trigger TRANSIENT_FAILURE.
	lis.Stop()

	// Verify that the control channel moves to IDLE.
	wantStates = []connectivity.State{
		connectivity.Idle,
	}
	for _, wantState := range wantStates {
		select {
		case gotState := <-wrappedSubscriber.connStateCh:
			if gotState != wantState {
				t.Fatalf("Unexpected connectivity state: got %v, want %v", gotState, wantState)
			}
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for RLS control channel to become %q", wantState)
		}
	}

	// Trigger a reconnection attempt by making a lookup (which will fail).
	// This should cause the channel to attempt to reconnect and move to TRANSIENT_FAILURE.
	ctrlCh.lookup(nil, rlspb.RouteLookupRequest_REASON_MISS, "", func(_ []string, _ string, _ error) {})

	// Verify that the control channel moves to TRANSIENT_FAILURE.
	wantStates = []connectivity.State{
		connectivity.Connecting,
		connectivity.TransientFailure,
	}
	for _, wantState := range wantStates {
		select {
		case gotState := <-wrappedSubscriber.connStateCh:
			if gotState != wantState {
				t.Fatalf("Unexpected connectivity state: got %v, want %v", gotState, wantState)
			}
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for RLS control channel to become %q", wantState)
		}
	}

	// Restart the RLS server.
	lis.Restart()

	// The control channel should eventually reconnect and move to READY.
	// This transition from TRANSIENT_FAILURE → READY should trigger the callback.
	// We drain states until we see READY, as the channel may go through intermediate
	// states (CONNECTING) very quickly after restart.
	for {
		select {
		case gotState := <-wrappedSubscriber.connStateCh:
			if gotState == connectivity.Ready {
				goto ready
			}
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for RLS control channel to become READY")
		}
	}
ready:

	// Verify that the callback was invoked exactly once (for TRANSIENT_FAILURE → READY).
	mu.Lock()
	got := callbackCount
	mu.Unlock()
	if got != 1 {
		t.Fatalf("Got %d callback invocations, want 1", got)
	}
}

// TestControlChannelConnectivityStateTransitions_IdleDoesNotTriggerCallback
// verifies that IDLE → READY transitions do not trigger backoff reset callbacks.
func (s) TestControlChannelConnectivityStateTransitions_IdleDoesNotTriggerCallback(t *testing.T) {
	// Create a restartable listener for the RLS server.
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)

	// Start an RLS server with the restartable listener.
	rlsServer, _ := rlstest.SetupFakeRLSServer(t, lis)

	// Override the connectivity state subscriber to wrap it for testing.
	wrappedSubscriber := &wrappingConnectivityStateSubscriber{connStateCh: make(chan connectivity.State, 10)}
	origConnectivityStateSubscriber := newConnectivityStateSubscriber
	newConnectivityStateSubscriber = func(delegate grpcsync.Subscriber) grpcsync.Subscriber {
		wrappedSubscriber.delegate = delegate
		return wrappedSubscriber
	}
	defer func() { newConnectivityStateSubscriber = origConnectivityStateSubscriber }()

	// Setup callback to track invocations.
	var mu sync.Mutex
	var callbackCount int
	callback := func() {
		mu.Lock()
		callbackCount++
		mu.Unlock()
	}

	// Create control channel.
	ctrlCh, err := newControlChannel(rlsServer.Address, "", defaultTestTimeout, balancer.BuildOptions{}, callback)
	if err != nil {
		t.Fatalf("Failed to create control channel: %v", err)
	}
	defer ctrlCh.close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Verify that the control channel moves to READY.
	wantStates := []connectivity.State{
		connectivity.Connecting,
		connectivity.Ready,
	}
	for _, wantState := range wantStates {
		select {
		case gotState := <-wrappedSubscriber.connStateCh:
			if gotState != wantState {
				t.Fatalf("Unexpected connectivity state: got %v, want %v", gotState, wantState)
			}
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for RLS control channel to become %q", wantState)
		}
	}

	// Stop the RLS server (without triggering TRANSIENT_FAILURE first).
	lis.Stop()

	// Verify that the control channel moves to IDLE.
	wantStates = []connectivity.State{
		connectivity.Idle,
	}
	for _, wantState := range wantStates {
		select {
		case gotState := <-wrappedSubscriber.connStateCh:
			if gotState != wantState {
				t.Fatalf("Unexpected connectivity state: got %v, want %v", gotState, wantState)
			}
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for RLS control channel to become %q", wantState)
		}
	}

	// Restart the RLS server before the channel goes to TRANSIENT_FAILURE.
	lis.Restart()

	// Trigger a reconnection by making a lookup.
	ctrlCh.lookup(nil, rlspb.RouteLookupRequest_REASON_MISS, "", func(_ []string, _ string, _ error) {})

	// The control channel should reconnect and move to READY.
	// This transition from IDLE → READY should NOT trigger the callback.
	// We drain states until we see READY, as the channel may go through intermediate
	// states (CONNECTING) very quickly.
	for {
		select {
		case gotState := <-wrappedSubscriber.connStateCh:
			if gotState == connectivity.Ready {
				goto idleready
			}
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for RLS control channel to become READY")
		}
	}
idleready:

	// Verify that the callback was never invoked (IDLE → READY doesn't trigger callback).
	// We check immediately after observing the READY state - if the callback was going
	// to be invoked, it would have happened during the state transition processing.
	mu.Lock()
	got := callbackCount
	mu.Unlock()
	if got != 0 {
		t.Fatalf("Got %d callback invocations for IDLE → READY, want 0", got)
	}
}
