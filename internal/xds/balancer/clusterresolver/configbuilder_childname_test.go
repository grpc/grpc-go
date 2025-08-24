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
		inputs [][][]xdsresource.Locality // Array of input steps
		want   []string
	}{
		{
			name:   "init, two new priorities",
			prefix: 3,
			inputs: [][][]xdsresource.Locality{
				nil, // first input is nil
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
				},
			},
			want: []string{"priority-3-0", "priority-3-1"},
		},
		{
			name:   "one new priority",
			prefix: 1,
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
				},
			},
			want: []string{"priority-1-0", "priority-1-1"},
		},
		{
			name:   "merge two priorities",
			prefix: 4,
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
				},
			},
			want: []string{"priority-4-0", "priority-4-2"},
		},
		{
			name: "swap two priorities",
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
				},
			},
			want: []string{"priority-0-1", "priority-0-0", "priority-0-2"},
		},
		{
			name: "split priority",
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}}, // This gets a newly generated name, since "0-0" was already picked.
					{{ID: clients.Locality{Zone: "L2"}}},
				},
			},
			want: []string{"priority-0-0", "priority-0-1", "priority-0-2"},
		},
		{
			name: "split and reverse priority",
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L2"}}},
					{{ID: clients.Locality{Zone: "L0"}}},
				},
			},
			want: []string{"priority-0-1", "priority-0-0"},
		},
		{
			name: "delete and bring back",
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L2"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L2"}}},
				},
			},
			want: []string{"priority-0-0"},
		},
		{
			name: "delete and bring back reverse",
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L2"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L2"}}, {ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L1"}}},
				},
			},
			want: []string{"priority-0-2"},
		},
		{
			name: "complex merge split sequence",
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
				},
			},
			want: []string{"priority-0-0", "priority-0-1", "priority-0-2"},
		},
		{
			name: "complex full merges splits sequence",
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}},
						{ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}},
						{ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}},
						{ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
				},
			},
			want: []string{"priority-0-0", "priority-0-1", "priority-0-2"},
		},
		{
			name: "merge-split reverse",
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L0"}}},
				},
			},
			want: []string{"priority-0-1", "priority-0-0"},
		},
		{
			name: "merge-split reverse times 2",
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L0"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L0"}}},
				},
			},
			want: []string{"priority-0-1", "priority-0-0"},
		},
		{
			name: "complex merge-split reverse",
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}, {ID: clients.Locality{Zone: "L3"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L3"}}, {ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L3"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L3"}}},
					{{ID: clients.Locality{Zone: "L0"}}},
				},
			},
			want: []string{"priority-0-3", "priority-0-0"},
		},
		{
			name: "2 by 2 shuffle sequence",
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}, {ID: clients.Locality{Zone: "L3"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}},
						{ID: clients.Locality{Zone: "L2"}}, {ID: clients.Locality{Zone: "L3"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L2"}}},
					{{ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L3"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
					{{ID: clients.Locality{Zone: "L3"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L3"}}},
					{{ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}, {ID: clients.Locality{Zone: "L3"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L2"}}},
					{{ID: clients.Locality{Zone: "L3"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L2"}}},
					{{ID: clients.Locality{Zone: "L3"}}},
				},
			},
			want: []string{"priority-0-0", "priority-0-1", "priority-0-2", "priority-0-3"},
		},
		{
			name: "traffic director proxyless example",
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L1"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L2"}}},
					{{ID: clients.Locality{Zone: "L3"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L2"}}},
					{{ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L3"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L3"}}, {ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L2"}}},
					{{ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L3"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L3"}}, {ID: clients.Locality{Zone: "L2"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L3"}}, {ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L3"}}, {ID: clients.Locality{Zone: "L2"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
				},
			},
			want: []string{"priority-0-3", "priority-0-1"},
		},
		{
			name: "defensive test - same zone in multiple priorities",
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L0"}}}, // Same zone in different priority
				},
			},
			want: []string{"priority-0-0", "priority-0-1"},
		},
		{
			name: "defensive test - zone conflict cascade",
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L2"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L1"}}}, // Both L0 and L1 conflict
				},
			},
			want: []string{"priority-0-1", "priority-0-0", "priority-0-3"},
		},
		{
			name: "defensive test - complex zone duplication across multiple priorities",
			inputs: [][][]xdsresource.Locality{
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L2"}}},
					{{ID: clients.Locality{Zone: "L0"}}, {ID: clients.Locality{Zone: "L3"}}},
				},
				{
					{{ID: clients.Locality{Zone: "L1"}}},
					{{ID: clients.Locality{Zone: "L0"}}},
					{{ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L2"}}},
					{{ID: clients.Locality{Zone: "L1"}}, {ID: clients.Locality{Zone: "L3"}}},
				},
			},
			want: []string{"priority-0-1", "priority-0-0", "priority-0-2", "priority-0-3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ng := newNameGenerator(tt.prefix)
			var got []string

			// Execute all input steps
			for i, input := range tt.inputs {
				got = ng.generate(input)
				t.Logf("Step %d: %v", i+1, got)
			}

			// The final result should match expected
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Errorf("generate() = got: %v, want: %v, diff (-got +want): %s", got, tt.want, diff)
			}
		})
	}
}
