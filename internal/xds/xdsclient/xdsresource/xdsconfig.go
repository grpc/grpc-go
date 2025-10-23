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

package xdsresource

import "google.golang.org/grpc/resolver"

// XDSConfig holds the complete gRPC client-side xDS configuration containing
// all necessary resources.
type XDSConfig struct {
	// Listener holds the listener configuration.
	Listener *ListenerUpdate

	// RouteConfig is the route configuration. It will be populated even if
	// RouteConfig is inlined into the Listener resource.
	RouteConfig RouteConfigUpdate

	// VirtualHost selected from the route configuration whose domain field
	// offers the best match against the provided dataplane authority.
	VirtualHost *VirtualHost

	// Clusters is a map from cluster name to its configuration.
	Clusters map[string]*ClusterResult
}

// ClusterResult contains either a cluster's configuration or an error.
type ClusterResult struct {
	Config ClusterConfig
	Err    error
}

// ClusterConfig contains configuration for a single cluster.
type ClusterConfig struct {
	Cluster         ClusterUpdate   // Cluster configuration. Always present.
	EndpointConfig  EndpointConfig  // Endpoint configuration for leaf clusters.
	AggregateConfig AggregateConfig // List of children for aggregate clusters.
}

// AggregateConfig contains a list of leaf cluster names.
type AggregateConfig struct {
	LeafClusters []string
}

// EndpointConfig contains configuration corresponding to the endpoints in a
// cluster. Only one of EDSUpdate or DNSEndpoints will be populated based on the
// cluster type.
type EndpointConfig struct {
	EDSUpdate      EndpointsUpdate // Configuration for EDS clusters.
	DNSEndpoints   DNSUpdate       // Configuration for LOGICAL_DNS clusters.
	ResolutionNote error           // Error obtaining endpoints data.
}

// DNSUpdate contains the update from DNS resolver.
type DNSUpdate struct {
	Endpoints []resolver.Endpoint
}

// xdsConfigkey is the type used as the key to store XDSConfig in
// the Attributes field of resolver.states.
type xdsConfigkey struct{}

// SetXDSConfig returns a copy of state in which the Attributes field
// is updated with the XDSConfig.
func SetXDSConfig(state resolver.State, config *XDSConfig) resolver.State {
	state.Attributes = state.Attributes.WithValue(xdsConfigkey{}, config)
	return state
}

// XDSConfigFromResolverState returns XDßSConfig stored in attribute in resolver
// state.
func XDSConfigFromResolverState(state resolver.State) *XDSConfig {
	state.Attributes.Value(xdsConfigkey{})
	if v := state.Attributes.Value(xdsConfigkey{}); v != nil {
		return v.(*XDSConfig)
	}
	return nil
}
