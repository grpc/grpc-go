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

// Config is used to configure an xDS client. After one has been passed to the
// xDS client's New function, no part of it may be modified. A Config may be
// reused; the xdsclient package will also not modify it.
type Config struct {
	// Servers specifies a list of xDS management servers to connect to. The
	// order of the servers in this list reflects the order of preference of
	// the data returned by those servers. The xDS client uses the first
	// available server from the list.
	//
	// See gRFC A71 for more details on fallback behavior when the primary
	// xDS server is unavailable.
	//
	// gRFC A71: https://github.com/grpc/proposal/blob/master/A71-xds-fallback.md
	Servers []clients.ServerConfig

	// Authorities defines the configuration for each xDS authority.  Federated resources
	// will be fetched from the servers specified by the corresponding Authority.
	Authorities map[string]Authority

	// Node is the identity of the xDS client connecting to the xDS
	// management server.
	Node clients.Node

	// TransportBuilder is used to create connections to xDS management servers.
	TransportBuilder clients.TransportBuilder

	// ResourceTypes is a map from resource type URLs to resource type
	// implementations. Each resource type URL uniquely identifies a specific
	// kind of xDS resource, and the corresponding resource type implementation
	// provides logic for parsing, validating, and processing resources of that
	// type.
	ResourceTypes map[string]ResourceType
}

// Authority contains configuration for an xDS control plane authority.
//
// See: https://www.envoyproxy.io/docs/envoy/latest/xds/core/v3/resource_locator.proto#xds-core-v3-resourcelocator
type Authority struct {
	// XDSServers contains the list of server configurations for this authority.
	//
	// See Config.Servers for more details.
	XDSServers []clients.ServerConfig

	// Extensions can be populated with arbitrary authority-specific data to be
	// passed from the xDS client configuration down to the user defined
	// resource type implementations. This allows the user to provide
	// authority-specific context or configuration to their resource
	// processing logic.
	//
	// The xDS client does not interpret the contents of this field. It is the
	// responsibility of the user's resource type implementations to handle and
	// interpret these extensions.
	Extensions any
}
