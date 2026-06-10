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
	// Standard system/envoy headers are ignored and not allowed to be mutated
	if strings.HasPrefix(key, ":") || strings.HasPrefix(key, "grpc-") {
		return false, nil
	}
	if key == "host" {
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

// Mutate applies HeaderValueOption mutations to the metadata.
// It returns a modified copy, leaving the original untouched.
func (m *HeaderMutator) Mutate(md metadata.MD, mutations []*v3corepb.HeaderValueOption) (metadata.MD, error) {
	res := md.Copy()

	for _, opt := range mutations {
		h := opt.GetHeader()
		if h == nil {
			continue
		}
		key := strings.ToLower(h.GetKey())
		val := h.GetValue()

		allowed, err := m.isMutationAllowed(key)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}

		action := opt.GetAppendAction()
		switch action {
		case v3corepb.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD:
			res.Append(key, val)
		case v3corepb.HeaderValueOption_ADD_IF_ABSENT:
			if len(res.Get(key)) == 0 {
				res.Set(key, val)
			}
		case v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD:
			res.Set(key, val)
		}

		// Any header mutation resulting in only empty values causes the key to
		// be removed.
		vals := res.Get(key)
		isEmpty := true
		for _, v := range vals {
			if v != "" {
				isEmpty = false
				break
			}
		}
		if isEmpty {
			delete(res, key)
		}
	}
	return res, nil
}
