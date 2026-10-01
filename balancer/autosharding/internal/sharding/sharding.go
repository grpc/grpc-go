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

// Package sharding provides communication with an external sharding service
// to fetch shard assignments and make them available to the autosharding
// load balancing policy.
package sharding

// Slice represents a key range and its assigned endpoints. The end key of the
// range is not stored here because it is always the start key of the next
// Slice, or nil if this is the last Slice. This is guaranteed by the client
// responsible for generating the Assignment.
type Slice struct {
	StartKey  []byte // Inclusive start key of the key-range
	Endpoints []int  // Indices into Assignment.EndpointNames
}

// Assignment represents a complete snapshot of sharding assignments, and is
// expected to cover the entire key range.
type Assignment struct {
	Slices        []Slice  // Sorted by StartKey
	EndpointNames []string // Complete list of endpoint names
	Generation    int64
}
