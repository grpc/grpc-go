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

package header_test

import (
	"testing"

	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	headermatcher "google.golang.org/grpc/internal/xds/matcher/header"
	"google.golang.org/grpc/metadata"
)

func TestFromProto(t *testing.T) {
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
			got, err := headermatcher.FromProto(test.matcherProto)
			if (err != nil) != test.wantErr {
				t.Fatalf("headermatcher.FromProto(%+v) error = %v, wantErr %v", test.matcherProto, err, test.wantErr)
			}
			if test.wantErr {
				if got != nil {
					t.Fatalf("headermatcher.FromProto(%+v) returned matcher %v with error; want nil", test.matcherProto, got)
				}
				return
			}
			if gotMatch := got.Match(test.md); gotMatch != test.wantMatch {
				t.Errorf("headermatcher.FromProto(%+v).Match(%v) = %v, want %v", test.matcherProto, test.md, gotMatch, test.wantMatch)
			}
		})
	}
}
