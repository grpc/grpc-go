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
	// Listener holds the listener configuration. It is garunteed to be
	// non-nil.
	Listener *ListenerUpdate

	// RouteConfig is the route configuration. It will be populated even if
	// RouteConfig is inlined into the Listener resource. It is garunteed to be
	// non-nil.
	RouteConfig *RouteConfigUpdate

	// VirtualHost selected from the route configuration whose domain field
	// offers the best match against the provided dataplane authority. It is
	// garunteed to be non-nil.
	VirtualHost *VirtualHost

	// Clusters is a map from cluster name to its configuration.
	Clusters map[string]*ClusterResult
}

// ClusterResult contains a cluster's configuration when we receive a
// valid resource from the management server. It contains an error when:
//   - we receive an invalid resource from the management server and
//     we did not already have a valid resource or
//   - the cluster resource does not exist on the management server
type ClusterResult struct {
	Config ClusterConfig
	Err    error
}

// ClusterConfig contains configuration for a single cluster.
type ClusterConfig struct {
	// Cluster configuration for the cluster. This field is always set to a non-zero value
	Cluster *ClusterUpdate
	// Endpoint configuration for the cluster. This field is only set if the
	// cluster is a leaf cluster.
	EndpointConfig *EndpointConfig
	// AggregateConfig is the set of leaf clusters for the cluster. This field
	// is only set if the cluster is of type AGGREGATE.
	AggregateConfig *AggregateConfig
}

// AggregateConfig holds the configuration for an aggregate cluster.
type AggregateConfig struct {
	// LeafClusters specifies the names of the leaf clusters for the cluster.
	LeafClusters []string
}

// EndpointConfig contains configuration corresponding to the endpoints in a
// cluster. Only one of EDSUpdate or DNSEndpoints will be populated based on the
// cluster type.
type EndpointConfig struct {
	// Endpoint configurartion for the EDS type cluster.
	EDSUpdate *EndpointsUpdate
	// Endpoint configuration for the LOGICAL_DNS type cluster.
	DNSEndpoints DNSUpdate
	// Stores error encountered while obtaining endpoints data for the cluster.
	ResolutionNote error
}

// DNSUpdate represents the result of a DNS resolution, containing a
// list of discovered endpoints. This is only populated for the
// LOGICAL_DNS cluster type.
type DNSUpdate struct {
	// Endpoints is the complete list of endpoints returned by the
	// DNS resolver.
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

// XDSConfigFromResolverState returns XDSConfig stored in attribute in resolver
// state.
func XDSConfigFromResolverState(state resolver.State) *XDSConfig {
	state.Attributes.Value(xdsConfigkey{})
	if v := state.Attributes.Value(xdsConfigkey{}); v != nil {
		return v.(*XDSConfig)
	}
	return nil
}
