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
 */

package xdsclient

import (
	"google.golang.org/grpc/xds/internal/clients/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
)

func supportedResourceTypes() map[string]xdsclient.ResourceType {
	return map[string]xdsclient.ResourceType{
		version.V3ListenerURL: {
			TypeURL:                    version.V3ListenerURL,
			TypeName:                   xdsresource.ListenerResourceTypeName,
			AllResourcesRequiredInSotW: true,
			Decoder:                    xdsresource.ListenerResourceTypeDecoder,
		},
		version.V3RouteConfigURL: {
			TypeURL:                    version.V3RouteConfigURL,
			TypeName:                   xdsresource.RouteConfigTypeName,
			AllResourcesRequiredInSotW: false,
			Decoder:                    xdsresource.RouteConfigResourceTypeDecoder,
		},
		version.V3ClusterURL: {
			TypeURL:                    version.V3ClusterURL,
			TypeName:                   xdsresource.ClusterResourceTypeName,
			AllResourcesRequiredInSotW: true,
			Decoder:                    xdsresource.ClusterResourceTypeDecoder,
		},
		version.V3EndpointsURL: {
			TypeURL:                    version.V3EndpointsURL,
			TypeName:                   xdsresource.EndpointsResourceTypeName,
			AllResourcesRequiredInSotW: false,
			Decoder:                    xdsresource.EndpointsResourceTypeDecoder,
		},
	}
}
