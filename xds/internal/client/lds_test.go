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
	"strings"
	"testing"
	"time"

	v2xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	v2corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v2httppb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	v2listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	anypb "github.com/golang/protobuf/ptypes/any"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/protobuf/types/known/durationpb"
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
		v3Lis = &anypb.Any{
			TypeUrl: version.V3ListenerURL,
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
					CommonHttpProtocolOptions: &v3corepb.HttpProtocolOptions{
						MaxStreamDuration: durationpb.New(time.Second),
					},
				}
				mcm, _ := ptypes.MarshalAny(cm)
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
	)

	tests := []struct {
		name       string
		resources  []*anypb.Any
		wantUpdate map[string]ListenerUpdate
		wantErr    bool
	}{
		{
			name:      "non-listener resource",
			resources: []*anypb.Any{{TypeUrl: version.V3HTTPConnManagerURL}},
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
			wantErr: true,
		},
		{
			name: "empty resource list",
		},
		{
			name:      "v2 listener resource",
			resources: []*anypb.Any{v2Lis},
			wantUpdate: map[string]ListenerUpdate{
				v2LDSTarget: {RouteConfigName: v2RouteConfigName},
			},
		},
		{
			name:      "v3 listener resource",
			resources: []*anypb.Any{v3Lis},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second},
			},
		},
		{
			name:      "multiple listener resources",
			resources: []*anypb.Any{v2Lis, v3Lis},
			wantUpdate: map[string]ListenerUpdate{
				v2LDSTarget: {RouteConfigName: v2RouteConfigName},
				v3LDSTarget: {RouteConfigName: v3RouteConfigName, MaxStreamDuration: time.Second},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update, err := UnmarshalListener(test.resources, nil)
			if ((err != nil) != test.wantErr) || !cmp.Equal(update, test.wantUpdate, cmpopts.EquateEmpty()) {
				t.Errorf("UnmarshalListener(%v) = (%v, %v) want (%v, %v)", test.resources, update, err, test.wantUpdate, test.wantErr)
			}
		})
	}
}

func (s) TestUnmarshalListener_ServerSide(t *testing.T) {
	const v3LDSTarget = "grpc/server?udpa.resource.listening_address=0.0.0.0:9999"

	tests := []struct {
		name       string
		resources  []*anypb.Any
		wantUpdate map[string]ListenerUpdate
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
			wantErr: "validation context contains unexpected type",
		},
		{
			name: "empty transport socket",
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
								},
							},
						}
						mLis, _ := proto.Marshal(lis)
						return mLis
					}(),
				},
			},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {},
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
			wantErr: "security configuration on the server-side does not contain identity certificate provider instance name",
		},
		{
			name: "happy case with no validation context",
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
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {
					SecurityCfg: &SecurityConfig{
						IdentityInstanceName: "identityPluginInstance",
						IdentityCertName:     "identityCertName",
					},
				},
			},
		},
		{
			name: "happy case with validation context provider instance",
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
				},
			},
			wantUpdate: map[string]ListenerUpdate{
				v3LDSTarget: {
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotUpdate, err := UnmarshalListener(test.resources, nil)
			if (err != nil) != (test.wantErr != "") {
				t.Fatalf("UnmarshalListener(%v) = %v wantErr: %q", test.resources, err, test.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("UnmarshalListener(%v) = %v wantErr: %q", test.resources, err, test.wantErr)
			}
			if !cmp.Equal(gotUpdate, test.wantUpdate, cmpopts.EquateEmpty()) {
				t.Errorf("UnmarshalListener(%v) = %v want %v", test.resources, gotUpdate, test.wantUpdate)
			}
		})
	}
}
