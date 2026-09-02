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

	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/grpc/metadata"
)

func TestHeaderMatcherFromProto(t *testing.T) {
	var (
		typedNilExact    *v3routepb.HeaderMatcher_ExactMatch
		typedNilRegex    *v3routepb.HeaderMatcher_SafeRegexMatch
		typedNilRange    *v3routepb.HeaderMatcher_RangeMatch
		typedNilPresent  *v3routepb.HeaderMatcher_PresentMatch
		typedNilPrefix   *v3routepb.HeaderMatcher_PrefixMatch
		typedNilSuffix   *v3routepb.HeaderMatcher_SuffixMatch
		typedNilContains *v3routepb.HeaderMatcher_ContainsMatch
		typedNilString   *v3routepb.HeaderMatcher_StringMatch
	)
	tests := []struct {
		name         string
		matcherProto *v3routepb.HeaderMatcher
		md           metadata.MD
		wantMatch    bool
		wantErr      bool
	}{
		{
			name: "exact match",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "value"},
			},
			md:        metadata.Pairs("x-test", "value"),
			wantMatch: true,
		},
		{
			name: "safe regex match",
			matcherProto: &v3routepb.HeaderMatcher{
				Name: "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_SafeRegexMatch{
					SafeRegexMatch: &v3matcherpb.RegexMatcher{Regex: "val.*"},
				},
			},
			md:        metadata.Pairs("x-test", "value"),
			wantMatch: true,
		},
		{
			name: "safe regex is implicitly anchored",
			matcherProto: &v3routepb.HeaderMatcher{
				Name: "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_SafeRegexMatch{
					SafeRegexMatch: &v3matcherpb.RegexMatcher{Regex: "alu"},
				},
			},
			md: metadata.Pairs("x-test", "value"),
		},
		{
			name: "range match",
			matcherProto: &v3routepb.HeaderMatcher{
				Name: "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_RangeMatch{
					RangeMatch: &v3typepb.Int64Range{Start: 1, End: 10},
				},
			},
			md:        metadata.Pairs("x-test", "5"),
			wantMatch: true,
		},
		{
			name: "present match",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PresentMatch{PresentMatch: true},
			},
			md:        metadata.Pairs("x-test", "value"),
			wantMatch: true,
		},
		{
			name: "prefix match",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PrefixMatch{PrefixMatch: "val"},
			},
			md:        metadata.Pairs("x-test", "value"),
			wantMatch: true,
		},
		{
			name: "suffix match",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_SuffixMatch{SuffixMatch: "lue"},
			},
			md:        metadata.Pairs("x-test", "value"),
			wantMatch: true,
		},
		{
			name: "contains match",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ContainsMatch{ContainsMatch: "alu"},
			},
			md:        metadata.Pairs("x-test", "value"),
			wantMatch: true,
		},
		{
			name: "string match",
			matcherProto: &v3routepb.HeaderMatcher{
				Name: "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_StringMatch{StringMatch: &v3matcherpb.StringMatcher{
					MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "VALUE"},
					IgnoreCase:   true,
				}},
			},
			md:        metadata.Pairs("x-test", "value"),
			wantMatch: true,
		},
		{
			name: "inverted match",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "other"},
				InvertMatch:          true,
			},
			md:        metadata.Pairs("x-test", "value"),
			wantMatch: true,
		},
		{
			name: "inverted match with missing header does not match",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "value"},
				InvertMatch:          true,
			},
			md: metadata.Pairs("other", "value"),
		},
		{
			name: "inverted present match with missing header matches",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PresentMatch{PresentMatch: true},
				InvertMatch:          true,
			},
			md:        metadata.Pairs("other", "value"),
			wantMatch: true,
		},
		{
			name: "treat missing header as empty remains unsupported",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                      "X-Test",
				HeaderMatchSpecifier:      &v3routepb.HeaderMatcher_ExactMatch{},
				TreatMissingHeaderAsEmpty: true,
			},
			md: metadata.Pairs("other", "value"),
		},
		{
			name: "empty exact match is accepted",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{},
			},
			md:        metadata.Pairs("x-test", ""),
			wantMatch: true,
		},
		{
			name:         "nil proto",
			matcherProto: nil,
			wantErr:      true,
		},
		{
			name:         "unset matcher type",
			matcherProto: &v3routepb.HeaderMatcher{Name: "X-Test"},
			wantErr:      true,
		},
		{
			name: "typed nil exact matcher",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: typedNilExact,
			},
			wantErr: true,
		},
		{
			name: "nil safe regex matcher",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_SafeRegexMatch{},
			},
			wantErr: true,
		},
		{
			name: "typed nil safe regex matcher",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: typedNilRegex,
			},
			wantErr: true,
		},
		{
			name: "invalid safe regex matcher",
			matcherProto: &v3routepb.HeaderMatcher{
				Name: "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_SafeRegexMatch{
					SafeRegexMatch: &v3matcherpb.RegexMatcher{Regex: "["},
				},
			},
			wantErr: true,
		},
		{
			name: "typed nil range matcher",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: typedNilRange,
			},
			wantErr: true,
		},
		{
			name: "nil range matcher",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_RangeMatch{},
			},
			wantErr: true,
		},
		{
			name: "typed nil present matcher",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: typedNilPresent,
			},
			wantErr: true,
		},
		{
			name: "empty prefix match is rejected",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PrefixMatch{},
			},
			wantErr: true,
		},
		{
			name: "typed nil prefix matcher",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: typedNilPrefix,
			},
			wantErr: true,
		},
		{
			name: "empty suffix match is rejected",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_SuffixMatch{},
			},
			wantErr: true,
		},
		{
			name: "typed nil suffix matcher",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: typedNilSuffix,
			},
			wantErr: true,
		},
		{
			name: "empty contains match is rejected",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ContainsMatch{},
			},
			wantErr: true,
		},
		{
			name: "typed nil contains matcher",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: typedNilContains,
			},
			wantErr: true,
		},
		{
			name: "typed nil string matcher",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: typedNilString,
			},
			wantErr: true,
		},
		{
			name: "nil string matcher",
			matcherProto: &v3routepb.HeaderMatcher{
				Name:                 "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_StringMatch{},
			},
			wantErr: true,
		},
		{
			name: "invalid string matcher",
			matcherProto: &v3routepb.HeaderMatcher{
				Name: "X-Test",
				HeaderMatchSpecifier: &v3routepb.HeaderMatcher_StringMatch{
					StringMatch: &v3matcherpb.StringMatcher{},
				},
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := HeaderMatcherFromProto(test.matcherProto)
			if (err != nil) != test.wantErr {
				t.Fatalf("HeaderMatcherFromProto(%+v) error = %v, wantErr %v", test.matcherProto, err, test.wantErr)
			}
			if test.wantErr {
				if got != nil {
					t.Fatalf("HeaderMatcherFromProto(%+v) returned matcher %v with error; want nil", test.matcherProto, got)
				}
				return
			}
			if gotMatch := got.Match(test.md); gotMatch != test.wantMatch {
				t.Errorf("HeaderMatcherFromProto(%+v).Match(%v) = %v, want %v", test.matcherProto, test.md, gotMatch, test.wantMatch)
			}
		})
	}
}

func TestHeaderMatcherConstructorsLowercaseKeys(t *testing.T) {
	tests := []struct {
		name    string
		matcher HeaderMatcher
		md      metadata.MD
	}{
		{name: "exact", matcher: NewHeaderExactMatcher("X-Test", "value", false), md: metadata.Pairs("x-test", "value")},
		{name: "regex", matcher: NewHeaderRegexMatcher("X-Test", regexp.MustCompile("value"), false), md: metadata.Pairs("x-test", "value")},
		{name: "range", matcher: NewHeaderRangeMatcher("X-Test", 1, 10, false), md: metadata.Pairs("x-test", "5")},
		{name: "present", matcher: NewHeaderPresentMatcher("X-Test", true, false), md: metadata.Pairs("x-test", "value")},
		{name: "prefix", matcher: NewHeaderPrefixMatcher("X-Test", "val", false), md: metadata.Pairs("x-test", "value")},
		{name: "suffix", matcher: NewHeaderSuffixMatcher("X-Test", "lue", false), md: metadata.Pairs("x-test", "value")},
		{name: "contains", matcher: NewHeaderContainsMatcher("X-Test", "alu", false), md: metadata.Pairs("x-test", "value")},
		{name: "string", matcher: NewHeaderStringMatcher("X-Test", NewExactStringMatcher("value", false), false), md: metadata.Pairs("x-test", "value")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !test.matcher.Match(test.md) {
				t.Errorf("matcher with mixed-case key did not match lowercase metadata: %v", test.matcher)
			}
		})
	}
}

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

func TestHeaderStringMatch(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		sm     StringMatcher
		invert bool
		md     metadata.MD
		want   bool
	}{
		{
			name: "should-match",
			key:  "th",
			sm: StringMatcher{
				exactMatch: newStringP("tv"),
			},
			invert: false,
			md:     metadata.Pairs("th", "tv"),
			want:   true,
		},
		{
			name: "not match",
			key:  "th",
			sm: StringMatcher{
				containsMatch: newStringP("tv"),
			},
			invert: false,
			md:     metadata.Pairs("th", "not-match"),
			want:   false,
		},
		{
			name: "invert string match",
			key:  "th",
			sm: StringMatcher{
				containsMatch: newStringP("tv"),
			},
			invert: true,
			md:     metadata.Pairs("th", "not-match"),
			want:   true,
		},
		{
			name: "header missing",
			key:  "th",
			sm: StringMatcher{
				containsMatch: newStringP("tv"),
			},
			invert: false,
			md:     metadata.Pairs("not-specified-key", "not-match"),
			want:   false,
		},
		{
			name: "header missing invert true",
			key:  "th",
			sm: StringMatcher{
				containsMatch: newStringP("tv"),
			},
			invert: true,
			md:     metadata.Pairs("not-specified-key", "not-match"),
			want:   false,
		},
		{
			name: "header empty string invert",
			key:  "th",
			sm: StringMatcher{
				containsMatch: newStringP("tv"),
			},
			invert: true,
			md:     metadata.Pairs("th", ""),
			want:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hsm := NewHeaderStringMatcher(test.key, test.sm, test.invert)
			if got := hsm.Match(test.md); got != test.want {
				t.Errorf("match() = %v, want %v", got, test.want)
			}
		})
	}
}
