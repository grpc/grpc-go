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

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/grpcrand"
)

type matcher interface {
	match(info balancer.PickInfo) bool
	Equal(matcher) bool
	String() string
}

type fractionMatcher struct {
	fraction int64 // real fraction is fraction/1,000,000.
}

func newFractionMatcher(fraction uint32) *fractionMatcher {
	return &fractionMatcher{fraction: int64(fraction)}
}

var grpcrandInt63n = grpcrand.Int63n

func (fm *fractionMatcher) match(balancer.PickInfo) bool {
	t := grpcrandInt63n(1000000)
	return t <= fm.fraction
}

func (fm *fractionMatcher) Equal(m matcher) bool {
	mm, ok := m.(*fractionMatcher)
	if !ok {
		return false
	}
	return fm.fraction == mm.fraction
}

func (fm *fractionMatcher) String() string {
	return fmt.Sprintf("fraction:%v", fm.fraction)
}

// andMatcher.match returns true if all matchers return true.
type andMatcher struct {
	matchers []matcher
}

func newAndMatcher(matchers []matcher) *andMatcher {
	return &andMatcher{matchers: matchers}
}

func (a *andMatcher) match(info balancer.PickInfo) bool {
	for _, m := range a.matchers {
		if !m.match(info) {
			return false
		}
	}
	return true
}

func (a *andMatcher) Equal(m matcher) bool {
	mm, ok := m.(*andMatcher)
	if !ok {
		return false
	}
	if len(a.matchers) != len(mm.matchers) {
		return false
	}
	for i := range a.matchers {
		if !a.matchers[i].Equal(mm.matchers[i]) {
			return false
		}
	}
	return true
}

func (a *andMatcher) String() string {
	var ret string
	for _, m := range a.matchers {
		ret += m.String()
	}
	return ret
}

type invertMatcher struct {
	m matcher
}

func newInvertMatcher(m matcher) *invertMatcher {
	return &invertMatcher{m: m}
}

func (i *invertMatcher) match(info balancer.PickInfo) bool {
	return !i.m.match(info)
}

func (i *invertMatcher) Equal(m matcher) bool {
	mm, ok := m.(*invertMatcher)
	if !ok {
		return false
	}
	return i.m.Equal(mm.m)
}

func (i *invertMatcher) String() string {
	return fmt.Sprintf("invert{%s}", i.m)
}
