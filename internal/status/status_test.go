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

package status_test

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/status"
)

func TestRawStatusProto(t *testing.T) {
	spb := status.RawStatusProto(nil)
	if spb != nil {
		t.Errorf("RawStatusProto(nil) = %v; must return nil", spb)
	}
	s := status.New(codes.Internal, "test internal error")
	spb1 := status.RawStatusProto(s)
	spb2 := status.RawStatusProto(s)
	// spb1 and spb2 should be the same pointer: no copies
	if spb1 != spb2 {
		t.Errorf("RawStatusProto(s)=%p then %p; must return the same pointer", spb1, spb2)
	}
}
