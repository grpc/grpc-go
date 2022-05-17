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
 */

package xdsresource

import (
	"fmt"
	"strings"
	"testing"
	"time"

	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/internal/httpfilter"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/types/known/durationpb"

	v1udpatypepb "github.com/cncf/udpa/go/udpa/type/v1"
	v3cncftypepb "github.com/cncf/xds/go/xds/type/v3"
	v2xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	v2corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v2httppb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	v2listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	rpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	anypb "github.com/golang/protobuf/ptypes/any"
	spb "github.com/golang/protobuf/ptypes/struct"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"

	_ "google.golang.org/grpc/xds/internal/httpfilter/rbac"   // Register the RBAC HTTP filter.
	_ "google.golang.org/grpc/xds/internal/httpfilter/router" // Register the router filter.
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
		oldTypedStructFilter = &v3httppb.HttpFilter{
			Name:       "customFilter",
			ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: wrappedCustomFilterOldTypedStructConfig},
		}
		newTypedStructFilter = &v3httppb.HttpFilter{
			Name:       "customFilter",
			ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: wrappedCustomFilterNewTypedStructConfig},
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
					HttpFilters: []*v3httppb.HttpFilter{emptyRouterFilter},
					CommonHttpProtocolOptions: &v3corepb.HttpProtocolOptions{
						MaxStreamDuration: durationpb.New(time.Second),
					},
				}),
			},
		})
		v3LisWithFilters = func(fs ...*v3httppb.HttpFilter) *anypb.Any {
			fs = append(fs, emptyRouterFilter)
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
		v3LisToTestRBAC = func(xffNumTrustedHops uint32, originalIpDetectionExtensions []*v3corepb.TypedExtensionConfig) *anypb.Any {
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
							HttpFilters:                   []*v3httppb.HttpFilter{emptyRouterFilter},
							XffNumTrustedHops:             xffNumTrustedHops,
							OriginalIpDetectionExtensions: originalIpDetectionExtensions,
						}),
				},
			})
		}

		v3ListenerWithCDSConfigSourceSelf = testutils.MarshalAny(&v3listenerpb.Listener{
			Name: v3LDSTarget,
			ApiListener: &v3listenerpb.ApiListener{
				ApiListener: testutils.MarshalAny(
					&v3httppb.HttpConnectionManager{
						RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
							Rds: &v3httppb.Rds{
								ConfigSource: &v3corepb.ConfigSource{
									ConfigSourceSpecifier: &v3corepb.ConfigSource_Self{},
								},
								RouteConfigName: v3RouteConfigName,
							},
						},
						HttpFilters: []*v3httppb.HttpFilter{emptyRouterFilter},
					}),
			},
		})

		errMD = UpdateMetadata{
			Status:  ServiceStatusNACKed,
			Version: testVersion,
			ErrState: &UpdateErrorMetadata{
				Version: testVersion,
				Err:     cmpopts.AnyError,
			},
		}
	)

	tests := []struct {
		name       string
		resources  []*anypb.Any
		wantUpdate map[string]ListenerUpdateErrTuple
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name:      "rds.ConfigSource in apiListener is Self",
			resources: []*anypb.Any{v3ListenerWithCDSConfigSourceSelf},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
					RouteConfigName: v3RouteConfigName,
					HTTPFilters:     []HTTPFilter{routerFilter},
					Raw:             v3ListenerWithCDSConfigSourceSelf,
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name: "rds.ConfigSource in apiListener is not ADS or Self",
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second, HTTPFilters: routerFilterList, Raw: v3LisWithFilters()}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name: "v3 no terminal filter",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
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
						}),
				},
			})},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name:      "v3 with custom filter",
			resources: []*anypb.Any{v3LisWithFilters(customFilter)},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
					RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second,
					HTTPFilters: []HTTPFilter{
						{
							Name:   "customFilter",
							Filter: httpFilter{},
							Config: filterConfig{Cfg: customFilterConfig},
						},
						routerFilter,
					},
					Raw: v3LisWithFilters(customFilter),
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v3 with custom filter in old typed struct",
			resources: []*anypb.Any{v3LisWithFilters(oldTypedStructFilter)},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
					RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second,
					HTTPFilters: []HTTPFilter{
						{
							Name:   "customFilter",
							Filter: httpFilter{},
							Config: filterConfig{Cfg: customFilterOldTypedStructConfig},
						},
						routerFilter,
					},
					Raw: v3LisWithFilters(oldTypedStructFilter),
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v3 with custom filter in new typed struct",
			resources: []*anypb.Any{v3LisWithFilters(newTypedStructFilter)},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
					RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second,
					HTTPFilters: []HTTPFilter{
						{
							Name:   "customFilter",
							Filter: httpFilter{},
							Config: filterConfig{Cfg: customFilterNewTypedStructConfig},
						},
						routerFilter,
					},
					Raw: v3LisWithFilters(newTypedStructFilter),
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v3 with optional custom filter",
			resources: []*anypb.Any{v3LisWithFilters(customOptionalFilter)},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
					RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second,
					HTTPFilters: []HTTPFilter{
						{
							Name:   "customFilter",
							Filter: httpFilter{},
							Config: filterConfig{Cfg: customFilterConfig},
						},
						routerFilter,
					},
					Raw: v3LisWithFilters(customOptionalFilter),
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:       "v3 with two filters with same name",
			resources:  []*anypb.Any{v3LisWithFilters(customFilter, customFilter)},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name:      "v3 with two filters - same type different name",
			resources: []*anypb.Any{v3LisWithFilters(customFilter, customFilter2)},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
					RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second,
					HTTPFilters: []HTTPFilter{{
						Name:   "customFilter",
						Filter: httpFilter{},
						Config: filterConfig{Cfg: customFilterConfig},
					}, {
						Name:   "customFilter2",
						Filter: httpFilter{},
						Config: filterConfig{Cfg: customFilterConfig},
					},
						routerFilter,
					},
					Raw: v3LisWithFilters(customFilter, customFilter2),
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:       "v3 with server-only filter",
			resources:  []*anypb.Any{v3LisWithFilters(serverOnlyCustomFilter)},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name:      "v3 with optional server-only filter",
			resources: []*anypb.Any{v3LisWithFilters(serverOnlyOptionalCustomFilter)},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
					RouteConfigName:   v3RouteConfigName,
					MaxStreamDuration: time.Second,
					Raw:               v3LisWithFilters(serverOnlyOptionalCustomFilter),
					HTTPFilters:       routerFilterList,
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v3 with client-only filter",
			resources: []*anypb.Any{v3LisWithFilters(clientOnlyCustomFilter)},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
					RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second,
					HTTPFilters: []HTTPFilter{
						{
							Name:   "clientOnlyCustomFilter",
							Filter: clientOnlyHTTPFilter{},
							Config: filterConfig{Cfg: clientOnlyCustomFilterConfig},
						},
						routerFilter},
					Raw: v3LisWithFilters(clientOnlyCustomFilter),
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:       "v3 with err filter",
			resources:  []*anypb.Any{v3LisWithFilters(errFilter)},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name:       "v3 with optional err filter",
			resources:  []*anypb.Any{v3LisWithFilters(errOptionalFilter)},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name:       "v3 with unknown filter",
			resources:  []*anypb.Any{v3LisWithFilters(unknownFilter)},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name:      "v3 with unknown filter (optional)",
			resources: []*anypb.Any{v3LisWithFilters(unknownOptionalFilter)},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
					RouteConfigName:   v3RouteConfigName,
					MaxStreamDuration: time.Second,
					HTTPFilters:       routerFilterList,
					Raw:               v3LisWithFilters(unknownOptionalFilter),
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v2 listener resource",
			resources: []*anypb.Any{v2Lis},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v2LDSTarget: {Update: ListenerUpdate{RouteConfigName: v2RouteConfigName, Raw: v2Lis}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v2 listener resource wrapped",
			resources: []*anypb.Any{testutils.MarshalAny(&v2xdspb.Resource{Resource: v2Lis})},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v2LDSTarget: {Update: ListenerUpdate{RouteConfigName: v2RouteConfigName, Raw: v2Lis}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v3 listener resource",
			resources: []*anypb.Any{v3LisWithFilters()},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second, HTTPFilters: routerFilterList, Raw: v3LisWithFilters()}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v3 listener resource wrapped",
			resources: []*anypb.Any{testutils.MarshalAny(&v3discoverypb.Resource{Resource: v3LisWithFilters()})},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second, HTTPFilters: routerFilterList, Raw: v3LisWithFilters()}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		// "To allow equating RBAC's direct_remote_ip and
		// remote_ip...HttpConnectionManager.xff_num_trusted_hops must be unset
		// or zero and HttpConnectionManager.original_ip_detection_extensions
		// must be empty." - A41
		{
			name:      "rbac-allow-equating-direct-remote-ip-and-remote-ip-valid",
			resources: []*anypb.Any{v3LisToTestRBAC(0, nil)},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
					RouteConfigName:   v3RouteConfigName,
					MaxStreamDuration: time.Second,
					HTTPFilters:       []HTTPFilter{routerFilter},
					Raw:               v3LisToTestRBAC(0, nil),
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		// In order to support xDS Configured RBAC HTTPFilter equating direct
		// remote ip and remote ip, xffNumTrustedHops cannot be greater than
		// zero. This is because if you can trust a ingress proxy hop when
		// determining an origin clients ip address, direct remote ip != remote
		// ip.
		{
			name:       "rbac-allow-equating-direct-remote-ip-and-remote-ip-invalid-num-untrusted-hops",
			resources:  []*anypb.Any{v3LisToTestRBAC(1, nil)},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    true,
		},
		// In order to support xDS Configured RBAC HTTPFilter equating direct
		// remote ip and remote ip, originalIpDetectionExtensions must be empty.
		// This is because if you have to ask ip-detection-extension for the
		// original ip, direct remote ip might not equal remote ip.
		{
			name:       "rbac-allow-equating-direct-remote-ip-and-remote-ip-invalid-original-ip-detection-extension",
			resources:  []*anypb.Any{v3LisToTestRBAC(0, []*v3corepb.TypedExtensionConfig{{Name: "something"}})},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    true,
		},
		{
			name:      "v3 listener with inline route configuration",
			resources: []*anypb.Any{v3LisWithInlineRoute},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
					InlineRouteConfig: &RouteConfigUpdate{
						VirtualHosts: []*VirtualHost{{
							Domains: []string{v3LDSTarget},
							Routes:  []*Route{{Prefix: newStringP("/"), WeightedClusters: map[string]WeightedCluster{clusterName: {Weight: 1}}, ActionType: RouteActionRoute}},
						}}},
					MaxStreamDuration: time.Second,
					Raw:               v3LisWithInlineRoute,
					HTTPFilters:       routerFilterList,
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "multiple listener resources",
			resources: []*anypb.Any{v2Lis, v3LisWithFilters()},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v2LDSTarget: {Update: ListenerUpdate{RouteConfigName: v2RouteConfigName, Raw: v2Lis}},
				v3LDSTarget: {Update: ListenerUpdate{RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second, Raw: v3LisWithFilters(), HTTPFilters: routerFilterList}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v2LDSTarget: {Update: ListenerUpdate{RouteConfigName: v2RouteConfigName, Raw: v2Lis}},
				v3LDSTarget: {Update: ListenerUpdate{RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second, Raw: v3LisWithFilters(), HTTPFilters: routerFilterList}},
				"bad":       {Err: cmpopts.AnyError},
			},
			wantMD:  errMD,
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := &UnmarshalOptions{
				Version:   testVersion,
				Resources: test.resources,
			}
			update, md, err := UnmarshalListener(opts)
			if (err != nil) != test.wantErr {
				t.Fatalf("UnmarshalListener(%+v), got err: %v, wantErr: %v", opts, err, test.wantErr)
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
	oldRBAC := envconfig.XDSRBAC
	envconfig.XDSRBAC = true
	defer func() {
		envconfig.XDSRBAC = oldRBAC
	}()
	const (
		v3LDSTarget = "grpc/server?xds.resource.listening_address=0.0.0.0:9999"
		testVersion = "test-version-lds-server"
	)

	var (
		serverOnlyCustomFilter = &v3httppb.HttpFilter{
			Name:       "serverOnlyCustomFilter",
			ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: serverOnlyCustomFilterConfig},
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
		emptyValidNetworkFilters = []*v3listenerpb.Filter{
			{
				Name: "filter-1",
				ConfigType: &v3listenerpb.Filter_TypedConfig{
					TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
						RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
							RouteConfig: routeConfig,
						},
						HttpFilters: []*v3httppb.HttpFilter{e2e.RouterHTTPFilter},
					}),
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
		listenerNoValidationContextDeprecatedFields = testutils.MarshalAny(&v3listenerpb.Listener{
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
		listenerNoValidationContextNewFields = testutils.MarshalAny(&v3listenerpb.Listener{
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
									TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
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
								TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
									InstanceName:    "defaultIdentityPluginInstance",
									CertificateName: "defaultIdentityCertName",
								},
							},
						}),
					},
				},
			},
		})
		listenerWithValidationContextDeprecatedFields = testutils.MarshalAny(&v3listenerpb.Listener{
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
		listenerWithValidationContextNewFields = testutils.MarshalAny(&v3listenerpb.Listener{
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
									TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
										InstanceName:    "identityPluginInstance",
										CertificateName: "identityCertName",
									},
									ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContext{
										ValidationContext: &v3tlspb.CertificateValidationContext{
											CaCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
												InstanceName:    "rootPluginInstance",
												CertificateName: "rootCertName",
											},
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
								TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
									InstanceName:    "defaultIdentityPluginInstance",
									CertificateName: "defaultIdentityCertName",
								},
								ValidationContextType: &v3tlspb.CommonTlsContext_CombinedValidationContext{
									CombinedValidationContext: &v3tlspb.CommonTlsContext_CombinedCertificateValidationContext{
										DefaultValidationContext: &v3tlspb.CertificateValidationContext{
											CaCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
												InstanceName:    "defaultRootPluginInstance",
												CertificateName: "defaultRootCertName",
											},
										},
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
				Err:     cmpopts.AnyError,
			},
		}
	)
	v3LisToTestRBAC := func(xffNumTrustedHops uint32, originalIpDetectionExtensions []*v3corepb.TypedExtensionConfig) *anypb.Any {
		return testutils.MarshalAny(&v3listenerpb.Listener{
			Name:    v3LDSTarget,
			Address: localSocketAddress,
			FilterChains: []*v3listenerpb.FilterChain{
				{
					Name: "filter-chain-1",
					Filters: []*v3listenerpb.Filter{
						{
							Name: "filter-1",
							ConfigType: &v3listenerpb.Filter_TypedConfig{
								TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
									RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
										RouteConfig: routeConfig,
									},
									HttpFilters:                   []*v3httppb.HttpFilter{e2e.RouterHTTPFilter},
									XffNumTrustedHops:             xffNumTrustedHops,
									OriginalIpDetectionExtensions: originalIpDetectionExtensions,
								}),
							},
						},
					},
				},
			},
		})
	}
	v3LisWithBadRBACConfiguration := func(rbacCfg *v3rbacpb.RBAC) *anypb.Any {
		return testutils.MarshalAny(&v3listenerpb.Listener{
			Name:    v3LDSTarget,
			Address: localSocketAddress,
			FilterChains: []*v3listenerpb.FilterChain{
				{
					Name: "filter-chain-1",
					Filters: []*v3listenerpb.Filter{
						{
							Name: "filter-1",
							ConfigType: &v3listenerpb.Filter_TypedConfig{
								TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
									RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
										RouteConfig: routeConfig,
									},
									HttpFilters: []*v3httppb.HttpFilter{e2e.HTTPFilter("rbac", rbacCfg), e2e.RouterHTTPFilter},
								}),
							},
						},
					},
				},
			},
		})
	}
	badRBACCfgRegex := &v3rbacpb.RBAC{
		Rules: &rpb.RBAC{
			Action: rpb.RBAC_ALLOW,
			Policies: map[string]*rpb.Policy{
				"bad-regex-value": {
					Permissions: []*rpb.Permission{
						{Rule: &rpb.Permission_Any{Any: true}},
					},
					Principals: []*rpb.Principal{
						{Identifier: &rpb.Principal_Header{Header: &v3routepb.HeaderMatcher{Name: ":method", HeaderMatchSpecifier: &v3routepb.HeaderMatcher_SafeRegexMatch{SafeRegexMatch: &v3matcherpb.RegexMatcher{Regex: "["}}}}},
					},
				},
			},
		},
	}
	badRBACCfgDestIP := &v3rbacpb.RBAC{
		Rules: &rpb.RBAC{
			Action: rpb.RBAC_ALLOW,
			Policies: map[string]*rpb.Policy{
				"certain-destination-ip": {
					Permissions: []*rpb.Permission{
						{Rule: &rpb.Permission_DestinationIp{DestinationIp: &v3corepb.CidrRange{AddressPrefix: "not a correct address", PrefixLen: &wrapperspb.UInt32Value{Value: uint32(10)}}}},
					},
					Principals: []*rpb.Principal{
						{Identifier: &rpb.Principal_Any{Any: true}},
					},
				},
			},
		},
	}

	tests := []struct {
		name       string
		resources  []*anypb.Any
		wantUpdate map[string]ListenerUpdateErrTuple
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    "unsupported field 'listener_filters'",
		},
		{
			name: "use_original_dst is set",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:           v3LDSTarget,
				UseOriginalDst: &wrapperspb.BoolValue{Value: true},
			})},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    "unsupported field 'use_original_dst'",
		},
		{
			name:       "no address field",
			resources:  []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{Name: v3LDSTarget})},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    "no address field in LDS response",
		},
		{
			name: "no socket address field",
			resources: []*anypb.Any{testutils.MarshalAny(&v3listenerpb.Listener{
				Name:    v3LDSTarget,
				Address: &v3corepb.Address{},
			})},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
									TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
										RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
											RouteConfig: routeConfig,
										},
										HttpFilters: []*v3httppb.HttpFilter{emptyRouterFilter},
									}),
								},
							},
							{
								Name: "name",
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
										RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
											RouteConfig: routeConfig,
										},
										HttpFilters: []*v3httppb.HttpFilter{emptyRouterFilter},
									}),
								},
							},
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    "duplicate filter name",
		},
		{
			name: "no terminal filter",
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
									TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
										RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
											RouteConfig: routeConfig,
										},
									}),
								},
							},
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    "http filters list is empty",
		},
		{
			name: "terminal filter not last",
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
									TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
										RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
											RouteConfig: routeConfig,
										},
										HttpFilters: []*v3httppb.HttpFilter{emptyRouterFilter, serverOnlyCustomFilter},
									}),
								},
							},
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    "is a terminal filter but it is not last in the filter chain",
		},
		{
			name: "last not terminal filter",
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
									TypedConfig: testutils.MarshalAny(&v3httppb.HttpConnectionManager{
										RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
											RouteConfig: routeConfig,
										},
										HttpFilters: []*v3httppb.HttpFilter{serverOnlyCustomFilter},
									}),
								},
							},
						},
					},
				},
			})},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    "is not a terminal filter",
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    "DownstreamTlsContext in LDS response does not contain a CommonTlsContext",
		},
		{
			name:      "rbac-allow-equating-direct-remote-ip-and-remote-ip-valid",
			resources: []*anypb.Any{v3LisToTestRBAC(0, nil)},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
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
															InlineRouteConfig: inlineRouteConfig,
															HTTPFilters:       routerFilterList,
														},
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
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:       "rbac-allow-equating-direct-remote-ip-and-remote-ip-invalid-num-untrusted-hops",
			resources:  []*anypb.Any{v3LisToTestRBAC(1, nil)},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    "xff_num_trusted_hops must be unset or zero",
		},
		{
			name:       "rbac-allow-equating-direct-remote-ip-and-remote-ip-invalid-original-ip-detection-extension",
			resources:  []*anypb.Any{v3LisToTestRBAC(0, []*v3corepb.TypedExtensionConfig{{Name: "something"}})},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    "original_ip_detection_extensions must be empty",
		},
		{
			name:       "rbac-with-invalid-regex",
			resources:  []*anypb.Any{v3LisWithBadRBACConfiguration(badRBACCfgRegex)},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    "error parsing config for filter",
		},
		{
			name:       "rbac-with-invalid-destination-ip-matcher",
			resources:  []*anypb.Any{v3LisWithBadRBACConfiguration(badRBACCfgDestIP)},
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    "error parsing config for filter",
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    "validation context contains unexpected type",
		},
		{
			name:      "empty transport socket",
			resources: []*anypb.Any{listenerEmptyTransportSocket},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
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
															InlineRouteConfig: inlineRouteConfig,
															HTTPFilters:       routerFilterList,
														},
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
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name: "no identity and root certificate providers using deprecated fields",
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    "security configuration on the server-side does not contain root certificate provider instance name, but require_client_cert field is set",
		},
		{
			name: "no identity and root certificate providers using new fields",
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
										TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
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
			wantUpdate: map[string]ListenerUpdateErrTuple{v3LDSTarget: {Err: cmpopts.AnyError}},
			wantMD:     errMD,
			wantErr:    "security configuration on the server-side does not contain identity certificate provider instance name",
		},
		{
			name:      "happy case with no validation context using deprecated fields",
			resources: []*anypb.Any{listenerNoValidationContextDeprecatedFields},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
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
															InlineRouteConfig: inlineRouteConfig,
															HTTPFilters:       routerFilterList,
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
								InlineRouteConfig: inlineRouteConfig,
								HTTPFilters:       routerFilterList,
							},
						},
					},
					Raw: listenerNoValidationContextDeprecatedFields,
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "happy case with no validation context using new fields",
			resources: []*anypb.Any{listenerNoValidationContextNewFields},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
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
															InlineRouteConfig: inlineRouteConfig,
															HTTPFilters:       routerFilterList,
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
								InlineRouteConfig: inlineRouteConfig,
								HTTPFilters:       routerFilterList,
							},
						},
					},
					Raw: listenerNoValidationContextNewFields,
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "happy case with validation context provider instance with deprecated fields",
			resources: []*anypb.Any{listenerWithValidationContextDeprecatedFields},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
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
															InlineRouteConfig: inlineRouteConfig,
															HTTPFilters:       routerFilterList,
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
								InlineRouteConfig: inlineRouteConfig,
								HTTPFilters:       routerFilterList,
							},
						},
					},
					Raw: listenerWithValidationContextDeprecatedFields,
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "happy case with validation context provider instance with new fields",
			resources: []*anypb.Any{listenerWithValidationContextNewFields},
			wantUpdate: map[string]ListenerUpdateErrTuple{
				v3LDSTarget: {Update: ListenerUpdate{
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
															InlineRouteConfig: inlineRouteConfig,
															HTTPFilters:       routerFilterList,
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
								InlineRouteConfig: inlineRouteConfig,
								HTTPFilters:       routerFilterList,
							},
						},
					},
					Raw: listenerWithValidationContextNewFields,
				}},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := &UnmarshalOptions{
				Version:   testVersion,
				Resources: test.resources,
			}
			gotUpdate, md, err := UnmarshalListener(opts)
			if (err != nil) != (test.wantErr != "") {
				t.Fatalf("UnmarshalListener(%+v), got err: %v, wantErr: %v", opts, err, test.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("UnmarshalListener(%+v) = %v wantErr: %q", opts, err, test.wantErr)
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

func (httpFilter) IsTerminal() bool {
	return false
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

func (errHTTPFilter) IsTerminal() bool {
	return false
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

func (serverOnlyHTTPFilter) IsTerminal() bool {
	return false
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

func (clientOnlyHTTPFilter) IsTerminal() bool {
	return false
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

// This custom filter uses the old TypedStruct message from the cncf/udpa repo.
var customFilterOldTypedStructConfig = &v1udpatypepb.TypedStruct{
	TypeUrl: "custom.filter",
	Value: &spb.Struct{
		Fields: map[string]*spb.Value{
			"foo": {Kind: &spb.Value_StringValue{StringValue: "bar"}},
		},
	},
}
var wrappedCustomFilterOldTypedStructConfig *anypb.Any

// This custom filter uses the new TypedStruct message from the cncf/xds repo.
var customFilterNewTypedStructConfig = &v3cncftypepb.TypedStruct{
	TypeUrl: "custom.filter",
	Value: &spb.Struct{
		Fields: map[string]*spb.Value{
			"foo": {Kind: &spb.Value_StringValue{StringValue: "bar"}},
		},
	},
}
var wrappedCustomFilterNewTypedStructConfig *anypb.Any

func init() {
	wrappedCustomFilterOldTypedStructConfig = testutils.MarshalAny(customFilterOldTypedStructConfig)
	wrappedCustomFilterNewTypedStructConfig = testutils.MarshalAny(customFilterNewTypedStructConfig)
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
