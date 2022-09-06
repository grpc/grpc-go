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
	_ xdsresource.Type         = routeConfigResourceType{}
	_ xdsresource.ResourceData = &routeConfigResourceData{}

	// Singleton instantiation of the resource type implementation.
	routeConfigType = routeConfigResourceType{}
)

// routeConfigResourceType TBD.
type routeConfigResourceType struct {
}

// V2TypeURL is the xDS type URL of this resource type for v2 transport.
func (routeConfigResourceType) V2TypeURL() string {
	return "type.googleapis.com/envoy.api.v2.RouteConfiguration"

}

// V3TypeURL is the xDS type URL of this resource type for v3 transport.
func (routeConfigResourceType) V3TypeURL() string {
	return "type.googleapis.com/envoy.config.route.v3.RouteConfiguration"
}

// TypeEnum TBD.
func (routeConfigResourceType) TypeEnum() xdsresource.ResourceType {
	return xdsresource.RouteConfigResource
}

// AllResourcesRequiredInSotW TBD.
func (routeConfigResourceType) AllResourcesRequiredInSotW() bool {
	return false
}

// Decode TBD.
func (routeConfigResourceType) Decode(opts *xdsresource.DecodeOptions, resource *anypb.Any) (*xdsresource.DecodeResult, error) {
	// TODO: Move all unmarshaling and validating code from xdsresource package
	// into here. And since we have the bootstrap config passed in to this
	// function, we can do the extra validation performed by
	// `UnmarshalOptions.UpdateValidator` right in here.
	name, rc, err := xdsresource.UnmarshalRouteConfigResource(resource, opts.Logger)
	switch {
	case name == "":
		// Name is unset only when protobuf deserialization fails.
		return nil, err
	case err != nil:
		// Protobuf deserialization succeeded, but resource validation failed.
		return &xdsresource.DecodeResult{Name: name, Resource: &routeConfigResourceData{Resource: xdsresource.RouteConfigUpdate{}}}, err
	}

	return &xdsresource.DecodeResult{Name: name, Resource: &routeConfigResourceData{Resource: rc}}, nil

}

// routeConfigResourceData TBD.
// TODO: Move the functionality of xdsresource.RouteConfigUpdate (rename it to
// routeConfigResourceData) into this package and have it implement the
// ResourceData interface.
type routeConfigResourceData struct {
	xdsresource.ResourceData
	Resource xdsresource.RouteConfigUpdate // TODO: Why do we store this type by value everywhere. Change it to a pointer?
}

// Equal returns true if the passed in resource data is equal to that of the
// receiver.
func (r *routeConfigResourceData) Equal(other xdsresource.ResourceData) bool {
	if r == nil && other == nil {
		return true
	}
	if (r == nil) != (other == nil) {
		return false
	}
	return proto.Equal(r.Resource.Raw, other.Raw())

}

// ToJSON returns a JSON string representation of the resource data.
func (r *routeConfigResourceData) ToJSON() string {
	return pretty.ToJSON(r.Resource)
}

// Raw returns the underlying raw protobuf form of the listener resource.
func (r *routeConfigResourceData) Raw() *anypb.Any {
	return r.Resource.Raw
}

// WatchRouteConfig TDB.
func WatchRouteConfig(c XDSClient, resourceName string, watcher ResourceWatcher) (cancel func()) {
	return c.WatchResource(routeConfigType, resourceName, watcher)
}
