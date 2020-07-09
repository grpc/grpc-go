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
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/metadata"
)

func mdValuesFromOutgoingCtx(ctx context.Context, key string) ([]string, bool) {
	if ctx == nil {
		return nil, false
	}
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return nil, false
	}
	vs, ok := md[key]
	return vs, ok
}

type headerExactMatcher struct {
	key   string
	exact string
}

func newHeaderExactMatcher(key, exact string) *headerExactMatcher {
	return &headerExactMatcher{key: key, exact: exact}
}

func (hem *headerExactMatcher) match(info balancer.PickInfo) bool {
	vs, ok := mdValuesFromOutgoingCtx(info.Ctx, hem.key)
	if !ok {
		return false
	}
	for _, v := range vs {
		if v == hem.exact {
			return true
		}
	}
	return false
}

func (hem *headerExactMatcher) Equal(m matcher) bool {
	mm, ok := m.(*headerExactMatcher)
	if !ok {
		return false
	}
	return hem.key == mm.key && hem.exact == mm.exact
}

func (hem *headerExactMatcher) String() string {
	return fmt.Sprintf("headerExact:%v:%v", hem.key, hem.exact)
}

type headerRegexMatcher struct {
	key string
	s   string
	re  *regexp.Regexp
}

func newHeaderRegexMatcher(key, regexStr string) *headerRegexMatcher {
	return &headerRegexMatcher{key: key, s: regexStr, re: regexp.MustCompile(regexStr)}
}

func (hrm *headerRegexMatcher) match(info balancer.PickInfo) bool {
	vs, ok := mdValuesFromOutgoingCtx(info.Ctx, hrm.key)
	if !ok {
		return false
	}
	for _, v := range vs {
		if hrm.re.MatchString(v) {
			return true
		}
	}
	return false
}

func (hrm *headerRegexMatcher) Equal(m matcher) bool {
	mm, ok := m.(*headerRegexMatcher)
	if !ok {
		return false
	}
	return hrm.key == mm.key && hrm.s == mm.s
}

func (hrm *headerRegexMatcher) String() string {
	return fmt.Sprintf("headerRegex:%v:%v", hrm.key, hrm.re.String())
}

type headerRangeMatcher struct {
	key        string
	start, end int64 // represents [start, end).
}

func newHeaderRangeMatcher(key string, start, end int64) *headerRangeMatcher {
	return &headerRangeMatcher{key: key, start: start, end: end}
}

func (hrm *headerRangeMatcher) match(info balancer.PickInfo) bool {
	vs, ok := mdValuesFromOutgoingCtx(info.Ctx, hrm.key)
	if !ok {
		return false
	}
	for _, v := range vs {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil && i >= hrm.start && i < hrm.end {
			return true
		}
	}
	return false
}

func (hrm *headerRangeMatcher) Equal(m matcher) bool {
	mm, ok := m.(*headerRangeMatcher)
	if !ok {
		return false
	}
	return hrm.key == mm.key && hrm.start == mm.start && hrm.end == mm.end
}

func (hrm *headerRangeMatcher) String() string {
	return fmt.Sprintf("headerRange:%v:[%d,%d)", hrm.key, hrm.start, hrm.end)
}

type headerPresentMatcher struct {
	key     string
	present bool
}

func newHeaderPresentMatcher(key string, present bool) *headerPresentMatcher {
	return &headerPresentMatcher{key: key, present: present}
}

func (hpm *headerPresentMatcher) match(info balancer.PickInfo) bool {
	vs, ok := mdValuesFromOutgoingCtx(info.Ctx, hpm.key)
	present := ok && len(vs) > 0
	return present == hpm.present
}

func (hpm *headerPresentMatcher) Equal(m matcher) bool {
	mm, ok := m.(*headerPresentMatcher)
	if !ok {
		return false
	}
	return hpm.key == mm.key && hpm.present == mm.present
}

func (hpm *headerPresentMatcher) String() string {
	return fmt.Sprintf("headerPresent:%v:%v", hpm.key, hpm.present)
}

type headerPrefixMatcher struct {
	key    string
	prefix string
}

func newHeaderPrefixMatcher(key string, prefix string) *headerPrefixMatcher {
	return &headerPrefixMatcher{key: key, prefix: prefix}
}

func (hpm *headerPrefixMatcher) match(info balancer.PickInfo) bool {
	vs, ok := mdValuesFromOutgoingCtx(info.Ctx, hpm.key)
	if !ok {
		return false
	}
	for _, v := range vs {
		if strings.HasPrefix(v, hpm.prefix) {
			return true
		}
	}
	return false
}

func (hpm *headerPrefixMatcher) Equal(m matcher) bool {
	mm, ok := m.(*headerPrefixMatcher)
	if !ok {
		return false
	}
	return hpm.key == mm.key && hpm.prefix == mm.prefix
}

func (hpm *headerPrefixMatcher) String() string {
	return fmt.Sprintf("headerPrefix:%v:%v", hpm.key, hpm.prefix)
}

type headerSuffixMatcher struct {
	key    string
	suffix string
}

func newHeaderSuffixMatcher(key string, suffix string) *headerSuffixMatcher {
	return &headerSuffixMatcher{key: key, suffix: suffix}
}

func (hsm *headerSuffixMatcher) match(info balancer.PickInfo) bool {
	vs, ok := mdValuesFromOutgoingCtx(info.Ctx, hsm.key)
	if !ok {
		return false
	}
	for _, v := range vs {
		if strings.HasSuffix(v, hsm.suffix) {
			return true
		}
	}
	return false
}

func (hsm *headerSuffixMatcher) Equal(m matcher) bool {
	mm, ok := m.(*headerSuffixMatcher)
	if !ok {
		return false
	}
	return hsm.key == mm.key && hsm.suffix == mm.suffix
}

func (hsm *headerSuffixMatcher) String() string {
	return fmt.Sprintf("headerSuffix:%v:%v", hsm.key, hsm.suffix)
}
