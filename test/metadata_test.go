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
	"io"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

// TestInvalidMetadata test invalid metadata
func (s) TestInvalidMetadata(t *testing.T) {

	tests := []struct {
		md   metadata.MD
		want error
	}{
		{
			md:   map[string][]string{string(rune(0x19)): {"testVal"}},
			want: status.Error(codes.Internal, "header key contains not 0-9a-z-_. characters"),
		},
		{
			md:   map[string][]string{"test": {string(rune(0x19))}},
			want: status.Error(codes.Internal, "header val contains not printable ASCII characters"),
		},
		{
			md:   map[string][]string{"test-bin": {string(rune(0x19))}},
			want: nil,
		},
	}

	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			for {
				_, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				for _, test := range tests {
					if err := stream.SetHeader(test.md); !reflect.DeepEqual(test.want, err) {
						t.Errorf("call stream.SendHeader(md) validate metadata which is %v got err :%v, want err :%v", test.md, err, test.want)
					}
					if err := stream.SendHeader(test.md); !reflect.DeepEqual(test.want, err) {
						t.Errorf("call stream.SendHeader(md) validate metadata which is %v got err :%v, want err :%v", test.md, err, test.want)
					}
					stream.SetTrailer(test.md)
				}
				err = stream.Send(&testpb.StreamingOutputCallResponse{})
				if err != nil {
					return err
				}
			}
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
			t.Fatalf("call ss.Client.EmptyCall() validate metadata which is %v got err :%v, want err :%v", test.md, err, test.want)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	stream, err := ss.Client.FullDuplexCall(ctx, grpc.WaitForReady(true))
	defer cancel()
	if err != nil {
		t.Fatalf("call ss.Client.FullDuplexCall(context.Background()) will success but got err :%v", err)
	}
	if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
	 	t.Fatalf("call ss.Client stream Send(nil) will success but got err :%v", err)
	}
	if _, err := stream.Header(); err != nil {
	 	t.Fatalf("call ss.Client stream Send(nil) will success but got err :%v", err)
	}
	if _, err := stream.Recv(); err != nil {
	 	t.Fatalf("stream.Recv() = _, %v", err)
	}
	stream.CloseSend()
}
