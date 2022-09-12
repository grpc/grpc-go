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
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

func (s) TestInvalidMetadata(t *testing.T) {
	grpctest.TLogger.ExpectErrorN("stream: failed to validate md when setting trailer", 2)

	tests := []struct {
		md   metadata.MD
		want error
		recv error
	}{
		{
			md:   map[string][]string{string(rune(0x19)): {"testVal"}},
			want: status.Error(codes.Internal, "header key \"\\x19\" contains illegal characters not in [0-9a-z-_.]"),
			recv: status.Error(codes.Internal, "invalid header field"),
		},
		{
			md:   map[string][]string{"test": {string(rune(0x19))}},
			want: status.Error(codes.Internal, "header key \"test\" contains value with non-printable ASCII characters"),
			recv: status.Error(codes.Internal, "invalid header field"),
		},
		{
			md:   map[string][]string{"test-bin": {string(rune(0x19))}},
			want: nil,
			recv: io.EOF,
		},
		{
			md:   map[string][]string{"test": {"value"}},
			want: nil,
			recv: io.EOF,
		},
	}

	testNum := 0
	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			_, err := stream.Recv()
			if err != nil {
				return err
			}
			test := tests[testNum]
			testNum++
			if err := stream.SetHeader(test.md); !reflect.DeepEqual(test.want, err) {
				return fmt.Errorf("call stream.SendHeader(md) validate metadata which is %v got err :%v, want err :%v", test.md, err, test.want)
			}
			if err := stream.SendHeader(test.md); !reflect.DeepEqual(test.want, err) {
				return fmt.Errorf("call stream.SendHeader(md) validate metadata which is %v got err :%v, want err :%v", test.md, err, test.want)
			}
			stream.SetTrailer(test.md)
			return nil
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting ss endpoint server: %v", err)
	}
	defer ss.Stop()

	for _, test := range tests {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		ctx = metadata.NewOutgoingContext(ctx, test.md)
		if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}); !reflect.DeepEqual(test.want, err) {
			t.Errorf("call ss.Client.EmptyCall() validate metadata which is %v got err :%v, want err :%v", test.md, err, test.want)
		}
	}

	// call the stream server's api to drive the server-side unit testing
	for _, test := range tests {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		stream, err := ss.Client.FullDuplexCall(ctx)
		defer cancel()
		if err != nil {
			t.Errorf("call ss.Client.FullDuplexCall(context.Background()) will success but got err :%v", err)
			continue
		}
		if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
			t.Errorf("call ss.Client stream Send(nil) will success but got err :%v", err)
		}
		if _, err := stream.Recv(); status.Code(err) != status.Code(test.recv) || !strings.Contains(err.Error(), test.recv.Error()) {
			t.Errorf("stream.Recv() = _, get err :%v, want err :%v", err, test.recv)
		}
	}
}
