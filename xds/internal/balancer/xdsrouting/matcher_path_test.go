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
	"testing"
)

func TestPathFullMatcherMatch(t *testing.T) {
	tests := []struct {
		name     string
		fullPath string
		path     string
		want     bool
	}{
		{name: "match", fullPath: "/s/m", path: "/s/m", want: true},
		{name: "not match", fullPath: "/s/m", path: "/a/b", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fpm := newPathExactMatcher(tt.fullPath)
			if got := fpm.match(tt.path); got != tt.want {
				t.Errorf("{%q}.match(%q) = %v, want %v", tt.fullPath, tt.path, got, tt.want)
			}
		})
	}
}

func TestPathPrefixMatcherMatch(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		path   string
		want   bool
	}{
		{name: "match", prefix: "/s/", path: "/s/m", want: true},
		{name: "not match", prefix: "/s/", path: "/a/b", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fpm := newPathPrefixMatcher(tt.prefix)
			if got := fpm.match(tt.path); got != tt.want {
				t.Errorf("{%q}.match(%q) = %v, want %v", tt.prefix, tt.path, got, tt.want)
			}
		})
	}
}

func TestPathRegexMatcherMatch(t *testing.T) {
	tests := []struct {
		name      string
		regexPath string
		path      string
		want      bool
	}{
		{name: "match", regexPath: "^/s+/m.*$", path: "/sss/me", want: true},
		{name: "not match", regexPath: "^/s+/m*$", path: "/sss/b", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fpm := newPathRegexMatcher(regexp.MustCompile(tt.regexPath))
			if got := fpm.match(tt.path); got != tt.want {
				t.Errorf("{%q}.match(%q) = %v, want %v", tt.regexPath, tt.path, got, tt.want)
			}
		})
	}
}
