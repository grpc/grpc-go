/*
 *
 * Copyright 2020 gRPC authors.
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

// Package common defines types and functionality shared across xds API and
// resource versions.
package common

import "google.golang.org/grpc/xds/internal"

// ListenerUpdate contains information received in an LDS response, which is of
// interest to the registered LDS watcher.
type ListenerUpdate struct {
	// RouteConfigName is the route configuration name corresponding to the
	// target which is being watched through LDS.
	RouteConfigName string
}

// RouteConfigUpdate contains information received in an RDS response, which is
// of interest to the registered RDS watcher.
type RouteConfigUpdate struct {
	// WeightedCluster contains a map of cluster names and their weights,
	// corresponding to the route configuration watched through RDS.
	WeightedCluster map[string]uint32
}

// ServiceUpdate contains information received from LDS and RDS responses,
// which is of interest to the registered service watcher.
type ServiceUpdate struct {
	// WeightedCluster contains a map of cluster names and their weights,
	// corresponding to the route configuration watched through RDS.
	WeightedCluster map[string]uint32
}

// ClusterUpdate contains information from a received CDS response, which is of
// interest to the registered CDS watcher.
type ClusterUpdate struct {
	// ServiceName is the service name corresponding to the clusterName which
	// is being watched for through CDS.
	ServiceName string
	// EnableLRS indicates whether or not load should be reported through LRS.
	EnableLRS bool
}

// OverloadDropConfig contains the config to drop overloads.
type OverloadDropConfig struct {
	Category    string
	Numerator   uint32
	Denominator uint32
}

// EndpointHealthStatus represents the health status of an endpoint.
type EndpointHealthStatus int32

const (
	// EndpointHealthStatusUnknown represents HealthStatus UNKNOWN.
	EndpointHealthStatusUnknown EndpointHealthStatus = iota
	// EndpointHealthStatusHealthy represents HealthStatus HEALTHY.
	EndpointHealthStatusHealthy
	// EndpointHealthStatusUnhealthy represents HealthStatus UNHEALTHY.
	EndpointHealthStatusUnhealthy
	// EndpointHealthStatusDraining represents HealthStatus DRAINING.
	EndpointHealthStatusDraining
	// EndpointHealthStatusTimeout represents HealthStatus TIMEOUT.
	EndpointHealthStatusTimeout
	// EndpointHealthStatusDegraded represents HealthStatus DEGRADED.
	EndpointHealthStatusDegraded
)

// Endpoint contains information of an endpoint.
type Endpoint struct {
	Address      string
	HealthStatus EndpointHealthStatus
	Weight       uint32
}

// Locality contains information of a locality.
type Locality struct {
	Endpoints []Endpoint
	ID        internal.LocalityID
	Priority  uint32
	Weight    uint32
}

// EndpointsUpdate contains an EDS update.
type EndpointsUpdate struct {
	Drops      []OverloadDropConfig
	Localities []Locality
}

// UpdateHandler receives and processes (by taking appropriate actions) xDS
// resource updates from an APIClient for a specific version. This is
// implemented by the top level Client.
type UpdateHandler interface {
	NewListeners(map[string]ListenerUpdate)
	NewRouteConfigs(map[string]RouteConfigUpdate)
	NewClusters(map[string]ClusterUpdate)
	NewEndpoints(map[string]EndpointsUpdate)
}
