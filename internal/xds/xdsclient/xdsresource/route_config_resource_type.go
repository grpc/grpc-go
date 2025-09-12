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
	xdsclient "google.golang.org/grpc/internal/xds/clients/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"
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
	_ xdsclient.Decoder      = routeConfigResourceType{}
	_ xdsclient.ResourceData = (*RouteConfigResourceData)(nil)

	// RouteConfigResource is a singleton instance of xdsclient.ResourceType
	// that defines the configuration for the Listener resource.
	RouteConfigResource = xdsclient.ResourceType{
		TypeURL:                    version.V3RouteConfigURL,
		TypeName:                   RouteConfigTypeName,
		AllResourcesRequiredInSotW: false,
		Decoder:                    routeConfigResourceType{resourceTypeState: resourceTypeState{typeURL: version.V3RouteConfigURL, typeName: RouteConfigTypeName, allResourcesRequiredInSotW: false}},
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
func (rt routeConfigResourceType) Decode(resource xdsclient.AnyProto, _ xdsclient.DecodeOptions) (*xdsclient.DecodeResult, error) {
	// Reconstruct anypb.Any from the generic AnyProto provided by the client.
	a := &anypb.Any{
		TypeUrl: resource.TypeURL,
		Value:   resource.Value,
	}

	name, rc, err := unmarshalRouteConfigResource(a)
	switch {
	case name == "":
		return nil, err
	case err != nil:
		return &xdsclient.DecodeResult{Name: name, Resource: &RouteConfigResourceData{Resource: RouteConfigUpdate{}}}, err
	}
	return &xdsclient.DecodeResult{Name: name, Resource: &RouteConfigResourceData{Resource: rc}}, nil
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

// Equal returns true if other is equal to c
func (r *RouteConfigResourceData) Equal(other xdsclient.ResourceData) bool {
	if r == nil && other == nil {
		return true
	}
	if (r == nil) != (other == nil) {
		return false
	}
	if otherRCRD, ok := other.(*RouteConfigResourceData); ok {
		return r.RawEqual(otherRCRD)
	}
	return bytes.Equal(r.Bytes(), other.Bytes())
}

// Bytes returns the underlying raw bytes of the clustered resource.
func (r *RouteConfigResourceData) Bytes() []byte {
	raw := r.Raw()
	if raw == nil {
		return nil
	}
	return raw.Value
}

// RouteConfigWatcher wraps the callbacks to be invoked for different
// events corresponding to the route configuration resource being watched. gRFC
// A88 contains an exhaustive list of what method is invoked under what
// conditions.
type RouteConfigWatcher interface {
	// ResourceChanged indicates a new version of the resource is available.
	ResourceChanged(resource *RouteConfigResourceData, done func())

	// ResourceError indicates an error occurred while trying to fetch or
	// decode the associated resource. The previous version of the resource
	// should be considered invalid.
	ResourceError(err error, done func())

	// AmbientError indicates an error occurred after a resource has been
	// received that should not modify the use of that resource but may provide
	// useful information about the state of the XDSClient for debugging
	// purposes. The previous version of the resource should still be
	// considered valid.
	AmbientError(err error, done func())
}

type delegatingRouteConfigWatcher struct {
	watcher RouteConfigWatcher
}

func (d *delegatingRouteConfigWatcher) ResourceChanged(gData xdsclient.ResourceData, onDone func()) {
	if gData == nil {
		d.watcher.ResourceError(fmt.Errorf("route config resource missing"), onDone)
		return
	}
	rc, ok := gData.(*RouteConfigResourceData)
	if !ok {
		d.watcher.ResourceError(fmt.Errorf("delegatingRouteConfigWatcher: unexpected resource data type %T", gData), onDone)
		return
	}
	d.watcher.ResourceChanged(rc, onDone)
}

func (d *delegatingRouteConfigWatcher) ResourceError(err error, onDone func()) {
	d.watcher.ResourceError(err, onDone)
}

func (d *delegatingRouteConfigWatcher) AmbientError(err error, onDone func()) {
	d.watcher.AmbientError(err, onDone)
}

// WatchRouteConfig uses xDS to discover the configuration associated with the
// provided route configuration resource name.
func WatchRouteConfig(p Producer, name string, w RouteConfigWatcher) (cancel func()) {
	var gw xdsclient.ResourceWatcher
	if w != nil {
		gw = &delegatingRouteConfigWatcher{watcher: w}
	}
	return p.WatchResource(RouteConfigResource, name, gw)
}

// NewRouteConfigResourceTypeDecoder returns a decoder for RouteConfig resources.
// RouteConfig is stateless so this just returns the zero-valued
// routeConfigResourceType.
func NewRouteConfigResourceTypeDecoder() xdsclient.Decoder {
	return routeConfigResourceType{
		resourceTypeState: resourceTypeState{
			typeURL:                    version.V3RouteConfigURL,
			typeName:                   RouteConfigTypeName,
			allResourcesRequiredInSotW: false,
		},
	}
}
