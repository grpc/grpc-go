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
	"fmt"
	"net/netip"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/protobuf/types/known/anypb"
)

func init() {
	registerMetadataConverter("type.googleapis.com/envoy.config.core.v3.Address", proxyAddressConvertor{})
}

var (
	// metdataRegistry is a map from proto type to Converter.
	metdataRegistry = make(map[string]metadataConverter)
)

// metadataConverter is the interface for a metadata converter. It is implemented by
// concrete types that convert raw bytes into a MetadataValue.
type metadataConverter interface {
	// convert parses the proto serialized bytes of an Any proto or
	// google.protobuf.Struct into a MetadataValue. The bytes of an Any proto
	// are from the value field, which has proto serialized bytes of the
	// protobuf message type specified by the type_url field.
	convert(*anypb.Any) (any, error)
}

// registerMetadataConverter registers the converter to the map keyed on a proto
// type. Must be called at init time. Not thread safe.
func registerMetadataConverter(protoType string, c metadataConverter) {
	metdataRegistry[protoType] = c
}

// metadataConverterForType retrieves a converter based on key given.
func metadataConverterForType(typeURL string) metadataConverter {
	return metdataRegistry[typeURL]
}

// JSONMetadataValue stores the values in a google.protobuf.Struct from
// FilterMetadata.
type StructMetadataValue struct {
	// Data stores the parsed JSON representation of a google.protobuf.Struct.
	Data map[string]any
}

// ProxyAddressMetadataValue holds the address parsed from the
// envoy.config.core.v3.Address proto message, as specified in gRFC A86.
type ProxyAddressMetadataValue struct {
	// Address stores the proxy address configured (A86). It will be in the form
	// of host:port. It has to be either IPv6 or IPv4.
	Address string
}

// proxyAddressConvertor implements the metadataConverter interface to handle
// the conversion of envoy.config.core.v3.Address protobuf messages into an
// internal representation.
type proxyAddressConvertor struct{}

func (proxyAddressConvertor) convert(anyProto *anypb.Any) (any, error) {
	addressProto := &v3corepb.Address{}
	if err := anyProto.UnmarshalTo(addressProto); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource from Any proto: %v", err)
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
	return ProxyAddressMetadataValue{Address: parseAddress(socketaddress)}, nil
}
