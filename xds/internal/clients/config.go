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

// Package clients provides implementations of the xDS and LRS clients,
// enabling applications to communicate with xDS management servers and report
// load.
//
// xDS Client
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
//
// See [README](https://github.com/grpc/grpc-go/tree/master/xds/clients/README.md).
package clients

import (
	"fmt"
	"slices"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

// ServerConfig holds settings for connecting to an xDS management server.
type ServerConfig struct {
	// ServerURI is the target URI of the xDS management server.
	ServerURI string

	// IgnoreResourceDeletion is a server feature which if set to true,
	// indicates that resource deletion errors can be ignored and cached
	// resource data can be used.
	//
	// This will be removed in the future once we implement gRFC A88
	// and two new fields FailOnDataErrors and
	// ResourceTimerIsTransientError will be introduced.
	IgnoreResourceDeletion bool

	// Extensions can be populated with arbitrary data to be passed to the
	// [TransportBuilder] and/or xDS Client's ResourceType implementations.
	// This field can be used to provide additional configuration or context
	// specific to the user's needs.
	//
	// The xDS and LRS clients do not interpret the contents of this field.
	// It is the responsibility of the user's custom [TransportBuilder] and/or
	// ResourceType implementations to handle and interpret these extensions.
	//
	// For example, a custom [TransportBuilder] might use this field to
	// configure a specific security credentials.
	//
	// Note: For custom types used in Extensions, ensure an Equal(any) bool
	// method is implemented for equality checks on ServerConfig.
	Extensions any
}

// equal returns true if sc and other are considered equal.
func (sc *ServerConfig) equal(other *ServerConfig) bool {
	switch {
	case sc == nil && other == nil:
		return true
	case (sc != nil) != (other != nil):
		return false
	case sc.ServerURI != other.ServerURI:
		return false
	case sc.IgnoreResourceDeletion != other.IgnoreResourceDeletion:
		return false
	}
	if sc.Extensions == nil && other.Extensions == nil {
		return true
	}
	if ex, ok := sc.Extensions.(interface{ Equal(any) bool }); ok && ex.Equal(other.Extensions) {
		return true
	}
	return false
}

// String returns a string representation of the [ServerConfig].
//
// WARNING: This method is primarily intended for logging and testing
// purposes. The output returned by this method is not guaranteed to be stable
// and may change at any time. Do not rely on it for production use.
func (sc *ServerConfig) String() string {
	return strings.Join([]string{sc.ServerURI, fmt.Sprintf("%v", sc.IgnoreResourceDeletion)}, "-")
}

// Authority contains configuration for an xDS control plane authority.
type Authority struct {
	// XDSServers contains the list of server configurations for this authority.
	XDSServers []ServerConfig

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

// Node represents the identity of the xDS client, allowing
// management servers to identify the source of xDS requests.
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
	// ClientFeatures is a list of xDS features supported by this client.
	// These features are set within the xDS client, but may be overridden only
	// for testing purposes.
	clientFeatures []string
}

// toProto converts an instance of [Node] to its protobuf representation.
func (n Node) toProto() *v3corepb.Node {
	return &v3corepb.Node{
		Id:      n.ID,
		Cluster: n.Cluster,
		Locality: func() *v3corepb.Locality {
			if n.Locality.isEmpty() {
				return nil
			}
			return &v3corepb.Locality{
				Region:  n.Locality.Region,
				Zone:    n.Locality.Zone,
				SubZone: n.Locality.SubZone,
			}
		}(),
		Metadata: func() *structpb.Struct {
			if n.Metadata == nil {
				return nil
			}
			if md, ok := n.Metadata.(*structpb.Struct); ok {
				return proto.Clone(md).(*structpb.Struct)
			}
			return nil
		}(),
		UserAgentName:        n.UserAgentName,
		UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: n.UserAgentVersion},
		ClientFeatures:       slices.Clone(n.clientFeatures),
	}
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

// isEmpty reports whether l is considered empty.
func (l Locality) isEmpty() bool {
	return l.equal(Locality{})
}

// equal returns true if l and other are considered equal.
func (l Locality) equal(other Locality) bool {
	return l.Region == other.Region && l.Zone == other.Zone && l.SubZone == other.SubZone
}
