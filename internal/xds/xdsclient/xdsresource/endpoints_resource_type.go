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
	// EndpointsResourceTypeName represents the transport agnostic name for the
	// endpoint resource.
	EndpointsResourceTypeName = "EndpointsResource"
)

var (
	// Compile time interface checks.
	_ xdsclient.Decoder      = endpointsResourceType{}
	_ xdsclient.ResourceData = (*EndpointsResourceData)(nil)

	// EndpointsResource is a singleton instance of xdsclient.ResourceType
	// that defines the configuration for the Listener resource.
	EndpointsResource = xdsclient.ResourceType{
		TypeURL:                    version.V3EndpointsURL,
		TypeName:                   EndpointsResourceTypeName,
		AllResourcesRequiredInSotW: false,
		Decoder:                    endpointsResourceType{resourceTypeState: resourceTypeState{typeURL: version.V3EndpointsURL, typeName: EndpointsResourceTypeName, allResourcesRequiredInSotW: false}},
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
func (et endpointsResourceType) Decode(resource xdsclient.AnyProto, _ xdsclient.DecodeOptions) (*xdsclient.DecodeResult, error) {
	// Build anypb.Any from the generic AnyProto passed by the generic client.
	a := &anypb.Any{
		TypeUrl: resource.TypeURL,
		Value:   resource.Value,
	}

	// Unmarshal using the helper (it expects an *anypb.Any)
	name, rc, err := unmarshalEndpointsResource(a)
	switch {
	case name == "":
		return nil, err
	case err != nil:
		return &xdsclient.DecodeResult{Name: name, Resource: &EndpointsResourceData{Resource: EndpointsUpdate{}}}, err
	}
	return &xdsclient.DecodeResult{Name: name, Resource: &EndpointsResourceData{Resource: rc}}, nil
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

// Equal returns true if other is equal to c
func (e *EndpointsResourceData) Equal(other xdsclient.ResourceData) bool {
	if e == nil && other == nil {
		return true
	}
	if (e == nil) != (other == nil) {
		return false
	}
	if otherERD, ok := other.(*EndpointsResourceData); ok {
		return e.RawEqual(otherERD)
	}
	return bytes.Equal(e.Bytes(), other.Bytes())
}

// Bytes returns the underlying raw bytes of the clustered resource.
func (e *EndpointsResourceData) Bytes() []byte {
	raw := e.Raw()
	if raw == nil {
		return nil
	}
	return raw.Value
}

// EndpointsWatcher wraps the callbacks to be invoked for different
// events corresponding to the endpoints resource being watched. gRFC A88
// contains an exhaustive list of what method is invoked under what conditions.
type EndpointsWatcher interface {
	// ResourceChanged indicates a new version of the resource is available.
	ResourceChanged(resource *EndpointsResourceData, done func())

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

type delegatingEndpointsWatcher struct {
	watcher EndpointsWatcher
}

func (d *delegatingEndpointsWatcher) ResourceChanged(gData xdsclient.ResourceData, onDone func()) {
	if gData == nil {
		d.watcher.ResourceError(fmt.Errorf("endpoints resource missing"), onDone)
		return
	}
	e, ok := gData.(*EndpointsResourceData)
	if !ok {
		d.watcher.ResourceError(fmt.Errorf("delegatingEndpointsWatcher: unexpected resource data type %T", gData), onDone)
		return
	}
	d.watcher.ResourceChanged(e, onDone)
}

func (d *delegatingEndpointsWatcher) ResourceError(err error, onDone func()) {
	d.watcher.ResourceError(err, onDone)
}

func (d *delegatingEndpointsWatcher) AmbientError(err error, onDone func()) {
	d.watcher.AmbientError(err, onDone)
}

// WatchEndpoints uses xDS to discover the configuration associated with the
// provided endpoints resource name.
func WatchEndpoints(p Producer, name string, w EndpointsWatcher) (cancel func()) {
	var gw xdsclient.ResourceWatcher
	if w != nil {
		gw = &delegatingEndpointsWatcher{watcher: w}
	}
	return p.WatchResource(EndpointsResource, name, gw)
}
