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
)

type pathMatcherInterface interface {
	match(path string) bool
	equal(pathMatcherInterface) bool
	String() string
}

type pathExactMatcher struct {
	fullPath string
}

func newPathExactMatcher(p string) *pathExactMatcher {
	return &pathExactMatcher{fullPath: p}
}

func (pem *pathExactMatcher) match(path string) bool {
	return pem.fullPath == path
}

func (pem *pathExactMatcher) equal(m pathMatcherInterface) bool {
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

func (ppm *pathPrefixMatcher) match(path string) bool {
	return strings.HasPrefix(path, ppm.prefix)
}

func (ppm *pathPrefixMatcher) equal(m pathMatcherInterface) bool {
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
	re *regexp.Regexp
}

func newPathRegexMatcher(re *regexp.Regexp) *pathRegexMatcher {
	return &pathRegexMatcher{re: re}
}

func (prm *pathRegexMatcher) match(path string) bool {
	return prm.re.MatchString(path)
}

func (prm *pathRegexMatcher) equal(m pathMatcherInterface) bool {
	mm, ok := m.(*pathRegexMatcher)
	if !ok {
		return false
	}
	return prm.re.String() == mm.re.String()
}

func (prm *pathRegexMatcher) String() string {
	return "pathRegex:" + prm.re.String()
}
