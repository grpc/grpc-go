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

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/peer"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type addr struct {
	ipAddress string
}

func (addr) Network() string   { return "" }
func (a *addr) String() string { return a.ipAddress }

// TestRBACEngineConstruction tests the construction of the RBAC Engine. Due to
// some types of RBAC configuration being logically wrong and returning an error
// rather than successfully constructing the RBAC Engine, this test tests both
// RBAC Configurations deemed successful and also RBAC Configurations that will
// raise errors.
func (s) TestRBACEngineConstruction(t *testing.T) {
	tests := []struct {
		name       string
		rbacConfig *v3rbacpb.RBAC
		wantErr    bool
	}{
		{
			name: "TestSuccessCaseAnyMatch",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"anyone": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_Any{Any: true}},
						},
						Principals: []*v3rbacpb.Principal{
							{Identifier: &v3rbacpb.Principal_Any{Any: true}},
						},
					},
				},
			},
		},
		{
			name: "TestSuccessCaseSimplePolicy",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"localhost-fan": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_DestinationPort{DestinationPort: 8080}},
							{Rule: &v3rbacpb.Permission_UrlPath{UrlPath: &v3matcherpb.PathMatcher{Rule: &v3matcherpb.PathMatcher_Path{Path: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "localhost-fan-page"}}}}}},
						},
						Principals: []*v3rbacpb.Principal{
							{Identifier: &v3rbacpb.Principal_Any{Any: true}},
						},
					},
				},
			},
		},
		{
			name: "TestSuccessCaseEnvoyExample",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"service-admin": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_Any{Any: true}},
						},
						Principals: []*v3rbacpb.Principal{
							{Identifier: &v3rbacpb.Principal_Authenticated_{Authenticated: &v3rbacpb.Principal_Authenticated{PrincipalName: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "cluster.local/ns/default/sa/admin"}}}}},
							{Identifier: &v3rbacpb.Principal_Authenticated_{Authenticated: &v3rbacpb.Principal_Authenticated{PrincipalName: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "cluster.local/ns/default/sa/superuser"}}}}},
						},
					},
					"product-viewer": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_AndRules{AndRules: &v3rbacpb.Permission_Set{
								Rules: []*v3rbacpb.Permission{
									{Rule: &v3rbacpb.Permission_Header{Header: &v3routepb.HeaderMatcher{Name: ":method", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "GET"}}}},
									{Rule: &v3rbacpb.Permission_UrlPath{UrlPath: &v3matcherpb.PathMatcher{Rule: &v3matcherpb.PathMatcher_Path{Path: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: "/products"}}}}}},
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
							{Identifier: &v3rbacpb.Principal_Any{Any: true}},
						},
					},
				},
			},
		},
		{
			name: "TestSourceIpMatcherSuccess",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"certain-source-ip": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_Any{Any: true}},
						},
						Principals: []*v3rbacpb.Principal{
							{Identifier: &v3rbacpb.Principal_DirectRemoteIp{DirectRemoteIp: &v3corepb.CidrRange{AddressPrefix: "0.0.0.0", PrefixLen: &wrapperspb.UInt32Value{Value: uint32(10)}}}},
						},
					},
				},
			},
		},
		{
			name: "TestSourceIpMatcherFailure",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"certain-source-ip": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_Any{Any: true}},
						},
						Principals: []*v3rbacpb.Principal{
							{Identifier: &v3rbacpb.Principal_DirectRemoteIp{DirectRemoteIp: &v3corepb.CidrRange{AddressPrefix: "not a correct address", PrefixLen: &wrapperspb.UInt32Value{Value: uint32(10)}}}},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "TestDestinationIpMatcherSuccess",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"certain-destination-ip": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_DestinationIp{DestinationIp: &v3corepb.CidrRange{AddressPrefix: "0.0.0.0", PrefixLen: &wrapperspb.UInt32Value{Value: uint32(10)}}}},
						},
						Principals: []*v3rbacpb.Principal{
							{Identifier: &v3rbacpb.Principal_Any{Any: true}},
						},
					},
				},
			},
		},
		{
			name: "TestDestinationIpMatcherFailure",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"certain-destination-ip": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_DestinationIp{DestinationIp: &v3corepb.CidrRange{AddressPrefix: "not a correct address", PrefixLen: &wrapperspb.UInt32Value{Value: uint32(10)}}}},
						},
						Principals: []*v3rbacpb.Principal{
							{Identifier: &v3rbacpb.Principal_Any{Any: true}},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "TestMatcherToNotPolicy",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"not-secret-content": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_NotRule{NotRule: &v3rbacpb.Permission{Rule: &v3rbacpb.Permission_UrlPath{UrlPath: &v3matcherpb.PathMatcher{Rule: &v3matcherpb.PathMatcher_Path{Path: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: "/secret-content"}}}}}}}},
						},
						Principals: []*v3rbacpb.Principal{
							{Identifier: &v3rbacpb.Principal_Any{Any: true}},
						},
					},
				},
			},
		},
		{
			name: "TestMatcherToNotPrincipal",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"not-from-certain-ip": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_Any{Any: true}},
						},
						Principals: []*v3rbacpb.Principal{
							{Identifier: &v3rbacpb.Principal_NotId{NotId: &v3rbacpb.Principal{Identifier: &v3rbacpb.Principal_DirectRemoteIp{DirectRemoteIp: &v3corepb.CidrRange{AddressPrefix: "0.0.0.0", PrefixLen: &wrapperspb.UInt32Value{Value: uint32(10)}}}}}},
						},
					},
				},
			},
		},
		{
			name: "TestPrincipalProductViewer",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"product-viewer": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_Any{Any: true}},
						},
						Principals: []*v3rbacpb.Principal{
							{
								Identifier: &v3rbacpb.Principal_AndIds{AndIds: &v3rbacpb.Principal_Set{Ids: []*v3rbacpb.Principal{
									{Identifier: &v3rbacpb.Principal_Header{Header: &v3routepb.HeaderMatcher{Name: ":method", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "GET"}}}},
									{Identifier: &v3rbacpb.Principal_OrIds{OrIds: &v3rbacpb.Principal_Set{
										Ids: []*v3rbacpb.Principal{
											{Identifier: &v3rbacpb.Principal_UrlPath{UrlPath: &v3matcherpb.PathMatcher{Rule: &v3matcherpb.PathMatcher_Path{Path: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: "/books"}}}}}},
											{Identifier: &v3rbacpb.Principal_UrlPath{UrlPath: &v3matcherpb.PathMatcher{Rule: &v3matcherpb.PathMatcher_Path{Path: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: "/cars"}}}}}},
										},
									}}},
								}}},
							},
						},
					},
				},
			},
		},
		{
			name: "TestCertainHeaders",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"certain-headers": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_Any{Any: true}},
						},
						Principals: []*v3rbacpb.Principal{
							{
								Identifier: &v3rbacpb.Principal_OrIds{OrIds: &v3rbacpb.Principal_Set{Ids: []*v3rbacpb.Principal{
									{Identifier: &v3rbacpb.Principal_Header{Header: &v3routepb.HeaderMatcher{Name: ":method", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "GET"}}}},
									{Identifier: &v3rbacpb.Principal_Header{Header: &v3routepb.HeaderMatcher{Name: ":method", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_SafeRegexMatch{SafeRegexMatch: &v3matcherpb.RegexMatcher{Regex: "GET"}}}}},
									{Identifier: &v3rbacpb.Principal_Header{Header: &v3routepb.HeaderMatcher{Name: ":method", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_RangeMatch{RangeMatch: &v3typepb.Int64Range{
										Start: 0,
										End:   64,
									}}}}},
									{Identifier: &v3rbacpb.Principal_Header{Header: &v3routepb.HeaderMatcher{Name: ":method", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PresentMatch{PresentMatch: true}}}},
									{Identifier: &v3rbacpb.Principal_Header{Header: &v3routepb.HeaderMatcher{Name: ":method", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PrefixMatch{PrefixMatch: "GET"}}}},
									{Identifier: &v3rbacpb.Principal_Header{Header: &v3routepb.HeaderMatcher{Name: ":method", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_SuffixMatch{SuffixMatch: "GET"}}}},
									{Identifier: &v3rbacpb.Principal_Header{Header: &v3routepb.HeaderMatcher{Name: ":method", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ContainsMatch{ContainsMatch: "GET"}}}},
								}}},
							},
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewEngine(test.rbacConfig); (err != nil) != test.wantErr {
				t.Fatalf("NewEngine(%+v) returned err: %v, wantErr: %v", test.rbacConfig, err, test.wantErr)
			}
		})
	}
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
			rpcData                *RPCData
			wantMatchingPolicyName string
			wantMatch              bool
		}
	}{
		// TestSuccessCaseAnyMatch tests an RBAC Engine instantiated with a
		// config with a policy with any rules for both permissions and
		// principals, meaning that any data about incoming RPC's that the RBAC
		// Engine is queried with should match that policy.
		{
			name: "TestSuccessCaseAnyMatch",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"anyone": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_Any{Any: true}},
						},
						Principals: []*v3rbacpb.Principal{
							{Identifier: &v3rbacpb.Principal_Any{Any: true}},
						},
					},
				},
			},
			rbacQueries: // Any incoming RPC should match with the anyone policy
			[]struct {
				rpcData                *RPCData
				wantMatchingPolicyName string
				wantMatch              bool
			}{
				{
					rpcData: &RPCData{
						FullMethod: "some method",
					},
					wantMatchingPolicyName: "anyone",
					wantMatch:              true,
				},
				{
					rpcData: &RPCData{
						DestinationPort: 100,
					},
					wantMatchingPolicyName: "anyone",
					wantMatch:              true,
				},
			},
		},
		// TestSuccessCaseSimplePolicy is a test that tests a simple policy that
		// only allows an rpc to proceed if the rpc is calling a certain path
		// and port.
		{
			name: "TestSuccessCaseSimplePolicy",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"localhost-fan": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_DestinationPort{DestinationPort: 8080}},
							{Rule: &v3rbacpb.Permission_UrlPath{UrlPath: &v3matcherpb.PathMatcher{Rule: &v3matcherpb.PathMatcher_Path{Path: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "localhost-fan-page"}}}}}},
						},
						Principals: []*v3rbacpb.Principal{
							{Identifier: &v3rbacpb.Principal_Any{Any: true}},
						},
					},
				},
			},
			rbacQueries: []struct {
				rpcData                *RPCData
				wantMatchingPolicyName string
				wantMatch              bool
			}{
				// This RPC should match with the local host fan policy.
				{
					rpcData: &RPCData{
						MD: map[string][]string{
							":path": {"localhost-fan-page"},
						},
						DestinationPort: 8080,
					},
					wantMatchingPolicyName: "localhost-fan",
					wantMatch:              true},
				// This RPC shouldn't match with the local host fan policy.
				{
					rpcData: &RPCData{
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
		{
			name: "TestSuccessCaseEnvoyExample",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"service-admin": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_Any{Any: true}},
						},
						Principals: []*v3rbacpb.Principal{
							{Identifier: &v3rbacpb.Principal_Authenticated_{Authenticated: &v3rbacpb.Principal_Authenticated{PrincipalName: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "cluster.local/ns/default/sa/admin"}}}}},
							{Identifier: &v3rbacpb.Principal_Authenticated_{Authenticated: &v3rbacpb.Principal_Authenticated{PrincipalName: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "cluster.local/ns/default/sa/superuser"}}}}},
						},
					},
					"product-viewer": {
						Permissions: []*v3rbacpb.Permission{
							{
								Rule: &v3rbacpb.Permission_AndRules{AndRules: &v3rbacpb.Permission_Set{
									Rules: []*v3rbacpb.Permission{
										{Rule: &v3rbacpb.Permission_Header{Header: &v3routepb.HeaderMatcher{Name: ":method", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "GET"}}}},
										{Rule: &v3rbacpb.Permission_UrlPath{UrlPath: &v3matcherpb.PathMatcher{Rule: &v3matcherpb.PathMatcher_Path{Path: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: "/products"}}}}}},
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
							{Identifier: &v3rbacpb.Principal_Any{Any: true}},
						},
					},
				},
			},
			rbacQueries: []struct {
				rpcData                *RPCData
				wantMatchingPolicyName string
				wantMatch              bool
			}{
				// This incoming RPC Call should match with the service admin
				// policy.
				{
					rpcData: &RPCData{
						FullMethod:    "some method",
						PrincipalName: "cluster.local/ns/default/sa/admin",
					},
					wantMatchingPolicyName: "service-admin",
					wantMatch:              true},
				// This incoming RPC Call should match with the product
				// viewer policy.
				{
					rpcData: &RPCData{
						DestinationPort: 80,
						MD: map[string][]string{
							":method": {"GET"},
						},
						FullMethod: "/products",
					},
					wantMatchingPolicyName: "product-viewer",
					wantMatch:              true},
				// These incoming RPC calls should not match any policy -
				// represented by an empty matching policy name and false being
				// returned.
				{
					rpcData: &RPCData{
						DestinationPort: 100,
					},
					wantMatchingPolicyName: ""},
				{
					rpcData: &RPCData{
						FullMethod:      "get-product-list",
						DestinationPort: 8080,
					},
					wantMatchingPolicyName: ""},
				{
					rpcData: &RPCData{
						PrincipalName:   "localhost",
						DestinationPort: 8080,
					},
					wantMatchingPolicyName: ""},
			},
		},
		{
			name: "TestNotMatcher",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"not-secret-content": {
						Permissions: []*v3rbacpb.Permission{
							{
								Rule: &v3rbacpb.Permission_NotRule{
									NotRule: &v3rbacpb.Permission{Rule: &v3rbacpb.Permission_UrlPath{UrlPath: &v3matcherpb.PathMatcher{Rule: &v3matcherpb.PathMatcher_Path{Path: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: "/secret-content"}}}}}},
								},
							},
						},
						Principals: []*v3rbacpb.Principal{
							{Identifier: &v3rbacpb.Principal_Any{Any: true}},
						},
					},
				},
			},
			rbacQueries: []struct {
				rpcData                *RPCData
				wantMatchingPolicyName string
				wantMatch              bool
			}{
				// This incoming RPC Call should match with the not-secret-content policy.
				{
					rpcData: &RPCData{
						FullMethod: "/regular-content",
					},
					wantMatchingPolicyName: "not-secret-content",
					wantMatch:              true,
				},
				// This incoming RPC Call shouldn't match with the not-secret-content policy.
				{
					rpcData: &RPCData{
						FullMethod: "/secret-content",
					},
					wantMatchingPolicyName: "",
					wantMatch:              false,
				},
			},
		},
		{
			name: "TestSourceIpMatcher",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"certain-source-ip": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_Any{Any: true}},
						},
						Principals: []*v3rbacpb.Principal{
							{Identifier: &v3rbacpb.Principal_DirectRemoteIp{DirectRemoteIp: &v3corepb.CidrRange{AddressPrefix: "0.0.0.0", PrefixLen: &wrapperspb.UInt32Value{Value: uint32(10)}}}},
						},
					},
				},
			},
			rbacQueries: []struct {
				rpcData                *RPCData
				wantMatchingPolicyName string
				wantMatch              bool
			}{
				// This incoming RPC Call should match with the certain-source-ip policy.
				{
					rpcData: &RPCData{
						PeerInfo: &peer.Peer{
							Addr: &addr{ipAddress: "0.0.0.0"},
						},
					},
					wantMatchingPolicyName: "certain-source-ip",
					wantMatch:              true,
				},
				// This incoming RPC Call shouldn't match with the certain-source-ip policy.
				{
					rpcData: &RPCData{
						PeerInfo: &peer.Peer{
							Addr: &addr{ipAddress: "10.0.0.0"},
						},
					},
					wantMatchingPolicyName: "",
					wantMatch:              false,
				},
			},
		},
		{
			name: "TestDestinationIpMatcher",
			rbacConfig: &v3rbacpb.RBAC{
				Policies: map[string]*v3rbacpb.Policy{
					"certain-destination-ip": {
						Permissions: []*v3rbacpb.Permission{
							{Rule: &v3rbacpb.Permission_DestinationIp{DestinationIp: &v3corepb.CidrRange{AddressPrefix: "0.0.0.0", PrefixLen: &wrapperspb.UInt32Value{Value: uint32(10)}}}},
						},
						Principals: []*v3rbacpb.Principal{
							{Identifier: &v3rbacpb.Principal_Any{Any: true}},
						},
					},
				},
			},
			rbacQueries: []struct {
				rpcData                *RPCData
				wantMatchingPolicyName string
				wantMatch              bool
			}{
				// This incoming RPC Call should match with the certain-destination-ip policy.
				{
					rpcData: &RPCData{
						DestinationAddr: &addr{ipAddress: "0.0.0.10"},
					},
					wantMatchingPolicyName: "certain-destination-ip",
					wantMatch:              true,
				},
				// This incoming RPC Call shouldn't match with the certain-destination-ip policy.
				{
					rpcData: &RPCData{
						DestinationAddr: &addr{ipAddress: "10.0.0.0"},
					},
					wantMatchingPolicyName: "",
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Instantiate the rbacEngine with different configurations that
			// interesting to test and to query.
			rbacEngine, err := NewEngine(test.rbacConfig)
			if err != nil {
				t.Fatalf("Error constructing RBAC Engine: %v", err)
			}
			// Query that created RBAC Engine with different args to see if the
			// RBAC Engine configured a certain way works as intended.
			for _, queryToRBACEngine := range test.rbacQueries {
				// The matchingPolicyName returned will be empty in the case of
				// no matching policy. Thus, matchingPolicyName can also be used
				// to test the "error" condition of no matching policies.
				matchingPolicyName, matchingPolicyFound := rbacEngine.FindMatchingPolicy(queryToRBACEngine.rpcData)
				if matchingPolicyFound != queryToRBACEngine.wantMatch || matchingPolicyName != queryToRBACEngine.wantMatchingPolicyName {
					t.Errorf("FindMatchingPolicy(%+v) returned (%v, %v), want (%v, %v)", queryToRBACEngine.rpcData, matchingPolicyName, matchingPolicyFound, queryToRBACEngine.wantMatchingPolicyName, queryToRBACEngine.wantMatch)
				}
			}
		})
	}
}
