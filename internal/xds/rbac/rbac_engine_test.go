/*
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
 */

package rbac

import (
	"testing"

	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	envoyconfigroutev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoytypematcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestRBACEngine tests the RBAC Engine by configuring the engine in different
// scenarios. After configuring the engine in a certain way, this test pings the
// engine with different kinds of data representing incoming RPC's, and verifies
// that it works as expected.
func (s) TestRBACEngine(t *testing.T) {
	tests := []struct {
		name        string
		rbacConfig  *v3rbacpb.RBAC
		rbacQueries []struct {
			evaluateArgs           *EvaluateArgs
			wantMatchingPolicyName string
		}
	}{
		// TestSuccessCaseAnyMatch tests an RBAC Engine instantiated with a
		// config with a policy with any rules for both permissions and
		// principals, meaning that any data about incoming RPC's that the RBAC
		// Engine is queried with should match that policy.
		{name: "TestSuccessCaseAnyMatch",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"anyone": {
						Permissions: []*v3rbacpb.Permission{
							{
								Rule: &v3rbacpb.Permission_Any{
									Any: true,
								},
							},
						},
						Principals: []*v3rbacpb.Principal{
							{
								Identifier: &v3rbacpb.Principal_Any{Any: true},
							},
						},
					},
				},
			},
			rbacQueries: // Any incoming RPC should match with the anyone policy
			[]struct {
				evaluateArgs           *EvaluateArgs
				wantMatchingPolicyName string
			}{
				{evaluateArgs: &EvaluateArgs{
					FullMethod: "some method",
				},
					wantMatchingPolicyName: "anyone"},
				{evaluateArgs: &EvaluateArgs{
					DestinationPort: 100,
				},
					wantMatchingPolicyName: "anyone"},
			},
		},
		// TestSuccessCaseSimplePolicy is a test that tests a simple policy that
		// only allows an rpc to proceed if the rpc is calling a certain path
		// and port.
		{name: "TestSuccessCaseSimplePolicy",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"localhost-fan": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_DestinationPort{DestinationPort: 8080}},
							{Rule: &v3rbacpb.Permission_UrlPath{UrlPath: &envoytypematcherv3.PathMatcher{Rule: &envoytypematcherv3.PathMatcher_Path{Path: &envoytypematcherv3.StringMatcher{MatchPattern: &envoytypematcherv3.StringMatcher_Exact{Exact: "localhost-fan-page"}}}}}},
						},
						Principals: []*v3rbacpb.Principal{
							{
								Identifier: &v3rbacpb.Principal_Any{Any: true},
							},
						},
					},
				},
			},
			rbacQueries: []struct {
				evaluateArgs           *EvaluateArgs
				wantMatchingPolicyName string
			}{
				// This RPC should match with the local host fan policy.
				{evaluateArgs: &EvaluateArgs{
					MD: map[string][]string{
						":path": {"localhost-fan-page"},
					},
					DestinationPort: 8080,
				},
					wantMatchingPolicyName: "localhost-fan"},
				// This RPC shouldn't match with the local host fan policy.
				{evaluateArgs: &EvaluateArgs{
					DestinationPort: 100,
				},
					wantMatchingPolicyName: ""},
			},
		},
		// TestSuccessCaseEnvoyExample is a test based on the example provided
		// in the EnvoyProxy docs. The RBAC Config contains two policies,
		// service admin and product viewer, that provides an example of a real
		// RBAC Config that might be configured for a given for a given backend
		// service.
		{name: "TestSuccessCaseEnvoyExample",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"service-admin": {
						Permissions: []*v3rbacpb.Permission{
							{
								Rule: &v3rbacpb.Permission_Any{Any: true},
							},
						},
						Principals: []*v3rbacpb.Principal{
							{
								Identifier: &v3rbacpb.Principal_Authenticated_{Authenticated: &v3rbacpb.Principal_Authenticated{PrincipalName: &envoytypematcherv3.StringMatcher{MatchPattern: &envoytypematcherv3.StringMatcher_Exact{Exact: "cluster.local/ns/default/sa/admin"}}}},
							},
							{
								Identifier: &v3rbacpb.Principal_Authenticated_{Authenticated: &v3rbacpb.Principal_Authenticated{PrincipalName: &envoytypematcherv3.StringMatcher{MatchPattern: &envoytypematcherv3.StringMatcher_Exact{Exact: "cluster.local/ns/default/sa/superuser"}}}},
							},
						},
					},
					"product-viewer": {
						Permissions: []*v3rbacpb.Permission{
							{
								Rule: &v3rbacpb.Permission_AndRules{AndRules: &v3rbacpb.Permission_Set{
									Rules: []*v3rbacpb.Permission{
										{Rule: &v3rbacpb.Permission_Header{Header: &envoyconfigroutev3.HeaderMatcher{Name: ":method", HeaderMatchSpecifier: &envoyconfigroutev3.HeaderMatcher_ExactMatch{ExactMatch: "GET"}}}},
										{Rule: &v3rbacpb.Permission_UrlPath{UrlPath: &envoytypematcherv3.PathMatcher{Rule: &envoytypematcherv3.PathMatcher_Path{Path: &envoytypematcherv3.StringMatcher{MatchPattern: &envoytypematcherv3.StringMatcher_Prefix{Prefix: "/products"}}}}}},
										{Rule: &v3rbacpb.Permission_OrRules{OrRules: &v3rbacpb.Permission_Set{
											Rules: []*v3rbacpb.Permission{
												{Rule: &v3rbacpb.Permission_DestinationPort{DestinationPort: 80}},
												{Rule: &v3rbacpb.Permission_DestinationPort{DestinationPort: 443}},
											},
										}}},
									},
								},
								},
							},
						},
						Principals: []*v3rbacpb.Principal{
							{
								Identifier: &v3rbacpb.Principal_Any{Any: true},
							},
						},
					},
				},
			},
			rbacQueries: []struct {
				evaluateArgs           *EvaluateArgs
				wantMatchingPolicyName string
			}{
				// This incoming RPC Call should match with the service admin
				// policy.
				{evaluateArgs: &EvaluateArgs{
					FullMethod:    "some method",
					PrincipalName: "cluster.local/ns/default/sa/admin",
				},
					wantMatchingPolicyName: "service-admin"},
				// This incoming RPC Call should match with the product
				// viewer policy.
				{evaluateArgs: &EvaluateArgs{
					DestinationPort: 80,
					MD: map[string][]string{
						":method": {"GET"},
					},
					FullMethod: "/products",
				},
					wantMatchingPolicyName: "product-viewer"},
				// These incoming RPC calls should not match any policy -
				// represented by an empty matching policy name being returned.
				{evaluateArgs: &EvaluateArgs{
					DestinationPort: 100,
				},
					wantMatchingPolicyName: ""},
				{evaluateArgs: &EvaluateArgs{
					FullMethod:      "get-product-list",
					DestinationPort: 8080,
				},
					wantMatchingPolicyName: ""},
				{evaluateArgs: &EvaluateArgs{
					PrincipalName:   "localhost",
					DestinationPort: 8080,
				},
					wantMatchingPolicyName: ""},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Instantiate the rbacEngine with different configurations that
			// interesting to test and to query.
			rbacEngine, err := NewRBACEngine(test.rbacConfig)
			if err != nil {
				t.Fatalf("Error constructing RBAC Engine: %v", err)
			}
			// Query that created RBAC Engine with different args to see if the
			// RBAC Engine configured a certain way works as intended.
			for _, queryToRBACEngine := range test.rbacQueries {
				// The matchingPolicyName returned will be empty in the case of
				// no matching policy. Thus, matchingPolicyName can also be used
				// to test the "error" condition of no matching policies.
				authorizationDecision := rbacEngine.Evaluate(queryToRBACEngine.evaluateArgs)
				if authorizationDecision.MatchingPolicyName != queryToRBACEngine.wantMatchingPolicyName {
					t.Fatalf("Got matching policy name: %v, want matching policy name: %v", authorizationDecision.MatchingPolicyName, queryToRBACEngine.wantMatchingPolicyName)
				}
			}
		})
	}
}
