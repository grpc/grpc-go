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

package grpc

import (
	"testing"
	"unsafe"
)

// TestServerStreamSize verifies that serverStream's bool fields are grouped at
// the tail of the struct to avoid alignment padding. If this test fails, add
// new bool fields in the grouped section, not inline.
func (s) TestServerStreamSize(t *testing.T) {
	if sz := unsafe.Sizeof(serverStream{}); sz > 256 {
		t.Fatalf("serverStream size = %d, want <= 256 (add new bool fields to the grouped section in stream.go)", sz)
	}
}

// TestCsAttemptSize verifies that csAttempt's bool fields are grouped at the
// tail of the struct to avoid alignment padding. If this test fails, add new
// bool fields in the grouped section, not inline.
func (s) TestCsAttemptSize(t *testing.T) {
	if sz := unsafe.Sizeof(csAttempt{}); sz > 232 {
		t.Fatalf("csAttempt size = %d, want <= 232 (add new bool fields to the grouped section in stream.go)", sz)
	}
}

// TestAddrConnStreamSize verifies that addrConnStream's bool fields are grouped
// at the tail of the struct to avoid alignment padding. If this test fails, add
// new bool fields in the grouped section, not inline.
func (s) TestAddrConnStreamSize(t *testing.T) {
	if sz := unsafe.Sizeof(addrConnStream{}); sz > 256 {
		t.Fatalf("addrConnStream size = %d, want <= 256 (add new bool fields to the grouped section in stream.go)", sz)
	}
}
