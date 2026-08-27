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

package autosharding

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func (s) TestSliceMap_Lookup(t *testing.T) {
	sm := &sliceMap{
		slices: []sliceMapEntry{
			{startKey: []byte("b"), endpoints: []int{1}},
			{startKey: []byte("d"), endpoints: []int{2}},
			{startKey: []byte("f"), endpoints: []int{3}},
		},
	}

	tests := []struct {
		name string
		key  []byte
		want int
	}{
		{
			name: "exact-match-first",
			key:  []byte("b"),
			want: 0,
		},
		{
			name: "exact-match-middle",
			key:  []byte("d"),
			want: 1,
		},
		{
			name: "exact-match-last",
			key:  []byte("f"),
			want: 2,
		},
		{
			name: "between-first-and-middle",
			key:  []byte("c"),
			want: 0,
		},
		{
			name: "between-middle-and-last",
			key:  []byte("e"),
			want: 1,
		},
		{
			name: "after-last",
			key:  []byte("g"),
			want: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sm.lookup(tc.key); got != tc.want {
				t.Errorf("lookup(%q) = %d, want %d", tc.key, got, tc.want)
			}
		})
	}
}

func (s) TestSliceMap_Lookup_Empty(t *testing.T) {
	sm := &sliceMap{}
	if got := sm.lookup([]byte("a")); got != -1 {
		t.Errorf("lookup() on empty map = %d, want -1", got)
	}
}

func (s) TestBuildSliceMap(t *testing.T) {
	// Setup endpointMap with unsorted order in map to verify deterministic
	// sorting of fallbackPool.
	epMap := &endpointMap{
		m: map[string]*endpointState{
			"hostC": {index: 2},
			"hostA": {index: 0},
			"hostB": {index: 1},
		},
	}

	tests := []struct {
		name       string
		assignment *assignment
		want       *sliceMap
	}{
		{
			name:       "nil-assignment-startup",
			assignment: nil,
			want:       &sliceMap{fallbackPool: []int{0, 1, 2}},
		},
		{
			name: "valid-assignment",
			assignment: &assignment{
				endpointNames: []string{"hostA", "hostB", "hostC", "hostD"},
				slices: []slice{
					{startKey: []byte("a"), endpoints: []int{0, 1}}, // hostA, hostB -> indices 0, 1
					{startKey: []byte("m"), endpoints: []int{1, 2}}, // hostB, hostC -> indices 1, 2
				},
				generation: 42,
			},
			want: &sliceMap{
				slices: []sliceMapEntry{
					{startKey: []byte("a"), endpoints: []int{0, 1}},
					{startKey: []byte("m"), endpoints: []int{1, 2}},
				},
				fallbackPool: []int{0, 1, 2},
				generation:   42,
			},
		},
		{
			name: "assignment-with-unknown-host",
			assignment: &assignment{
				endpointNames: []string{"hostA", "hostUnknown", "hostC"},
				slices: []slice{
					{startKey: []byte("a"), endpoints: []int{0, 1}}, // hostUnknown is skipped
				},
				generation: 43,
			},
			want: &sliceMap{
				slices: []sliceMapEntry{
					{startKey: []byte("a"), endpoints: []int{0}}, // Only hostA (index 0)
				},
				fallbackPool: []int{0, 1, 2},
				generation:   43,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildSliceMap(epMap, tc.assignment)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildSliceMap() diff (-want +got):\n%s", diff)
			}
		})
	}
}

func (e sliceMapEntry) Equal(b sliceMapEntry) bool {
	return bytes.Equal(e.startKey, b.startKey) && slices.Equal(e.endpoints, b.endpoints)
}

func (sm *sliceMap) Equal(b *sliceMap) bool {
	if sm == nil || b == nil {
		return sm == b
	}
	fallbackPoolEqual := slices.Equal(sm.fallbackPool, b.fallbackPool)
	slicesEqual := slices.EqualFunc(sm.slices, b.slices, func(x, y sliceMapEntry) bool { return x.Equal(y) })
	return sm.generation == b.generation && fallbackPoolEqual && slicesEqual
}

func BenchmarkSliceMap_Lookup(b *testing.B) {
	for _, numSlices := range []int{1, 10, 100, 1000, 10000} {
		for _, keySize := range []int{16, 32, 64, 128, 256, 512} {
			b.Run(fmt.Sprintf("slices_%d_keySize_%d", numSlices, keySize), func(b *testing.B) {
				sm := &sliceMap{
					slices: make([]sliceMapEntry, numSlices),
				}
				for i := 0; i < numSlices; i++ {
					// Generate lexicographically sorted keys to serve as slice
					// boundaries.  We multiply by 1000 to create ranges (gaps) between
					// successive slices (e.g., Slice 0 covers [0, 1000), Slice 1 covers
					// [1000, 2000)). This allows us to test lookups that fall
					// *mid-slice*, rather than just exact matches.
					//
					// We use zero-padding on the left via "%0*d" (where * is the keySize
					// width).  This fills the key to its full length (e.g., 512 bytes)
					// with leading zeros.  Scanning long identical prefixes forces
					// bytes.Compare to traverse the full length, simulating a
					// conservative, worst-case latency scenario.
					key := fmt.Appendf(nil, "%0*d", keySize, i*1000)
					sm.slices[i] = sliceMapEntry{startKey: key, endpoints: []int{i}}
				}

				// Pre-generate a pool of lookup keys. This helps avoid
				// measuring string formatting overhead inside the timer loop.
				lookupKeys := make([][]byte, 10000)
				for i := 0; i < 10000; i++ {
					// Distribute the 10,000 lookup keys proportionally across the entire
					// synthetic keyspace [0, numSlices * 1000). This ensures a good mix
					// of boundary hits and interior hits.
					val := i * (numSlices * 1000) / 10000
					lookupKeys[i] = fmt.Appendf(nil, "%0*d", keySize, val)
				}

				var i int
				for b.Loop() {
					sm.lookup(lookupKeys[i%10000])
					i++
				}
			})
		}
	}
}
