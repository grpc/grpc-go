/*
 *
 * Copyright 2024 gRPC authors.
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

package grpchttp2_test

import (
	"testing"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/transport/grpchttp2"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestErrorCodeString(t *testing.T) {
	err := grpchttp2.ErrCodeNoError
	if err.String() != "NO_ERROR" {
		t.Errorf("got %q, want %q", err.String(), "NO_ERROR")
	}

	err = grpchttp2.ErrCode(0x1)
	if err.String() != "PROTOCOL_ERROR" {
		t.Errorf("got %q, want %q", err.String(), "PROTOCOL_ERROR")
	}

	err = grpchttp2.ErrCode(0xf)
	if err.String() != "unknown error code 0xf" {
		t.Errorf("got %q, want %q", err.String(), "unknown error code 0xf")
	}
}
