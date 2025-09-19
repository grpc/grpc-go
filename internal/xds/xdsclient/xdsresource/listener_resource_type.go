/*
 *
 * Copyright 2022 gRPC authors.
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
	"bytes"
	"fmt"

	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/internal/xds/bootstrap"
	xdsclient "google.golang.org/grpc/internal/xds/clients/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	// ListenerResourceTypeName represents the transport agnostic name for the
	// listener resource.
	ListenerResourceTypeName = "ListenerResource"
)

var (
	// Compile time interface checks.
	_ xdsclient.Decoder      = listenerResourceType{}
	_ xdsclient.ResourceData = (*ListenerResourceData)(nil)

	// ListenerResource is a singleton instance of xdsclient.ResourceType that
	// defines the configuration for the Listener resource.
	ListenerResource = xdsclient.ResourceType{
		TypeURL:                    version.V3ListenerURL,
		TypeName:                   ListenerResourceTypeName,
		AllResourcesRequiredInSotW: true,
	}
)

// listenerResourceType provides the resource-type specific functionality for a
// Listener resource.
//
// Implements the Type interface.
type listenerResourceType struct {
	resourceTypeState
	BootstrapConfig *bootstrap.Config
	ServerConfigMap map[xdsclient.ServerConfig]*bootstrap.ServerConfig
}

func securityConfigValidator(bc *bootstrap.Config, sc *SecurityConfig) error {
	if sc == nil {
		return nil
	}
	if sc.IdentityInstanceName != "" {
		if _, ok := bc.CertProviderConfigs()[sc.IdentityInstanceName]; !ok {
			return fmt.Errorf("identity certificate provider instance name %q missing in bootstrap configuration", sc.IdentityInstanceName)
		}
	}
	if sc.RootInstanceName != "" {
		if _, ok := bc.CertProviderConfigs()[sc.RootInstanceName]; !ok {
			return fmt.Errorf("root certificate provider instance name %q missing in bootstrap configuration", sc.RootInstanceName)
		}
	}
	return nil
}

func listenerValidator(bc *bootstrap.Config, lis ListenerUpdate) error {
	if lis.InboundListenerCfg == nil || lis.InboundListenerCfg.FilterChains == nil {
		return nil
	}
	return lis.InboundListenerCfg.FilterChains.Validate(func(fc *FilterChain) error {
		if fc == nil {
			return nil
		}
		return securityConfigValidator(bc, fc.SecurityCfg)
	})
}

// Decode deserializes and validates an xDS resource serialized inside the
// provided `Any` proto, as received from the xDS management server.
func (lt listenerResourceType) Decode(resource xdsclient.AnyProto, gOpts xdsclient.DecodeOptions) (*xdsclient.DecodeResult, error) {
	a := &anypb.Any{
		TypeUrl: resource.TypeURL,
		Value:   resource.Value,
	}

	// Map generic decode options to internal options:
	internalOpts := &DecodeOptions{BootstrapConfig: lt.BootstrapConfig}
	if gOpts.ServerConfig != nil && lt.ServerConfigMap != nil {
		if sc, ok := lt.ServerConfigMap[*gOpts.ServerConfig]; ok {
			internalOpts.ServerConfig = sc
		}
	}
	name, listener, err := unmarshalListenerResource(a)
	switch {
	case name == "":
		// Name is unset only when protobuf deserialization fails.
		return nil, err
	case err != nil:
		// Protobuf deserialization succeeded, but resource validation failed.
		return &xdsclient.DecodeResult{
			Name:     name,
			Resource: &ListenerResourceData{Resource: ListenerUpdate{}},
		}, err
	}

	// Perform extra validation here.
	if err := listenerValidator(internalOpts.BootstrapConfig, listener); err != nil {
		return &xdsclient.DecodeResult{Name: name, Resource: &ListenerResourceData{Resource: ListenerUpdate{}}}, err
	}

	return &xdsclient.DecodeResult{Name: name, Resource: &ListenerResourceData{Resource: listener}}, nil
}

// ListenerResourceData wraps the configuration of a Listener resource as
// received from the management server.
//
// Implements the ResourceData interface.
type ListenerResourceData struct {
	ResourceData

	// TODO: We have always stored update structs by value. See if this can be
	// switched to a pointer?
	Resource ListenerUpdate
}

// RawEqual returns true if other is equal to l.
func (l *ListenerResourceData) RawEqual(other ResourceData) bool {
	if l == nil && other == nil {
		return true
	}
	if (l == nil) != (other == nil) {
		return false
	}
	return proto.Equal(l.Resource.Raw, other.Raw())
}

// ToJSON returns a JSON string representation of the resource data.
func (l *ListenerResourceData) ToJSON() string {
	return pretty.ToJSON(l.Resource)
}

// Raw returns the underlying raw protobuf form of the listener resource.
func (l *ListenerResourceData) Raw() *anypb.Any {
	return l.Resource.Raw
}

// Equal returns true if other xdsclient.ResourceData is equal to l.
func (l *ListenerResourceData) Equal(other xdsclient.ResourceData) bool {
	if l == nil && other == nil {
		return true
	}
	if (l == nil) != (other == nil) {
		return false
	}
	if otherLRD, ok := other.(*ListenerResourceData); ok {
		return l.RawEqual(otherLRD)
	}
	return bytes.Equal(l.Bytes(), other.Bytes())
}

// Bytes returns the underlying raw bytes of the listener resource.
func (l *ListenerResourceData) Bytes() []byte {
	raw := l.Raw()
	if raw == nil {
		return nil
	}
	return raw.Value
}

// WatchListener uses xDS to discover the configuration associated with the
// provided listener resource name.
func WatchListener(p Producer, name string, w xdsclient.ResourceWatcher) (cancel func()) {
	return p.WatchResource(ListenerResource, name, w)
}

// NewListenerResourceTypeDecoder returns an xdsclient.Decoder that has access to
// bootstrap config and server config mapping for decoding.
func NewListenerResourceTypeDecoder(bc *bootstrap.Config, serverConfigMap map[xdsclient.ServerConfig]*bootstrap.ServerConfig) xdsclient.Decoder {
	return &listenerResourceType{
		resourceTypeState: resourceTypeState{
			typeURL:                    version.V3ListenerURL,
			typeName:                   ListenerResourceTypeName,
			allResourcesRequiredInSotW: true,
		},
		BootstrapConfig: bc,
		ServerConfigMap: serverConfigMap,
	}
}
