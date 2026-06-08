/*
 *
 * Copyright 2026 gRPC authors.
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
	"strings"
	"time"

	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/metadata"

	v3mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
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
			return rules, fmt.Errorf("httpfilter: %v", err)
		}
		rules.AllowExpr = re
	}
	if disallowExpr := mr.GetDisallowExpression(); disallowExpr != nil {
		re, err := regexp.Compile(disallowExpr.GetRegex())
		if err != nil {
			return rules, fmt.Errorf("httpfilter: %v", err)
		}
		rules.DisallowExpr = re
	}
	rules.DisallowAll = mr.GetDisallowAll().GetValue()
	rules.DisallowIsError = mr.GetDisallowIsError().GetValue()
	return rules, nil
}

// ApplyAdditions takes a set of header mutations (for additions and
// modifications) received from an external server and applies them to the
// provided metadata, subject to the rules defined in hmr.
//
// If the DisallowAll field is true, no mutations are performed, and the input
// metadata is returned unmodified.
//
// It iterates through each header mutation, performs validation on the header
// key and value, and checks if the mutation is permitted by the AllowExpr and
// DisallowExpr regular expressions.
//
// The following headers are always ignored:
// - Pseudo-headers (keys starting with ':').
// - The 'host' header.
// - Headers with non-lowercase keys.
// - Headers with keys or values exceeding 16384 bytes.
//
// If a mutation is disallowed and DisallowIsError is true, an error is
// returned. Otherwise, the disallowed mutation is silently ignored.
//
// The input metadata must not be nil.
func (hmr *HeaderMutationRules) ApplyAdditions(hvos []*v3corepb.HeaderValueOption, input metadata.MD) error {
	if hmr == nil {
		hmr = &HeaderMutationRules{}
	}

	if hmr.DisallowAll {
		return nil
	}

	for _, hvo := range hvos {
		header := hvo.GetHeader()
		key := header.GetKey()
		if len(key) == 0 || key[0] == ':' || key == "host" || key != strings.ToLower(key) || len(key) > 16384 {
			continue
		}

		value := header.GetValue()
		if strings.HasSuffix(key, "-bin") {
			value = string(header.GetRawValue())
		}
		if len(value) > 16384 {
			continue
		}

		if !hmr.allow(key) {
			if hmr.DisallowIsError {
				return fmt.Errorf("extauthz: header mutation disallowed by headerMutationRules for header key %q", key)
			}
			continue
		}

		// Perform the mutation on output metadata using the append_action
		// field from the header value option.
		switch hvo.GetAppendAction() {
		case v3corepb.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD:
			input.Append(key, value)
		case v3corepb.HeaderValueOption_ADD_IF_ABSENT:
			if input.Get(key) == nil {
				input.Set(key, value)
			}
		case v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD:
			input.Set(key, value)
		case v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS:
			if input.Get(key) != nil {
				input.Set(key, value)
			}
		}
	}
	return nil
}

// ApplyRemovals takes a set of headers (for removal) received from an
// authorization server and applies them to the provided metadata, subject to
// the rules defined in hmr.
//
// This method is very similar to ApplyAdditions, except that headers are
// removed here instead of added or mutated as is the case in the latter. See
// ApplyAdditions for more details.
//
// The input metadata must not be nil.
func (hmr *HeaderMutationRules) ApplyRemovals(headersToRemove []string, input metadata.MD) error {
	if hmr == nil {
		hmr = &HeaderMutationRules{}
	}

	if hmr.DisallowAll {
		return nil
	}

	for _, header := range headersToRemove {
		if len(header) == 0 || header[0] == ':' || header == "host" || header != strings.ToLower(header) || len(header) > 16384 {
			continue
		}
		if !hmr.allow(header) {
			if hmr.DisallowIsError {
				return fmt.Errorf("extauthz: header mutation disallowed by headerMutationRules for header %q", header)
			}
			continue
		}
		input.Delete(header)
	}
	return nil
}

func (hmr *HeaderMutationRules) allow(key string) bool {
	if hmr.DisallowExpr != nil && hmr.DisallowExpr.MatchString(key) {
		return false
	}
	if hmr.AllowExpr != nil && hmr.AllowExpr.MatchString(key) {
		return true
	}
	if hmr.AllowExpr != nil {
		return false
	}
	return true
}

// ConstructHeaderMap constructs a HeaderMap from the given metadata, using the
// following rules:
//   - if the header is matched by the disallowed_headers config field, it will
//     not be added to the map, otherwise,
//   - if the allowed_headers config field is unset or matches the header, the
//     header will be added to the map, otherwise,
//   - the header will be excluded from the map.
func ConstructHeaderMap(md metadata.MD, allowedHeaders, disallowedHeaders []matcher.StringMatcher) *v3corepb.HeaderMap {
	headerMap := &v3corepb.HeaderMap{}
	for key, values := range md {
		if IsDisallowedHeader(key, disallowedHeaders) {
			continue
		}
		if IsAllowedHeader(key, allowedHeaders) {
			for _, value := range values {
				headerMap.Headers = append(headerMap.Headers, &v3corepb.HeaderValue{
					Key:      key,
					RawValue: []byte(value),
				})
			}
		}
	}
	if len(headerMap.Headers) == 0 {
		return nil
	}
	return headerMap
}

// IsDisallowedHeader returns true if the given header key matches any of the
// provided disallowed header matchers.
func IsDisallowedHeader(key string, matchers []matcher.StringMatcher) bool {
	for _, m := range matchers {
		if m.Match(key) {
			return true
		}
	}
	return false
}

// IsAllowedHeader returns true if the allowed header matchers list is empty,
// or if the given header key matches any of the provided allowed header
// matchers.
func IsAllowedHeader(key string, matchers []matcher.StringMatcher) bool {
	if len(matchers) == 0 {
		return true
	}
	for _, m := range matchers {
		if m.Match(key) {
			return true
		}
	}
	return false
}
