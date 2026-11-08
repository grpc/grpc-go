/*
 *
 * Copyright 2023 gRPC authors.
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

// Package xdslbregistry provides a registry of converters that convert proto
// from load balancing configuration, defined by the xDS API spec, to JSON load
// balancing configuration.
package xdslbregistry

import (
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/grpcservice"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
)

var (
	// m is a map from proto type to Converter.
	m = make(map[string]Converter)
)

// Register registers the converter to the map keyed on a proto type. Must be
// called at init time. Not thread safe.
func Register(protoType string, c Converter) {
	m[protoType] = c
}

// UnregisterForTesting unregisters the converter for the given proto type for
// testing purposes. Not thread safe.
func UnregisterForTesting(protoType string) {
	delete(m, protoType)
}

// SetRegistry sets the xDS LB registry. Must be called at init time. Not thread
// safe.
func SetRegistry(registry map[string]Converter) {
	m = registry
}

// ConverterOptions contains additional information passed to a Converter, and
// is passed unmodified to converters of child policies.
type ConverterOptions struct {
	// BootstrapConfig contains the complete bootstrap configuration used by the
	// xDS client that received the resource being converted.
	BootstrapConfig *bootstrap.Config
	// ServerConfig contains the configuration of the xDS management server that
	// sent the resource being converted.
	ServerConfig *bootstrap.ServerConfig
}

// ConverterResult contains the result of a successful conversion.
type ConverterResult struct {
	// LBConfig is the gRPC JSON representation of the converted LB policy
	// configuration.
	LBConfig json.RawMessage
	// GRPCServices contains the parsed representation of GrpcService protos
	// (gRFC A102) found in the LB policy configuration (or in the
	// configuration of any of its child policies), keyed by a string that
	// uniquely identifies the contents of the GrpcService. The JSON
	// configuration in LBConfig refers to entries in this map using these keys.
	// It is nil if no such protos were found.
	GRPCServices map[string]*grpcservice.Config
}

// Converter converts raw proto bytes of an LB policy configuration into a
// ConverterResult. depth is the depth of the policy in the LB policy tree, and
// converters of policies that have child policies must pass depth+1 and opts
// when calling ConvertToServiceConfig for those child policies. Such
// converters must also propagate the GRPCServices from the results of their
// child policies.
//
// If both returned values are nil, the policy is considered unsupported, and
// the next policy in the list is attempted.
type Converter func(rawProto []byte, depth int, opts ConverterOptions) (*ConverterResult, error)

// ConvertToServiceConfig converts a proto Load Balancing Policy configuration
// into a ConverterResult containing the JSON configuration. Returns an error
// if:
//   - no supported policy found
//   - there is more than 16 layers of recursion in the configuration
//   - a failure occurs when converting the policy
func ConvertToServiceConfig(lbPolicy *v3clusterpb.LoadBalancingPolicy, depth int, opts ConverterOptions) (*ConverterResult, error) {
	// "Configurations that require more than 16 levels of recursion are
	// considered invalid and should result in a NACK response." - A51
	if depth > 15 {
		return nil, fmt.Errorf("lb policy %v exceeds max depth supported: 16 layers", lbPolicy)
	}

	// "This function iterate over the list of policy messages in
	// LoadBalancingPolicy, attempting to convert each one to gRPC form,
	// stopping at the first supported policy." - A52
	for _, policy := range lbPolicy.GetPolicies() {
		converter := m[policy.GetTypedExtensionConfig().GetTypedConfig().GetTypeUrl()]
		// "Any entry not in the above list is unsupported and will be skipped."
		// - A52
		if converter == nil {
			continue
		}
		res, err := converter(policy.GetTypedExtensionConfig().GetTypedConfig().GetValue(), depth, opts)
		if err != nil {
			return nil, err
		}
		if res == nil {
			continue
		}
		return res, nil
	}
	return nil, fmt.Errorf("no supported policy found in policy list +%v", lbPolicy)
}
