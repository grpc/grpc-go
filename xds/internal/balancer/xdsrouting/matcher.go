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

package xdsrouting

import (
	"fmt"
	"strings"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/grpcrand"
	"google.golang.org/grpc/internal/grpcutil"
	"google.golang.org/grpc/metadata"
)

// compositeMatcher.match returns true if all matchers return true.
type compositeMatcher struct {
	pm  pathMatcherInterface
	hms []headerMatcherInterface
	fm  *fractionMatcher
}

func newCompositeMatcher(pm pathMatcherInterface, hms []headerMatcherInterface, fm *fractionMatcher) *compositeMatcher {
	return &compositeMatcher{pm: pm, hms: hms, fm: fm}
}

func (a *compositeMatcher) match(info balancer.PickInfo) bool {
	if a.pm != nil && !a.pm.match(info.FullMethodName) {
		return false
	}

	// Call headerMatchers even if md is nil, because routes may match
	// non-presence of some headers.
	var md metadata.MD
	if info.Ctx != nil {
		md, _ = metadata.FromOutgoingContext(info.Ctx)
		if extraMD, ok := grpcutil.ExtraMetadata(info.Ctx); ok {
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

func (a *compositeMatcher) equal(mm *compositeMatcher) bool {
	if a == mm {
		return true
	}

	if a == nil || mm == nil {
		return false
	}

	if (a.pm != nil || mm.pm != nil) && (a.pm == nil || !a.pm.equal(mm.pm)) {
		return false
	}

	if len(a.hms) != len(mm.hms) {
		return false
	}
	for i := range a.hms {
		if !a.hms[i].equal(mm.hms[i]) {
			return false
		}
	}

	if (a.fm != nil || mm.fm != nil) && (a.fm == nil || !a.fm.equal(mm.fm)) {
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

func (fm *fractionMatcher) equal(m *fractionMatcher) bool {
	if fm == m {
		return true
	}
	if fm == nil || m == nil {
		return false
	}

	return fm.fraction == m.fraction
}

func (fm *fractionMatcher) String() string {
	return fmt.Sprintf("fraction:%v", fm.fraction)
}
