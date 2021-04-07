/*
 *
 * Copyright 2019 gRPC authors.
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

package edsbalancer

import (
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/serviceconfig"
)

type ClusterType int

const (
	// ClusterTypeEDS represents the EDS cluster type, which will delegate endpoint
	// discovery to the management server.
	ClusterTypeEDS ClusterType = iota
	// ClusterTypeLogicalDNS represents the Logical DNS cluster type, which essentially
	// maps to the gRPC behavior of using the DNS resolver with pick_first LB policy.
	ClusterTypeLogicalDNS
)
// TOMORROW: HANDLE CDS BALANCER CHANGES (pull functionality out, put new functionality in
// including constructing this new struct)
type DiscoveryMechanism struct {
	// Cluster name.
	cluster string
	// LRS server to send load reports to.  If not present, load reporting
	// will be disabled.  If set to the empty string, load reporting will
	// be sent to the same server that we obtained CDS data from.
	LrsLoadReportingServerName *string

	// Maximum number of outstanding requests can be made to the upstream cluster. Default
	// is 1024.
	MaxConcurrentRequests *uint32

	ClusterType ClusterType
	// Name to use in EDS query.  If not present, defaults to the server
	// name from the target URI. For type EDS only.
	EDSServiceName string
}

type XDSClusterResolverConfig struct {
	// Ordered list of discovery mechanisms. Must have at least one element.
	// Results from each discovery mechanism are concatenated together in
	// successive priorities.
	DiscoveryMechanisms []DiscoveryMechanism

	// Locality picking policy. This config is expected to be in the format used
	// by the weighted target policy. Note that the config should include an empty
	// value for the "targets" field; that empty value will be replaced by one that is
	// dynamically generated based on the EDS data. Optional, defaults to "weighted
	// target".
	LocalityPickingPolicy []*loadBalancingConfig
	// Endpoint picking policy. This will be configured as the policy for each child in
	// the locality-policy's config. Optional; defaults to "round_robin".
	EndpointPickingPolicy []*loadBalancingConfig
}

// EDSConfig represents the loadBalancingConfig section of the service config
// for EDS balancers.
type EDSConfig struct {
	serviceconfig.LoadBalancingConfig
	// ChildPolicy represents the load balancing config for the child
	// policy.
	ChildPolicy *loadBalancingConfig
	// FallBackPolicy represents the load balancing config for the
	// fallback.
	FallBackPolicy *loadBalancingConfig
	// Name to use in EDS query.  If not present, defaults to the server
	// name from the target URI.
	EDSServiceName string
	// MaxConcurrentRequests is the max number of concurrent request allowed for
	// this service. If unset, default value 1024 is used.
	//
	// Note that this is not defined in the service config proto. And the reason
	// is, we are dropping EDS and moving the features into cluster_impl. But in
	// the mean time, to keep things working, we need to add this field. And it
	// should be fine to add this extra field here, because EDS is only used in
	// CDS today, so we have full control.
	MaxConcurrentRequests *uint32
	// LRS server to send load reports to.  If not present, load reporting
	// will be disabled.  If set to the empty string, load reporting will
	// be sent to the same server that we obtained CDS data from.
	LrsLoadReportingServerName *string
}

// edsConfigJSON is the intermediate unmarshal result of EDSConfig. ChildPolicy
// and Fallbackspolicy are post-processed, and for each, the first installed
// policy is kept.
type edsConfigJSON struct {
	ChildPolicy                []*loadBalancingConfig
	FallbackPolicy             []*loadBalancingConfig
	EDSServiceName             string
	MaxConcurrentRequests      *uint32
	LRSLoadReportingServerName *string
}

// UnmarshalJSON parses the JSON-encoded byte slice in data and stores it in l.
// When unmarshalling, we iterate through the childPolicy/fallbackPolicy lists
// and select the first LB policy which has been registered.
func (l *EDSConfig) UnmarshalJSON(data []byte) error {
	var configJSON edsConfigJSON
	if err := json.Unmarshal(data, &configJSON); err != nil {
		return err
	}

	l.EDSServiceName = configJSON.EDSServiceName
	l.MaxConcurrentRequests = configJSON.MaxConcurrentRequests
	l.LrsLoadReportingServerName = configJSON.LRSLoadReportingServerName

	for _, lbcfg := range configJSON.ChildPolicy {
		if balancer.Get(lbcfg.Name) != nil {
			l.ChildPolicy = lbcfg
			break
		}
	}

	for _, lbcfg := range configJSON.FallbackPolicy {
		if balancer.Get(lbcfg.Name) != nil {
			l.FallBackPolicy = lbcfg
			break
		}
	}
	return nil
}

// MarshalJSON returns a JSON encoding of l.
func (l *EDSConfig) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("EDSConfig.MarshalJSON() is unimplemented")
}

// loadBalancingConfig represents a single load balancing config,
// stored in JSON format.
type loadBalancingConfig struct {
	Name   string
	Config json.RawMessage
}

// MarshalJSON returns a JSON encoding of l.
func (l *loadBalancingConfig) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("loadBalancingConfig.MarshalJSON() is unimplemented")
}

// UnmarshalJSON parses the JSON-encoded byte slice in data and stores it in l.
func (l *loadBalancingConfig) UnmarshalJSON(data []byte) error {
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	for name, config := range cfg {
		l.Name = name
		l.Config = config
	}
	return nil
}
