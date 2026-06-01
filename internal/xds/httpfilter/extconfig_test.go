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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
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
		AllowExpr:       regexp.MustCompile("^allow$"),
		DisallowExpr:    regexp.MustCompile("^disallow$"),
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
