/*
 *
 * Copyright 2025 gRPC authors.
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

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/protobuf/types/known/anypb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3gcpauthnpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/gcp_authn/v3"
)

const (
	proxyAddressTypeURL = "type.googleapis.com/envoy.config.core.v3.Address"
	audienceTypeURL     = "type.googleapis.com/envoy.extensions.filters.http.gcp_authn.v3.Audience"
)

func setupProxyAddressConverter(t *testing.T) {
	registerMetadataConverter(proxyAddressTypeURL, proxyAddressConvertor{})
	t.Cleanup(func() {
		unregisterMetadataConverterForTesting(proxyAddressTypeURL)
	})
}

func setupAudienceConverter(t *testing.T) {
	registerMetadataConverter(audienceTypeURL, audienceConverter{})
	t.Cleanup(func() {
		unregisterMetadataConverterForTesting(audienceTypeURL)
	})
}

func (s) TestProxyAddressConverterSuccess(t *testing.T) {
	setupProxyAddressConverter(t)
	converter := metadataConverterForType(proxyAddressTypeURL)
	if converter == nil {
		t.Fatalf("Converter for %q not found in registry", proxyAddressTypeURL)
	}
	tests := []struct {
		name string
		addr *v3corepb.Address
		want ProxyAddressMetadataValue
	}{
		{
			name: "valid_IPv4_address_and_port",
			addr: &v3corepb.Address{
				Address: &v3corepb.Address_SocketAddress{
					SocketAddress: &v3corepb.SocketAddress{
						Address: "192.168.1.1",
						PortSpecifier: &v3corepb.SocketAddress_PortValue{
							PortValue: 8080,
						},
					},
				},
			},
			want: ProxyAddressMetadataValue{
				Address: "192.168.1.1:8080",
			},
		},
		{
			name: "valid_full_IPv6_address_and_port",
			addr: &v3corepb.Address{
				Address: &v3corepb.Address_SocketAddress{
					SocketAddress: &v3corepb.SocketAddress{
						Address: "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
						PortSpecifier: &v3corepb.SocketAddress_PortValue{
							PortValue: 9090,
						},
					},
				},
			},
			want: ProxyAddressMetadataValue{
				Address: "[2001:0db8:85a3:0000:0000:8a2e:0370:7334]:9090",
			},
		},
		{
			name: "valid_shortened_IPv6_address",
			addr: &v3corepb.Address{
				Address: &v3corepb.Address_SocketAddress{
					SocketAddress: &v3corepb.SocketAddress{
						Address: "2001:db8::1",
						PortSpecifier: &v3corepb.SocketAddress_PortValue{
							PortValue: 9090,
						},
					},
				},
			},
			want: ProxyAddressMetadataValue{
				Address: "[2001:db8::1]:9090",
			},
		},
		{
			name: "valid_link-local_IPv6_address",
			addr: &v3corepb.Address{
				Address: &v3corepb.Address_SocketAddress{
					SocketAddress: &v3corepb.SocketAddress{
						Address: "fe80::1ff:fe23:4567:890a",
						PortSpecifier: &v3corepb.SocketAddress_PortValue{
							PortValue: 8888,
						},
					},
				},
			},
			want: ProxyAddressMetadataValue{
				Address: "[fe80::1ff:fe23:4567:890a]:8888",
			},
		},
		{
			name: "valid_IPv4-mapped_IPv6_address",
			addr: &v3corepb.Address{
				Address: &v3corepb.Address_SocketAddress{
					SocketAddress: &v3corepb.SocketAddress{
						Address: "::ffff:192.0.2.128",
						PortSpecifier: &v3corepb.SocketAddress_PortValue{
							PortValue: 1234,
						},
					},
				},
			},
			want: ProxyAddressMetadataValue{
				Address: "[::ffff:192.0.2.128]:1234",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			anyProto := testutils.MarshalAny(t, tt.addr)
			got, err := converter.convert(anyProto)
			if err != nil {
				t.Fatalf("convert() failed with error: %v", err)
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(ProxyAddressMetadataValue{})); diff != "" {
				t.Errorf("convert() returned unexpected value (-want +got):\n%s", diff)
			}
		})
	}
}

func (s) TestProxyAddressConverterFailure(t *testing.T) {
	setupProxyAddressConverter(t)
	converter := metadataConverterForType(proxyAddressTypeURL)
	if converter == nil {
		t.Fatalf("Converter for %q not found in registry", proxyAddressTypeURL)
	}
	tests := []struct {
		name    string
		addr    *v3corepb.Address
		wantErr string
	}{
		{
			name: "invalid_address",
			addr: &v3corepb.Address{
				Address: &v3corepb.Address_SocketAddress{
					SocketAddress: &v3corepb.SocketAddress{
						Address: "invalid-ip",
					},
				},
			},
			wantErr: "address field is not a valid IPv4 or IPv6 address: \"invalid-ip\"",
		},
		{
			name: "missing_socket_address",
			addr: &v3corepb.Address{
				// No SocketAddress field set.
			},
			wantErr: "no socket_address field in metadata",
		},
		{
			name: "address_is_not_a_socket_address",
			addr: &v3corepb.Address{
				Address: &v3corepb.Address_EnvoyInternalAddress{
					EnvoyInternalAddress: &v3corepb.EnvoyInternalAddress{
						AddressNameSpecifier: &v3corepb.EnvoyInternalAddress_ServerListenerName{
							ServerListenerName: "some-internal-listener",
						},
					},
				},
			},
			wantErr: "no socket_address field in metadata",
		},
		{
			name: "port_value_not_set",
			addr: &v3corepb.Address{
				Address: &v3corepb.Address_SocketAddress{
					SocketAddress: &v3corepb.SocketAddress{
						Address:       "127.0.0.1",
						PortSpecifier: nil,
					},
				},
			},
			wantErr: "port value not set in socket_address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			anyProto := testutils.MarshalAny(t, tt.addr)
			_, err := converter.convert(anyProto)
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("convert() got error = %v, wantErr = %q", err, tt.wantErr)
			}
		})
	}
}

func (s) TestAudienceConverterSuccess(t *testing.T) {
	setupAudienceConverter(t)
	converter := metadataConverterForType(audienceTypeURL)
	if converter == nil {
		t.Fatalf("Converter for %q not found in registry", audienceTypeURL)
	}
	tests := []struct {
		name     string
		audience *anypb.Any
		want     AudienceMetadataValue
		wantErr  string
	}{
		{
			name:     "valid_audience",
			audience: testutils.MarshalAny(t, &v3gcpauthnpb.Audience{Url: "https://example.com"}),
			want:     AudienceMetadataValue{Audience: "https://example.com"},
		},
		{
			name:     "empty_audience",
			audience: testutils.MarshalAny(t, &v3gcpauthnpb.Audience{Url: ""}),
			want:     AudienceMetadataValue{Audience: ""},
			wantErr:  "empty url field in audience metadata",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := converter.convert(tt.audience)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("convert() got error = %v, wantErr = %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("convert() failed with error: %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("convert(%s) returned unexpected diff (-want +got):\n%s", tt.audience.GetTypeUrl(), diff)
			}
		})
	}
}
