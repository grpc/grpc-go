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

// XDSConfig holds the complete and resolved xDS resource configuration
// including LDS, RDS, CDS and endpoints.
type XDSConfig struct {
	// Listener is the listener resource update
	Listener ListenerUpdate

	// RouteConfig is the route configuration resource update. It will be
	// populated even if RouteConfig is inlined into the Listener resource.
	RouteConfig RouteConfigUpdate

	// VirtualHost is the virtual host from the route configuration matched with
	// dataplane authority .
	VirtualHost *VirtualHost

	// Clusters maps the cluster name with the ClusterResult which will have
	// either the cluster configuration or error. It will have an error status
	// if either
	//
	// (a) there was an error and we did not already have a valid resource or
	//
	// (b) the resource does not exist.
	Clusters map[string]*ClusterResult
}

// ClusterResult contains either a cluster's configuration or an error.
type ClusterResult struct {
	Config ClusterConfig
	Err    error
}

// ClusterConfig contains cluster configuration for a single cluster.
type ClusterConfig struct {
	Cluster         ClusterUpdate   // Cluster configuration. Always present.
	EndpointConfig  EndpointConfig  // Endpoint configuration for leaf clusters which will of type EDS or DNS.
	AggregateConfig AggregateConfig // List of children for aggregate clusters.
}

// AggregateConfig contains a list of leaf cluster names.
type AggregateConfig struct {
	LeafClusters []string
}

// EndpointConfig contains resolved endpoints for a leaf cluster either from DNS
// or EDS watchers and error.
type EndpointConfig struct {
	EDSUpdate      EndpointsUpdate // Resolved endpoints for EDS clusters.
	DNSEndpoints   DNSUpdate       // Resolved endpoints for LOGICAL_DNS clusters.
	ResolutionNote error           // Error obtaining endpoints data
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

// XDSConfigFromResolverState returns XDSConfig stored in attribute in resolver state.
func XDSConfigFromResolverState(state resolver.State) *XDSConfig {
	state.Attributes.Value(xdsConfigkey{})
	if v := state.Attributes.Value(xdsConfigkey{}); v != nil {
		return v.(*XDSConfig)
	}
	return nil
}
