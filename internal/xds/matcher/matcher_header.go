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

package matcher

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/grpc/metadata"
)

type HeaderMatcherInterface interface {
	Match(metadata.MD) bool
	String() string
}

// mdValuesFromOutgoingCtx retrieves metadata from context. If there are
// multiple values, the values are concatenated with "," (comma and no space).
//
// All header matchers only match against the comma-concatenated string.
func mdValuesFromOutgoingCtx(md metadata.MD, key string) (string, bool) {
	vs, ok := md[key]
	if !ok {
		return "", false
	}
	return strings.Join(vs, ","), true
}

type headerExactMatcher struct {
	key   string
	exact string
}

func NewHeaderExactMatcher(key, exact string) *headerExactMatcher {
	return &headerExactMatcher{key: key, exact: exact}
}

func (hem *headerExactMatcher) Match(md metadata.MD) bool {
	v, ok := mdValuesFromOutgoingCtx(md, hem.key)
	if !ok {
		return false
	}
	return v == hem.exact
}

func (hem *headerExactMatcher) String() string {
	return fmt.Sprintf("headerExact:%v:%v", hem.key, hem.exact)
}

type headerRegexMatcher struct {
	key string
	re  *regexp.Regexp
}

func NewHeaderRegexMatcher(key string, re *regexp.Regexp) *headerRegexMatcher {
	return &headerRegexMatcher{key: key, re: re}
}

func (hrm *headerRegexMatcher) Match(md metadata.MD) bool {
	v, ok := mdValuesFromOutgoingCtx(md, hrm.key)
	if !ok {
		return false
	}
	return hrm.re.MatchString(v)
}

func (hrm *headerRegexMatcher) String() string {
	return fmt.Sprintf("headerRegex:%v:%v", hrm.key, hrm.re.String())
}

type headerRangeMatcher struct {
	key        string
	start, end int64 // represents [start, end).
}

func NewHeaderRangeMatcher(key string, start, end int64) *headerRangeMatcher {
	return &headerRangeMatcher{key: key, start: start, end: end}
}

func (hrm *headerRangeMatcher) Match(md metadata.MD) bool {
	v, ok := mdValuesFromOutgoingCtx(md, hrm.key)
	if !ok {
		return false
	}
	if i, err := strconv.ParseInt(v, 10, 64); err == nil && i >= hrm.start && i < hrm.end {
		return true
	}
	return false
}

func (hrm *headerRangeMatcher) String() string {
	return fmt.Sprintf("headerRange:%v:[%d,%d)", hrm.key, hrm.start, hrm.end)
}

type headerPresentMatcher struct {
	key     string
	present bool
}

func NewHeaderPresentMatcher(key string, present bool) *headerPresentMatcher {
	return &headerPresentMatcher{key: key, present: present}
}

func (hpm *headerPresentMatcher) Match(md metadata.MD) bool {
	vs, ok := mdValuesFromOutgoingCtx(md, hpm.key)
	present := ok && len(vs) > 0
	return present == hpm.present
}

func (hpm *headerPresentMatcher) String() string {
	return fmt.Sprintf("headerPresent:%v:%v", hpm.key, hpm.present)
}

type headerPrefixMatcher struct {
	key    string
	prefix string
}

func NewHeaderPrefixMatcher(key string, prefix string) *headerPrefixMatcher {
	return &headerPrefixMatcher{key: key, prefix: prefix}
}

func (hpm *headerPrefixMatcher) Match(md metadata.MD) bool {
	v, ok := mdValuesFromOutgoingCtx(md, hpm.key)
	if !ok {
		return false
	}
	return strings.HasPrefix(v, hpm.prefix)
}

func (hpm *headerPrefixMatcher) String() string {
	return fmt.Sprintf("headerPrefix:%v:%v", hpm.key, hpm.prefix)
}

type headerSuffixMatcher struct {
	key    string
	suffix string
}

func NewHeaderSuffixMatcher(key string, suffix string) *headerSuffixMatcher {
	return &headerSuffixMatcher{key: key, suffix: suffix}
}

func (hsm *headerSuffixMatcher) Match(md metadata.MD) bool {
	v, ok := mdValuesFromOutgoingCtx(md, hsm.key)
	if !ok {
		return false
	}
	return strings.HasSuffix(v, hsm.suffix)
}

func (hsm *headerSuffixMatcher) String() string {
	return fmt.Sprintf("headerSuffix:%v:%v", hsm.key, hsm.suffix)
}

type invertMatcher struct {
	m HeaderMatcherInterface
}

func NewInvertMatcher(m HeaderMatcherInterface) *invertMatcher {
	return &invertMatcher{m: m}
}

func (i *invertMatcher) Match(md metadata.MD) bool {
	return !i.m.Match(md)
}

func (i *invertMatcher) String() string {
	return fmt.Sprintf("invert{%s}", i.m)
}
