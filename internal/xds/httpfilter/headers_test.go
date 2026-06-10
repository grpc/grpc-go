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
	"testing"

	v3mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestHeaderMutator_Mutate(t *testing.T) {
	mr := &v3mutationpb.HeaderMutationRules{
		AllowExpression:    &v3matcherpb.RegexMatcher{Regex: "allowed-.*"},
		DisallowExpression: &v3matcherpb.RegexMatcher{Regex: ".*-blocked"},
		DisallowIsError:    wrapperspb.Bool(true),
	}

	mutator, err := NewHeaderMutatorFromProto(mr)
	if err != nil {
		t.Fatalf("NewHeaderMutatorFromProto() returned error: %v", err)
	}

	md := metadata.Pairs("existing-key", "existing-val")

	mutations := []*v3corepb.HeaderValueOption{
		{
			Header:       &v3corepb.HeaderValue{Key: "allowed-header", Value: "mutated-val"},
			AppendAction: v3corepb.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
		},
	}

	res, err := mutator.Mutate(md, mutations)
	if err != nil {
		t.Fatalf("Mutate() returned unexpected error: %v", err)
	}

	if got := res.Get("allowed-header"); len(got) != 1 || got[0] != "mutated-val" {
		t.Errorf("Mutate() allowed-header got: %v, want: %v", got, []string{"mutated-val"})
	}

	disallowedMutations := []*v3corepb.HeaderValueOption{
		{
			Header: &v3corepb.HeaderValue{Key: "header-blocked", Value: "val"},
		},
	}
	_, err = mutator.Mutate(md, disallowedMutations)
	if err == nil {
		t.Fatal("Mutate() succeeded for disallowed header with DisallowIsError=true, want error")
	}

	systemMutations := []*v3corepb.HeaderValueOption{
		{
			Header: &v3corepb.HeaderValue{Key: ":path", Value: "/new/path"},
		},
	}
	res, err = mutator.Mutate(md, systemMutations)
	if err != nil {
		t.Fatalf("Mutate() returned unexpected error for system header: %v", err)
	}
	if got := res.Get(":path"); len(got) != 0 {
		t.Errorf("System header mutation applied; got: %v, want: []", got)
	}
}

func TestHeaderMutator_KeepEmptyValue(t *testing.T) {
	mr := &v3mutationpb.HeaderMutationRules{}
	mutator, err := NewHeaderMutatorFromProto(mr)
	if err != nil {
		t.Fatalf("NewHeaderMutatorFromProto() returned error: %v", err)
	}

	t.Run("KeepEmptyValue = false (default)", func(t *testing.T) {
		md := metadata.Pairs("existing-key", "existing-val")
		mutations := []*v3corepb.HeaderValueOption{
			{
				Header:       &v3corepb.HeaderValue{Key: "existing-key", Value: ""},
				AppendAction: v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
			},
			{
				Header:       &v3corepb.HeaderValue{Key: "new-key", Value: ""},
				AppendAction: v3corepb.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
			},
		}
		res, err := mutator.Mutate(md, mutations)
		if err != nil {
			t.Fatalf("Mutate() returned error: %v", err)
		}
		if got := res.Get("existing-key"); len(got) != 0 {
			t.Errorf("existing-key got: %v, want: []", got)
		}
		if got := res.Get("new-key"); len(got) != 0 {
			t.Errorf("new-key got: %v, want: []", got)
		}
	})

	t.Run("KeepEmptyValue = true", func(t *testing.T) {
		md := metadata.Pairs("existing-key", "existing-val")
		mutations := []*v3corepb.HeaderValueOption{
			{
				Header:         &v3corepb.HeaderValue{Key: "existing-key", Value: ""},
				AppendAction:   v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
				KeepEmptyValue: true,
			},
			{
				Header:         &v3corepb.HeaderValue{Key: "new-key", Value: ""},
				AppendAction:   v3corepb.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
				KeepEmptyValue: true,
			},
		}
		res, err := mutator.Mutate(md, mutations)
		if err != nil {
			t.Fatalf("Mutate() returned error: %v", err)
		}
		if got := res.Get("existing-key"); len(got) != 1 || got[0] != "" {
			t.Errorf("existing-key got: %v, want: %v", got, []string{""})
		}
		if got := res.Get("new-key"); len(got) != 1 || got[0] != "" {
			t.Errorf("new-key got: %v, want: %v", got, []string{""})
		}
	})
}
