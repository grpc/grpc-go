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
 *
 */

package testutils

import (
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/xds/internal/clients/xdsclient"
	"google.golang.org/grpc/xds/internal/clients/xdsclient/internal/xdsresource"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	// ListenerResourceTypeName represents the transport agnostic name for the
	// listener resource.
	ListenerResourceTypeName = "ListenerResource"
)

type listenerDecoder struct{}

var (
	// Singleton instantiation of the resource type implementation.
	listenerType = xdsclient.ResourceType{
		TypeURL:                    xdsresource.V3ListenerURL,
		TypeName:                   ListenerResourceTypeName,
		AllResourcesRequiredInSotW: true,
		Decoder:                    listenerDecoder{},
	}
)

func securityConfigValidator(sc *SecurityConfig) error {
	return nil
}

func listenerValidator(lis ListenerUpdate) error {
	if lis.InboundListenerCfg == nil || lis.InboundListenerCfg.FilterChains == nil {
		return nil
	}
	return lis.InboundListenerCfg.FilterChains.Validate(func(fc *FilterChain) error {
		if fc == nil {
			return nil
		}
		return securityConfigValidator(fc.SecurityCfg)
	})
}

// Decode deserializes and validates an xDS resource serialized inside the
// provided `Any` proto, as received from the xDS management server.
func (listenerDecoder) Decode(resource any, opts xdsclient.DecodeOptions) (*xdsclient.DecodeResult, error) {
	name, listener, err := unmarshalListenerResource(resource.(*anypb.Any))
	switch {
	case name == "":
		// Name is unset only when protobuf deserialization fails.
		return nil, err
	case err != nil:
		// Protobuf deserialization succeeded, but resource validation failed.
		return &xdsclient.DecodeResult{Name: name, Resource: &ListenerResourceData{Resource: ListenerUpdate{}}}, err
	}

	// Perform extra validation here.
	if err := listenerValidator(listener); err != nil {
		return &xdsclient.DecodeResult{Name: name, Resource: &ListenerResourceData{Resource: ListenerUpdate{}}}, err
	}

	return &xdsclient.DecodeResult{Name: name, Resource: &ListenerResourceData{Resource: listener}}, nil

}

// ListenerResourceData wraps the configuration of a Listener resource as
// received from the management server.
//
// Implements the ResourceData interface.
type ListenerResourceData struct {
	xdsclient.ResourceData

	// TODO: We have always stored update structs by value. See if this can be
	// switched to a pointer?
	Resource ListenerUpdate
}

// RawEqual returns true if other is equal to l.
func (l *ListenerResourceData) RawEqual(other xdsclient.ResourceData) bool {
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
