/*
 *
 * Copyright 2025 gRPC authors.
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

	"google.golang.org/grpc/codes"
)

func TestRawStatusProto(t *testing.T) {
	spb := RawStatusProto(nil)
	if spb != nil {
		t.Errorf("RawStatusProto(nil) must return nil (was %v)", spb)
	}
	s := New(codes.Internal, "test internal error")
	spb = RawStatusProto(s)
	if spb != s.s {
		t.Errorf("RawStatusProto(s) must return s.s=%p (was %p)", s.s, spb)
	}
}
