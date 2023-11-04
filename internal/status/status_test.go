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

package status

import (
	"testing"

	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"

	protoV1 "github.com/golang/protobuf/proto"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestWithDetailsVersionsCompat(t *testing.T) {
	details := []protoreflect.ProtoMessage{ // Create with old ptypes and new tspb
		ptypes.TimestampNow(),
		&timestamppb.Timestamp{},
	}
	status := New(codes.InvalidArgument, "an error")

	if _, err := status.WithDetails(details...); err != nil {
		t.Fatal(err)
	}
}

func (s) TestDetailsVersionsCompat(t *testing.T) {
	details := []protoreflect.ProtoMessage{ // Create with old ptypes and new tspb
		ptypes.TimestampNow(),
		timestamppb.Now(),
	}
	status := New(codes.InvalidArgument, "an error")
	status, err := status.WithDetails(details...)

	if err != nil {
		t.Fatal(err)
	}

	for _, detail := range status.Details() {
		if _, ok := detail.(protoV1.Message); !ok {
			t.Fatalf("detail with type %T is not a protoV1.Message", detail)
		}
		if _, ok := detail.(*timestamppb.Timestamp); !ok {
			t.Fatalf("detail with type %T is not a timestamppb.Timestamp", detail)
		}
	}
}
