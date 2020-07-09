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
	"regexp"
	"strings"

	"google.golang.org/grpc/balancer"
)

type pathExactMatcher struct {
	fullPath string
}

func newPathExactMatcher(p string) *pathExactMatcher {
	return &pathExactMatcher{fullPath: p}
}

func (pem *pathExactMatcher) match(info balancer.PickInfo) bool {
	return pem.fullPath == info.FullMethodName
}

func (pem *pathExactMatcher) Equal(m matcher) bool {
	mm, ok := m.(*pathExactMatcher)
	if !ok {
		return false
	}
	return pem.fullPath == mm.fullPath
}

func (pem *pathExactMatcher) String() string {
	return "pathExact:" + pem.fullPath
}

type pathPrefixMatcher struct {
	prefix string
}

func newPathPrefixMatcher(p string) *pathPrefixMatcher {
	return &pathPrefixMatcher{prefix: p}
}

func (ppm *pathPrefixMatcher) match(info balancer.PickInfo) bool {
	return strings.HasPrefix(info.FullMethodName, ppm.prefix)
}

func (ppm *pathPrefixMatcher) Equal(m matcher) bool {
	mm, ok := m.(*pathPrefixMatcher)
	if !ok {
		return false
	}
	return ppm.prefix == mm.prefix
}

func (ppm *pathPrefixMatcher) String() string {
	return "pathPrefix:" + ppm.prefix
}

type pathRegexMatcher struct {
	s  string
	re *regexp.Regexp
}

func newPathRegexMatcher(p string) *pathRegexMatcher {
	return &pathRegexMatcher{s: p, re: regexp.MustCompile(p)}
}

func (prm *pathRegexMatcher) match(info balancer.PickInfo) bool {
	return prm.re.MatchString(info.FullMethodName)
}

func (prm *pathRegexMatcher) Equal(m matcher) bool {
	mm, ok := m.(*pathRegexMatcher)
	if !ok {
		return false
	}
	return prm.s == mm.s
}

func (prm *pathRegexMatcher) String() string {
	return "pathRegex:" + prm.re.String()
}
