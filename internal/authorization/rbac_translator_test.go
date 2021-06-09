/*
 *
 * Copyright 2021 gRPC authors.
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

package authz

import (
	"testing"

	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
)

func TestTranslatePolicy(t *testing.T) {
	tests := map[string]struct {
		authzPolicy     string
		wantErr         string
		wantDenyPolicy  *v3rbacpb.RBAC
		wantAllowPolicy *v3rbacpb.RBAC
	}{
		"valid policy": {
			authzPolicy: `{
						"name": "authz",
						"deny_rules": [
						{
							"name": "deny_policy_1",
							"source": {								
								"principals":[
								"spiffe://foo.abc",
								"spiffe://bar*",
								"*baz",
								"spiffe://abc.*.com"
								]
							}
						}],
						"allow_rules": [
						{
							"name": "allow_policy_1",
							"source": {
								"principals":[
								"*"
								]
							},
							"request": {
								"paths": [
								"path-foo*"
								]
							}
						},
						{
							"name": "allow_policy_2",
							"request": {
								"paths": [
								"path-bar",
								"*baz"
								],
								"headers": [
								{
									"key": "key-1",
									"values": ["foo", "*bar"]
								},
								{
									"key": "key-2",
									"values": ["baz*"]
								}
								]
							}
						}]
					}`,
			wantErr: "",
			wantDenyPolicy: &v3rbacpb.RBAC{Action: v3rbacpb.RBAC_DENY, Policies: map[string]*v3rbacpb.Policy{
				"authz_deny_policy_1": {
					Principals: []*v3rbacpb.Principal{
						{Identifier: &v3rbacpb.Principal_AndIds{AndIds: &v3rbacpb.Principal_Set{
							Ids: []*v3rbacpb.Principal{
								{Identifier: &v3rbacpb.Principal_OrIds{OrIds: &v3rbacpb.Principal_Set{
									Ids: []*v3rbacpb.Principal{
										{Identifier: &v3rbacpb.Principal_Authenticated_{
											Authenticated: &v3rbacpb.Principal_Authenticated{PrincipalName: &v3matcherpb.StringMatcher{
												MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "spiffe://foo.abc"}}}}},
										{Identifier: &v3rbacpb.Principal_Authenticated_{
											Authenticated: &v3rbacpb.Principal_Authenticated{PrincipalName: &v3matcherpb.StringMatcher{
												MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: "spiffe://bar"}}}}},
										{Identifier: &v3rbacpb.Principal_Authenticated_{
											Authenticated: &v3rbacpb.Principal_Authenticated{PrincipalName: &v3matcherpb.StringMatcher{
												MatchPattern: &v3matcherpb.StringMatcher_Suffix{Suffix: "baz"}}}}},
										{Identifier: &v3rbacpb.Principal_Authenticated_{
											Authenticated: &v3rbacpb.Principal_Authenticated{PrincipalName: &v3matcherpb.StringMatcher{
												MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "spiffe://abc.*.com"}}}}},
									}}}}},
						}}}},
					Permissions: []*v3rbacpb.Permission{
						{Rule: &v3rbacpb.Permission_Any{Any: true}}},
				},
			}},
			wantAllowPolicy: &v3rbacpb.RBAC{Action: v3rbacpb.RBAC_ALLOW, Policies: map[string]*v3rbacpb.Policy{
				"authz_allow_policy_1": {
					Principals: []*v3rbacpb.Principal{
						{Identifier: &v3rbacpb.Principal_AndIds{AndIds: &v3rbacpb.Principal_Set{
							Ids: []*v3rbacpb.Principal{
								{Identifier: &v3rbacpb.Principal_OrIds{OrIds: &v3rbacpb.Principal_Set{
									Ids: []*v3rbacpb.Principal{
										{Identifier: &v3rbacpb.Principal_Authenticated_{
											Authenticated: &v3rbacpb.Principal_Authenticated{PrincipalName: &v3matcherpb.StringMatcher{
												MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: ""}}}}},
									}}}}},
						}}}},
					Permissions: []*v3rbacpb.Permission{
						{Rule: &v3rbacpb.Permission_AndRules{AndRules: &v3rbacpb.Permission_Set{
							Rules: []*v3rbacpb.Permission{
								{Rule: &v3rbacpb.Permission_OrRules{OrRules: &v3rbacpb.Permission_Set{
									Rules: []*v3rbacpb.Permission{
										{Rule: &v3rbacpb.Permission_UrlPath{
											UrlPath: &v3matcherpb.PathMatcher{Rule: &v3matcherpb.PathMatcher_Path{Path: &v3matcherpb.StringMatcher{
												MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: "path-foo"}}}}}},
									}}}}}}}}},
				},
				"authz_allow_policy_2": {
					Principals: []*v3rbacpb.Principal{
						{Identifier: &v3rbacpb.Principal_Any{Any: true}}},
					Permissions: []*v3rbacpb.Permission{
						{Rule: &v3rbacpb.Permission_AndRules{AndRules: &v3rbacpb.Permission_Set{
							Rules: []*v3rbacpb.Permission{
								{Rule: &v3rbacpb.Permission_OrRules{OrRules: &v3rbacpb.Permission_Set{
									Rules: []*v3rbacpb.Permission{
										{Rule: &v3rbacpb.Permission_UrlPath{
											UrlPath: &v3matcherpb.PathMatcher{Rule: &v3matcherpb.PathMatcher_Path{Path: &v3matcherpb.StringMatcher{
												MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "path-bar"}}}}}},
										{Rule: &v3rbacpb.Permission_UrlPath{
											UrlPath: &v3matcherpb.PathMatcher{Rule: &v3matcherpb.PathMatcher_Path{Path: &v3matcherpb.StringMatcher{
												MatchPattern: &v3matcherpb.StringMatcher_Suffix{Suffix: "baz"}}}}}},
									}}}},
								{Rule: &v3rbacpb.Permission_AndRules{AndRules: &v3rbacpb.Permission_Set{
									Rules: []*v3rbacpb.Permission{
										{Rule: &v3rbacpb.Permission_OrRules{OrRules: &v3rbacpb.Permission_Set{
											Rules: []*v3rbacpb.Permission{
												{Rule: &v3rbacpb.Permission_Header{
													Header: &v3routepb.HeaderMatcher{
														Name: "key-1", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "foo"}}}},
												{Rule: &v3rbacpb.Permission_Header{
													Header: &v3routepb.HeaderMatcher{
														Name: "key-1", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_SuffixMatch{SuffixMatch: "bar"}}}},
											}}}},
										{Rule: &v3rbacpb.Permission_OrRules{OrRules: &v3rbacpb.Permission_Set{
											Rules: []*v3rbacpb.Permission{
												{Rule: &v3rbacpb.Permission_Header{
													Header: &v3routepb.HeaderMatcher{
														Name: "key-2", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PrefixMatch{PrefixMatch: "baz"}}}},
											}}}}}}}}}}}}},
				},
			}},
		},
		"parsing json failed": {
			wantErr:         "failed to parse authorization policy",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"missing name field": {
			authzPolicy:     `{}`,
			wantErr:         "\"name\" is not present",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"invalid name type": {
			authzPolicy:     `{"name": 123}`,
			wantErr:         "\"name\" 123 is not a string",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"missing allow rules field": {
			authzPolicy:     `{"name": "policy_abc"}`,
			wantErr:         "\"allow_rules\" is not present",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"invalid rules type": {
			authzPolicy: `{
				"name": "policy_abc",
				"allow_rules": {}
			}`,
			wantErr:         "\"allow_rules\" rules is not an array",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"missing rule name field": {
			authzPolicy: `{
				"name": "policy_abc",
				"allow_rules": [{}]
			}`,
			wantErr:         "\"allow_rules\" 0: \"name\" is not present",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"invalid rule name type": {
			authzPolicy: `{
				"name": "policy_abc",
				"allow_rules": [{"name": 123}]
			}`,
			wantErr:         "\"allow_rules\" 0: \"name\" 123 is not a string",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"invalid request type": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [{"name": "allow_policy_1", "request": []}]
			}`,
			wantErr:         "\"allow_rules\" 0: \"request\" is not an object",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"invalid headers type": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [{"name": "allow_policy_1", "request": {"headers":{}}}]
			}`,
			wantErr:         "\"allow_rules\" 0: \"headers\" is not an array",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"empty headers": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [{"name": "allow_policy_1", "request": {"headers":[]}}]
			}`,
			wantErr:         "\"allow_rules\" 0: \"headers\" is empty",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"missing headers key": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [{"name": "allow_policy_1", "request": {"headers":[{}]}}]
			}`,
			wantErr:         "\"allow_rules\" 0: \"headers\" 0: \"key\" is not present",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"invalid headers key type": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [{"name": "allow_policy_1", "request": {"headers":[{"key":123}]}}]
			}`,
			wantErr:         "\"allow_rules\" 0: \"headers\" 0: \"key\" 123 is not a string",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"missing header values": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [{"name": "allow_policy_1", "request": {"headers":[{"key":"key-a"}]}}]
			}`,
			wantErr:         "\"allow_rules\" 0: \"headers\" 0: \"values\" is not present",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"invalid header values type": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [{"name": "allow_policy_1", "request": {"headers":[{"key":"key-a", "values":{}}]}}]
			}`,
			wantErr:         "\"allow_rules\" 0: \"headers\" 0: \"values\" is not an array",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"invalid header value type": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [{"name": "allow_policy_1", "request": {"headers":[{"key":"key-a", "values":[123]}]}}]
			}`,
			wantErr:         "\"allow_rules\" 0: \"headers\" 0: \"values\" 0: 123 is not a string",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"invalid paths type": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [{"name": "allow_policy_1", "request": {"paths":{}}}]
			}`,
			wantErr:         "\"allow_rules\" 0: \"paths\" is not an array",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"invalid path type": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [{"name": "allow_policy_1", "request": {"paths":[123]}}]
			}`,
			wantErr:         "\"allow_rules\" 0: \"paths\" 0: 123 is not a string",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"invalid source type": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [{"name": "allow_policy_1", "source": []}]
			}`,
			wantErr:         "\"allow_rules\" 0: \"source\" is not an object",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"invalid principals type": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [{"name": "allow_policy_1", "source": {"principals":{}}}]
			}`,
			wantErr:         "\"allow_rules\" 0: \"principals\" is not an array",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
		"invalid principal type": {
			authzPolicy: `{
				"name": "authz",
				"allow_rules": [{"name": "allow_policy_1", "source": {"principals":[123]}}]
			}`,
			wantErr:         "\"allow_rules\" 0: \"principals\" 0: 123 is not a string",
			wantDenyPolicy:  nil,
			wantAllowPolicy: nil,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			gotDenyPolicy, gotAllowPolicy, gotErr := translatePolicy(test.authzPolicy)
			if gotErr != nil && gotErr.Error() != test.wantErr {
				t.Fatalf("translatePolicy%v\nerror want:%v\ngot:%v", test.authzPolicy, test.wantErr, gotErr)
			}
			if gotDenyPolicy.String() != test.wantDenyPolicy.String() {
				t.Fatalf("translatePolicy%v\nunexpected deny policy\nwant:\n%s\ngot:\n%s",
					test.authzPolicy, test.wantDenyPolicy.String(), gotDenyPolicy.String())
			}
			if gotAllowPolicy.String() != test.wantAllowPolicy.String() {
				t.Fatalf("translatePolicy%v\nunexpected allow policy\nwant:\n%s\ngot:\n%s",
					test.authzPolicy, test.wantAllowPolicy.String(), gotAllowPolicy.String())
			}
		})
	}
}
