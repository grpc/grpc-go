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
	"context"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"net"
	"testing"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	"google.golang.org/grpc/internal/grpctest"
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

// Actually I might keep these...

// "Getters don't make sense"
// So keep this function here, but the only thing RBAC defines in the API layer is the context, but will call this function

// Errors combine

// How does the logic in c core work {1, 2, 3, 4, 5, 6} <- does each field need to be populated with data, does this make sense?
// Right now this function has to have everything
// Could instantiate it with ctx, pull stuff out of context as needed (will return errors there)
// Instantiate with ctx
// Pull stuff out of that generic data as needed...with getters
// Or helperfunciton(ctx) data prepopulated...

/*
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
}*/

// Function here for configuration (chaining)
// Take singular configurations from previous

// TestChainedRBACEngineConstruction tests the construction of the
// ChainedRBACEngine. Due to some types of RBAC configuration being logically
// wrong and returning an error rather than successfully constructing the RBAC
// Engine, this test tests both RBAC Configurations deemed successful and also
// RBAC Configurations that will raise errors.
func (s) TestChainedRBACEngineConstruction(t *testing.T) {
	tests := []struct {
		name        string
		rbacConfigs []*v3rbacpb.RBAC
		wantErr     bool
	}{
		{
			name: "TestSuccessCaseAnyMatchSingular",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
					Action: v3rbacpb.RBAC_ALLOW,
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
		},
		{
			name: "TestSuccessCaseAnyMatchMultiple",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
					Action: v3rbacpb.RBAC_ALLOW,
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
				{
					Action: v3rbacpb.RBAC_DENY,
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
		},
		{
			name: "TestSuccessCaseSimplePolicySingular",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
					Action: v3rbacpb.RBAC_ALLOW,
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
		},
		{
			name: "TestSuccessCaseSimplePolicyMultiple",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
					Action: v3rbacpb.RBAC_ALLOW,
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
				{
					Action: v3rbacpb.RBAC_DENY,
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
		},
		{
			name: "TestSuccessCaseEnvoyExampleSingular",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
					Action: v3rbacpb.RBAC_ALLOW,
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
		},
		{
			name: "TestSourceIpMatcherSuccessSingular",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
					Action: v3rbacpb.RBAC_ALLOW,
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
		},
		{
			name: "TestSourceIpMatcherFailureSingular",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
					Action: v3rbacpb.RBAC_ALLOW,
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
			},
			wantErr: true,
		},
		{
			name: "TestDestinationIpMatcherSuccess",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
					Action: v3rbacpb.RBAC_ALLOW,
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
		},
		{
			name: "TestDestinationIpMatcherFailure",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
					Action: v3rbacpb.RBAC_ALLOW,
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
			},
			wantErr: true,
		},
		{
			name: "TestMatcherToNotPolicy",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
					Action: v3rbacpb.RBAC_ALLOW,
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
		},
		{
			name: "TestMatcherToNotPrinicipal",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
					Action: v3rbacpb.RBAC_ALLOW,
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
		},
		{
			name: "TestPrincipalProductViewer",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
					Action: v3rbacpb.RBAC_ALLOW,
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
		},
		{
			name: "TestCertainHeaders",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
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
		},
		// Test LOG and also an action not specified
		{
			name: "TestLogAction",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
					Action: v3rbacpb.RBAC_LOG,
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
			wantErr: true,
		},
		{
			name: "TestActionNotSpecified",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
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
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewChainEngine(test.rbacConfigs); (err != nil) != test.wantErr {
				t.Fatalf("NewChainEngine(%+v) returned err: %v, wantErr: %v", test.rbacConfigs, err, test.wantErr)
			}
		})
	}
}

// Function here for once configured and instantiated, check incoming RPC's against it
// Will check conversion function + chaining logic of whether incoming RPC's should be allowed

// Thus, this needs to construct a context at the beginning full of the things that represent data

// TestChainedRBACEngine tests the chain of RBAC Engines by configuring the
// chain of engines in a certain way in different scenarios. After configuring
// the chain of engines in a certain way, this test pings the chain of engines
// with different types of data representing incoming RPC's (usually data piped
// into a context), and verifies that it works as expected.
func (s) TestChainedRBACEngine(t *testing.T) {
	tests := []struct {
		name        string
		rbacConfigs []*v3rbacpb.RBAC // Scaled up to handle long list
		// Actually, rather than defining the three things you pipe into context, define RPC Data,
		// and pull stuff off, build md, pi and conn, put that into context, send that to chain
		rbacQueries []struct {
			/*Previously:
			(What you send into API, What you get back from API)
			rpcData *RPCData
			wantMatchingPolicyName string
			wantMatch bool
			*/
			// DetermineStatus(data Data) error <- will have information about what to do about the RPC
			/*Now: You send in generic data to API, you get a status code without matching policy name there*/
			// Generic Data in context (Metadata, PeerInfo, Conn) that will get pulled out
			// Why not literally have the four data points: Metadata, PeerInfo, and Connection and MethodName
			// Pipe first three into context, call the function with fourth as well
			/*md             metadata.MD
			pi             *peer.Peer
			conn           net.Conn
			methodName     string
			wantStatusCode codes.Code*/
			rpcData        *rpcData
			wantStatusCode codes.Code
		}
	}{
		// TestSuccessCaseAnyMatch tests a single RBAC Engine instantiated with
		// a config with a policy with any rules for both permissions and
		// principals, meaning that any data about incoming RPC's that the RBAC
		// Engine is queried with should match that policy.
		{
			name: "TestSuccessCaseAnyMatch",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
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
			rbacQueries: []struct {
				rpcData        *rpcData
				wantStatusCode codes.Code
			}{
				{
					// If you unspecify the three, this will logically represent the same thing as previous. What will happen in this scenario?
					rpcData: &rpcData{
						fullMethod: "some method",
						// The rest default i.e. make connection, but otherwise have it default
					},
					wantStatusCode: codes.OK,
				},
			},
		},
		// TestSuccessCaseSimplePolicy is a test that tests a single policy
		// that only allows an rpc to proceed if the rpc is calling with a certain
		// path and port.
		/*{
			name: "TestSuccessCaseSimplePolicy",
			rbacConfigs: []*v3rbacpb.RBAC{
				{
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
			rbacQueries: []struct {
				// Why
			},
		},*/
	}

	// Also check what happens if stuff isn't already in context (i.e. metadata, peerinfo, conn)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Instantiate the chainedRBACEngine with different configurations that are
			// interesting to test and to query.
			cre, err := NewChainEngine(test.rbacConfigs)
			if err != nil {
				t.Fatalf("Error constructing RBAC Engine: %v", err)
			}
			// Query the created chain of RBAC Engines with different args to see
			// if the chain of RBAC Engines configured as such works as intended.
			for _, data := range test.rbacQueries { // What about specifying the RPC Data to send around, pulling stuff off that RPC Data to construct three basic...?
				// Construct the context with three data points that have enough
				// information to represent incoming RPC's. This will be how a
				// user uses this API.
				ctx := metadata.NewIncomingContext(context.Background(), data.rpcData.md) // This stays the same

				// Make a TCP connection with a certain destination port. The
				// address/port of this connection will be used to populate the
				// destination ip/port in RPCData struct. This represents what
				// the user of ChainedRBACEngine will have to place into
				// context, as this is only way to get destination ip and port.
				lis, err := net.Listen("tcp", "localhost:"+fmt.Sprint(data.rpcData.destinationPort))
				var conn net.Conn
				go func() {
					conn, err = lis.Accept()
					if err != nil {
						t.Fatalf("Error accepting connection: %v", err)
						return
					}
				}()
				// I feel like what I have to do is this:
				// Client: Dial, Server: net.Listen (listening on a configurable port), dest addr + port
				// ==, now have a conn connecting client and server
				// the thing is, we're testing that the conversion works, so can't mock or hook into RPCData passed
				// into chain of engines
				net.Dial("tcp", lis.Addr().String())

				// Construct connection to put in context here
				// Connection needs to have a certain IP and a certain PORT (dest ip + dest port)
				// How do you construct a connection like that?
				// net.Listen up there? Pass that in? - I think this might be best option
				// conn := /*Mock Connection here...*/ // PROBLEM STATEMENT: HOW DOES (IF EVEN...) THE CODEBASE DEFINE A MOCK CONNECTION?
				// The only thing that this connection needs is a way to get IP Address string it will split into address + port

				// If we construct PeerInfo from net.Listen, how would that work?
				// Construct PeerInfo to put in context here
				// PeerInfo has authinfo + addr
				// if authinfo isn't tls, unauthenticated user
				// addr is something easily mockable, take addr defined in RPCData specified and put it inside PeerInfo
				// Like we had before
				// pi := &peer.Peer{
				// Addr: /*Construct an address based on rpc specification, also have this default*/,
				// AuthInfo: // If the test specifies list of credentials, have this thing have tls auth info,
				// } Outside of Connection, PeerInfo is still defined in RPCData...

				// These three things -> RPCData, define RPCData, use that to build three things, like how user of API will use

				// data.conn is used for local addr + port
				ctx = SetConnection(ctx, conn) // The RPC has to happen on a connection, so this will never be unspecified..., how do we have a connection with a mock address
				// data.pi is used for remote addr + port
				// Set this to TLS Credentials if specified...
				ctx = peer.NewContext(ctx, data.rpcData.peerInfo) // This has auth info (new thing) + addr, define this in test case
				// Default should be present, but when looks into it it pulls off nil value...
				err = cre.DetermineStatus(Data{
					Ctx:        ctx,
					MethodName: data.rpcData.fullMethod,
				})

				// I think a nil status represents a OK code, perhaps return a nil error instead?
				status, _ := status.FromError(err)
				if data.wantStatusCode != status.Code() {
					t.Fatalf("DetermineStatus() returned (), want()")
				}
			}
		})
	}
}

// After writing all these tests, also need to check code coverage
