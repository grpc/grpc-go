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

// Config is used to configure an xDS client. After one has been passed to an
// xDS function, it must not be modified. A Config may be used; the xDS package
// will also not modify it.
type Config struct {
	// Servers specifies a list of xDS management servers to connect to. The
	// order of the servers in this list reflects the order of preference of
	// the data returned by those servers. xDS client use the first
	// available server from the list.
	//
	// See gRFC A71 for more details on fallback behavior when the primary
	// xDS server is unavailable.
	//
	// gRFC A71: https://github.com/grpc/proposal/blob/master/A71-xds-fallback.md
	Servers []clients.ServerConfig

	// Authorities map is used to define different authorities, in a federated
	// setup, each with its own set of xDS management servers.
	Authorities map[string]Authority

	// Node is the identity of the xDS client connecting to the xDS
	// management server.
	Node clients.Node

	// TransportBuilder is used to connect to the xDS management server.
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
// See: https://github.com/grpc/grpc/blob/master/doc/grpc_xds_bootstrap_format.md
type Authority struct {
	// XDSServers contains the list of server configurations for this authority.
	// The order of the servers in this list reflects the order of preference
	// of the data returned by those servers. xDS client use the first
	// available server from the list.
	//
	// See gRFC A71 for more details on fallback behavior when the primary
	// xDS server is unavailable.
	//
	// gRFC A71: https://github.com/grpc/proposal/blob/master/A71-xds-fallback.md
	XDSServers []clients.ServerConfig

	// Extensions can be populated with arbitrary data to be passed to the xDS
	// Client's user specific implementations. This field can be used to
	// provide additional configuration or context specific to the user's
	// needs.
	//
	// The xDS and LRS clients do not interpret the contents of this field. It
	// is the responsibility of the user's implementations to handle and
	// interpret these extensions.
	Extensions any
}
