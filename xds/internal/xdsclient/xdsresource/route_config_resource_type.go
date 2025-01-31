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
	// RouteConfigTypeName represents the transport agnostic name for the
	// route config resource.
	RouteConfigTypeName = "RouteConfigResource"
)

var (
	// Compile time interface checks.
	_ Type = routeConfigResourceType{}

	// Singleton instantiation of the resource type implementation.
	routeConfigType = routeConfigResourceType{
		resourceTypeState: resourceTypeState{
			typeURL:                    version.V3RouteConfigURL,
			typeName:                   "RouteConfigResource",
			allResourcesRequiredInSotW: false,
		},
	}
)

// routeConfigResourceType provides the resource-type specific functionality for
// a RouteConfiguration resource.
//
// Implements the Type interface.
type routeConfigResourceType struct {
	resourceTypeState
}

// Decode deserializes and validates an xDS resource serialized inside the
// provided `Any` proto, as received from the xDS management server.
func (routeConfigResourceType) Decode(_ *DecodeOptions, resource *anypb.Any) (*DecodeResult, error) {
	name, rc, err := unmarshalRouteConfigResource(resource)
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

// RawEqual returns true if other is equal to r.
func (r *RouteConfigResourceData) RawEqual(other ResourceData) bool {
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

// RouteConfigWatcher wraps the callbacks to be invoked for different
// events corresponding to the route configuration resource being watched. gRFC
// A88 contains an exhaustive list of what method is invoked under what
// conditions.
type RouteConfigWatcher interface {
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
	// Upon receiving this, the watcher may continue using the previously seen
	// resource. xDS client will not remove the resource from its cache.
	OnAmbientError(error, OnDoneFunc)
}

type delegatingRouteConfigWatcher struct {
	watcher RouteConfigWatcher
}

func (d *delegatingRouteConfigWatcher) OnResourceChanged(update ResourceDataOrError, onDone OnDoneFunc) {
	if update.Err != nil {
		d.watcher.OnResourceChanged(&ResourceDataOrError{Err: update.Err}, onDone)
		return
	}
	rc := update.Data.(*RouteConfigResourceData)
	d.watcher.OnResourceChanged(&ResourceDataOrError{Data: rc}, onDone)
}

func (d *delegatingRouteConfigWatcher) OnAmbientError(err error, onDone OnDoneFunc) {
	d.watcher.OnAmbientError(err, onDone)
}

// WatchRouteConfig uses xDS to discover the configuration associated with the
// provided route configuration resource name.
func WatchRouteConfig(p Producer, name string, w RouteConfigWatcher) (cancel func()) {
	delegator := &delegatingRouteConfigWatcher{watcher: w}
	return p.WatchResource(routeConfigType, name, delegator)
}
