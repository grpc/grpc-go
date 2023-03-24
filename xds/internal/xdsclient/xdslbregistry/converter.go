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

// Package xdslbregistry provides utilities to convert proto Load Balancing
// configuration to JSON Load Balancing configuration.
package xdslbregistry

import (
	"encoding/json"
	"fmt"
	"strings"

	v1 "github.com/cncf/xds/go/udpa/type/v1"
	v3 "github.com/cncf/xds/go/xds/type/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3ringhashpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/ring_hash/v3"
	v3wrrlocalitypb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/wrr_locality/v3"
	"github.com/golang/protobuf/proto"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/xds/internal/balancer/ringhash"
	"google.golang.org/grpc/xds/internal/balancer/wrrlocality"
)

const (
	defaultRingHashMinSize = 1024
	defaultRingHashMaxSize = 8 * 1024 * 1024 // 8M
)

// ConvertToServiceConfig converts a proto Load Balancing Policy configuration
// into a json string. Returns an error if no supported policy found, or if
// there is more than 16 layers of recursion in the configuration, or a failure
// to convert the policy.
func ConvertToServiceConfig(policy *v3clusterpb.LoadBalancingPolicy, depth int) (json.RawMessage, error) {
	// "Configurations that require more than 16 levels of recursion are
	// considered invalid and should result in a NACK response." - A51
	if depth > 15 {
		return nil, fmt.Errorf("lb policy %v exceeds max depth supported: 16 layers", policy)
	}

	// "This function iterate over the list of policy messages in
	// LoadBalancingPolicy, attempting to convert each one to gRPC form,
	// stopping at the first supported policy." - A52
	for _, plcy := range policy.GetPolicies() {
		// The policy message contains a TypedExtensionConfig
		// message with the configuration information. TypedExtensionConfig in turn
		// uses an Any typed typed_config field to store policy configuration of any
		// type. This typed_config field is used to determine both the name of a
		// policy and the configuration for it, depending on its type:
		switch plcy.GetTypedExtensionConfig().GetTypedConfig().GetTypeUrl() {
		case "type.googleapis.com/envoy.extensions.load_balancing_policies.ring_hash.v3.RingHash":
			if !envconfig.XDSRingHash {
				return nil, fmt.Errorf("unexpected lbPolicy %v", policy)
			}
			rhProto := &v3ringhashpb.RingHash{}
			if err := proto.Unmarshal(plcy.GetTypedExtensionConfig().GetTypedConfig().GetValue(), rhProto); err != nil {
				return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
			}
			return convertRingHash(rhProto)
		case "type.googleapis.com/envoy.extensions.load_balancing_policies.round_robin.v3.RoundRobin":
			return makeJSONValueOfName("round_robin", json.RawMessage("{}")), nil
		case "type.googleapis.com/envoy.extensions.load_balancing_policies.wrr_locality.v3.WrrLocality":
			wrrlProto := &v3wrrlocalitypb.WrrLocality{}
			if err := proto.Unmarshal(plcy.GetTypedExtensionConfig().GetTypedConfig().GetValue(), wrrlProto); err != nil {
				return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
			}
			return convertWrrLocality(wrrlProto, depth)
		// Any entry not in the above list is unsupported and will be skipped.
		// This includes Least Request as well, since grpc-go does not support
		// the Least Request Load Balancing Policy.
		case "type.googleapis.com/xds.type.v3.TypedStruct":
			tsProto := &v3.TypedStruct{}
			if err := proto.Unmarshal(plcy.GetTypedExtensionConfig().GetTypedConfig().GetValue(), tsProto); err != nil {
				return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
			}
			return convertCustomPolicyV3(tsProto)
		case "type.googleapis.com/udpa.type.v1.TypedStruct":
			tsProto := &v1.TypedStruct{}
			if err := proto.Unmarshal(plcy.GetTypedExtensionConfig().GetTypedConfig().GetValue(), tsProto); err != nil {
				return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
			}
			return convertCustomPolicyV1(tsProto)
		}
	}
	return nil, fmt.Errorf("no supported policy found in policy list +%v", policy)
}

// "the registry will maintain a set of converters that are able to map
// from the xDS LoadBalancingPolicy to the internal gRPC JSON format"
func convertRingHash(rhCfg *v3ringhashpb.RingHash) (json.RawMessage, error) {
	if rhCfg.GetHashFunction() != v3ringhashpb.RingHash_XX_HASH {
		return nil, fmt.Errorf("unsupported ring_hash hash function %v", rhCfg.GetHashFunction())
	}

	var minSize, maxSize uint64 = defaultRingHashMinSize, defaultRingHashMaxSize
	if min := rhCfg.GetMinimumRingSize(); min != nil {
		minSize = min.GetValue()
	}
	if max := rhCfg.GetMaximumRingSize(); max != nil {
		maxSize = max.GetValue()
	}

	rhLBCfg := ringhash.LBConfig{
		MinRingSize: minSize,
		MaxRingSize: maxSize,
	}
	rhLBCfgJSON, err := json.Marshal(rhLBCfg)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling json in ring hash converter: %v", err)
	}
	return makeJSONValueOfName(ringhash.Name, rhLBCfgJSON), nil
}

func convertWrrLocality(wrrlCfg *v3wrrlocalitypb.WrrLocality, depth int) (json.RawMessage, error) {
	epJSON, err := ConvertToServiceConfig(wrrlCfg.GetEndpointPickingPolicy(), depth+1)
	if err != nil {
		return nil, fmt.Errorf("error converting endpoint picking policy: %v for %+v", err, wrrlCfg)
	}
	wrrCfgJSON := createWRRConfig(epJSON)
	return makeJSONValueOfName(wrrlocality.Name, wrrCfgJSON), nil
}

func createWRRConfig(epCfgJSON json.RawMessage) json.RawMessage {
	return []byte(fmt.Sprintf(`{"childPolicy": %s}`, epCfgJSON))
}

// A52 defines a LeastRequest converter but grpc-go does not support least_request.

func convertCustomPolicyV3(typedStruct *v3.TypedStruct) (json.RawMessage, error) {
	return convertCustomPolicy(typedStruct.GetTypeUrl(), typedStruct.GetValue())
}

func convertCustomPolicyV1(typedStruct *v1.TypedStruct) (json.RawMessage, error) {
	return convertCustomPolicy(typedStruct.GetTypeUrl(), typedStruct.GetValue())
}

func convertCustomPolicy(typeURL string, s *structpb.Struct) (json.RawMessage, error) {
	// The gRPC policy name will be the "type name" part of the value of the
	// type_url field in the TypedStruct. We get this by using the part after
	// the last / character. Can assume a valid type_url from the control plane.
	urlsSplt := strings.Split(typeURL, "/")
	plcyName := urlsSplt[len(urlsSplt)-1]

	rawJSON, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("error converting custom lb policy %v: %v for %+v", err, typeURL, s)
	}
	// The Struct contained in the TypedStruct will be returned as-is as the
	// configuration JSON object.
	return makeJSONValueOfName(plcyName, rawJSON), nil
}

func makeJSONValueOfName(name string, value json.RawMessage) []byte {
	return []byte(fmt.Sprintf(`[{%q: %s}]`, name, value))
}
