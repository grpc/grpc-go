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

// Package matcher contains matchers that need to be shared by the routing on
// client side and routing on server side.
package matcher

import (
	"fmt"
	"strings"

	"google.golang.org/grpc/internal/grpcrand"
	"google.golang.org/grpc/internal/grpcutil"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/xds/internal/xdsclient"
)

func RouteToMatcher(r *xdsclient.Route) (*CompositeMatcher, error) {
	var pm pathMatcher
	switch {
	case r.Regex != nil:
		pm = newPathRegexMatcher(r.Regex)
	case r.Path != nil:
		pm = newPathExactMatcher(*r.Path, r.CaseInsensitive)
	case r.Prefix != nil:
		pm = newPathPrefixMatcher(*r.Prefix, r.CaseInsensitive)
	default:
		return nil, fmt.Errorf("illegal route: missing path_matcher")
	}

	var headerMatchers []matcher.HeaderMatcher
	for _, h := range r.Headers {
		var matcherT matcher.HeaderMatcher
		switch {
		case h.ExactMatch != nil && *h.ExactMatch != "":
			matcherT = matcher.NewHeaderExactMatcher(h.Name, *h.ExactMatch)
		case h.RegexMatch != nil:
			matcherT = matcher.NewHeaderRegexMatcher(h.Name, h.RegexMatch)
		case h.PrefixMatch != nil && *h.PrefixMatch != "":
			matcherT = matcher.NewHeaderPrefixMatcher(h.Name, *h.PrefixMatch)
		case h.SuffixMatch != nil && *h.SuffixMatch != "":
			matcherT = matcher.NewHeaderSuffixMatcher(h.Name, *h.SuffixMatch)
		case h.RangeMatch != nil:
			matcherT = matcher.NewHeaderRangeMatcher(h.Name, h.RangeMatch.Start, h.RangeMatch.End)
		case h.PresentMatch != nil:
			matcherT = matcher.NewHeaderPresentMatcher(h.Name, *h.PresentMatch)
		default:
			return nil, fmt.Errorf("illegal route: missing header_match_specifier")
		}
		if h.InvertMatch != nil && *h.InvertMatch {
			matcherT = matcher.NewInvertMatcher(matcherT)
		}
		headerMatchers = append(headerMatchers, matcherT)
	}

	var fractionMatcher *fractionMatcher
	if r.Fraction != nil {
		fractionMatcher = newFractionMatcher(*r.Fraction)
	}
	return newCompositeMatcher(pm, headerMatchers, fractionMatcher), nil
}

// So...just like in RBAC Matcher,
// This patches incoming RPC's to different types of routes, just like policies etc.

// If it's the same Route stuff as on client side, why not just reuse this xds/internal/resolver internal representation/implementation?

// CompositeMatcher.match returns true if all matchers return true.
type CompositeMatcher struct {
	pm  pathMatcher
	hms []matcher.HeaderMatcher
	fm  *fractionMatcher
}

func newCompositeMatcher(pm pathMatcher, hms []matcher.HeaderMatcher, fm *fractionMatcher) *CompositeMatcher {
	return &CompositeMatcher{pm: pm, hms: hms, fm: fm}
}

func (a *CompositeMatcher) Match(info iresolver.RPCInfo) bool {
	if a.pm != nil && !a.pm.match(info.Method) {
		return false
	}

	// Call headerMatchers even if md is nil, because routes may match
	// non-presence of some headers.
	var md metadata.MD
	if info.Context != nil {
		md, _ = metadata.FromOutgoingContext(info.Context)
		if extraMD, ok := grpcutil.ExtraMetadata(info.Context); ok {
			md = metadata.Join(md, extraMD)
			// Remove all binary headers. They are hard to match with. May need
			// to add back if asked by users.
			for k := range md {
				if strings.HasSuffix(k, "-bin") {
					delete(md, k)
				}
			}
		}
	}
	for _, m := range a.hms {
		if !m.Match(md) {
			return false
		}
	}

	if a.fm != nil && !a.fm.match() {
		return false
	}
	return true
}

func (a *CompositeMatcher) String() string {
	var ret string
	if a.pm != nil {
		ret += a.pm.String()
	}
	for _, m := range a.hms {
		ret += m.String()
	}
	if a.fm != nil {
		ret += a.fm.String()
	}
	return ret
}

type fractionMatcher struct {
	fraction int64 // real fraction is fraction/1,000,000.
}

func newFractionMatcher(fraction uint32) *fractionMatcher {
	return &fractionMatcher{fraction: int64(fraction)}
}

var grpcrandInt63n = grpcrand.Int63n

func (fm *fractionMatcher) match() bool {
	t := grpcrandInt63n(1000000)
	return t <= fm.fraction
}

func (fm *fractionMatcher) String() string {
	return fmt.Sprintf("fraction:%v", fm.fraction)
}
