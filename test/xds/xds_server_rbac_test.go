/*
 *
 * Copyright 2022 gRPC authors.
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

package xds_test

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	rpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	testgrpc "google.golang.org/grpc/test/grpc_testing"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

// TestServerSideXDS_RouteConfiguration is an e2e test which verifies routing
// functionality. The xDS enabled server will be set up with route configuration
// where the route configuration has routes with the correct routing actions
// (NonForwardingAction), and the RPC's matching those routes should proceed as
// normal.
func (s) TestServerSideXDS_RouteConfiguration(t *testing.T) {
	oldRBAC := envconfig.XDSRBAC
	envconfig.XDSRBAC = true
	defer func() {
		envconfig.XDSRBAC = oldRBAC
	}()
	managementServer, nodeID, bootstrapContents, resolver, cleanup1 := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup1()

	lis, cleanup2 := setupGRPCServer(t, bootstrapContents)
	defer cleanup2()

	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("failed to retrieve host and port of server: %v", err)
	}
	const serviceName = "my-service-fallback"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       host,
		Port:       port,
		SecLevel:   e2e.SecurityLevelNone,
	})

	// Create an inbound xDS listener resource with route configuration which
	// selectively will allow RPC's through or not. This will test routing in
	// xds(Unary|Stream)Interceptors.
	vhs := []*v3routepb.VirtualHost{
		// Virtual host that will never be matched to test Virtual Host selection.
		{
			Domains: []string{"this will not match*"},
			Routes: []*v3routepb.Route{
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
					},
					Action: &v3routepb.Route_NonForwardingAction{},
				},
			},
		},
		// This Virtual Host will actually get matched to.
		{
			Domains: []string{"*"},
			Routes: []*v3routepb.Route{
				// A routing rule that can be selectively triggered based on properties about incoming RPC.
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"},
						// "Fully-qualified RPC method name with leading slash. Same as :path header".
					},
					// Correct Action, so RPC's that match this route should proceed to interceptor processing.
					Action: &v3routepb.Route_NonForwardingAction{},
				},
				// This routing rule is matched the same way as the one above,
				// except has an incorrect action for the server side. However,
				// since routing chooses the first route which matches an
				// incoming RPC, this should never get invoked (iteration
				// through this route slice is deterministic).
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"},
						// "Fully-qualified RPC method name with leading slash. Same as :path header".
					},
					// Incorrect Action, so RPC's that match this route should get denied.
					Action: &v3routepb.Route_Route{
						Route: &v3routepb.RouteAction{ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: ""}},
					},
				},
				// Another routing rule that can be selectively triggered based on incoming RPC.
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/UnaryCall"},
					},
					// Wrong action (!Non_Forwarding_Action) so RPC's that match this route should get denied.
					Action: &v3routepb.Route_Route{
						Route: &v3routepb.RouteAction{ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: ""}},
					},
				},
				// Another routing rule that can be selectively triggered based on incoming RPC.
				{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/StreamingInputCall"},
					},
					// Wrong action (!Non_Forwarding_Action) so RPC's that match this route should get denied.
					Action: &v3routepb.Route_Route{
						Route: &v3routepb.RouteAction{ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: ""}},
					},
				},
				// Not matching route, this is be able to get invoked logically (i.e. doesn't have to match the Route configurations above).
			}},
	}
	inboundLis := &v3listenerpb.Listener{
		Name: fmt.Sprintf(e2e.ServerListenerResourceNameTemplate, net.JoinHostPort(host, strconv.Itoa(int(port)))),
		Address: &v3corepb.Address{
			Address: &v3corepb.Address_SocketAddress{
				SocketAddress: &v3corepb.SocketAddress{
					Address: host,
					PortSpecifier: &v3corepb.SocketAddress_PortValue{
						PortValue: port,
					},
				},
			},
		},
		FilterChains: []*v3listenerpb.FilterChain{
			{
				Name: "v4-wildcard",
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "0.0.0.0",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
					SourceType: v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK,
					SourcePrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "0.0.0.0",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
				},
				Filters: []*v3listenerpb.Filter{
					{
						Name: "filter-1",
						ConfigType: &v3listenerpb.Filter_TypedConfig{
							TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
								HttpFilters: []*v3httppb.HttpFilter{e2e.HTTPFilter("router", &v3routerpb.Router{})},
								RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
									RouteConfig: &v3routepb.RouteConfiguration{
										Name:         "routeName",
										VirtualHosts: vhs,
									},
								},
							}),
						},
					},
				},
			},
			{
				Name: "v6-wildcard",
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "::",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
					SourceType: v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK,
					SourcePrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "::",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
				},
				Filters: []*v3listenerpb.Filter{
					{
						Name: "filter-1",
						ConfigType: &v3listenerpb.Filter_TypedConfig{
							TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
								HttpFilters: []*v3httppb.HttpFilter{e2e.HTTPFilter("router", &v3routerpb.Router{})},
								RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
									RouteConfig: &v3routepb.RouteConfiguration{
										Name:         "routeName",
										VirtualHosts: vhs,
									},
								},
							}),
						},
					},
				},
			},
		},
	}
	resources.Listeners = append(resources.Listeners, inboundLis)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Setup the management server with client and server-side resources.
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.DialContext(ctx, fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// This Empty Call should match to a route with a correct action
	// (NonForwardingAction). Thus, this RPC should proceed as normal. There is
	// a routing rule that this RPC would match to that has an incorrect action,
	// but the server should only use the first route matched to with the
	// correct action.
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// This Unary Call should match to a route with an incorrect action. Thus,
	// this RPC should not go through as per A36, and this call should receive
	// an error with codes.Unavailable.
	if _, err = client.UnaryCall(ctx, &testpb.SimpleRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("client.UnaryCall() = _, %v, want _, error code %s", err, codes.Unavailable)
	}

	// This Streaming Call should match to a route with an incorrect action.
	// Thus, this RPC should not go through as per A36, and this call should
	// receive an error with codes.Unavailable.
	stream, err := client.StreamingInputCall(ctx)
	if err != nil {
		t.Fatalf("StreamingInputCall(_) = _, %v, want <nil>", err)
	}
	if _, err = stream.CloseAndRecv(); status.Code(err) != codes.Unavailable || !strings.Contains(err.Error(), "the incoming RPC matched to a route that was not of action type non forwarding") {
		t.Fatalf("streaming RPC should have been denied")
	}

	// This Full Duplex should not match to a route, and thus should return an
	// error and not proceed.
	dStream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall(_) = _, %v, want <nil>", err)
	}
	if _, err = dStream.Recv(); status.Code(err) != codes.Unavailable || !strings.Contains(err.Error(), "the incoming RPC did not match a configured Route") {
		t.Fatalf("streaming RPC should have been denied")
	}
}

// serverListenerWithRBACHTTPFilters returns an xds Listener resource with HTTP Filters defined in the HCM, and a route
// configuration that always matches to a route and a VH.
func serverListenerWithRBACHTTPFilters(host string, port uint32, rbacCfg *rpb.RBAC) *v3listenerpb.Listener {
	// Rather than declare typed config inline, take a HCM proto and append the
	// RBAC Filters to it.
	hcm := &v3httppb.HttpConnectionManager{
		RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
			RouteConfig: &v3routepb.RouteConfiguration{
				Name: "routeName",
				VirtualHosts: []*v3routepb.VirtualHost{{
					Domains: []string{"*"},
					Routes: []*v3routepb.Route{{
						Match: &v3routepb.RouteMatch{
							PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
						},
						Action: &v3routepb.Route_NonForwardingAction{},
					}},
					// This tests override parsing + building when RBAC Filter
					// passed both normal and override config.
					TypedPerFilterConfig: map[string]*anypb.Any{
						"rbac": testutils.MarshalAny(&rpb.RBACPerRoute{Rbac: rbacCfg}),
					},
				}}},
		},
	}
	hcm.HttpFilters = nil
	hcm.HttpFilters = append(hcm.HttpFilters, e2e.HTTPFilter("rbac", rbacCfg))
	hcm.HttpFilters = append(hcm.HttpFilters, e2e.RouterHTTPFilter)

	return &v3listenerpb.Listener{
		Name: fmt.Sprintf(e2e.ServerListenerResourceNameTemplate, net.JoinHostPort(host, strconv.Itoa(int(port)))),
		Address: &v3corepb.Address{
			Address: &v3corepb.Address_SocketAddress{
				SocketAddress: &v3corepb.SocketAddress{
					Address: host,
					PortSpecifier: &v3corepb.SocketAddress_PortValue{
						PortValue: port,
					},
				},
			},
		},
		FilterChains: []*v3listenerpb.FilterChain{
			{
				Name: "v4-wildcard",
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "0.0.0.0",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
					SourceType: v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK,
					SourcePrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "0.0.0.0",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
				},
				Filters: []*v3listenerpb.Filter{
					{
						Name: "filter-1",
						ConfigType: &v3listenerpb.Filter_TypedConfig{
							TypedConfig: testutils.MarshalAny(hcm),
						},
					},
				},
			},
			{
				Name: "v6-wildcard",
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "::",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
					SourceType: v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK,
					SourcePrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "::",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
				},
				Filters: []*v3listenerpb.Filter{
					{
						Name: "filter-1",
						ConfigType: &v3listenerpb.Filter_TypedConfig{
							TypedConfig: testutils.MarshalAny(hcm),
						},
					},
				},
			},
		},
	}
}

// TestRBACHTTPFilter tests the xds configured RBAC HTTP Filter. It sets up the
// full end to end flow, and makes sure certain RPC's are successful and proceed
// as normal and certain RPC's are denied by the RBAC HTTP Filter which gets
// called by hooked xds interceptors.
func (s) TestRBACHTTPFilter(t *testing.T) {
	oldRBAC := envconfig.XDSRBAC
	envconfig.XDSRBAC = true
	defer func() {
		envconfig.XDSRBAC = oldRBAC
	}()
	internal.RegisterRBACHTTPFilterForTesting()
	defer internal.UnregisterRBACHTTPFilterForTesting()
	tests := []struct {
		name                string
		rbacCfg             *rpb.RBAC
		wantStatusEmptyCall codes.Code
		wantStatusUnaryCall codes.Code
	}{
		// This test tests an RBAC HTTP Filter which is configured to allow any RPC.
		// Any RPC passing through this RBAC HTTP Filter should proceed as normal.
		{
			name: "allow-anything",
			rbacCfg: &rpb.RBAC{
				Rules: &v3rbacpb.RBAC{
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
			wantStatusEmptyCall: codes.OK,
			wantStatusUnaryCall: codes.OK,
		},
		// This test tests an RBAC HTTP Filter which is configured to allow only
		// RPC's with certain paths ("UnaryCall"). Only unary calls passing
		// through this RBAC HTTP Filter should proceed as normal, and any
		// others should be denied.
		{
			name: "allow-certain-path",
			rbacCfg: &rpb.RBAC{
				Rules: &v3rbacpb.RBAC{
					Action: v3rbacpb.RBAC_ALLOW,
					Policies: map[string]*v3rbacpb.Policy{
						"certain-path": {
							Permissions: []*v3rbacpb.Permission{
								{Rule: &v3rbacpb.Permission_UrlPath{UrlPath: &v3matcherpb.PathMatcher{Rule: &v3matcherpb.PathMatcher_Path{Path: &v3matcherpb.StringMatcher{MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: "/grpc.testing.TestService/UnaryCall"}}}}}},
							},
							Principals: []*v3rbacpb.Principal{
								{Identifier: &v3rbacpb.Principal_Any{Any: true}},
							},
						},
					},
				},
			},
			wantStatusEmptyCall: codes.PermissionDenied,
			wantStatusUnaryCall: codes.OK,
		},
		// This test that a RBAC Config with nil rules means that every RPC is
		// allowed. This maps to the line "If absent, no enforcing RBAC policy
		// will be applied" from the RBAC Proto documentation for the Rules
		// field.
		{
			name: "absent-rules",
			rbacCfg: &rpb.RBAC{
				Rules: nil,
			},
			wantStatusEmptyCall: codes.OK,
			wantStatusUnaryCall: codes.OK,
		},
		// The two tests below test that configuring the xDS RBAC HTTP Filter
		// with :authority and host header matchers end up being logically
		// equivalent. This represents functionality from this line in A41 -
		// "As documented for HeaderMatcher, Envoy aliases :authority and Host
		// in its header map implementation, so they should be treated
		// equivalent for the RBAC matchers; there must be no behavior change
		// depending on which of the two header names is used in the RBAC
		// policy."

		// This test tests an xDS RBAC Filter with an :authority header matcher.
		{
			name: "match-on-authority",
			rbacCfg: &rpb.RBAC{
				Rules: &v3rbacpb.RBAC{
					Action: v3rbacpb.RBAC_ALLOW,
					Policies: map[string]*v3rbacpb.Policy{
						"match-on-authority": {
							Permissions: []*v3rbacpb.Permission{
								{Rule: &v3rbacpb.Permission_Header{Header: &v3routepb.HeaderMatcher{Name: ":authority", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PrefixMatch{PrefixMatch: "my-service-fallback"}}}},
							},
							Principals: []*v3rbacpb.Principal{
								{Identifier: &v3rbacpb.Principal_Any{Any: true}},
							},
						},
					},
				},
			},
			wantStatusEmptyCall: codes.OK,
			wantStatusUnaryCall: codes.OK,
		},
		// This test tests that configuring an xDS RBAC Filter with a host
		// header matcher has the same behavior as if it was configured with
		// :authority. Since host and authority are aliased, this should still
		// continue to match on incoming RPC's :authority, just as the test
		// above.
		{
			name: "match-on-host",
			rbacCfg: &rpb.RBAC{
				Rules: &v3rbacpb.RBAC{
					Action: v3rbacpb.RBAC_ALLOW,
					Policies: map[string]*v3rbacpb.Policy{
						"match-on-authority": {
							Permissions: []*v3rbacpb.Permission{
								{Rule: &v3rbacpb.Permission_Header{Header: &v3routepb.HeaderMatcher{Name: "host", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PrefixMatch{PrefixMatch: "my-service-fallback"}}}},
							},
							Principals: []*v3rbacpb.Principal{
								{Identifier: &v3rbacpb.Principal_Any{Any: true}},
							},
						},
					},
				},
			},
			wantStatusEmptyCall: codes.OK,
			wantStatusUnaryCall: codes.OK,
		},
		// This test tests that the RBAC HTTP Filter hard codes the :method
		// header to POST. Since the RBAC Configuration says to deny every RPC
		// with a method :POST, every RPC tried should be denied.
		{
			name: "deny-post",
			rbacCfg: &rpb.RBAC{
				Rules: &v3rbacpb.RBAC{
					Action: v3rbacpb.RBAC_DENY,
					Policies: map[string]*v3rbacpb.Policy{
						"post-method": {
							Permissions: []*v3rbacpb.Permission{
								{Rule: &v3rbacpb.Permission_Header{Header: &v3routepb.HeaderMatcher{Name: ":method", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "POST"}}}},
							},
							Principals: []*v3rbacpb.Principal{
								{Identifier: &v3rbacpb.Principal_Any{Any: true}},
							},
						},
					},
				},
			},
			wantStatusEmptyCall: codes.PermissionDenied,
			wantStatusUnaryCall: codes.PermissionDenied,
		},
		// This test tests that RBAC ignores the TE: trailers header (which is
		// hardcoded in http2_client.go for every RPC). Since the RBAC
		// Configuration says to only ALLOW RPC's with a TE: Trailers, every RPC
		// tried should be denied.
		{
			name: "allow-only-te",
			rbacCfg: &rpb.RBAC{
				Rules: &v3rbacpb.RBAC{
					Action: v3rbacpb.RBAC_ALLOW,
					Policies: map[string]*v3rbacpb.Policy{
						"post-method": {
							Permissions: []*v3rbacpb.Permission{
								{Rule: &v3rbacpb.Permission_Header{Header: &v3routepb.HeaderMatcher{Name: "TE", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: "trailers"}}}},
							},
							Principals: []*v3rbacpb.Principal{
								{Identifier: &v3rbacpb.Principal_Any{Any: true}},
							},
						},
					},
				},
			},
			wantStatusEmptyCall: codes.PermissionDenied,
			wantStatusUnaryCall: codes.PermissionDenied,
		},
		// This test tests that an RBAC Config with Action.LOG configured allows
		// every RPC through. This maps to the line "At this time, if the
		// RBAC.action is Action.LOG then the policy will be completely ignored,
		// as if RBAC was not configurated." from A41
		{
			name: "action-log",
			rbacCfg: &rpb.RBAC{
				Rules: &v3rbacpb.RBAC{
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
			wantStatusEmptyCall: codes.OK,
			wantStatusUnaryCall: codes.OK,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			func() {
				managementServer, nodeID, bootstrapContents, resolver, cleanup1 := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
				defer cleanup1()

				lis, cleanup2 := setupGRPCServer(t, bootstrapContents)
				defer cleanup2()

				host, port, err := hostPortFromListener(lis)
				if err != nil {
					t.Fatalf("failed to retrieve host and port of server: %v", err)
				}
				const serviceName = "my-service-fallback"
				resources := e2e.DefaultClientResources(e2e.ResourceParams{
					DialTarget: serviceName,
					NodeID:     nodeID,
					Host:       host,
					Port:       port,
					SecLevel:   e2e.SecurityLevelNone,
				})
				inboundLis := serverListenerWithRBACHTTPFilters(host, port, test.rbacCfg)
				resources.Listeners = append(resources.Listeners, inboundLis)

				ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
				defer cancel()
				// Setup the management server with client and server-side resources.
				if err := managementServer.Update(ctx, resources); err != nil {
					t.Fatal(err)
				}

				cc, err := grpc.DialContext(ctx, fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
				if err != nil {
					t.Fatalf("failed to dial local test server: %v", err)
				}
				defer cc.Close()

				client := testgrpc.NewTestServiceClient(cc)

				if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); status.Code(err) != test.wantStatusEmptyCall {
					t.Fatalf("EmptyCall() returned err with status: %v, wantStatusEmptyCall: %v", status.Code(err), test.wantStatusEmptyCall)
				}

				if _, err := client.UnaryCall(ctx, &testpb.SimpleRequest{}); status.Code(err) != test.wantStatusUnaryCall {
					t.Fatalf("UnaryCall() returned err with status: %v, wantStatusUnaryCall: %v", err, test.wantStatusUnaryCall)
				}

				// Toggle the RBAC Env variable off, this should disable RBAC and allow any RPC"s through (will not go through
				// routing or processed by HTTP Filters and thus will never get denied by RBAC).
				envconfig.XDSRBAC = false
				if _, err := client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.OK {
					t.Fatalf("EmptyCall() returned err with status: %v, once RBAC is disabled all RPC's should proceed as normal", status.Code(err))
				}
				if _, err := client.UnaryCall(ctx, &testpb.SimpleRequest{}); status.Code(err) != codes.OK {
					t.Fatalf("UnaryCall() returned err with status: %v, once RBAC is disabled all RPC's should proceed as normal", status.Code(err))
				}
				// Toggle RBAC back on for next iterations.
				envconfig.XDSRBAC = true
			}()
		})
	}
}

// serverListenerWithBadRouteConfiguration returns an xds Listener resource with
// a Route Configuration that will never successfully match in order to test
// RBAC Environment variable being toggled on and off.
func serverListenerWithBadRouteConfiguration(host string, port uint32) *v3listenerpb.Listener {
	return &v3listenerpb.Listener{
		Name: fmt.Sprintf(e2e.ServerListenerResourceNameTemplate, net.JoinHostPort(host, strconv.Itoa(int(port)))),
		Address: &v3corepb.Address{
			Address: &v3corepb.Address_SocketAddress{
				SocketAddress: &v3corepb.SocketAddress{
					Address: host,
					PortSpecifier: &v3corepb.SocketAddress_PortValue{
						PortValue: port,
					},
				},
			},
		},
		FilterChains: []*v3listenerpb.FilterChain{
			{
				Name: "v4-wildcard",
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "0.0.0.0",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
					SourceType: v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK,
					SourcePrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "0.0.0.0",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
				},
				Filters: []*v3listenerpb.Filter{
					{
						Name: "filter-1",
						ConfigType: &v3listenerpb.Filter_TypedConfig{
							TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
								RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
									RouteConfig: &v3routepb.RouteConfiguration{
										Name: "routeName",
										VirtualHosts: []*v3routepb.VirtualHost{{
											// Incoming RPC's will try and match to Virtual Hosts based on their :authority header.
											// Thus, incoming RPC's will never match to a Virtual Host (server side requires matching
											// to a VH/Route of type Non Forwarding Action to proceed normally), and all incoming RPC's
											// with this route configuration will be denied.
											Domains: []string{"will-never-match"},
											Routes: []*v3routepb.Route{{
												Match: &v3routepb.RouteMatch{
													PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
												},
												Action: &v3routepb.Route_NonForwardingAction{},
											}}}}},
								},
								HttpFilters: []*v3httppb.HttpFilter{e2e.RouterHTTPFilter},
							}),
						},
					},
				},
			},
			{
				Name: "v6-wildcard",
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "::",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
					SourceType: v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK,
					SourcePrefixRanges: []*v3corepb.CidrRange{
						{
							AddressPrefix: "::",
							PrefixLen: &wrapperspb.UInt32Value{
								Value: uint32(0),
							},
						},
					},
				},
				Filters: []*v3listenerpb.Filter{
					{
						Name: "filter-1",
						ConfigType: &v3listenerpb.Filter_TypedConfig{
							TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
								RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
									RouteConfig: &v3routepb.RouteConfiguration{
										Name: "routeName",
										VirtualHosts: []*v3routepb.VirtualHost{{
											// Incoming RPC's will try and match to Virtual Hosts based on their :authority header.
											// Thus, incoming RPC's will never match to a Virtual Host (server side requires matching
											// to a VH/Route of type Non Forwarding Action to proceed normally), and all incoming RPC's
											// with this route configuration will be denied.
											Domains: []string{"will-never-match"},
											Routes: []*v3routepb.Route{{
												Match: &v3routepb.RouteMatch{
													PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
												},
												Action: &v3routepb.Route_NonForwardingAction{},
											}}}}},
								},
								HttpFilters: []*v3httppb.HttpFilter{e2e.RouterHTTPFilter},
							}),
						},
					},
				},
			},
		},
	}
}

func (s) TestRBACToggledOn_WithBadRouteConfiguration(t *testing.T) {
	// Turn RBAC support on.
	oldRBAC := envconfig.XDSRBAC
	envconfig.XDSRBAC = true
	defer func() {
		envconfig.XDSRBAC = oldRBAC
	}()

	managementServer, nodeID, bootstrapContents, resolver, cleanup1 := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup1()

	lis, cleanup2 := setupGRPCServer(t, bootstrapContents)
	defer cleanup2()

	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("failed to retrieve host and port of server: %v", err)
	}
	const serviceName = "my-service-fallback"

	// The inbound listener needs a route table that will never match on a VH,
	// and thus shouldn't allow incoming RPC's to proceed.
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       host,
		Port:       port,
		SecLevel:   e2e.SecurityLevelNone,
	})
	// Since RBAC support is turned ON, all the RPC's should get denied with
	// status code Unavailable due to not matching to a route of type Non
	// Forwarding Action (Route Table not configured properly).
	inboundLis := serverListenerWithBadRouteConfiguration(host, port)
	resources.Listeners = append(resources.Listeners, inboundLis)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Setup the management server with client and server-side resources.
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.DialContext(ctx, fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testpb.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("EmptyCall() returned err with status: %v, if RBAC is disabled all RPC's should proceed as normal", status.Code(err))
	}
	if _, err := client.UnaryCall(ctx, &testpb.SimpleRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("UnaryCall() returned err with status: %v, if RBAC is disabled all RPC's should proceed as normal", status.Code(err))
	}
}

func (s) TestRBACToggledOff_WithBadRouteConfiguration(t *testing.T) {
	// Turn RBAC support off.
	oldRBAC := envconfig.XDSRBAC
	envconfig.XDSRBAC = false
	defer func() {
		envconfig.XDSRBAC = oldRBAC
	}()

	managementServer, nodeID, bootstrapContents, resolver, cleanup1 := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup1()

	lis, cleanup2 := setupGRPCServer(t, bootstrapContents)
	defer cleanup2()

	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("failed to retrieve host and port of server: %v", err)
	}
	const serviceName = "my-service-fallback"

	// The inbound listener needs a route table that will never match on a VH,
	// and thus shouldn't allow incoming RPC's to proceed.
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       host,
		Port:       port,
		SecLevel:   e2e.SecurityLevelNone,
	})
	// This bad route configuration shouldn't affect incoming RPC's from
	// proceeding as normal, as the configuration shouldn't be parsed due to the
	// RBAC Environment variable not being set to true.
	inboundLis := serverListenerWithBadRouteConfiguration(host, port)
	resources.Listeners = append(resources.Listeners, inboundLis)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Setup the management server with client and server-side resources.
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.DialContext(ctx, fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testpb.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.OK {
		t.Fatalf("EmptyCall() returned err with status: %v, if RBAC is disabled all RPC's should proceed as normal", status.Code(err))
	}
	if _, err := client.UnaryCall(ctx, &testpb.SimpleRequest{}); status.Code(err) != codes.OK {
		t.Fatalf("UnaryCall() returned err with status: %v, if RBAC is disabled all RPC's should proceed as normal", status.Code(err))
	}
}
