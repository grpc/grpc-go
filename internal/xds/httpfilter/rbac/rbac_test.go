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

package rbac

import (
	"testing"

	"google.golang.org/grpc/internal/grpctest"

	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	rpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func headerMatcher(name string) *v3routepb.HeaderMatcher {
	return &v3routepb.HeaderMatcher{
		Name:                 name,
		HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PresentMatch{PresentMatch: true},
	}
}

func headerPermission(name string) *v3rbacpb.Permission {
	return &v3rbacpb.Permission{Rule: &v3rbacpb.Permission_Header{Header: headerMatcher(name)}}
}

func headerPrincipal(name string) *v3rbacpb.Principal {
	return &v3rbacpb.Principal{Identifier: &v3rbacpb.Principal_Header{Header: headerMatcher(name)}}
}

func rbacConfig(perm *v3rbacpb.Permission, principal *v3rbacpb.Principal) *rpb.RBAC {
	return &rpb.RBAC{Rules: &v3rbacpb.RBAC{
		Action: v3rbacpb.RBAC_ALLOW,
		Policies: map[string]*v3rbacpb.Policy{
			"test-policy": {
				Permissions: []*v3rbacpb.Permission{perm},
				Principals:  []*v3rbacpb.Principal{principal},
			},
		},
	}}
}

// TestNestedHeaderMatcherValidation checks that a header matcher for :scheme or
// a grpc- prefixed name is rejected even when it is nested inside an and/or/not
// rule, as A41 requires.
func (s) TestNestedHeaderMatcherValidation(t *testing.T) {
	anyPermission := &v3rbacpb.Permission{Rule: &v3rbacpb.Permission_Any{Any: true}}
	anyPrincipal := &v3rbacpb.Principal{Identifier: &v3rbacpb.Principal_Any{Any: true}}

	tests := []struct {
		name string
		cfg  *rpb.RBAC
	}{
		{
			name: "permission and_rules :scheme",
			cfg: rbacConfig(&v3rbacpb.Permission{Rule: &v3rbacpb.Permission_AndRules{AndRules: &v3rbacpb.Permission_Set{
				Rules: []*v3rbacpb.Permission{headerPermission(":scheme")},
			}}}, anyPrincipal),
		},
		{
			name: "permission not_rule grpc- prefix",
			cfg:  rbacConfig(&v3rbacpb.Permission{Rule: &v3rbacpb.Permission_NotRule{NotRule: headerPermission("grpc-timeout")}}, anyPrincipal),
		},
		{
			name: "principal or_ids :scheme",
			cfg: rbacConfig(anyPermission, &v3rbacpb.Principal{Identifier: &v3rbacpb.Principal_OrIds{OrIds: &v3rbacpb.Principal_Set{
				Ids: []*v3rbacpb.Principal{headerPrincipal(":scheme")},
			}}}),
		},
		{
			name: "principal not_id grpc- prefix",
			cfg:  rbacConfig(anyPermission, &v3rbacpb.Principal{Identifier: &v3rbacpb.Principal_NotId{NotId: headerPrincipal("grpc-encoding")}}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseConfig(test.cfg); err == nil {
				t.Fatalf("parseConfig() succeeded; want error rejecting a nested :scheme/grpc- header matcher")
			}
		})
	}
}

// TestNestedHostHeaderAliasing checks that a "host" header matcher nested inside
// an and/or/not rule is rewritten to ":authority", so it behaves the same as a
// top-level host matcher (A41 host/:authority equivalence).
func (s) TestNestedHostHeaderAliasing(t *testing.T) {
	perm := &v3rbacpb.Permission{Rule: &v3rbacpb.Permission_NotRule{NotRule: headerPermission("host")}}
	principal := &v3rbacpb.Principal{Identifier: &v3rbacpb.Principal_AndIds{AndIds: &v3rbacpb.Principal_Set{
		Ids: []*v3rbacpb.Principal{headerPrincipal("host")},
	}}}

	if _, err := parseConfig(rbacConfig(perm, principal)); err != nil {
		t.Fatalf("parseConfig() failed: %v", err)
	}

	gotPerm := perm.GetRule().(*v3rbacpb.Permission_NotRule).NotRule.GetRule().(*v3rbacpb.Permission_Header).Header.GetName()
	if gotPerm != ":authority" {
		t.Errorf("Nested permission host matcher name = %q, want %q", gotPerm, ":authority")
	}
	gotPrincipal := principal.GetIdentifier().(*v3rbacpb.Principal_AndIds).AndIds.GetIds()[0].GetIdentifier().(*v3rbacpb.Principal_Header).Header.GetName()
	if gotPrincipal != ":authority" {
		t.Errorf("Nested principal host matcher name = %q, want %q", gotPrincipal, ":authority")
	}
}

// TestHeaderMatcherNameIsLowercased checks that a header matcher name is
// lowercased before it reaches the matching engine. gRPC lowercases every
// metadata key, so a name that carries an uppercase character matches no
// header at all, and a DENY policy using one fails open.
func (s) TestHeaderMatcherNameIsLowercased(t *testing.T) {
	tests := []struct {
		name     string
		wantName string
	}{
		{name: "X-Role", wantName: "x-role"},
		{name: "USER-AGENT", wantName: "user-agent"},
		// The host to :authority alias must not depend on the case either.
		{name: "Host", wantName: ":authority"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// The permission matcher is nested and the principal matcher is at
			// the top level, so both the recursive walk and the top level are
			// covered.
			permHeader, principalHeader := headerMatcher(test.name), headerMatcher(test.name)
			perm := &v3rbacpb.Permission{Rule: &v3rbacpb.Permission_NotRule{
				NotRule: &v3rbacpb.Permission{Rule: &v3rbacpb.Permission_Header{Header: permHeader}},
			}}
			principal := &v3rbacpb.Principal{Identifier: &v3rbacpb.Principal_Header{Header: principalHeader}}

			if _, err := parseConfig(rbacConfig(perm, principal)); err != nil {
				t.Fatalf("parseConfig() failed: %v", err)
			}
			if got := permHeader.GetName(); got != test.wantName {
				t.Errorf("Permission header matcher name = %q, want %q", got, test.wantName)
			}
			if got := principalHeader.GetName(); got != test.wantName {
				t.Errorf("Principal header matcher name = %q, want %q", got, test.wantName)
			}
		})
	}
}

// TestHeaderMatcherValidationIsCaseInsensitive checks that the A41 rejection of
// :scheme and grpc- prefixed header matchers cannot be evaded by spelling the
// name in another case.
func (s) TestHeaderMatcherValidationIsCaseInsensitive(t *testing.T) {
	anyPermission := &v3rbacpb.Permission{Rule: &v3rbacpb.Permission_Any{Any: true}}
	anyPrincipal := &v3rbacpb.Principal{Identifier: &v3rbacpb.Principal_Any{Any: true}}

	for _, name := range []string{":Scheme", "Grpc-Timeout", "GRPC-STATUS"} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseConfig(rbacConfig(headerPermission(name), anyPrincipal)); err == nil {
				t.Errorf("parseConfig() succeeded for permission header matcher %q; want error rejecting a :scheme/grpc- header matcher", name)
			}
			if _, err := parseConfig(rbacConfig(anyPermission, headerPrincipal(name))); err == nil {
				t.Errorf("parseConfig() succeeded for principal header matcher %q; want error rejecting a :scheme/grpc- header matcher", name)
			}
		})
	}
}
