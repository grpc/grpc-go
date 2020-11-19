/*
 *
 * Copyright 2020 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     https://www.apache.org/licenses/LICENSE-2.0
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
	"os"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

func authorityChecker(ctx context.Context, expectedAuthority string) (*testpb.Empty, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "failed to parse metadata")
	}
	auths, ok := md[":authority"]
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "no authority header")
	}
	if len(auths) != 1 {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("no authority header, auths = %v", auths))
	}
	if auths[0] != expectedAuthority {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid authority header %v, expected %v", auths[0], expectedAuthority))
	}
	return &testpb.Empty{}, nil
}

func runUnixTest(t *testing.T, address, target, expectedAuthority string, dialer func(context.Context, string) (net.Conn, error)) {
	if err := os.RemoveAll(address); err != nil {
		t.Fatalf("Error removing socket file %v: %v\n", address, err)
	}
	ss := &stubServer{
		emptyCall: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			return authorityChecker(ctx, expectedAuthority)
		},
		network: "unix",
		address: address,
		target:  target,
	}
	opts := []grpc.DialOption{}
	if dialer != nil {
		opts = append(opts, grpc.WithContextDialer(dialer))
	}
	if err := ss.Start(nil, opts...); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := ss.client.EmptyCall(ctx, &testpb.Empty{})
	if err != nil {
		t.Errorf("us.client.EmptyCall(_, _) = _, %v; want _, nil", err)
	}
}

// TestUnix does end to end tests with the various supported unix target
// formats, ensuring that the authority is set to localhost in every case.
func (s) TestUnix(t *testing.T) {
	tests := []struct {
		name      string
		address   string
		target    string
		authority string
	}{
		{
			name:      "UnixRelative",
			address:   "sock.sock",
			target:    "unix:sock.sock",
			authority: "localhost",
		},
		{
			name:      "UnixAbsolute",
			address:   "/tmp/sock.sock",
			target:    "unix:/tmp/sock.sock",
			authority: "localhost",
		},
		{
			name:      "UnixAbsoluteAlternate",
			address:   "/tmp/sock.sock",
			target:    "unix:///tmp/sock.sock",
			authority: "localhost",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runUnixTest(t, test.address, test.target, test.authority, nil)
		})
	}
}

// TestUnixCustomDialer does end to end tests with various supported unix target
// formats, ensuring that the target sent to the dialer does NOT have the
// "unix:" prefix stripped.
func (s) TestUnixCustomDialer(t *testing.T) {
	tests := []struct {
		name      string
		address   string
		target    string
		authority string
	}{
		{
			name:      "UnixRelative",
			address:   "sock.sock",
			target:    "unix:sock.sock",
			authority: "localhost",
		},
		{
			name:      "UnixAbsolute",
			address:   "/tmp/sock.sock",
			target:    "unix:/tmp/sock.sock",
			authority: "localhost",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dialer := func(ctx context.Context, address string) (net.Conn, error) {
				if address != test.target {
					return nil, fmt.Errorf("expected target %v in custom dialer, instead got %v", test.target, address)
				}
				address = address[len("unix:"):]
				return (&net.Dialer{}).DialContext(ctx, "unix", address)
			}
			runUnixTest(t, test.address, test.target, test.authority, dialer)
		})
	}
}

func (s) TestColonPortAuthority(t *testing.T) {
	expectedAuthority := ""
	var authorityMu sync.Mutex
	ss := &stubServer{
		emptyCall: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			authorityMu.Lock()
			defer authorityMu.Unlock()
			return authorityChecker(ctx, expectedAuthority)
		},
		network: "tcp",
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()
	_, port, err := net.SplitHostPort(ss.address)
	if err != nil {
		t.Fatalf("Failed splitting host from post: %v", err)
	}
	authorityMu.Lock()
	expectedAuthority = "localhost:" + port
	authorityMu.Unlock()
	// ss.Start dials, but not the ":[port]" target that is being tested here.
	// Dial again, with ":[port]" as the target.
	//
	// Append "localhost" before calling net.Dial, in case net.Dial on certain
	// platforms doesn't work well for address without the IP.
	cc, err := grpc.Dial(":"+port, grpc.WithInsecure(), grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", "localhost"+addr)
	}))
	if err != nil {
		t.Fatalf("grpc.Dial(%q) = %v", ss.target, err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = testpb.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{})
	if err != nil {
		t.Errorf("us.client.EmptyCall(_, _) = _, %v; want _, nil", err)
	}
}
