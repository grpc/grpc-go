// Copyright 2020 Envoyproxy Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

package cache

import (
	"context"

	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/server/stream/v3"
)

// groups together resource-related arguments for the createDeltaResponse function
type resourceContainer struct {
	resourceMap   map[string]types.Resource
	versionMap    map[string]string
	systemVersion string
}

func createDeltaResponse(ctx context.Context, req *DeltaRequest, state stream.StreamState, resources resourceContainer) *RawDeltaResponse {
	// variables to build our response with
	var nextVersionMap map[string]string
	var filtered []types.Resource
	var toRemove []string

	// If we are handling a wildcard request, we want to respond with all resources
	switch {
	case state.IsWildcard():
		if len(state.GetResourceVersions()) == 0 {
			filtered = make([]types.Resource, 0, len(resources.resourceMap))
		}
		nextVersionMap = make(map[string]string, len(resources.resourceMap))
		for name, r := range resources.resourceMap {
			// Since we've already precomputed the version hashes of the new snapshot,
			// we can just set it here to be used for comparison later
			version := resources.versionMap[name]
			nextVersionMap[name] = version
			prevVersion, found := state.GetResourceVersions()[name]
			if !found || (prevVersion != version) {
				filtered = append(filtered, r)
			}
		}

		// Compute resources for removal
		// The resource version can be set to "" here to trigger a removal even if never returned before
		for name := range state.GetResourceVersions() {
			if _, ok := resources.resourceMap[name]; !ok {
				toRemove = append(toRemove, name)
			}
		}
	default:
		nextVersionMap = make(map[string]string, len(state.GetSubscribedResourceNames()))
		// state.GetResourceVersions() may include resources no longer subscribed
		// In the current code this gets silently cleaned when updating the version map
		for name := range state.GetSubscribedResourceNames() {
			prevVersion, found := state.GetResourceVersions()[name]
			if r, ok := resources.resourceMap[name]; ok {
				nextVersion := resources.versionMap[name]
				if prevVersion != nextVersion {
					filtered = append(filtered, r)
				}
				nextVersionMap[name] = nextVersion
			} else if found {
				toRemove = append(toRemove, name)
			}
		}
	}

	return &RawDeltaResponse{
		DeltaRequest:      req,
		Resources:         filtered,
		RemovedResources:  toRemove,
		NextVersionMap:    nextVersionMap,
		SystemVersionInfo: resources.systemVersion,
		Ctx:               ctx,
	}
}
