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

package httpfilter

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestHeaderMutationRulesFromProto_HappyPath(t *testing.T) {
	mr := &v3mutationpb.HeaderMutationRules{
		AllowExpression:    &v3matcherpb.RegexMatcher{Regex: "^allow$"},
		DisallowExpression: &v3matcherpb.RegexMatcher{Regex: "^disallow$"},
		DisallowAll:        wrapperspb.Bool(true),
		DisallowIsError:    wrapperspb.Bool(false),
	}
	want := HeaderMutationRules{
		AllowExpr:       regexp.MustCompile("^(?:^allow$)$"),
		DisallowExpr:    regexp.MustCompile("^(?:^disallow$)$"),
		DisallowAll:     true,
		DisallowIsError: false,
	}

	got, err := HeaderMutationRulesFromProto(mr)
	if err != nil {
		t.Fatalf("HeaderMutationRulesFromProto() unexpected error: %v", err)
	}
	if diff := cmp.Diff(got, want,
		cmp.Transformer("RegexpToString", func(r *regexp.Regexp) string {
			if r == nil {
				return ""
			}
			return r.String()
		}),
	); diff != "" {
		t.Fatalf("HeaderMutationRulesFromProto() mismatch (-got +want):\n%s", diff)
	}
}

func (s) TestHeaderMutationRulesFromProto_Errors(t *testing.T) {
	tests := []struct {
		name          string
		mutationRules *v3mutationpb.HeaderMutationRules
		wantErr       string
	}{
		{
			name: "invalid allow expression",
			mutationRules: &v3mutationpb.HeaderMutationRules{
				AllowExpression: &v3matcherpb.RegexMatcher{Regex: "["},
			},
			wantErr: "httpfilter: error parsing regexp",
		},
		{
			name: "invalid disallow expression",
			mutationRules: &v3mutationpb.HeaderMutationRules{
				DisallowExpression: &v3matcherpb.RegexMatcher{Regex: "["},
			},
			wantErr: "httpfilter: error parsing regexp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := HeaderMutationRulesFromProto(tt.mutationRules)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("HeaderMutationRulesFromProto() error = %v, wantErr %q", err, tt.wantErr)
			}
		})
	}
}

func (s) TestHeaderMutationRules_ApplyAdditons(t *testing.T) {
	tests := []struct {
		name    string
		hmr     *HeaderMutationRules
		hvos    []*v3corepb.HeaderValueOption
		inputMD metadata.MD
		wantMD  metadata.MD
		wantErr bool
	}{
		{
			name: "NilReceiver",
			hmr:  nil,
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a", Value: "1"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{"a": []string{"1"}},
		},
		{
			name: "DisallowAll",
			hmr:  &HeaderMutationRules{DisallowAll: true},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a", Value: "1"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{"x": []string{"y"}},
			wantMD:  metadata.MD{"x": []string{"y"}},
		},
		{
			name: "DisallowExprMatchAndDisallowIsErrorIsFalse",
			hmr:  &HeaderMutationRules{DisallowExpr: regexp.MustCompile("^a$")},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a", Value: "1"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{},
		},
		{
			name: "DisallowExprMatchAndDisallowIsErrorIsTrue",
			hmr:  &HeaderMutationRules{DisallowExpr: regexp.MustCompile("^a$"), DisallowIsError: true},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a", Value: "1"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{},
			wantErr: true,
		},
		{
			name: "AllowExprMatch",
			hmr:  &HeaderMutationRules{AllowExpr: regexp.MustCompile("^a$")},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a", Value: "1"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
				{Header: &v3corepb.HeaderValue{Key: "b", Value: "2"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{"a": []string{"1"}},
		},
		{
			name: "AllowExprNoMatch",
			hmr:  &HeaderMutationRules{AllowExpr: regexp.MustCompile("^a$")},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "b", Value: "2"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{},
		},
		{
			name: "DisallowOverridesAllow",
			hmr:  &HeaderMutationRules{AllowExpr: regexp.MustCompile("."), DisallowExpr: regexp.MustCompile("^a$")},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a", Value: "1"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
				{Header: &v3corepb.HeaderValue{Key: "b", Value: "2"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{"b": []string{"2"}},
		},
		{
			name: "InvalidHeaderKeyPseudoHeader",
			hmr:  &HeaderMutationRules{},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: ":path", Value: "/"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{},
		},
		{
			name: "InvalidHeaderKeyHost",
			hmr:  &HeaderMutationRules{},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "host", Value: "example.com"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{},
		},
		{
			name: "InvalidHeaderKeyNotLowercase",
			hmr:  &HeaderMutationRules{},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "A", Value: "1"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{},
		},
		{
			name: "InvalidHeaderKeyTooLong",
			hmr:  &HeaderMutationRules{},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: strings.Repeat("a", 16385), Value: "1"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{},
		},
		{
			name: "InvalidHeaderValueTooLong",
			hmr:  &HeaderMutationRules{},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a", Value: strings.Repeat("1", 16385)}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{},
		},
		{
			name: "InvalidBinaryHeaderValueTooLong",
			hmr:  &HeaderMutationRules{},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a-bin", RawValue: make([]byte, 16385)}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{},
		},
		{
			name: "BinaryHeaderValue",
			hmr:  &HeaderMutationRules{},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a-bin", RawValue: []byte{1, 2, 3}}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{"a-bin": []string{string([]byte{1, 2, 3})}},
		},
		{
			name: "AppendIfExistsOrAdd_Exists",
			hmr:  &HeaderMutationRules{},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a", Value: "1"}, AppendAction: v3corepb.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{"a": []string{"0"}},
			wantMD:  metadata.MD{"a": []string{"0", "1"}},
		},
		{
			name: "AppendIfExistsOrAdd_Add",
			hmr:  &HeaderMutationRules{},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a", Value: "1"}, AppendAction: v3corepb.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{"a": []string{"1"}},
		},
		{
			name: "AddIfAbsent_Absent",
			hmr:  &HeaderMutationRules{},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a", Value: "1"}, AppendAction: v3corepb.HeaderValueOption_ADD_IF_ABSENT},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{"a": []string{"1"}},
		},
		{
			name: "AddIfAbsent_Present",
			hmr:  &HeaderMutationRules{},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a", Value: "1"}, AppendAction: v3corepb.HeaderValueOption_ADD_IF_ABSENT},
			},
			inputMD: metadata.MD{"a": []string{"1"}},
			wantMD:  metadata.MD{"a": []string{"1"}},
		},
		{
			name: "OverwriteIfExistsOrAdd_Absent",
			hmr:  &HeaderMutationRules{},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a", Value: "1"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{"a": []string{"1"}},
		},
		{
			name: "OverwriteIfExistsOrAdd_Present",
			hmr:  &HeaderMutationRules{},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a", Value: "1"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD},
			},
			inputMD: metadata.MD{"a": []string{"0"}},
			wantMD:  metadata.MD{"a": []string{"1"}},
		},
		{
			name: "OverwriteIfExists_Absent",
			hmr:  &HeaderMutationRules{},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a", Value: "1"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS},
			},
			inputMD: metadata.MD{},
			wantMD:  metadata.MD{},
		},
		{
			name: "OverwriteIfExists_Present",
			hmr:  &HeaderMutationRules{},
			hvos: []*v3corepb.HeaderValueOption{
				{Header: &v3corepb.HeaderValue{Key: "a", Value: "1"}, AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS},
			},
			inputMD: metadata.MD{"a": []string{"0"}},
			wantMD:  metadata.MD{"a": []string{"1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.hmr.ApplyAdditions(tt.hvos, tt.inputMD); (err != nil) != tt.wantErr {
				t.Fatalf("ApplyAdditions() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if diff := cmp.Diff(tt.wantMD, tt.inputMD); diff != "" {
				t.Fatalf("ApplyAdditions() returned diff in metadata (-want +got):\n%s", diff)
			}
		})
	}
}

func (s) TestHeaderMutationRules_ApplyRemovals(t *testing.T) {
	tests := []struct {
		name            string
		hmr             *HeaderMutationRules
		headersToRemove []string
		inputMD         metadata.MD
		wantMD          metadata.MD
		wantErr         bool
	}{
		{
			name:            "NilReceiver",
			hmr:             nil,
			headersToRemove: []string{"a"},
			inputMD:         metadata.MD{"a": []string{"1"}},
			wantMD:          metadata.MD{},
		},
		{
			name:            "DisallowAll",
			hmr:             &HeaderMutationRules{DisallowAll: true},
			headersToRemove: []string{"a"},
			inputMD:         metadata.MD{"a": []string{"1"}},
			wantMD:          metadata.MD{"a": []string{"1"}},
		},
		{
			name:            "DisallowExprMatchAndDisallowIsErrorIsFalse",
			hmr:             &HeaderMutationRules{DisallowExpr: regexp.MustCompile("^a$")},
			headersToRemove: []string{"a"},
			inputMD:         metadata.MD{"a": []string{"1"}},
			wantMD:          metadata.MD{"a": []string{"1"}},
		},
		{
			name:            "DisallowExprMatchAndDisallowIsErrorIsTrue",
			hmr:             &HeaderMutationRules{DisallowExpr: regexp.MustCompile("^a$"), DisallowIsError: true},
			headersToRemove: []string{"a"},
			inputMD:         metadata.MD{"a": []string{"1"}},
			wantErr:         true,
		},
		{
			name:            "AllowExprMatch",
			hmr:             &HeaderMutationRules{AllowExpr: regexp.MustCompile("^a$")},
			headersToRemove: []string{"a", "b"},
			inputMD:         metadata.MD{"a": []string{"1"}, "b": []string{"2"}},
			wantMD:          metadata.MD{"b": []string{"2"}},
		},
		{
			name:            "AllowExprNoMatch",
			hmr:             &HeaderMutationRules{AllowExpr: regexp.MustCompile("^a$")},
			headersToRemove: []string{"b"},
			inputMD:         metadata.MD{"a": []string{"1"}, "b": []string{"2"}},
			wantMD:          metadata.MD{"a": []string{"1"}, "b": []string{"2"}},
		},
		{
			name:            "DisallowOverridesAllow",
			hmr:             &HeaderMutationRules{AllowExpr: regexp.MustCompile("."), DisallowExpr: regexp.MustCompile("^a$")},
			headersToRemove: []string{"a", "b"},
			inputMD:         metadata.MD{"a": []string{"1"}, "b": []string{"2"}},
			wantMD:          metadata.MD{"a": []string{"1"}},
		},
		{
			name:            "InvalidHeaderKeyPseudoHeader",
			hmr:             &HeaderMutationRules{},
			headersToRemove: []string{":path"},
			inputMD:         metadata.MD{":path": []string{"/"}},
			wantMD:          metadata.MD{":path": []string{"/"}},
		},
		{
			name:            "InvalidHeaderKeyHost",
			hmr:             &HeaderMutationRules{},
			headersToRemove: []string{"host"},
			inputMD:         metadata.MD{"host": []string{"example.com"}},
			wantMD:          metadata.MD{"host": []string{"example.com"}},
		},
		{
			name:            "InvalidHeaderKeyNotLowercase",
			hmr:             &HeaderMutationRules{},
			headersToRemove: []string{"A"},
			inputMD:         metadata.MD{"A": []string{"1"}},
			wantMD:          metadata.MD{"A": []string{"1"}},
		},
		{
			name:            "InvalidHeaderKeyTooLong",
			hmr:             &HeaderMutationRules{},
			headersToRemove: []string{strings.Repeat("a", 16385)},
			inputMD:         metadata.MD{strings.Repeat("a", 16385): []string{"1"}},
			wantMD:          metadata.MD{strings.Repeat("a", 16385): []string{"1"}},
		},
		{
			name:            "HeaderExists",
			hmr:             &HeaderMutationRules{},
			headersToRemove: []string{"a"},
			inputMD:         metadata.MD{"a": []string{"1"}},
			wantMD:          metadata.MD{},
		},
		{
			name:            "HeaderDoesNotExist",
			hmr:             &HeaderMutationRules{},
			headersToRemove: []string{"a"},
			inputMD:         metadata.MD{"b": []string{"2"}},
			wantMD:          metadata.MD{"b": []string{"2"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.hmr.ApplyRemovals(tt.headersToRemove, tt.inputMD); (err != nil) != tt.wantErr {
				t.Fatalf("ApplyRemovals() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if diff := cmp.Diff(tt.wantMD, tt.inputMD); diff != "" {
				t.Fatalf("ApplyRemovals() returned diff in metadata (-want +got):\n%s", diff)
			}
		})
	}
}

func (s) TestConstructHeaderMap(t *testing.T) {
	tests := []struct {
		name              string
		md                metadata.MD
		added             [][]string
		allowedHeaders    []matcher.StringMatcher
		disallowedHeaders []matcher.StringMatcher
		wantHeaderMap     *v3corepb.HeaderMap
	}{
		{
			name:          "NoHeaders",
			md:            metadata.MD{},
			wantHeaderMap: nil,
		},
		{
			name: "NoAllowedHeaders",
			md: metadata.MD{
				"a": {"1"},
				"b": {"2"},
			},
			wantHeaderMap: &v3corepb.HeaderMap{
				Headers: []*v3corepb.HeaderValue{
					{Key: "a", RawValue: []byte("1")},
					{Key: "b", RawValue: []byte("2")},
				},
			},
		},
		{
			name: "WithMultipleValues_ForSingleHeader",
			md: metadata.MD{
				"a": {"1", "11"},
				"b": {"2"},
			},
			wantHeaderMap: &v3corepb.HeaderMap{
				Headers: []*v3corepb.HeaderValue{
					{Key: "a", RawValue: []byte("1")},
					{Key: "a", RawValue: []byte("11")},
					{Key: "b", RawValue: []byte("2")},
				},
			},
		},
		{
			name: "WithAllowedHeaders",
			md: metadata.MD{
				"a": {"1"},
				"b": {"2"},
				"c": {"3"},
			},
			allowedHeaders: []matcher.StringMatcher{
				matcher.NewExactStringMatcher("a", false),
				matcher.NewPrefixStringMatcher("c", false),
			},
			wantHeaderMap: &v3corepb.HeaderMap{
				Headers: []*v3corepb.HeaderValue{
					{Key: "a", RawValue: []byte("1")},
					{Key: "c", RawValue: []byte("3")},
				},
			},
		},
		{
			name: "WithDisallowedHeaders",
			md: metadata.MD{
				"a": {"1"},
				"b": {"2"},
				"c": {"3"},
			},
			disallowedHeaders: []matcher.StringMatcher{
				matcher.NewExactStringMatcher("b", false),
				matcher.NewPrefixStringMatcher("c", false),
			},
			wantHeaderMap: &v3corepb.HeaderMap{
				Headers: []*v3corepb.HeaderValue{
					{Key: "a", RawValue: []byte("1")},
				},
			},
		},
		{
			name: "WithAllowedAndDisallowedHeaders",
			md: metadata.MD{
				"a": {"1"},
				"b": {"2"},
				"c": {"3"},
				"d": {"4"},
			},
			allowedHeaders: []matcher.StringMatcher{
				matcher.NewExactStringMatcher("a", false),
				matcher.NewPrefixStringMatcher("b", false),
				matcher.NewPrefixStringMatcher("c", false),
			},
			disallowedHeaders: []matcher.StringMatcher{
				matcher.NewExactStringMatcher("b", false),
				matcher.NewPrefixStringMatcher("c", false),
			},
			wantHeaderMap: &v3corepb.HeaderMap{
				Headers: []*v3corepb.HeaderValue{
					{Key: "a", RawValue: []byte("1")},
				},
			},
		},
		{
			name: "WithAddedHeaders",
			added: [][]string{
				{"a", "1", "b", "2"},
				{"c", "3"},
			},
			wantHeaderMap: &v3corepb.HeaderMap{
				Headers: []*v3corepb.HeaderValue{
					{Key: "a", RawValue: []byte("1")},
					{Key: "b", RawValue: []byte("2")},
					{Key: "c", RawValue: []byte("3")},
				},
			},
		},
		{
			name: "WithAddedAndBaseHeaders",
			md: metadata.MD{
				"a": {"1"},
			},
			added: [][]string{
				{"B", "2"}, // Lowercase check
			},
			wantHeaderMap: &v3corepb.HeaderMap{
				Headers: []*v3corepb.HeaderValue{
					{Key: "a", RawValue: []byte("1")},
					{Key: "b", RawValue: []byte("2")},
				},
			},
		},
		{
			name: "WithAddedHeadersAndFiltering",
			added: [][]string{
				{"a", "1", "b", "2", "c", "3"},
			},
			allowedHeaders: []matcher.StringMatcher{
				matcher.NewExactStringMatcher("a", false),
				matcher.NewExactStringMatcher("b", false),
			},
			disallowedHeaders: []matcher.StringMatcher{
				matcher.NewExactStringMatcher("b", false),
			},
			wantHeaderMap: &v3corepb.HeaderMap{
				Headers: []*v3corepb.HeaderValue{
					{Key: "a", RawValue: []byte("1")},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHeaderMap := ConstructHeaderMap(tt.md, tt.added, tt.allowedHeaders, tt.disallowedHeaders)
			if gotHeaderMap != nil {
				sort.Slice(gotHeaderMap.Headers, func(i, j int) bool {
					return gotHeaderMap.Headers[i].Key < gotHeaderMap.Headers[j].Key
				})
			}
			if diff := cmp.Diff(tt.wantHeaderMap, gotHeaderMap, protocmp.Transform()); diff != "" {
				t.Fatalf("constructHeaderMap() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
