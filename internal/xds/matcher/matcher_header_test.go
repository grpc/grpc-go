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
	"regexp"
	"testing"

	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"google.golang.org/grpc/metadata"
)

func TestHeaderExactMatcherMatch(t *testing.T) {
	tests := []struct {
		name       string
		key, exact string
		md         metadata.MD
		want       bool
		invert     bool
	}{
		{
			name:  "one value one match",
			key:   "th",
			exact: "tv",
			md:    metadata.Pairs("th", "tv"),
			want:  true,
		},
		{
			name:  "two value one match",
			key:   "th",
			exact: "tv",
			md:    metadata.Pairs("th", "abc", "th", "tv"),
			// Doesn't match comma-concatenated string.
			want: false,
		},
		{
			name:  "two value match concatenated",
			key:   "th",
			exact: "abc,tv",
			md:    metadata.Pairs("th", "abc", "th", "tv"),
			want:  true,
		},
		{
			name:  "not match",
			key:   "th",
			exact: "tv",
			md:    metadata.Pairs("th", "abc"),
			want:  false,
		},
		{
			name:   "invert header not present",
			key:    "th",
			exact:  "tv",
			md:     metadata.Pairs(":method", "GET"),
			want:   false,
			invert: true,
		},
		{
			name:   "invert header match",
			key:    "th",
			exact:  "tv",
			md:     metadata.Pairs("th", "tv"),
			want:   false,
			invert: true,
		},
		{
			name:   "invert header not match",
			key:    "th",
			exact:  "tv",
			md:     metadata.Pairs("th", "tvv"),
			want:   true,
			invert: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hem := NewHeaderExactMatcher(tt.key, tt.exact, tt.invert)
			if got := hem.Match(tt.md); got != tt.want {
				t.Errorf("match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeaderRegexMatcherMatch(t *testing.T) {
	tests := []struct {
		name          string
		key, regexStr string
		md            metadata.MD
		want          bool
		invert        bool
	}{
		{
			name:     "one value one match",
			key:      "th",
			regexStr: "^t+v*$",
			md:       metadata.Pairs("th", "tttvv"),
			want:     true,
		},
		{
			name:     "two value one match",
			key:      "th",
			regexStr: "^t+v*$",
			md:       metadata.Pairs("th", "abc", "th", "tttvv"),
			want:     false,
		},
		{
			name:     "two value match concatenated",
			key:      "th",
			regexStr: "^[abc]*,t+v*$",
			md:       metadata.Pairs("th", "abc", "th", "tttvv"),
			want:     true,
		},
		{
			name:     "no match",
			key:      "th",
			regexStr: "^t+v*$",
			md:       metadata.Pairs("th", "abc"),
			want:     false,
		},
		{
			name:     "no match because only part of value matches with regex",
			key:      "header",
			regexStr: "^a+$",
			md:       metadata.Pairs("header", "ab"),
			want:     false,
		},
		{
			name:     "match because full value matches with regex",
			key:      "header",
			regexStr: "^a+$",
			md:       metadata.Pairs("header", "aa"),
			want:     true,
		},
		{
			name:     "invert header not present",
			key:      "th",
			regexStr: "^t+v*$",
			md:       metadata.Pairs(":method", "GET"),
			want:     false,
			invert:   true,
		},
		{
			name:     "invert header match",
			key:      "th",
			regexStr: "^t+v*$",
			md:       metadata.Pairs("th", "tttvv"),
			want:     false,
			invert:   true,
		},
		{
			name:     "invert header not match",
			key:      "th",
			regexStr: "^t+v*$",
			md:       metadata.Pairs("th", "abc"),
			want:     true,
			invert:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hrm := NewHeaderRegexMatcher(tt.key, regexp.MustCompile(tt.regexStr), tt.invert)
			if got := hrm.Match(tt.md); got != tt.want {
				t.Errorf("match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeaderRangeMatcherMatch(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		start, end int64
		md         metadata.MD
		want       bool
		invert     bool
	}{
		{
			name:  "match",
			key:   "th",
			start: 1, end: 10,
			md:   metadata.Pairs("th", "5"),
			want: true,
		},
		{
			name:  "equal to start",
			key:   "th",
			start: 1, end: 10,
			md:   metadata.Pairs("th", "1"),
			want: true,
		},
		{
			name:  "equal to end",
			key:   "th",
			start: 1, end: 10,
			md:   metadata.Pairs("th", "10"),
			want: false,
		},
		{
			name:  "negative",
			key:   "th",
			start: -10, end: 10,
			md:   metadata.Pairs("th", "-5"),
			want: true,
		},
		{
			name:  "invert header not present",
			key:   "th",
			start: 1, end: 10,
			md:     metadata.Pairs(":method", "GET"),
			want:   false,
			invert: true,
		},
		{
			name:  "invert header match",
			key:   "th",
			start: 1, end: 10,
			md:     metadata.Pairs("th", "5"),
			want:   false,
			invert: true,
		},
		{
			name:  "invert header not match",
			key:   "th",
			start: 1, end: 9,
			md:     metadata.Pairs("th", "10"),
			want:   true,
			invert: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hrm := NewHeaderRangeMatcher(tt.key, tt.start, tt.end, tt.invert)
			if got := hrm.Match(tt.md); got != tt.want {
				t.Errorf("match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeaderPresentMatcherMatch(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		present bool
		md      metadata.MD
		want    bool
		invert  bool
	}{
		{
			name:    "want present is present",
			key:     "th",
			present: true,
			md:      metadata.Pairs("th", "tv"),
			want:    true,
		},
		{
			name:    "want present not present",
			key:     "th",
			present: true,
			md:      metadata.Pairs("abc", "tv"),
			want:    false,
		},
		{
			name:    "want not present is present",
			key:     "th",
			present: false,
			md:      metadata.Pairs("th", "tv"),
			want:    false,
		},
		{
			name:    "want not present is not present",
			key:     "th",
			present: false,
			md:      metadata.Pairs("abc", "tv"),
			want:    true,
		},
		{
			name:    "invert header not present",
			key:     "th",
			present: true,
			md:      metadata.Pairs(":method", "GET"),
			want:    true,
			invert:  true,
		},
		{
			name:    "invert header match",
			key:     "th",
			present: true,
			md:      metadata.Pairs("th", "tv"),
			want:    false,
			invert:  true,
		},
		{
			name:    "invert header not match",
			key:     "th",
			present: true,
			md:      metadata.Pairs(":method", "GET"),
			want:    true,
			invert:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hpm := NewHeaderPresentMatcher(tt.key, tt.present, tt.invert)
			if got := hpm.Match(tt.md); got != tt.want {
				t.Errorf("match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeaderPrefixMatcherMatch(t *testing.T) {
	tests := []struct {
		name        string
		key, prefix string
		md          metadata.MD
		want        bool
		invert      bool
	}{
		{
			name:   "one value one match",
			key:    "th",
			prefix: "tv",
			md:     metadata.Pairs("th", "tv123"),
			want:   true,
		},
		{
			name:   "two value one match",
			key:    "th",
			prefix: "tv",
			md:     metadata.Pairs("th", "abc", "th", "tv123"),
			want:   false,
		},
		{
			name:   "two value match concatenated",
			key:    "th",
			prefix: "tv",
			md:     metadata.Pairs("th", "tv123", "th", "abc"),
			want:   true,
		},
		{
			name:   "not match",
			key:    "th",
			prefix: "tv",
			md:     metadata.Pairs("th", "abc"),
			want:   false,
		},
		{
			name:   "invert header not present",
			key:    "th",
			prefix: "tv",
			md:     metadata.Pairs(":method", "GET"),
			want:   false,
			invert: true,
		},
		{
			name:   "invert header match",
			key:    "th",
			prefix: "tv",
			md:     metadata.Pairs("th", "tv123"),
			want:   false,
			invert: true,
		},
		{
			name:   "invert header not match",
			key:    "th",
			prefix: "tv",
			md:     metadata.Pairs("th", "abc"),
			want:   true,
			invert: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hpm := NewHeaderPrefixMatcher(tt.key, tt.prefix, tt.invert)
			if got := hpm.Match(tt.md); got != tt.want {
				t.Errorf("match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeaderSuffixMatcherMatch(t *testing.T) {
	tests := []struct {
		name        string
		key, suffix string
		md          metadata.MD
		want        bool
		invert      bool
	}{
		{
			name:   "one value one match",
			key:    "th",
			suffix: "tv",
			md:     metadata.Pairs("th", "123tv"),
			want:   true,
		},
		{
			name:   "two value one match",
			key:    "th",
			suffix: "tv",
			md:     metadata.Pairs("th", "123tv", "th", "abc"),
			want:   false,
		},
		{
			name:   "two value match concatenated",
			key:    "th",
			suffix: "tv",
			md:     metadata.Pairs("th", "abc", "th", "123tv"),
			want:   true,
		},
		{
			name:   "not match",
			key:    "th",
			suffix: "tv",
			md:     metadata.Pairs("th", "abc"),
			want:   false,
		},
		{
			name:   "invert header not present",
			key:    "th",
			suffix: "tv",
			md:     metadata.Pairs(":method", "GET"),
			want:   false,
			invert: true,
		},
		{
			name:   "invert header match",
			key:    "th",
			suffix: "tv",
			md:     metadata.Pairs("th", "123tv"),
			want:   false,
			invert: true,
		},
		{
			name:   "invert header not match",
			key:    "th",
			suffix: "tv",
			md:     metadata.Pairs("th", "abc"),
			want:   true,
			invert: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hsm := NewHeaderSuffixMatcher(tt.key, tt.suffix, tt.invert)
			if got := hsm.Match(tt.md); got != tt.want {
				t.Errorf("match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeaderStringMatcherMatch(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		stringMatch *v3matcherpb.StringMatcher
		md          metadata.MD
		want        bool
		invert      bool
	}{
		// exact match
		{
			name:        "[exact] match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "tv"}},
			md:          metadata.Pairs("th", "tv"),
			want:        true,
			invert:      false,
		},
		{
			name:        "[exact] not match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "tv"}},
			md:          metadata.Pairs("th", "tv123"),
			want:        false,
			invert:      false,
		},
		{
			name:        "[exact] two value one match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "tv"}},
			md:          metadata.Pairs("th", "abc", "th", "tv"),
			want:        false,
			invert:      false,
		},
		{
			name:        "[exact] two value match concatenated",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "abc,tv"}},
			md:          metadata.Pairs("th", "abc", "th", "tv"),
			want:        true,
			invert:      false,
		},
		{
			name:        "[exact] invert match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "tv"}},
			md:          metadata.Pairs("th", "tv"),
			want:        false,
			invert:      true,
		},
		{
			name:        "[exact] invert not match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "tv"}},
			md:          metadata.Pairs("th", "tvv"),
			want:        true,
			invert:      true,
		},
		{
			name:        "[exact] invert header not present",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "tv"}},
			md:          metadata.Pairs(":method", "GET"),
			want:        false,
			invert:      true,
		},
		// prefix match
		{
			name:        "[prefix] prefix match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: "tv"}},
			md:          metadata.Pairs("th", "tv123"),
			want:        true,
			invert:      false,
		},
		{
			name:        "[prefix] prefix not match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: "tv"}},
			md:          metadata.Pairs("th", "abc"),
			want:        false,
			invert:      false,
		},
		{
			name:        "[prefix] two value one match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: "tv"}},
			md:          metadata.Pairs("th", "abc", "th", "tv"),
			want:        false,
			invert:      false,
		},
		{
			name:        "[prefix] two value match concatenated",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: "tv"}},
			md:          metadata.Pairs("th", "tv123", "th", "abc"),
			want:        true,
			invert:      false,
		},
		{
			name:        "[prefix] invert match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: "tv"}},
			md:          metadata.Pairs("th", "tv123"),
			want:        false,
			invert:      true,
		},
		{
			name:        "[prefix] invert not match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: "tv"}},
			md:          metadata.Pairs("th", "abc"),
			want:        true,
			invert:      true,
		},
		{
			name:        "[prefix] invert header not present",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: "tv"}},
			md:          metadata.Pairs(":method", "GET"),
			want:        false,
			invert:      true,
		},
		// suffix match
		{
			name:        "[suffix] match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Suffix{Suffix: "tv"}},
			md:          metadata.Pairs("th", "123tv"),
			want:        true,
			invert:      false,
		},
		{
			name:        "[suffix] not match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Suffix{Suffix: "tv"}},
			md:          metadata.Pairs("th", "tv123"),
			want:        false,
			invert:      false,
		},
		{
			name:        "[suffix] two value one match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Suffix{Suffix: "tv"}},
			md:          metadata.Pairs("th", "tv", "th", "abc"),
			want:        false,
			invert:      false,
		},
		{
			name:        "[suffix] two value match concatenated",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Suffix{Suffix: "tv"}},
			md:          metadata.Pairs("th", "abc", "th", "tv"),
			want:        true,
			invert:      false,
		},
		{
			name:        "[suffix] invert match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Suffix{Suffix: "tv"}},
			md:          metadata.Pairs("th", "tv"),
			want:        false,
			invert:      true,
		},
		{
			name:        "[suffix] invert not match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Suffix{Suffix: "tv"}},
			md:          metadata.Pairs("th", "tv123"),
			want:        true,
			invert:      true,
		},
		{
			name:        "[suffix] invert header not present",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Suffix{Suffix: "tv"}},
			md:          metadata.Pairs(":method", "GET"),
			want:        false,
			invert:      true,
		},
		// regexp match
		{
			name:        "[regexp] match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_SafeRegex{SafeRegex: &v3matcherpb.RegexMatcher{Regex: "^t+v*$"}}},
			md:          metadata.Pairs("th", "tv"),
			want:        true,
			invert:      false,
		},
		{
			name:        "[regexp] not match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_SafeRegex{SafeRegex: &v3matcherpb.RegexMatcher{Regex: "^t+v*$"}}},
			md:          metadata.Pairs("th", "abc"),
			want:        false,
			invert:      false,
		},
		{
			name:        "[regexp] two value one match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_SafeRegex{SafeRegex: &v3matcherpb.RegexMatcher{Regex: "^t+v*$"}}},
			md:          metadata.Pairs("th", "abc", "th", "ttttvvv"),
			want:        false,
			invert:      false,
		},
		{
			name:        "[regexp] two value match concatenated",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_SafeRegex{SafeRegex: &v3matcherpb.RegexMatcher{Regex: "^[abc]*,t+v*$"}}},
			md:          metadata.Pairs("th", "abc", "th", "tttvv"),
			want:        true,
			invert:      false,
		},
		{
			name:        "[regexp] invert match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_SafeRegex{SafeRegex: &v3matcherpb.RegexMatcher{Regex: "^t+v*$"}}},
			md:          metadata.Pairs("th", "tv"),
			want:        false,
			invert:      true,
		},
		{
			name:        "[regexp] invert not match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_SafeRegex{SafeRegex: &v3matcherpb.RegexMatcher{Regex: "^t+v*$"}}},
			md:          metadata.Pairs("th", "abc"),
			want:        true,
			invert:      true,
		},
		{
			name:        "[regexp] invert header not present",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_SafeRegex{SafeRegex: &v3matcherpb.RegexMatcher{Regex: "^t+v*$"}}},
			md:          metadata.Pairs(":method", "GET"),
			want:        false,
			invert:      true,
		},

		{
			name:        "[contains] match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Contains{Contains: "tv"}},
			md:          metadata.Pairs("th", "abctvvv"),
			want:        true,
			invert:      false,
		},
		{
			name:        "[contains] not match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Contains{Contains: "tv"}},
			md:          metadata.Pairs("th", "t11v123"),
			want:        false,
			invert:      false,
		},
		{
			name:        "[contains] two value one match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Contains{Contains: "tv"}},
			md:          metadata.Pairs("th", "abc", "th", "tv"),
			want:        true,
			invert:      false,
		},
		{
			name:        "[contains] two value match concatenated",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Contains{Contains: "tv"}},
			md:          metadata.Pairs("th", "abc", "th", "tv123"),
			want:        true,
			invert:      false,
		},
		{
			name:        "[contains] invert match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Contains{Contains: "tv"}},
			md:          metadata.Pairs("th", "tttvv"),
			want:        false,
			invert:      true,
		},
		{
			name:        "[contains] invert not match",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Contains{Contains: "tv"}},
			md:          metadata.Pairs("th", "abc"),
			want:        true,
			invert:      true,
		},
		{
			name:        "[contains] invert header not present",
			key:         "th",
			stringMatch: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Contains{Contains: "tv"}},
			md:          metadata.Pairs(":method", "GET"),
			want:        false,
			invert:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hsm, err := NewHeaderStringMatcher(tt.key, tt.stringMatch, tt.invert)
			if err != nil {
				t.Fatalf("NewHeaderStringMatcher returned err: %v", err)
			}
			if got := hsm.Match(tt.md); got != tt.want {
				t.Errorf("match() = %v, want %v", got, tt.want)
			}
		})
	}
}
