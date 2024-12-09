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

package clients

import (
	"fmt"
	"slices"
	"strings"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// ServerConfig contains the configuration to connect to a server.
type ServerConfig struct {
	ServerURI              string
	IgnoreResourceDeletion bool

	Extensions any
}

// Authority provides the functionality required to communicate with
// management servers corresponding to an authority.
type Authority struct {
	XDSServers []ServerConfig

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

// Locality is the representation of the locality field within a node.
type Locality struct {
	Region  string
	Zone    string
	SubZone string
}

// Equal reports whether sc and other are considered equal.
func (sc *ServerConfig) Equal(other *ServerConfig) bool {
	switch {
	case sc == nil && other == nil:
		return true
	case sc.ServerURI != other.ServerURI:
		return false
	}
	return true
}

// String returns the string representation of the ServerConfig.
func (sc *ServerConfig) String() string {
	return strings.Join([]string{sc.ServerURI, fmt.Sprintf("%v", sc.IgnoreResourceDeletion)}, "-")
}

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

func (l Locality) IsEmpty() bool {
	return l.Equal(Locality{})
}

func (l Locality) Equal(other Locality) bool {
	return l.Region == other.Region && l.Zone == other.Zone && l.SubZone == other.SubZone
}
