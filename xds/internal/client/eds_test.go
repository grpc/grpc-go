/*
 *
 * Copyright 2019 gRPC authors.
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

package client

import (
	"testing"
)

// Only error cases are tested, normal cases are covered because EDS balancer
// tests build an EDS responses and parses them.
// TODO: add more tests, with error cases and normal cases.

// Test that parsing fails if EDS response doesn't have all priorities.
// Priorities should range from 0 (highest) to N (lowest) without skipping
func TestParseEDSRespProtoPriorityError(t *testing.T) {
	clab0 := NewClusterLoadAssignmentBuilder("test", nil)
	clab0.AddLocality("locality-1", 1, 0, []string{"addr1:314"}, nil)
	clab0.AddLocality("locality-2", 1, 2, []string{"addr2:159"}, nil)
	_, err := ParseEDSRespProto(clab0.Build())
	if err == nil {
		t.Errorf("ParseEDSRespProto() error = %v, wantErr <non-nil>", err)
		return
	}
}
