//  +build !appengine

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

// Package rls implements the RLS LB policy.
package rls

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/rls/internal/keys"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/grpcutil"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"

	rlspb "google.golang.org/grpc/balancer/rls/internal/proto/grpc_lookup_v1"
)

const (
	rlsBalancerName = "rls"
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

func init() {
	balancer.Register(&rlsBB{})
}

// rlsBB helps build RLS load balancers and parse the service config to be
// passed to the RLS load balancer.
type rlsBB struct {
	// TODO(easwars): Implement the Build() method and remove this embedding.
	balancer.Builder
}

// Name returns the name of the RLS LB policy and helps implement the
// balancer.Balancer interface.
func (*rlsBB) Name() string {
	return rlsBalancerName
}

// ParseConfig parses and validates the JSON representation of the service
// config and returns the loadBalancingConfig to be used by the RLS LB policy.
//
// Helps implement the balancer.ConfigParser interface.
//
// The following validation checks are performed:
// * routeLookupConfig:
//   ** grpc_keybuilders field:
//      - must have at least one entry
//      - must not have two entries with the same Name
//      - must not have any entry with a Name with the service field unset or
//        empty
//      - must not have any entries without a Name
//      - must not have a headers entry that has required_match set
//      - must not have two headers entries with the same key within one entry
//   ** lookup_service field:
//      - must be set and non-empty and must parse as a target URI
//   ** max_age field:
//      - if not specified or is greater than maxMaxAge, it will be reset to
//        maxMaxAge
//   ** stale_age field:
//      - if the value is greater than or equal to max_age, it is ignored
//      - if set, then max_age must also be set
//   ** valid_targets field:
//      - will be ignored
//   ** cache_size_bytes field:
//      - must be greater than zero
//      - TODO(easwars): Define a minimum value for this field, to be used when
//        left unspecified
//   ** request_processing_strategy field:
//      - must have a value other than STRATEGY_UNSPECIFIED
//      - if set to SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR or
//        ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS, the default_target field must be
//        set to a non-empty value
// * childPolicy field:
//  - must find a valid child policy with a valid config (the child policy must
//    be able to parse the provided config successfully when we pass it a dummy
//    target name in the target_field provided by the
//    childPolicyConfigTargetFieldName field)
// * childPolicyConfigTargetFieldName field:
//   - must be set and non-empty
func (*rlsBB) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	cfg := &lbParsedConfig{}
	if err := json.Unmarshal(c, cfg); err != nil {
		return nil, fmt.Errorf("rls: json unmarshal failed for service config {%+v}: %v", string(c), err)
	}

	kbMap, err := keys.MakeBuilderMap(cfg.rlsProto)
	if err != nil {
		return nil, err
	}

	lookupService := cfg.rlsProto.GetLookupService()
	if lookupService == "" {
		return nil, fmt.Errorf("rls: empty lookup_service in service config {%+v}", string(c))
	}
	parsedTarget := grpcutil.ParseTarget(lookupService)
	if parsedTarget.Scheme == "" {
		parsedTarget.Scheme = resolver.GetDefaultScheme()
	}
	if resolver.Get(parsedTarget.Scheme) == nil {
		return nil, fmt.Errorf("rls: invalid target URI in lookup_service {%s}", lookupService)
	}

	var lookupServiceTimeout, maxAge, staleAge time.Duration

	if lst := cfg.rlsProto.GetLookupServiceTimeout(); lst != nil {
		lookupServiceTimeout, err = ptypes.Duration(lst)
		if err != nil {
			return nil, fmt.Errorf("rls: failed to parse lookup_service_timeout in service config {%+v}", string(c))
		}
	}
	if lookupServiceTimeout == 0 {
		lookupServiceTimeout = defaultLookupServiceTimeout
	}

	if ma := cfg.rlsProto.GetMaxAge(); ma != nil {
		maxAge, err = ptypes.Duration(ma)
		if err != nil {
			return nil, fmt.Errorf("rls: failed to parse max_age in service config {%+v}", string(c))
		}
	}
	if sa := cfg.rlsProto.GetStaleAge(); sa != nil {
		staleAge, err = ptypes.Duration(sa)
		if err != nil {
			return nil, fmt.Errorf("rls: failed to parse staleAge in service config {%+v}", string(c))
		}
	}
	if staleAge != 0 && maxAge == 0 {
		return nil, fmt.Errorf("rls: stale_age is set, but max_age is not in service config {%+v}", string(c))
	}
	if staleAge >= maxAge {
		grpclog.Info("rls: stale_age {%v} is greater than max_age {%v}, ignoring it", staleAge, maxAge)
		staleAge = 0
	}
	if maxAge == 0 || maxAge > maxMaxAge {
		grpclog.Infof("rls: max_age in service config is %v, using %v", maxAge, maxMaxAge)
		maxAge = maxMaxAge
	}

	cacheSizeBytes := cfg.rlsProto.GetCacheSizeBytes()
	if cacheSizeBytes <= 0 {
		return nil, fmt.Errorf("rls: cache_size_bytes must be greater than 0 in service config {%+v}", string(c))
	}

	rpStrategy := cfg.rlsProto.GetRequestProcessingStrategy()
	if rpStrategy == rlspb.RouteLookupConfig_STRATEGY_UNSPECIFIED {
		return nil, fmt.Errorf("rls: request_processing_strategy cannot be left unspecified in service config {%+v}", string(c))
	}
	defaultTarget := cfg.rlsProto.GetDefaultTarget()
	if (rpStrategy == rlspb.RouteLookupConfig_SYNC_LOOKUP_DEFAULT_TARGET_ON_ERROR ||
		rpStrategy == rlspb.RouteLookupConfig_ASYNC_LOOKUP_DEFAULT_TARGET_ON_MISS) && defaultTarget == "" {
		return nil, fmt.Errorf("rls: request_processing_strategy is %s, but default_target is not set", rpStrategy.String())
	}

	if cfg.childPolicy == nil {
		return nil, fmt.Errorf("rls: childPolicy is invalid in service config {%+v}", string(c))
	}
	if cfg.targetField == "" {
		return nil, fmt.Errorf("rls: childPolicyConfigTargetFieldName field is not set in service config {%+v}", string(c))
	}
	cpCfg, err := validateChildPolicyConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &lbConfig{
		kbMap:                kbMap,
		lookupService:        parsedTarget,
		lookupServiceTimeout: lookupServiceTimeout,
		maxAge:               maxAge,
		staleAge:             staleAge,
		cacheSizeBytes:       cacheSizeBytes,
		rpStrategy:           rpStrategy,
		cpName:               cfg.childPolicy.Name,
		cpTargetField:        cfg.targetField,
		cpConfig:             cpCfg,
	}, nil
}

// validateChildPolicyConfig validates the child policy config received in the
// service config. This makes it possible for us to reject service configs
// which contain invalid child policy configs which we know will fail for sure.
//
// It does the following:
// * Unmarshals the provided child policy config into a map of string to
//   json.RawMessage. This allows us to add an entry to the map corresponding
//   to the targetFieldName that we received in the service config.
// * Marshals the map back into JSON, finds the config parser associated with
//   the child policy and asks it to validate the config.
// * If the validation succeeded, removes the dummy entry from the map and
//   returns it. If any of the above steps failed, it returns an error.
func validateChildPolicyConfig(cfg *lbParsedConfig) (map[string]json.RawMessage, error) {
	var childConfig map[string]json.RawMessage
	if err := json.Unmarshal(cfg.childPolicy.Config, &childConfig); err != nil {
		return nil, fmt.Errorf("rls: json unmarshal failed for child policy config {%+v}: %v", cfg.childPolicy.Config, err)
	}
	childConfig[cfg.targetField], _ = json.Marshal(dummyChildPolicyTarget)

	jsonCfg, err := json.Marshal(childConfig)
	if err != nil {
		return nil, fmt.Errorf("rls: json marshal failed for child policy config {%+v}: %v", childConfig, err)
	}
	builder := balancer.Get(cfg.childPolicy.Name)
	if builder == nil {
		// This should never happen since we already made sure that the child
		// policy name mentioned in the service config is a valid one.
		return nil, fmt.Errorf("rls: balancer builder not found for child_policy %v", cfg.childPolicy.Name)
	}
	parser, ok := builder.(balancer.ConfigParser)
	if !ok {
		return nil, fmt.Errorf("rls: balancer builder for child_policy does not implement balancer.ConfigParser: %v", cfg.childPolicy.Name)
	}
	_, err = parser.ParseConfig(jsonCfg)
	if err != nil {
		return nil, fmt.Errorf("rls: childPolicy config validation failed: %v", err)
	}
	delete(childConfig, cfg.targetField)
	return childConfig, nil
}
