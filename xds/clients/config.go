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

// Package clients provides the functionality to create xDS and LRS client
// using possible options for resource decoding and transport.
//
// # Experimental
//
// Notice: This package is EXPERIMENTAL and may be changed or removed
// in a later release.
package clients

import (
	"fmt"
	"slices"
	"strings"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// ServerConfig contains the configuration to connect to an xDS management
// server.
type ServerConfig struct {
	// ServerURI is the server url of the xDS management server.
	ServerURI string

	// IgnoreResourceDeletion is a server feature which if set to true,
	// indicates that resource deletion errors can be ignored and cached
	// resource data can be used.
	//
	// This will be removed in future once we implement gRFC A88
	// and two new fields `FailOnDataErrors` and
	// `ResourceTimerIsTransientError` will be introduced.
	IgnoreResourceDeletion bool

	// Extensions for `ServerConfig` can be populated with arbitrary data to be
	// passed to the `TransportBuilder` and/or xDS Client's `ResourceType`
	// implementations. This field can be used to provide additional
	// configuration or context specific to the user's needs.
	//
	// The xDS and LRS clients itself does not interpret the contents of this
	// field. It is the responsibility of the user's custom `TransportBuilder`
	// and/or `ResourceType` implementations to handle and interpret these
	// extensions.
	//
	// For example, a custom TransportBuilder might use this field to configure
	// a specific security credentials.
	Extensions any
}

// Equal reports whether sc and other `ServerConfig` objects are considered
// equal.
func (sc *ServerConfig) Equal(other *ServerConfig) bool {
	switch {
	case sc == nil && other == nil:
		return true
	case sc.ServerURI != other.ServerURI:
		return false
	}
	return true
}

// String returns the string representation of the `ServerConfig`.
func (sc *ServerConfig) String() string {
	return strings.Join([]string{sc.ServerURI, fmt.Sprintf("%v", sc.IgnoreResourceDeletion)}, "-")
}

// Authority contains configuration for an xDS control plane authority.
type Authority struct {
	// XDSServers contains the list of server configurations for this authority.
	XDSServers []ServerConfig

	// Extensions for `Authority` can be populated with arbitrary data to be
	// passed to the xDS Client's user specific implementations. This field
	// can be used to provide additional configuration or context specific to
	// the user's needs.
	//
	// The xDS and LRS clients itself does not interpret the contents of this
	// field. It is the responsibility of the user's implementations to handle
	// and interpret these extensions.
	//
	// For example, a custom name resolver might use this field for the name of
	// listener resource to subscribe to.
	Extensions any
}

// Node is the representation of the client node of xDS Client.
type Node struct {
	ID               string
	Cluster          string
	Locality         Locality
	Metadata         any
	UserAgentName    string
	UserAgentVersion string

	clientFeatures []string
}

// ToProto converts the `Node` object to its protobuf representation.
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
		ClientFeatures:       slices.Clone(n.clientFeatures),
	}
}

// Locality is the representation of the locality field within `Node`.
type Locality struct {
	Region  string
	Zone    string
	SubZone string
}

// IsEmpty reports whether l is considered empty.
func (l Locality) IsEmpty() bool {
	return l.Equal(Locality{})
}

// Equal reports whether l and other `Locality` objects are considered equal.
func (l Locality) Equal(other Locality) bool {
	return l.Region == other.Region && l.Zone == other.Zone && l.SubZone == other.SubZone
}
