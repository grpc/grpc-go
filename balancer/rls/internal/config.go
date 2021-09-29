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
 *
 */

package rls

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/ptypes"
	durationpb "github.com/golang/protobuf/ptypes/duration"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/rls/internal/keys"
	rlspb "google.golang.org/grpc/balancer/rls/internal/proto/grpc_lookup_v1"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

const (
	// This is max duration that we are willing to cache RLS responses. If the
	// service config doesn't specify a value for max_age or if it specified a
	// value greater that this, we will use this value instead.
	maxMaxAge = 5 * time.Minute
	// If lookup_service_timeout is not specified in the service config, we use
	// a default of 10 seconds.
	defaultLookupServiceTimeout = 10 * time.Second
	// This is set to the targetNameField in the child policy config during
	// service config validation.
	dummyChildPolicyTarget = "target_name_to_be_filled_in_later"
)

// lbConfig contains the parsed and validated contents of the
// loadBalancingConfig section of the service config. The RLS LB policy will
// use this to directly access config data instead of ploughing through proto
// fields.
type lbConfig struct {
	serviceconfig.LoadBalancingConfig

	kbMap                keys.BuilderMap
	lookupService        string
	lookupServiceTimeout time.Duration
	maxAge               time.Duration
	staleAge             time.Duration
	cacheSizeBytes       int64
	defaultTarget        string

	childPolicyName        string
	childPolicyConfig      map[string]json.RawMessage
	childPolicyTargetField string
}

func (lbCfg *lbConfig) Equal(other *lbConfig) bool {
	return lbCfg.kbMap.Equal(other.kbMap) &&
		lbCfg.lookupService == other.lookupService &&
		lbCfg.lookupServiceTimeout == other.lookupServiceTimeout &&
		lbCfg.maxAge == other.maxAge &&
		lbCfg.staleAge == other.staleAge &&
		lbCfg.cacheSizeBytes == other.cacheSizeBytes &&
		lbCfg.defaultTarget == other.defaultTarget &&
		lbCfg.childPolicyName == other.childPolicyName &&
		lbCfg.childPolicyTargetField == other.childPolicyTargetField &&
		childPolicyConfigEqual(lbCfg.childPolicyConfig, other.childPolicyConfig)
}

func childPolicyConfigEqual(a, b map[string]json.RawMessage) bool {
	if (b == nil) != (a == nil) {
		return false
	}
	if len(b) != len(a) {
		return false
	}

	for k, jsonA := range a {
		jsonB, ok := b[k]
		if !ok {
			return false
		}
		if !bytes.Equal(jsonA, jsonB) {
			return false
		}
	}
	return true
}

// This struct resembles the JSON representation of the loadBalancing config
// and makes it easier to unmarshal.
type lbConfigJSON struct {
	RouteLookupConfig                json.RawMessage
	ChildPolicy                      []map[string]json.RawMessage
	ChildPolicyConfigTargetFieldName string
}

// ParseConfig parses and validates the JSON representation of the service
// config and returns the loadBalancingConfig to be used by the RLS LB policy.
//
// Helps implement the balancer.ConfigParser interface.
// * childPolicy field:
//  - must find a valid child policy with a valid config (the child policy must
//    be able to parse the provided config successfully when we pass it a dummy
//    target name in the target_field provided by the
//    childPolicyConfigTargetFieldName field)
// * childPolicyConfigTargetFieldName field:
//   - must be set and non-empty
func (*rlsBB) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	cfgJSON := &lbConfigJSON{}
	if err := json.Unmarshal(c, cfgJSON); err != nil {
		return nil, fmt.Errorf("rls: json unmarshal failed for service config {%+v}: %v", string(c), err)
	}

	// Unmarshal and validate contents of the RLS proto.
	m := jsonpb.Unmarshaler{AllowUnknownFields: true}
	rlsProto := &rlspb.RouteLookupConfig{}
	if err := m.Unmarshal(bytes.NewReader(cfgJSON.RouteLookupConfig), rlsProto); err != nil {
		return nil, fmt.Errorf("rls: bad RouteLookupConfig proto {%+v}: %v", string(cfgJSON.RouteLookupConfig), err)
	}
	lbCfg, err := parseRLSProto(rlsProto)
	if err != nil {
		return nil, err
	}

	// Unmarshal and validate child policy configs.
	if cfgJSON.ChildPolicyConfigTargetFieldName == "" {
		return nil, fmt.Errorf("rls: childPolicyConfigTargetFieldName field is not set in service config {%+v}", string(c))
	}
	name, config, err := parseChildPolicyConfigs(cfgJSON.ChildPolicy, cfgJSON.ChildPolicyConfigTargetFieldName)
	if err != nil {
		return nil, err
	}
	lbCfg.childPolicyName = name
	lbCfg.childPolicyConfig = config
	lbCfg.childPolicyTargetField = cfgJSON.ChildPolicyConfigTargetFieldName
	return lbCfg, nil
}

// parseRLSProto fetches relevant information from the RouteLookupConfig proto
// and validates the values in the process.
//
// The following validation checks are performed:
// ** grpc_keybuilders field:
//    - must have at least one entry
//    - must not have two entries with the same Name
//    - must not have any entry with a Name with the service field unset or
//      empty
//    - must not have any entries without a Name
//    - must not have a headers entry that has required_match set
//    - must not have two headers entries with the same key within one entry
// ** lookup_service field:
//    - must be set and non-empty and must parse as a target URI
// ** max_age field:
//    - if not specified or is greater than maxMaxAge, it will be reset to
//      maxMaxAge
// ** stale_age field:
//    - if the value is greater than or equal to max_age, it is ignored
//    - if set, then max_age must also be set
// ** valid_targets field:
//    - will be ignored
// ** cache_size_bytes field:
//    - must be greater than zero
//    - TODO(easwars): Define a minimum value for this field, to be used when
//      left unspecified
func parseRLSProto(rlsProto *rlspb.RouteLookupConfig) (*lbConfig, error) {
	kbMap, err := keys.MakeBuilderMap(rlsProto)
	if err != nil {
		return nil, err
	}

	lookupService := rlsProto.GetLookupService()
	if lookupService == "" {
		return nil, fmt.Errorf("rls: empty lookup_service in route lookup config {%+v}", rlsProto)
	}
	parsedTarget, err := url.Parse(lookupService)
	if err != nil {
		// If the first attempt failed because of a missing scheme, try again
		// with the default scheme.
		parsedTarget, err = url.Parse(resolver.GetDefaultScheme() + ":///" + lookupService)
		if err != nil {
			return nil, fmt.Errorf("rls: invalid target URI in lookup_service {%s}", lookupService)
		}
	}
	if parsedTarget.Scheme == "" {
		parsedTarget.Scheme = resolver.GetDefaultScheme()
	}
	if resolver.Get(parsedTarget.Scheme) == nil {
		return nil, fmt.Errorf("rls: unregistered scheme in lookup_service {%s}", lookupService)
	}

	lookupServiceTimeout, err := convertDuration(rlsProto.GetLookupServiceTimeout())
	if err != nil {
		return nil, fmt.Errorf("rls: failed to parse lookup_service_timeout in route lookup config {%+v}: %v", rlsProto, err)
	}
	if lookupServiceTimeout == 0 {
		lookupServiceTimeout = defaultLookupServiceTimeout
	}
	maxAge, err := convertDuration(rlsProto.GetMaxAge())
	if err != nil {
		return nil, fmt.Errorf("rls: failed to parse max_age in route lookup config {%+v}: %v", rlsProto, err)
	}
	staleAge, err := convertDuration(rlsProto.GetStaleAge())
	if err != nil {
		return nil, fmt.Errorf("rls: failed to parse staleAge in route lookup config {%+v}: %v", rlsProto, err)
	}
	if staleAge != 0 && maxAge == 0 {
		return nil, fmt.Errorf("rls: stale_age is set, but max_age is not in route lookup config {%+v}", rlsProto)
	}
	if staleAge >= maxAge {
		logger.Info("rls: stale_age {%v} is greater than max_age {%v}, ignoring it", staleAge, maxAge)
		staleAge = 0
	}
	if maxAge == 0 || maxAge > maxMaxAge {
		logger.Infof("rls: max_age in route lookup config is %v, using %v", maxAge, maxMaxAge)
		maxAge = maxMaxAge
	}
	cacheSizeBytes := rlsProto.GetCacheSizeBytes()
	if cacheSizeBytes <= 0 {
		return nil, fmt.Errorf("rls: cache_size_bytes must be greater than 0 in route lookup config {%+v}", rlsProto)
	}
	return &lbConfig{
		kbMap:                kbMap,
		lookupService:        lookupService,
		lookupServiceTimeout: lookupServiceTimeout,
		maxAge:               maxAge,
		staleAge:             staleAge,
		cacheSizeBytes:       cacheSizeBytes,
		defaultTarget:        rlsProto.GetDefaultTarget(),
	}, nil
}

// parseChildPolicyConfigs iterates through the list of child policies and picks
// the first registered policy and validates its config.
func parseChildPolicyConfigs(childPolicies []map[string]json.RawMessage, targetFieldName string) (string, map[string]json.RawMessage, error) {
	for i, config := range childPolicies {
		if len(config) != 1 {
			return "", nil, fmt.Errorf("rls: invalid childPolicy: entry %v does not contain exactly 1 policy/config pair: %q", i, config)
		}

		var name string
		var rawCfg json.RawMessage
		for name, rawCfg = range config {
		}
		builder := balancer.Get(name)
		if builder == nil {
			continue
		}
		parser, ok := builder.(balancer.ConfigParser)
		if !ok {
			return "", nil, fmt.Errorf("rls: childPolicy %q with config %q does not support config parsing", name, string(rawCfg))
		}

		// To validate child policy configs we do the following:
		// - unmarshal the raw JSON bytes of the child policy config into a map
		// - add an entry with key set to `target_field_name` and a dummy value
		// - marshal the map back to JSON and parse the config using the parser
		// retrieved previously
		var childConfig map[string]json.RawMessage
		if err := json.Unmarshal(rawCfg, &childConfig); err != nil {
			return "", nil, fmt.Errorf("rls: json unmarshal failed for child policy config %q: %v", string(rawCfg), err)
		}
		childConfig[targetFieldName], _ = json.Marshal(dummyChildPolicyTarget)
		jsonCfg, err := json.Marshal(childConfig)
		if err != nil {
			return "", nil, fmt.Errorf("rls: json marshal failed for child policy config {%+v}: %v", childConfig, err)
		}
		if _, err := parser.ParseConfig(jsonCfg); err != nil {
			return "", nil, fmt.Errorf("rls: childPolicy config validation failed: %v", err)
		}
		return name, childConfig, nil
	}
	return "", nil, fmt.Errorf("rls: invalid childPolicy config: no supported policies found in %+v", childPolicies)
}

func convertDuration(d *durationpb.Duration) (time.Duration, error) {
	if d == nil {
		return 0, nil
	}
	return ptypes.Duration(d)
}
