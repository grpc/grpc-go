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
 */

package clusterresolver

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
)

func Test_nameGenerator_generate(t *testing.T) {
	tests := []struct {
		name   string
		prefix uint64
		input1 [][]xdsresource.Locality
		input2 [][]xdsresource.Locality
		want   []string
	}{
		{
			name:   "init, two new priorities",
			prefix: 3,
			input1: nil,
			input2: [][]xdsresource.Locality{
				{{ID: clients.Locality{Zone: "L0"}}},
				{{ID: clients.Locality{Zone: "L1"}}},
			},
			want: []string{"priority-3-0", "priority-3-1"},
		},
		{
			name:   "one new priority",
			prefix: 1,
			input1: [][]xdsresource.Locality{
				{{ID: clients.Locality{Zone: "L0"}}},
			},
			input2: [][]xdsresource.Locality{
				{{ID: clients.Locality{Zone: "L0"}}},
				{{ID: clients.Locality{Zone: "L1"}}},
			},
			want: []string{"priority-1-0", "priority-1-1"},
		},
		{
			name:   "merge two priorities",
			prefix: 4,
			input1: [][]xdsresource.Locality{
				{{ID: clients.Locality{Zone: "L0"}}},
				{{ID: clients.Locality{Zone: "L1"}}},
				{{ID: clients.Locality{Zone: "L2"}}},
			},
			input2: [][]xdsresource.Locality{
				{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}},
				{{ID: clients.Locality{Zone: "L2"}}},
			},
			want: []string{"priority-4-0", "priority-4-2"},
		},
		{
			name: "swap two priorities",
			input1: [][]xdsresource.Locality{
				{{ID: clients.Locality{Zone: "L0"}}},
				{{ID: clients.Locality{Zone: "L1"}}},
				{{ID: clients.Locality{Zone: "L2"}}},
			},
			input2: [][]xdsresource.Locality{
				{{ID: clients.Locality{Zone: "L1"}}},
				{{ID: clients.Locality{Zone: "L0"}}},
				{{ID: clients.Locality{Zone: "L2"}}},
			},
			want: []string{"priority-0-1", "priority-0-0", "priority-0-2"},
		},
		{
			name: "split priority",
			input1: [][]xdsresource.Locality{
				{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}},
				{{ID: clients.Locality{Zone: "L2"}}},
			},
			input2: [][]xdsresource.Locality{
				{{ID: clients.Locality{Zone: "L0"}}},
				{{ID: clients.Locality{Zone: "L1"}}}, // This gets a newly generated name, since "0-0" was already picked.
				{{ID: clients.Locality{Zone: "L2"}}},
			},
			want: []string{"priority-0-0", "priority-0-2", "priority-0-1"},
		},
		{
			name:   "merge priorities, pick lowest existing name",
			prefix: 0,
			// Initial state simulation:
			// Update 1: P0=[A], P1=[B, C, D, E]
			// We simulate this by input1.
			// A has 0-0. B has 0-1.
			input1: [][]xdsresource.Locality{
				{{ID: clients.Locality{Zone: "A"}}},
				{{ID: clients.Locality{Zone: "B"}}, {ID: clients.Locality{Zone: "C"}}, {ID: clients.Locality{Zone: "D"}}, {ID: clients.Locality{Zone: "E"}}},
			},
			// Update 2: P0=[B, A], P1=[C, D, E]
			input2: [][]xdsresource.Locality{
				// B comes first. Previous code picks 0-1. We want 0-0 (A's name).
				{{ID: clients.Locality{Zone: "B"}}, {ID: clients.Locality{Zone: "A"}}},
				{{ID: clients.Locality{Zone: "C"}}, {ID: clients.Locality{Zone: "D"}}, {ID: clients.Locality{Zone: "E"}}},
			},
			// We want P0 to be "priority-0-0" (stable with A).
			// We want P1 to be "priority-0-1" (stable with C).
			want: []string{"priority-0-0", "priority-0-1"},
		},
		{
			name:   "complex shuffle, verify stability",
			prefix: 0,
			// Initial: P0=[A,B,C] (0-0), P1=[D,E] (0-1)
			input1: [][]xdsresource.Locality{
				{{ID: clients.Locality{Zone: "A"}}, {ID: clients.Locality{Zone: "B"}}, {ID: clients.Locality{Zone: "C"}}},
				{{ID: clients.Locality{Zone: "D"}}, {ID: clients.Locality{Zone: "E"}}},
			},
			// Update: P0=[B,C], P1=[A,D,E]
			// P0 takes 0-0 (from B,C).
			// P1 takes 0-1 (from D,E). A's 0-0 is taken.
			input2: [][]xdsresource.Locality{
				{{ID: clients.Locality{Zone: "B"}}, {ID: clients.Locality{Zone: "C"}}},
				{{ID: clients.Locality{Zone: "A"}}, {ID: clients.Locality{Zone: "D"}}, {ID: clients.Locality{Zone: "E"}}},
			},
			want: []string{"priority-0-0", "priority-0-1"},
		},
		{
			name:   "traffic spillover reversal (sticky naming)",
			prefix: 0,
			// Initial: P0=[A] (0-0), P1=[B] (0-1)
			input1: [][]xdsresource.Locality{
				{{ID: clients.Locality{Zone: "A"}}},
				{{ID: clients.Locality{Zone: "B"}}},
			},
			// Update 2: Recovery. P0=[B, A].
			// Candidates for P0: [0-1 (from B), 0-0 (from A)].
			// Sticky check: Ordered[0] (from Update 1) is 0-1.
			// 0-1 IS in candidates.
			// Pick 0-1.
			// (Simple sort would pick 0-0, causing churn).
			input2: [][]xdsresource.Locality{
				{{ID: clients.Locality{Zone: "B"}}, {ID: clients.Locality{Zone: "A"}}},
			},
			want: []string{"priority-0-1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ng := newNameGenerator(tt.prefix)
			got1 := ng.generate(tt.input1)
			t.Logf("Update 1: %v", got1)

			// Special handling for the sticky test case to simulate the intermediate failover step
			if tt.name == "traffic spillover reversal (sticky naming)" {
				// Intermediate Failover Step: P0=[B]
				failoverInput := [][]xdsresource.Locality{
					{{ID: clients.Locality{Zone: "B"}}},
				}
				gotFailover := ng.generate(failoverInput)
				t.Logf("Update Failover: %v", gotFailover)

				// Verification of Failover state: P0 should take 0-1 (B's name)
				// Because B (0-1) is the only candidate, and 0-0 (from A) is gone.
				if diff := cmp.Diff(gotFailover, []string{"priority-0-1"}); diff != "" {
					t.Errorf("Failover generate() = got: %v, want: %v, diff (-got +want): %s", gotFailover, []string{"priority-0-1"}, diff)
				}
			}

			got := ng.generate(tt.input2)
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("generate() = got: %v, want: %v, diff (-got +want): %s", got, tt.want, diff)
			}
		})
	}
}
