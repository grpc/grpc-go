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
	"strings"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"

	rlspb "google.golang.org/grpc/balancer/rls/internal/proto/grpc_lookup_v1"
)

// Equal is used by cmp.Equal to check equality.
func (lb *lbParsedConfig) Equal(other *lbParsedConfig) bool {
	switch {
	case lb == nil && other != nil:
		return false
	case lb != nil && other == nil:
		return false
	case lb == nil && other == nil:
		return true
	}
	return proto.Equal(lb.rlsProto, other.rlsProto) &&
		cmp.Equal(lb.childPolicy, other.childPolicy) &&
		lb.targetField == other.targetField
}

func TestLBConfigUnmarshalJSON(t *testing.T) {
	tests := []struct {
		desc    string
		input   []byte
		wantCfg *lbParsedConfig
	}{
		{
			desc: "all-good-lbCfg",
			// This input validates a few cases:
			// - A top-level unknown field should not fail.
			// - An unknown field in routeLookupConfig proto should not fail.
			// - When the child_policy list contains known and unknown child
			//   policies, we should end up picking the first one that we know.
			input: []byte(`{
				"top-level-unknown-field": "unknown-value",
				"routeLookupConfig": {
					"unknown-field": "unknown-value",
					"cacheSizeBytes": 1000,
					"defaultTarget": "foobar"
				},
				"childPolicy": [
					{"cds_experimental": {"Cluster": "my-fav-cluster"}},
					{"grpclb": {}},
					{"unknown-policy": {"unknown-field": "unknown-value"}}
				],
				"childPolicyConfigTargetFieldName": "serviceName"
			}`),
			wantCfg: &lbParsedConfig{
				rlsProto: &rlspb.RouteLookupConfig{
					CacheSizeBytes: 1000,
					DefaultTarget:  "foobar",
				},
				childPolicy: &loadBalancingConfig{
					Name:   "grpclb",
					Config: json.RawMessage(`{}`),
				},
				targetField: "serviceName",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			lbCfg := &lbParsedConfig{}
			err := lbCfg.UnmarshalJSON(test.input)
			if err != nil || !cmp.Equal(lbCfg, test.wantCfg) {
				t.Errorf("lbCfg.UnmarshalJSON(%s) = {%+v, %v} want {%+v, %v}", string(test.input), lbCfg, err, test.wantCfg, nil)
			}
		})
	}
}

func TestLBConfigUnmarshalJSONErrors(t *testing.T) {
	tests := []struct {
		desc          string
		input         []byte
		wantErrPrefix string
	}{
		{
			desc:          "empty input",
			input:         nil,
			wantErrPrefix: "bad input json data",
		},
		{
			desc:          "bad JSON",
			input:         []byte(`bad bad json`),
			wantErrPrefix: "bad input json data",
		},
		{
			desc: "bad RLS proto",
			input: []byte(`{
				"routeLookupConfig": "not-a-proto"
			}`),
			wantErrPrefix: "bad routeLookupConfig proto",
		},
		{
			desc: "bad child policy config",
			input: []byte(`{
				"childPolicy": "not-a-valid-loadbalancing-config"
			}`),
			wantErrPrefix: "bad childPolicy config",
		},
		{
			desc: "child policy config not an array",
			input: []byte(`{
				"childPolicy": {
					"cds_experimental":{
						"Cluster": "my-fav-cluster"
					}
				}
			}`),
			wantErrPrefix: "bad childPolicy config",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			lbCfg := &lbParsedConfig{}
			if err := lbCfg.UnmarshalJSON(test.input); err == nil || !strings.HasPrefix(err.Error(), test.wantErrPrefix) {
				t.Errorf("lbCfg.UnmarshalJSON(%s) = %v, want %q", string(test.input), err, test.wantErrPrefix)
			}
			if !cmp.Equal(lbCfg, &lbParsedConfig{}) {
				t.Errorf("lbCfg.UnmarshalJSON(%s) change lbCfg object to: {%+v}, wanted unchanged", string(test.input), lbCfg)
			}
		})
	}
}
