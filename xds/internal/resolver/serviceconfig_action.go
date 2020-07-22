/*
 *
 * Copyright 2020 gRPC authors.
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

package resolver

import (
	"fmt"
	"sort"
	"strconv"

	xdsclient "google.golang.org/grpc/xds/internal/client"
)

type actionWithAssignedName struct {
	// cluster:weight, "A":40, "B":60
	clustersWithWeights map[string]uint32
	// clusterNames, without weights, sorted and hashed, "A_B_"
	clusterNames string
	// The assigned name, clusters plus index number, "A_B_1"
	assignedName string
}

// newActionsFromRoutes gets actions from the routes, and turns them into a map
// keyed by the hash of the clusters.
//
// In the returned map, all actions don't have assignedName. The assignedName
// will be filled in after comparing the new actions with the existing actions,
// so when a new and old action only diff in weights, the new action can reuse
// the old action's name.
//
// from
//   {B:60, A:40}, {A:30, B:70}, {B:90, C:10}
//
// to
//   A40_B60_: {{A:40, B:60}, "A_B_", ""}
//   A30_B70_: {{A:30, B:70}, "A_B_", ""}
//   B90_C10_: {{B:90, C:10}, "B_C_", ""}
func newActionsFromRoutes(routes []*xdsclient.Route) map[string]actionWithAssignedName {
	newActions := make(map[string]actionWithAssignedName)
	for _, route := range routes {
		var clusterNames []string
		for n := range route.Action {
			clusterNames = append(clusterNames, n)
		}

		// Sort names to be consistent.
		sort.Strings(clusterNames)
		clustersOnly := ""
		clustersWithWeight := ""
		for _, c := range clusterNames {
			// Generates A_B_
			clustersOnly = clustersOnly + c + "_"
			// Generates A40_B60_
			clustersWithWeight = clustersWithWeight + c + strconv.FormatUint(uint64(route.Action[c]), 10) + "_"
		}

		if _, ok := newActions[clustersWithWeight]; !ok {
			newActions[clustersWithWeight] = actionWithAssignedName{
				clustersWithWeights: route.Action,
				clusterNames:        clustersOnly,
			}
		}
	}
	return newActions
}

// updateActions takes a new map of actions, and updates the existing action map in the resolver.
//
// In the old map, all actions have assignedName set.
// In the new map, all actions have no assignedName.
//
// After the update, the action map is updated to have all actions from the new
// map, with assignedName:
// - if the new action exists in old, get the old name
// - if the new action doesn't exist in old
//   - if there is an old action that will be removed, and has the same set of
//   clusters, reuse the old action's name
//   - otherwise, generate a new name
func (r *xdsResolver) updateActions(newActions map[string]actionWithAssignedName) {
	if r.actions == nil {
		r.actions = make(map[string]actionWithAssignedName)
	}

	// Delete actions from existingActions if they are not in newActions. Keep
	// the removed actions in a map, with key as clusterNames without weights,
	// so their assigned names can be reused.
	existingActions := r.actions
	actionsRemoved := make(map[string][]string)
	for actionHash, act := range existingActions {
		if _, ok := newActions[actionHash]; !ok {
			actionsRemoved[act.clusterNames] = append(actionsRemoved[act.clusterNames], act.assignedName)
			delete(existingActions, actionHash)
		}
	}

	// Find actions in newActions but not in oldActions. Add them, and try to
	// reuse assigned names from actionsRemoved.
	if r.nextIndex == nil {
		r.nextIndex = make(map[string]uint64)
	}
	nextIndex := r.nextIndex
	for actionHash, act := range newActions {
		if _, ok := existingActions[actionHash]; !ok {
			if assignedNamed, ok := actionsRemoved[act.clusterNames]; ok {
				// Reuse the first assigned name from actionsRemoved.
				act.assignedName = assignedNamed[0]
				// If there are more names to reuse after this, update the slice
				// in the map. Otherwise, remove the entry from the map.
				if len(assignedNamed) > 1 {
					actionsRemoved[act.clusterNames] = assignedNamed[1:]
				} else {
					delete(actionsRemoved, act.clusterNames)
				}
				existingActions[actionHash] = act
				continue
			}
			// Generate a new name.
			idx := nextIndex[act.clusterNames]
			nextIndex[act.clusterNames]++
			act.assignedName = fmt.Sprintf("%s%d", act.clusterNames, idx)
			existingActions[actionHash] = act
		}
	}

	// Delete entry from nextIndex if all actions with the clusters are removed.
	remainingClusterNames := make(map[string]bool)
	for _, act := range existingActions {
		remainingClusterNames[act.clusterNames] = true
	}
	for clusterNames := range nextIndex {
		if !remainingClusterNames[clusterNames] {
			delete(nextIndex, clusterNames)
		}
	}
	// For all other entries, don't try to decrement the index number. Because
	// there are cases that a_b_c_1 is in use, but a_b_c_0 is removed.
}

// getActionAssignedName hashes the clusters from the action, and find the
// assigned action name. The assigned action names are kept in r.actions, with
// the clusters name hash as map key.
//
// The assigned action name is not simply the hash. For example, the hash can be
// "A40_B60_", but the assigned name can be "A_B_0". It's this way so the action
// can be reused if only weights are changing.
func (r *xdsResolver) getActionAssignedName(action map[string]uint32) string {
	var clusterNames []string
	for n := range action {
		clusterNames = append(clusterNames, n)
	}
	// Hash cluster names. Sort names to be consistent.
	sort.Strings(clusterNames)
	clustersWithWeight := ""
	for _, c := range clusterNames {
		// Generates hash "A40_B60_".
		clustersWithWeight = clustersWithWeight + c + strconv.FormatUint(uint64(action[c]), 10) + "_"
	}
	// Look in r.actions for the assigned action name.
	if act, ok := r.actions[clustersWithWeight]; ok {
		return act.assignedName
	}
	r.logger.Warningf("no assigned name found for action %v", action)
	return ""
}
