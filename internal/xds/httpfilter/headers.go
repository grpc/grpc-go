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

// maxHeaderLen is the maximum allowed length, in bytes, of a header key or
// value, per gRFC A102.
const maxHeaderLen = 16384

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

// isDisallowedHeader reports whether a mutation on this header is
// unconditionally invalid, regardless of the configured mutation rules, per
// gRFC A102's HeaderValue validation: an empty, over-length, non-lower-case,
// "grpc-"-prefixed, ":"-prefixed, or "host" key, or an over-length value.
func isDisallowedHeader(h *v3corepb.HeaderValue) bool {
	key := h.GetKey()
	switch {
	case key == "", len(key) > maxHeaderLen:
		return true
	case key != strings.ToLower(key):
		return true
	case strings.HasPrefix(key, ":"), key == "host", strings.HasPrefix(key, "grpc-"):
		return true
	case len(h.GetValue()) > maxHeaderLen, len(h.GetRawValue()) > maxHeaderLen:
		return true
	}
	return false
}

// isRuleAllowed reports whether the configured mutation rules permit mutating
// key. A key matching disallow_expression is rejected; a key matching
// allow_expression is permitted (even under disallow_all); otherwise it is
// permitted unless disallow_all is set (gRFC A102).
func (m *HeaderMutator) isRuleAllowed(key string) bool {
	if m.rules.DisallowExpr != nil && m.rules.DisallowExpr.MatchString(key) {
		return false
	}
	if m.rules.AllowExpr != nil && m.rules.AllowExpr.MatchString(key) {
		return true
	}
	return !m.rules.DisallowAll
}

// Mutate applies HeaderValueOption mutations and returns the result, leaving
// the original metadata untouched. The metadata is copied lazily (copy-on-
// write): a call that applies no mutations returns the input as-is without
// allocating a copy.
//
// A mutation is applied only if the header is valid and permitted by the
// mutation rules. A disallowed mutation fails the call with an error when
// disallow_is_error is set, and is silently ignored otherwise (gRFC A102).
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
		key := h.GetKey()
		// Per gRFC A102, raw_value takes precedence over the legacy value
		// field. A non-nil raw_value (including an explicitly empty one) is
		// used; only an unset raw_value falls back to value.
		val := h.GetValue()
		if h.GetRawValue() != nil {
			val = string(h.GetRawValue())
		}

		if isDisallowedHeader(h) || !m.isRuleAllowed(key) {
			if m.rules.DisallowIsError {
				return nil, fmt.Errorf("httpfilter: header mutation for key %q is disallowed by mutation rules", key)
			}
			continue
		}

		// key is validated lower-case and, after ensureCopy, res is a non-nil
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
		// empty value; keep_empty_value is intentionally unsupported.
	}
	return res, nil
}
