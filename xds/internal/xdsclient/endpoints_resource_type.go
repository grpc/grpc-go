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
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

var (
	// Compile type interface assertions.
	_ xdsresource.Type         = endpointsResourceType{}
	_ xdsresource.ResourceData = &endpointsResourceData{}

	// Singleton instantiation of the resource type implementation.
	endpointsType = endpointsResourceType{}
)

// endpointsResourceType TBD.
type endpointsResourceType struct {
}

// V2TypeURL is the xDS type URL of this resource type for v2 transport.
func (endpointsResourceType) V2TypeURL() string {
	return "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment"

}

// V3TypeURL is the xDS type URL of this resource type for v3 transport.
func (endpointsResourceType) V3TypeURL() string {
	return "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment"
}

// TypeEnum TBD.
func (endpointsResourceType) TypeEnum() xdsresource.ResourceType {
	return xdsresource.EndpointsResource
}

// AllResourcesRequiredInSotW TBD.
func (endpointsResourceType) AllResourcesRequiredInSotW() bool {
	return false
}

// Decode TBD.
func (endpointsResourceType) Decode(opts *xdsresource.DecodeOptions, resource *anypb.Any) (*xdsresource.DecodeResult, error) {
	// TODO: Move all unmarshaling and validating code from xdsresource package
	// into here. And since we have the bootstrap config passed in to this
	// function, we can do the extra validation performed by
	// `UnmarshalOptions.UpdateValidator` right in here.
	name, rc, err := xdsresource.UnmarshalEndpointsResource(resource, opts.Logger)
	switch {
	case name == "":
		// Name is unset only when protobuf deserialization fails.
		return nil, err
	case err != nil:
		// Protobuf deserialization succeeded, but resource validation failed.
		return &xdsresource.DecodeResult{Name: name, Resource: &endpointsResourceData{Resource: xdsresource.EndpointsUpdate{}}}, err
	}

	return &xdsresource.DecodeResult{Name: name, Resource: &endpointsResourceData{Resource: rc}}, nil

}

// endpointsResourceData TBD.
// TODO: Move the functionality of xdsresource.EndpointsUpdate (rename it to
// endpointsResourceData) into this package and have it implement the
// ResourceData interface.
type endpointsResourceData struct {
	xdsresource.ResourceData
	Resource xdsresource.EndpointsUpdate // TODO: Why do we store this type by value everywhere. Change it to a pointer?
}

// Equal returns true if the passed in resource data is equal to that of the
// receiver.
func (e *endpointsResourceData) Equal(other xdsresource.ResourceData) bool {
	if e == nil && other == nil {
		return true
	}
	if (e == nil) != (other == nil) {
		return false
	}
	return proto.Equal(e.Resource.Raw, other.Raw())

}

// ToJSON returns a JSON string representation of the resource data.
func (e *endpointsResourceData) ToJSON() string {
	return pretty.ToJSON(e.Resource)
}

// Raw returns the underlying raw protobuf form of the listener resource.
func (e *endpointsResourceData) Raw() *anypb.Any {
	return e.Resource.Raw
}

// WatchEndpoints TDB.
func WatchEndpoints(c XDSClient, resourceName string, watcher ResourceWatcher) (cancel func()) {
	logger.Infof("easwars: in WatchEndpoints for resource: %v", resourceName)
	return c.WatchResource(endpointsType, resourceName, watcher)
}
