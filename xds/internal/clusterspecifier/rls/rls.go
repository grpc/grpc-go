/*
 *
 * Copyright 2021 gRPC authors.
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

// Package rls implements the RLS cluster specifier plugin.
package rls

import (
	"encoding/json"
	"fmt"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"google.golang.org/grpc/xds/internal/clusterspecifier"
	"google.golang.org/protobuf/types/known/anypb"
)

func init() {
	clusterspecifier.Register(rls{})
}

type rls struct{}

func (rls) TypeURLs() []string {
	return []string{"google.golang.org/grpc/xds/internal/clusterspecifier/rls/RouteLookupClusterSpecifier"}
}

// Same as balancer/rls/internal.lbConfigJSON
type lbConfigJSON struct {
	RouteLookupConfig                json.RawMessage
	ChildPolicy                      []map[string]json.RawMessage
	ChildPolicyConfigTargetFieldName string
}

func (rls) ParseClusterSpecifierConfig(cfg proto.Message) (clusterspecifier.BalancerConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("rls_csp: nil configuration message provided")
	}
	any, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("rls_csp: error parsing config %v: unknown type %T", cfg, cfg)
	}
	rlcs := new(RouteLookupClusterSpecifier)

	if err := ptypes.UnmarshalAny(any, rlcs); err != nil {
		return nil, fmt.Errorf("rls_csp: error parsing config %v: %v", cfg, err)
	}
	rlcJSON, err := json.Marshal(rlcs.GetRouteLookupConfig())
	if err != nil {
		return nil, fmt.Errorf("rls_csp: error marshaling config: %v: %v", rlcs.GetRouteLookupConfig(), err)
	}

	lbCfgJSON := &lbConfigJSON{
		RouteLookupConfig: rlcJSON,
		ChildPolicy: []map[string]json.RawMessage{
			{
				"cds_experimental": {},
			},
		},
		ChildPolicyConfigTargetFieldName: "cluster",
	}

	rawJSON, err := json.Marshal(lbCfgJSON)
	if err != nil {
		return nil, fmt.Errorf("rls_csp: error marshaling config %v: %v", lbCfgJSON, err)
	}

	// _, err := rls.ParseConfig(rawJSON) (will this function need to change its logic at all?)
	// if err != nil {
	// return nil, fmt.Errorf("error: %v", err)
	// }

	cfgRet := clusterspecifier.BalancerConfig{}

	err = json.Unmarshal(rawJSON, cfgRet)
	if err != nil {
		return nil, fmt.Errorf("error: %v", err)
	}

	return cfgRet, nil
}
