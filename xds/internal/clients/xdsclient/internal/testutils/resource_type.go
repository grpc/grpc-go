/*
 *
 * Copyright 2025 gRPC authors.
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

// Package testutils implements the xDS data model layer.
//
// Provides resource-type specific functionality to unmarshal xDS protos into
// internal data structures for end2end testing of xDS client.
package testutils

import (
	"google.golang.org/grpc/xds/internal/clients/xdsclient/internal/xdsresource"
)

// ResourceTypeMapForTesting maps TypeUrl to corresponding ResourceType.
var ResourceTypeMapForTesting map[string]any

func init() {
	ResourceTypeMapForTesting = make(map[string]any)
	ResourceTypeMapForTesting[xdsresource.V3ListenerURL] = listenerType
}
