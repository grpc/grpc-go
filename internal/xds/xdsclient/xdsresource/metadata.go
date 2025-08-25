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
	"encoding/json"
	"fmt"
	"net/netip"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/protobuf/proto"
)

func init() {
	Register("envoy.http11_proxy_transport_socket.proxy_address", proxyAddressConvertor{})
}

var (
	// metadataregistry is a map from proto type to Converter.
	metadataregistry = make(map[string]converter)
)

// MetadataValue is the interface for a converted metadata value. It is
// implemented by concrete types that hold the converted metadata.
type MetadataValue interface {
	// Type returns the type.
	Type() string
}

// converter is the interface for a metadata converter. It is implemented by
// concrete types that convert raw bytes into a MetadataValue.
type converter interface {
	// convert parses the proto serialized bytes of an Any proto or
	// google.protobuf.Struct into a MetadataValue. The bytes of an Any proto
	// are from the value field, which has proto serialized bytes of the
	// protobuf message type specified by the type_url field.
	convert([]byte) (MetadataValue, error)
}

// Register registers the converter to the map keyed on a proto type. Must be
// called at init time. Not thread safe.
func Register(protoType string, c converter) {
	metadataregistry[protoType] = c
}

// ConverterForType retrieves a converter based on key given.
func ConverterForType(typeURL string) converter {
	return metadataregistry[typeURL]
}

// JSONMetadata stores the values in a google.protobufStruct from
// FilterMetadata.
type JSONMetadata struct {
	MetadataValue
	Data json.RawMessage
}

// ProxyAddressMetadataValue holds the address parsed from the
// envoy.config.core.v3.Address proto message, as specified in gRFC A86.
type ProxyAddressMetadataValue struct {
	// Address stores the proxy address configured (A86). It will be in the form
	// of host:port. It has to be either IPv6 or IPv4.
	Address string
}

// Type returns a string representing this metadata type.
func (ProxyAddressMetadataValue) Type() string {
	return "envoy.config.core.v3.Address"
}

// ProxyAddressConvertor implements the converter for A86 (Proxy Address) metadata.
type proxyAddressConvertor struct{}

// Convert parses the bytes from the value field of an Any proto containing an
// Address proto into a ProxyAddressMetadataValue.
func (proxyAddressConvertor) convert(anyBytes []byte) (MetadataValue, error) {
	addressProto := &v3corepb.Address{}
	if err := proto.Unmarshal(anyBytes, addressProto); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource: %v", err)
	}
	socketaddress := addressProto.GetSocketAddress()
	if socketaddress == nil {
		return nil, fmt.Errorf("no socket_address field in metadata")
	}
	if _, err := netip.ParseAddr(socketaddress.GetAddress()); err != nil {
		return nil, fmt.Errorf("address field is not a valid IPv4 or IPv6 address: %q", socketaddress.GetAddress())
	}
	portvalue := socketaddress.GetPortValue()
	if portvalue == 0 {
		return nil, fmt.Errorf("port value not set in socket_address")
	}

	metadata := ProxyAddressMetadataValue{
		Address: parseAddress(socketaddress),
	}
	return metadata, nil
}
