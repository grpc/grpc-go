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
	_ Type         = routeConfigResourceType{}
	_ ResourceData = &RouteConfigResourceData{}

	// Singleton instantiation of the resource type implementation.
	routeConfigType = routeConfigResourceType{}
)

// routeConfigResourceType provides the resource-type specific functionality for
// a RouteConfiguration resource.
//
// Implements the Type interface.
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

// TypeEnum identifies resources in a transport protocol agnostic way.
func (routeConfigResourceType) TypeEnum() ResourceType {
	return RouteConfigResource
}

// AllResourcesRequiredInSotW indicates whether this resource type requires that
// all resources be present in every SotW response from the server. This is
// false for a RouteConfiguration resource.
func (routeConfigResourceType) AllResourcesRequiredInSotW() bool {
	return false
}

// Decode deserializes and validates an xDS resource serialized inside the
// provided `Any` proto, as received from the xDS management server.
func (routeConfigResourceType) Decode(opts *DecodeOptions, resource *anypb.Any) (*DecodeResult, error) {
	name, rc, err := UnmarshalRouteConfigResource(resource, opts.Logger)
	switch {
	case name == "":
		// Name is unset only when protobuf deserialization fails.
		return nil, err
	case err != nil:
		// Protobuf deserialization succeeded, but resource validation failed.
		return &DecodeResult{Name: name, Resource: &RouteConfigResourceData{Resource: RouteConfigUpdate{}}}, err
	}

	return &DecodeResult{Name: name, Resource: &RouteConfigResourceData{Resource: rc}}, nil

}

// RouteConfigResourceData wraps the configuration of a RouteConfiguration
// resource as received from the management server.
//
// Implements the ResourceData interface.
type RouteConfigResourceData struct {
	ResourceData

	// TODO: We have always stored update structs by value. See if this can be
	// switched to a pointer?
	Resource RouteConfigUpdate
}

// Equal returns true if other is equal to r.
func (r *RouteConfigResourceData) Equal(other ResourceData) bool {
	if r == nil && other == nil {
		return true
	}
	if (r == nil) != (other == nil) {
		return false
	}
	return proto.Equal(r.Resource.Raw, other.Raw())

}

// ToJSON returns a JSON string representation of the resource data.
func (r *RouteConfigResourceData) ToJSON() string {
	return pretty.ToJSON(r.Resource)
}

// Raw returns the underlying raw protobuf form of the route configuration
// resource.
func (r *RouteConfigResourceData) Raw() *anypb.Any {
	return r.Resource.Raw
}

// RouteConfigResourceWatcher wraps the callbacks invoked by the xDS client
// implementation for different events corresponding to the route configuration
// resource being watched.
type RouteConfigResourceWatcher interface {
	// OnResourceChanged is invoked when an update for the resource being
	// watched is received from the management server. The ResourceData
	// parameter needs to be type asserted to the appropriate type for the
	// resource being watched.
	OnResourceChanged(*RouteConfigResourceData)

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

type delegatingRouteConfigWatcher struct {
	watcher RouteConfigResourceWatcher
}

func (d *delegatingRouteConfigWatcher) OnGenericResourceChanged(data ResourceData) {
	l := data.(*RouteConfigResourceData)
	d.watcher.OnResourceChanged(l)
}

func (d *delegatingRouteConfigWatcher) OnError(err error) {
	d.watcher.OnError(err)
}

func (d *delegatingRouteConfigWatcher) OnResourceDoesNotExist() {
	d.watcher.OnResourceDoesNotExist()
}

// WatchRouteConfig uses xDS to discover the configuration associated with the
// provided route configuration resource name.
//
// This is a convenience wrapper around the WatchResource() API and callers are
// encouraged to use this over the latter.
func WatchRouteConfig(c XDSClient, resourceName string, watcher RouteConfigResourceWatcher) (cancel func()) {
	delegator := &delegatingRouteConfigWatcher{watcher: watcher}
	return c.WatchResource(routeConfigType, resourceName, delegator)
}
