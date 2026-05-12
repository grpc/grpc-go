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

// Package httpfilter contains interface definitions for xDS-based HTTP filters
// and a registry for filter builders.
package httpfilter

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/metadata"

	v3mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
)

// HeaderMutationRules specifies the rules for what modifications an external
// processing server may make to headers sent on the data plane RPC.
type HeaderMutationRules struct {
	// AllowExpr specifies a regular expression that matches the headers that can
	// be mutated.
	AllowExpr *regexp.Regexp
	// DisallowExpr specifies a regular expression that matches the headers that
	// cannot be mutated. This overrides the above allowExpr if a header matches
	// both.
	DisallowExpr *regexp.Regexp
	// DisallowAll specifies that no header mutations are allowed. This overrides
	// all other settings.
	DisallowAll bool
	// DisallowIsError specifies whether to return an error if a header mutation
	// is disallowed. If true, the data plane RPC will be failed with a grpc
	// status code of Unknown.
	DisallowIsError bool
}

// ServerConfig contains the configuration for an external server.
type ServerConfig struct {
	// TargetURI is the name of the external server.
	TargetURI string
	// ChannelCredentials specifies the transport credentials to use to connect to
	// the external server. Must not be nil.
	ChannelCredentials json.RawMessage
	// CallCredentials specifies the per-RPC credentials to use when making calls
	// to the external server.
	CallCredentials []json.RawMessage
	// Timeout is the RPC Timeout for the call to the external server. If unset,
	// the Timeout depends on the usage of this external server. For example,
	// cases like ext_authz and ext_proc, where there is a 1:1 mapping between the
	// data plane RPC and the external server call, the Timeout will be capped by
	// the Timeout on the data plane RPC. For cases like RLQS where there is a
	// side channel to the external server, an unset Timeout will result in no
	// Timeout being applied to the external server call.
	Timeout time.Duration
	// InitialMetadata is the additional metadata to include in all RPCs sent to
	// the external server.
	InitialMetadata metadata.MD
}

// ConvertStringMatchers converts a slice of protobuf StringMatcher messages to
// a slice of matcher.StringMatcher.
func ConvertStringMatchers(patterns []*v3matcherpb.StringMatcher) ([]matcher.StringMatcher, error) {
	matchers := make([]matcher.StringMatcher, 0, len(patterns))
	for _, p := range patterns {
		sm, err := matcher.StringMatcherFromProto(p)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, sm)
	}
	return matchers, nil
}

// HeaderMutationRulesFromProto converts a protobuf HeaderMutationRules message
// to a headerMutationRules struct.
func HeaderMutationRulesFromProto(mr *v3mutationpb.HeaderMutationRules) (HeaderMutationRules, error) {
	var rules HeaderMutationRules
	if mr == nil {
		return rules, nil
	}
	if allowExpr := mr.GetAllowExpression(); allowExpr != nil {
		re, err := regexp.Compile(allowExpr.GetRegex())
		if err != nil {
			return rules, fmt.Errorf("extproc: %v", err)
		}
		rules.AllowExpr = re
	}
	if disallowExpr := mr.GetDisallowExpression(); disallowExpr != nil {
		re, err := regexp.Compile(disallowExpr.GetRegex())
		if err != nil {
			return rules, fmt.Errorf("extproc: %v", err)
		}
		rules.DisallowExpr = re
	}
	rules.DisallowAll = mr.GetDisallowAll().GetValue()
	rules.DisallowIsError = mr.GetDisallowIsError().GetValue()
	return rules, nil
}
