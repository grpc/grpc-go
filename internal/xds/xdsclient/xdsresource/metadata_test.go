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

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/testutils"
)

const proxyAddressTypeURL = "type.googleapis.com/envoy.config.core.v3.Address"

func (s) TestProxyAddressConverterSuccess(t *testing.T) {
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
			name: "valid IPv4 address and port",
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
			name: "valid full IPv6 address and port",
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
			name: "valid shortened IPv6 address",
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
			name: "valid link-local IPv6 address",
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
			name: "valid IPv4-mapped IPv6 address",
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
			name: "invalid address",
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
			name: "missing socket_address",
			addr: &v3corepb.Address{
				// No SocketAddress field set.
			},
			wantErr: "no socket_address field in metadata",
		},
		{
			name: "address is not a socket address",
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
			name: "port value not set",
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
