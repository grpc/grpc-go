/*
 *
 * Copyright 2020 gRPC authors.
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

package xdsresource

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/httpfilter/router"
	"google.golang.org/protobuf/testing/protocmp"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const defaultTestTimeout = 10 * time.Second

var (
	cmpOpts = cmp.Options{
		cmpopts.EquateEmpty(),
		cmp.FilterValues(func(_, _ error) bool { return true }, cmpopts.EquateErrors()),
		cmp.Comparer(func(_, _ time.Time) bool { return true }),
		protocmp.Transform(),
	}
	routeConfig = &v3routepb.RouteConfiguration{
		Name: "routeName",
		VirtualHosts: []*v3routepb.VirtualHost{{
			Domains: []string{"lds.target.good:3333"},
			Routes: []*v3routepb.Route{{
				Match: &v3routepb.RouteMatch{
					PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
				},
				Action: &v3routepb.Route_NonForwardingAction{},
			}}}}}
	inlineRouteConfig = &RouteConfigUpdate{
		VirtualHosts: []*VirtualHost{{
			Domains: []string{"lds.target.good:3333"},
			Routes:  []*Route{{Prefix: newStringP("/"), ActionType: RouteActionNonForwardingAction}},
		}}}

	validServerSideHTTPFilter1 = &v3httppb.HttpFilter{
		Name:       "serverOnlyCustomFilter",
		ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: serverOnlyCustomFilterConfig},
	}
	validServerSideHTTPFilter2 = &v3httppb.HttpFilter{
		Name:       "serverOnlyCustomFilter2",
		ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: serverOnlyCustomFilterConfig},
	}
	emptyRouterFilter  = e2e.RouterHTTPFilter
	localSocketAddress = &v3corepb.Address{
		Address: &v3corepb.Address_SocketAddress{
			SocketAddress: &v3corepb.SocketAddress{
				Address: "0.0.0.0",
				PortSpecifier: &v3corepb.SocketAddress_PortValue{
					PortValue: 9999,
				},
			},
		},
	}
)

func makeRouterFilter(t *testing.T) HTTPFilter {
	t.Helper()
	routerBuilder := httpfilter.Get(router.TypeURL)
	routerConfig, err := routerBuilder.ParseFilterConfig(testutils.MarshalAny(t, &v3routerpb.Router{}))
	if err != nil {
		t.Fatalf("Failed to parse Router filter configuration: %v", err)
	}
	return HTTPFilter{Name: "router", Filter: routerBuilder, Config: routerConfig}
}

func makeRouterFilterList(t *testing.T) []HTTPFilter {
	return []HTTPFilter{makeRouterFilter(t)}
}

func emptyValidNetworkFilters(t *testing.T) []*v3listenerpb.Filter {
	return []*v3listenerpb.Filter{
		{
			Name: "filter-1",
			ConfigType: &v3listenerpb.Filter_TypedConfig{
				TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
					RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
						RouteConfig: routeConfig,
					},
					HttpFilters: []*v3httppb.HttpFilter{emptyRouterFilter},
				}),
			},
		},
	}
}
