/*
 *
 * Copyright 2021 gRPC authors.
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

// Package testutils provides utility types, for use in xds tests.
package testutils

import (
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
)

// BuildResourceName returns the resource name in the format of an xdstp://
// resource.
func BuildResourceName(typ xdsresource.ResourceType, auth, id string, ctxParams map[string]string) string {
	var typS string
	switch typ {
	case xdsresource.ListenerResource:
		typS = version.V3ListenerType
	case xdsresource.RouteConfigResource:
		typS = version.V3RouteConfigType
	case xdsresource.ClusterResource:
		typS = version.V3ClusterType
	case xdsresource.EndpointsResource:
		typS = version.V3EndpointsType
	}
	return (&xdsresource.Name{
		Scheme:        "xdstp",
		Authority:     auth,
		Type:          typS,
		ID:            id,
		ContextParams: ctxParams,
	}).String()
}
