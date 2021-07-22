// +build go1.12

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
 *
 */

package xdsclient

import (
	"fmt"
	"strings"
	"testing"
	"time"

	v1typepb "github.com/cncf/udpa/go/udpa/type/v1"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/golang/protobuf/proto"
	spb "github.com/golang/protobuf/ptypes/struct"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/types/known/durationpb"

	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal/httpfilter"
	"google.golang.org/grpc/xds/internal/version"

	v2xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	v2corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v2httppb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	v2listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	anypb "github.com/golang/protobuf/ptypes/any"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
)

func (s) TestUnmarshalListener_ClientSide(t *testing.T) {
	const (
		v2LDSTarget       = "lds.target.good:2222"
		v3LDSTarget       = "lds.target.good:3333"
		v2RouteConfigName = "v2RouteConfig"
		v3RouteConfigName = "v3RouteConfig"
		routeName         = "routeName"
		testVersion       = "test-version-lds-client"
	)

	var (
		v2Lis = testutils.MarshalAny(&v2xdspb.Listener{
			Name: v2LDSTarget,
			ApiListener: &v2listenerpb.ApiListener{
				ApiListener: testutils.MarshalAny(&v2httppb.HttpConnectionManager{
					RouteSpecifier: &v2httppb.HttpConnectionManager_Rds{
						Rds: &v2httppb.Rds{
							ConfigSource: &v2corepb.ConfigSource{
								ConfigSourceSpecifier: &v2corepb.ConfigSource_Ads{Ads: &v2corepb.AggregatedConfigSource{}},
							},
							RouteConfigName: v2RouteConfigName,
						},
					},
				}),
			},
		})
		customFilter = &v3httppb.HttpFilter{
			Name:       "customFilter",
			ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: customFilterConfig},
		}
		typedStructFilter = &v3httppb.HttpFilter{
			Name:       "customFilter",
			ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: wrappedCustomFilterTypedStructConfig},
		}
		customOptionalFilter = &v3httppb.HttpFilter{
			Name:       "customFilter",
			ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: customFilterConfig},
			IsOptional: true,
		}
		customFilter2 = &v3httppb.HttpFilter{
			Name:       "customFilter2",
			ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: customFilterConfig},
		}
		errFilter = &v3httppb.HttpFilter{
			Name:       "errFilter",
			ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: errFilterConfig},
		}
		errOptionalFilter = &v3httppb.HttpFilter{
			Name:       "errFilter",
			ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: errFilterConfig},
			IsOptional: true,
		}
		clientOnlyCustomFilter = &v3httppb.HttpFilter{
			Name:       "clientOnlyCustomFilter",
			ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: clientOnlyCustomFilterConfig},
		}
		serverOnlyCustomFilter = &v3httppb.HttpFilter{
			Name:       "serverOnlyCustomFilter",
			ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: serverOnlyCustomFilterConfig},
		}
		serverOnlyOptionalCustomFilter = &v3httppb.HttpFilter{
			Name:       "serverOnlyOptionalCustomFilter",
			ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: serverOnlyCustomFilterConfig},
			IsOptional: true,
		}
		unknownFilter = &v3httppb.HttpFilter{
			Name:       "unknownFilter",
			ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: unknownFilterConfig},
		}
		unknownOptionalFilter = &v3httppb.HttpFilter{
			Name:       "unknownFilter",
			ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: unknownFilterConfig},
			IsOptional: true,
		}
		v3LisWithInlineRoute = testutils.MarshalAny(&v3listenerpb.Listener{
			Name: v3LDSTarget,
			ApiListener: &v3listenerpb.ApiListener{
				ApiListener: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
					RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
						RouteConfig: &v3routepb.RouteConfiguration{
							Name: routeName,
							VirtualHosts: []*v3routepb.VirtualHost{{
								Domains: []string{v3LDSTarget},
								Routes: []*v3routepb.Route{{
									Match: &v3routepb.RouteMatch{
										PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"},
									},
									Action: &v3routepb.Route_Route{
										Route: &v3routepb.RouteAction{
											ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
										}}}}}}},
					},
					CommonHttpProtocolOptions: &v3corepb.HttpProtocolOptions{
						MaxStreamDuration: durationpb.New(time.Second),
					},
				}),
			},
		})
		v3LisWithFilters = func(fs ...*v3httppb.HttpFilter) *anypb.Any {
			return testutils.MarshalAny(&v3listenerpb.Listener{
				Name: v3LDSTarget,
				ApiListener: &v3listenerpb.ApiListener{
					ApiListener: testutils.MarshalAny(
						&v3httppb.HttpConnectionManager{
							RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
								Rds: &v3httppb.Rds{
									ConfigSource: &v3corepb.ConfigSource{
										ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
									},
									RouteConfigName: v3RouteConfigName,
								},
							},
							CommonHttpProtocolOptions: &v3corepb.HttpProtocolOptions{
								MaxStreamDuration: durationpb.New(time.Second),
							},
							HttpFilters: fs,
						}),
				},
			})
		}
		errMD = UpdateMetadata{
			Status:  ServiceStatusNACKed,
			Version: testVersion,
			ErrState: &UpdateErrorMetadata{
				Version: testVersion,
				Err:     errPlaceHolder,
			},
		}
	)

	tests := []struct {
		name       string
		resources  []*anypb.Any
		wantUpdate map[string]ListenerUpdate
		wantMD     UpdateMetadata
		wantErr    bool
	}{
		{
			name:      "non-listener resource",
			resources: []*anypb.Any{{TypeUrl: version.V3HTTPConnManagerURL}},
			wantMD:    errMD,
			wantErr:   true,
		},
		{
			name: "badly marshaled listener resource",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							ApiListener: &v3listenerpb.ApiListener{
								ApiListener: &anypb.Any{
									TypeUrl: version.V3HTTPConnManagerURL,
									Value:   []byte{1, 2, 3, 4},
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name: "wrong type in apiListener",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name: v3LDSTarget,
				ApiListener: &v3listenerpb.ApiListener{
					ApiListener: testutils.MarshalAny(&v2xdspb.Listener{}),
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name: "empty httpConnMgr in apiListener",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name: v3LDSTarget,
				ApiListener: &v3listenerpb.ApiListener{
					ApiListener: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
						RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
							Rds: &v3httppb.Rds{},
						},
					}),
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name: "scopedRoutes routeConfig in apiListener",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name: v3LDSTarget,
				ApiListener: &v3listenerpb.ApiListener{
					ApiListener: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
						RouteSpecifier: &v3httppb.HttpConnectionManager_ScopedRoutes{},
					}),
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name: "rds.ConfigSource in apiListener is not ADS",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name: v3LDSTarget,
				ApiListener: &v3listenerpb.ApiListener{
					ApiListener: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
						RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
							Rds: &v3httppb.Rds{
								ConfigSource: &v3corepb.ConfigSource{
									ConfigSourceSpecifier: &v3corepb.ConfigSource_Path{
										Path: "/some/path",
									},
								},
								RouteConfigName: v3RouteConfigName,
							},
						},
					}),
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name: "empty resource list",
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v3 with no filters",
			resources: []*anypb.Any{v3LisWithFilters()},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second, Raw: v3LisWithFilters()},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v3 with custom filter",
			resources: []*anypb.Any{v3LisWithFilters(customFilter)},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {
					RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second,
					HTTPFilters: []HTTPFilter{{
						Name:   "customFilter",
						Filter: httpFilter{},
						Config: filterConfig{Cfg: customFilterConfig},
					}},
					Raw: v3LisWithFilters(customFilter),
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v3 with custom filter in typed struct",
			resources: []*anypb.Any{v3LisWithFilters(typedStructFilter)},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {
					RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second,
					HTTPFilters: []HTTPFilter{{
						Name:   "customFilter",
						Filter: httpFilter{},
						Config: filterConfig{Cfg: customFilterTypedStructConfig},
					}},
					Raw: v3LisWithFilters(typedStructFilter),
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v3 with optional custom filter",
			resources: []*anypb.Any{v3LisWithFilters(customOptionalFilter)},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {
					RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second,
					HTTPFilters: []HTTPFilter{{
						Name:   "customFilter",
						Filter: httpFilter{},
						Config: filterConfig{Cfg: customFilterConfig},
					}},
					Raw: v3LisWithFilters(customOptionalFilter),
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:       "v3 with two filters with same name",
			resources:  []*anypb.Any{v3LisWithFilters(customFilter, customFilter)},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name:      "v3 with two filters - same type different name",
			resources: []*anypb.Any{v3LisWithFilters(customFilter, customFilter2)},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {
					RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second,
					HTTPFilters: []HTTPFilter{{
						Name:   "customFilter",
						Filter: httpFilter{},
						Config: filterConfig{Cfg: customFilterConfig},
					}, {
						Name:   "customFilter2",
						Filter: httpFilter{},
						Config: filterConfig{Cfg: customFilterConfig},
					}},
					Raw: v3LisWithFilters(customFilter, customFilter2),
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:       "v3 with server-only filter",
			resources:  []*anypb.Any{v3LisWithFilters(serverOnlyCustomFilter)},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name:      "v3 with optional server-only filter",
			resources: []*anypb.Any{v3LisWithFilters(serverOnlyOptionalCustomFilter)},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {
					RouteConfigName:   v3RouteConfigName,
					MaxStreamDuration: time.Second,
					Raw:               v3LisWithFilters(serverOnlyOptionalCustomFilter),
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v3 with client-only filter",
			resources: []*anypb.Any{v3LisWithFilters(clientOnlyCustomFilter)},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {
					RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second,
					HTTPFilters: []HTTPFilter{{
						Name:   "clientOnlyCustomFilter",
						Filter: clientOnlyHTTPFilter{},
						Config: filterConfig{Cfg: clientOnlyCustomFilterConfig},
					}},
					Raw: v3LisWithFilters(clientOnlyCustomFilter),
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:       "v3 with err filter",
			resources:  []*anypb.Any{v3LisWithFilters(errFilter)},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name:       "v3 with optional err filter",
			resources:  []*anypb.Any{v3LisWithFilters(errOptionalFilter)},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name:       "v3 with unknown filter",
			resources:  []*anypb.Any{v3LisWithFilters(unknownFilter)},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name:      "v3 with unknown filter (optional)",
			resources: []*anypb.Any{v3LisWithFilters(unknownOptionalFilter)},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {
					RouteConfigName:   v3RouteConfigName,
					MaxStreamDuration: time.Second,
					Raw:               v3LisWithFilters(unknownOptionalFilter),
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v2 listener resource",
			resources: []*anypb.Any{v2Lis},
			wantUpdate: map[string]ListenerUpdate{
				v2LDSTarget: {RouteConfigName: v2RouteConfigName, Raw: v2Lis},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v3 listener resource",
			resources: []*anypb.Any{v3LisWithFilters()},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second, Raw: v3LisWithFilters()},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v3 listener with inline route configuration",
			resources: []*anypb.Any{v3LisWithInlineRoute},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {
					InlineRouteConfig: &RouteConfigUpdate{
						VirtualHosts: []*VirtualHost{{
							Domains: []string{v3LDSTarget},
							Routes:  []*Route{{Prefix: newStringP("/"), WeightedClusters: map[string]WeightedCluster{clusterName: {Weight: 1}}}},
						}}},
					MaxStreamDuration: time.Second,
					Raw:               v3LisWithInlineRoute,
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "multiple listener resources",
			resources: []*anypb.Any{v2Lis, v3LisWithFilters()},
			wantUpdate: map[string]ListenerUpdate{
				v2LDSTarget: {RouteConfigName: v2RouteConfigName, Raw: v2Lis},
				v3LDSTarget: {RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second, Raw: v3LisWithFilters()},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			// To test that unmarshal keeps processing on errors.
			name: "good and bad listener resources",
			resources: []*anypb.Any{
				v2Lis,
				testutils.MarshalAny(&v3listenerpb.Listener{
					Name: "bad",
					ApiListener: &v3listenerpb.ApiListener{
						ApiListener: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
							RouteSpecifier: &v3httppb.HttpConnectionManager_ScopedRoutes{},
						}),
					}}),
				v3LisWithFilters(),
			},
			wantUpdate: map[string]ListenerUpdate{
				v2LDSTarget: {RouteConfigName: v2RouteConfigName, Raw: v2Lis},
				v3LDSTarget: {RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second, Raw: v3LisWithFilters()},
				"bad":       {},
			},
			wantMD:  errMD,
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update, md, err := UnmarshalListener(testVersion, test.resources, nil)
			if (err != nil) != test.wantErr {
				t.Fatalf("UnmarshalListener(), got err: %v, wantErr: %v", err, test.wantErr)
			}
			if diff := cmp.Diff(update, test.wantUpdate, cmpOpts); diff != "" {
				t.Errorf("got unexpected update, diff (-got +want): %v", diff)
			}
			if diff := cmp.Diff(md, test.wantMD, cmpOptsIgnoreDetails); diff != "" {
				t.Errorf("got unexpected metadata, diff (-got +want): %v", diff)
			}
		})
	}
}

func (s) TestUnmarshalListener_ServerSide(t *testing.T) {
	const (
		v3LDSTarget = "grpc/server?xds.resource.listening_address=0.0.0.0:9999"
		testVersion = "test-version-lds-server"
	)

	var (
		emptyValidNetworkFilters = []*v3listenerpb.Filter{
			{
				Name: "filter-1",
				ConfigType: &v3listenerpb.Filter_TypedConfig{
					TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{}),
				},
			},
		}
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
		listenerEmptyTransportSocket = testutils.MarshalAny(&v3listenerpb.Listener{
			Name:    v3LDSTarget,
			Address: localSocketAddress,
			FilterChains: []*v3listenerpb.FilterChain{
				{
					Name:    "filter-chain-1",
					Filters: emptyValidNetworkFilters,
				},
			},
		})
		listenerNoValidationContext = testutils.MarshalAny(&v3listenerpb.Listener{
			Name:    v3LDSTarget,
			Address: localSocketAddress,
			FilterChains: []*v3listenerpb.FilterChain{
				{
					Name:    "filter-chain-1",
					Filters: emptyValidNetworkFilters,
					TransportSocket: &v3corepb.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &v3corepb.TransportSocket_TypedConfig{
							TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{
								CommonTlsContext: &v3tlspb.CommonTlsContext{
									TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
										InstanceName:    "identityPluginInstance",
										CertificateName: "identityCertName",
									},
								},
							}),
						},
					},
				},
			},
			DefaultFilterChain: &v3listenerpb.FilterChain{
				Name:    "default-filter-chain-1",
				Filters: emptyValidNetworkFilters,
				TransportSocket: &v3corepb.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{
							CommonTlsContext: &v3tlspb.CommonTlsContext{
								TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
									InstanceName:    "defaultIdentityPluginInstance",
									CertificateName: "defaultIdentityCertName",
								},
							},
						}),
					},
				},
			},
		})
		listenerWithValidationContext = testutils.MarshalAny(&v3listenerpb.Listener{
			Name:    v3LDSTarget,
			Address: localSocketAddress,
			FilterChains: []*v3listenerpb.FilterChain{
				{
					Name:    "filter-chain-1",
					Filters: emptyValidNetworkFilters,
					TransportSocket: &v3corepb.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &v3corepb.TransportSocket_TypedConfig{
							TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{
								RequireClientCertificate: &wrapperspb.BoolValue{Value: true},
								CommonTlsContext: &v3tlspb.CommonTlsContext{
									TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
										InstanceName:    "identityPluginInstance",
										CertificateName: "identityCertName",
									},
									ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance{
										ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
											InstanceName:    "rootPluginInstance",
											CertificateName: "rootCertName",
										},
									},
								},
							}),
						},
					},
				},
			},
			DefaultFilterChain: &v3listenerpb.FilterChain{
				Name:    "default-filter-chain-1",
				Filters: emptyValidNetworkFilters,
				TransportSocket: &v3corepb.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{
							RequireClientCertificate: &wrapperspb.BoolValue{Value: true},
							CommonTlsContext: &v3tlspb.CommonTlsContext{
								TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
									InstanceName:    "defaultIdentityPluginInstance",
									CertificateName: "defaultIdentityCertName",
								},
								ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance{
									ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
										InstanceName:    "defaultRootPluginInstance",
										CertificateName: "defaultRootCertName",
									},
								},
							},
						}),
					},
				},
			},
		})
		errMD = UpdateMetadata{
			Status:  ServiceStatusNACKed,
			Version: testVersion,
			ErrState: &UpdateErrorMetadata{
				Version: testVersion,
				Err:     errPlaceHolder,
			},
		}
	)

	tests := []struct {
		name       string
		resources  []*anypb.Any
		wantUpdate map[string]ListenerUpdate
		wantMD     UpdateMetadata
		wantErr    string
	}{
		{
			name: "non-empty listener filters",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name: v3LDSTarget,
				ListenerFilters: []*v3listenerpb.ListenerFilter{
					{Name: "listener-filter-1"},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "unsupported field 'listener_filters'",
		},
		{
			name: "use_original_dst is set",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:           v3LDSTarget,
				UseOriginalDst: &wrapperspb.BoolValue{Value: true},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "unsupported field 'use_original_dst'",
		},
		{
			name:       "no address field",
			resources:  []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{Name: v3LDSTarget})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "no address field in LDS response",
		},
		{
			name: "no socket address field",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: &v3corepb.Address{},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "no socket_address field in LDS response",
		},
		{
			name: "no filter chains and no default filter chain",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{DestinationPort: &wrapperspb.UInt32Value{Value: 666}},
						Filters:          emptyValidNetworkFilters,
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "no supported filter chains and no default filter chain",
		},
		{
			name: "missing http connection manager network filter",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "missing HttpConnectionManager filter",
		},
		{
			name: "missing filter name in http filter",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{}),
								},
							},
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "missing name field in filter",
		},
		{
			name: "duplicate filter names in http filter",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								Name: "name",
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{}),
								},
							},
							{
								Name: "name",
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{}),
								},
							},
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "duplicate filter name",
		},
		{
			name: "unsupported oneof in typed config of http filter",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								Name:       "name",
								ConfigType: &v3listenerpb.Filter_ConfigDiscovery{},
							},
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "unsupported config_type",
		},
		{
			name: "overlapping filter chain match criteria",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePorts: []uint32{1, 2, 3, 4, 5}},
						Filters:          emptyValidNetworkFilters,
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{},
						Filters:          emptyValidNetworkFilters,
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePorts: []uint32{5, 6, 7}},
						Filters:          emptyValidNetworkFilters,
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "multiple filter chains with overlapping matching rules are defined",
		},
		{
			name: "unsupported network filter",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								Name: "name",
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: testutils.MarshalAny(&v3httppb.LocalReplyConfig{}),
								},
							},
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "unsupported network filter",
		},
		{
			name: "badly marshaled network filter",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								Name: "name",
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: &anypb.Any{
										TypeUrl: version.V3HTTPConnManagerURL,
										Value:   []byte{1, 2, 3, 4},
									},
								},
							},
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "failed unmarshaling of network filter",
		},
		{
			name: "unexpected transport socket name",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name:    "filter-chain-1",
						Filters: emptyValidNetworkFilters,
						TransportSocket: &v3corepb.TransportSocket{
							Name: "unsupported-transport-socket-name",
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "transport_socket field has unexpected name",
		},
		{
			name: "unexpected transport socket typedConfig URL",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name:    "filter-chain-1",
						Filters: emptyValidNetworkFilters,
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{}),
							},
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "transport_socket field has unexpected typeURL",
		},
		{
			name: "badly marshaled transport socket",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name:    "filter-chain-1",
						Filters: emptyValidNetworkFilters,
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: &anypb.Any{
									TypeUrl: version.V3DownstreamTLSContextURL,
									Value:   []byte{1, 2, 3, 4},
								},
							},
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "failed to unmarshal DownstreamTlsContext in LDS response",
		},
		{
			name: "missing CommonTlsContext",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name:    "filter-chain-1",
						Filters: emptyValidNetworkFilters,
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{}),
							},
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "DownstreamTlsContext in LDS response does not contain a CommonTlsContext",
		},
		{
			name: "unsupported validation context in transport socket",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name:    "filter-chain-1",
						Filters: emptyValidNetworkFilters,
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{
									CommonTlsContext: &v3tlspb.CommonTlsContext{
										ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextSdsSecretConfig{
											ValidationContextSdsSecretConfig: &v3tlspb.SdsSecretConfig{
												Name: "foo-sds-secret",
											},
										},
									},
								}),
							},
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "validation context contains unexpected type",
		},
		{
			name:      "empty transport socket",
			resources: []*anypb.Any{listenerEmptyTransportSocket},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {
					InboundListenerCfg: &InboundListenerConfig{
						Address: "0.0.0.0",
						Port:    "9999",
						FilterChains: &FilterChainManager{
							dstPrefixMap: map[string]*destPrefixEntry{
								unspecifiedPrefixMapKey: {
									srcTypeArr: [3]*sourcePrefixes{
										{
											srcPrefixMap: map[string]*sourcePrefixEntry{
												unspecifiedPrefixMapKey: {
													srcPortMap: map[int]*FilterChain{
														0: {},
													},
												},
											},
										},
									},
								},
							},
						},
					},
					Raw: listenerEmptyTransportSocket,
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name: "no identity and root certificate providers",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name:    "filter-chain-1",
						Filters: emptyValidNetworkFilters,
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{
									RequireClientCertificate: &wrapperspb.BoolValue{Value: true},
									CommonTlsContext: &v3tlspb.CommonTlsContext{
										TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
											InstanceName:    "identityPluginInstance",
											CertificateName: "identityCertName",
										},
									},
								}),
							},
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "security configuration on the server-side does not contain root certificate provider instance name, but require_client_cert field is set",
		},
		{
			name: "no identity certificate provider with require_client_cert",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name:    "filter-chain-1",
						Filters: emptyValidNetworkFilters,
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{
									CommonTlsContext: &v3tlspb.CommonTlsContext{},
								}),
							},
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD:     errMD,
			wantErr:    "security configuration on the server-side does not contain identity certificate provider instance name",
		},
		{
			name:      "happy case with no validation context",
			resources: []*anypb.Any{listenerNoValidationContext},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {
					InboundListenerCfg: &InboundListenerConfig{
						Address: "0.0.0.0",
						Port:    "9999",
						FilterChains: &FilterChainManager{
							dstPrefixMap: map[string]*destPrefixEntry{
								unspecifiedPrefixMapKey: {
									srcTypeArr: [3]*sourcePrefixes{
										{
											srcPrefixMap: map[string]*sourcePrefixEntry{
												unspecifiedPrefixMapKey: {
													srcPortMap: map[int]*FilterChain{
														0: {
															SecurityCfg: &SecurityConfig{
																IdentityInstanceName: "identityPluginInstance",
																IdentityCertName:     "identityCertName",
															},
														},
													},
												},
											},
										},
									},
								},
							},
							def: &FilterChain{
								SecurityCfg: &SecurityConfig{
									IdentityInstanceName: "defaultIdentityPluginInstance",
									IdentityCertName:     "defaultIdentityCertName",
								},
							},
						},
					},
					Raw: listenerNoValidationContext,
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "happy case with validation context provider instance",
			resources: []*anypb.Any{listenerWithValidationContext},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {
					InboundListenerCfg: &InboundListenerConfig{
						Address: "0.0.0.0",
						Port:    "9999",
						FilterChains: &FilterChainManager{
							dstPrefixMap: map[string]*destPrefixEntry{
								unspecifiedPrefixMapKey: {
									srcTypeArr: [3]*sourcePrefixes{
										{
											srcPrefixMap: map[string]*sourcePrefixEntry{
												unspecifiedPrefixMapKey: {
													srcPortMap: map[int]*FilterChain{
														0: {
															SecurityCfg: &SecurityConfig{
																RootInstanceName:     "rootPluginInstance",
																RootCertName:         "rootCertName",
																IdentityInstanceName: "identityPluginInstance",
																IdentityCertName:     "identityCertName",
																RequireClientCert:    true,
															},
														},
													},
												},
											},
										},
									},
								},
							},
							def: &FilterChain{
								SecurityCfg: &SecurityConfig{
									RootInstanceName:     "defaultRootPluginInstance",
									RootCertName:         "defaultRootCertName",
									IdentityInstanceName: "defaultIdentityPluginInstance",
									IdentityCertName:     "defaultIdentityCertName",
									RequireClientCert:    true,
								},
							},
						},
					},
					Raw: listenerWithValidationContext,
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotUpdate, md, err := UnmarshalListener(testVersion, test.resources, nil)
			if (err != nil) != (test.wantErr != "") {
				t.Fatalf("UnmarshalListener(), got err: %v, wantErr: %v", err, test.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("UnmarshalListener() = %v wantErr: %q", err, test.wantErr)
			}
			if diff := cmp.Diff(gotUpdate, test.wantUpdate, cmpOpts); diff != "" {
				t.Errorf("got unexpected update, diff (-got +want): %v", diff)
			}
			if diff := cmp.Diff(md, test.wantMD, cmpOptsIgnoreDetails); diff != "" {
				t.Errorf("got unexpected metadata, diff (-got +want): %v", diff)
			}
		})
	}
}

type filterConfig struct {
	httpfilter.FilterConfig
	Cfg      proto.Message
	Override proto.Message
}

// httpFilter allows testing the http filter registry and parsing functionality.
type httpFilter struct {
	httpfilter.ClientInterceptorBuilder
	httpfilter.ServerInterceptorBuilder
}

func (httpFilter) TypeURLs() []string { return []string{"custom.filter"} }

func (httpFilter) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	return filterConfig{Cfg: cfg}, nil
}

func (httpFilter) ParseFilterConfigOverride(override proto.Message) (httpfilter.FilterConfig, error) {
	return filterConfig{Override: override}, nil
}

// errHTTPFilter returns errors no matter what is passed to ParseFilterConfig.
type errHTTPFilter struct {
	httpfilter.ClientInterceptorBuilder
}

func (errHTTPFilter) TypeURLs() []string { return []string{"err.custom.filter"} }

func (errHTTPFilter) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	return nil, fmt.Errorf("error from ParseFilterConfig")
}

func (errHTTPFilter) ParseFilterConfigOverride(override proto.Message) (httpfilter.FilterConfig, error) {
	return nil, fmt.Errorf("error from ParseFilterConfigOverride")
}

func init() {
	httpfilter.Register(httpFilter{})
	httpfilter.Register(errHTTPFilter{})
	httpfilter.Register(serverOnlyHTTPFilter{})
	httpfilter.Register(clientOnlyHTTPFilter{})
}

// serverOnlyHTTPFilter does not implement ClientInterceptorBuilder
type serverOnlyHTTPFilter struct {
	httpfilter.ServerInterceptorBuilder
}

func (serverOnlyHTTPFilter) TypeURLs() []string { return []string{"serverOnly.custom.filter"} }

func (serverOnlyHTTPFilter) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	return filterConfig{Cfg: cfg}, nil
}

func (serverOnlyHTTPFilter) ParseFilterConfigOverride(override proto.Message) (httpfilter.FilterConfig, error) {
	return filterConfig{Override: override}, nil
}

// clientOnlyHTTPFilter does not implement ServerInterceptorBuilder
type clientOnlyHTTPFilter struct {
	httpfilter.ClientInterceptorBuilder
}

func (clientOnlyHTTPFilter) TypeURLs() []string { return []string{"clientOnly.custom.filter"} }

func (clientOnlyHTTPFilter) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	return filterConfig{Cfg: cfg}, nil
}

func (clientOnlyHTTPFilter) ParseFilterConfigOverride(override proto.Message) (httpfilter.FilterConfig, error) {
	return filterConfig{Override: override}, nil
}

var customFilterConfig = &anypb.Any{
	TypeUrl: "custom.filter",
	Value:   []byte{1, 2, 3},
}

var errFilterConfig = &anypb.Any{
	TypeUrl: "err.custom.filter",
	Value:   []byte{1, 2, 3},
}

var serverOnlyCustomFilterConfig = &anypb.Any{
	TypeUrl: "serverOnly.custom.filter",
	Value:   []byte{1, 2, 3},
}

var clientOnlyCustomFilterConfig = &anypb.Any{
	TypeUrl: "clientOnly.custom.filter",
	Value:   []byte{1, 2, 3},
}

var customFilterTypedStructConfig = &v1typepb.TypedStruct{
	TypeUrl: "custom.filter",
	Value: &spb.Struct{
		Fields: map[string]*spb.Value{
			"foo": {Kind: &spb.Value_StringValue{StringValue: "bar"}},
		},
	},
}
var wrappedCustomFilterTypedStructConfig *anypb.Any

func init() {
	wrappedCustomFilterTypedStructConfig = testutils.MarshalAny(customFilterTypedStructConfig)
}

var unknownFilterConfig = &anypb.Any{
	TypeUrl: "unknown.custom.filter",
	Value:   []byte{1, 2, 3},
}

func wrappedOptionalFilter(name string) *anypb.Any {
	return testutils.MarshalAny(&v3routepb.FilterConfig{
		IsOptional: true,
		Config: &anypb.Any{
			TypeUrl: name,
			Value:   []byte{1, 2, 3},
		},
	})
}
