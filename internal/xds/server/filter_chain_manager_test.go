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
 */

package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/credentials/tls/certprovider"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/clients/xdsclient"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/httpfilter/router"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
)

const (
	topLevel = "top level"
	vhLevel  = "virtual host level"
	rLevel   = "route level"
)

func init() {
	certprovider.Register(&testCertProviderBuilder{})
}

type testCertProviderBuilder struct{}

func (b *testCertProviderBuilder) ParseConfig(any) (*certprovider.BuildableConfig, error) {
	return certprovider.NewBuildableConfig("test_cert_provider", nil, func(certprovider.BuildOptions) certprovider.Provider {
		return nil
	}), nil
}

func (b *testCertProviderBuilder) Name() string {
	return "test_cert_provider"
}

// newFilterChainManagerForTesting creates a filterChainManager using the
// newFilterChainManager() function. It parses the provided listener resource
// using the xdsresource package.
func newFilterChainManagerForTesting(t *testing.T, lis *v3listenerpb.Listener) *filterChainManager {
	t.Helper()
	if lis.Address == nil {
		lis.Address = &v3corepb.Address{
			Address: &v3corepb.Address_SocketAddress{
				SocketAddress: &v3corepb.SocketAddress{
					Address:       "0.0.0.0",
					PortSpecifier: &v3corepb.SocketAddress_PortValue{PortValue: 80},
				},
			},
		}
	}
	if lis.Name == "" {
		lis.Name = "default_listener"
	}
	lAny, err := anypb.New(lis)
	if err != nil {
		t.Fatalf("anypb.New() failed: %v", err)
	}
	bc, err := bootstrap.NewConfigFromContents([]byte(`{
		"xds_servers": [
			{
				"server_uri": "ipv4:///127.0.0.1:443",
				"channel_creds": [
					{
						"type": "insecure"
					}
				]
			}
		],
		"certificate_providers": {
			"default": {
				"plugin_name": "test_cert_provider"
			},
			"instance1": {
				"plugin_name": "test_cert_provider"
			},
            "unspecified-dest-and-source-prefix": {
                "plugin_name": "test_cert_provider"
            },
            "wildcard-prefixes-v4": {
                "plugin_name": "test_cert_provider"
            },
            "wildcard-source-prefix-v6": {
                "plugin_name": "test_cert_provider"
            },
            "specific-destination-prefix-unspecified-source-type": {
                "plugin_name": "test_cert_provider"
            },
            "specific-destination-prefix-specific-source-type": {
                "plugin_name": "test_cert_provider"
            },
            "specific-destination-prefix-specific-source-type-specific-source-prefix": {
                "plugin_name": "test_cert_provider"
            },
            "specific-destination-prefix-specific-source-type-specific-source-prefix-specific-source-port": {
                "plugin_name": "test_cert_provider"
            }
		}
	}`))
	if err != nil {
		t.Fatalf("bootstrap.NewConfigFromContents() failed: %v", err)
	}
	decoder := xdsresource.NewListenerResourceTypeDecoder(bc)
	res, err := decoder.Decode(xdsclient.NewAnyProto(lAny), xdsclient.DecodeOptions{})
	if err != nil {
		t.Fatalf("decoder.Decode() failed: %v", err)
	}
	lrd, ok := res.Resource.(*xdsresource.ListenerResourceData)
	if !ok {
		t.Fatalf("Resource type mismatch: %T", res.Resource)
	}
	upd := lrd.Resource
	if upd.TCPListener == nil {
		t.Fatalf("Decoded listener is not TCP or failed validation")
	}
	return newFilterChainManager(&upd.TCPListener.FilterChains, &upd.TCPListener.DefaultFilterChain)
}

func emptyValidNetworkFilters(t *testing.T) []*v3listenerpb.Filter {
	return []*v3listenerpb.Filter{
		{
			Name: "filter-1",
			ConfigType: &v3listenerpb.Filter_TypedConfig{
				TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
					RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
						RouteConfig: &v3routepb.RouteConfiguration{
							Name: "routeName",
							VirtualHosts: []*v3routepb.VirtualHost{{
								Domains: []string{"lds.target.good:3333"},
								Routes: []*v3routepb.Route{{
									Match: &v3routepb.RouteMatch{
										PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
									},
									Action: &v3routepb.Route_NonForwardingAction{},
								}},
							}},
						},
					},
					HttpFilters: []*v3httppb.HttpFilter{e2e.RouterHTTPFilter},
				}),
			},
		},
	}
}

var inlineRouteConfig = &xdsresource.RouteConfigUpdate{
	VirtualHosts: []*xdsresource.VirtualHost{{
		Domains: []string{"lds.target.good:3333"},
		Routes:  []*xdsresource.Route{{Prefix: newStringP("/"), ActionType: xdsresource.RouteActionNonForwardingAction}},
	}},
}

func newStringP(s string) *string {
	return &s
}

func makeRouterFilterList(t *testing.T) []xdsresource.HTTPFilter {
	routerBuilder := httpfilter.Get(router.TypeURL)
	routerConfig, _ := routerBuilder.ParseFilterConfig(testutils.MarshalAny(t, &v3routerpb.Router{}))
	return []xdsresource.HTTPFilter{{
		Name:   "router",
		Filter: routerBuilder,
		Config: routerConfig,
	}}
}

func (s) TestLookup_Failures(t *testing.T) {
	tests := []struct {
		desc    string
		lis     *v3listenerpb.Listener
		params  lookupParams
		wantErr string
	}{
		{
			desc: "no_destination_prefix_match",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)}},
						Filters:          emptyValidNetworkFilters(t),
					},
				},
			},
			params: lookupParams{
				isUnspecifiedListener: true,
				dstAddr:               net.IPv4(10, 1, 1, 1),
			},
			wantErr: "no matching filter chain based on destination prefix match",
		},
		{
			desc: "no_source_type_match",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							SourceType:   v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK,
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
			params: lookupParams{
				isUnspecifiedListener: true,
				dstAddr:               net.IPv4(192, 168, 100, 1),
				srcAddr:               net.IPv4(192, 168, 100, 2),
			},
			wantErr: "no matching filter chain based on source type match",
		},
		{
			desc: "no_source_prefix_match",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 24)},
							SourceType:         v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK,
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
			params: lookupParams{
				isUnspecifiedListener: true,
				dstAddr:               net.IPv4(192, 168, 100, 1),
				srcAddr:               net.IPv4(192, 168, 100, 1),
			},
			wantErr: "no matching filter chain after all match criteria",
		},
		{
			desc: "multiple_matching_filter_chains",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePorts: []uint32{1, 2, 3}},
						Filters:          emptyValidNetworkFilters(t),
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							SourcePorts:  []uint32{1},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
			params: lookupParams{
				// IsUnspecified is not set. This means that the destination
				// prefix matchers will be ignored.
				dstAddr: net.IPv4(192, 168, 100, 1),
				srcAddr: net.IPv4(192, 168, 100, 1),
				srcPort: 1,
			},
			wantErr: "multiple matching filter chains",
		},
		{
			desc: "no_default_filter_chain",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePorts: []uint32{1, 2, 3}},
						Filters:          emptyValidNetworkFilters(t),
					},
				},
			},
			params: lookupParams{
				isUnspecifiedListener: true,
				dstAddr:               net.IPv4(192, 168, 100, 1),
				srcAddr:               net.IPv4(192, 168, 100, 1),
				srcPort:               80,
			},
			wantErr: "no matching filter chain after all match criteria",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			fci := newFilterChainManagerForTesting(t, test.lis)
			if _, err := fci.lookup(test.params); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("filterChainManager.lookup() failed with %q want %q", err, test.wantErr)
			}
		})
	}
}

func (s) TestLookup_Successes(t *testing.T) {
	lisWithDefaultChain := &v3listenerpb.Listener{
		FilterChains: []*v3listenerpb.FilterChain{
			{
				FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)}},
				TransportSocket: &v3corepb.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
							CommonTlsContext: &v3tlspb.CommonTlsContext{
								TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{InstanceName: "instance1"},
							},
						}),
					},
				},
				Filters: emptyValidNetworkFilters(t),
			},
		},
		// A default filter chain with an empty transport socket.
		DefaultFilterChain: &v3listenerpb.FilterChain{
			TransportSocket: &v3corepb.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &v3corepb.TransportSocket_TypedConfig{
					TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
						CommonTlsContext: &v3tlspb.CommonTlsContext{
							TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{InstanceName: "default"},
						},
					}),
				},
			},
			Filters: emptyValidNetworkFilters(t),
		},
	}
	lisWithoutDefaultChain := &v3listenerpb.Listener{
		FilterChains: []*v3listenerpb.FilterChain{
			{
				TransportSocket: transportSocketWithInstanceName(t, "unspecified-dest-and-source-prefix"),
				Filters:         emptyValidNetworkFilters(t),
			},
			{
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges:       []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("0.0.0.0", 0)},
					SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("0.0.0.0", 0)},
				},
				TransportSocket: transportSocketWithInstanceName(t, "wildcard-prefixes-v4"),
				Filters:         emptyValidNetworkFilters(t),
			},
			{
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("::", 0)},
				},
				TransportSocket: transportSocketWithInstanceName(t, "wildcard-source-prefix-v6"),
				Filters:         emptyValidNetworkFilters(t),
			},
			{
				FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)}},
				TransportSocket:  transportSocketWithInstanceName(t, "specific-destination-prefix-unspecified-source-type"),
				Filters:          emptyValidNetworkFilters(t),
			},
			{
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 24)},
					SourceType:   v3listenerpb.FilterChainMatch_EXTERNAL,
				},
				TransportSocket: transportSocketWithInstanceName(t, "specific-destination-prefix-specific-source-type"),
				Filters:         emptyValidNetworkFilters(t),
			},
			{
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges:       []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 24)},
					SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.92.1", 24)},
					SourceType:         v3listenerpb.FilterChainMatch_EXTERNAL,
				},
				TransportSocket: transportSocketWithInstanceName(t, "specific-destination-prefix-specific-source-type-specific-source-prefix"),
				Filters:         emptyValidNetworkFilters(t),
			},
			{
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges:       []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 24)},
					SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.92.1", 24)},
					SourceType:         v3listenerpb.FilterChainMatch_EXTERNAL,
					SourcePorts:        []uint32{80},
				},
				TransportSocket: transportSocketWithInstanceName(t, "specific-destination-prefix-specific-source-type-specific-source-prefix-specific-source-port"),
				Filters:         emptyValidNetworkFilters(t),
			},
		},
	}

	tests := []struct {
		desc   string
		lis    *v3listenerpb.Listener
		params lookupParams
		wantFC *filterChain
	}{
		{
			desc: "default_filter_chain",
			lis:  lisWithDefaultChain,
			params: lookupParams{
				isUnspecifiedListener: true,
				dstAddr:               net.IPv4(10, 1, 1, 1),
			},
			wantFC: &filterChain{
				securityCfg:       &xdsresource.SecurityConfig{IdentityInstanceName: "default"},
				inlineRouteConfig: inlineRouteConfig,
				httpFilters:       makeRouterFilterList(t),
			},
		},
		{
			desc: "unspecified_destination_match",
			lis:  lisWithoutDefaultChain,
			params: lookupParams{
				isUnspecifiedListener: true,
				dstAddr:               netip.MustParseAddr("2001:68::db8").AsSlice(),
				srcAddr:               net.IPv4(10, 1, 1, 1),
				srcPort:               1,
			},
			wantFC: &filterChain{
				securityCfg:       &xdsresource.SecurityConfig{IdentityInstanceName: "unspecified-dest-and-source-prefix"},
				inlineRouteConfig: inlineRouteConfig,
				httpFilters:       makeRouterFilterList(t),
			},
		},
		{
			desc: "wildcard_destination_match_v4",
			lis:  lisWithoutDefaultChain,
			params: lookupParams{
				isUnspecifiedListener: true,
				dstAddr:               net.IPv4(10, 1, 1, 1),
				srcAddr:               net.IPv4(10, 1, 1, 1),
				srcPort:               1,
			},
			wantFC: &filterChain{
				securityCfg:       &xdsresource.SecurityConfig{IdentityInstanceName: "wildcard-prefixes-v4"},
				inlineRouteConfig: inlineRouteConfig,
				httpFilters:       makeRouterFilterList(t),
			},
		},
		{
			desc: "wildcard_source_match_v6",
			lis:  lisWithoutDefaultChain,
			params: lookupParams{
				isUnspecifiedListener: true,
				dstAddr:               netip.MustParseAddr("2001:68::1").AsSlice(),
				srcAddr:               netip.MustParseAddr("2001:68::2").AsSlice(),
				srcPort:               1,
			},
			wantFC: &filterChain{
				securityCfg:       &xdsresource.SecurityConfig{IdentityInstanceName: "wildcard-source-prefix-v6"},
				inlineRouteConfig: inlineRouteConfig,
				httpFilters:       makeRouterFilterList(t),
			},
		},
		{
			desc: "specific_destination_and_wildcard_source_type_match",
			lis:  lisWithoutDefaultChain,
			params: lookupParams{
				isUnspecifiedListener: true,
				dstAddr:               net.IPv4(192, 168, 100, 1),
				srcAddr:               net.IPv4(192, 168, 100, 1),
				srcPort:               80,
			},
			wantFC: &filterChain{
				securityCfg:       &xdsresource.SecurityConfig{IdentityInstanceName: "specific-destination-prefix-unspecified-source-type"},
				inlineRouteConfig: inlineRouteConfig,
				httpFilters:       makeRouterFilterList(t),
			},
		},
		{
			desc: "specific_destination_and_source_type_match",
			lis:  lisWithoutDefaultChain,
			params: lookupParams{
				isUnspecifiedListener: true,
				dstAddr:               net.IPv4(192, 168, 1, 1),
				srcAddr:               net.IPv4(10, 1, 1, 1),
				srcPort:               80,
			},
			wantFC: &filterChain{
				securityCfg:       &xdsresource.SecurityConfig{IdentityInstanceName: "specific-destination-prefix-specific-source-type"},
				inlineRouteConfig: inlineRouteConfig,
				httpFilters:       makeRouterFilterList(t),
			},
		},
		{
			desc: "specific_destination_source_type_and_source_prefix",
			lis:  lisWithoutDefaultChain,
			params: lookupParams{
				isUnspecifiedListener: true,
				dstAddr:               net.IPv4(192, 168, 1, 1),
				srcAddr:               net.IPv4(192, 168, 92, 100),
				srcPort:               70,
			},
			wantFC: &filterChain{
				securityCfg:       &xdsresource.SecurityConfig{IdentityInstanceName: "specific-destination-prefix-specific-source-type-specific-source-prefix"},
				inlineRouteConfig: inlineRouteConfig,
				httpFilters:       makeRouterFilterList(t),
			},
		},
		{
			desc: "specific_destination_source_type_source_prefix_and_source_port",
			lis:  lisWithoutDefaultChain,
			params: lookupParams{
				isUnspecifiedListener: true,
				dstAddr:               net.IPv4(192, 168, 1, 1),
				srcAddr:               net.IPv4(192, 168, 92, 100),
				srcPort:               80,
			},
			wantFC: &filterChain{
				securityCfg:       &xdsresource.SecurityConfig{IdentityInstanceName: "specific-destination-prefix-specific-source-type-specific-source-prefix-specific-source-port"},
				inlineRouteConfig: inlineRouteConfig,
				httpFilters:       makeRouterFilterList(t),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			fci := newFilterChainManagerForTesting(t, test.lis)
			gotFC, err := fci.lookup(test.params)
			if err != nil {
				t.Fatalf("FilterChainManager.Lookup(%v) failed: %v", test.params, err)
			}
			if diff := cmp.Diff(gotFC, test.wantFC, cmpopts.EquateEmpty(), cmpopts.IgnoreFields(filterChain{}, "usableRouteConfiguration"), cmp.AllowUnexported(filterChainManager{}, destPrefixEntry{}, sourcePrefixes{}, sourcePrefixEntry{}, filterChain{}, usableRouteConfiguration{})); diff != "" {
				t.Fatalf("FilterChainManager.Lookup(%v) mismatch (-got +want):\n%s", test.params, diff)
			}
		})
	}
}

type filterCfg struct {
	httpfilter.FilterConfig
	// Level is what differentiates top level filters ("top level") vs. second
	// level ("virtual host level"), and third level ("route level").
	level string
}

type filterBuilder struct {
	httpfilter.Builder
}

func (fb *filterBuilder) TypeURLs() []string { return []string{"custom.server.filter"} }

func (fb *filterBuilder) BuildServerFilter() httpfilter.ServerFilter {
	return fb
}

func (fb *filterBuilder) Close() {}

var _ httpfilter.ServerFilterBuilder = &filterBuilder{}

func (fb *filterBuilder) BuildServerInterceptor(config httpfilter.FilterConfig, override httpfilter.FilterConfig) (iresolver.ServerInterceptor, func(), error) {
	var level string
	level = config.(filterCfg).level

	if override != nil {
		level = override.(filterCfg).level
	}
	return &serverInterceptor{level: level}, nil, nil
}

type serverInterceptor struct {
	level string
}

func (si *serverInterceptor) AllowRPC(context.Context) error {
	return errors.New(si.level)
}

func (s) TestHTTPFilterInstantiation(t *testing.T) {
	tests := []struct {
		name        string
		filters     []xdsresource.HTTPFilter
		routeConfig xdsresource.RouteConfigUpdate
		// A list of strings which will be built from iterating through the
		// filters ["top level", "vh level", "route level", "route level"...]
		// wantErrs is the list of error strings that will be constructed from
		// the deterministic iteration through the vh list and route list. The
		// error string will be determined by the level of config that the
		// filter builder receives (i.e. top level, vs. virtual host level vs.
		// route level).
		wantErrs []string
	}{
		{
			name: "one http filter no overrides",
			filters: []xdsresource.HTTPFilter{
				{Name: "server-interceptor", Filter: &filterBuilder{}, Config: filterCfg{level: topLevel}},
			},
			routeConfig: xdsresource.RouteConfigUpdate{
				VirtualHosts: []*xdsresource.VirtualHost{
					{
						Domains: []string{"target"},
						Routes: []*xdsresource.Route{{
							Prefix: newStringP("1"),
						},
						},
					},
				}},
			wantErrs: []string{topLevel},
		},
		{
			name: "one http filter vh override",
			filters: []xdsresource.HTTPFilter{
				{Name: "server-interceptor", Filter: &filterBuilder{}, Config: filterCfg{level: topLevel}},
			},
			routeConfig: xdsresource.RouteConfigUpdate{
				VirtualHosts: []*xdsresource.VirtualHost{
					{
						Domains: []string{"target"},
						Routes: []*xdsresource.Route{{
							Prefix: newStringP("1"),
						},
						},
						HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
							"server-interceptor": filterCfg{level: vhLevel},
						},
					},
				}},
			wantErrs: []string{vhLevel},
		},
		{
			name: "one http filter route override",
			filters: []xdsresource.HTTPFilter{
				{Name: "server-interceptor", Filter: &filterBuilder{}, Config: filterCfg{level: topLevel}},
			},
			routeConfig: xdsresource.RouteConfigUpdate{
				VirtualHosts: []*xdsresource.VirtualHost{
					{
						Domains: []string{"target"},
						Routes: []*xdsresource.Route{{
							Prefix: newStringP("1"),
							HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
								"server-interceptor": filterCfg{level: rLevel},
							},
						},
						},
					},
				}},
			wantErrs: []string{rLevel},
		},
		{
			name: "three routes with different overrides",
			filters: []xdsresource.HTTPFilter{
				{Name: "server-interceptor", Filter: &filterBuilder{}, Config: filterCfg{level: topLevel}},
			},
			routeConfig: xdsresource.RouteConfigUpdate{
				VirtualHosts: []*xdsresource.VirtualHost{
					{
						Domains: []string{"no overrides"},
						Routes: []*xdsresource.Route{{
							Prefix: newStringP("1"),
						}},
					},
					{
						Domains: []string{"virtual host override"},
						Routes: []*xdsresource.Route{{
							Prefix: newStringP("1"),
						}},
						HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
							"server-interceptor": filterCfg{level: vhLevel},
						},
					},
					{
						Domains: []string{"route and virtual host override"},
						Routes: []*xdsresource.Route{{
							Prefix: newStringP("1"),
							HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
								"server-interceptor": filterCfg{level: rLevel},
							},
						}},
						HTTPFilterConfigOverride: map[string]httpfilter.FilterConfig{
							"server-interceptor": filterCfg{level: vhLevel},
						},
					},
				},
			},
			wantErrs: []string{topLevel, vhLevel, rLevel},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fc := filterChain{
				httpFilters: test.filters,
			}

			filters := make(map[serverFilterKey]*refCountedServerFilter)
			provider := func(filter xdsresource.HTTPFilter) (httpfilter.ServerFilter, error) {
				builder, ok := filter.Filter.(httpfilter.ServerFilterBuilder)
				if !ok {
					return nil, fmt.Errorf("filter %q does not support use in server", filter.Name)
				}
				return getOrCreateServerFilterWithMap(filters, builder, newServerFilterKey(&filter)), nil
			}
			urc := fc.constructUsableRouteConfiguration(test.routeConfig, provider)
			if urc.err != nil {
				t.Fatalf("Error constructing usable route configuration: %v", urc.err)
			}
			// Build out list of errors by iterating through the virtual hosts and routes,
			// and running the filters in route configurations.
			var errs []string
			for _, vh := range urc.vhs {
				for _, r := range vh.routes {
					errs = append(errs, r.interceptor.AllowRPC(ctx).Error())
				}
			}
			if !cmp.Equal(errs, test.wantErrs) {
				t.Fatalf("List of errors %v, want %v", errs, test.wantErrs)
			}
		})
	}
}

func transportSocketWithInstanceName(t *testing.T, name string) *v3corepb.TransportSocket {
	return &v3corepb.TransportSocket{
		Name: "envoy.transport_sockets.tls",
		ConfigType: &v3corepb.TransportSocket_TypedConfig{
			TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
				CommonTlsContext: &v3tlspb.CommonTlsContext{
					TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{InstanceName: name},
				},
			}),
		},
	}
}

func cidrRangeFromAddressAndPrefixLen(address string, prefixLen int) *v3corepb.CidrRange {
	return &v3corepb.CidrRange{
		AddressPrefix: address,
		PrefixLen: &wrapperspb.UInt32Value{
			Value: uint32(prefixLen),
		},
	}
}

func (s) TestLookup_DroppedChainFallback(t *testing.T) {
	lis := &v3listenerpb.Listener{
		FilterChains: []*v3listenerpb.FilterChain{
			{
				// This chain has server names, so it should be dropped.
				Name: "test-filter-chain",
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.100.1", 32)},
					ServerNames:  []string{"foo"},
				},
				Filters: emptyValidNetworkFilters(t),
			},
			{
				// This chain matches destination and has no unsupported fields.
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.100.0", 16)},
				},
				Filters: emptyValidNetworkFilters(t),
			},
		},
	}
	params := lookupParams{
		isUnspecifiedListener: true,
		dstAddr:               net.IPv4(192, 168, 100, 1),
		srcAddr:               net.IPv4(192, 168, 100, 1),
		srcPort:               80,
	}

	fci := newFilterChainManagerForTesting(t, lis)
	fc, err := fci.lookup(params)
	if err != nil {
		t.Fatalf("filterChainManager.lookup(%v) failed: %v", params, err)
	}
	if fc == nil {
		t.Fatalf("filterChainManager.lookup(%v) returned nil", params)
	}
}
