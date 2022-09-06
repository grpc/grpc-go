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

package xdsclient

import (
	"fmt"

	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

var (
	// Compile type interface assertions.
	_ xdsresource.Type         = listenerResourceType{}
	_ xdsresource.ResourceData = &listenerResourceData{}

	// Singleton instantiation of the resource type implementation.
	listenerType = listenerResourceType{}
)

// listenerResourceType TBD.
type listenerResourceType struct {
}

// V2TypeURL is the xDS type URL of this resource type for v2 transport.
func (listenerResourceType) V2TypeURL() string {
	return "type.googleapis.com/envoy.api.v2.Listener"

}

// V3TypeURL is the xDS type URL of this resource type for v3 transport.
func (listenerResourceType) V3TypeURL() string {
	return "type.googleapis.com/envoy.config.listener.v3.Listener"
}

// TypeEnum TBD.
func (listenerResourceType) TypeEnum() xdsresource.ResourceType {
	return xdsresource.ListenerResource
}

// AllResourcesRequiredInSotW TBD.
func (listenerResourceType) AllResourcesRequiredInSotW() bool {
	return true
}

func securityConfigValidator(bc *bootstrap.Config, sc *xdsresource.SecurityConfig) error {
	if sc == nil {
		return nil
	}
	if sc.IdentityInstanceName != "" {
		if _, ok := bc.CertProviderConfigs[sc.IdentityInstanceName]; !ok {
			return fmt.Errorf("identitiy certificate provider instance name %q missing in bootstrap configuration", sc.IdentityInstanceName)
		}
	}
	if sc.RootInstanceName != "" {
		if _, ok := bc.CertProviderConfigs[sc.RootInstanceName]; !ok {
			return fmt.Errorf("root certificate provider instance name %q missing in bootstrap configuration", sc.RootInstanceName)
		}
	}
	return nil
}

func listenerResourceValidator(bc *bootstrap.Config, lis xdsresource.ListenerUpdate) error {
	if lis.InboundListenerCfg == nil || lis.InboundListenerCfg.FilterChains == nil {
		return nil
	}
	return lis.InboundListenerCfg.FilterChains.Validate(func(fc *xdsresource.FilterChain) error {
		if fc == nil {
			return nil
		}
		return securityConfigValidator(bc, fc.SecurityCfg)
	})
}

// Decode TBD.
func (listenerResourceType) Decode(opts *xdsresource.DecodeOptions, resource *anypb.Any) (*xdsresource.DecodeResult, error) {
	// TODO: Move all unmarshaling and validating code from xdsresource package
	// into here. And since we have the bootstrap config passed in to this
	// function, we can do the extra validation performed by
	// `UnmarshalOptions.UpdateValidator` right in here.
	name, listener, err := xdsresource.UnmarshalListenerResource(resource, nil, opts.Logger)
	switch {
	case name == "":
		// Name is unset only when protobuf deserialization fails.
		return nil, err
	case err != nil:
		// Protobuf deserialization succeeded, but resource validation failed.
		return &xdsresource.DecodeResult{Name: name, Resource: &listenerResourceData{Resource: xdsresource.ListenerUpdate{}}}, err
	}

	// Perform extra validation here.
	if err := listenerResourceValidator(opts.BootstrapConfig, listener); err != nil {
		return &xdsresource.DecodeResult{Name: name, Resource: &listenerResourceData{Resource: xdsresource.ListenerUpdate{}}}, err
	}

	return &xdsresource.DecodeResult{Name: name, Resource: &listenerResourceData{Resource: listener}}, nil

}

// listenerResourceData TBD.
// TODO: Move the functionality of xdsresource.ListenerUpdate (rename it to
// listenerResourceData) into this package and have it implement the
// ResourceData interface.
type listenerResourceData struct {
	xdsresource.ResourceData
	Resource xdsresource.ListenerUpdate // TODO: Why do we store this type by value everywhere. Change it to a pointer?
}

// Equal returns true if the passed in resource data is equal to that of the
// receiver.
func (l *listenerResourceData) Equal(other xdsresource.ResourceData) bool {
	if l == nil && other == nil {
		return true
	}
	if (l == nil) != (other == nil) {
		return false
	}
	return proto.Equal(l.Resource.Raw, other.Raw())

}

// ToJSON returns a JSON string representation of the resource data.
func (l *listenerResourceData) ToJSON() string {
	return pretty.ToJSON(l.Resource)
}

// Raw returns the underlying raw protobuf form of the listener resource.
func (l *listenerResourceData) Raw() *anypb.Any {
	return l.Resource.Raw
}

// WatchListener TDB.
func WatchListener(c XDSClient, resourceName string, watcher ResourceWatcher) (cancel func()) {
	return c.WatchResource(listenerType, resourceName, watcher)
}
