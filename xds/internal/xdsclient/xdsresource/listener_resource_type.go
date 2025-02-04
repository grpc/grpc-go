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
	"fmt"

	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
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
	_ Type = listenerResourceType{}

	// Singleton instantiation of the resource type implementation.
	listenerType = listenerResourceType{
		resourceTypeState: resourceTypeState{
			typeURL:                    version.V3ListenerURL,
			typeName:                   ListenerResourceTypeName,
			allResourcesRequiredInSotW: true,
		},
	}
)

// listenerResourceType provides the resource-type specific functionality for a
// Listener resource.
//
// Implements the Type interface.
type listenerResourceType struct {
	resourceTypeState
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
func (listenerResourceType) Decode(opts *DecodeOptions, resource *anypb.Any) (*DecodeResult, error) {
	name, listener, err := unmarshalListenerResource(resource)
	switch {
	case name == "":
		// Name is unset only when protobuf deserialization fails.
		return nil, err
	case err != nil:
		// Protobuf deserialization succeeded, but resource validation failed.
		return &DecodeResult{Name: name, Resource: &ListenerResourceData{Resource: ListenerUpdate{}}}, err
	}

	// Perform extra validation here.
	if err := listenerValidator(opts.BootstrapConfig, listener); err != nil {
		return &DecodeResult{Name: name, Resource: &ListenerResourceData{Resource: ListenerUpdate{}}}, err
	}

	return &DecodeResult{Name: name, Resource: &ListenerResourceData{Resource: listener}}, nil

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

// ListenerWatcher wraps the callbacks to be invoked for different
// events corresponding to the listener resource being watched. gRFC A88
// contains an exhaustive list of what method is invoked under what conditions.
type ListenerWatcher interface {
	// OnResourceChanged is invoked to notify the watcher of a new version of
	// the resource received from the xDS server or an error indicating the
	// reason why the resource cannot be obtained.
	//
	// Upon receiving this, in case of an error, the watcher should
	// stop using any previously seen resource. xDS client will remove the
	// resource from its cache.
	OnResourceChanged(*ResourceDataOrError, OnDoneFunc)

	// OnAmbientError is invoked if resource is already cached under different
	// error conditions.
	//
	// Upon receiving this, the watcher should not stop using the previously
	// seen resource. xDS client will not remove the resource from its cache.
	OnAmbientError(error, OnDoneFunc)
}

type delegatingListenerWatcher struct {
	watcher ListenerWatcher
}

func (d *delegatingListenerWatcher) OnResourceChanged(update ResourceDataOrError, onDone OnDoneFunc) {
	if update.Err != nil {
		d.watcher.OnResourceChanged(&ResourceDataOrError{Err: update.Err}, onDone)
		return
	}
	l := update.Data.(*ListenerResourceData)
	d.watcher.OnResourceChanged(&ResourceDataOrError{Data: l}, onDone)
}

func (d *delegatingListenerWatcher) OnAmbientError(err error, onDone OnDoneFunc) {
	d.watcher.OnAmbientError(err, onDone)
}

// WatchListener uses xDS to discover the configuration associated with the
// provided listener resource name.
func WatchListener(p Producer, name string, w ListenerWatcher) (cancel func()) {
	delegator := &delegatingListenerWatcher{watcher: w}
	return p.WatchResource(listenerType, name, delegator)
}
