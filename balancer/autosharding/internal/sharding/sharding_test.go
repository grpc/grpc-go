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

package sharding

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/protobuf/testing/protocmp"

	aspb "google.golang.org/grpc/balancer/autosharding/internal/proto"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func makeSliceAssignment(startKey, endKey []byte, epIndices ...int32) *aspb.SliceAssignment {
	eps := make([]*aspb.PerSliceEndpointState, len(epIndices))
	for i, idx := range epIndices {
		eps[i] = &aspb.PerSliceEndpointState{EndpointIndex: idx}
	}
	return &aspb.SliceAssignment{
		Slice: &aspb.Slice{
			StartKey: startKey,
			EndKey:   endKey,
		},
		Endpoints: eps,
	}
}

func makeEndpoints(names ...string) []*aspb.EndpointState {
	eps := make([]*aspb.EndpointState, len(names))
	for i, name := range names {
		eps[i] = &aspb.EndpointState{Endpoint: name}
	}
	return eps
}

func (s) TestValidateAssignment(t *testing.T) {
	longKey := bytes.Repeat([]byte("a"), 513)
	maxKey := bytes.Repeat([]byte("a"), 512)

	tests := []struct {
		name          string
		chunks        []*aspb.AssignmentChunk
		wantSlices    []*aspb.SliceAssignment
		wantEndpoints []string
		wantErrSubstr string
	}{
		{
			name: "single_chunk_all_valid",
			chunks: []*aspb.AssignmentChunk{
				{
					Endpoints: makeEndpoints("ep0", "ep1"),
					SliceAssignments: []*aspb.SliceAssignment{
						makeSliceAssignment(nil, []byte("m"), 0),
						makeSliceAssignment([]byte("m"), nil, 1),
					},
				},
			},
			wantSlices: []*aspb.SliceAssignment{
				makeSliceAssignment(nil, []byte("m"), 0),
				makeSliceAssignment([]byte("m"), nil, 1),
			},
			wantEndpoints: []string{"ep0", "ep1"},
		},
		{
			name: "multiple_chunks_unsorted_slices_and_max_512_byte_keys",
			chunks: []*aspb.AssignmentChunk{
				{
					Endpoints: makeEndpoints("ep0", "ep1"),
					SliceAssignments: []*aspb.SliceAssignment{
						makeSliceAssignment(maxKey, nil, 1),
					},
				},
				{
					Endpoints: makeEndpoints("ep2"),
					SliceAssignments: []*aspb.SliceAssignment{
						makeSliceAssignment([]byte{}, maxKey, 0, 2),
					},
				},
			},
			wantSlices: []*aspb.SliceAssignment{
				makeSliceAssignment([]byte{}, maxKey, 0, 2),
				makeSliceAssignment(maxKey, nil, 1),
			},
			wantEndpoints: []string{"ep0", "ep1", "ep2"},
		},
		{
			name: "missing_slice_in_assignment_covers_whole_keyspace",
			chunks: []*aspb.AssignmentChunk{
				{
					Endpoints: makeEndpoints("ep0"),
					SliceAssignments: []*aspb.SliceAssignment{
						{Endpoints: []*aspb.PerSliceEndpointState{{EndpointIndex: 0}}},
					},
				},
			},
			wantSlices: []*aspb.SliceAssignment{
				{Endpoints: []*aspb.PerSliceEndpointState{{EndpointIndex: 0}}},
			},
			wantEndpoints: []string{"ep0"},
		},
		{
			name: "missing_slice_in_assignment_overlaps_every_other_slice",
			chunks: []*aspb.AssignmentChunk{
				{
					Endpoints: makeEndpoints("ep0"),
					SliceAssignments: []*aspb.SliceAssignment{
						{Endpoints: []*aspb.PerSliceEndpointState{{EndpointIndex: 0}}},
						makeSliceAssignment([]byte("a"), nil, 0),
					},
				},
			},
			wantEndpoints: []string{"ep0"},
			wantErrSubstr: "2 overlapping slices",
		},
		{
			name: "keys_longer_than_512_bytes_dropped",
			chunks: []*aspb.AssignmentChunk{
				{
					Endpoints: makeEndpoints("ep0"),
					SliceAssignments: []*aspb.SliceAssignment{
						makeSliceAssignment(longKey, nil, 0),
						makeSliceAssignment([]byte("a"), longKey, 0),
						makeSliceAssignment([]byte(""), []byte("a"), 0),
					},
				},
			},
			wantSlices: []*aspb.SliceAssignment{
				makeSliceAssignment([]byte(""), []byte("a"), 0),
			},
			wantEndpoints: []string{"ep0"},
			wantErrSubstr: "1 slice(s) with start_key longer than 512 bytes; 1 slice(s) with end_key longer than 512 bytes",
		},
		{
			name: "start_key_greater_than_or_equal_to_end_key_dropped",
			chunks: []*aspb.AssignmentChunk{
				{
					Endpoints: makeEndpoints("ep0"),
					SliceAssignments: []*aspb.SliceAssignment{
						makeSliceAssignment(nil, []byte{}, 0),            // "" >= "" (explicit empty end_key)
						makeSliceAssignment([]byte("d"), []byte("d"), 0), // "d" == "d"
						makeSliceAssignment([]byte("m"), []byte("b"), 0), // "m" > "b"
						makeSliceAssignment([]byte("z"), nil, 0),         // valid
					},
				},
			},
			wantSlices: []*aspb.SliceAssignment{
				makeSliceAssignment([]byte("z"), nil, 0),
			},
			wantEndpoints: []string{"ep0"},
			wantErrSubstr: "3 slice(s) with start_key >= end_key",
		},
		{
			name: "invalid_endpoint_indices_dropped",
			chunks: []*aspb.AssignmentChunk{
				{
					Endpoints: makeEndpoints("ep0", "ep1"),
					SliceAssignments: []*aspb.SliceAssignment{
						makeSliceAssignment([]byte("a"), []byte("d"), -1),
						makeSliceAssignment([]byte("d"), []byte("m"), 0, 2),
						makeSliceAssignment([]byte("m"), nil, 0, 1),
					},
				},
			},
			wantSlices: []*aspb.SliceAssignment{
				makeSliceAssignment([]byte("m"), nil, 0, 1),
			},
			wantEndpoints: []string{"ep0", "ep1"},
			wantErrSubstr: "2 slice(s) with invalid endpoint indices",
		},
		{
			name: "two_overlapping_slices_both_dropped_non_overlapping_kept",
			chunks: []*aspb.AssignmentChunk{
				{
					Endpoints: makeEndpoints("ep0", "ep1"),
					SliceAssignments: []*aspb.SliceAssignment{
						makeSliceAssignment([]byte("a"), []byte("f"), 0),
						makeSliceAssignment([]byte("d"), []byte("k"), 1),
						makeSliceAssignment([]byte("m"), nil, 0),
					},
				},
			},
			wantSlices: []*aspb.SliceAssignment{
				makeSliceAssignment([]byte("m"), nil, 0),
			},
			wantEndpoints: []string{"ep0", "ep1"},
			wantErrSubstr: "2 overlapping slices",
		},
		{
			name: "three_way_overlap_all_dropped_non_overlapping_kept",
			chunks: []*aspb.AssignmentChunk{
				{
					Endpoints: makeEndpoints("ep0"),
					SliceAssignments: []*aspb.SliceAssignment{
						makeSliceAssignment([]byte("a"), []byte("z"), 0),
						makeSliceAssignment([]byte("b"), []byte("d"), 0),
						makeSliceAssignment([]byte("m"), []byte("p"), 0),
						makeSliceAssignment([]byte("z"), nil, 0),
					},
				},
			},
			wantSlices: []*aspb.SliceAssignment{
				makeSliceAssignment([]byte("z"), nil, 0),
			},
			wantEndpoints: []string{"ep0"},
			wantErrSubstr: "3 overlapping slices",
		},
		{
			name: "same_start_key_overlap_both_dropped",
			chunks: []*aspb.AssignmentChunk{
				{
					Endpoints: makeEndpoints("ep0"),
					SliceAssignments: []*aspb.SliceAssignment{
						makeSliceAssignment([]byte("a"), []byte("d"), 0),
						makeSliceAssignment([]byte("a"), []byte("f"), 0),
						makeSliceAssignment([]byte("f"), nil, 0),
					},
				},
			},
			wantSlices: []*aspb.SliceAssignment{
				makeSliceAssignment([]byte("f"), nil, 0),
			},
			wantEndpoints: []string{"ep0"},
			wantErrSubstr: "2 overlapping slices",
		},
		{
			name: "nil_end_key_not_last_slice_drops_itself_and_subsequent_slices",
			chunks: []*aspb.AssignmentChunk{
				{
					Endpoints: makeEndpoints("ep0"),
					SliceAssignments: []*aspb.SliceAssignment{
						makeSliceAssignment([]byte(""), []byte("d"), 0),
						makeSliceAssignment([]byte("d"), nil, 0),
						makeSliceAssignment([]byte("m"), []byte("z"), 0),
					},
				},
			},
			wantSlices: []*aspb.SliceAssignment{
				makeSliceAssignment([]byte(""), []byte("d"), 0),
			},
			wantEndpoints: []string{"ep0"},
			wantErrSubstr: "2 overlapping slices",
		},
		{
			name: "all_slices_invalid_returns_empty_slices",
			chunks: []*aspb.AssignmentChunk{
				{
					Endpoints: makeEndpoints("ep0"),
					SliceAssignments: []*aspb.SliceAssignment{
						makeSliceAssignment([]byte("a"), []byte("f"), 0),
						makeSliceAssignment([]byte("d"), nil, 0),
					},
				},
			},
			wantSlices:    nil,
			wantEndpoints: []string{"ep0"},
			wantErrSubstr: "2 overlapping slices",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotSlices, gotEndpoints, gotErrMsg := validateAssignment(tc.chunks)
			if diff := cmp.Diff(tc.wantSlices, gotSlices, protocmp.Transform(), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("validateAssignment() slices diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantEndpoints, gotEndpoints, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("validateAssignment() endpoints diff (-want +got):\n%s", diff)
			}
			if tc.wantErrSubstr == "" && gotErrMsg != "" {
				t.Errorf("validateAssignment() returned unexpected error message: %q", gotErrMsg)
			}
			if tc.wantErrSubstr != "" && !strings.Contains(gotErrMsg, tc.wantErrSubstr) {
				t.Errorf("validateAssignment() error message %q does not contain %q", gotErrMsg, tc.wantErrSubstr)
			}
		})
	}
}

func (s) TestBuildAssignment(t *testing.T) {
	tests := []struct {
		name           string
		slices         []*aspb.SliceAssignment
		endpoints      []string
		generation     int64
		wantAssignment *Assignment
	}{
		{
			name: "contiguous_slices_no_gaps",
			slices: []*aspb.SliceAssignment{
				makeSliceAssignment(nil, []byte("d"), 0),
				makeSliceAssignment([]byte("d"), []byte("m"), 1, 0),
				makeSliceAssignment([]byte("m"), nil, 1),
			},
			endpoints:  []string{"ep0", "ep1"},
			generation: 3,
			wantAssignment: &Assignment{
				EndpointNames: []string{"ep0", "ep1"},
				Generation:    3,
				Slices: []Slice{
					{StartKey: []byte{}, Endpoints: []int{0}},
					{StartKey: []byte("d"), Endpoints: []int{1, 0}},
					{StartKey: []byte("m"), Endpoints: []int{1}},
				},
			},
		},
		{
			name: "leading_gap_filled",
			slices: []*aspb.SliceAssignment{
				makeSliceAssignment([]byte("d"), nil, 0),
			},
			endpoints:  []string{"ep0"},
			generation: 1,
			wantAssignment: &Assignment{
				EndpointNames: []string{"ep0"},
				Generation:    1,
				Slices: []Slice{
					{StartKey: []byte{}, Endpoints: []int{}},
					{StartKey: []byte("d"), Endpoints: []int{0}},
				},
			},
		},
		{
			name: "middle_gap_filled",
			slices: []*aspb.SliceAssignment{
				makeSliceAssignment([]byte{}, []byte("d"), 0),
				makeSliceAssignment([]byte("m"), nil, 1),
			},
			endpoints:  []string{"ep0", "ep1"},
			generation: 2,
			wantAssignment: &Assignment{
				EndpointNames: []string{"ep0", "ep1"},
				Generation:    2,
				Slices: []Slice{
					{StartKey: []byte{}, Endpoints: []int{0}},
					{StartKey: []byte("d"), Endpoints: []int{}},
					{StartKey: []byte("m"), Endpoints: []int{1}},
				},
			},
		},
		{
			name: "trailing_gap_filled",
			slices: []*aspb.SliceAssignment{
				makeSliceAssignment([]byte{}, []byte("m"), 0),
			},
			endpoints:  []string{"ep0"},
			generation: 4,
			wantAssignment: &Assignment{
				EndpointNames: []string{"ep0"},
				Generation:    4,
				Slices: []Slice{
					{StartKey: []byte{}, Endpoints: []int{0}},
					{StartKey: []byte("m"), Endpoints: []int{}},
				},
			},
		},
		{
			name: "leading_middle_and_trailing_gaps_filled",
			slices: []*aspb.SliceAssignment{
				makeSliceAssignment([]byte("b"), []byte("d"), 0),
				makeSliceAssignment([]byte("f"), []byte("k"), 1),
			},
			endpoints:  []string{"ep0", "ep1"},
			generation: 5,
			wantAssignment: &Assignment{
				EndpointNames: []string{"ep0", "ep1"},
				Generation:    5,
				Slices: []Slice{
					{StartKey: []byte{}, Endpoints: []int{}},
					{StartKey: []byte("b"), Endpoints: []int{0}},
					{StartKey: []byte("d"), Endpoints: []int{}},
					{StartKey: []byte("f"), Endpoints: []int{1}},
					{StartKey: []byte("k"), Endpoints: []int{}},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildAssignment(tc.slices, tc.endpoints, tc.generation)
			if diff := cmp.Diff(tc.wantAssignment, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("buildAssignment() diff (-want +got):\n%s", diff)
			}
		})
	}
}

func (s) TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxRunes int
		want     string
	}{
		{
			name:     "empty_string",
			input:    "",
			maxRunes: 5,
			want:     "",
		},
		{
			name:     "shorter_than_max",
			input:    "hello",
			maxRunes: 10,
			want:     "hello",
		},
		{
			name:     "equal_to_max",
			input:    "hello",
			maxRunes: 5,
			want:     "hello",
		},
		{
			name:     "longer_than_max_ascii",
			input:    "hello world",
			maxRunes: 5,
			want:     "hello",
		},
		{
			name:     "longer_than_max_multibyte_utf8",
			input:    "αβγδε",
			maxRunes: 3,
			want:     "αβγ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncateRunes(tc.input, tc.maxRunes); got != tc.want {
				t.Errorf("truncateRunes(%q, %d) = %q, want %q", tc.input, tc.maxRunes, got, tc.want)
			}
		})
	}
}
