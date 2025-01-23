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

// Package clients contains implementations of the xDS and LRS clients, to be
// used by applications to communicate with xDS management servers.
//
// The xDS client enable users to create client instance with in-memory
// configurations and register watches for named resource that can be received
// on ADS stream.
//
// The LRS client allows to report load through LRS Stream.
//
// # Experimental
//
// Notice: This package is EXPERIMENTAL and may be changed or removed
// in a later release. See [README](https://github.com/grpc/grpc-go/tree/master/xds/clients/README.md)
package clients

import (
	"fmt"
	"slices"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/google/go-cmp/cmp"
)

// ServerConfig contains the configuration to connect to an xDS management
// server.
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
	// The xDS and LRS clients itself do not interpret the contents of this
	// field. It is the responsibility of the user's custom [TransportBuilder]
	// and/or ResourceType implementations to handle and interpret these
	// extensions.
	//
	// For example, a custom [TransportBuilder] might use this field to
	// configure a specific security credentials.
	Extensions any
}

// Equal returns true if sc and other are considered equal.
func (sc *ServerConfig) Equal(other *ServerConfig) bool {
	switch {
	case sc == nil && other == nil:
		return true
	case (sc != nil) != (other != nil):
		return false
	case sc.ServerURI != other.ServerURI:
		return false
	case sc.IgnoreResourceDeletion != other.IgnoreResourceDeletion:
		return false
	case !cmp.Equal(sc.Extensions, other.Extensions):
		return false
	}
	return true
}

// String returns a string representation of the [ServerConfig].
//
// NOTICE: This interface is intended mainly for logging/testing purposes and
// that the user must not assume anything about the stability of the output
// returned from this method.
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
	// The xDS and LRS clients itself do not interpret the contents of this
	// field. It is the responsibility of the user's implementations to handle
	// and interpret these extensions.
	//
	// For example, a custom name resolver might use this field for the name of
	// listener resource to subscribe to.
	Extensions any
}

// Node represents the node of the xDS client for management servers to
// identify the application making the request.
type Node struct {
	// ID is a string identifier of the node.
	ID string
	// Cluster is the name of the cluster the node belongs to.
	Cluster string
	// Locality is the location of the node including region, zone, sub-zone.
	Locality Locality
	// Metadata is any arbitrary values associated with the node to provide
	// additional context.
	Metadata any
	// UserAgentName is the user agent name. It is typically set to a hardcoded
	// constant such as grpc-go.
	UserAgentName string
	// UserAgentVersion is the user agent version. It is typically set to the
	// version of the library.
	UserAgentVersion string
	// ClientFeatures is a list of features supported by this client. These are
	// typically hardcoded within the xDS client, but may be overridden for
	// testing purposes.
	ClientFeatures []string
}

// ToProto converts the [Node] object to its protobuf representation.
func (n Node) ToProto() *v3corepb.Node {
	return &v3corepb.Node{
		Id:      n.ID,
		Cluster: n.Cluster,
		Locality: func() *v3corepb.Locality {
			if n.Locality.IsEmpty() {
				return nil
			}
			return &v3corepb.Locality{
				Region:  n.Locality.Region,
				Zone:    n.Locality.Zone,
				SubZone: n.Locality.SubZone,
			}
		}(),
		Metadata:             proto.Clone(n.Metadata.(*structpb.Struct)).(*structpb.Struct),
		UserAgentName:        n.UserAgentName,
		UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: n.UserAgentVersion},
		ClientFeatures:       slices.Clone(n.ClientFeatures),
	}
}

// Locality represents the location of the xDS client node.
type Locality struct {
	// Region is the region of the xDS client node.
	Region string
	// Zone is the area within a region.
	Zone string
	// SubZone is the further subdivision within a sub-zone.
	SubZone string
}

// IsEmpty reports whether l is considered empty.
func (l Locality) IsEmpty() bool {
	return l.Equal(Locality{})
}

// Equal returns true if l and other are considered equal.
func (l Locality) Equal(other Locality) bool {
	return l.Region == other.Region && l.Zone == other.Zone && l.SubZone == other.SubZone
}
