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
 */

package testutils

import (
	"fmt"
	"google.golang.org/protobuf/protoadapt"
	"testing"

	"github.com/golang/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// MarshalAny is a convenience function to marshal protobuf messages into any
// protos. It will panic if the marshaling fails.
func MarshalAny(m proto.Message) (*anypb.Any, error) {
	a, err := anypb.New(protoadapt.MessageV2Of(m))
	if err != nil {
		return nil, fmt.Errorf("anypb.New(%+v) failed: %w", m, err)
	}
	return a, nil
}

// TestMarshalAny is a wrapper function for testing. It marks itself as a test helper.
// It uses MarshalAny and calls t.Fatalf if marshaling fails.
func TestMarshalAny(t *testing.T, m proto.Message) *anypb.Any {
	t.Helper()
	a, err := MarshalAny(m)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}
	return a
}
