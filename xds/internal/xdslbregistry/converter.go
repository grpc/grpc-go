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

// Package xdslbregistry provides utilities to convert proto load balancing
// configuration, defined by the xDS API spec, to JSON load balancing
// configuration. These converters are registered by proto type in a registry,
// which gets pulled from based off proto type passed in.
package xdslbregistry

import (
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/weightedroundrobin"
	"google.golang.org/grpc/internal/envconfig"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/ringhash"
	"google.golang.org/grpc/xds/internal/balancer/wrrlocality"

	v1xdsudpatypepb "github.com/cncf/xds/go/udpa/type/v1"
	v3xdsxdstypepb "github.com/cncf/xds/go/xds/type/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3clientsideweightedroundrobinpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/client_side_weighted_round_robin/v3"
	v3ringhashpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/ring_hash/v3"
	v3wrrlocalitypb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/wrr_locality/v3"
	"github.com/golang/protobuf/proto"
	structpb "github.com/golang/protobuf/ptypes/struct"
)

var (
	// m is a map from proto type to converter.
	m = make(map[string]converter)
)

func init() {
	m = map[string]converter{
		"type.googleapis.com/envoy.extensions.load_balancing_policies.ring_hash.v3.RingHash":                                            convertRingHashProtoToServiceConfig,
		"type.googleapis.com/envoy.extensions.load_balancing_policies.round_robin.v3.RoundRobin":                                        convertRoundRobinProtoToServiceConfig,
		"type.googleapis.com/envoy.extensions.load_balancing_policies.wrr_locality.v3.WrrLocality":                                      convertWRRLocalityProtoToServiceConfig,
		"type.googleapis.com/envoy.extensions.load_balancing_policies.client_side_weighted_round_robin.v3.ClientSideWeightedRoundRobin": convertWeightedRoundRobinProtoToServiceConfig,
		"type.googleapis.com/xds.type.v3.TypedStruct":                                                                                   convertV3TypedStructToServiceConfig,
		"type.googleapis.com/udpa.type.v1.TypedStruct":                                                                                  convertV1TypedStructToServiceConfig,
	}
}

// converter converts raw proto bytes into the internal Go JSON representation
// of the proto passed. Returns the json message,  and an error. If both
// returned are nil, it represents continuing to the next proto.
type converter func([]byte, int) (json.RawMessage, error)

const (
	defaultRingHashMinSize = 1024
	defaultRingHashMaxSize = 8 * 1024 * 1024 // 8M
)

// ConvertToServiceConfig converts a proto Load Balancing Policy configuration
// into a json string. Returns an error if:
//   - no supported policy found
//   - there is more than 16 layers of recursion in the configuration
//   - a failure occurs when converting the policy
func ConvertToServiceConfig(lbPolicy *v3clusterpb.LoadBalancingPolicy) (json.RawMessage, error) {
	return convertToServiceConfig(lbPolicy, 0)
}

func convertToServiceConfig(lbPolicy *v3clusterpb.LoadBalancingPolicy, depth int) (json.RawMessage, error) {
	// "Configurations that require more than 16 levels of recursion are
	// considered invalid and should result in a NACK response." - A51
	if depth > 15 {
		return nil, fmt.Errorf("lb policy %v exceeds max depth supported: 16 layers", lbPolicy)
	}

	// "This function iterate over the list of policy messages in
	// LoadBalancingPolicy, attempting to convert each one to gRPC form,
	// stopping at the first supported policy." - A52
	for _, policy := range lbPolicy.GetPolicies() {
		policy.GetTypedExtensionConfig().GetTypedConfig().GetTypeUrl()
		converter := m[policy.GetTypedExtensionConfig().GetTypedConfig().GetTypeUrl()]
		// "Any entry not in the above list is unsupported and will be skipped."
		// - A52
		// This includes Least Request as well, since grpc-go does not support
		// the Least Request Load Balancing Policy.
		if converter == nil {
			continue
		}
		json, err := converter(policy.GetTypedExtensionConfig().GetTypedConfig().GetValue(), depth)
		if json == nil && err == nil {
			continue
		}
		return json, err
	}
	return nil, fmt.Errorf("no supported policy found in policy list +%v", lbPolicy)
}

func convertRingHashProtoToServiceConfig(rawProto []byte, depth int) (json.RawMessage, error) {
	if !envconfig.XDSRingHash {
		return nil, nil
	}
	rhProto := &v3ringhashpb.RingHash{}
	if err := proto.Unmarshal(rawProto, rhProto); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
	}
	if rhProto.GetHashFunction() != v3ringhashpb.RingHash_XX_HASH {
		return nil, fmt.Errorf("unsupported ring_hash hash function %v", rhProto.GetHashFunction())
	}

	var minSize, maxSize uint64 = defaultRingHashMinSize, defaultRingHashMaxSize
	if min := rhProto.GetMinimumRingSize(); min != nil {
		minSize = min.GetValue()
	}
	if max := rhProto.GetMaximumRingSize(); max != nil {
		maxSize = max.GetValue()
	}

	rhCfg := &ringhash.LBConfig{
		MinRingSize: minSize,
		MaxRingSize: maxSize,
	}

	rhCfgJSON, err := json.Marshal(rhCfg)
	if err != nil {
		return nil, fmt.Errorf("error marshaling JSON for type %T: %v", rhCfg, err)
	}
	return makeBalancerConfigJSON(ringhash.Name, rhCfgJSON), nil
}

func convertRoundRobinProtoToServiceConfig([]byte, int) (json.RawMessage, error) {
	return makeBalancerConfigJSON("round_robin", json.RawMessage("{}")), nil
}

type wrrLocalityLBConfig struct {
	ChildPolicy json.RawMessage `json:"childPolicy,omitempty"`
}

func convertWRRLocalityProtoToServiceConfig(rawProto []byte, depth int) (json.RawMessage, error) {
	wrrlProto := &v3wrrlocalitypb.WrrLocality{}
	if err := proto.Unmarshal(rawProto, wrrlProto); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
	}
	epJSON, err := convertToServiceConfig(wrrlProto.GetEndpointPickingPolicy(), depth+1)
	if err != nil {
		return nil, fmt.Errorf("error converting endpoint picking policy: %v for %+v", err, wrrlProto)
	}
	wrrLCfg := wrrLocalityLBConfig{
		ChildPolicy: epJSON,
	}

	lbCfgJSON, err := json.Marshal(wrrLCfg)
	if err != nil {
		return nil, fmt.Errorf("error marshaling JSON for type %T: %v", wrrLCfg, err)
	}
	return makeBalancerConfigJSON(wrrlocality.Name, lbCfgJSON), nil
}

func convertWeightedRoundRobinProtoToServiceConfig(rawProto []byte, depth int) (json.RawMessage, error) {
	cswrrProto := &v3clientsideweightedroundrobinpb.ClientSideWeightedRoundRobin{}
	if err := proto.Unmarshal(rawProto, cswrrProto); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
	}
	wrrLBCfg := &wrrLBConfig{}
	// Only set fields if specified in proto. If not set, ParseConfig of the WRR
	// will populate the config with defaults.
	if enableOOBLoadReportCfg := cswrrProto.GetEnableOobLoadReport(); enableOOBLoadReportCfg != nil {
		wrrLBCfg.EnableOOBLoadReport = enableOOBLoadReportCfg.GetValue()
	}
	if oobReportingPeriodCfg := cswrrProto.GetOobReportingPeriod(); oobReportingPeriodCfg != nil {
		wrrLBCfg.OOBReportingPeriod = internalserviceconfig.Duration(oobReportingPeriodCfg.AsDuration())
	}
	if blackoutPeriodCfg := cswrrProto.GetBlackoutPeriod(); blackoutPeriodCfg != nil {
		wrrLBCfg.BlackoutPeriod = internalserviceconfig.Duration(blackoutPeriodCfg.AsDuration())
	}
	if weightExpirationPeriodCfg := cswrrProto.GetBlackoutPeriod(); weightExpirationPeriodCfg != nil {
		wrrLBCfg.WeightExpirationPeriod = internalserviceconfig.Duration(weightExpirationPeriodCfg.AsDuration())
	}
	if weightUpdatePeriodCfg := cswrrProto.GetWeightUpdatePeriod(); weightUpdatePeriodCfg != nil {
		wrrLBCfg.WeightUpdatePeriod = internalserviceconfig.Duration(weightUpdatePeriodCfg.AsDuration())
	}
	if errorUtilizationPenaltyCfg := cswrrProto.GetErrorUtilizationPenalty(); errorUtilizationPenaltyCfg != nil {
		wrrLBCfg.ErrorUtilizationPenalty = float64(errorUtilizationPenaltyCfg.GetValue())
	}

	lbCfgJSON, err := json.Marshal(wrrLBCfg)
	if err != nil {
		return nil, fmt.Errorf("error marshaling JSON for type %T: %v", wrrLBCfg, err)
	}
	return makeBalancerConfigJSON(weightedroundrobin.Name, lbCfgJSON), nil
}

func convertV1TypedStructToServiceConfig(rawProto []byte, depth int) (json.RawMessage, error) {
	tsProto := &v1xdsudpatypepb.TypedStruct{}
	if err := proto.Unmarshal(rawProto, tsProto); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
	}
	return convertCustomPolicy(tsProto.GetTypeUrl(), tsProto.GetValue())
}

func convertV3TypedStructToServiceConfig(rawProto []byte, depth int) (json.RawMessage, error) {
	tsProto := &v3xdsxdstypepb.TypedStruct{}
	if err := proto.Unmarshal(rawProto, tsProto); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
	}
	return convertCustomPolicy(tsProto.GetTypeUrl(), tsProto.GetValue())
}

// convertCustomPolicy attempts to prepare json configuration for a custom lb
// proto, which specifies the gRPC balancer type and configuration. Returns the
// converted json and an error which should cause caller to error if error
// converting. If both json and error returned are nil, it means the gRPC
// Balancer registry does not contain that balancer type, and the caller should
// continue to the next policy.
func convertCustomPolicy(typeURL string, s *structpb.Struct) (json.RawMessage, error) {
	// The gRPC policy name will be the "type name" part of the value of the
	// type_url field in the TypedStruct. We get this by using the part after
	// the last / character. Can assume a valid type_url from the control plane.
	pos := strings.LastIndex(typeURL, "/")
	name := typeURL[pos+1:]

	if balancer.Get(name) == nil {
		return nil, nil
	}

	rawJSON, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("error converting custom lb policy %v: %v for %+v", err, typeURL, s)
	}

	// The Struct contained in the TypedStruct will be returned as-is as the
	// configuration JSON object.
	return makeBalancerConfigJSON(name, rawJSON), nil
}

type wrrLBConfig struct {
	EnableOOBLoadReport     bool                           `json:"enableOobLoadReport,omitempty"`
	OOBReportingPeriod      internalserviceconfig.Duration `json:"oobReportingPeriod,omitempty"`
	BlackoutPeriod          internalserviceconfig.Duration `json:"blackoutPeriod,omitempty"`
	WeightExpirationPeriod  internalserviceconfig.Duration `json:"weightExpirationPeriod,omitempty"`
	WeightUpdatePeriod      internalserviceconfig.Duration `json:"weightUpdatePeriod,omitempty"`
	ErrorUtilizationPenalty float64                        `json:"errorUtilizationPenalty,omitempty"`
}

func makeBalancerConfigJSON(name string, value json.RawMessage) []byte {
	return []byte(fmt.Sprintf(`[{%q: %s}]`, name, value))
}
