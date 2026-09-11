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

// Package header converts xDS header matcher protos to internal matchers.
package header

import (
	"errors"
	"fmt"

	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"google.golang.org/grpc/internal/xds/matcher"
)

// FromProto creates a header matcher from the corresponding proto.
func FromProto(matcherProto *v3routepb.HeaderMatcher) (matcher.HeaderMatcher, error) {
	if matcherProto == nil {
		return nil, errors.New("input HeaderMatcher proto is nil")
	}

	name := matcherProto.GetName()
	invert := matcherProto.GetInvertMatch()
	switch m := matcherProto.GetHeaderMatchSpecifier().(type) {
	case *v3routepb.HeaderMatcher_ExactMatch:
		if m == nil {
			return nil, errors.New("exact header matcher is nil")
		}
		return matcher.NewHeaderExactMatcher(name, m.ExactMatch, invert), nil
	case *v3routepb.HeaderMatcher_SafeRegexMatch:
		if m == nil || m.SafeRegexMatch == nil {
			return nil, errors.New("safe regex header matcher is nil")
		}
		re, err := matcher.CompileSafeRegex(m.SafeRegexMatch.GetRegex())
		if err != nil {
			return nil, fmt.Errorf("safe regex header matcher %q is invalid: %v", m.SafeRegexMatch.GetRegex(), err)
		}
		return matcher.NewHeaderRegexMatcher(name, re, invert), nil
	case *v3routepb.HeaderMatcher_RangeMatch:
		if m == nil || m.RangeMatch == nil {
			return nil, errors.New("range header matcher is nil")
		}
		return matcher.NewHeaderRangeMatcher(name, m.RangeMatch.GetStart(), m.RangeMatch.GetEnd(), invert), nil
	case *v3routepb.HeaderMatcher_PresentMatch:
		if m == nil {
			return nil, errors.New("present header matcher is nil")
		}
		return matcher.NewHeaderPresentMatcher(name, m.PresentMatch, invert), nil
	case *v3routepb.HeaderMatcher_PrefixMatch:
		if m == nil {
			return nil, errors.New("prefix header matcher is nil")
		}
		if m.PrefixMatch == "" {
			return nil, errors.New("empty prefix is not allowed in HeaderMatcher")
		}
		return matcher.NewHeaderPrefixMatcher(name, m.PrefixMatch, invert), nil
	case *v3routepb.HeaderMatcher_SuffixMatch:
		if m == nil {
			return nil, errors.New("suffix header matcher is nil")
		}
		if m.SuffixMatch == "" {
			return nil, errors.New("empty suffix is not allowed in HeaderMatcher")
		}
		return matcher.NewHeaderSuffixMatcher(name, m.SuffixMatch, invert), nil
	case *v3routepb.HeaderMatcher_ContainsMatch:
		if m == nil {
			return nil, errors.New("contains header matcher is nil")
		}
		if m.ContainsMatch == "" {
			return nil, errors.New("empty contains is not allowed in HeaderMatcher")
		}
		return matcher.NewHeaderContainsMatcher(name, m.ContainsMatch, invert), nil
	case *v3routepb.HeaderMatcher_StringMatch:
		if m == nil || m.StringMatch == nil {
			return nil, errors.New("string header matcher is nil")
		}
		sm, err := matcher.StringMatcherFromProto(m.StringMatch)
		if err != nil {
			return nil, fmt.Errorf("string header matcher is invalid: %v", err)
		}
		return matcher.NewHeaderStringMatcher(name, sm, invert), nil
	default:
		return nil, errors.New("header matcher type is not set")
	}
}
