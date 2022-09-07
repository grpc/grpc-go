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

package controller

import (
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

// AddWatch adds a watch for an xDS resource given its type and name.
func (t *Controller) AddWatch(rType xdsresource.ResourceType, resourceName string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var current map[string]bool
	current, ok := t.watchMap[rType]
	if !ok {
		current = make(map[string]bool)
		t.watchMap[rType] = current
	}
	current[resourceName] = true

	targets := mapToSlice(current)
	t.transport.SendRequest(rType, targets)
}

// RemoveWatch cancels an already registered watch for an xDS resource
// given its type and name.
func (t *Controller) RemoveWatch(rType xdsresource.ResourceType, resourceName string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	current, ok := t.watchMap[rType]
	if !ok {
		return
	}
	delete(current, resourceName)
	if len(current) == 0 {
		delete(t.watchMap, rType)
	}

	targets := mapToSlice(current)
	t.transport.SendRequest(rType, targets)
}

func mapToSlice(m map[string]bool) []string {
	ret := make([]string, 0, len(m))
	for i := range m {
		ret = append(ret, i)
	}
	return ret
}
