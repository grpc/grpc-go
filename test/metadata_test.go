/*
 *
 * Copyright 2022 gRPC authors.
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
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

func (s) TestInvalidMetadata(t *testing.T) {
	grpctest.ExpectErrorN("stream: failed to validate md when setting trailer", 5)

	tests := []struct {
		name     string
		md       metadata.MD
		appendMD []string
		want     error
		recv     error
	}{
		{
			name: "invalid key",
			md:   map[string][]string{string(rune(0x19)): {"testVal"}},
			want: status.Error(codes.Internal, "header key \"\\x19\" contains illegal characters not in [0-9a-z-_.]"),
			recv: status.Error(codes.Internal, "invalid header field"),
		},
		{
			name: "invalid value",
			md:   map[string][]string{"test": {string(rune(0x19))}},
			want: status.Error(codes.Internal, "header key \"test\" contains value with non-printable ASCII characters"),
			recv: status.Error(codes.Internal, "invalid header field"),
		},
		{
			name:     "invalid appended value",
			md:       map[string][]string{"test": {"test"}},
			appendMD: []string{"/", "value"},
			want:     status.Error(codes.Internal, "header key \"/\" contains illegal characters not in [0-9a-z-_.]"),
			recv:     status.Error(codes.Internal, "invalid header field"),
		},
		{
			name:     "empty appended key",
			md:       map[string][]string{"test": {"test"}},
			appendMD: []string{"", "value"},
			want:     status.Error(codes.Internal, "there is an empty key in the header"),
			recv:     status.Error(codes.Internal, "invalid header field"),
		},
		{
			name: "empty key",
			md:   map[string][]string{"": {"test"}},
			want: status.Error(codes.Internal, "there is an empty key in the header"),
			recv: status.Error(codes.Internal, "invalid header field"),
		},
		{
			name: "-bin key with arbitrary value",
			md:   map[string][]string{"test-bin": {string(rune(0x19))}},
			want: nil,
			recv: io.EOF,
		},
		{
			name: "valid key and value",
			md:   map[string][]string{"test": {"value"}},
			want: nil,
			recv: io.EOF,
		},
	}

	testNum := 0
	ss := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			_, err := stream.Recv()
			if err != nil {
				return err
			}
			test := tests[testNum]
			testNum++
			// merge original md and added md.
			md := metadata.Join(test.md, metadata.Pairs(test.appendMD...))

			if err := stream.SetHeader(md); !reflect.DeepEqual(test.want, err) {
				return fmt.Errorf("call stream.SendHeader(md) validate metadata which is %v got err :%v, want err :%v", md, err, test.want)
			}
			if err := stream.SendHeader(md); !reflect.DeepEqual(test.want, err) {
				return fmt.Errorf("call stream.SendHeader(md) validate metadata which is %v got err :%v, want err :%v", md, err, test.want)
			}
			stream.SetTrailer(md)
			return nil
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting ss endpoint server: %v", err)
	}
	defer ss.Stop()

	for _, test := range tests {
		t.Run("unary "+test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			ctx = metadata.NewOutgoingContext(ctx, test.md)
			ctx = metadata.AppendToOutgoingContext(ctx, test.appendMD...)
			if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}); !reflect.DeepEqual(test.want, err) {
				t.Errorf("call ss.Client.EmptyCall() validate metadata which is %v got err :%v, want err :%v", test.md, err, test.want)
			}
		})
	}

	// call the stream server's api to drive the server-side unit testing
	for _, test := range tests {
		t.Run("streaming "+test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			stream, err := ss.Client.FullDuplexCall(ctx)
			if err != nil {
				t.Errorf("call ss.Client.FullDuplexCall got err :%v", err)
				return
			}
			if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
				t.Errorf("call ss.Client stream Send(nil) will success but got err :%v", err)
			}
			if _, err := stream.Recv(); status.Code(err) != status.Code(test.recv) || !strings.Contains(err.Error(), test.recv.Error()) {
				t.Errorf("stream.Recv() = _, get err :%v, want err :%v", err, test.recv)
			}
		})
	}
}

// TestLowercaseMetadataKeys verifies that, by default, outgoing metadata with
// a non-lowercase key is rejected without reaching the server, and that
// WithLowercaseMetadataKeys makes it be sent with the key lowercased.
func (s) TestLowercaseMetadataKeys(t *testing.T) {
	// A StubServer can only be started once, so each subtest gets its own.
	newServer := func(gotMD chan metadata.MD) *stubserver.StubServer {
		return &stubserver.StubServer{
			EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
				md, _ := metadata.FromIncomingContext(ctx)
				gotMD <- md
				return &testpb.Empty{}, nil
			},
			FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
				md, _ := metadata.FromIncomingContext(stream.Context())
				gotMD <- md
				return nil
			},
		}
	}

	// Built as a map literal, so the key is not lowercased by the metadata package.
	outgoingCtx := func(ctx context.Context) context.Context {
		ctx = metadata.NewOutgoingContext(ctx, metadata.MD{"X-Mixed-Case": {"v1"}})
		return metadata.AppendToOutgoingContext(ctx, "X-Appended", "v2")
	}

	tests := []struct {
		name         string
		dialOpts     []grpc.DialOption
		envEnabled   bool
		wantRejected bool
	}{
		{name: "default is strict", wantRejected: true},
		{name: "option lowercases", dialOpts: []grpc.DialOption{grpc.WithLowercaseMetadataKeys()}},
		{name: "environment variable lowercases for every connection", envEnabled: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.envEnabled {
				// Mirrors GRPC_GO_LOWERCASE_METADATA_KEYS=true, which is read at start up.
				testutils.SetEnvConfig(t, &envconfig.LowercaseMetadataKeys, true)
			}
			gotMD := make(chan metadata.MD, 1)
			ss := newServer(gotMD)
			if err := ss.Start(nil, test.dialOpts...); err != nil {
				t.Fatalf("Error starting endpoint server: %v", err)
			}
			defer ss.Stop()

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			_, err := ss.Client.EmptyCall(outgoingCtx(ctx), &testpb.Empty{})
			if test.wantRejected {
				if status.Code(err) != codes.Internal || !strings.Contains(err.Error(), "illegal characters") {
					t.Fatalf("EmptyCall() = %v, want an Internal error about illegal characters", err)
				}
				select {
				case md := <-gotMD:
					t.Fatalf("server was reached with metadata %v, want the call rejected by the client", md)
				default:
				}
				return
			}
			if err != nil {
				t.Fatalf("EmptyCall() = %v, want nil", err)
			}
			md := <-gotMD
			if got := md.Get("x-mixed-case"); !reflect.DeepEqual(got, []string{"v1"}) {
				t.Errorf("server saw x-mixed-case = %v, want [v1]", got)
			}
			if got := md.Get("x-appended"); !reflect.DeepEqual(got, []string{"v2"}) {
				t.Errorf("server saw x-appended = %v, want [v2]", got)
			}

			// Streaming RPCs take the same path.
			stream, err := ss.Client.FullDuplexCall(outgoingCtx(ctx))
			if err != nil {
				t.Fatalf("FullDuplexCall() = %v, want nil", err)
			}
			if _, err := stream.Recv(); err != io.EOF {
				t.Fatalf("stream.Recv() = %v, want io.EOF", err)
			}
			md = <-gotMD
			if got := md.Get("x-mixed-case"); !reflect.DeepEqual(got, []string{"v1"}) {
				t.Errorf("stream: server saw x-mixed-case = %v, want [v1]", got)
			}
		})
	}
}
