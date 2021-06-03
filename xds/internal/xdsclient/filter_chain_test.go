// +build go1.12

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
 *
 */

package xdsclient

import (
	"fmt"
	"net"
	"strings"
	"testing"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal/version"
)

// TestNewFilterChainImpl_Failure_BadMatchFields verifies cases where we have a
// single filter chain with match criteria that contains unsupported fields.
func TestNewFilterChainImpl_Failure_BadMatchFields(t *testing.T) {
	tests := []struct {
		desc string
		lis  *v3listenerpb.Listener
	}{
		{
			desc: "unsupported destination port field",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{DestinationPort: &wrapperspb.UInt32Value{Value: 666}},
					},
				},
			},
		},
		{
			desc: "unsupported server names field",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{ServerNames: []string{"example-server"}},
					},
				},
			},
		},
		{
			desc: "unsupported transport protocol ",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{TransportProtocol: "tls"},
					},
				},
			},
		},
		{
			desc: "unsupported application protocol field",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{ApplicationProtocols: []string{"h2"}},
					},
				},
			},
		},
		{
			desc: "bad dest address prefix",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{{AddressPrefix: "a.b.c.d"}}},
					},
				},
			},
		},
		{
			desc: "bad dest prefix length",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("10.1.1.0", 50)}},
					},
				},
			},
		},
		{
			desc: "bad source address prefix",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePrefixRanges: []*v3corepb.CidrRange{{AddressPrefix: "a.b.c.d"}}},
					},
				},
			},
		},
		{
			desc: "bad source prefix length",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("10.1.1.0", 50)}},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			if fci, err := NewFilterChainManager(test.lis); err == nil {
				t.Fatalf("NewFilterChainManager() returned %v when expected to fail", fci)
			}
		})
	}
}

// TestNewFilterChainImpl_Failure_OverlappingMatchingRules verifies cases where
// there are multiple filter chains and they have overlapping match rules.
func TestNewFilterChainImpl_Failure_OverlappingMatchingRules(t *testing.T) {
	tests := []struct {
		desc string
		lis  *v3listenerpb.Listener
	}{
		{
			desc: "matching destination prefixes with no other matchers",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16), cidrRangeFromAddressAndPrefixLen("10.0.0.0", 0)},
						},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.2.2", 16)},
						},
					},
				},
			},
		},
		{
			desc: "matching source type",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourceType: v3listenerpb.FilterChainMatch_ANY},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourceType: v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourceType: v3listenerpb.FilterChainMatch_EXTERNAL},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourceType: v3listenerpb.FilterChainMatch_EXTERNAL},
					},
				},
			},
		},
		{
			desc: "matching source prefixes",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16), cidrRangeFromAddressAndPrefixLen("10.0.0.0", 0)},
						},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.2.2", 16)},
						},
					},
				},
			},
		},
		{
			desc: "matching source ports",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePorts: []uint32{1, 2, 3, 4, 5}},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePorts: []uint32{5, 6, 7}},
					},
				},
			},
		},
	}

	const wantErr = "multiple filter chains with overlapping matching rules are defined"
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			_, err := NewFilterChainManager(test.lis)
			if err == nil || !strings.Contains(err.Error(), wantErr) {
				t.Fatalf("NewFilterChainManager() returned err: %v, wantErr: %s", err, wantErr)
			}
		})
	}
}

// TestNewFilterChainImpl_Failure_BadSecurityConfig verifies cases where the
// security configuration in the filter chain is invalid.
func TestNewFilterChainImpl_Failure_BadSecurityConfig(t *testing.T) {
	tests := []struct {
		desc    string
		lis     *v3listenerpb.Listener
		wantErr string
	}{
		{
			desc:    "no filter chains",
			lis:     &v3listenerpb.Listener{},
			wantErr: "no supported filter chains and no default filter chain",
		},
		{
			desc: "unexpected transport socket name",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{Name: "unsupported-transport-socket-name"},
					},
				},
			},
			wantErr: "transport_socket field has unexpected name",
		},
		{
			desc: "unexpected transport socket URL",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{}),
							},
						},
					},
				},
			},
			wantErr: "transport_socket field has unexpected typeURL",
		},
		{
			desc: "badly marshaled transport socket",
			lis: &v3listenerpb.Listener{
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
					},
				},
			},
			wantErr: "failed to unmarshal DownstreamTlsContext in LDS response",
		},
		{
			desc: "missing CommonTlsContext",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						TransportSocket: &v3corepb.TransportSocket{
							Name: "envoy.transport_sockets.tls",
							ConfigType: &v3corepb.TransportSocket_TypedConfig{
								TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{}),
							},
						},
					},
				},
			},
			wantErr: "DownstreamTlsContext in LDS response does not contain a CommonTlsContext",
		},
		{
			desc: "unsupported validation context in transport socket",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
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
			},
			wantErr: "validation context contains unexpected type",
		},
		{
			desc: "no root certificate provider with require_client_cert",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
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
			},
			wantErr: "security configuration on the server-side does not contain root certificate provider instance name, but require_client_cert field is set",
		},
		{
			desc: "no identity certificate provider",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
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
			},
			wantErr: "security configuration on the server-side does not contain identity certificate provider instance name",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			_, err := NewFilterChainManager(test.lis)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("NewFilterChainManager() returned err: %v, wantErr: %s", err, test.wantErr)
			}
		})
	}
}

// TestNewFilterChainImpl_Success_SecurityConfig verifies cases where the
// security configuration in the filter chain contains valid data.
func TestNewFilterChainImpl_Success_SecurityConfig(t *testing.T) {
	tests := []struct {
		desc   string
		lis    *v3listenerpb.Listener
		wantFC *FilterChainManager
	}{
		{
			desc: "empty transport socket",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "filter-chain-1",
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{},
			},
			wantFC: &FilterChainManager{
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
				def: &FilterChain{},
			},
		},
		{
			desc: "no validation context",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
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
			},
			wantFC: &FilterChainManager{
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
		{
			desc: "validation context with certificate provider",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
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
					Name: "default-filter-chain-1",
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
			},
			wantFC: &FilterChainManager{
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
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			gotFC, err := NewFilterChainManager(test.lis)
			if err != nil {
				t.Fatalf("NewFilterChainManager() returned err: %v, wantErr: nil", err)
			}
			if !cmp.Equal(gotFC, test.wantFC, cmp.AllowUnexported(FilterChainManager{}, destPrefixEntry{}, sourcePrefixes{}, sourcePrefixEntry{})) {
				t.Fatalf("NewFilterChainManager() returned %+v, want: %+v", gotFC, test.wantFC)
			}
		})
	}
}

// TestNewFilterChainImpl_Success_UnsupportedMatchFields verifies cases where
// there are multiple filter chains, and one of them is valid while the other
// contains unsupported match fields. These configurations should lead to
// success at config validation time and the filter chains which contains
// unsupported match fields will be skipped at lookup time.
func TestNewFilterChainImpl_Success_UnsupportedMatchFields(t *testing.T) {
	unspecifiedEntry := &destPrefixEntry{
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
	}

	tests := []struct {
		desc   string
		lis    *v3listenerpb.Listener
		wantFC *FilterChainManager
	}{
		{
			desc: "unsupported destination port",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "good-chain",
					},
					{
						Name: "unsupported-destination-port",
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:    []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							DestinationPort: &wrapperspb.UInt32Value{Value: 666},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{},
			},
			wantFC: &FilterChainManager{
				dstPrefixMap: map[string]*destPrefixEntry{
					unspecifiedPrefixMapKey: unspecifiedEntry,
				},
				def: &FilterChain{},
			},
		},
		{
			desc: "unsupported server names",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "good-chain",
					},
					{
						Name: "unsupported-server-names",
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							ServerNames:  []string{"example-server"},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{},
			},
			wantFC: &FilterChainManager{
				dstPrefixMap: map[string]*destPrefixEntry{
					unspecifiedPrefixMapKey: unspecifiedEntry,
					"192.168.0.0/16": {
						net: ipNetFromCIDR("192.168.2.2/16"),
					},
				},
				def: &FilterChain{},
			},
		},
		{
			desc: "unsupported transport protocol",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "good-chain",
					},
					{
						Name: "unsupported-transport-protocol",
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:      []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							TransportProtocol: "tls",
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{},
			},
			wantFC: &FilterChainManager{
				dstPrefixMap: map[string]*destPrefixEntry{
					unspecifiedPrefixMapKey: unspecifiedEntry,
					"192.168.0.0/16": {
						net: ipNetFromCIDR("192.168.2.2/16"),
					},
				},
				def: &FilterChain{},
			},
		},
		{
			desc: "unsupported application protocol",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						Name: "good-chain",
					},
					{
						Name: "unsupported-application-protocol",
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:         []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							ApplicationProtocols: []string{"h2"},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{},
			},
			wantFC: &FilterChainManager{
				dstPrefixMap: map[string]*destPrefixEntry{
					unspecifiedPrefixMapKey: unspecifiedEntry,
					"192.168.0.0/16": {
						net: ipNetFromCIDR("192.168.2.2/16"),
					},
				},
				def: &FilterChain{},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			gotFC, err := NewFilterChainManager(test.lis)
			if err != nil {
				t.Fatalf("NewFilterChainManager() returned err: %v, wantErr: nil", err)
			}
			if !cmp.Equal(gotFC, test.wantFC, cmp.AllowUnexported(FilterChainManager{}, destPrefixEntry{}, sourcePrefixes{}, sourcePrefixEntry{})) {
				t.Fatalf("NewFilterChainManager() returned %+v, want: %+v", gotFC, test.wantFC)
			}
		})
	}
}

// TestNewFilterChainImpl_Success_AllCombinations verifies different
// combinations of the supported match criteria.
func TestNewFilterChainImpl_Success_AllCombinations(t *testing.T) {
	tests := []struct {
		desc   string
		lis    *v3listenerpb.Listener
		wantFC *FilterChainManager
	}{
		{
			desc: "multiple destination prefixes",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						// Unspecified destination prefix.
						FilterChainMatch: &v3listenerpb.FilterChainMatch{},
					},
					{
						// v4 wildcard destination prefix.
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("0.0.0.0", 0)},
							SourceType:   v3listenerpb.FilterChainMatch_EXTERNAL,
						},
					},
					{
						// v6 wildcard destination prefix.
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("::", 0)},
							SourceType:   v3listenerpb.FilterChainMatch_EXTERNAL,
						},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)}},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("10.0.0.0", 8)}},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{},
			},
			wantFC: &FilterChainManager{
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
					"0.0.0.0/0": {
						net: ipNetFromCIDR("0.0.0.0/0"),
						srcTypeArr: [3]*sourcePrefixes{
							nil,
							nil,
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
					"::/0": {
						net: ipNetFromCIDR("::/0"),
						srcTypeArr: [3]*sourcePrefixes{
							nil,
							nil,
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
					"192.168.0.0/16": {
						net: ipNetFromCIDR("192.168.2.2/16"),
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
					"10.0.0.0/8": {
						net: ipNetFromCIDR("10.0.0.0/8"),
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
				def: &FilterChain{},
			},
		},
		{
			desc: "multiple source types",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourceType: v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							SourceType:   v3listenerpb.FilterChainMatch_EXTERNAL,
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{},
			},
			wantFC: &FilterChainManager{
				dstPrefixMap: map[string]*destPrefixEntry{
					unspecifiedPrefixMapKey: {
						srcTypeArr: [3]*sourcePrefixes{
							nil,
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
					"192.168.0.0/16": {
						net: ipNetFromCIDR("192.168.2.2/16"),
						srcTypeArr: [3]*sourcePrefixes{
							nil,
							nil,
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
				def: &FilterChain{},
			},
		},
		{
			desc: "multiple source prefixes",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("10.0.0.0", 8)}},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:       []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{},
			},
			wantFC: &FilterChainManager{
				dstPrefixMap: map[string]*destPrefixEntry{
					unspecifiedPrefixMapKey: {
						srcTypeArr: [3]*sourcePrefixes{
							{
								srcPrefixMap: map[string]*sourcePrefixEntry{
									"10.0.0.0/8": {
										net: ipNetFromCIDR("10.0.0.0/8"),
										srcPortMap: map[int]*FilterChain{
											0: {},
										},
									},
								},
							},
						},
					},
					"192.168.0.0/16": {
						net: ipNetFromCIDR("192.168.2.2/16"),
						srcTypeArr: [3]*sourcePrefixes{
							{
								srcPrefixMap: map[string]*sourcePrefixEntry{
									"192.168.0.0/16": {
										net: ipNetFromCIDR("192.168.0.0/16"),
										srcPortMap: map[int]*FilterChain{
											0: {},
										},
									},
								},
							},
						},
					},
				},
				def: &FilterChain{},
			},
		},
		{
			desc: "multiple source ports",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePorts: []uint32{1, 2, 3}},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:       []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							SourceType:         v3listenerpb.FilterChainMatch_EXTERNAL,
							SourcePorts:        []uint32{1, 2, 3},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{},
			},
			wantFC: &FilterChainManager{
				dstPrefixMap: map[string]*destPrefixEntry{
					unspecifiedPrefixMapKey: {
						srcTypeArr: [3]*sourcePrefixes{
							{
								srcPrefixMap: map[string]*sourcePrefixEntry{
									unspecifiedPrefixMapKey: {
										srcPortMap: map[int]*FilterChain{
											1: {},
											2: {},
											3: {},
										},
									},
								},
							},
						},
					},
					"192.168.0.0/16": {
						net: ipNetFromCIDR("192.168.2.2/16"),
						srcTypeArr: [3]*sourcePrefixes{
							nil,
							nil,
							{
								srcPrefixMap: map[string]*sourcePrefixEntry{
									"192.168.0.0/16": {
										net: ipNetFromCIDR("192.168.0.0/16"),
										srcPortMap: map[int]*FilterChain{
											1: {},
											2: {},
											3: {},
										},
									},
								},
							},
						},
					},
				},
				def: &FilterChain{},
			},
		},
		{
			desc: "some chains have unsupported fields",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)}},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:      []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("10.0.0.0", 8)},
							TransportProtocol: "raw_buffer",
						},
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
					},
					{
						// This chain will be dropped for unsupported server
						// names.
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.100.1", 32)},
							ServerNames:  []string{"foo", "bar"},
						},
					},
					{
						// This chain will be dropped for unsupported transport
						// protocol.
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:      []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.100.2", 32)},
							TransportProtocol: "not-raw-buffer",
						},
					},
					{
						// This chain will be dropped for unsupported
						// application protocol.
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges:         []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.100.3", 32)},
							ApplicationProtocols: []string{"h2"},
						},
					},
				},
				DefaultFilterChain: &v3listenerpb.FilterChain{},
			},
			wantFC: &FilterChainManager{
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
					"192.168.0.0/16": {
						net: ipNetFromCIDR("192.168.2.2/16"),
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
					"10.0.0.0/8": {
						net: ipNetFromCIDR("10.0.0.0/8"),
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
					"192.168.100.1/32": {
						net:        ipNetFromCIDR("192.168.100.1/32"),
						srcTypeArr: [3]*sourcePrefixes{},
					},
					"192.168.100.2/32": {
						net:        ipNetFromCIDR("192.168.100.2/32"),
						srcTypeArr: [3]*sourcePrefixes{},
					},
					"192.168.100.3/32": {
						net:        ipNetFromCIDR("192.168.100.3/32"),
						srcTypeArr: [3]*sourcePrefixes{},
					},
				},
				def: &FilterChain{},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			gotFC, err := NewFilterChainManager(test.lis)
			if err != nil {
				t.Fatalf("NewFilterChainManager() returned err: %v, wantErr: nil", err)
			}
			if !cmp.Equal(gotFC, test.wantFC, cmp.AllowUnexported(FilterChainManager{}, destPrefixEntry{}, sourcePrefixes{}, sourcePrefixEntry{})) {
				t.Fatalf("NewFilterChainManager() returned %+v, want: %+v", gotFC, test.wantFC)
			}
		})
	}
}

func TestLookup_Failures(t *testing.T) {
	tests := []struct {
		desc    string
		lis     *v3listenerpb.Listener
		params  FilterChainLookupParams
		wantErr string
	}{
		{
			desc: "no destination prefix match",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)}},
					},
				},
			},
			params: FilterChainLookupParams{
				IsUnspecifiedListener: true,
				DestAddr:              net.IPv4(10, 1, 1, 1),
			},
			wantErr: "no matching filter chain based on destination prefix match",
		},
		{
			desc: "no source type match",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							SourceType:   v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK,
						},
					},
				},
			},
			params: FilterChainLookupParams{
				IsUnspecifiedListener: true,
				DestAddr:              net.IPv4(192, 168, 100, 1),
				SourceAddr:            net.IPv4(192, 168, 100, 2),
			},
			wantErr: "no matching filter chain based on source type match",
		},
		{
			desc: "no source prefix match",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 24)},
							SourceType:         v3listenerpb.FilterChainMatch_SAME_IP_OR_LOOPBACK,
						},
					},
				},
			},
			params: FilterChainLookupParams{
				IsUnspecifiedListener: true,
				DestAddr:              net.IPv4(192, 168, 100, 1),
				SourceAddr:            net.IPv4(192, 168, 100, 1),
			},
			wantErr: "no matching filter chain after all match criteria",
		},
		{
			desc: "multiple matching filter chains",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePorts: []uint32{1, 2, 3}},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)},
							SourcePorts:  []uint32{1},
						},
					},
				},
			},
			params: FilterChainLookupParams{
				// IsUnspecified is not set. This means that the destination
				// prefix matchers will be ignored.
				DestAddr:   net.IPv4(192, 168, 100, 1),
				SourceAddr: net.IPv4(192, 168, 100, 1),
				SourcePort: 1,
			},
			wantErr: "multiple matching filter chains",
		},
		{
			desc: "no default filter chain",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{SourcePorts: []uint32{1, 2, 3}},
					},
				},
			},
			params: FilterChainLookupParams{
				IsUnspecifiedListener: true,
				DestAddr:              net.IPv4(192, 168, 100, 1),
				SourceAddr:            net.IPv4(192, 168, 100, 1),
				SourcePort:            80,
			},
			wantErr: "no matching filter chain after all match criteria",
		},
		{
			desc: "most specific match dropped for unsupported field",
			lis: &v3listenerpb.Listener{
				FilterChains: []*v3listenerpb.FilterChain{
					{
						// This chain will be picked in the destination prefix
						// stage, but will be dropped at the server names stage.
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.100.1", 32)},
							ServerNames:  []string{"foo"},
						},
					},
					{
						FilterChainMatch: &v3listenerpb.FilterChainMatch{
							PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.100.0", 16)},
						},
					},
				},
			},
			params: FilterChainLookupParams{
				IsUnspecifiedListener: true,
				DestAddr:              net.IPv4(192, 168, 100, 1),
				SourceAddr:            net.IPv4(192, 168, 100, 1),
				SourcePort:            80,
			},
			wantErr: "no matching filter chain based on source type match",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			fci, err := NewFilterChainManager(test.lis)
			if err != nil {
				t.Fatalf("NewFilterChainManager() failed: %v", err)
			}
			fc, err := fci.Lookup(test.params)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("FilterChainManager.Lookup(%v) = (%v, %v) want (nil, %s)", test.params, fc, err, test.wantErr)
			}
		})
	}
}

func TestLookup_Successes(t *testing.T) {
	lisWithDefaultChain := &v3listenerpb.Listener{
		FilterChains: []*v3listenerpb.FilterChain{
			{
				FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)}},
				TransportSocket: &v3corepb.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{
							CommonTlsContext: &v3tlspb.CommonTlsContext{
								TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{InstanceName: "instance1"},
							},
						}),
					},
				},
			},
		},
		// A default filter chain with an empty transport socket.
		DefaultFilterChain: &v3listenerpb.FilterChain{
			TransportSocket: &v3corepb.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &v3corepb.TransportSocket_TypedConfig{
					TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{
						CommonTlsContext: &v3tlspb.CommonTlsContext{
							TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{InstanceName: "default"},
						},
					}),
				},
			},
		},
	}
	lisWithoutDefaultChain := &v3listenerpb.Listener{
		FilterChains: []*v3listenerpb.FilterChain{
			{
				TransportSocket: transportSocketWithInstanceName("unspecified-dest-and-source-prefix"),
			},
			{
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges:       []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("0.0.0.0", 0)},
					SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("0.0.0.0", 0)},
				},
				TransportSocket: transportSocketWithInstanceName("wildcard-prefixes-v4"),
			},
			{
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("::", 0)},
				},
				TransportSocket: transportSocketWithInstanceName("wildcard-source-prefix-v6"),
			},
			{
				FilterChainMatch: &v3listenerpb.FilterChainMatch{PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 16)}},
				TransportSocket:  transportSocketWithInstanceName("specific-destination-prefix-unspecified-source-type"),
			},
			{
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 24)},
					SourceType:   v3listenerpb.FilterChainMatch_EXTERNAL,
				},
				TransportSocket: transportSocketWithInstanceName("specific-destination-prefix-specific-source-type"),
			},
			{
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges:       []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 24)},
					SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.92.1", 24)},
					SourceType:         v3listenerpb.FilterChainMatch_EXTERNAL,
				},
				TransportSocket: transportSocketWithInstanceName("specific-destination-prefix-specific-source-type-specific-source-prefix"),
			},
			{
				FilterChainMatch: &v3listenerpb.FilterChainMatch{
					PrefixRanges:       []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.1.1", 24)},
					SourcePrefixRanges: []*v3corepb.CidrRange{cidrRangeFromAddressAndPrefixLen("192.168.92.1", 24)},
					SourceType:         v3listenerpb.FilterChainMatch_EXTERNAL,
					SourcePorts:        []uint32{80},
				},
				TransportSocket: transportSocketWithInstanceName("specific-destination-prefix-specific-source-type-specific-source-prefix-specific-source-port"),
			},
		},
	}

	tests := []struct {
		desc   string
		lis    *v3listenerpb.Listener
		params FilterChainLookupParams
		wantFC *FilterChain
	}{
		{
			desc: "default filter chain",
			lis:  lisWithDefaultChain,
			params: FilterChainLookupParams{
				IsUnspecifiedListener: true,
				DestAddr:              net.IPv4(10, 1, 1, 1),
			},
			wantFC: &FilterChain{SecurityCfg: &SecurityConfig{IdentityInstanceName: "default"}},
		},
		{
			desc: "unspecified destination match",
			lis:  lisWithoutDefaultChain,
			params: FilterChainLookupParams{
				IsUnspecifiedListener: true,
				DestAddr:              net.ParseIP("2001:68::db8"),
				SourceAddr:            net.IPv4(10, 1, 1, 1),
				SourcePort:            1,
			},
			wantFC: &FilterChain{SecurityCfg: &SecurityConfig{IdentityInstanceName: "unspecified-dest-and-source-prefix"}},
		},
		{
			desc: "wildcard destination match v4",
			lis:  lisWithoutDefaultChain,
			params: FilterChainLookupParams{
				IsUnspecifiedListener: true,
				DestAddr:              net.IPv4(10, 1, 1, 1),
				SourceAddr:            net.IPv4(10, 1, 1, 1),
				SourcePort:            1,
			},
			wantFC: &FilterChain{SecurityCfg: &SecurityConfig{IdentityInstanceName: "wildcard-prefixes-v4"}},
		},
		{
			desc: "wildcard source match v6",
			lis:  lisWithoutDefaultChain,
			params: FilterChainLookupParams{
				IsUnspecifiedListener: true,
				DestAddr:              net.ParseIP("2001:68::1"),
				SourceAddr:            net.ParseIP("2001:68::2"),
				SourcePort:            1,
			},
			wantFC: &FilterChain{SecurityCfg: &SecurityConfig{IdentityInstanceName: "wildcard-source-prefix-v6"}},
		},
		{
			desc: "specific destination and wildcard source type match",
			lis:  lisWithoutDefaultChain,
			params: FilterChainLookupParams{
				IsUnspecifiedListener: true,
				DestAddr:              net.IPv4(192, 168, 100, 1),
				SourceAddr:            net.IPv4(192, 168, 100, 1),
				SourcePort:            80,
			},
			wantFC: &FilterChain{SecurityCfg: &SecurityConfig{IdentityInstanceName: "specific-destination-prefix-unspecified-source-type"}},
		},
		{
			desc: "specific destination and source type match",
			lis:  lisWithoutDefaultChain,
			params: FilterChainLookupParams{
				IsUnspecifiedListener: true,
				DestAddr:              net.IPv4(192, 168, 1, 1),
				SourceAddr:            net.IPv4(10, 1, 1, 1),
				SourcePort:            80,
			},
			wantFC: &FilterChain{SecurityCfg: &SecurityConfig{IdentityInstanceName: "specific-destination-prefix-specific-source-type"}},
		},
		{
			desc: "specific destination source type and source prefix",
			lis:  lisWithoutDefaultChain,
			params: FilterChainLookupParams{
				IsUnspecifiedListener: true,
				DestAddr:              net.IPv4(192, 168, 1, 1),
				SourceAddr:            net.IPv4(192, 168, 92, 100),
				SourcePort:            70,
			},
			wantFC: &FilterChain{SecurityCfg: &SecurityConfig{IdentityInstanceName: "specific-destination-prefix-specific-source-type-specific-source-prefix"}},
		},
		{
			desc: "specific destination source type source prefix and source port",
			lis:  lisWithoutDefaultChain,
			params: FilterChainLookupParams{
				IsUnspecifiedListener: true,
				DestAddr:              net.IPv4(192, 168, 1, 1),
				SourceAddr:            net.IPv4(192, 168, 92, 100),
				SourcePort:            80,
			},
			wantFC: &FilterChain{SecurityCfg: &SecurityConfig{IdentityInstanceName: "specific-destination-prefix-specific-source-type-specific-source-prefix-specific-source-port"}},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			fci, err := NewFilterChainManager(test.lis)
			if err != nil {
				t.Fatalf("NewFilterChainManager() failed: %v", err)
			}
			gotFC, err := fci.Lookup(test.params)
			if err != nil {
				t.Fatalf("FilterChainManager.Lookup(%v) failed: %v", test.params, err)
			}
			if !cmp.Equal(gotFC, test.wantFC) {
				t.Fatalf("FilterChainManager.Lookup(%v) = %v, want %v", test.params, gotFC, test.wantFC)
			}
		})
	}
}

// The Equal() methods defined below help with using cmp.Equal() on these types
// which contain all unexported fields.

func (fci *FilterChainManager) Equal(other *FilterChainManager) bool {
	if (fci == nil) != (other == nil) {
		return false
	}
	if fci == nil {
		return true
	}
	switch {
	case !cmp.Equal(fci.dstPrefixMap, other.dstPrefixMap):
		return false
	case !cmp.Equal(fci.def, other.def):
		return false
	}
	return true
}

func (dpe *destPrefixEntry) Equal(other *destPrefixEntry) bool {
	if (dpe == nil) != (other == nil) {
		return false
	}
	if dpe == nil {
		return true
	}
	if !cmp.Equal(dpe.net, other.net) {
		return false
	}
	for i, st := range dpe.srcTypeArr {
		if !cmp.Equal(st, other.srcTypeArr[i]) {
			return false
		}
	}
	return true
}

func (sp *sourcePrefixes) Equal(other *sourcePrefixes) bool {
	if (sp == nil) != (other == nil) {
		return false
	}
	if sp == nil {
		return true
	}
	return cmp.Equal(sp.srcPrefixMap, other.srcPrefixMap)
}

func (spe *sourcePrefixEntry) Equal(other *sourcePrefixEntry) bool {
	if (spe == nil) != (other == nil) {
		return false
	}
	if spe == nil {
		return true
	}
	switch {
	case !cmp.Equal(spe.net, other.net):
		return false
	case !cmp.Equal(spe.srcPortMap, other.srcPortMap):
		return false
	}
	return true
}

// The String() methods defined below help with debugging test failures as the
// regular %v or %+v formatting directives do not expands pointer fields inside
// structs, and these types have a lot of pointers pointing to other structs.
func (fci *FilterChainManager) String() string {
	if fci == nil {
		return ""
	}

	var sb strings.Builder
	if fci.dstPrefixMap != nil {
		sb.WriteString("destination_prefix_map: map {\n")
		for k, v := range fci.dstPrefixMap {
			sb.WriteString(fmt.Sprintf("%q: %v\n", k, v))
		}
		sb.WriteString("}\n")
	}
	if fci.dstPrefixes != nil {
		sb.WriteString("destination_prefixes: [")
		for _, p := range fci.dstPrefixes {
			sb.WriteString(fmt.Sprintf("%v ", p))
		}
		sb.WriteString("]")
	}
	if fci.def != nil {
		sb.WriteString(fmt.Sprintf("default_filter_chain: %+v ", fci.def))
	}
	return sb.String()
}

func (dpe *destPrefixEntry) String() string {
	if dpe == nil {
		return ""
	}
	var sb strings.Builder
	if dpe.net != nil {
		sb.WriteString(fmt.Sprintf("destination_prefix: %s ", dpe.net.String()))
	}
	sb.WriteString("source_types_array: [")
	for _, st := range dpe.srcTypeArr {
		sb.WriteString(fmt.Sprintf("%v ", st))
	}
	sb.WriteString("]")
	return sb.String()
}

func (sp *sourcePrefixes) String() string {
	if sp == nil {
		return ""
	}
	var sb strings.Builder
	if sp.srcPrefixMap != nil {
		sb.WriteString("source_prefix_map: map {")
		for k, v := range sp.srcPrefixMap {
			sb.WriteString(fmt.Sprintf("%q: %v ", k, v))
		}
		sb.WriteString("}")
	}
	if sp.srcPrefixes != nil {
		sb.WriteString("source_prefixes: [")
		for _, p := range sp.srcPrefixes {
			sb.WriteString(fmt.Sprintf("%v ", p))
		}
		sb.WriteString("]")
	}
	return sb.String()
}

func (spe *sourcePrefixEntry) String() string {
	if spe == nil {
		return ""
	}
	var sb strings.Builder
	if spe.net != nil {
		sb.WriteString(fmt.Sprintf("source_prefix: %s ", spe.net.String()))
	}
	if spe.srcPortMap != nil {
		sb.WriteString("source_ports_map: map {")
		for k, v := range spe.srcPortMap {
			sb.WriteString(fmt.Sprintf("%d: %+v ", k, v))
		}
		sb.WriteString("}")
	}
	return sb.String()
}

func (f *FilterChain) String() string {
	if f == nil || f.SecurityCfg == nil {
		return ""
	}
	return fmt.Sprintf("security_config: %v", f.SecurityCfg)
}

func ipNetFromCIDR(cidr string) *net.IPNet {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return ipnet
}

func transportSocketWithInstanceName(name string) *v3corepb.TransportSocket {
	return &v3corepb.TransportSocket{
		Name: "envoy.transport_sockets.tls",
		ConfigType: &v3corepb.TransportSocket_TypedConfig{
			TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{
				CommonTlsContext: &v3tlspb.CommonTlsContext{
					TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{InstanceName: name},
				},
			}),
		},
	}
}

func cidrRangeFromAddressAndPrefixLen(address string, len int) *v3corepb.CidrRange {
	return &v3corepb.CidrRange{
		AddressPrefix: address,
		PrefixLen: &wrapperspb.UInt32Value{
			Value: uint32(len),
		},
	}
}
