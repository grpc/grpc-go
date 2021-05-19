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
	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	"google.golang.org/grpc/internal/grpctest"
	"testing"
)

type s struct { // Do you need one of these for every package?
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// Setup logic here?

// Unit test that is extremely basic

// Call setup function NewRBACEngine

// Validate that the setup function works correctly by making sure that when you ping into the tree
// created, that it correctly was built

// Call with different types of rpc data

// TestSuccessCaseSimpleConfig is a test that tests a very simple RBAC Configuration. It first sets
// the RBAC Engine up with a policy that is very simple. Then, it calls that engine with multiple
// types of RPC Data and validates that it matches the policy or not

// We could test it with an any proto
func (s) TestSuccessCaseSimpleConfig(t *testing.T) {

	// Write out proto config here because you'll need it regardless
	// Perhapas
	simpleProtoConfig := &v3rbacpb.RBAC{}

	// You need the object here, which is this case is an RBAC engine
	// Test the error case pathway too.
	rbacEngine, err := NewRBACEngine(simpleProtoConfig) // This thing takes a policy, so you need to construct a policy somewhere, this could be those crazy JSON things that represent protos in other parts of codebase
	// Call an entrance function, then have someway of validating the tree after calling entrance function (of constructing a new RBAC Engine)
	// Make sure err is nil? Yeah I think so
	if err != nil {
		t.Fatalf("Error constructing RBAC Engine: %v", err)
	}

	// Call this constructed engine with different types of data about incoming RPC's
	EvaluateArgs
	rbacEngine.Evaluate()

	// After evaluating, see if correctly matching policy name
}

// TestSuccessCaseAnyMatch tests that an RBAC Engine instantiated with a config with
// a policy with any types for both permissions and principals that it always matches.
func (s) TestSuccessCaseAnyMatch(t *testing.T) {
	matchAnythingProtoConfig := &v3rbacpb.RBAC{
		Policies: map[string]*v3rbacpb.Policy{
			"anyone": &v3rbacpb.Policy{
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
	}
	rbacEngine, err := NewRBACEngine(matchAnythingProtoConfig)
	if err != nil {
		t.Fatalf("Error constructing RBAC Engine: %v", err)
	}

	// After constructing that RBAC Engine with an any config, any evaluate call,
	// regardless of the incoming RPC Data being evaluated, should match with the
	// anyone policy.
	matchingPolicyName := rbacEngine.Evaluate(&EvaluateArgs{})
	// TODO: Should this be authorization decision? Should we even have an authorization decision type?
	if matchingPolicyName.MatchingPolicyName != "anyone" {
		t.Fatalf("Any incoming RPC should have matched policies with anyone policy.")
	}
	matchingPolicyName = rbacEngine.Evaluate(&EvaluateArgs{
		DestinationPort: 100,
	})
	if matchingPolicyName.MatchingPolicyName != "anyone" {
		t.Fatalf("Any incoming RPC should have matched policies with anyone policy.")
	}
}

// Unit test that is based off Envoy example

// Call setup function NewRBACEngine

// Validate that the setup function works

// Call with different types of rpc data

// TestUnitTestEnvoyExample is a test based on the example provided by EnvoyProxy (link here?)
func (s) TestSuccessCaseEnvoyExample(t *testing.T) {
	// Write out proto config that Envoy proto represents.
	envoyExampleProtoConfig := &v3rbacpb.RBAC{
		// Taking what's in Envoy, and filling out this proto
		// Envoy: service-admin, product-viewer
		// Policies is map[string]*Policy
		Policies: map[string]*v3rbacpb.Policy{
			"service-admin": &v3rbacpb.Policy{
				Permissions: []*v3rbacpb.Permission{
					{
						Rule: &v3rbacpb.Permission_Any{Any: true},
					},
				},
				Principals: []*v3rbacpb.Principal{
					// Two authenticated principals here
					{
						Identifier: &v3rbacpb.Principal_Authenticated{PrincipalName: "cluster.local/ns/default/sa/admin"}, // This is a string matcher
					},
				},
			},
			"product-viewer": &v3rbacpb.Policy{
				Permissions: []*v3rbacpb.Permission{
					{
						Rule: &v3rbacpb.Permission_Any{Any: true},
					},
				},
				Principals: []*v3rbacpb.Principal{
					{
						Identifier: &v3rbacpb.Principal_Any{Any: true},
					},
				},
			},
		},
	}

	rbacEngine, err := NewRBACEngine(envoyExampleProtoConfig)
	if err != nil {
		t.Fatalf("Error constructing RBAC Engine: %v", err)
	}
	// Validate that the RBAC Engine successfully created itself properly
	// ^^ I don't think you actually need this step, as I think that this can be implictly tested from asking the engine for a matching policy name.

	// Success case here (where success means it successfully matched one of the Envoy policies)

	rbacEngine.Evaluate( /*Something here that is logically successful and matches to either service admin or product viewer*/ )

	// Not Successful here (meaning didn't match one of the Envoy policies
	rbacEngine.Evaluate( /*Something here that isn't logically successful and doesn't match to either service admin or product viewer*/ )
}

// Table driven test here with variables config (struct literal)
func (s) TestRBACEngine_Success(t *testing.T) {
	tests := []struct {
		name                   string
		rbacConfig             *v3rbacpb.RBAC
		rbacQueries            []struct {
			evaluateArgs           *EvaluateArgs
			wantMatchingPolicyName string
		}
	}{
		// TestSuccessCaseAnyMatch tests that an RBAC Engine instantiated with a config with
		// a policy with any rules for both permissions and principals that it always matches.
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
			rbacQueries: // Any incoming RPC should match with the policy
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
		// TestSuccessCaseEnvoyExample is a test based on the example provided in the EnvoyProxy docs.
		{name: "TestSuccessCaseEnvoyExample",
			rbacConfig: &v3rbacpb.RBAC{
				// Taking what's in Envoy, and filling out this proto
				// Envoy: service-admin, product-viewer
				// Policies is map[string]*Policy
				Policies: map[string]*v3rbacpb.Policy{
					"service-admin": &v3rbacpb.Policy{
						Permissions: []*v3rbacpb.Permission{
							{
								Rule: &v3rbacpb.Permission_Any{Any: true},
							},
						},
						Principals: []*v3rbacpb.Principal{
							// Two authenticated principals here
							{
								Identifier: &v3rbacpb.Principal_Authenticated{PrincipalName: "cluster.local/ns/default/sa/admin"}, // This is a string matcher
							},
						},
					},
					"product-viewer": &v3rbacpb.Policy{
						Permissions: []*v3rbacpb.Permission{
							{
								Rule: &v3rbacpb.Permission_Any{Any: true},
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
			evaluateArgs: ,
		wantMatchingPolicyName: "service-admin"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rbacEngine, err := NewRBACEngine(test.rbacConfig)
			if err != nil {
				t.Fatalf("Error constructing RBAC Engine: %v", err)
			}
			matchingPolicyName := rbacEngine.Evaluate(test.evaluateArgs)
			// TODO: Should this be authorization decision? Should we even have the authorization decision type?
			if matchingPolicyName.MatchingPolicyName != test.wantMatchingPolicyName {
				t.Fatalf("Got matching policy name: %v, want matching policy name: %v", matchingPolicyName.MatchingPolicyName, test.wantMatchingPolicyName)
			}
		})
	}
}

func (s) TestRBACEngine_Failure(t *testing.T) {
	tests := []struct {
		name         string
		rbacConfig   v3rbacpb.RBAC
		evaluateArgs EvaluateArgs
	}{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

		})
	}
}
