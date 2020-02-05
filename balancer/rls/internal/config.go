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
	"time"

	"github.com/golang/protobuf/jsonpb"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"

	"google.golang.org/grpc/balancer/rls/internal/keys"
	rlspb "google.golang.org/grpc/balancer/rls/internal/proto/grpc_lookup_v1"
)

// lbConfig contains the parsed and validated contents of the
// loadBalancingConfig section of the service config. The RLS LB policy will
// use this to directly access config data instead of ploughing through proto
// fields.
type lbConfig struct {
	serviceconfig.LoadBalancingConfig

	kbMap                keys.BuilderMap
	lookupService        resolver.Target
	lookupServiceTimeout time.Duration
	maxAge               time.Duration
	staleAge             time.Duration
	cacheSizeBytes       int64
	rpStrategy           rlspb.RouteLookupConfig_RequestProcessingStrategy
	cpName               string
	cpTargetField        string
	cpConfig             map[string]json.RawMessage
}

// lbParsedConfig is an parsed internal representation of the JSON
// loadBalancing config for the RLS LB policy. This config is put through
// further validation checks and transformed into another representation before
// handing it off to the RLS LB policy.
type lbParsedConfig struct {
	rlsProto    *rlspb.RouteLookupConfig
	childPolicy *loadBalancingConfig
	targetField string
}

// UnmarshalJSON parses the JSON-encoded byte slice in data and stores it in l.
// When unmarshalling, we iterate through the childPolicy list and select the
// first LB policy which has been registered.
// Returns error only when JSON unmarshaling fails. Further validation checks
// are required to verify the sanity of the parsed config.
func (l *lbParsedConfig) UnmarshalJSON(data []byte) error {
	var jsonData map[string]json.RawMessage
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return fmt.Errorf("bad input json data: %v", err)
	}

	// We declare local variables to store the result of JSON unmarshaling, and
	// set them to appropriate fields in the lbParsedConfig only if everything goes
	// smoothly.
	var (
		rlsProto    *rlspb.RouteLookupConfig
		childPolicy *loadBalancingConfig
		targetField string
	)
	for k, v := range jsonData {
		switch k {
		case "routeLookupConfig":
			m := jsonpb.Unmarshaler{AllowUnknownFields: true}
			rlsProto = &rlspb.RouteLookupConfig{}
			if err := m.Unmarshal(bytes.NewReader(v), rlsProto); err != nil {
				return fmt.Errorf("bad routeLookupConfig proto: %v", err)
			}
		case "childPolicy":
			var lbCfgs []*loadBalancingConfig
			if err := json.Unmarshal(v, &lbCfgs); err != nil {
				return fmt.Errorf("bad childPolicy config: %v", err)
			}
			for _, lbcfg := range lbCfgs {
				if balancer.Get(lbcfg.Name) != nil {
					childPolicy = lbcfg
					break
				}
			}
		case "childPolicyConfigTargetFieldName":
			if err := json.Unmarshal(v, &targetField); err != nil {
				return fmt.Errorf("bad childPolicyConfigTargetFieldName: %v", err)
			}
		default:
			grpclog.Warningf("rls: unknown field %q in ServiceConfig", k)
		}
	}

	l.rlsProto = rlsProto
	l.childPolicy = childPolicy
	l.targetField = targetField
	return nil
}

// MarshalJSON returns a JSON encoding of l.
func (l *lbParsedConfig) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("lbParsedConfig.MarshalJSON() is unimplemented")
}

// loadBalancingConfig represents a single load balancing config,
// stored in JSON format.
type loadBalancingConfig struct {
	Name   string
	Config json.RawMessage
}

// MarshalJSON returns a JSON encoding of l.
func (l *loadBalancingConfig) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("rls: loadBalancingConfig.MarshalJSON() is unimplemented")
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
