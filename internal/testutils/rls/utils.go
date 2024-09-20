/*
 *
 * Copyright 2024 gRPC authors.
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
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal"
	rlspb "google.golang.org/grpc/internal/proto/grpc_lookup_v1"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 100 * time.Millisecond
	// rLSChildPolicyTargetNameField is a top-level field name to add to the child
	// policy's config, whose value is set to the target for the child policy.
	rlsChildPolicyTargetNameField = "Backend"
)

// BuildBasicRLSConfigWithChildPolicy constructs a very basic service config for
// the RLS LB policy. It also registers a test LB policy which is capable of
// being a child of the RLS LB policy.
func BuildBasicRLSConfigWithChildPolicy(t *testing.T, childPolicyName, rlsServerAddress string) *RLSConfig {
	childPolicyName = "test-child-policy" + childPolicyName
	RegisterRLSChildPolicy(childPolicyName, nil)
	t.Logf("Registered child policy with name %q", childPolicyName)

	return &RLSConfig{
		RouteLookupConfig: &rlspb.RouteLookupConfig{
			GrpcKeybuilders:      []*rlspb.GrpcKeyBuilder{{Names: []*rlspb.GrpcKeyBuilder_Name{{Service: "grpc.testing.TestService"}}}},
			LookupService:        rlsServerAddress,
			LookupServiceTimeout: durationpb.New(defaultTestTimeout),
			CacheSizeBytes:       1024,
		},
		RouteLookupChannelServiceConfig:  `{"loadBalancingConfig": [{"pick_first": {}}]}`,
		ChildPolicy:                      &internalserviceconfig.BalancerConfig{Name: childPolicyName},
		ChildPolicyConfigTargetFieldName: rlsChildPolicyTargetNameField,
	}
}

// StartManualResolverWithConfig registers and returns a manual resolver which
// pushes the RLS LB policy's service config on the channel.
func StartManualResolverWithConfig(t *testing.T, rlsConfig *RLSConfig) *manual.Resolver {
	t.Helper()

	scJSON, err := rlsConfig.ServiceConfigJSON()
	if err != nil {
		t.Fatal(err)
	}

	sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(scJSON)
	r := manual.NewBuilderWithScheme("rls-e2e")
	r.InitialState(resolver.State{ServiceConfig: sc})
	t.Cleanup(r.Close)
	return r
}

// RLSConfig is a utility type to build service config for the RLS LB policy.
type RLSConfig struct {
	RouteLookupConfig                *rlspb.RouteLookupConfig
	RouteLookupChannelServiceConfig  string
	ChildPolicy                      *internalserviceconfig.BalancerConfig
	ChildPolicyConfigTargetFieldName string
}

// ServiceConfigJSON generates service config with a load balancing config
// corresponding to the RLS LB policy.
func (c *RLSConfig) ServiceConfigJSON() (string, error) {
	m := protojson.MarshalOptions{
		Multiline:     true,
		Indent:        "  ",
		UseProtoNames: true,
	}
	routeLookupCfg, err := m.Marshal(c.RouteLookupConfig)
	if err != nil {
		return "", err
	}
	childPolicy, err := c.ChildPolicy.MarshalJSON()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`
{
  "loadBalancingConfig": [
    {
      "rls_experimental": {
        "routeLookupConfig": %s,
				"routeLookupChannelServiceConfig": %s,
        "childPolicy": %s,
        "childPolicyConfigTargetFieldName": %q
      }
    }
  ]
}`, string(routeLookupCfg), c.RouteLookupChannelServiceConfig, string(childPolicy), c.ChildPolicyConfigTargetFieldName), nil
}

// LoadBalancingConfig generates load balancing config which can used as part of
// a ClientConnState update to the RLS LB policy.
func (c *RLSConfig) LoadBalancingConfig() (serviceconfig.LoadBalancingConfig, error) {
	m := protojson.MarshalOptions{
		Multiline:     true,
		Indent:        "  ",
		UseProtoNames: true,
	}
	routeLookupCfg, err := m.Marshal(c.RouteLookupConfig)
	if err != nil {
		return nil, err
	}
	childPolicy, err := c.ChildPolicy.MarshalJSON()
	if err != nil {
		return nil, err
	}
	lbConfigJSON := fmt.Sprintf(`
{
  "routeLookupConfig": %s,
  "routeLookupChannelServiceConfig": %s,
  "childPolicy": %s,
  "childPolicyConfigTargetFieldName": %q
}`, string(routeLookupCfg), c.RouteLookupChannelServiceConfig, string(childPolicy), c.ChildPolicyConfigTargetFieldName)

	builder := balancer.Get("rls_experimental")
	if builder == nil {
		return nil, errors.New("balancer builder not found for RLS LB policy")
	}
	parser := builder.(balancer.ConfigParser)
	return parser.ParseConfig([]byte(lbConfigJSON))
}
