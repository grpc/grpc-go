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
	v3gcpauthnpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/gcp_authn/v3"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/protobuf/types/known/anypb"
)

func init() {
	if envconfig.XDSHTTPConnectEnabled {
		registerMetadataConverter("type.googleapis.com/envoy.config.core.v3.Address", proxyAddressConvertor{})
	}
	if envconfig.XDSGCPAuthenticationFilterEnabled {
		registerMetadataConverter("type.googleapis.com/envoy.extensions.filters.http.gcp_authn.v3.Audience", audienceConverter{})
	}
}

var (
	// metadataRegistry is a map from proto type to metadataConverter.
	metadataRegistry = make(map[string]metadataConverter)
)

// metadataConverter converts xds metadata entries in
// Metadata.typed_filter_metadata into an internal form with the fields relevant
// to gRPC.
type metadataConverter interface {
	// convert parses the Any proto into a concrete struct.
	convert(*anypb.Any) (any, error)
}

// registerMetadataConverter registers the converter to the map keyed on a proto
// type_url. Must be called at init time. Not thread safe.
func registerMetadataConverter(protoType string, c metadataConverter) {
	metadataRegistry[protoType] = c
}

// metadataConverterForType retrieves a converter based on key given.
func metadataConverterForType(typeURL string) metadataConverter {
	return metadataRegistry[typeURL]
}

// unregisterMetadataConverterForTesting removes a converter from the registry.
// For testing only.
func unregisterMetadataConverterForTesting(typeURL string) {
	delete(metadataRegistry, typeURL)
}

// StructMetadataValue stores the values in a google.protobuf.Struct from
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

// AudienceMetadataValue holds the audience parsed from the
// envoy.extensions.filters.http.gcp_authn.v3.Audience proto message, as
// specified in gRFC A83.
type AudienceMetadataValue struct {
	// Audience is the URL of the receiving service that performs token
	// authentication.
	Audience string
}

// audienceConverter implements the metadataConverter interface to
// handle the conversion of envoy.extensions.filters.http.gcp_authn.v3.Audience
// protobuf messages into an internal representation.
type audienceConverter struct{}

func (audienceConverter) convert(anyProto *anypb.Any) (any, error) {
	audienceProto := &v3gcpauthnpb.Audience{}
	if err := anyProto.UnmarshalTo(audienceProto); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource from Any proto: %v", err)
	}
	if audienceProto.GetUrl() == "" {
		return nil, fmt.Errorf("empty url field in audience metadata")
	}
	return AudienceMetadataValue{Audience: audienceProto.GetUrl()}, nil
}

// ValidateAndConstructMetadata processes the metadata from the xDS resource
// and returns a map of parsed metadata values.
func ValidateAndConstructMetadata(metadataProto *v3corepb.Metadata) (map[string]any, error) {
	if metadataProto == nil {
		return nil, nil
	}
	metadata := make(map[string]any)
	// First go through TypedFilterMetadata.
	for key, anyProto := range metadataProto.GetTypedFilterMetadata() {
		converter := metadataConverterForType(anyProto.GetTypeUrl())
		// Ignore types we don't have a converter for.
		if converter == nil {
			continue
		}
		val, err := converter.convert(anyProto)
		if err != nil {
			// If the converter fails, nack the whole resource.
			return nil, fmt.Errorf("metadata conversion for key %q and type %q failed: %v", key, anyProto.GetTypeUrl(), err)
		}
		metadata[key] = val
	}

	// Process FilterMetadata for any keys not already handled.
	for key, structProto := range metadataProto.GetFilterMetadata() {
		// Skip keys already added from TypedFilterMetadata.
		if metadata[key] != nil {
			continue
		}
		metadata[key] = StructMetadataValue{Data: structProto.AsMap()}
	}
	return metadata, nil
}
