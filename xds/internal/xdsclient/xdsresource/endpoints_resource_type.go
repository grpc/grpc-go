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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

var (
	// Compile time interface checks.
	_ Type         = endpointsResourceType{}
	_ ResourceData = &EndpointsResourceData{}

	// Singleton instantiation of the resource type implementation.
	endpointsType = endpointsResourceType{
		resourceTypeState: resourceTypeState{
			v2TypeURL:                  "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment",
			v3TypeURL:                  "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment",
			typeEnum:                   EndpointsResource,
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
func (endpointsResourceType) Decode(opts *DecodeOptions, resource *anypb.Any) (*DecodeResult, error) {
	name, rc, err := UnmarshalEndpointsResource(resource, opts.Logger)
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

// Equal returns true if other is equal to r.
func (e *EndpointsResourceData) Equal(other ResourceData) bool {
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

// EndpointsResourceWatcher wraps the callbacks invoked by the xDS client
// implementation for different events corresponding to the listener resource
// being watched.
type EndpointsResourceWatcher interface {
	// OnResourceChanged is invoked when an update for the resource being
	// watched is received from the management server. The ResourceData
	// parameter needs to be type asserted to the appropriate type for the
	// resource being watched.
	OnResourceChanged(*EndpointsResourceData)

	// OnError is invoked under different error conditions including but not
	// limited to the following:
	//	- authority mentioned in the resource is not found
	//	- resource name parsing error
	//	- resource deserialization error
	//	- resource validation error
	//	- ADS stream failure
	//	- connection failure
	OnError(error)

	// OnResourceDoesNotExist is invoked for a specific error condition where
	// the requested resource is not found on the xDS management server.
	OnResourceDoesNotExist()
}

type delegatingEndpointsWatcher struct {
	watcher EndpointsResourceWatcher
}

func (d *delegatingEndpointsWatcher) OnGenericResourceChanged(data ResourceData) {
	l := data.(*EndpointsResourceData)
	d.watcher.OnResourceChanged(l)
}

func (d *delegatingEndpointsWatcher) OnError(err error) {
	d.watcher.OnError(err)
}

func (d *delegatingEndpointsWatcher) OnResourceDoesNotExist() {
	d.watcher.OnResourceDoesNotExist()
}

// WatchEndpoints uses xDS to discover the configuration associated with the
// provided endpoints resource name.
//
// This is a convenience wrapper around the WatchResource() API and callers are
// encouraged to use this over the latter.
func WatchEndpoints(c XDSClient, resourceName string, watcher EndpointsResourceWatcher) (cancel func()) {
	delegator := &delegatingEndpointsWatcher{watcher: watcher}
	return c.WatchResource(endpointsType, resourceName, delegator)
}
