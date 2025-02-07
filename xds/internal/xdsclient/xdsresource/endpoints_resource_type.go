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
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	// EndpointsResourceTypeName represents the transport agnostic name for the
	// endpoint resource.
	EndpointsResourceTypeName = "EndpointsResource"
)

var (
	// Compile time interface checks.
	_ Type = endpointsResourceType{}

	// Singleton instantiation of the resource type implementation.
	endpointsType = endpointsResourceType{
		resourceTypeState: resourceTypeState{
			typeURL:                    version.V3EndpointsURL,
			typeName:                   "EndpointsResource",
			allResourcesRequiredInSotW: false,
		},
	}
)

// endpointsResourceType provides the resource-type specific functionality for a
// ClusterLoadAssignment (or Endpoints) resource.
//
// Implements the Type interface.
type endpointsResourceType struct {
	resourceTypeState
}

// Decode deserializes and validates an xDS resource serialized inside the
// provided `Any` proto, as received from the xDS management server.
func (endpointsResourceType) Decode(_ *DecodeOptions, resource *anypb.Any) (*DecodeResult, error) {
	name, rc, err := unmarshalEndpointsResource(resource)
	switch {
	case name == "":
		// Name is unset only when protobuf deserialization fails.
		return nil, err
	case err != nil:
		// Protobuf deserialization succeeded, but resource validation failed.
		return &DecodeResult{Name: name, Resource: &EndpointsResourceData{Resource: EndpointsUpdate{}}}, err
	}

	return &DecodeResult{Name: name, Resource: &EndpointsResourceData{Resource: rc}}, nil

}

// EndpointsResourceData wraps the configuration of an Endpoints resource as
// received from the management server.
//
// Implements the ResourceData interface.
type EndpointsResourceData struct {
	ResourceData

	// TODO: We have always stored update structs by value. See if this can be
	// switched to a pointer?
	Resource EndpointsUpdate
}

// RawEqual returns true if other is equal to r.
func (e *EndpointsResourceData) RawEqual(other ResourceData) bool {
	if e == nil && other == nil {
		return true
	}
	if (e == nil) != (other == nil) {
		return false
	}
	return proto.Equal(e.Resource.Raw, other.Raw())

}

// ToJSON returns a JSON string representation of the resource data.
func (e *EndpointsResourceData) ToJSON() string {
	return pretty.ToJSON(e.Resource)
}

// Raw returns the underlying raw protobuf form of the listener resource.
func (e *EndpointsResourceData) Raw() *anypb.Any {
	return e.Resource.Raw
}

// EndpointsWatcher wraps the callbacks to be invoked for different
// events corresponding to the endpoints resource being watched. gRFC A88
// contains an exhaustive list of what method is invoked under what conditions.
type EndpointsWatcher interface {
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

type delegatingEndpointsWatcher struct {
	watcher EndpointsWatcher
}

func (d *delegatingEndpointsWatcher) OnResourceChanged(update ResourceDataOrError, onDone OnDoneFunc) {
	if update.Err != nil {
		d.watcher.OnResourceChanged(&ResourceDataOrError{Err: update.Err}, onDone)
		return
	}
	e := update.Data.(*EndpointsResourceData)
	d.watcher.OnResourceChanged(&ResourceDataOrError{Data: e}, onDone)
}

func (d *delegatingEndpointsWatcher) OnAmbientError(err error, onDone OnDoneFunc) {
	d.watcher.OnAmbientError(err, onDone)
}

// WatchEndpoints uses xDS to discover the configuration associated with the
// provided endpoints resource name.
func WatchEndpoints(p Producer, name string, w EndpointsWatcher) (cancel func()) {
	delegator := &delegatingEndpointsWatcher{watcher: w}
	return p.WatchResource(endpointsType, name, delegator)
}
