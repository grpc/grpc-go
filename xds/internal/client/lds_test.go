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

package client

import (
	"fmt"
	"strings"
	"testing"
	"time"

	v1typepb "github.com/cncf/udpa/go/udpa/type/v1"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	spb "github.com/golang/protobuf/ptypes/struct"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/xds/internal/env"
	"google.golang.org/grpc/xds/internal/httpfilter"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/protobuf/types/known/durationpb"

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
	)

	var (
		v2Lis = &anypb.Any{
			TypeUrl: version.V2ListenerURL,
			Value: func() []byte {
				cm := &v2httppb.HttpConnectionManager{
					RouteSpecifier: &v2httppb.HttpConnectionManager_Rds{
						Rds: &v2httppb.Rds{
							ConfigSource: &v2corepb.ConfigSource{
								ConfigSourceSpecifier: &v2corepb.ConfigSource_Ads{Ads: &v2corepb.AggregatedConfigSource{}},
							},
							RouteConfigName: v2RouteConfigName,
						},
					},
				}
				mcm, _ := proto.Marshal(cm)
				lis := &v2xdspb.Listener{
					Name: v2LDSTarget,
					ApiListener: &v2listenerpb.ApiListener{
						ApiListener: &anypb.Any{
							TypeUrl: version.V2HTTPConnManagerURL,
							Value:   mcm,
						},
					},
				}
				mLis, _ := proto.Marshal(lis)
				return mLis
			}(),
		}
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
		v3LisWithFilters = func(fs ...*v3httppb.HttpFilter) *anypb.Any {
			hcm := &v3httppb.HttpConnectionManager{
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
			}
			return &anypb.Any{
				TypeUrl: version.V3ListenerURL,
				Value: func() []byte {
					mcm, _ := ptypes.MarshalAny(hcm)
					lis := &v3listenerpb.Listener{
						Name: v3LDSTarget,
						ApiListener: &v3listenerpb.ApiListener{
							ApiListener: mcm,
						},
					}
					mLis, _ := proto.Marshal(lis)
					return mLis
				}(),
			}
		}
	)
	const testVersion = "test-version-lds-client"

	tests := []struct {
		name       string
		resources  []*anypb.Any
		wantUpdate map[string]ListenerUpdate
		wantMD     UpdateMetadata
		wantErr    bool
		disableFI  bool // disable fault injection
	}{
		{
			name:      "non-listener resource",
			resources: []*anypb.Any{{TypeUrl: version.V3HTTPConnManagerURL}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: true,
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
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: true,
		},
		{
			name: "wrong type in apiListener",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							ApiListener: &v3listenerpb.ApiListener{
								ApiListener: &anypb.Any{
									TypeUrl: version.V2ListenerURL,
									Value: func() []byte {
										cm := &v3httppb.HttpConnectionManager{
											RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
												Rds: &v3httppb.Rds{
													ConfigSource: &v3corepb.ConfigSource{
														ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
													},
													RouteConfigName: v3RouteConfigName,
												},
											},
										}
										mcm, _ := proto.Marshal(cm)
										return mcm
									}(),
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: true,
		},
		{
			name: "empty httpConnMgr in apiListener",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							ApiListener: &v3listenerpb.ApiListener{
								ApiListener: &anypb.Any{
									TypeUrl: version.V2ListenerURL,
									Value: func() []byte {
										cm := &v3httppb.HttpConnectionManager{
											RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
												Rds: &v3httppb.Rds{},
											},
										}
										mcm, _ := proto.Marshal(cm)
										return mcm
									}(),
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: true,
		},
		{
			name: "scopedRoutes routeConfig in apiListener",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							ApiListener: &v3listenerpb.ApiListener{
								ApiListener: &anypb.Any{
									TypeUrl: version.V2ListenerURL,
									Value: func() []byte {
										cm := &v3httppb.HttpConnectionManager{
											RouteSpecifier: &v3httppb.HttpConnectionManager_ScopedRoutes{},
										}
										mcm, _ := proto.Marshal(cm)
										return mcm
									}(),
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: true,
		},
		{
			name: "rds.ConfigSource in apiListener is not ADS",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							ApiListener: &v3listenerpb.ApiListener{
								ApiListener: &anypb.Any{
									TypeUrl: version.V2ListenerURL,
									Value: func() []byte {
										cm := &v3httppb.HttpConnectionManager{
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
										}
										mcm, _ := proto.Marshal(cm)
										return mcm
									}(),
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: true,
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
			name:      "v3 with custom filter, fault injection disabled",
			resources: []*anypb.Any{v3LisWithFilters(customFilter)},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second, Raw: v3LisWithFilters(customFilter)},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
			disableFI: true,
		},
		{
			name:       "v3 with two filters with same name",
			resources:  []*anypb.Any{v3LisWithFilters(customFilter, customFilter)},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: true,
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
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: true,
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
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: true,
		},
		{
			name:       "v3 with optional err filter",
			resources:  []*anypb.Any{v3LisWithFilters(errOptionalFilter)},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: true,
		},
		{
			name:       "v3 with unknown filter",
			resources:  []*anypb.Any{v3LisWithFilters(unknownFilter)},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: true,
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
			name:      "v3 with error filter, fault injection disabled",
			resources: []*anypb.Any{v3LisWithFilters(errFilter)},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {
					RouteConfigName:   v3RouteConfigName,
					MaxStreamDuration: time.Second,
					Raw:               v3LisWithFilters(errFilter),
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
			disableFI: true,
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
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: "bad",
							ApiListener: &v3listenerpb.ApiListener{
								ApiListener: &anypb.Any{
									TypeUrl: version.V2ListenerURL,
									Value: func() []byte {
										cm := &v3httppb.HttpConnectionManager{
											RouteSpecifier: &v3httppb.HttpConnectionManager_ScopedRoutes{},
										}
										mcm, _ := proto.Marshal(cm)
										return mcm
									}()}}}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
				v3Lis,
			},
			wantUpdate: map[string]ListenerUpdate{
				v2LDSTarget: {RouteConfigName: v2RouteConfigName, Raw: v2Lis},
				v3LDSTarget: {RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second, Raw: v3Lis},
				"bad":       {},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			oldFI := env.FaultInjectionSupport
			env.FaultInjectionSupport = !test.disableFI

			update, md, err := UnmarshalListener(testVersion, test.resources, nil)
			if (err != nil) != test.wantErr {
				t.Errorf("UnmarshalListener(), got err: %v, wantErr: %v", err, test.wantErr)
			}
			if diff := cmp.Diff(update, test.wantUpdate, cmpOpts); diff != "" {
				t.Errorf("got unexpected update, diff (-got +want): %v", diff)
			}
			if diff := cmp.Diff(md, test.wantMD, cmpOptsIgnoreDetails); diff != "" {
				t.Errorf("got unexpected metadata, diff (-got +want): %v", diff)
			}
			env.FaultInjectionSupport = oldFI
		})
	}
}

func (s) TestUnmarshalListener_ServerSide(t *testing.T) {
	const v3LDSTarget = "grpc/server?udpa.resource.listening_address=0.0.0.0:9999"

	var (
		listenerEmptyTransportSocket = &anypb.Any{
			TypeUrl: version.V3ListenerURL,
			Value: func() []byte {
				lis := &v3listenerpb.Listener{
					Name: v3LDSTarget,
					Address: &v3corepb.Address{
						Address: &v3corepb.Address_SocketAddress{
							SocketAddress: &v3corepb.SocketAddress{
								Address: "0.0.0.0",
								PortSpecifier: &v3corepb.SocketAddress_PortValue{
									PortValue: 9999,
								},
							},
						},
					},
					FilterChains: []*v3listenerpb.FilterChain{
						{
							Name: "filter-chain-1",
						},
					},
				}
				mLis, _ := proto.Marshal(lis)
				return mLis
			}(),
		}
		listenerNoValidationContext = &anypb.Any{
			TypeUrl: version.V3ListenerURL,
			Value: func() []byte {
				lis := &v3listenerpb.Listener{
					Name: v3LDSTarget,
					Address: &v3corepb.Address{
						Address: &v3corepb.Address_SocketAddress{
							SocketAddress: &v3corepb.SocketAddress{
								Address: "0.0.0.0",
								PortSpecifier: &v3corepb.SocketAddress_PortValue{
									PortValue: 9999,
								},
							},
						},
					},
					FilterChains: []*v3listenerpb.FilterChain{
						{
							Name: "filter-chain-1",
							TransportSocket: &v3corepb.TransportSocket{
								Name: "envoy.transport_sockets.tls",
								ConfigType: &v3corepb.TransportSocket_TypedConfig{
									TypedConfig: &anypb.Any{
										TypeUrl: version.V3DownstreamTLSContextURL,
										Value: func() []byte {
											tls := &v3tlspb.DownstreamTlsContext{
												CommonTlsContext: &v3tlspb.CommonTlsContext{
													TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
														InstanceName:    "identityPluginInstance",
														CertificateName: "identityCertName",
													},
												},
											}
											mtls, _ := proto.Marshal(tls)
											return mtls
										}(),
									},
								},
							},
						},
					},
				}
				mLis, _ := proto.Marshal(lis)
				return mLis
			}(),
		}
		listenerWithValidationContext = &anypb.Any{
			TypeUrl: version.V3ListenerURL,
			Value: func() []byte {
				lis := &v3listenerpb.Listener{
					Name: v3LDSTarget,
					Address: &v3corepb.Address{
						Address: &v3corepb.Address_SocketAddress{
							SocketAddress: &v3corepb.SocketAddress{
								Address: "0.0.0.0",
								PortSpecifier: &v3corepb.SocketAddress_PortValue{
									PortValue: 9999,
								},
							},
						},
					},
					FilterChains: []*v3listenerpb.FilterChain{
						{
							Name: "filter-chain-1",
							TransportSocket: &v3corepb.TransportSocket{
								Name: "envoy.transport_sockets.tls",
								ConfigType: &v3corepb.TransportSocket_TypedConfig{
									TypedConfig: &anypb.Any{
										TypeUrl: version.V3DownstreamTLSContextURL,
										Value: func() []byte {
											tls := &v3tlspb.DownstreamTlsContext{
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
											}
											mtls, _ := proto.Marshal(tls)
											return mtls
										}(),
									},
								},
							},
						},
					},
				}
				mLis, _ := proto.Marshal(lis)
				return mLis
			}(),
		}
	)

	const testVersion = "test-version-lds-server"

	tests := []struct {
		name       string
		resources  []*anypb.Any
		wantUpdate map[string]ListenerUpdate
		wantMD     UpdateMetadata
		wantErr    string
	}{
		{
			name: "no address field",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: "no address field in LDS response",
		},
		{
			name: "no socket address field",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name:    v3LDSTarget,
							Address: &v3corepb.Address{},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: "no socket_address field in LDS response",
		},
		{
			name: "listener name does not match expected format",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: "foo",
							Address: &v3corepb.Address{
								Address: &v3corepb.Address_SocketAddress{
									SocketAddress: &v3corepb.SocketAddress{
										Address: "0.0.0.0",
										PortSpecifier: &v3corepb.SocketAddress_PortValue{
											PortValue: 9999,
										},
									},
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{"foo": {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: "no host:port in name field of LDS response",
		},
		{
			name: "host mismatch",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							Address: &v3corepb.Address{
								Address: &v3corepb.Address_SocketAddress{
									SocketAddress: &v3corepb.SocketAddress{
										Address: "1.2.3.4",
										PortSpecifier: &v3corepb.SocketAddress_PortValue{
											PortValue: 9999,
										},
									},
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: "socket_address host does not match the one in name",
		},
		{
			name: "port mismatch",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							Address: &v3corepb.Address{
								Address: &v3corepb.Address_SocketAddress{
									SocketAddress: &v3corepb.SocketAddress{
										Address: "0.0.0.0",
										PortSpecifier: &v3corepb.SocketAddress_PortValue{
											PortValue: 1234,
										},
									},
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: "socket_address port does not match the one in name",
		},
		{
			name: "unexpected number of filter chains",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							Address: &v3corepb.Address{
								Address: &v3corepb.Address_SocketAddress{
									SocketAddress: &v3corepb.SocketAddress{
										Address: "0.0.0.0",
										PortSpecifier: &v3corepb.SocketAddress_PortValue{
											PortValue: 9999,
										},
									},
								},
							},
							FilterChains: []*v3listenerpb.FilterChain{
								{Name: "filter-chain-1"},
								{Name: "filter-chain-2"},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: "filter chains count in LDS response does not match expected",
		},
		{
			name: "unexpected transport socket name",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							Address: &v3corepb.Address{
								Address: &v3corepb.Address_SocketAddress{
									SocketAddress: &v3corepb.SocketAddress{
										Address: "0.0.0.0",
										PortSpecifier: &v3corepb.SocketAddress_PortValue{
											PortValue: 9999,
										},
									},
								},
							},
							FilterChains: []*v3listenerpb.FilterChain{
								{
									Name: "filter-chain-1",
									TransportSocket: &v3corepb.TransportSocket{
										Name: "unsupported-transport-socket-name",
									},
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: "transport_socket field has unexpected name",
		},
		{
			name: "unexpected transport socket typedConfig URL",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							Address: &v3corepb.Address{
								Address: &v3corepb.Address_SocketAddress{
									SocketAddress: &v3corepb.SocketAddress{
										Address: "0.0.0.0",
										PortSpecifier: &v3corepb.SocketAddress_PortValue{
											PortValue: 9999,
										},
									},
								},
							},
							FilterChains: []*v3listenerpb.FilterChain{
								{
									Name: "filter-chain-1",
									TransportSocket: &v3corepb.TransportSocket{
										Name: "envoy.transport_sockets.tls",
										ConfigType: &v3corepb.TransportSocket_TypedConfig{
											TypedConfig: &anypb.Any{
												TypeUrl: version.V3UpstreamTLSContextURL,
											},
										},
									},
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: "transport_socket field has unexpected typeURL",
		},
		{
			name: "badly marshaled transport socket",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							Address: &v3corepb.Address{
								Address: &v3corepb.Address_SocketAddress{
									SocketAddress: &v3corepb.SocketAddress{
										Address: "0.0.0.0",
										PortSpecifier: &v3corepb.SocketAddress_PortValue{
											PortValue: 9999,
										},
									},
								},
							},
							FilterChains: []*v3listenerpb.FilterChain{
								{
									Name: "filter-chain-1",
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
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: "failed to unmarshal DownstreamTlsContext in LDS response",
		},
		{
			name: "missing CommonTlsContext",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							Address: &v3corepb.Address{
								Address: &v3corepb.Address_SocketAddress{
									SocketAddress: &v3corepb.SocketAddress{
										Address: "0.0.0.0",
										PortSpecifier: &v3corepb.SocketAddress_PortValue{
											PortValue: 9999,
										},
									},
								},
							},
							FilterChains: []*v3listenerpb.FilterChain{
								{
									Name: "filter-chain-1",
									TransportSocket: &v3corepb.TransportSocket{
										Name: "envoy.transport_sockets.tls",
										ConfigType: &v3corepb.TransportSocket_TypedConfig{
											TypedConfig: &anypb.Any{
												TypeUrl: version.V3DownstreamTLSContextURL,
												Value: func() []byte {
													tls := &v3tlspb.DownstreamTlsContext{}
													mtls, _ := proto.Marshal(tls)
													return mtls
												}(),
											},
										},
									},
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: "DownstreamTlsContext in LDS response does not contain a CommonTlsContext",
		},
		{
			name: "unsupported validation context in transport socket",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							Address: &v3corepb.Address{
								Address: &v3corepb.Address_SocketAddress{
									SocketAddress: &v3corepb.SocketAddress{
										Address: "0.0.0.0",
										PortSpecifier: &v3corepb.SocketAddress_PortValue{
											PortValue: 9999,
										},
									},
								},
							},
							FilterChains: []*v3listenerpb.FilterChain{
								{
									Name: "filter-chain-1",
									TransportSocket: &v3corepb.TransportSocket{
										Name: "envoy.transport_sockets.tls",
										ConfigType: &v3corepb.TransportSocket_TypedConfig{
											TypedConfig: &anypb.Any{
												TypeUrl: version.V3DownstreamTLSContextURL,
												Value: func() []byte {
													tls := &v3tlspb.DownstreamTlsContext{
														CommonTlsContext: &v3tlspb.CommonTlsContext{
															ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextSdsSecretConfig{
																ValidationContextSdsSecretConfig: &v3tlspb.SdsSecretConfig{
																	Name: "foo-sds-secret",
																},
															},
														},
													}
													mtls, _ := proto.Marshal(tls)
													return mtls
												}(),
											},
										},
									},
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: "validation context contains unexpected type",
		},
		{
			name:      "empty transport socket",
			resources: []*anypb.Any{listenerEmptyTransportSocket},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {Raw: listenerEmptyTransportSocket},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name: "no identity and root certificate providers",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							Address: &v3corepb.Address{
								Address: &v3corepb.Address_SocketAddress{
									SocketAddress: &v3corepb.SocketAddress{
										Address: "0.0.0.0",
										PortSpecifier: &v3corepb.SocketAddress_PortValue{
											PortValue: 9999,
										},
									},
								},
							},
							FilterChains: []*v3listenerpb.FilterChain{
								{
									Name: "filter-chain-1",
									TransportSocket: &v3corepb.TransportSocket{
										Name: "envoy.transport_sockets.tls",
										ConfigType: &v3corepb.TransportSocket_TypedConfig{
											TypedConfig: &anypb.Any{
												TypeUrl: version.V3DownstreamTLSContextURL,
												Value: func() []byte {
													tls := &v3tlspb.DownstreamTlsContext{
														RequireClientCertificate: &wrapperspb.BoolValue{Value: true},
														CommonTlsContext: &v3tlspb.CommonTlsContext{
															TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
																InstanceName:    "identityPluginInstance",
																CertificateName: "identityCertName",
															},
														},
													}
													mtls, _ := proto.Marshal(tls)
													return mtls
												}(),
											},
										},
									},
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: "security configuration on the server-side does not contain root certificate provider instance name, but require_client_cert field is set",
		},
		{
			name: "no identity certificate provider with require_client_cert",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value: func() []byte {
						lis := &v3listenerpb.Listener{
							Name: v3LDSTarget,
							Address: &v3corepb.Address{
								Address: &v3corepb.Address_SocketAddress{
									SocketAddress: &v3corepb.SocketAddress{
										Address: "0.0.0.0",
										PortSpecifier: &v3corepb.SocketAddress_PortValue{
											PortValue: 9999,
										},
									},
								},
							},
							FilterChains: []*v3listenerpb.FilterChain{
								{
									Name: "filter-chain-1",
									TransportSocket: &v3corepb.TransportSocket{
										Name: "envoy.transport_sockets.tls",
										ConfigType: &v3corepb.TransportSocket_TypedConfig{
											TypedConfig: &anypb.Any{
												TypeUrl: version.V3DownstreamTLSContextURL,
												Value: func() []byte {
													tls := &v3tlspb.DownstreamTlsContext{
														CommonTlsContext: &v3tlspb.CommonTlsContext{},
													}
													mtls, _ := proto.Marshal(tls)
													return mtls
												}(),
											},
										},
									},
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{v3LDSTarget: {}},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusNACKed,
				Version: testVersion,
				ErrState: &UpdateErrorMetadata{
					Version: testVersion,
					Err:     errPlaceHolder,
				},
			},
			wantErr: "security configuration on the server-side does not contain identity certificate provider instance name",
		},
		{
			name:      "happy case with no validation context",
			resources: []*anypb.Any{listenerNoValidationContext},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {
					SecurityCfg: &SecurityConfig{
						IdentityInstanceName: "identityPluginInstance",
						IdentityCertName:     "identityCertName",
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
					SecurityCfg: &SecurityConfig{
						RootInstanceName:     "rootPluginInstance",
						RootCertName:         "rootCertName",
						IdentityInstanceName: "identityPluginInstance",
						IdentityCertName:     "identityCertName",
						RequireClientCert:    true,
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
				t.Errorf("UnmarshalListener(), got err: %v, wantErr: %v", err, test.wantErr)
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
	var err error
	wrappedCustomFilterTypedStructConfig, err = ptypes.MarshalAny(customFilterTypedStructConfig)
	if err != nil {
		panic(err.Error())
	}
}

var unknownFilterConfig = &anypb.Any{
	TypeUrl: "unknown.custom.filter",
	Value:   []byte{1, 2, 3},
}

func wrappedOptionalFilter(name string) *anypb.Any {
	filter := &v3routepb.FilterConfig{
		IsOptional: true,
		Config: &anypb.Any{
			TypeUrl: name,
			Value:   []byte{1, 2, 3},
		},
	}
	w, err := ptypes.MarshalAny(filter)
	if err != nil {
		panic("error marshalling any: " + err.Error())
	}
	return w
}
