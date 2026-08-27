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
	"slices"
	"sort"
)

// sliceMapEntry represents an entry for a key-range in the sliceMap.
type sliceMapEntry struct {
	startKey  []byte // Inclusive start key of the key-range
	endpoints []int  // Indices into list[PickerEndpoint] in the Picker
}

// sliceMap is a data structure optimized for lookups. Given a key, it returns a
// matching key-range.
//
// The sliceMap must be immutable, allowing the Picker to access it without any
// explicit synchronization with the LB policy.
//
// The sliceMap is meant to be used by the Picker in conjunction with a list of
// pickerEndpoints such that the list can be swapped out, as long as the number
// and order of endpoints don't change.
type sliceMap struct {
	slices       []sliceMapEntry // Sorted by startKey
	fallbackPool []int           // Indices into list[PickerEndpoint] for all endpoints
	generation   int64           // Snapshot generation number
}

// lookup returns the index of the slice covering the given key.
//
// Because assignments are pre-validated to have no gaps and cover the full key
// range, and since slices is sorted by startKey, lookup boils down to a binary
// search, looking for the insertion point.
//
// If the key matches the startKey of a slice, that slice index is returned.
// Otherwise, the index of the slice immediately preceding the insertion point
// is returned (i.e., the slice covering the range [startKey, nextStartKey)).
//
// Returns -1 when the sliceMap is empty, which is the case before the first
// assignment is received.
func (sm *sliceMap) lookup(key []byte) int {
	if len(sm.slices) == 0 {
		return -1
	}

	idx, found := slices.BinarySearchFunc(sm.slices, key, func(e sliceMapEntry, k []byte) int {
		return bytes.Compare(e.startKey, k)
	})

	if found {
		return idx
	}

	// Key falls in range [slices[idx - 1].startKey, slices[idx].startKey).
	return idx - 1
}

// buildSliceMap is used to generate a new sliceMap from the EndpointMap and
// Assignment when either of them change.
func buildSliceMap(endpointMap *endpointMap, assignment *assignment) *sliceMap {
	sm := &sliceMap{}

	// Populate fallbackPool deterministically sorted by endpoint index.
	states := make([]*endpointState, 0, len(endpointMap.m))
	for _, es := range endpointMap.m {
		states = append(states, es)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].index < states[j].index
	})
	sm.fallbackPool = make([]int, len(states))
	for i, es := range states {
		sm.fallbackPool[i] = es.index
	}

	// If no assignment has been received yet (startup case), return early with
	// empty slices.
	if assignment == nil {
		return sm
	}

	sm.generation = assignment.generation

	// Build sliceMapEntry for each Slice in the assignment.
	sm.slices = make([]sliceMapEntry, 0, len(assignment.slices))
	for _, s := range assignment.slices {
		entry := sliceMapEntry{
			startKey:  s.startKey,
			endpoints: []int{},
		}

		for _, idx := range s.endpoints {
			hostname := assignment.endpointNames[idx]
			if es, ok := endpointMap.m[hostname]; ok {
				entry.endpoints = append(entry.endpoints, es.index)
			}
		}

		sm.slices = append(sm.slices, entry)
	}

	return sm
}
