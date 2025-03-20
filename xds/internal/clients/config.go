/*
 *
 * Copyright 2024 gRPC authors.
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

// Package clients provides implementations of the clients to interact with
// xDS and LRS servers.
//
// # xDS Client
//
// The xDS client allows applications to:
//   - Create client instances with in-memory configurations.
//   - Register watches for named resources.
//   - Receive resources via the ADS (Aggregated Discovery Service) stream.
//
// This enables applications to dynamically discover and configure resources
// such as listeners, routes, clusters, and endpoints from an xDS management
// server.
//
// # LRS Client
//
// The LRS (Load Reporting Service) client allows applications to report load
// data to an LRS server via the LRS stream. This data can be used for
// monitoring, traffic management, and other purposes.
//
// # Experimental
//
// NOTICE: This package is EXPERIMENTAL and may be changed or removed
// in a later release.
package clients

// ServerIdentifier holds identifying information for connecting to an xDS
// management or LRS server.
type ServerIdentifier struct {
	// ServerURI is the target URI of the server.
	ServerURI string

	// Extensions can be populated with arbitrary data to be passed to the
	// TransportBuilder and/or xDS Client's ResourceType implementations.
	// This field can be used to provide additional configuration or context
	// specific to the user's needs.
	//
	// The xDS and LRS clients do not interpret the contents of this field.
	// It is the responsibility of the user's custom TransportBuilder and/or
	// ResourceType implementations to handle and interpret these extensions.
	//
	// For example, a custom TransportBuilder might use this field to
	// configure a specific security credentials.
	//
	// If present, Extensions must implement the `Equal(other any) bool`
	// method. Two ServerIdentifiers with Extensions will be considered unequal
	// if either Extensions does not implement this method.
	Extensions any
}

// Node represents the identity of the xDS client, allowing xDS and LRS servers
// to identify the source of xDS requests.
type Node struct {
	// ID is a string identifier of the application.
	ID string
	// Cluster is the name of the cluster the application belongs to.
	Cluster string
	// Locality is the location of the application including region, zone,
	// sub-zone.
	Locality Locality
	// Metadata provides additional context about the application by associating
	// arbitrary key-value pairs with it.
	Metadata any
	// UserAgentName is the user agent name of application.
	UserAgentName string
	// UserAgentVersion is the user agent version of application.
	UserAgentVersion string
}

// Locality represents the location of the xDS client application.
type Locality struct {
	// Region is the region of the xDS client application.
	Region string
	// Zone is the area within a region.
	Zone string
	// SubZone is the further subdivision within a zone.
	SubZone string
}
