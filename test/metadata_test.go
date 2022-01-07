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
	"reflect"
	"testing"
	"time"

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

	ss2 := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {

			for _, test := range tests {
				if err := stream.SendHeader(test.md); !reflect.DeepEqual(test.want, err) {
					t.Fatalf("call stream.SendHeader(md) validate metadata which is %v got err :%v, want err :%v", test.md, err, test.want)
				}
				stream.SetTrailer(test.md)
			}
			if err := stream.Send(nil); err != nil {
				t.Fatalf("call stream.Send(nil) will success but got err :%v", err)
			}
			return nil
		},
	}
	if err := ss2.Start(nil); err != nil {
		t.Fatalf("Error starting ss2 endpoint server: %v", err)
	}
	defer ss2.Stop()

	if _, err := ss2.Client.FullDuplexCall(context.Background()); err != nil {
		t.Fatalf("call ss2.Client.FullDuplexCall(context.Background()) will success but got err :%v", err)
	}

}
