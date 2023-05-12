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

	v1xdsudpatypepb "github.com/cncf/xds/go/udpa/type/v1"
	v3xdsxdstypepb "github.com/cncf/xds/go/xds/type/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3clientsideweightedroundrobinpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/client_side_weighted_round_robin/v3"
	v3ringhashpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/ring_hash/v3"
	v3wrrlocalitypb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/wrr_locality/v3"
	"github.com/golang/protobuf/proto"
	structpb "github.com/golang/protobuf/ptypes/struct"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/weightedroundrobin"
	"google.golang.org/grpc/internal/envconfig"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/ringhash"
	"google.golang.org/grpc/xds/internal/balancer/wrrlocality"
)

var (
	// m is a map from proto type to converter.
	m = make(map[string]converter)
)

// converter converts raw proto bytes into the internal Go JSON representation
// of the proto passed. Returns the json message, an error, and a bool
// determining if the caller should continue to the next proto in the policy
// list.
type converter interface {
	convertToServiceConfig([]byte, int) (json.RawMessage, bool, error)
}

const (
	defaultRingHashMinSize = 1024
	defaultRingHashMaxSize = 8 * 1024 * 1024 // 8M
)

func init() {
	register("type.googleapis.com/envoy.extensions.load_balancing_policies.ring_hash.v3.RingHash", &ringHashConverter{})
	register("type.googleapis.com/envoy.extensions.load_balancing_policies.round_robin.v3.RoundRobin", &roundRobinConverter{})
	register("type.googleapis.com/envoy.extensions.load_balancing_policies.wrr_locality.v3.WrrLocality", &wrrLocalityConverter{})
	register("type.googleapis.com/envoy.extensions.load_balancing_policies.client_side_weighted_round_robin.v3.ClientSideWeightedRoundRobin", &weightedRoundRobinConverter{})
	register("type.googleapis.com/xds.type.v3.TypedStruct", &v3TypedStructConverter{})
	register("type.googleapis.com/udpa.type.v1.TypedStruct", &v1TypedStructConverter{})
}

// register registers the converter to the map keyed on a proto type.
func register(protoType string, c converter) {
	m[strings.ToLower(protoType)] = c
}

// getConverter returns the converter registered with the given proto type. If
// no converter is registered with the name, nil is returned.
func getConverter(name string) converter {
	if c, ok := m[strings.ToLower(name)]; ok {
		return c
	}
	return nil
}

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
		converter := getConverter(policy.GetTypedExtensionConfig().GetTypedConfig().GetTypeUrl())
		// "Any entry not in the above list is unsupported and will be skipped."
		// - A52
		// This includes Least Request as well, since grpc-go does not support
		// the Least Request Load Balancing Policy.
		if converter == nil {
			continue
		}
		json, cont, err := converter.convertToServiceConfig(policy.GetTypedExtensionConfig().GetTypedConfig().GetValue(), depth)
		if cont {
			continue
		}
		return json, err
	}
	return nil, fmt.Errorf("no supported policy found in policy list +%v", lbPolicy)
}

type ringHashConverter struct{}

func (rhc *ringHashConverter) convertToServiceConfig(rawProto []byte, depth int) (json.RawMessage, bool, error) {
	if !envconfig.XDSRingHash { // either have this as part of converter or top level - not a switch so top level
		return nil, true, nil
	}
	rhProto := &v3ringhashpb.RingHash{}
	if err := proto.Unmarshal(rawProto, rhProto); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal resource: %v", err)
	}
	if rhProto.GetHashFunction() != v3ringhashpb.RingHash_XX_HASH {
		return nil, false, fmt.Errorf("unsupported ring_hash hash function %v", rhProto.GetHashFunction())
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
		return nil, false, fmt.Errorf("error marshaling JSON for type %T: %v", rhCfg, err)
	}
	return makeBalancerConfigJSON(ringhash.Name, rhCfgJSON), false, nil
}

type roundRobinConverter struct{}

func (rrc *roundRobinConverter) convertToServiceConfig([]byte, int) (json.RawMessage, bool, error) {
	return makeBalancerConfigJSON("round_robin", json.RawMessage("{}")), false, nil
}

type wrrLocalityConverter struct {
	depth int
}

type wrrLocalityLBConfig struct {
	ChildPolicy json.RawMessage `json:"childPolicy,omitempty"`
}

func (wlc *wrrLocalityConverter) convertToServiceConfig(rawProto []byte, depth int) (json.RawMessage, bool, error) {
	wrrlProto := &v3wrrlocalitypb.WrrLocality{}
	if err := proto.Unmarshal(rawProto, wrrlProto); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal resource: %v", err)
	}
	epJSON, err := convertToServiceConfig(wrrlProto.GetEndpointPickingPolicy(), depth+1)
	if err != nil {
		return nil, false, fmt.Errorf("error converting endpoint picking policy: %v for %+v", err, wrrlProto)
	}
	wrrLCfg := wrrLocalityLBConfig{
		ChildPolicy: epJSON,
	}

	lbCfgJSON, err := json.Marshal(wrrLCfg)
	if err != nil {
		return nil, false, fmt.Errorf("error marshaling JSON for type %T: %v", wrrLCfg, err)
	}
	return makeBalancerConfigJSON(wrrlocality.Name, lbCfgJSON), false, nil
}

type weightedRoundRobinConverter struct{}

func (wrrc *weightedRoundRobinConverter) convertToServiceConfig(rawProto []byte, depth int) (json.RawMessage, bool, error) {
	cswrrProto := &v3clientsideweightedroundrobinpb.ClientSideWeightedRoundRobin{}
	if err := proto.Unmarshal(rawProto, cswrrProto); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal resource: %v", err)
	}
	wrrLBConfig := &wrrLBConfig{}
	// Only set fields if specified in proto. If not set, ParseConfig of the WRR
	// will populate the config with defaults.
	if enableOOBLoadReportCfg := cswrrProto.GetEnableOobLoadReport(); enableOOBLoadReportCfg != nil {
		wrrLBConfig.EnableOOBLoadReport = enableOOBLoadReportCfg.GetValue()
	}
	if oobReportingPeriodCfg := cswrrProto.GetOobReportingPeriod(); oobReportingPeriodCfg != nil {
		wrrLBConfig.OOBReportingPeriod = internalserviceconfig.Duration(oobReportingPeriodCfg.AsDuration())
	}
	if blackoutPeriodCfg := cswrrProto.GetBlackoutPeriod(); blackoutPeriodCfg != nil {
		wrrLBConfig.BlackoutPeriod = internalserviceconfig.Duration(blackoutPeriodCfg.AsDuration())
	}
	if weightExpirationPeriodCfg := cswrrProto.GetBlackoutPeriod(); weightExpirationPeriodCfg != nil {
		wrrLBConfig.WeightExpirationPeriod = internalserviceconfig.Duration(weightExpirationPeriodCfg.AsDuration())
	}
	if weightUpdatePeriodCfg := cswrrProto.GetWeightUpdatePeriod(); weightUpdatePeriodCfg != nil {
		wrrLBConfig.WeightUpdatePeriod = internalserviceconfig.Duration(weightUpdatePeriodCfg.AsDuration())
	}
	if errorUtilizationPenaltyCfg := cswrrProto.GetErrorUtilizationPenalty(); errorUtilizationPenaltyCfg != nil {
		wrrLBConfig.ErrorUtilizationPenalty = float64(errorUtilizationPenaltyCfg.GetValue())
	}

	lbCfgJSON, err := json.Marshal(wrrLBConfig)
	if err != nil {
		return nil, false, fmt.Errorf("error marshaling JSON for type %T: %v", wrrLBConfig, err)
	}
	return makeBalancerConfigJSON(weightedroundrobin.Name, lbCfgJSON), false, nil
}

type v1TypedStructConverter struct{}

func (v1tsc *v1TypedStructConverter) convertToServiceConfig(rawProto []byte, depth int) (json.RawMessage, bool, error) {
	tsProto := &v1xdsudpatypepb.TypedStruct{}
	if err := proto.Unmarshal(rawProto, tsProto); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal resource: %v", err)
	}
	return convertCustomPolicy(tsProto.GetTypeUrl(), tsProto.GetValue())
}

type v3TypedStructConverter struct{}

func (v3tsc *v3TypedStructConverter) convertToServiceConfig(rawProto []byte, depth int) (json.RawMessage, bool, error) {
	tsProto := &v3xdsxdstypepb.TypedStruct{}
	if err := proto.Unmarshal(rawProto, tsProto); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal resource: %v", err)
	}
	return convertCustomPolicy(tsProto.GetTypeUrl(), tsProto.GetValue())
}

// convertCustomPolicy attempts to prepare json configuration for a custom lb
// proto, which specifies the gRPC balancer type and configuration. Returns the
// converted json, a bool representing whether the caller should continue to the
// next policy, which is true if the gRPC Balancer registry does not contain
// that balancer type, and an error which should cause caller to error if error
// converting.
func convertCustomPolicy(typeURL string, s *structpb.Struct) (json.RawMessage, bool, error) {
	// The gRPC policy name will be the "type name" part of the value of the
	// type_url field in the TypedStruct. We get this by using the part after
	// the last / character. Can assume a valid type_url from the control plane.
	urls := strings.Split(typeURL, "/")
	name := urls[len(urls)-1]

	if balancer.Get(name) == nil {
		return nil, true, nil
	}

	rawJSON, err := json.Marshal(s)
	if err != nil {
		return nil, false, fmt.Errorf("error converting custom lb policy %v: %v for %+v", err, typeURL, s)
	}

	// The Struct contained in the TypedStruct will be returned as-is as the
	// configuration JSON object.
	return makeBalancerConfigJSON(name, rawJSON), false, nil
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
