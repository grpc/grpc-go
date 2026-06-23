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
	"reflect"
	"testing"

	v3mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	appendOrAdd = v3corepb.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD
	addIfAbsent = v3corepb.HeaderValueOption_ADD_IF_ABSENT
	overwrite   = v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD
	overwriteIf = v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS
)

// hvo builds a HeaderValueOption with the legacy value field.
func hvo(key, val string, action v3corepb.HeaderValueOption_HeaderAppendAction) *v3corepb.HeaderValueOption {
	return &v3corepb.HeaderValueOption{
		Header:       &v3corepb.HeaderValue{Key: key, Value: val},
		AppendAction: action,
	}
}

func TestHeaderMutator_Mutate(t *testing.T) {
	tests := []struct {
		name      string
		rules     *v3mutationpb.HeaderMutationRules
		input     metadata.MD
		mutations []*v3corepb.HeaderValueOption
		want      metadata.MD
		wantErr   bool
	}{
		{
			name:      "append_to_absent_adds",
			input:     metadata.MD{"existing": {"v"}},
			mutations: []*v3corepb.HeaderValueOption{hvo("new", "a", appendOrAdd)},
			want:      metadata.MD{"existing": {"v"}, "new": {"a"}},
		},
		{
			name:      "append_to_existing_appends",
			input:     metadata.MD{"k": {"a"}},
			mutations: []*v3corepb.HeaderValueOption{hvo("k", "b", appendOrAdd)},
			want:      metadata.MD{"k": {"a", "b"}},
		},
		{
			name:      "add_if_absent_when_absent_adds",
			input:     metadata.MD{},
			mutations: []*v3corepb.HeaderValueOption{hvo("k", "a", addIfAbsent)},
			want:      metadata.MD{"k": {"a"}},
		},
		{
			name:      "add_if_absent_when_present_noop",
			input:     metadata.MD{"k": {"a"}},
			mutations: []*v3corepb.HeaderValueOption{hvo("k", "b", addIfAbsent)},
			want:      metadata.MD{"k": {"a"}},
		},
		{
			name:      "overwrite_or_add_when_present_overwrites",
			input:     metadata.MD{"k": {"a", "b"}},
			mutations: []*v3corepb.HeaderValueOption{hvo("k", "c", overwrite)},
			want:      metadata.MD{"k": {"c"}},
		},
		{
			name:      "overwrite_or_add_when_absent_adds",
			input:     metadata.MD{},
			mutations: []*v3corepb.HeaderValueOption{hvo("k", "c", overwrite)},
			want:      metadata.MD{"k": {"c"}},
		},
		{
			name:      "overwrite_if_exists_when_present_overwrites",
			input:     metadata.MD{"k": {"a"}},
			mutations: []*v3corepb.HeaderValueOption{hvo("k", "c", overwriteIf)},
			want:      metadata.MD{"k": {"c"}},
		},
		{
			name:      "overwrite_if_exists_when_absent_noop",
			input:     metadata.MD{},
			mutations: []*v3corepb.HeaderValueOption{hvo("k", "c", overwriteIf)},
			want:      metadata.MD{},
		},
		{
			// gRFC A102: headers are kept even when a mutation yields an empty
			// value; keep_empty_value is intentionally unsupported.
			name:      "empty_value_is_kept_on_existing_key",
			input:     metadata.MD{"k": {"a"}},
			mutations: []*v3corepb.HeaderValueOption{hvo("k", "", overwrite)},
			want:      metadata.MD{"k": {""}},
		},
		{
			name:      "empty_value_is_kept_on_new_key",
			input:     metadata.MD{},
			mutations: []*v3corepb.HeaderValueOption{hvo("new", "", appendOrAdd)},
			want:      metadata.MD{"new": {""}},
		},
		{
			name:  "keep_empty_value_field_is_ignored",
			input: metadata.MD{"k": {"a"}},
			mutations: []*v3corepb.HeaderValueOption{{
				Header:         &v3corepb.HeaderValue{Key: "k", Value: ""},
				AppendAction:   overwrite,
				KeepEmptyValue: true,
			}},
			want: metadata.MD{"k": {""}},
		},
		{
			name:  "raw_value_takes_precedence_over_value",
			input: metadata.MD{},
			mutations: []*v3corepb.HeaderValueOption{{
				Header:       &v3corepb.HeaderValue{Key: "k", Value: "legacy", RawValue: []byte("raw")},
				AppendAction: overwrite,
			}},
			want: metadata.MD{"k": {"raw"}},
		},
		{
			// An explicitly empty raw_value is still set (it is non-nil), and
			// must not fall back to the legacy value field.
			name:  "empty_raw_value_overrides_legacy_value",
			input: metadata.MD{},
			mutations: []*v3corepb.HeaderValueOption{{
				Header:       &v3corepb.HeaderValue{Key: "k", Value: "legacy", RawValue: []byte{}},
				AppendAction: overwrite,
			}},
			want: metadata.MD{"k": {""}},
		},
		{
			name:      "key_is_lowercased",
			input:     metadata.MD{},
			mutations: []*v3corepb.HeaderValueOption{hvo("MixedCase", "a", overwrite)},
			want:      metadata.MD{"mixedcase": {"a"}},
		},
		{
			name:      "nil_header_is_skipped",
			input:     metadata.MD{"k": {"a"}},
			mutations: []*v3corepb.HeaderValueOption{{AppendAction: overwrite}},
			want:      metadata.MD{"k": {"a"}},
		},
		{
			name:      "pseudo_header_never_mutated",
			input:     metadata.MD{},
			mutations: []*v3corepb.HeaderValueOption{hvo(":path", "/new", overwrite)},
			want:      metadata.MD{},
		},
		{
			name:      "host_header_never_mutated",
			input:     metadata.MD{},
			mutations: []*v3corepb.HeaderValueOption{hvo("host", "evil", overwrite)},
			want:      metadata.MD{},
		},
		{
			// gRFC A102 only hard-protects ":"-prefixed and "host"; "grpc-*" is
			// subject to the allow/disallow rules and may be mutated.
			name:      "grpc_prefixed_header_is_mutable",
			input:     metadata.MD{},
			mutations: []*v3corepb.HeaderValueOption{hvo("grpc-foo", "a", overwrite)},
			want:      metadata.MD{"grpc-foo": {"a"}},
		},
		{
			name:      "disallow_all_ignored_when_not_error",
			rules:     &v3mutationpb.HeaderMutationRules{DisallowAll: wrapperspb.Bool(true)},
			input:     metadata.MD{"k": {"a"}},
			mutations: []*v3corepb.HeaderValueOption{hvo("k", "b", overwrite)},
			want:      metadata.MD{"k": {"a"}},
		},
		{
			name:      "disallow_all_errors_when_is_error",
			rules:     &v3mutationpb.HeaderMutationRules{DisallowAll: wrapperspb.Bool(true), DisallowIsError: wrapperspb.Bool(true)},
			input:     metadata.MD{"k": {"a"}},
			mutations: []*v3corepb.HeaderValueOption{hvo("k", "b", overwrite)},
			wantErr:   true,
		},
		{
			name: "disallow_expr_ignored_when_not_error",
			rules: &v3mutationpb.HeaderMutationRules{
				DisallowExpression: &v3matcherpb.RegexMatcher{Regex: ".*-blocked"},
			},
			input:     metadata.MD{},
			mutations: []*v3corepb.HeaderValueOption{hvo("k-blocked", "a", overwrite)},
			want:      metadata.MD{},
		},
		{
			name: "disallow_expr_errors_when_is_error",
			rules: &v3mutationpb.HeaderMutationRules{
				DisallowExpression: &v3matcherpb.RegexMatcher{Regex: ".*-blocked"},
				DisallowIsError:    wrapperspb.Bool(true),
			},
			input:     metadata.MD{},
			mutations: []*v3corepb.HeaderValueOption{hvo("k-blocked", "a", overwrite)},
			wantErr:   true,
		},
		{
			name: "allow_expr_blocks_non_matching_when_not_error",
			rules: &v3mutationpb.HeaderMutationRules{
				AllowExpression: &v3matcherpb.RegexMatcher{Regex: "allowed-.*"},
			},
			input:     metadata.MD{},
			mutations: []*v3corepb.HeaderValueOption{hvo("other", "a", overwrite)},
			want:      metadata.MD{},
		},
		{
			name: "allow_expr_permits_matching",
			rules: &v3mutationpb.HeaderMutationRules{
				AllowExpression: &v3matcherpb.RegexMatcher{Regex: "allowed-.*"},
			},
			input:     metadata.MD{},
			mutations: []*v3corepb.HeaderValueOption{hvo("allowed-k", "a", overwrite)},
			want:      metadata.MD{"allowed-k": {"a"}},
		},
		{
			name: "disallow_expr_overrides_allow_expr",
			rules: &v3mutationpb.HeaderMutationRules{
				AllowExpression:    &v3matcherpb.RegexMatcher{Regex: "allowed-.*"},
				DisallowExpression: &v3matcherpb.RegexMatcher{Regex: "allowed-secret"},
			},
			input:     metadata.MD{},
			mutations: []*v3corepb.HeaderValueOption{hvo("allowed-secret", "a", overwrite)},
			want:      metadata.MD{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutator, err := NewHeaderMutatorFromProto(test.rules)
			if err != nil {
				t.Fatalf("NewHeaderMutatorFromProto() returned error: %v", err)
			}
			got, err := mutator.Mutate(test.input, test.mutations)
			if (err != nil) != test.wantErr {
				t.Fatalf("Mutate() error = %v, wantErr = %v", err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Mutate() result mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestHeaderMutator_DoesNotMutateInput verifies that Mutate operates on a copy
// and leaves the caller's metadata untouched.
func TestHeaderMutator_DoesNotMutateInput(t *testing.T) {
	mutator := NewHeaderMutator(HeaderMutationRules{})
	input := metadata.MD{"k": {"a"}}
	if _, err := mutator.Mutate(input, []*v3corepb.HeaderValueOption{
		hvo("k", "b", appendOrAdd),
		hvo("new", "c", overwrite),
	}); err != nil {
		t.Fatalf("Mutate() returned error: %v", err)
	}
	want := metadata.MD{"k": {"a"}}
	if diff := cmp.Diff(want, input); diff != "" {
		t.Errorf("Mutate() modified the input metadata (-want +got):\n%s", diff)
	}
}

// TestHeaderMutator_NoOpDoesNotCopy verifies the copy-on-write behavior: a
// call that applies no mutations returns the input metadata without allocating
// a copy.
func TestHeaderMutator_NoOpDoesNotCopy(t *testing.T) {
	mutator := NewHeaderMutator(HeaderMutationRules{})
	input := metadata.MD{"k": {"v"}}
	tests := map[string][]*v3corepb.HeaderValueOption{
		"no_mutations":      nil,
		"all_skipped":       {hvo(":path", "/x", overwrite)}, // pseudo-header
		"nil_header_option": {{AppendAction: overwrite}},
	}
	for name, mutations := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := mutator.Mutate(input, mutations)
			if err != nil {
				t.Fatalf("Mutate() returned error: %v", err)
			}
			if reflect.ValueOf(got).Pointer() != reflect.ValueOf(input).Pointer() {
				t.Errorf("Mutate() copied the metadata for a no-op call; want the input returned without copying")
			}
		})
	}
}

func TestNewHeaderMutatorFromProto_InvalidRegex(t *testing.T) {
	_, err := NewHeaderMutatorFromProto(&v3mutationpb.HeaderMutationRules{
		AllowExpression: &v3matcherpb.RegexMatcher{Regex: "("},
	})
	if err == nil {
		t.Fatal("NewHeaderMutatorFromProto() succeeded for invalid regex, want error")
	}
}
