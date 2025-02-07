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

package xdsclient

import (
	"google.golang.org/grpc/xds/internal/clients"
)

// A Config structure is used to configure an xDS client. After one has been
// passed to an xDS function it must not be modified. A Config may be used;
// the xDS package will also not modify it.
type Config struct {
	// Servers specifies a list of xDS management servers to connect to,
	// including fallbacks. xDS client use the first available server from the
	// list. To ensure high availability, list the most reliable server first.
	Servers []clients.ServerConfig

	// Authorities map is used to define different authorities, in a federated
	// setup, each with its own set of xDS management servers.
	Authorities map[string]clients.Authority

	// Node is the identity of the xDS client connecting to the xDS
	// management server.
	Node clients.Node

	// TransportBuilder is used to connect to the management server.
	TransportBuilder clients.TransportBuilder

	// ResourceTypes is a map from resource type URLs to resource type
	// implementations. Each resource type URL uniquely identifies a specific
	// kind of xDS resource, and the corresponding resource type implementation
	// provides logic for parsing, validating, and processing resources of that
	// type.
	ResourceTypes map[string]ResourceType
}
