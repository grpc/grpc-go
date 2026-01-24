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
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var cmpOptsIgnoreRawProto = cmp.Options{
	cmpopts.EquateEmpty(),
	cmpopts.IgnoreFields(ListenerUpdate{}, "Raw"),
	cmpopts.IgnoreFields(RouteConfigUpdate{}, "Raw"),
	protocmp.Transform(),
}

// Tests cases where the filter chain match criteria contains unsupported
// fields, that result in the chain being dropped.
func (s) TestUnmarshalListener_ServerSide_DroppedFilterChains(t *testing.T) {
	tests := []struct {
		desc     string
		resource *anypb.Any
		lis      *v3listenerpb.Listener
		wantErr  string
	}{
		{
			desc: "unsupported destination port field",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{DestinationPort: &wrapperspb.UInt32Value{Value: 666}},
						Name:             "test-filter-chain",
					},
				},
			},
			wantErr: `Dropping filter chain "test-filter-chain" since it contains unsupported destination_port match field`,
		},
		{
			desc: "unsupported server names field",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{ServerNames: []string{"example-server"}},
						Name:             "test-filter-chain",
					},
				},
			},
			wantErr: `Dropping filter chain "test-filter-chain" since it contains unsupported server_names match field`,
		},
		{
			desc: "unsupported transport protocol field",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{TransportProtocol: "tls"},
						Name:             "test-filter-chain",
					},
				},
			},
			wantErr: `Dropping filter chain "test-filter-chain" since it contains unsupported value for transport_protocol match field`,
		},
		{
			desc: "unsupported application protocol field",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{ApplicationProtocols: []string{"h2"}},
						Name:             "test-filter-chain",
					},
				},
			},
			wantErr: `Dropping filter chain "test-filter-chain" since it contains unsupported application_protocols match field`,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			grpctest.ExpectWarning(test.wantErr)
			resource := listenerProtoToAny(t, test.lis)
			if _, _, err := unmarshalListenerResource(resource, nil); err == nil {
				t.Errorf("unmarshalListenerResource(%s) succeeded when expected to fail", pretty.ToJSON(resource))
			}
		})
	}
}

// Tests cases where the filter chain match criteria contains invalid
// information, that result in unmarshaling failure.
func (s) TestUnmarshalListener_ServerSide_FilterChains_FailureCases(t *testing.T) {
	tests := []struct {
		desc    string
		lis     *v3listenerpb.Listener
		wantErr string
	}{
		{
			desc: "bad dest address prefix",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{{AddressPrefix: "a.b.c.d"}}},
						Name:             "test-filter-chain",
					},
				},
			},
			wantErr: `failed to parse destination prefix range: address_prefix:"a.b.c.d"`,
		},
		{
			desc: "bad dest prefix length",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("10.1.1.0", 50)}},
						Name:             "test-filter-chain",
					},
				},
			},
			wantErr: `failed to parse destination prefix range: address_prefix:"10.1.1.0"`,
		},
		{
			desc: "bad source address prefix",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePrefixRanges: []*v3corepb.CidrRange{{AddressPrefix: "a.b.c.d"}}},
						Name:             "test-filter-chain",
					},
				},
			},
			wantErr: `failed to parse source prefix range: address_prefix:"a.b.c.d"`,
		},
		{
			desc: "bad source prefix length",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("10.1.1.0", 50)}},
						Name:             "test-filter-chain",
					},
				},
			},
			wantErr: `failed to parse source prefix range: address_prefix:"10.1.1.0"`,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			resource := listenerProtoToAny(t, test.lis)
			if _, _, err := unmarshalListenerResource(resource, nil); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("unmarshalListenerResource(%s) failed with error: %v, want: %v", pretty.ToJSON(resource), err, test.wantErr)
			}
		})
	}
}

// Tests cases where there are multiple filter chains and they have overlapping
// match rules.
func (s) TestUnmarshalListener_ServerSide_OverlappingMatchingRules(t *testing.T) {
	tests := []struct {
		desc string
		lis  *v3listenerpb.Listener
	}{
		{
			desc: "matching destination prefixes with no other matchers",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16), cidrRangeFromAddressAndPrefixLen("10.0.0.0", 0)},
						},
						Filters: emptyValidNetworkFilters(t),
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.2.2", 16)},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
		},
		{
			desc: "matching source type",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourceType: v3listenerpb.FilterChainMatch_ANY},
						Filters:          emptyValidNetworkFilters(t),
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourceType: v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK},
						Filters:          emptyValidNetworkFilters(t),
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourceType: v3listenerpb.FilterChainMatch_EXTERNAL},
						Filters:          emptyValidNetworkFilters(t),
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourceType: v3listenerpb.FilterChainMatch_EXTERNAL},
						Filters:          emptyValidNetworkFilters(t),
					},
				},
			},
		},
		{
			desc: "matching source prefixes",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16), cidrRangeFromAddressAndPrefixLen("10.0.0.0", 0)},
						},
						Filters: emptyValidNetworkFilters(t),
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.2.2", 16)},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
		},
		{
			desc: "matching source ports",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePorts: []uint32{1, 2, 3, 4, 5}},
						Filters:          emptyValidNetworkFilters(t),
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{},
						Filters:          emptyValidNetworkFilters(t),
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePorts: []uint32{5, 6, 7}},
						Filters:          emptyValidNetworkFilters(t),
					},
				},
			},
		},
	}

	const wantErr = "multiple filter chains with overlapping matching rules are defined"
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			resource := listenerProtoToAny(t, test.lis)
			if _, _, err := unmarshalListenerResource(resource, nil); err == nil || !strings.Contains(err.Error(), wantErr) {
				t.Errorf("unmarshalListenerResource(%s) failed with error: %v, want: %v", pretty.ToJSON(resource), err, wantErr)
			}
		})
	}
}

// Tests cases where the security configuration in the filter chain is invalid.
func (s) TestUnmarshalListener_ServerSide_BadSecurityConfig(t *testing.T) {
	tests := []struct {
		desc                      string
		lis                       *v3listenerpb.Listener
		wantErr                   string
		enableSystemRootCertsFlag bool
	}{
		{
			desc:    "no filter chains",
			lis:     &v3listenerpb.Listener{Address: localSocketAddress},
			wantErr: "no supported filter chains and no default filter chain",
		},
		{
			desc: "unexpected transport socket name",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{Name: "unsupported-transport-socket-name"},
						Filters:         emptyValidNetworkFilters(t),
					},
				},
			},
			wantErr: "transport_socket field has unexpected name",
		},
		{
			desc: "unexpected transport socket URL",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3tlspb.UpstreamTlsContext{}),
							},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
			wantErr: fmt.Sprintf("transport_socket missing typed_config or wrong type_url: \"%s\"", testutils.MarshalAny(t, &v3tlspb.UpstreamTlsContext{}).TypeUrl),
		},
		{
			desc: "badly marshaled transport socket",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: &anypb.Any{
									TypeUrl: version.V3DownstreamTLSContextURL,
									Value:   []byte{1, 2, 3, 4},
								},
							},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
			wantErr: "failed to unmarshal DownstreamTlsContext in LDS response",
		},
		{
			desc: "missing CommonTlsContext",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{}),
							},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
			wantErr: "DownstreamTlsContext in LDS response does not contain a CommonTlsContext",
		},
		{
			desc: "require_sni-set-to-true-in-downstreamTlsContext",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
									RequireSni: &wrapperspb.BoolValue{Value: true},
								}),
							},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
			wantErr: "require_sni field set to true in DownstreamTlsContext message",
		},
		{
			desc: "unsupported-ocsp_staple_policy-in-downstreamTlsContext",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
									OcspStaplePolicy: v3tlspb.DownstreamTlsContext_STRICT_STAPLING,
								}),
							},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
			wantErr: "ocsp_staple_policy field set to unsupported value in DownstreamTlsContext message",
		},
		{
			desc: "unsupported validation context in transport socket",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
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
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
			wantErr: "validation context contains unexpected type",
		},
		{
			desc: "unsupported match_subject_alt_names field in transport socket",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
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
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
			wantErr: "validation context contains unexpected type",
		},
		{
			desc: "no root certificate provider with require_client_cert",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
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
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
			wantErr: "security configuration on the server-side does not contain root certificate provider instance name, but require_client_cert field is set",
		},
		{
			desc: "no identity certificate provider",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
									CommonTlsContext: &v3tlspb.CommonTlsContext{},
								}),
							},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
			wantErr: "security configuration on the server-side does not contain identity certificate provider instance name",
		},
		{
			desc:                      "system root certificate field set on server",
			enableSystemRootCertsFlag: true,
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
									RequireClientCertificate: &wrapperspb.BoolValue{Value: true},
									CommonTlsContext: &v3tlspb.CommonTlsContext{
										TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
											InstanceName:    "identityPluginInstance",
											CertificateName: "identityCertName",
										},
										ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContext{
											ValidationContext: &v3tlspb.CertificateValidationContext{
												SystemRootCerts: &v3tlspb.CertificateValidationContext_SystemRootCerts{},
											},
										},
									},
								}),
							},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
			wantErr: "expected field ca_certificate_provider_instance is missing and unexpected field system_root_certs is set",
		},
		{
			desc: "system root certificate field set on server, env var disabled",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
									RequireClientCertificate: &wrapperspb.BoolValue{Value: true},
									CommonTlsContext: &v3tlspb.CommonTlsContext{
										TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
											InstanceName:    "identityPluginInstance",
											CertificateName: "identityCertName",
										},
										ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContext{
											ValidationContext: &v3tlspb.CertificateValidationContext{
												SystemRootCerts: &v3tlspb.CertificateValidationContext_SystemRootCerts{},
											},
										},
									},
								}),
							},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
			},
			wantErr: "expected field ca_certificate_provider_instance is missing",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			testutils.SetEnvConfig(t, &envconfig.XDSSystemRootCertsEnabled, test.enableSystemRootCertsFlag)
			resource := listenerProtoToAny(t, test.lis)
			if _, _, err := unmarshalListenerResource(resource, nil); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("unmarshalListenerResource(%s) failed with error: %v, want: %v", pretty.ToJSON(resource), err, test.wantErr)
			}
		})
	}
}

// Tests cases where the route configuration specified in the HTTP Connection
// Manager within the filter chain is valid.
func (s) TestUnmarshalListener_ServerSide_GoodRouteUpdate(t *testing.T) {
	tests := []struct {
		name         string
		lis          *v3listenerpb.Listener
		wantListener ListenerUpdate
	}{
		{
			name: "one_route_config_name",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								Name: "hcm",
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
										RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
											Rds: &v3httppb.Rds{
												ConfigSource: &v3corepb.ConfigSource{
													ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
												},
												RouteConfigName: "route-1",
											},
										},
										HttpFilters: []*v3httppb.HttpFilter{emptyRouterFilter},
									}),
								},
							},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{
					Filters: []*v3listenerpb.Filter{
						{
							Name: "hcm",
							ConfigType: &v3listenerpb.Filter_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
									RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
										Rds: &v3httppb.Rds{
											ConfigSource: &v3corepb.ConfigSource{
												ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
											},
											RouteConfigName: "route-1",
										},
									},
									HttpFilters: []*v3httppb.HttpFilter{emptyRouterFilter},
								}),
							},
						},
					},
				},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{{
							SourceTypeArr: [3]*SourcePrefixes{{
								Entries: []*SourcePrefixEntry{{
									PortMap: map[int]*NetworkFilterChainConfig{
										0: {
											HTTPConnMgr: &HTTPConnectionManagerConfig{
												RouteConfigName: "route-1",
												HTTPFilters:     makeRouterFilterList(t),
											},
										},
									}},
								},
							}},
						}},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							RouteConfigName: "route-1",
							HTTPFilters:     makeRouterFilterList(t),
						},
					},
				},
			},
		},
		{
			name: "inline_route_config",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								Name: "hcm",
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
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
				DefaultFilterChain: &v3listenerpb.FilterChain{
					Filters: []*v3listenerpb.Filter{
						{
							Name: "hcm",
							ConfigType: &v3listenerpb.Filter_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
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
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{{
							SourceTypeArr: [3]*SourcePrefixes{{
								Entries: []*SourcePrefixEntry{{
									PortMap: map[int]*NetworkFilterChainConfig{
										0: {
											HTTPConnMgr: &HTTPConnectionManagerConfig{
												InlineRouteConfig: inlineRouteConfig,
												HTTPFilters:       makeRouterFilterList(t),
											},
										},
									}},
								},
							}},
						}},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters:       makeRouterFilterList(t),
						},
					},
				},
			},
		},
		{
			name: "two_route_config_names",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								Name: "hcm",
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
										RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
											Rds: &v3httppb.Rds{
												ConfigSource: &v3corepb.ConfigSource{
													ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
												},
												RouteConfigName: "route-1",
											},
										},
										HttpFilters: []*v3httppb.HttpFilter{emptyRouterFilter},
									}),
								},
							},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{
					Filters: []*v3listenerpb.Filter{
						{
							Name: "hcm",
							ConfigType: &v3listenerpb.Filter_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
									RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
										Rds: &v3httppb.Rds{
											ConfigSource: &v3corepb.ConfigSource{
												ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
											},
											RouteConfigName: "route-2",
										},
									},
									HttpFilters: []*v3httppb.HttpFilter{emptyRouterFilter},
								}),
							},
						},
					},
				},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{{
							SourceTypeArr: [3]*SourcePrefixes{{
								Entries: []*SourcePrefixEntry{{
									PortMap: map[int]*NetworkFilterChainConfig{
										0: {
											HTTPConnMgr: &HTTPConnectionManagerConfig{
												RouteConfigName: "route-1",
												HTTPFilters:     makeRouterFilterList(t),
											},
										},
									}},
								},
							}},
						}},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							RouteConfigName: "route-2",
							HTTPFilters:     makeRouterFilterList(t),
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource := listenerProtoToAny(t, test.lis)
			_, gotListener, err := unmarshalListenerResource(resource, nil)
			if err != nil {
				t.Fatalf("unmarshalListenerResource(%s) failed with error: %v", pretty.ToJSON(resource), err)
			}
			if diff := cmp.Diff(test.wantListener, gotListener, cmpOptsIgnoreRawProto); diff != "" {
				t.Errorf("unmarshalListenerResource(%s), got unexpected update, diff (-want, +got):\n%s", pretty.ToJSON(resource), diff)
			}
		})
	}
}

// Tests cases where the route configuration specified in the HTTP Connection
// Manager within the filter chain is invalid.
func (s) TestUnmarshalListener_ServerSide_BadRouteUpdate(t *testing.T) {
	tests := []struct {
		name    string
		lis     *v3listenerpb.Listener
		wantErr string
	}{
		{
			name: "missing-route-specifier",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								Name: "hcm",
								ConfigType: &v3listenerpb.Filter_TypedConfig{

									TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
										HttpFilters: []*v3httppb.HttpFilter{emptyRouterFilter},
									}),
								},
							},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{
					Filters: []*v3listenerpb.Filter{
						{
							Name: "hcm",
							ConfigType: &v3listenerpb.Filter_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
									HttpFilters: []*v3httppb.HttpFilter{emptyRouterFilter},
								}),
							},
						},
					},
				},
			},
			wantErr: "no RouteSpecifier",
		},
		{
			name: "not-ads",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								Name: "hcm",
								ConfigType: &v3listenerpb.Filter_TypedConfig{

									TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
										RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
											Rds: &v3httppb.Rds{
												RouteConfigName: "route-1",
											},
										},
										HttpFilters: []*v3httppb.HttpFilter{emptyRouterFilter},
									}),
								},
							},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{
					Filters: []*v3listenerpb.Filter{
						{
							Name: "hcm",
							ConfigType: &v3listenerpb.Filter_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
									RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
										Rds: &v3httppb.Rds{
											RouteConfigName: "route-1",
										},
									},
									HttpFilters: []*v3httppb.HttpFilter{emptyRouterFilter},
								}),
							},
						},
					},
				},
			},
			wantErr: "ConfigSource is not ADS",
		},
		{
			name: "unsupported-route-specifier",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								Name: "hcm",
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
										RouteSpecifier: &v3httppb.HttpConnectionManager_ScopedRoutes{},
										HttpFilters:    []*v3httppb.HttpFilter{emptyRouterFilter},
									}),
								},
							},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{
					Filters: []*v3listenerpb.Filter{
						{
							Name: "hcm",
							ConfigType: &v3listenerpb.Filter_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
									RouteSpecifier: &v3httppb.HttpConnectionManager_ScopedRoutes{},
									HttpFilters:    []*v3httppb.HttpFilter{emptyRouterFilter},
								}),
							},
						},
					},
				},
			},
			wantErr: "unsupported type",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource := listenerProtoToAny(t, test.lis)
			if _, _, err := unmarshalListenerResource(resource, nil); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("unmarshalListenerResource(%s) failed with error: %v, want: %v", pretty.ToJSON(resource), err, test.wantErr)
			}
		})
	}
}

// Tests cases where the HTTP Filters in the filter chain are invalid.
func (s) TestUnmarshalListener_ServerSide_BadHTTPFilters(t *testing.T) {
	tests := []struct {
		name    string
		lis     *v3listenerpb.Listener
		wantErr string
	}{
		{
			name: "client side HTTP filter",
			lis: &v3listenerpb.Listener{
				Name:    "grpc/server?xds.resource.listening_address=0.0.0.0:9999",
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								Name: "hcm",
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
										HttpFilters: []*v3httppb.HttpFilter{
											{
												Name:       "clientOnlyCustomFilter",
												ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: clientOnlyCustomFilterConfig},
											},
										},
									}),
								},
							},
						},
					},
				},
			},
			wantErr: "invalid server side HTTP Filters",
		},
		{
			name: "one valid then one invalid HTTP filter",
			lis: &v3listenerpb.Listener{
				Name:    "grpc/server?xds.resource.listening_address=0.0.0.0:9999",
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								Name: "hcm",
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
										HttpFilters: []*v3httppb.HttpFilter{
											validServerSideHTTPFilter1,
											{
												Name:       "clientOnlyCustomFilter",
												ConfigType: &v3httppb.HttpFilter_TypedConfig{TypedConfig: clientOnlyCustomFilterConfig},
											},
										},
									}),
								},
							},
						},
					},
				},
			},
			wantErr: "invalid server side HTTP Filters",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource := listenerProtoToAny(t, test.lis)
			if _, _, err := unmarshalListenerResource(resource, nil); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("unmarshalListenerResource(%s) failed with error: %v, want: %v", pretty.ToJSON(resource), err, test.wantErr)
			}
		})
	}
}

// Tests cases where the HTTP Filters in the filter chain are valid.
func (s) TestUnmarshalListener_ServerSide_GoodHTTPFilters(t *testing.T) {
	tests := []struct {
		name         string
		lis          *v3listenerpb.Listener
		wantListener ListenerUpdate
	}{
		{
			name: "singular valid http filter",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								Name: "hcm",
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
										HttpFilters: []*v3httppb.HttpFilter{
											validServerSideHTTPFilter1,
											emptyRouterFilter,
										},
										RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
											RouteConfig: routeConfig,
										},
									}),
								},
							},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{
					Filters: []*v3listenerpb.Filter{
						{
							Name: "hcm",
							ConfigType: &v3listenerpb.Filter_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
									HttpFilters: []*v3httppb.HttpFilter{
										validServerSideHTTPFilter1,
										emptyRouterFilter,
									},
									RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
										RouteConfig: routeConfig,
									},
								}),
							},
						},
					},
				},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{{
							SourceTypeArr: [3]*SourcePrefixes{{
								Entries: []*SourcePrefixEntry{{
									PortMap: map[int]*NetworkFilterChainConfig{
										0: {
											HTTPConnMgr: &HTTPConnectionManagerConfig{
												InlineRouteConfig: inlineRouteConfig,
												HTTPFilters: []HTTPFilter{
													{
														Name:   "serverOnlyCustomFilter",
														Filter: serverOnlyHTTPFilter{},
														Config: filterConfig{Cfg: serverOnlyCustomFilterConfig},
													},
													makeRouterFilter(t),
												},
											},
										},
									}},
								},
							}},
						}},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters: []HTTPFilter{
								{
									Name:   "serverOnlyCustomFilter",
									Filter: serverOnlyHTTPFilter{},
									Config: filterConfig{Cfg: serverOnlyCustomFilterConfig},
								},
								makeRouterFilter(t),
							},
						},
					},
				},
			},
		},
		{
			name: "two valid http filters",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								Name: "hcm",
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
										HttpFilters: []*v3httppb.HttpFilter{
											validServerSideHTTPFilter1,
											validServerSideHTTPFilter2,
											emptyRouterFilter,
										},
										RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
											RouteConfig: routeConfig,
										},
									}),
								},
							},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{
					Filters: []*v3listenerpb.Filter{
						{
							Name: "hcm",
							ConfigType: &v3listenerpb.Filter_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
									HttpFilters: []*v3httppb.HttpFilter{
										validServerSideHTTPFilter1,
										validServerSideHTTPFilter2,
										emptyRouterFilter,
									},
									RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
										RouteConfig: routeConfig,
									},
								}),
							},
						},
					},
				},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{{
							SourceTypeArr: [3]*SourcePrefixes{{
								Entries: []*SourcePrefixEntry{{
									PortMap: map[int]*NetworkFilterChainConfig{
										0: {
											HTTPConnMgr: &HTTPConnectionManagerConfig{
												InlineRouteConfig: inlineRouteConfig,
												HTTPFilters: []HTTPFilter{
													{
														Name:   "serverOnlyCustomFilter",
														Filter: serverOnlyHTTPFilter{},
														Config: filterConfig{Cfg: serverOnlyCustomFilterConfig},
													},
													{
														Name:   "serverOnlyCustomFilter2",
														Filter: serverOnlyHTTPFilter{},
														Config: filterConfig{Cfg: serverOnlyCustomFilterConfig},
													},
													makeRouterFilter(t),
												},
											},
										},
									}},
								},
							}},
						}},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters: []HTTPFilter{
								{
									Name:   "serverOnlyCustomFilter",
									Filter: serverOnlyHTTPFilter{},
									Config: filterConfig{Cfg: serverOnlyCustomFilterConfig},
								},
								{
									Name:   "serverOnlyCustomFilter2",
									Filter: serverOnlyHTTPFilter{},
									Config: filterConfig{Cfg: serverOnlyCustomFilterConfig},
								},
								makeRouterFilter(t),
							},
						},
					},
				},
			},
		},
		// In the case of two HTTP Connection Manager's being present, the
		// second HTTP Connection Manager should be validated, but ignored.
		{
			name: "two hcms",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
						Filters: []*v3listenerpb.Filter{
							{
								Name: "hcm",
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
										HttpFilters: []*v3httppb.HttpFilter{
											validServerSideHTTPFilter1,
											validServerSideHTTPFilter2,
											emptyRouterFilter,
										},
										RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
											RouteConfig: routeConfig,
										},
									}),
								},
							},
							{
								Name: "hcm2",
								ConfigType: &v3listenerpb.Filter_TypedConfig{
									TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
										HttpFilters: []*v3httppb.HttpFilter{
											validServerSideHTTPFilter1,
											emptyRouterFilter,
										},
										RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
											RouteConfig: routeConfig,
										},
									}),
								},
							},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{
					Filters: []*v3listenerpb.Filter{
						{
							Name: "hcm",
							ConfigType: &v3listenerpb.Filter_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
									HttpFilters: []*v3httppb.HttpFilter{
										validServerSideHTTPFilter1,
										validServerSideHTTPFilter2,
										emptyRouterFilter,
									},
									RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
										RouteConfig: routeConfig,
									},
								}),
							},
						},
						{
							Name: "hcm2",
							ConfigType: &v3listenerpb.Filter_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
									HttpFilters: []*v3httppb.HttpFilter{
										validServerSideHTTPFilter1,
										emptyRouterFilter,
									},
									RouteSpecifier: &v3httppb.HttpConnectionManager_RouteConfig{
										RouteConfig: routeConfig,
									},
								}),
							},
						},
					},
				},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{{
							SourceTypeArr: [3]*SourcePrefixes{{
								Entries: []*SourcePrefixEntry{{
									PortMap: map[int]*NetworkFilterChainConfig{
										0: {
											HTTPConnMgr: &HTTPConnectionManagerConfig{
												InlineRouteConfig: inlineRouteConfig,
												HTTPFilters: []HTTPFilter{
													{
														Name:   "serverOnlyCustomFilter",
														Filter: serverOnlyHTTPFilter{},
														Config: filterConfig{Cfg: serverOnlyCustomFilterConfig},
													},
													{
														Name:   "serverOnlyCustomFilter2",
														Filter: serverOnlyHTTPFilter{},
														Config: filterConfig{Cfg: serverOnlyCustomFilterConfig},
													},
													makeRouterFilter(t),
												},
											},
										},
									}},
								},
							}},
						}},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters: []HTTPFilter{
								{
									Name:   "serverOnlyCustomFilter",
									Filter: serverOnlyHTTPFilter{},
									Config: filterConfig{Cfg: serverOnlyCustomFilterConfig},
								},
								{
									Name:   "serverOnlyCustomFilter2",
									Filter: serverOnlyHTTPFilter{},
									Config: filterConfig{Cfg: serverOnlyCustomFilterConfig},
								},
								makeRouterFilter(t),
							},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource := listenerProtoToAny(t, test.lis)
			_, gotListener, err := unmarshalListenerResource(resource, nil)
			if err != nil {
				t.Fatalf("unmarshalListenerResource(%s) failed with error: %v", pretty.ToJSON(resource), err)
			}
			if diff := cmp.Diff(test.wantListener, gotListener, cmpOptsIgnoreRawProto); diff != "" {
				t.Errorf("unmarshalListenerResource(%s), got unexpected update, diff (-want, +got):\n%s", pretty.ToJSON(resource), diff)
			}
		})
	}
}

// Tests cases where the security configuration in the filter chain contains
// valid data.
func (s) TestUnmarshalListener_ServerSide_GoodSecurityConfig(t *testing.T) {
	tests := []struct {
		desc                      string
		lis                       *v3listenerpb.Listener
		wantListener              ListenerUpdate
		enableSystemRootCertsFlag bool
	}{
		{
			desc: "empty transport socket",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name:    "filter-chain-1",
						Filters: emptyValidNetworkFilters(t),
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{
					Filters: emptyValidNetworkFilters(t),
				},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{{
							SourceTypeArr: [3]*SourcePrefixes{{
								Entries: []*SourcePrefixEntry{{
									PortMap: map[int]*NetworkFilterChainConfig{
										0: {
											HTTPConnMgr: &HTTPConnectionManagerConfig{
												InlineRouteConfig: inlineRouteConfig,
												HTTPFilters:       makeRouterFilterList(t),
											},
										},
									}},
								},
							}},
						}},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters:       makeRouterFilterList(t),
						},
					},
				},
			},
		},
		{
			desc: "no validation context",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
									CommonTlsContext: &v3tlspb.CommonTlsContext{
										TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
											InstanceName:    "identityPluginInstance",
											CertificateName: "identityCertName",
										},
									},
								}),
							},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{
					TransportSocket: &v3corepb.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &v3corepb.TransportSocket_TypedConfig{
							TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
								CommonTlsContext: &v3tlspb.CommonTlsContext{
									TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
										InstanceName:    "defaultIdentityPluginInstance",
										CertificateName: "defaultIdentityCertName",
									},
								},
							}),
						},
					},
					Filters: emptyValidNetworkFilters(t),
				},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{{
							SourceTypeArr: [3]*SourcePrefixes{{
								Entries: []*SourcePrefixEntry{{
									PortMap: map[int]*NetworkFilterChainConfig{
										0: {
											SecurityCfg: &SecurityConfig{
												IdentityInstanceName: "identityPluginInstance",
												IdentityCertName:     "identityCertName",
											},
											HTTPConnMgr: &HTTPConnectionManagerConfig{
												InlineRouteConfig: inlineRouteConfig,
												HTTPFilters:       makeRouterFilterList(t),
											},
										},
									}},
								},
							}},
						}},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						SecurityCfg: &SecurityConfig{
							IdentityInstanceName: "defaultIdentityPluginInstance",
							IdentityCertName:     "defaultIdentityCertName",
						},
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters:       makeRouterFilterList(t),
						},
					},
				},
			},
		},
		{
			desc: "validation context with certificate provider",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
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
						Filters: emptyValidNetworkFilters(t),
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{
					Name: "default-filter-chain-1",
					TransportSocket: &v3corepb.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &v3corepb.TransportSocket_TypedConfig{
							TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
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
					Filters: emptyValidNetworkFilters(t),
				},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{{
							SourceTypeArr: [3]*SourcePrefixes{{
								Entries: []*SourcePrefixEntry{{
									PortMap: map[int]*NetworkFilterChainConfig{
										0: {
											SecurityCfg: &SecurityConfig{
												RootInstanceName:     "rootPluginInstance",
												RootCertName:         "rootCertName",
												IdentityInstanceName: "identityPluginInstance",
												IdentityCertName:     "identityCertName",
												RequireClientCert:    true,
											},
											HTTPConnMgr: &HTTPConnectionManagerConfig{
												InlineRouteConfig: inlineRouteConfig,
												HTTPFilters:       makeRouterFilterList(t),
											},
										},
									}},
								},
							}},
						}},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						SecurityCfg: &SecurityConfig{
							RootInstanceName:     "defaultRootPluginInstance",
							RootCertName:         "defaultRootCertName",
							IdentityInstanceName: "defaultIdentityPluginInstance",
							IdentityCertName:     "defaultIdentityCertName",
							RequireClientCert:    true,
						},
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters:       makeRouterFilterList(t),
						},
					},
				},
			},
		},
		{
			desc:                      "validation context with certificate provider and system root certs",
			enableSystemRootCertsFlag: true,
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
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
												// SystemRootCerts will be ignored
												// when
												// CaCertificateProviderInstance is
												// set.
												SystemRootCerts: &v3tlspb.CertificateValidationContext_SystemRootCerts{},
											},
										},
									},
								}),
							},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{
					Name: "default-filter-chain-1",
					TransportSocket: &v3corepb.TransportSocket{
						Name: "envoy.transport_sockets.tls",
						ConfigType: &v3corepb.TransportSocket_TypedConfig{
							TypedConfig: testutils.MarshalAny(t, &v3tlspb.DownstreamTlsContext{
								RequireClientCertificate: &wrapperspb.BoolValue{Value: true},
								CommonTlsContext: &v3tlspb.CommonTlsContext{
									TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
										InstanceName:    "defaultIdentityPluginInstance",
										CertificateName: "defaultIdentityCertName",
									},
									ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContext{
										ValidationContext: &v3tlspb.CertificateValidationContext{
											CaCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
												InstanceName:    "defaultRootPluginInstance",
												CertificateName: "defaultRootCertName",
											},
											// SystemRootCerts will be ignored
											// when
											// CaCertificateProviderInstance is
											// set.
											SystemRootCerts: &v3tlspb.CertificateValidationContext_SystemRootCerts{},
										},
									},
								},
							}),
						},
					},
					Filters: emptyValidNetworkFilters(t),
				},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{{
							SourceTypeArr: [3]*SourcePrefixes{{
								Entries: []*SourcePrefixEntry{{
									PortMap: map[int]*NetworkFilterChainConfig{
										0: {
											SecurityCfg: &SecurityConfig{
												RootInstanceName:     "rootPluginInstance",
												RootCertName:         "rootCertName",
												IdentityInstanceName: "identityPluginInstance",
												IdentityCertName:     "identityCertName",
												RequireClientCert:    true,
											},
											HTTPConnMgr: &HTTPConnectionManagerConfig{
												InlineRouteConfig: inlineRouteConfig,
												HTTPFilters:       makeRouterFilterList(t),
											},
										},
									}},
								},
							}},
						}},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						SecurityCfg: &SecurityConfig{
							RootInstanceName:     "defaultRootPluginInstance",
							RootCertName:         "defaultRootCertName",
							IdentityInstanceName: "defaultIdentityPluginInstance",
							IdentityCertName:     "defaultIdentityCertName",
							RequireClientCert:    true,
						},
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters:       makeRouterFilterList(t),
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			testutils.SetEnvConfig(t, &envconfig.XDSSystemRootCertsEnabled, test.enableSystemRootCertsFlag)
			resource := listenerProtoToAny(t, test.lis)
			_, gotListener, err := unmarshalListenerResource(resource, nil)
			if err != nil {
				t.Fatalf("unmarshalListenerResource(%s) failed with error: %v", pretty.ToJSON(resource), err)
			}
			if diff := cmp.Diff(test.wantListener, gotListener, cmpOptsIgnoreRawProto); diff != "" {
				t.Errorf("unmarshalListenerResource(%s), got unexpected update, diff (-want, +got):\n%s", pretty.ToJSON(resource), diff)
			}
		})
	}
}

// TestNewFilterChainImpl_Success_UnsupportedMatchFields verifies cases where
// there are multiple filter chains, and one of them is valid while the other
// contains unsupported match fields. These configurations should lead to
// success at config validation time and the filter chains which contains
// unsupported match fields will be skipped at lookup time.
func (s) TestUnmarshalListener_ServerSide_Success_UnsupportedMatchFields(t *testing.T) {
	tests := []struct {
		desc         string
		lis          *v3listenerpb.Listener
		wantListener ListenerUpdate
	}{
		{
			desc: "unsupported destination port",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name:    "good-chain",
						Filters: emptyValidNetworkFilters(t),
					},
					{
						Name: "unsupported-destination-port",
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:    []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							DestinationPort: &wrapperspb.UInt32Value{Value: 666},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{Filters: emptyValidNetworkFilters(t)},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{{
							SourceTypeArr: [3]*SourcePrefixes{{
								Entries: []*SourcePrefixEntry{{
									PortMap: map[int]*NetworkFilterChainConfig{
										0: {
											HTTPConnMgr: &HTTPConnectionManagerConfig{
												InlineRouteConfig: inlineRouteConfig,
												HTTPFilters:       makeRouterFilterList(t),
											},
										},
									}},
								},
							}},
						}},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters:       makeRouterFilterList(t),
						},
					},
				},
			},
		},
		{
			desc: "unsupported server names",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name:    "good-chain",
						Filters: emptyValidNetworkFilters(t),
					},
					{
						Name: "unsupported-server-names",
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							ServerNames:  []string{"example-server"},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{Filters: emptyValidNetworkFilters(t)},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{{
							SourceTypeArr: [3]*SourcePrefixes{{
								Entries: []*SourcePrefixEntry{{
									PortMap: map[int]*NetworkFilterChainConfig{
										0: {
											HTTPConnMgr: &HTTPConnectionManagerConfig{
												InlineRouteConfig: inlineRouteConfig,
												HTTPFilters:       makeRouterFilterList(t),
											},
										},
									}},
								},
							}},
						}},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters:       makeRouterFilterList(t),
						},
					},
				},
			},
		},
		{
			desc: "unsupported transport protocol",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name:    "good-chain",
						Filters: emptyValidNetworkFilters(t),
					},
					{
						Name: "unsupported-transport-protocol",
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:      []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							TransportProtocol: "tls",
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{Filters: emptyValidNetworkFilters(t)},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{{
							SourceTypeArr: [3]*SourcePrefixes{{
								Entries: []*SourcePrefixEntry{{
									PortMap: map[int]*NetworkFilterChainConfig{
										0: {
											HTTPConnMgr: &HTTPConnectionManagerConfig{
												InlineRouteConfig: inlineRouteConfig,
												HTTPFilters:       makeRouterFilterList(t),
											},
										},
									}},
								},
							}},
						}},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters:       makeRouterFilterList(t),
						},
					},
				},
			},
		},
		{
			desc: "unsupported application protocol",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name:    "good-chain",
						Filters: emptyValidNetworkFilters(t),
					},
					{
						Name: "unsupported-application-protocol",
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:         []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							ApplicationProtocols: []string{"h2"},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{Filters: emptyValidNetworkFilters(t)},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{{
							SourceTypeArr: [3]*SourcePrefixes{{
								Entries: []*SourcePrefixEntry{{
									PortMap: map[int]*NetworkFilterChainConfig{
										0: {
											HTTPConnMgr: &HTTPConnectionManagerConfig{
												InlineRouteConfig: inlineRouteConfig,
												HTTPFilters:       makeRouterFilterList(t),
											},
										},
									}},
								},
							}},
						}},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters:       makeRouterFilterList(t),
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			resource := listenerProtoToAny(t, test.lis)
			_, gotListener, err := unmarshalListenerResource(resource, nil)
			if err != nil {
				t.Fatalf("unmarshalListenerResource(%s) failed with error: %v", pretty.ToJSON(resource), err)
			}
			if diff := cmp.Diff(test.wantListener, gotListener, cmpOptsIgnoreRawProto); diff != "" {
				t.Errorf("unmarshalListenerResource(%s), got unexpected update, diff (-want, +got):\n%s", pretty.ToJSON(resource), diff)
			}
		})
	}
}

// TestNewFilterChainImpl_Success_AllCombinations verifies different
// combinations of the supported match criteria.
func (s) TestUnmarshalListener_ServerSide_Success_AllCombinations(t *testing.T) {
	tests := []struct {
		desc         string
		lis          *v3listenerpb.Listener
		wantListener ListenerUpdate
	}{
		{
			desc: "multiple destination prefixes",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						// Unspecified destination prefix.
						FilterChainMatch: &v3listenerpb.FilterChainMatch{},
						Filters:          emptyValidNetworkFilters(t),
					},
					{
						// v4 wildcard destination prefix.
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("0.0.0.0", 0)},
							SourceType:   v3listenerpb.FilterChainMatch_EXTERNAL,
						},
						Filters: emptyValidNetworkFilters(t),
					},
					{
						// v6 wildcard destination prefix.
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("::", 0)},
							SourceType:   v3listenerpb.FilterChainMatch_EXTERNAL,
						},
						Filters: emptyValidNetworkFilters(t),
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)}},
						Filters:          emptyValidNetworkFilters(t),
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("10.0.0.0", 8)}},
						Filters:          emptyValidNetworkFilters(t),
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{Filters: emptyValidNetworkFilters(t)},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{
							{
								SourceTypeArr: [3]*SourcePrefixes{{
									Entries: []*SourcePrefixEntry{{
										PortMap: map[int]*NetworkFilterChainConfig{
											0: {
												HTTPConnMgr: &HTTPConnectionManagerConfig{
													InlineRouteConfig: inlineRouteConfig,
													HTTPFilters:       makeRouterFilterList(t),
												},
											},
										}},
									},
								}},
							},
							{
								Prefix: ipNetFromCIDR("0.0.0.0/0"),
								SourceTypeArr: [3]*SourcePrefixes{
									nil,
									nil,
									{
										Entries: []*SourcePrefixEntry{{
											PortMap: map[int]*NetworkFilterChainConfig{
												0: {
													HTTPConnMgr: &HTTPConnectionManagerConfig{
														InlineRouteConfig: inlineRouteConfig,
														HTTPFilters:       makeRouterFilterList(t),
													},
												},
											}},
										},
									},
								},
							},
							{
								Prefix: ipNetFromCIDR("::/0"),
								SourceTypeArr: [3]*SourcePrefixes{
									nil,
									nil,
									{
										Entries: []*SourcePrefixEntry{{
											PortMap: map[int]*NetworkFilterChainConfig{
												0: {
													HTTPConnMgr: &HTTPConnectionManagerConfig{
														InlineRouteConfig: inlineRouteConfig,
														HTTPFilters:       makeRouterFilterList(t),
													},
												},
											}},
										},
									},
								},
							},
							{
								Prefix: ipNetFromCIDR("192.168.1.1/16"),
								SourceTypeArr: [3]*SourcePrefixes{{
									Entries: []*SourcePrefixEntry{{
										PortMap: map[int]*NetworkFilterChainConfig{
											0: {
												HTTPConnMgr: &HTTPConnectionManagerConfig{
													InlineRouteConfig: inlineRouteConfig,
													HTTPFilters:       makeRouterFilterList(t),
												},
											},
										}},
									},
								}},
							},
							{
								Prefix: ipNetFromCIDR("10.0.0.0/8"),
								SourceTypeArr: [3]*SourcePrefixes{{
									Entries: []*SourcePrefixEntry{{
										PortMap: map[int]*NetworkFilterChainConfig{
											0: {
												HTTPConnMgr: &HTTPConnectionManagerConfig{
													InlineRouteConfig: inlineRouteConfig,
													HTTPFilters:       makeRouterFilterList(t),
												},
											},
										}},
									},
								}},
							},
						},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters:       makeRouterFilterList(t),
						},
					},
				},
			},
		},
		{
			desc: "multiple source types",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourceType: v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK},
						Filters:          emptyValidNetworkFilters(t),
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							SourceType:   v3listenerpb.FilterChainMatch_EXTERNAL,
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{Filters: emptyValidNetworkFilters(t)},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{
							{
								SourceTypeArr: [3]*SourcePrefixes{
									nil,
									{
										Entries: []*SourcePrefixEntry{{
											PortMap: map[int]*NetworkFilterChainConfig{
												0: {
													HTTPConnMgr: &HTTPConnectionManagerConfig{
														InlineRouteConfig: inlineRouteConfig,
														HTTPFilters:       makeRouterFilterList(t),
													},
												},
											}},
										},
									},
								},
							},
							{
								Prefix: ipNetFromCIDR("192.168.1.1/16"),
								SourceTypeArr: [3]*SourcePrefixes{
									nil,
									nil,
									{
										Entries: []*SourcePrefixEntry{{
											PortMap: map[int]*NetworkFilterChainConfig{
												0: {
													HTTPConnMgr: &HTTPConnectionManagerConfig{
														InlineRouteConfig: inlineRouteConfig,
														HTTPFilters:       makeRouterFilterList(t),
													},
												},
											}},
										},
									},
								},
							},
						},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters:       makeRouterFilterList(t),
						},
					},
				},
			},
		},
		{
			desc: "multiple source prefixes",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("10.0.0.0", 8)}},
						Filters:          emptyValidNetworkFilters(t),
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:       []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{Filters: emptyValidNetworkFilters(t)},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{
							{
								SourceTypeArr: [3]*SourcePrefixes{{
									Entries: []*SourcePrefixEntry{{
										Prefix: ipNetFromCIDR("10.0.0.0/8"),
										PortMap: map[int]*NetworkFilterChainConfig{
											0: {
												HTTPConnMgr: &HTTPConnectionManagerConfig{
													InlineRouteConfig: inlineRouteConfig,
													HTTPFilters:       makeRouterFilterList(t),
												},
											},
										}},
									},
								}},
							},
							{
								Prefix: ipNetFromCIDR("192.168.1.1/16"),
								SourceTypeArr: [3]*SourcePrefixes{
									{
										Entries: []*SourcePrefixEntry{
											{
												Prefix: ipNetFromCIDR("192.168.1.1/16"),
												PortMap: map[int]*NetworkFilterChainConfig{
													0: {
														HTTPConnMgr: &HTTPConnectionManagerConfig{
															InlineRouteConfig: inlineRouteConfig,
															HTTPFilters:       makeRouterFilterList(t),
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
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters:       makeRouterFilterList(t),
						},
					},
				},
			},
		},
		{
			desc: "multiple source ports",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePorts: []uint32{1, 2, 3}},
						Filters:          emptyValidNetworkFilters(t),
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:       []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							SourceType:         v3listenerpb.FilterChainMatch_EXTERNAL,
							SourcePorts:        []uint32{1, 2, 3},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{Filters: emptyValidNetworkFilters(t)},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{
							{
								SourceTypeArr: [3]*SourcePrefixes{{
									Entries: []*SourcePrefixEntry{{
										PortMap: map[int]*NetworkFilterChainConfig{
											1: {
												HTTPConnMgr: &HTTPConnectionManagerConfig{
													InlineRouteConfig: inlineRouteConfig,
													HTTPFilters:       makeRouterFilterList(t),
												},
											},
											2: {
												HTTPConnMgr: &HTTPConnectionManagerConfig{
													InlineRouteConfig: inlineRouteConfig,
													HTTPFilters:       makeRouterFilterList(t),
												},
											},
											3: {
												HTTPConnMgr: &HTTPConnectionManagerConfig{
													InlineRouteConfig: inlineRouteConfig,
													HTTPFilters:       makeRouterFilterList(t),
												},
											},
										}},
									},
								}},
							},
							{
								Prefix: ipNetFromCIDR("192.168.1.1/16"),
								SourceTypeArr: [3]*SourcePrefixes{
									nil,
									nil,
									{
										Entries: []*SourcePrefixEntry{
											{
												Prefix: ipNetFromCIDR("192.168.1.1/16"),
												PortMap: map[int]*NetworkFilterChainConfig{
													1: {
														HTTPConnMgr: &HTTPConnectionManagerConfig{
															InlineRouteConfig: inlineRouteConfig,
															HTTPFilters:       makeRouterFilterList(t),
														},
													},
													2: {
														HTTPConnMgr: &HTTPConnectionManagerConfig{
															InlineRouteConfig: inlineRouteConfig,
															HTTPFilters:       makeRouterFilterList(t),
														},
													},
													3: {
														HTTPConnMgr: &HTTPConnectionManagerConfig{
															InlineRouteConfig: inlineRouteConfig,
															HTTPFilters:       makeRouterFilterList(t),
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
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters:       makeRouterFilterList(t),
						},
					},
				},
			},
		},
		{
			desc: "some chains have unsupported fields",
			lis: &v3listenerpb.Listener{
				Address: localSocketAddress,
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{},
						Filters:          emptyValidNetworkFilters(t),
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)}},
						Filters:          emptyValidNetworkFilters(t),
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:      []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("10.0.0.0", 8)},
							TransportProtocol: "raw_buffer",
						},
						Filters: emptyValidNetworkFilters(t),
					},
					{
						// This chain will be dropped in favor of the above
						// filter chain because they both have the same
						// destination prefix, but this one has an empty
						// transport protocol while the above chain has the more
						// preferred "raw_buffer".
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:       []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("10.0.0.0", 8)},
							TransportProtocol:  "",
							SourceType:         v3listenerpb.FilterChainMatch_EXTERNAL,
							SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("10.0.0.0", 16)},
						},
						Filters: emptyValidNetworkFilters(t),
					},
					{
						// This chain will be dropped for unsupported server
						// names.
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.100.1", 32)},
							ServerNames:  []string{"foo", "bar"},
						},
						Filters: emptyValidNetworkFilters(t),
					},
					{
						// This chain will be dropped for unsupported transport
						// protocol.
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:      []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.100.2", 32)},
							TransportProtocol: "not-raw-buffer",
						},
						Filters: emptyValidNetworkFilters(t),
					},
					{
						// This chain will be dropped for unsupported
						// application protocol.
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:         []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.100.3", 32)},
							ApplicationProtocols: []string{"h2"},
						},
						Filters: emptyValidNetworkFilters(t),
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{Filters: emptyValidNetworkFilters(t)},
			},
			wantListener: ListenerUpdate{
				TCPListener: &InboundListenerConfig{
					Address: "0.0.0.0",
					Port:    "9999",
					FilterChains: NetworkFilterChainMap{
						DstPrefixes: []*DestinationPrefixEntry{
							{
								SourceTypeArr: [3]*SourcePrefixes{{
									Entries: []*SourcePrefixEntry{{
										PortMap: map[int]*NetworkFilterChainConfig{
											0: {
												HTTPConnMgr: &HTTPConnectionManagerConfig{
													InlineRouteConfig: inlineRouteConfig,
													HTTPFilters:       makeRouterFilterList(t),
												},
											},
										}},
									},
								}},
							},
							{
								Prefix: ipNetFromCIDR("192.168.1.1/16"),
								SourceTypeArr: [3]*SourcePrefixes{{
									Entries: []*SourcePrefixEntry{{
										PortMap: map[int]*NetworkFilterChainConfig{
											0: {
												HTTPConnMgr: &HTTPConnectionManagerConfig{
													InlineRouteConfig: inlineRouteConfig,
													HTTPFilters:       makeRouterFilterList(t),
												},
											},
										}},
									},
								}},
							},
							{
								Prefix: ipNetFromCIDR("10.0.0.0/8"),
								SourceTypeArr: [3]*SourcePrefixes{{
									Entries: []*SourcePrefixEntry{{
										PortMap: map[int]*NetworkFilterChainConfig{
											0: {
												HTTPConnMgr: &HTTPConnectionManagerConfig{
													InlineRouteConfig: inlineRouteConfig,
													HTTPFilters:       makeRouterFilterList(t),
												},
											},
										}},
									},
								}},
							},
						},
					},
					DefaultFilterChain: &NetworkFilterChainConfig{
						HTTPConnMgr: &HTTPConnectionManagerConfig{
							InlineRouteConfig: inlineRouteConfig,
							HTTPFilters:       makeRouterFilterList(t),
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			resource := listenerProtoToAny(t, test.lis)
			_, gotListener, err := unmarshalListenerResource(resource, nil)
			if err != nil {
				t.Fatalf("unmarshalListenerResource(%s) failed with error: %v", pretty.ToJSON(resource), err)
			}
			if diff := cmp.Diff(test.wantListener, gotListener, cmpOptsIgnoreRawProto); diff != "" {
				t.Errorf("unmarshalListenerResource(%s), got unexpected update, diff (-want, +got):\n%s", pretty.ToJSON(resource), diff)
			}
		})
	}
}

func listenerProtoToAny(t *testing.T, lis *v3listenerpb.Listener) *anypb.Any {
	t.Helper()
	mLis, err := proto.Marshal(lis)
	if err != nil {
		t.Fatalf("Failed to marshal listener proto %+v: %v", lis, err)
	}
	return &anypb.Any{
		TypeUrl: version.V3ListenerURL,
		Value:   mLis,
	}
}

func ipNetFromCIDR(cidr string) *net.IPNet {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return ipnet
}

func cidrRangeFromAddressAndPrefixLen(address string, len int) *v3corepb.CidrRange {
	return &v3corepb.CidrRange{
		AddressPrefix: address,
		PrefixLen: &wrapperspb.UInt32Value{
			Value: uint32(len),
		},
	}
}
