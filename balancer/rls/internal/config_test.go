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
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	_ "google.golang.org/grpc/balancer/grpclb"               // grpclb for config parsing.
	_ "google.golang.org/grpc/internal/resolver/passthrough" // passthrough resolver.
)

const balancerWithoutConfigParserName = "dummy_balancer"

type dummyBB struct {
	balancer.Builder
}

func (*dummyBB) Name() string {
	return balancerWithoutConfigParserName
}

func init() {
	balancer.Register(&dummyBB{})
}

// testEqual reports whether the lbCfgs a and b are equal. This is to be used
// only from tests. This ignores the keyBuilderMap field because its internals
// are not exported, and hence not possible to specify in the want section of
// the test. This is fine because we already have tests to make sure that the
// keyBuilder is parsed properly from the service config.
func testEqual(a, b *lbConfig) bool {
	return a.lookupService == b.lookupService &&
		a.lookupServiceTimeout == b.lookupServiceTimeout &&
		a.maxAge == b.maxAge &&
		a.staleAge == b.staleAge &&
		a.cacheSizeBytes == b.cacheSizeBytes &&
		a.defaultTarget == b.defaultTarget &&
		a.childPolicyName == b.childPolicyName &&
		a.childPolicyTargetField == b.childPolicyTargetField &&
		childPolicyConfigEqual(a.childPolicyConfig, b.childPolicyConfig)
}

func TestParseConfig(t *testing.T) {
	childPolicyTargetFieldVal, _ := json.Marshal(dummyChildPolicyTarget)
	tests := []struct {
		desc    string
		input   []byte
		wantCfg *lbConfig
	}{
		// This input validates a few cases:
		// - A top-level unknown field should not fail.
		// - An unknown field in routeLookupConfig proto should not fail.
		// - lookupServiceTimeout is set to its default value, since it is not specified in the input.
		// - maxAge is set to maxMaxAge since the value is too large in the input.
		// - staleAge is ignore because it is higher than maxAge in the input.
		{
			desc: "with transformations",
			input: []byte(`{
				"top-level-unknown-field": "unknown-value",
				"routeLookupConfig": {
					"unknown-field": "unknown-value",
					"grpcKeybuilders": [{
						"names": [{"service": "service", "method": "method"}],
						"headers": [{"key": "k1", "names": ["v1"]}]
					}],
					"lookupService": ":///target",
					"maxAge" : "500s",
					"staleAge": "600s",
					"cacheSizeBytes": 1000,
					"defaultTarget": "passthrough:///default"
				},
				"childPolicy": [
					{"cds_experimental": {"Cluster": "my-fav-cluster"}},
					{"unknown-policy": {"unknown-field": "unknown-value"}},
					{"grpclb": {"childPolicy": [{"pickfirst": {}}]}}
				],
				"childPolicyConfigTargetFieldName": "service_name"
			}`),
			wantCfg: &lbConfig{
				lookupService:          ":///target",
				lookupServiceTimeout:   10 * time.Second, // This is the default value.
				maxAge:                 5 * time.Minute,  // This is max maxAge.
				staleAge:               time.Duration(0), // StaleAge is ignore because it was higher than maxAge.
				cacheSizeBytes:         1000,
				defaultTarget:          "passthrough:///default",
				childPolicyName:        "grpclb",
				childPolicyTargetField: "service_name",
				childPolicyConfig: map[string]json.RawMessage{
					"childPolicy":  json.RawMessage(`[{"pickfirst": {}}]`),
					"service_name": json.RawMessage(childPolicyTargetFieldVal),
				},
			},
		},
		{
			desc: "without transformations",
			input: []byte(`{
				"routeLookupConfig": {
					"grpcKeybuilders": [{
						"names": [{"service": "service", "method": "method"}],
						"headers": [{"key": "k1", "names": ["v1"]}]
					}],
					"lookupService": "target",
					"lookupServiceTimeout" : "100s",
					"maxAge": "60s",
					"staleAge" : "50s",
					"cacheSizeBytes": 1000,
					"defaultTarget": "passthrough:///default"
				},
				"childPolicy": [{"grpclb": {"childPolicy": [{"pickfirst": {}}]}}],
				"childPolicyConfigTargetFieldName": "service_name"
			}`),
			wantCfg: &lbConfig{
				lookupService:          "target",
				lookupServiceTimeout:   100 * time.Second,
				maxAge:                 60 * time.Second,
				staleAge:               50 * time.Second,
				cacheSizeBytes:         1000,
				defaultTarget:          "passthrough:///default",
				childPolicyName:        "grpclb",
				childPolicyTargetField: "service_name",
				childPolicyConfig: map[string]json.RawMessage{
					"childPolicy":  json.RawMessage(`[{"pickfirst": {}}]`),
					"service_name": json.RawMessage(childPolicyTargetFieldVal),
				},
			},
		},
	}

	builder := &rlsBB{}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			lbCfg, err := builder.ParseConfig(test.input)
			if err != nil || !testEqual(lbCfg.(*lbConfig), test.wantCfg) {
				t.Errorf("ParseConfig(%s) = {%+v, %v}, want {%+v, nil}", string(test.input), lbCfg, err, test.wantCfg)
			}
		})
	}
}

func TestParseConfigErrors(t *testing.T) {
	tests := []struct {
		desc    string
		input   []byte
		wantErr string
	}{
		{
			desc:    "empty input",
			input:   nil,
			wantErr: "rls: json unmarshal failed for service config",
		},
		{
			desc:    "bad json",
			input:   []byte(`bad bad json`),
			wantErr: "rls: json unmarshal failed for service config",
		},
		{
			desc: "bad grpcKeyBuilder",
			input: []byte(`{
					"routeLookupConfig": {
						"grpcKeybuilders": [{
							"names": [{"service": "service", "method": "method"}],
							"headers": [{"key": "k1", "requiredMatch": true, "names": ["v1"]}]
						}]
					}
				}`),
			wantErr: "rls: GrpcKeyBuilder in RouteLookupConfig has required_match field set",
		},
		{
			desc: "empty lookup service",
			input: []byte(`{
					"routeLookupConfig": {
						"grpcKeybuilders": [{
							"names": [{"service": "service", "method": "method"}],
							"headers": [{"key": "k1", "names": ["v1"]}]
						}]
					}
				}`),
			wantErr: "rls: empty lookup_service in route lookup config",
		},
		{
			desc: "unregistered scheme in lookup service URI",
			input: []byte(`{
					"routeLookupConfig": {
						"grpcKeybuilders": [{
							"names": [{"service": "service", "method": "method"}],
							"headers": [{"key": "k1", "names": ["v1"]}]
						}],
						"lookupService": "badScheme:///target"
					}
				}`),
			wantErr: "rls: unregistered scheme in lookup_service",
		},
		{
			desc: "invalid lookup service timeout",
			input: []byte(`{
				"routeLookupConfig": {
					"grpcKeybuilders": [{
						"names": [{"service": "service", "method": "method"}],
						"headers": [{"key": "k1", "names": ["v1"]}]
					}],
					"lookupService": "passthrough:///target",
					"lookupServiceTimeout" : "315576000001s"
				}
			}`),
			wantErr: "bad Duration: time: invalid duration",
		},
		{
			desc: "invalid max age",
			input: []byte(`{
				"routeLookupConfig": {
					"grpcKeybuilders": [{
						"names": [{"service": "service", "method": "method"}],
						"headers": [{"key": "k1", "names": ["v1"]}]
					}],
					"lookupService": "passthrough:///target",
					"lookupServiceTimeout" : "10s",
					"maxAge" : "315576000001s"
				}
			}`),
			wantErr: "bad Duration: time: invalid duration",
		},
		{
			desc: "invalid stale age",
			input: []byte(`{
				"routeLookupConfig": {
					"grpcKeybuilders": [{
						"names": [{"service": "service", "method": "method"}],
						"headers": [{"key": "k1", "names": ["v1"]}]
					}],
					"lookupService": "passthrough:///target",
					"lookupServiceTimeout" : "10s",
					"maxAge" : "10s",
					"staleAge" : "315576000001s"
				}
			}`),
			wantErr: "bad Duration: time: invalid duration",
		},
		{
			desc: "invalid max age stale age combo",
			input: []byte(`{
				"routeLookupConfig": {
					"grpcKeybuilders": [{
						"names": [{"service": "service", "method": "method"}],
						"headers": [{"key": "k1", "names": ["v1"]}]
					}],
					"lookupService": "passthrough:///target",
					"lookupServiceTimeout" : "10s",
					"staleAge" : "10s"
				}
			}`),
			wantErr: "rls: stale_age is set, but max_age is not in route lookup config",
		},
		{
			desc: "invalid cache size",
			input: []byte(`{
				"routeLookupConfig": {
					"grpcKeybuilders": [{
						"names": [{"service": "service", "method": "method"}],
						"headers": [{"key": "k1", "names": ["v1"]}]
					}],
					"lookupService": "passthrough:///target",
					"lookupServiceTimeout" : "10s",
					"maxAge": "30s",
					"staleAge" : "25s"
				}
			}`),
			wantErr: "rls: cache_size_bytes must be greater than 0 in route lookup config",
		},
		{
			desc: "no child policy",
			input: []byte(`{
				"routeLookupConfig": {
					"grpcKeybuilders": [{
						"names": [{"service": "service", "method": "method"}],
						"headers": [{"key": "k1", "names": ["v1"]}]
					}],
					"lookupService": "passthrough:///target",
					"lookupServiceTimeout" : "10s",
					"maxAge": "30s",
					"staleAge" : "25s",
					"cacheSizeBytes": 1000,
					"defaultTarget": "passthrough:///default"
				},
				"childPolicyConfigTargetFieldName": "service_name"
			}`),
			wantErr: "rls: invalid childPolicy config: no supported policies found",
		},
		{
			desc: "no known child policy",
			input: []byte(`{
				"routeLookupConfig": {
					"grpcKeybuilders": [{
						"names": [{"service": "service", "method": "method"}],
						"headers": [{"key": "k1", "names": ["v1"]}]
					}],
					"lookupService": "passthrough:///target",
					"lookupServiceTimeout" : "10s",
					"maxAge": "30s",
					"staleAge" : "25s",
					"cacheSizeBytes": 1000,
					"defaultTarget": "passthrough:///default"
				},
				"childPolicy": [
					{"cds_experimental": {"Cluster": "my-fav-cluster"}},
					{"unknown-policy": {"unknown-field": "unknown-value"}}
				],
				"childPolicyConfigTargetFieldName": "service_name"
			}`),
			wantErr: "rls: invalid childPolicy config: no supported policies found",
		},
		{
			desc: "invalid child policy config - more than one entry in map",
			input: []byte(`{
				"routeLookupConfig": {
					"grpcKeybuilders": [{
						"names": [{"service": "service", "method": "method"}],
						"headers": [{"key": "k1", "names": ["v1"]}]
					}],
					"lookupService": "passthrough:///target",
					"lookupServiceTimeout" : "10s",
					"maxAge": "30s",
					"staleAge" : "25s",
					"cacheSizeBytes": 1000,
					"defaultTarget": "passthrough:///default"
				},
				"childPolicy": [
					{
						"cds_experimental": {"Cluster": "my-fav-cluster"},
						"unknown-policy": {"unknown-field": "unknown-value"}
					}
				],
				"childPolicyConfigTargetFieldName": "service_name"
			}`),
			wantErr: "does not contain exactly 1 policy/config pair",
		},
		{
			desc: "no childPolicyConfigTargetFieldName",
			input: []byte(`{
				"routeLookupConfig": {
					"grpcKeybuilders": [{
						"names": [{"service": "service", "method": "method"}],
						"headers": [{"key": "k1", "names": ["v1"]}]
					}],
					"lookupService": "passthrough:///target",
					"lookupServiceTimeout" : "10s",
					"maxAge": "30s",
					"staleAge" : "25s",
					"cacheSizeBytes": 1000,
					"defaultTarget": "passthrough:///default"
				},
				"childPolicy": [
					{"cds_experimental": {"Cluster": "my-fav-cluster"}},
					{"unknown-policy": {"unknown-field": "unknown-value"}},
					{"grpclb": {}}
				]
			}`),
			wantErr: "rls: childPolicyConfigTargetFieldName field is not set in service config",
		},
		{
			desc: "child policy config validation failure",
			input: []byte(`{
				"routeLookupConfig": {
					"grpcKeybuilders": [{
						"names": [{"service": "service", "method": "method"}],
						"headers": [{"key": "k1", "names": ["v1"]}]
					}],
					"lookupService": "passthrough:///target",
					"lookupServiceTimeout" : "10s",
					"maxAge": "30s",
					"staleAge" : "25s",
					"cacheSizeBytes": 1000,
					"defaultTarget": "passthrough:///default"
				},
				"childPolicy": [
					{"cds_experimental": {"Cluster": "my-fav-cluster"}},
					{"unknown-policy": {"unknown-field": "unknown-value"}},
					{"grpclb": {"childPolicy": "not-an-array"}}
				],
				"childPolicyConfigTargetFieldName": "service_name"
			}`),
			wantErr: "rls: childPolicy config validation failed",
		},
	}

	builder := &rlsBB{}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			lbCfg, err := builder.ParseConfig(test.input)
			if lbCfg != nil || !strings.Contains(fmt.Sprint(err), test.wantErr) {
				t.Errorf("ParseConfig(%s) = {%+v, %v}, want {nil, %s}", string(test.input), lbCfg, err, test.wantErr)
			}
		})
	}
}
