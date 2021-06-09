/*
 *
 * Copyright 2021 gRPC authors.
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

package clusterresolver

import (
	"encoding/json"

	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/clusterresolver/balancerconfig"
)

// LBConfig is the config for cluster resolver balancer.
type LBConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`
	// DiscoveryMechanisms is an ordered list of discovery mechanisms.
	//
	// Must have at least one element. Results from each discovery mechanism are
	// concatenated together in successive priorities.
	DiscoveryMechanisms []balancerconfig.DiscoveryMechanism `json:"discoveryMechanisms,omitempty"`

	// LocalityPickingPolicy is policy for locality picking.
	//
	// This policy's config is expected to be in the format used by the
	// weighted_target policy.  Note that the config should include an empty
	// value for the "targets" field; that empty value will be replaced by one
	// that is dynamically generated based on the EDS data. Optional; defaults
	// to "weighted_target".
	LocalityPickingPolicy *internalserviceconfig.BalancerConfig `json:"localityPickingPolicy,omitempty"`

	// EndpointPickingPolicy is policy for endpoint picking.
	//
	// This will be configured as the policy for each child in the
	// locality-policy's config. Optional; defaults to "round_robin".
	EndpointPickingPolicy *internalserviceconfig.BalancerConfig `json:"endpointPickingPolicy,omitempty"`

	// FIXME: read and warn if endpoint is not roundrobin or locality is not
	// weightedtarget.
}

func parseConfig(c json.RawMessage) (*LBConfig, error) {
	var cfg LBConfig
	if err := json.Unmarshal(c, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
