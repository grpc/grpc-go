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

package cdsbalancer

import (
	"fmt"

	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
)

// nameGenerator generates a child name for a list of priorities (each priority
// is a list of localities).
//
// The purpose of this generator is to reuse names between updates. So the
// struct keeps state between generate() calls, and a later generate() might
// return names returned by the previous call.
type nameGenerator struct {
	existingNames map[clients.Locality]string
	// existingEmptyPriorityNames stores names assigned to priorities with no
	// localities in the previous update, so they can be reused in subsequent
	// updates to maintain name stability.
	existingEmptyPriorityNames []string
	prefix                     uint64
	nextID                     uint64
}

func newNameGenerator(prefix uint64) *nameGenerator {
	return &nameGenerator{prefix: prefix}
}

// generate returns a list of names for the given list of priorities.
//
// Each priority is a list of localities. The name for the priority is picked as
// - for each locality in this priority, if it exists in the existing names,
// this priority will reuse the name
// - if no reusable name is found for this priority, a new name is generated
//
// For example:
// - update 1: [[L1], [L2], [L3]] --> ["0", "1", "2"]
// - update 2: [[L1], [L2], [L3]] --> ["0", "1", "2"]
// - update 3: [[L1, L2], [L3]] --> ["0", "2"]   (Two priorities were merged)
// - update 4: [[L1], [L4]] --> ["0", "3",]      (A priority was split, and a new priority was added)
func (ng *nameGenerator) generate(priorities [][]xdsresource.Locality) []string {
	var ret []string
	usedNames := make(map[string]bool)
	newNames := make(map[clients.Locality]string)
	var newEmptyPriorityNames []string
	emptyPriorityIdx := 0
	for _, priority := range priorities {
		var nameFound string
		for _, locality := range priority {
			if name, ok := ng.existingNames[locality.ID]; ok {
				if !usedNames[name] {
					nameFound = name
					// Found a name to use. No need to process the remaining
					// localities.
					break
				}
			}
		}

		if nameFound == "" {
			// For priorities with no localities, try to reuse a name from the
			// previous update's empty-priority names list (in order) to
			// maintain stability across updates.
			//
			// If the candidate name is already in usedNames (e.g. due to a
			// priority reorder where a non-empty priority claimed it first),
			// emptyPriorityIdx does not advance and a new name is generated
			// instead. xDS priority ordering guarantees that each priority is
			// processed in index order, so a collision can only occur when a
			// previously-empty priority now has localities — in that case
			// falling through to generate a fresh name is the correct
			// behaviour.
			if len(priority) == 0 && emptyPriorityIdx < len(ng.existingEmptyPriorityNames) {
				candidate := ng.existingEmptyPriorityNames[emptyPriorityIdx]
				if !usedNames[candidate] {
					nameFound = candidate
					emptyPriorityIdx++
				}
			}
		}

		if nameFound == "" {
			// No appropriate used name is found. Make a new name.
			nameFound = fmt.Sprintf("priority-%d-%d", ng.prefix, ng.nextID)
			ng.nextID++
		}

		ret = append(ret, nameFound)
		// All localities in this priority share the same name. Add them all to
		// the new map.
		for _, l := range priority {
			newNames[l.ID] = nameFound
		}
		if len(priority) == 0 {
			newEmptyPriorityNames = append(newEmptyPriorityNames, nameFound)
		}
		usedNames[nameFound] = true
	}
	ng.existingNames = newNames
	ng.existingEmptyPriorityNames = newEmptyPriorityNames
	return ret
}
