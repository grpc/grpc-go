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

package httpfilter

import (
	"fmt"
	"strings"

	v3mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/grpc/metadata"
)

// HeaderMutator compiles and applies mutations on gRPC metadata.
type HeaderMutator struct {
	rules HeaderMutationRules
}

// NewHeaderMutator creates a new compiled HeaderMutator.
func NewHeaderMutator(rules HeaderMutationRules) *HeaderMutator {
	return &HeaderMutator{rules: rules}
}

// NewHeaderMutatorFromProto compiles a HeaderMutator from the proto message.
func NewHeaderMutatorFromProto(mr *v3mutationpb.HeaderMutationRules) (*HeaderMutator, error) {
	rules, err := HeaderMutationRulesFromProto(mr)
	if err != nil {
		return nil, err
	}
	return &HeaderMutator{rules: rules}, nil
}

// isMutationAllowed checks if a specific header key mutation is allowed.
func (m *HeaderMutator) isMutationAllowed(key string) (bool, error) {
	// Per gRFC A102, gRPC never allows modifying pseudo-headers (keys starting
	// with ":") or the "host" header, regardless of the configured mutation
	// rules. Other headers (including "grpc-*") are subject to the
	// allow/disallow rules below.
	if strings.HasPrefix(key, ":") || key == "host" {
		return false, nil
	}

	if m.rules.DisallowAll {
		if m.rules.DisallowIsError {
			return false, fmt.Errorf("httpfilter: all header mutations are disallowed by mutation rules")
		}
		return false, nil
	}

	// DisallowExpr has higher priority and overrides AllowExpr.
	if m.rules.DisallowExpr != nil && m.rules.DisallowExpr.MatchString(key) {
		if m.rules.DisallowIsError {
			return false, fmt.Errorf("httpfilter: header mutation for key %q is disallowed by mutation rules", key)
		}
		return false, nil
	}

	if m.rules.AllowExpr != nil {
		if !m.rules.AllowExpr.MatchString(key) {
			if m.rules.DisallowIsError {
				return false, fmt.Errorf("httpfilter: header mutation for key %q is not allowed by mutation rules", key)
			}
			return false, nil
		}
	}

	return true, nil
}

// Mutate applies HeaderValueOption mutations and returns the result, leaving
// the original metadata untouched. The metadata is copied lazily (copy-on-
// write): a call that applies no mutations returns the input as-is without
// allocating a copy.
func (m *HeaderMutator) Mutate(md metadata.MD, mutations []*v3corepb.HeaderValueOption) (metadata.MD, error) {
	res := md
	copied := false
	// ensureCopy copies the metadata the first time an actual mutation is
	// applied, so reads before that observe the original and no allocation is
	// made when nothing changes.
	ensureCopy := func() {
		if !copied {
			res = md.Copy()
			copied = true
		}
	}

	for _, opt := range mutations {
		h := opt.GetHeader()
		if h == nil {
			continue
		}
		key := strings.ToLower(h.GetKey())
		// Per gRFC A102, raw_value takes precedence over the legacy value field.
		// A non-nil raw_value (including an explicitly empty one) is used; only
		// an unset raw_value falls back to value.
		val := h.GetValue()
		if h.GetRawValue() != nil {
			val = string(h.GetRawValue())
		}

		allowed, err := m.isMutationAllowed(key)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}

		// key is already lower-cased and, after ensureCopy, res is a non-nil
		// map whose value slices were deep-copied, so we operate on the map
		// directly to avoid the redundant strings.ToLower scans and the
		// variadic allocation in metadata.MD's Get/Set/Append helpers.
		switch opt.GetAppendAction() {
		case v3corepb.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD:
			ensureCopy()
			res[key] = append(res[key], val)
		case v3corepb.HeaderValueOption_ADD_IF_ABSENT:
			if len(res[key]) == 0 {
				ensureCopy()
				res[key] = []string{val}
			}
		case v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD:
			ensureCopy()
			res[key] = []string{val}
		case v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS:
			if len(res[key]) > 0 {
				ensureCopy()
				res[key] = []string{val}
			}
		}

		// Per gRFC A102, gRPC keeps headers even if a mutation results in an
		// empty value; the keep_empty_value field is intentionally unsupported.
	}
	return res, nil
}
