/*
 *
 * Copyright 2026 gRPC authors.
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

package extauthz

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	xdscreds "google.golang.org/grpc/internal/xds/credentials"
	"google.golang.org/grpc/internal/xds/grpcservice"
	"google.golang.org/grpc/internal/xds/httpfilter"
	iextauthz "google.golang.org/grpc/internal/xds/httpfilter/ext_authz/internal"
)

type testServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *testServerStream) Context() context.Context {
	return s.ctx
}

// Test verifies that BuildServerInterceptor returns appropriate errors for
// invalid inputs or failures.
func (s) TestBuildServerInterceptor_Failure(t *testing.T) {
	tests := []struct {
		name       string
		cfg        httpfilter.FilterConfig
		wantErrStr string
	}{
		{
			name:       "InvalidConfigType",
			cfg:        httpfilter.DisabledFilterConfig{},
			wantErrStr: "extauthz: incorrect config type provided",
		},
		{
			name: "ChannelCreationFailure",
			cfg: config{
				grpcService: &grpcservice.Config{TargetURI: "localhost:1234"},
			},
			wantErrStr: "extauthz: failed to create channel to the external authorization server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := iextauthz.CreateExtAuthzChannel
			iextauthz.CreateExtAuthzChannel = func(*grpcservice.Config) (grpc.ClientConnInterface, func(), error) {
				return nil, nil, fmt.Errorf("injected error")
			}
			t.Cleanup(func() { iextauthz.CreateExtAuthzChannel = orig })

			sf := builder{}.BuildServerFilter(httpfilter.ServerFilterOptions{})
			defer sf.Close()

			if _, err := sf.BuildServerInterceptor(tt.cfg, nil); err == nil || !strings.Contains(err.Error(), tt.wantErrStr) {
				t.Fatalf("BuildServerInterceptor() returned error = %v, want error containing %q", err, tt.wantErrStr)
			}
		})
	}
}

func buildServerInterceptor(t *testing.T, sf httpfilter.ServerFilter, cfg httpfilter.FilterConfig) httpfilter.ServerInterceptor {
	t.Helper()
	intptr, err := sf.BuildServerInterceptor(cfg, nil)
	if err != nil {
		t.Fatalf("BuildServerInterceptor() failed: %v", err)
	}
	return intptr
}

// Test verifies that channels are shared when configurations have identical
// service config (matching TargetURI, ChannelCredentials, and CallCredentials)
// and isolated when any of these three fields differ.
func (s) TestBuildServerInterceptor_ChannelSharingAndIsolation(t *testing.T) {
	// Override createExtAuthzChannel for testing to track how many times a new
	// channel is dialed.
	origCreateExtAuthzChannel := iextauthz.CreateExtAuthzChannel
	var dialCount int
	iextauthz.CreateExtAuthzChannel = func(cfg *grpcservice.Config) (grpc.ClientConnInterface, func(), error) {
		dialCount++
		conn, err := grpc.NewClient(cfg.TargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, nil, err
		}
		return conn, func() { conn.Close() }, nil
	}
	defer func() { iextauthz.CreateExtAuthzChannel = origCreateExtAuthzChannel }()

	sf := builder{}.BuildServerFilter(httpfilter.ServerFilterOptions{})
	defer sf.Close()

	creds1 := xdscreds.NewChannelCreds(insecure.NewBundle(), xdscreds.Identity{Type: "creds1"}, nil)
	creds2 := xdscreds.NewChannelCreds(insecure.NewBundle(), xdscreds.Identity{Type: "creds2"}, nil)
	callCreds1 := xdscreds.NewCallCreds(testPerRPCCreds{}, xdscreds.Identity{Type: "callCreds1"}, nil)

	cfg1 := config{
		grpcService: &grpcservice.Config{
			TargetURI:          "localhost:1234",
			ChannelCredentials: creds1,
			CallCredentials:    []*xdscreds.CallCreds{callCreds1},
		},
	}
	cfg2 := cfg1

	// cfg3 has a different TargetURI than cfg1 and cfg2.
	cfg3 := config{
		grpcService: &grpcservice.Config{
			TargetURI:          "localhost:5678",
			ChannelCredentials: creds1,
			CallCredentials:    []*xdscreds.CallCreds{callCreds1},
		},
	}

	// cfg4 has a different ChannelCredentials than cfg1 and cfg2.
	cfg4 := config{
		grpcService: &grpcservice.Config{
			TargetURI:          "localhost:1234",
			ChannelCredentials: creds2,
			CallCredentials:    []*xdscreds.CallCreds{callCreds1},
		},
	}

	// Build the first interceptor with cfg1. Since this is the first request for
	// this configuration key, a new channel should be created, incrementing
	// dialCount to 1.
	interceptor1 := buildServerInterceptor(t, sf, cfg1)
	defer interceptor1.Close()
	if dialCount != 1 {
		t.Fatalf("Unexpected dialCount: got %d, want 1", dialCount)
	}

	// Build an interceptor with cfg2, which has the exact same service config
	// as cfg1. The server filter should share the existing gRPC channel
	// instead of creating a new one, so dialCount should remain 1.
	interceptor2 := buildServerInterceptor(t, sf, cfg2)
	defer interceptor2.Close()
	if dialCount != 1 {
		t.Fatalf("Unexpected dialCount: got %d, want 1", dialCount)
	}

	// Build an interceptor with cfg3, which has a different TargetURI.
	// Since no cached channel exists for this new key, a new gRPC channel
	// must be created, incrementing the dialCount to 2.
	interceptor3 := buildServerInterceptor(t, sf, cfg3)
	defer interceptor3.Close()
	if dialCount != 2 {
		t.Fatalf("Unexpected dialCount: got %d, want 2", dialCount)
	}

	// Build an interceptor with cfg4, which has different ChannelCredentials.
	// Since the channel key includes ChannelCredentials, a new gRPC channel
	// must be created, incrementing dialCount to 3.
	interceptor4 := buildServerInterceptor(t, sf, cfg4)
	defer interceptor4.Close()
	if dialCount != 3 {
		t.Fatalf("Unexpected dialCount: got %d, want 3", dialCount)
	}
}

// Test verifies that channels are cleaned up when reference count reaches
// zero.
func (s) TestBuildServerInterceptor_ChannelCleanup(t *testing.T) {
	// Override createExtAuthzChannel for testing to track how many times a new
	// channel is dialed and closed.
	origCreateExtAuthzChannel := iextauthz.CreateExtAuthzChannel
	defer func() { iextauthz.CreateExtAuthzChannel = origCreateExtAuthzChannel }()

	var dialCount int
	var closeCount int
	iextauthz.CreateExtAuthzChannel = func(cfg *grpcservice.Config) (grpc.ClientConnInterface, func(), error) {
		dialCount++
		conn, err := grpc.NewClient(cfg.TargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, nil, err
		}
		return conn, func() {
			closeCount++
			conn.Close()
		}, nil
	}

	sf := builder{}.BuildServerFilter(httpfilter.ServerFilterOptions{})
	defer sf.Close()

	cfg := config{
		grpcService: &grpcservice.Config{
			TargetURI:          "localhost:1234",
			ChannelCredentials: xdscreds.NewChannelCreds(insecure.NewBundle(), xdscreds.Identity{Type: "creds1"}, nil),
			CallCredentials:    []*xdscreds.CallCreds{xdscreds.NewCallCreds(testPerRPCCreds{}, xdscreds.Identity{Type: "callCreds1"}, nil)},
		},
	}

	// Build the first interceptor with cfg. Since this is the initial request
	// for this configuration key, a new channel is created.
	intptr1 := buildServerInterceptor(t, sf, cfg)
	if dialCount != 1 {
		t.Fatalf("Unexpected dialCount: got %d, want 1", dialCount)
	}

	// Build a second interceptor with the exact same config key. The existing
	// gRPC channel should be shared and its reference count incremented to 2.
	intptr2 := buildServerInterceptor(t, sf, cfg)
	if dialCount != 1 {
		t.Fatalf("Unexpected dialCount: got %d, want 1", dialCount)
	}

	// Close the first interceptor, decrementing the reference count from 2 to 1.
	// Because the reference count is still greater than zero, the channel should
	// not be closed yet.
	intptr1.Close()
	if closeCount != 0 {
		t.Fatalf("Unexpected closeCount: got %d, want 0", closeCount)
	}

	// Close the second interceptor, decrementing the reference count to zero.
	// This should trigger the cleanup callback, deleting the channel from the
	// cache and closing the underlying gRPC connection.
	intptr2.Close()
	if closeCount != 1 {
		t.Fatalf("Unexpected closeCount: got %d, want 1", closeCount)
	}

	// Recreating an interceptor with the same config after the previous channel
	// was cleaned up and removed from the cache should trigger a new dial.
	intptr3 := buildServerInterceptor(t, sf, cfg)
	defer intptr3.Close()
	if dialCount != 2 {
		t.Fatalf("Unexpected dialCount: got %d, want 2", dialCount)
	}
}

// Test verifies that InterceptRPC returns an error when the interceptor is closed.
func (s) TestServerInterceptor_Closed(t *testing.T) {
	origCreateExtAuthzChannel := iextauthz.CreateExtAuthzChannel
	iextauthz.CreateExtAuthzChannel = func(cfg *grpcservice.Config) (grpc.ClientConnInterface, func(), error) {
		conn, err := grpc.NewClient(cfg.TargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, nil, err
		}
		return conn, func() { conn.Close() }, nil
	}
	defer func() { iextauthz.CreateExtAuthzChannel = origCreateExtAuthzChannel }()

	sf := builder{}.BuildServerFilter(httpfilter.ServerFilterOptions{})
	defer sf.Close()

	cfg := config{
		grpcService: &grpcservice.Config{
			TargetURI:          "localhost:1234",
			ChannelCredentials: allowlistInsecureCreds,
		},
	}

	intptr := buildServerInterceptor(t, sf, cfg)
	intptr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const wantErr = "extauthz: interceptor is closed"
	ss := &testServerStream{ctx: ctx}
	if _, err := intptr.InterceptRPC(ss); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("InterceptRPC() returned unexpected results, got %q, want error containing %q", err, wantErr)
	}
}

// Test verifies that InterceptRPC returns an error when authzClient is closed
// (refcount reached 0) even if the interceptor closed flag is false.
func (s) TestServerInterceptor_AuthzClientClosed(t *testing.T) {
	origCreateExtAuthzChannel := iextauthz.CreateExtAuthzChannel
	iextauthz.CreateExtAuthzChannel = func(cfg *grpcservice.Config) (grpc.ClientConnInterface, func(), error) {
		conn, err := grpc.NewClient(cfg.TargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, nil, err
		}
		return conn, func() { conn.Close() }, nil
	}
	defer func() { iextauthz.CreateExtAuthzChannel = origCreateExtAuthzChannel }()

	sf := builder{}.BuildServerFilter(httpfilter.ServerFilterOptions{})
	defer sf.Close()

	cfg := config{
		filterEnabled: fraction{
			numerator:   100,
			denominator: 100,
		},
		grpcService: &grpcservice.Config{
			TargetURI:          "localhost:1234",
			ChannelCredentials: allowlistInsecureCreds,
		},
	}

	intptr := buildServerInterceptor(t, sf, cfg)
	si := intptr.(*serverInterceptor)
	// Decrement authzClient to zero without marking interceptor as closed to simulate race.
	si.authzClient.Decrement()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const wantErr = "extauthz: authz client is closed"
	ss := &testServerStream{ctx: ctx}
	if _, err := intptr.InterceptRPC(ss); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("InterceptRPC() returned unexpected results, got %v, want error containing %q", err, wantErr)
	}
}
