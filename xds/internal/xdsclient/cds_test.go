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
	"regexp"
	"testing"

	v2xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	v2corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3aggregateclusterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/clusters/aggregate/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	anypb "github.com/golang/protobuf/ptypes/any"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/env"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	clusterName = "clusterName"
	serviceName = "service"
)

var emptyUpdate = ClusterUpdate{ClusterName: clusterName, EnableLRS: false}

func (s) TestValidateCluster_Failure(t *testing.T) {
	tests := []struct {
		name       string
		cluster    *v3clusterpb.Cluster
		wantUpdate ClusterUpdate
		wantErr    bool
	}{
		{
			name: "non-supported-cluster-type-static",
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
			name: "non-supported-cluster-type-original-dst",
			cluster: &v3clusterpb.Cluster{
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_ORIGINAL_DST},
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
		{
			name: "logical-dns-multiple-localities",
			cluster: &v3clusterpb.Cluster{
				Name:                 clusterName,
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_LOGICAL_DNS},
				LbPolicy:             v3clusterpb.Cluster_ROUND_ROBIN,
				LoadAssignment: &v3endpointpb.ClusterLoadAssignment{
					Endpoints: []*v3endpointpb.LocalityLbEndpoints{
						// Invalid if there are more than one locality.
						{LbEndpoints: nil},
						{LbEndpoints: nil},
					},
				},
			},
			wantUpdate: emptyUpdate,
			wantErr:    true,
		},
	}

	oldAggregateAndDNSSupportEnv := env.AggregateAndDNSSupportEnv
	env.AggregateAndDNSSupportEnv = true
	defer func() { env.AggregateAndDNSSupportEnv = oldAggregateAndDNSSupportEnv }()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if update, err := validateClusterAndConstructClusterUpdate(test.cluster); err == nil {
				t.Errorf("validateClusterAndConstructClusterUpdate(%+v) = %v, wanted error", test.cluster, update)
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
			name: "happy-case-logical-dns",
			cluster: &v3clusterpb.Cluster{
				Name:                 clusterName,
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_LOGICAL_DNS},
				LbPolicy:             v3clusterpb.Cluster_ROUND_ROBIN,
				LoadAssignment: &v3endpointpb.ClusterLoadAssignment{
					Endpoints: []*v3endpointpb.LocalityLbEndpoints{{
						LbEndpoints: []*v3endpointpb.LbEndpoint{{
							HostIdentifier: &v3endpointpb.LbEndpoint_Endpoint{
								Endpoint: &v3endpointpb.Endpoint{
									Address: &v3corepb.Address{
										Address: &v3corepb.Address_SocketAddress{
											SocketAddress: &v3corepb.SocketAddress{
												Address: "dns_host",
												PortSpecifier: &v3corepb.SocketAddress_PortValue{
													PortValue: 8080,
												},
											},
										},
									},
								},
							},
						}},
					}},
				},
			},
			wantUpdate: ClusterUpdate{
				ClusterName: clusterName,
				ClusterType: ClusterTypeLogicalDNS,
				DNSHostName: "dns_host:8080",
			},
		},
		{
			name: "happy-case-aggregate-v3",
			cluster: &v3clusterpb.Cluster{
				Name: clusterName,
				ClusterDiscoveryType: &v3clusterpb.Cluster_ClusterType{
					ClusterType: &v3clusterpb.Cluster_CustomClusterType{
						Name: "envoy.clusters.aggregate",
						TypedConfig: testutils.MarshalAny(&v3aggregateclusterpb.ClusterConfig{
							Clusters: []string{"a", "b", "c"},
						}),
					},
				},
				LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
			},
			wantUpdate: ClusterUpdate{
				ClusterName: clusterName, EnableLRS: false, ClusterType: ClusterTypeAggregate,
				PrioritizedClusterNames: []string{"a", "b", "c"},
			},
		},
		{
			name: "happy-case-no-service-name-no-lrs",
			cluster: &v3clusterpb.Cluster{
				Name:                 clusterName,
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
			},
			wantUpdate: ClusterUpdate{ClusterName: clusterName, EDSServiceName: serviceName, EnableLRS: false},
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
			wantUpdate: ClusterUpdate{ClusterName: clusterName, EDSServiceName: serviceName, EnableLRS: true},
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
			wantUpdate: ClusterUpdate{ClusterName: clusterName, EDSServiceName: serviceName, EnableLRS: true, MaxRequests: func() *uint32 { i := uint32(512); return &i }()},
		},
	}

	oldAggregateAndDNSSupportEnv := env.AggregateAndDNSSupportEnv
	env.AggregateAndDNSSupportEnv = true
	defer func() { env.AggregateAndDNSSupportEnv = oldAggregateAndDNSSupportEnv }()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update, err := validateClusterAndConstructClusterUpdate(test.cluster)
			if err != nil {
				t.Errorf("validateClusterAndConstructClusterUpdate(%+v) failed: %v", test.cluster, err)
			}
			if diff := cmp.Diff(update, test.wantUpdate, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("validateClusterAndConstructClusterUpdate(%+v) got diff: %v (-got, +want)", test.cluster, diff)
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
		TransportSocket: &v3corepb.TransportSocket{
			Name: "envoy.transport_sockets.tls",
			ConfigType: &v3corepb.TransportSocket_TypedConfig{
				TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{
					CommonTlsContext: &v3tlspb.CommonTlsContext{
						ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance{
							ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
								InstanceName:    "rootInstance",
								CertificateName: "rootCert",
							},
						},
					},
				}),
			},
		},
	}
	wantUpdate := ClusterUpdate{
		ClusterName:    clusterName,
		EDSServiceName: serviceName,
		EnableLRS:      false,
	}
	gotUpdate, err := validateClusterAndConstructClusterUpdate(cluster)
	if err != nil {
		t.Errorf("validateClusterAndConstructClusterUpdate() failed: %v", err)
	}
	if diff := cmp.Diff(wantUpdate, gotUpdate); diff != "" {
		t.Errorf("validateClusterAndConstructClusterUpdate() returned unexpected diff (-want, got):\n%s", diff)
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
		clusterName            = "cluster"
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
						TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{
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
						TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{
							CommonTlsContext: &v3tlspb.CommonTlsContext{},
						}),
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
						TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{
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
						}),
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
						TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{
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
						}),
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
						TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{
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
						}),
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
						TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{
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
						}),
					},
				},
			},
			wantErr: true,
		},
		{
			name: "happy-case-with-no-identity-certs",
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
				TransportSocket: &v3corepb.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{
							CommonTlsContext: &v3tlspb.CommonTlsContext{
								ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance{
									ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
										InstanceName:    rootPluginInstance,
										CertificateName: rootCertName,
									},
								},
							},
						}),
					},
				},
			},
			wantUpdate: ClusterUpdate{
				ClusterName:    clusterName,
				EDSServiceName: serviceName,
				EnableLRS:      false,
				SecurityCfg: &SecurityConfig{
					RootInstanceName: rootPluginInstance,
					RootCertName:     rootCertName,
				},
			},
		},
		{
			name: "happy-case-with-validation-context-provider-instance",
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
				TransportSocket: &v3corepb.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{
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
						}),
					},
				},
			},
			wantUpdate: ClusterUpdate{
				ClusterName:    clusterName,
				EDSServiceName: serviceName,
				EnableLRS:      false,
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
				TransportSocket: &v3corepb.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{
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
						}),
					},
				},
			},
			wantUpdate: ClusterUpdate{
				ClusterName:    clusterName,
				EDSServiceName: serviceName,
				EnableLRS:      false,
				SecurityCfg: &SecurityConfig{
					RootInstanceName:     rootPluginInstance,
					RootCertName:         rootCertName,
					IdentityInstanceName: identityPluginInstance,
					IdentityCertName:     identityCertName,
					SubjectAltNameMatchers: []matcher.StringMatcher{
						matcher.StringMatcherForTesting(newStringP(sanExact), nil, nil, nil, nil, true),
						matcher.StringMatcherForTesting(nil, newStringP(sanPrefix), nil, nil, nil, false),
						matcher.StringMatcherForTesting(nil, nil, newStringP(sanSuffix), nil, nil, false),
						matcher.StringMatcherForTesting(nil, nil, nil, nil, sanRE, false),
						matcher.StringMatcherForTesting(nil, nil, nil, newStringP(sanContains), nil, false),
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update, err := validateClusterAndConstructClusterUpdate(test.cluster)
			if (err != nil) != test.wantErr {
				t.Errorf("validateClusterAndConstructClusterUpdate() returned err %v wantErr %v)", err, test.wantErr)
			}
			if diff := cmp.Diff(test.wantUpdate, update, cmpopts.EquateEmpty(), cmp.AllowUnexported(regexp.Regexp{})); diff != "" {
				t.Errorf("validateClusterAndConstructClusterUpdate() returned unexpected diff (-want, +got):\n%s", diff)
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
		v2ClusterAny = testutils.MarshalAny(&v2xdspb.Cluster{
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
		})

		v3ClusterAny = testutils.MarshalAny(&v3clusterpb.Cluster{
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
		})
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
				testutils.MarshalAny(&v3clusterpb.Cluster{
					Name:                 "test",
					ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_STATIC},
				}),
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
					ClusterName:    v2ClusterName,
					EDSServiceName: v2Service, EnableLRS: true,
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
					ClusterName:    v3ClusterName,
					EDSServiceName: v3Service, EnableLRS: true,
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
					ClusterName:    v2ClusterName,
					EDSServiceName: v2Service, EnableLRS: true,
					Raw: v2ClusterAny,
				},
				v3ClusterName: {
					ClusterName:    v3ClusterName,
					EDSServiceName: v3Service, EnableLRS: true,
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
				// bad cluster resource
				testutils.MarshalAny(&v3clusterpb.Cluster{
					Name:                 "bad",
					ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_STATIC},
				}),
				v3ClusterAny,
			},
			wantUpdate: map[string]ClusterUpdate{
				v2ClusterName: {
					ClusterName:    v2ClusterName,
					EDSServiceName: v2Service, EnableLRS: true,
					Raw: v2ClusterAny,
				},
				v3ClusterName: {
					ClusterName:    v3ClusterName,
					EDSServiceName: v3Service, EnableLRS: true,
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
