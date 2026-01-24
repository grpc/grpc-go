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

package server

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func (s) TestMatchTypeForDomain(t *testing.T) {
	tests := []struct {
		d    string
		want domainMatchType
	}{
		{d: "", want: domainMatchTypeInvalid},
		{d: "*", want: domainMatchTypeUniversal},
		{d: "bar.*", want: domainMatchTypePrefix},
		{d: "*.abc.com", want: domainMatchTypeSuffix},
		{d: "foo.bar.com", want: domainMatchTypeExact},
		{d: "foo.*.com", want: domainMatchTypeInvalid},
	}
	for _, tt := range tests {
		if got := matchTypeForDomain(tt.d); got != tt.want {
			t.Errorf("matchTypeForDomain(%q) = %v, want %v", tt.d, got, tt.want)
		}
	}
}

func (s) TestMatch(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		host        string
		wantTyp     domainMatchType
		wantMatched bool
	}{
		{name: "invalid-empty", domain: "", host: "", wantTyp: domainMatchTypeInvalid, wantMatched: false},
		{name: "invalid", domain: "a.*.b", host: "", wantTyp: domainMatchTypeInvalid, wantMatched: false},
		{name: "universal", domain: "*", host: "abc.com", wantTyp: domainMatchTypeUniversal, wantMatched: true},
		{name: "prefix-match", domain: "abc.*", host: "abc.123", wantTyp: domainMatchTypePrefix, wantMatched: true},
		{name: "prefix-no-match", domain: "abc.*", host: "abcd.123", wantTyp: domainMatchTypePrefix, wantMatched: false},
		{name: "suffix-match", domain: "*.123", host: "abc.123", wantTyp: domainMatchTypeSuffix, wantMatched: true},
		{name: "suffix-no-match", domain: "*.123", host: "abc.1234", wantTyp: domainMatchTypeSuffix, wantMatched: false},
		{name: "exact-match", domain: "foo.bar", host: "foo.bar", wantTyp: domainMatchTypeExact, wantMatched: true},
		{name: "exact-no-match", domain: "foo.bar.com", host: "foo.bar", wantTyp: domainMatchTypeExact, wantMatched: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotTyp, gotMatched := match(tt.domain, tt.host); gotTyp != tt.wantTyp || gotMatched != tt.wantMatched {
				t.Errorf("match() = %v, %v, want %v, %v", gotTyp, gotMatched, tt.wantTyp, tt.wantMatched)
			}
		})
	}
}

func (s) TestFindBestMatchingVirtualHost(t *testing.T) {
	var (
		oneExactMatch     = virtualHostWithInterceptors{domains: []string{"foo.bar.com"}}
		oneSuffixMatch    = virtualHostWithInterceptors{domains: []string{"*.bar.com"}}
		onePrefixMatch    = virtualHostWithInterceptors{domains: []string{"foo.bar.*"}}
		oneUniversalMatch = virtualHostWithInterceptors{domains: []string{"*"}}
		longExactMatch    = virtualHostWithInterceptors{domains: []string{"v2.foo.bar.com"}}
		multipleMatch     = virtualHostWithInterceptors{domains: []string{"pi.foo.bar.com", "314.*", "*.159"}}
		vhs               = []virtualHostWithInterceptors{oneExactMatch, oneSuffixMatch, onePrefixMatch, oneUniversalMatch, longExactMatch, multipleMatch}
	)

	tests := []struct {
		name   string
		host   string
		vHosts []virtualHostWithInterceptors
		want   *virtualHostWithInterceptors
	}{
		{name: "exact-match", host: "foo.bar.com", vHosts: vhs, want: &oneExactMatch},
		{name: "suffix-match", host: "123.bar.com", vHosts: vhs, want: &oneSuffixMatch},
		{name: "prefix-match", host: "foo.bar.org", vHosts: vhs, want: &onePrefixMatch},
		{name: "universal-match", host: "abc.123", vHosts: vhs, want: &oneUniversalMatch},
		{name: "long-exact-match", host: "v2.foo.bar.com", vHosts: vhs, want: &longExactMatch},
		// Matches suffix "*.bar.com" and exact "pi.foo.bar.com". Takes exact.
		{name: "multiple-match-exact", host: "pi.foo.bar.com", vHosts: vhs, want: &multipleMatch},
		// Matches suffix "*.159" and prefix "foo.bar.*". Takes suffix.
		{name: "multiple-match-suffix", host: "foo.bar.159", vHosts: vhs, want: &multipleMatch},
		// Matches suffix "*.bar.com" and prefix "314.*". Takes suffix.
		{name: "multiple-match-prefix", host: "314.bar.com", vHosts: vhs, want: &oneSuffixMatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findBestMatchingVirtualHostServer(tt.host, tt.vHosts); !cmp.Equal(got, tt.want, cmp.AllowUnexported(virtualHostWithInterceptors{}, routeWithInterceptors{})) {
				t.Errorf("FindBestMatchingxdsclient.virtualHostWithInterceptors() = %v, want %v", got, tt.want)
			}
		})
	}
}
