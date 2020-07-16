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
	"os"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

/*// unixServer is used to test servers listening over a unix socket.
type unixServer struct {
	// Guarantees we satisfy this interface; panics if unimplemented methods are called.
	testpb.TestServiceServer

	// Customizable implementations of server handlers.
	emptyCall func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error)

	// A client connected to this service the test may use.  Created in Start().
	client testpb.TestServiceClient
	cc     *grpc.ClientConn
	s      *grpc.Server

	cleanups []func() // Lambdas executed in Stop(); populated by Start().
}

func (us *unixServer) EmptyCall(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
	return us.emptyCall(ctx, in)
}

func (us *unixServer) Start(address, target string) error {
	lis, err := net.Listen("unix", address)
	if err != nil {
		return err
	}
	us.cleanups = append(us.cleanups, func() { lis.Close() })

	s := grpc.NewServer()
	testpb.RegisterTestServiceServer(s, us)
	go s.Serve(lis)
	us.cleanups = append(us.cleanups, s.Stop)
	us.s = s

	cc, err := grpc.Dial(target, grpc.WithInsecure())
	if err != nil {
		return fmt.Errorf("grpc.Dial(%q) = %v", target, err)
	}
	us.cc = cc
	if err := us.waitForReady(cc); err != nil {
		return err
	}
	us.cleanups = append(us.cleanups, func() { cc.Close() })

	us.client = testpb.NewTestServiceClient(cc)

	return nil
}

func (us *unixServer) waitForReady(cc *grpc.ClientConn) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		s := cc.GetState()
		if s == connectivity.Ready {
			return nil
		}
		if !cc.WaitForStateChange(ctx, s) {
			return ctx.Err()
		}
	}
}

func (us *unixServer) Stop() {
	for i := len(us.cleanups) - 1; i >= 0; i-- {
		us.cleanups[i]()
	}
}*/

func runUnixTest(t *testing.T, address, target, expectedAuthority string) {
	if err := os.RemoveAll(address); err != nil {
		t.Fatalf("Error removing socket file %v: %v\n", address, err)
	}
	us := &stubServer{
		emptyCall: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return nil, status.Error(codes.InvalidArgument, "failed to parse metadata")
			}
			auths, ok := md[":authority"]
			if !ok {
				return nil, status.Error(codes.InvalidArgument, "no authority header")
			}
			if len(auths) < 1 {
				return nil, status.Error(codes.InvalidArgument, "no authority header")
			}
			if auths[0] != expectedAuthority {
				return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid authority header %v, expected %v", auths[0], expectedAuthority))
			}
			return &testpb.Empty{}, nil
		},
		network: "unix",
		address: address,
		target:  target,
	}
	if err := us.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
		return
	}
	defer us.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := us.client.EmptyCall(ctx, &testpb.Empty{})
	if err != nil {
		t.Errorf("us.client.EmptyCall(_, _) = _, %v; want _, nil", err)
	}
}

func (s) TestUnix(t *testing.T) {
	tests := []struct {
		name      string
		address   string
		target    string
		authority string
	}{
		{
			name:      "Unix1",
			address:   "sock.sock",
			target:    "unix:sock.sock",
			authority: "localhost",
		},
		{
			name:      "Unix2",
			address:   "/tmp/sock.sock",
			target:    "unix:/tmp/sock.sock",
			authority: "localhost",
		},
		{
			name:      "Unix3",
			address:   "/tmp/sock.sock",
			target:    "unix:///tmp/sock.sock",
			authority: "localhost",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runUnixTest(t, test.address, test.target, test.authority)
		})
	}
}
