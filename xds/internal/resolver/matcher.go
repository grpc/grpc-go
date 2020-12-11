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

package resolver

import (
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/grpc/internal/grpcrand"
	"google.golang.org/grpc/internal/grpcutil"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/metadata"
	xdsclient "google.golang.org/grpc/xds/internal/client"
)

func routeToMatcher(r *xdsclient.Route) (*compositeMatcher, error) {
	var pathMatcher pathMatcherInterface
	switch {
	case r.Regex != nil:
		re, err := regexp.Compile(*r.Regex)
		if err != nil {
			return nil, fmt.Errorf("failed to compile regex %q", *r.Regex)
		}
		pathMatcher = newPathRegexMatcher(re)
	case r.Path != nil:
		pathMatcher = newPathExactMatcher(*r.Path, r.CaseInsensitive)
	case r.Prefix != nil:
		pathMatcher = newPathPrefixMatcher(*r.Prefix, r.CaseInsensitive)
	default:
		return nil, fmt.Errorf("illegal route: missing path_matcher")
	}

	var headerMatchers []headerMatcherInterface
	for _, h := range r.Headers {
		var matcherT headerMatcherInterface
		switch {
		case h.ExactMatch != nil && *h.ExactMatch != "":
			matcherT = newHeaderExactMatcher(h.Name, *h.ExactMatch)
		case h.RegexMatch != nil && *h.RegexMatch != "":
			re, err := regexp.Compile(*h.RegexMatch)
			if err != nil {
				return nil, fmt.Errorf("failed to compile regex %q, skipping this matcher", *h.RegexMatch)
			}
			matcherT = newHeaderRegexMatcher(h.Name, re)
		case h.PrefixMatch != nil && *h.PrefixMatch != "":
			matcherT = newHeaderPrefixMatcher(h.Name, *h.PrefixMatch)
		case h.SuffixMatch != nil && *h.SuffixMatch != "":
			matcherT = newHeaderSuffixMatcher(h.Name, *h.SuffixMatch)
		case h.RangeMatch != nil:
			matcherT = newHeaderRangeMatcher(h.Name, h.RangeMatch.Start, h.RangeMatch.End)
		case h.PresentMatch != nil:
			matcherT = newHeaderPresentMatcher(h.Name, *h.PresentMatch)
		default:
			return nil, fmt.Errorf("illegal route: missing header_match_specifier")
		}
		if h.InvertMatch != nil && *h.InvertMatch {
			matcherT = newInvertMatcher(matcherT)
		}
		headerMatchers = append(headerMatchers, matcherT)
	}

	var fractionMatcher *fractionMatcher
	if r.Fraction != nil {
		fractionMatcher = newFractionMatcher(*r.Fraction)
	}
	return newCompositeMatcher(pathMatcher, headerMatchers, fractionMatcher), nil
}

// compositeMatcher.match returns true if all matchers return true.
type compositeMatcher struct {
	pm  pathMatcherInterface
	hms []headerMatcherInterface
	fm  *fractionMatcher
}

func newCompositeMatcher(pm pathMatcherInterface, hms []headerMatcherInterface, fm *fractionMatcher) *compositeMatcher {
	return &compositeMatcher{pm: pm, hms: hms, fm: fm}
}

func (a *compositeMatcher) match(info iresolver.RPCInfo) bool {
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
		if !m.match(md) {
			return false
		}
	}

	if a.fm != nil && !a.fm.match() {
		return false
	}
	return true
}

func (a *compositeMatcher) String() string {
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
