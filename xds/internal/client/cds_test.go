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
	"regexp"
	"testing"

	v2xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	v2corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/golang/protobuf/proto"
	anypb "github.com/golang/protobuf/ptypes/any"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	xdsinternal "google.golang.org/grpc/internal/xds"
	"google.golang.org/grpc/internal/xds/env"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	clusterName = "clusterName"
	serviceName = "service"
)

var emptyUpdate = ClusterUpdate{ServiceName: "", EnableLRS: false}

func (s) TestValidateCluster_Failure(t *testing.T) {
	tests := []struct {
		name       string
		cluster    *v3clusterpb.Cluster
		wantUpdate ClusterUpdate
		wantErr    bool
	}{
		{
			name: "non-eds-cluster-type",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_STATIC},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
				},
				LbPolicy: v3clusterpb.Cluster_LEAST_REQUEST,
			},
			wantUpdate: emptyUpdate,
			wantErr:    true,
		},
		{
			name: "no-eds-config",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				LbPolicy:             v3clusterpb.Cluster_ROUND_ROBIN,
			},
			wantUpdate: emptyUpdate,
			wantErr:    true,
		},
		{
			name: "no-ads-config-source",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig:     &v3clusterpb.Cluster_EdsClusterConfig{},
				LbPolicy:             v3clusterpb.Cluster_ROUND_ROBIN,
			},
			wantUpdate: emptyUpdate,
			wantErr:    true,
		},
		{
			name: "non-round-robin-lb-policy",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
				},
				LbPolicy: v3clusterpb.Cluster_LEAST_REQUEST,
			},
			wantUpdate: emptyUpdate,
			wantErr:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if update, err := validateCluster(test.cluster); err == nil {
				t.Errorf("validateCluster(%+v) = %v, wanted error", test.cluster, update)
			}
		})
	}
}

func (s) TestValidateCluster_Success(t *testing.T) {
	tests := []struct {
		name       string
		cluster    *v3clusterpb.Cluster
		wantUpdate ClusterUpdate
	}{
		{
			name: "happy-case-no-service-name-no-lrs",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
			},
			wantUpdate: emptyUpdate,
		},
		{
			name: "happy-case-no-lrs",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
			},
			wantUpdate: ClusterUpdate{ServiceName: serviceName, EnableLRS: false},
		},
		{
			name: "happiest-case",
			cluster: &v3clusterpb.Cluster{
				Name:                 clusterName,
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
				LrsServer: &v3corepb.ConfigSource{
					ConfigSourceSpecifier: &v3corepb.ConfigSource_Self{
						Self: &v3corepb.SelfConfigSource{},
					},
				},
			},
			wantUpdate: ClusterUpdate{ServiceName: serviceName, EnableLRS: true},
		},
		{
			name: "happiest-case-with-circuitbreakers",
			cluster: &v3clusterpb.Cluster{
				Name:                 clusterName,
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
				CircuitBreakers: &v3clusterpb.CircuitBreakers{
					Thresholds: []*v3clusterpb.CircuitBreakers_Thresholds{
						{
							Priority:    v3corepb.RoutingPriority_DEFAULT,
							MaxRequests: wrapperspb.UInt32(512),
						},
						{
							Priority:    v3corepb.RoutingPriority_HIGH,
							MaxRequests: nil,
						},
					},
				},
				LrsServer: &v3corepb.ConfigSource{
					ConfigSourceSpecifier: &v3corepb.ConfigSource_Self{
						Self: &v3corepb.SelfConfigSource{},
					},
				},
			},
			wantUpdate: ClusterUpdate{ServiceName: serviceName, EnableLRS: true, MaxRequests: func() *uint32 { i := uint32(512); return &i }()},
		},
	}

	origCircuitBreakingSupport := env.CircuitBreakingSupport
	env.CircuitBreakingSupport = true
	defer func() { env.CircuitBreakingSupport = origCircuitBreakingSupport }()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update, err := validateCluster(test.cluster)
			if err != nil {
				t.Errorf("validateCluster(%+v) failed: %v", test.cluster, err)
			}
			if !cmp.Equal(update, test.wantUpdate, cmpopts.EquateEmpty()) {
				t.Errorf("validateCluster(%+v) = %v, want: %v", test.cluster, update, test.wantUpdate)
			}
		})
	}
}

func (s) TestValidateClusterWithSecurityConfig_EnvVarOff(t *testing.T) {
	// Turn off the env var protection for client-side security.
	origClientSideSecurityEnvVar := env.ClientSideSecuritySupport
	env.ClientSideSecuritySupport = false
	defer func() { env.ClientSideSecuritySupport = origClientSideSecurityEnvVar }()

	cluster := &v3clusterpb.Cluster{
		ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
		EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
			EdsConfig: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
					Ads: &v3corepb.AggregatedConfigSource{},
				},
			},
			ServiceName: serviceName,
		},
		LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
		TransportSocket: &v3corepb.TransportSocket{
			Name: "envoy.transport_sockets.tls",
			ConfigType: &v3corepb.TransportSocket_TypedConfig{
				TypedConfig: &anypb.Any{
					TypeUrl: version.V3UpstreamTLSContextURL,
					Value: func() []byte {
						tls := &v3tlspb.UpstreamTlsContext{
							CommonTlsContext: &v3tlspb.CommonTlsContext{
								ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance{
									ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
										InstanceName:    "rootInstance",
										CertificateName: "rootCert",
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
	}
	wantUpdate := ClusterUpdate{
		ServiceName: serviceName,
		EnableLRS:   false,
	}
	gotUpdate, err := validateCluster(cluster)
	if err != nil {
		t.Errorf("validateCluster() failed: %v", err)
	}
	if diff := cmp.Diff(wantUpdate, gotUpdate); diff != "" {
		t.Errorf("validateCluster() returned unexpected diff (-want, got):\n%s", diff)
	}
}

func (s) TestValidateClusterWithSecurityConfig(t *testing.T) {
	// Turn on the env var protection for client-side security.
	origClientSideSecurityEnvVar := env.ClientSideSecuritySupport
	env.ClientSideSecuritySupport = true
	defer func() { env.ClientSideSecuritySupport = origClientSideSecurityEnvVar }()

	const (
		identityPluginInstance = "identityPluginInstance"
		identityCertName       = "identityCert"
		rootPluginInstance     = "rootPluginInstance"
		rootCertName           = "rootCert"
		serviceName            = "service"
		sanExact               = "san-exact"
		sanPrefix              = "san-prefix"
		sanSuffix              = "san-suffix"
		sanRegexBad            = "??"
		sanRegexGood           = "san?regex?"
		sanContains            = "san-contains"
	)
	var sanRE = regexp.MustCompile(sanRegexGood)

	tests := []struct {
		name       string
		cluster    *v3clusterpb.Cluster
		wantUpdate ClusterUpdate
		wantErr    bool
	}{
		{
			name: "transport-socket-unsupported-name",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
				TransportSocket: &v3corepb.TransportSocket{
					Name: "unsupported-foo",
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: &anypb.Any{
							TypeUrl: version.V3UpstreamTLSContextURL,
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "transport-socket-unsupported-typeURL",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
				TransportSocket: &v3corepb.TransportSocket{
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: &anypb.Any{
							TypeUrl: version.V3HTTPConnManagerURL,
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "transport-socket-unsupported-type",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
				TransportSocket: &v3corepb.TransportSocket{
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: &anypb.Any{
							TypeUrl: version.V3UpstreamTLSContextURL,
							Value:   []byte{1, 2, 3, 4},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "transport-socket-unsupported-validation-context",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
				TransportSocket: &v3corepb.TransportSocket{
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: &anypb.Any{
							TypeUrl: version.V3UpstreamTLSContextURL,
							Value: func() []byte {
								tls := &v3tlspb.UpstreamTlsContext{
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
			wantErr: true,
		},
		{
			name: "transport-socket-without-validation-context",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
				TransportSocket: &v3corepb.TransportSocket{
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: &anypb.Any{
							TypeUrl: version.V3UpstreamTLSContextURL,
							Value: func() []byte {
								tls := &v3tlspb.UpstreamTlsContext{
									CommonTlsContext: &v3tlspb.CommonTlsContext{},
								}
								mtls, _ := proto.Marshal(tls)
								return mtls
							}(),
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "empty-prefix-in-matching-SAN",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
				TransportSocket: &v3corepb.TransportSocket{
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: &anypb.Any{
							TypeUrl: version.V3UpstreamTLSContextURL,
							Value: func() []byte {
								tls := &v3tlspb.UpstreamTlsContext{
									CommonTlsContext: &v3tlspb.CommonTlsContext{
										ValidationContextType: &v3tlspb.CommonTlsContext_CombinedValidationContext{
											CombinedValidationContext: &v3tlspb.CommonTlsContext_CombinedCertificateValidationContext{
												DefaultValidationContext: &v3tlspb.CertificateValidationContext{
													MatchSubjectAltNames: []*v3matcherpb.StringMatcher{
														{MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: ""}},
													},
												},
												ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
													InstanceName:    rootPluginInstance,
													CertificateName: rootCertName,
												},
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
			wantErr: true,
		},
		{
			name: "empty-suffix-in-matching-SAN",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
				TransportSocket: &v3corepb.TransportSocket{
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: &anypb.Any{
							TypeUrl: version.V3UpstreamTLSContextURL,
							Value: func() []byte {
								tls := &v3tlspb.UpstreamTlsContext{
									CommonTlsContext: &v3tlspb.CommonTlsContext{
										ValidationContextType: &v3tlspb.CommonTlsContext_CombinedValidationContext{
											CombinedValidationContext: &v3tlspb.CommonTlsContext_CombinedCertificateValidationContext{
												DefaultValidationContext: &v3tlspb.CertificateValidationContext{
													MatchSubjectAltNames: []*v3matcherpb.StringMatcher{
														{MatchPattern: &v3matcherpb.StringMatcher_Suffix{Suffix: ""}},
													},
												},
												ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
													InstanceName:    rootPluginInstance,
													CertificateName: rootCertName,
												},
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
			wantErr: true,
		},
		{
			name: "empty-contains-in-matching-SAN",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
				TransportSocket: &v3corepb.TransportSocket{
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: &anypb.Any{
							TypeUrl: version.V3UpstreamTLSContextURL,
							Value: func() []byte {
								tls := &v3tlspb.UpstreamTlsContext{
									CommonTlsContext: &v3tlspb.CommonTlsContext{
										ValidationContextType: &v3tlspb.CommonTlsContext_CombinedValidationContext{
											CombinedValidationContext: &v3tlspb.CommonTlsContext_CombinedCertificateValidationContext{
												DefaultValidationContext: &v3tlspb.CertificateValidationContext{
													MatchSubjectAltNames: []*v3matcherpb.StringMatcher{
														{MatchPattern: &v3matcherpb.StringMatcher_Contains{Contains: ""}},
													},
												},
												ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
													InstanceName:    rootPluginInstance,
													CertificateName: rootCertName,
												},
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
			wantErr: true,
		},
		{
			name: "invalid-regex-in-matching-SAN",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
				TransportSocket: &v3corepb.TransportSocket{
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: &anypb.Any{
							TypeUrl: version.V3UpstreamTLSContextURL,
							Value: func() []byte {
								tls := &v3tlspb.UpstreamTlsContext{
									CommonTlsContext: &v3tlspb.CommonTlsContext{
										ValidationContextType: &v3tlspb.CommonTlsContext_CombinedValidationContext{
											CombinedValidationContext: &v3tlspb.CommonTlsContext_CombinedCertificateValidationContext{
												DefaultValidationContext: &v3tlspb.CertificateValidationContext{
													MatchSubjectAltNames: []*v3matcherpb.StringMatcher{
														{MatchPattern: &v3matcherpb.StringMatcher_SafeRegex{SafeRegex: &v3matcherpb.RegexMatcher{Regex: sanRegexBad}}},
													},
												},
												ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
													InstanceName:    rootPluginInstance,
													CertificateName: rootCertName,
												},
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
			wantErr: true,
		},
		{
			name: "happy-case-with-no-identity-certs",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
				TransportSocket: &v3corepb.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: &anypb.Any{
							TypeUrl: version.V3UpstreamTLSContextURL,
							Value: func() []byte {
								tls := &v3tlspb.UpstreamTlsContext{
									CommonTlsContext: &v3tlspb.CommonTlsContext{
										ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance{
											ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
												InstanceName:    rootPluginInstance,
												CertificateName: rootCertName,
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
			wantUpdate: ClusterUpdate{
				ServiceName: serviceName,
				EnableLRS:   false,
				SecurityCfg: &SecurityConfig{
					RootInstanceName: rootPluginInstance,
					RootCertName:     rootCertName,
				},
			},
		},
		{
			name: "happy-case-with-validation-context-provider-instance",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
				TransportSocket: &v3corepb.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: &anypb.Any{
							TypeUrl: version.V3UpstreamTLSContextURL,
							Value: func() []byte {
								tls := &v3tlspb.UpstreamTlsContext{
									CommonTlsContext: &v3tlspb.CommonTlsContext{
										TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
											InstanceName:    identityPluginInstance,
											CertificateName: identityCertName,
										},
										ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance{
											ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
												InstanceName:    rootPluginInstance,
												CertificateName: rootCertName,
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
			wantUpdate: ClusterUpdate{
				ServiceName: serviceName,
				EnableLRS:   false,
				SecurityCfg: &SecurityConfig{
					RootInstanceName:     rootPluginInstance,
					RootCertName:         rootCertName,
					IdentityInstanceName: identityPluginInstance,
					IdentityCertName:     identityCertName,
				},
			},
		},
		{
			name: "happy-case-with-combined-validation-context",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: serviceName,
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
				TransportSocket: &v3corepb.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: &anypb.Any{
							TypeUrl: version.V3UpstreamTLSContextURL,
							Value: func() []byte {
								tls := &v3tlspb.UpstreamTlsContext{
									CommonTlsContext: &v3tlspb.CommonTlsContext{
										TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
											InstanceName:    identityPluginInstance,
											CertificateName: identityCertName,
										},
										ValidationContextType: &v3tlspb.CommonTlsContext_CombinedValidationContext{
											CombinedValidationContext: &v3tlspb.CommonTlsContext_CombinedCertificateValidationContext{
												DefaultValidationContext: &v3tlspb.CertificateValidationContext{
													MatchSubjectAltNames: []*v3matcherpb.StringMatcher{
														{
															MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: sanExact},
															IgnoreCase:   true,
														},
														{MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: sanPrefix}},
														{MatchPattern: &v3matcherpb.StringMatcher_Suffix{Suffix: sanSuffix}},
														{MatchPattern: &v3matcherpb.StringMatcher_SafeRegex{SafeRegex: &v3matcherpb.RegexMatcher{Regex: sanRegexGood}}},
														{MatchPattern: &v3matcherpb.StringMatcher_Contains{Contains: sanContains}},
													},
												},
												ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
													InstanceName:    rootPluginInstance,
													CertificateName: rootCertName,
												},
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
			wantUpdate: ClusterUpdate{
				ServiceName: serviceName,
				EnableLRS:   false,
				SecurityCfg: &SecurityConfig{
					RootInstanceName:     rootPluginInstance,
					RootCertName:         rootCertName,
					IdentityInstanceName: identityPluginInstance,
					IdentityCertName:     identityCertName,
					SubjectAltNameMatchers: []xdsinternal.StringMatcher{
						xdsinternal.StringMatcherForTesting(newStringP(sanExact), nil, nil, nil, nil, true),
						xdsinternal.StringMatcherForTesting(nil, newStringP(sanPrefix), nil, nil, nil, false),
						xdsinternal.StringMatcherForTesting(nil, nil, newStringP(sanSuffix), nil, nil, false),
						xdsinternal.StringMatcherForTesting(nil, nil, nil, nil, sanRE, false),
						xdsinternal.StringMatcherForTesting(nil, nil, nil, newStringP(sanContains), nil, false),
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update, err := validateCluster(test.cluster)
			if (err != nil) != test.wantErr {
				t.Errorf("validateCluster() returned err %v wantErr %v)", err, test.wantErr)
			}
			if diff := cmp.Diff(test.wantUpdate, update, cmpopts.EquateEmpty(), cmp.AllowUnexported(regexp.Regexp{})); diff != "" {
				t.Errorf("validateCluster() returned unexpected diff (-want, +got):\n%s", diff)
			}
		})
	}
}

func (s) TestUnmarshalCluster(t *testing.T) {
	const (
		v2ClusterName = "v2clusterName"
		v3ClusterName = "v3clusterName"
		v2Service     = "v2Service"
		v3Service     = "v2Service"
	)
	var (
		v2Cluster = &v2xdspb.Cluster{
			Name:                 v2ClusterName,
			ClusterDiscoveryType: &v2xdspb.Cluster_Type{Type: v2xdspb.Cluster_EDS},
			EdsClusterConfig: &v2xdspb.Cluster_EdsClusterConfig{
				EdsConfig: &v2corepb.ConfigSource{
					ConfigSourceSpecifier: &v2corepb.ConfigSource_Ads{
						Ads: &v2corepb.AggregatedConfigSource{},
					},
				},
				ServiceName: v2Service,
			},
			LbPolicy: v2xdspb.Cluster_ROUND_ROBIN,
			LrsServer: &v2corepb.ConfigSource{
				ConfigSourceSpecifier: &v2corepb.ConfigSource_Self{
					Self: &v2corepb.SelfConfigSource{},
				},
			},
		}
		v2ClusterAny = &anypb.Any{
			TypeUrl: version.V2ClusterURL,
			Value: func() []byte {
				mcl, _ := proto.Marshal(v2Cluster)
				return mcl
			}(),
		}

		v3Cluster = &v3clusterpb.Cluster{
			Name:                 v3ClusterName,
			ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
			EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
				EdsConfig: &v3corepb.ConfigSource{
					ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
						Ads: &v3corepb.AggregatedConfigSource{},
					},
				},
				ServiceName: v3Service,
			},
			LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
			LrsServer: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Self{
					Self: &v3corepb.SelfConfigSource{},
				},
			},
		}
		v3ClusterAny = &anypb.Any{
			TypeUrl: version.V3ClusterURL,
			Value: func() []byte {
				mcl, _ := proto.Marshal(v3Cluster)
				return mcl
			}(),
		}
	)
	const testVersion = "test-version-cds"

	tests := []struct {
		name       string
		resources  []*anypb.Any
		wantUpdate map[string]ClusterUpdate
		wantMD     UpdateMetadata
		wantErr    bool
	}{
		{
			name:      "non-cluster resource type",
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
			name: "badly marshaled cluster resource",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ClusterURL,
					Value:   []byte{1, 2, 3, 4},
				},
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
		{
			name: "bad cluster resource",
			resources: []*anypb.Any{
				{
					TypeUrl: version.V3ClusterURL,
					Value: func() []byte {
						cl := &v3clusterpb.Cluster{
							Name:                 "test",
							ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_STATIC},
						}
						mcl, _ := proto.Marshal(cl)
						return mcl
					}(),
				},
			},
			wantUpdate: map[string]ClusterUpdate{"test": {}},
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
			name:      "v2 cluster",
			resources: []*anypb.Any{v2ClusterAny},
			wantUpdate: map[string]ClusterUpdate{
				v2ClusterName: {
					ServiceName: v2Service, EnableLRS: true,
					Raw: v2ClusterAny,
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "v3 cluster",
			resources: []*anypb.Any{v3ClusterAny},
			wantUpdate: map[string]ClusterUpdate{
				v3ClusterName: {
					ServiceName: v3Service, EnableLRS: true,
					Raw: v3ClusterAny,
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			name:      "multiple clusters",
			resources: []*anypb.Any{v2ClusterAny, v3ClusterAny},
			wantUpdate: map[string]ClusterUpdate{
				v2ClusterName: {
					ServiceName: v2Service, EnableLRS: true,
					Raw: v2ClusterAny,
				},
				v3ClusterName: {
					ServiceName: v3Service, EnableLRS: true,
					Raw: v3ClusterAny,
				},
			},
			wantMD: UpdateMetadata{
				Status:  ServiceStatusACKed,
				Version: testVersion,
			},
		},
		{
			// To test that unmarshal keeps processing on errors.
			name: "good and bad clusters",
			resources: []*anypb.Any{
				v2ClusterAny,
				{
					// bad cluster resource
					TypeUrl: version.V3ClusterURL,
					Value: func() []byte {
						cl := &v3clusterpb.Cluster{
							Name:                 "bad",
							ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_STATIC},
						}
						mcl, _ := proto.Marshal(cl)
						return mcl
					}(),
				},
				v3ClusterAny,
			},
			wantUpdate: map[string]ClusterUpdate{
				v2ClusterName: {
					ServiceName: v2Service, EnableLRS: true,
					Raw: v2ClusterAny,
				},
				v3ClusterName: {
					ServiceName: v3Service, EnableLRS: true,
					Raw: v3ClusterAny,
				},
				"bad": {},
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
			update, md, err := UnmarshalCluster(testVersion, test.resources, nil)
			if (err != nil) != test.wantErr {
				t.Fatalf("UnmarshalCluster(), got err: %v, wantErr: %v", err, test.wantErr)
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
