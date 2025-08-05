// xdsresource/types.go
/*
 *
 * Copyright 2022 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
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

	"google.golang.org/grpc/internal/xds/bootstrap"
	xdsinternal "google.golang.org/grpc/xds/internal"
	xdsclient "google.golang.org/grpc/xds/internal/clients/xdsclient" // New import for xdsclient.Decoder and related types
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/proto" // Needed for proto.Unmarshal and proto.Equal
	"google.golang.org/protobuf/types/known/anypb"

	// PLACEHOLDER IMPORTS FOR ENVOY PROTOS
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
)

// The init function will now use instances of our new concrete types
func init() {
	xdsinternal.ResourceTypeMapForTesting = make(map[string]any)
	xdsinternal.ResourceTypeMapForTesting[version.V3ListenerURL] = listenerType
	xdsinternal.ResourceTypeMapForTesting[version.V3RouteConfigURL] = routeConfigType
	xdsinternal.ResourceTypeMapForTesting[version.V3ClusterURL] = clusterType
	xdsinternal.ResourceTypeMapForTesting[version.V3EndpointsURL] = endpointsType
}

// Producer, ResourceWatcher, ResourceData (interface), DecodeOptions, DecodeResult, resourceTypeState
// These interfaces and structs remain as they were, defining the internal
// xdsresource package's data model and behavior.

// Producer contains a single method to discover resource configuration from a
// remote management server using xDS APIs.
//
// The xdsclient package provides a concrete implementation of this interface.
type Producer interface {
	// WatchResource uses xDS to discover the resource associated with the
	// provided resource name. The resource type implementation determines how
	// xDS responses are are deserialized and validated, as received from the
	// xDS management server. Upon receipt of a response from the management
	// server, an appropriate callback on the watcher is invoked.
	WatchResource(rType Type, resourceName string, watcher ResourceWatcher) (cancel func())
}

// ResourceWatcher is notified of the resource updates and errors that are
// received by the xDS client from the management server.
//
// All methods contain a done parameter which should be called when processing
// of the update has completed.
type ResourceWatcher interface {
	// ResourceChanged indicates a new version of the resource is available.
	ResourceChanged(resourceData ResourceData, done func())

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

// Type wraps all resource-type specific functionality. Each supported resource
// type will provide an implementation of this interface.
type Type interface {
	// TypeURL is the xDS type URL of this resource type for v3 transport.
	TypeURL() string

	// TypeName identifies resources in a transport protocol agnostic way. This
	// can be used for logging/debugging purposes, as well in cases where the
	// resource type name is to be uniquely identified but the actual
	// functionality provided by the resource type is not required.

	TypeName() string

	// AllResourcesRequiredInSotW indicates whether this resource type requires
	// that all resources be present in every SotW response from the server. If
	// true, a response that does not include a previously seen resource will be
	// interpreted as a deletion of that resource.
	AllResourcesRequiredInSotW() bool

	// Decode deserializes and validates an xDS resource serialized inside the
	// provided `Any` proto, as received from the xDS management server.
	//
	// If protobuf deserialization fails or resource validation fails,
	// returns a non-nil error. Otherwise, returns a fully populated
	// DecodeResult.
	Decode(*DecodeOptions, *anypb.Any) (*DecodeResult, error)
}

// ResourceData contains the configuration data sent by the xDS management
// server, associated with the resource being watched. Every resource type must
// provide an implementation of this interface to represent the configuration
// received from the xDS management server.
type ResourceData interface {
	// RawEqual returns true if the passed in resource data is equal to that of
	// the receiver, based on the underlying raw protobuf message.
	RawEqual(ResourceData) bool

	// ToJSON returns a JSON string representation of the resource data.
	ToJSON() string

	Raw() *anypb.Any
	Bytes() []byte
	Equal(xdsclient.ResourceData) bool
}

// DecodeOptions wraps the options required by ResourceType implementation for
// decoding configuration received from the xDS management server.
type DecodeOptions struct {
	// BootstrapConfig contains the complete bootstrap configuration passed to
	// the xDS client. This contains useful data for resource validation.
	BootstrapConfig *bootstrap.Config
	// ServerConfig contains the server config (from the above bootstrap
	// configuration) of the xDS server from which the current resource, for
	// which Decode() is being invoked, was received.
	ServerConfig *bootstrap.ServerConfig
}

// DecodeResult is the result of a decode operation.
type DecodeResult struct {
	// Name is the name of the resource being watched.
	Name string
	// Resource contains the configuration associated with the resource being
	// watched.
	Resource ResourceData
}

// resourceTypeState wraps the static state associated with concrete resource
// type implementations, which can then embed this struct and get the methods
// implemented here for free.
type resourceTypeState struct {
	typeURL                    string
	typeName                   string
	allResourcesRequiredInSotW bool
}

func (r resourceTypeState) TypeURL() string {
	return r.typeURL
}

func (r resourceTypeState) TypeName() string {
	return r.typeName
}

func (r resourceTypeState) AllResourcesRequiredInSotW() bool {
	return r.allResourcesRequiredInSotW
}

// --- CONCRETE RESOURCE TYPE IMPLEMENTATIONS ---
// These structs are the "users" that will now directly implement xdsclient.Decoder.

// Listener represents the internal data structure for a Listener resource.
// You should adapt this to your actual internal Listener representation.
type Listener struct {
	*v3listenerpb.Listener // Embedding the proto for simplicity in this example
}

// RawEqual implements xdsresource.ResourceData.
func (l *Listener) RawEqual(other ResourceData) bool {
	o, ok := other.(*Listener)
	if !ok {
		return false
	}
	return proto.Equal(l.Listener, o.Listener)
}

// ToJSON implements xdsresource.ResourceData.
func (l *Listener) ToJSON() string {
	return l.Listener.String() // Or your actual JSON serialization logic
}

// Raw implements xdsresource.ResourceData.
func (l *Listener) Raw() *anypb.Any {
	any, _ := anypb.New(l.Listener) // Error ignored for brevity, handle in real code
	return any
}
func (l *Listener) Bytes() []byte {
	b, _ := proto.Marshal(l.Listener)
	return b
}
func (l *Listener) Equal(other xdsclient.ResourceData) bool {
	o, ok := other.(*Listener)
	if !ok {
		return false
	}
	return l.RawEqual(o)
}

// listenerTypeImpl implements xdsresource.Type and xdsclient.Decoder.
type listenerTypeImpl struct {
	resourceTypeState
	bootstrapConfig *bootstrap.Config
	serverConfigMap map[xdsclient.ServerConfig]*bootstrap.ServerConfig
}

var listenerType = listenerTypeImpl{
	resourceTypeState: resourceTypeState{
		typeURL:                    version.V3ListenerURL,
		typeName:                   "Listener",
		allResourcesRequiredInSotW: false,
	},
}

// TypeURL implements xdsresource.Type.
func (lt listenerTypeImpl) TypeURL() string { return lt.resourceTypeState.TypeURL() }

// TypeName implements xdsresource.Type.
func (lt listenerTypeImpl) TypeName() string { return lt.resourceTypeState.TypeName() }

// AllResourcesRequiredInSotW implements xdsresource.Type.
func (lt listenerTypeImpl) AllResourcesRequiredInSotW() bool {
	return lt.resourceTypeState.AllResourcesRequiredInSotW()
}

// Decode implements xdsresource.Type.
// This is the internal decode logic for the xdsresource package.
func (lt listenerTypeImpl) Decode(opts *DecodeOptions, anyProto *anypb.Any) (*DecodeResult, error) {
	var listenerProto v3listenerpb.Listener
	if err := anyProto.UnmarshalTo(&listenerProto); err != nil {
		return nil, fmt.Errorf("xdsresource: failed to unmarshal Listener: %v", err)
	}

	if err := validateListener(&listenerProto); err != nil {
		return nil, fmt.Errorf("xdsresource: Listener validation failed for %q: %v", listenerProto.GetName(), err)
	}

	return &DecodeResult{
		Name:     listenerProto.GetName(),
		Resource: &Listener{Listener: &listenerProto},
	}, nil
}

// XDSClientDecode implements xdsclient.Decoder.
// This method bridges the xdsclient.Decoder interface to the xdsresource.Type.Decode method.
// This is to eliminate GenericResourceTypeDecoder.
func (lt listenerTypeImpl) XDSClientDecode(resource xdsclient.AnyProto, gOpts xdsclient.DecodeOptions) (*xdsclient.DecodeResult, error) {
	anyProto := &anypb.Any{
		TypeUrl: resource.TypeURL,
		Value:   resource.Value,
	}
	// The BootstrapConfig is now taken from the struct's field.
	opts := &DecodeOptions{BootstrapConfig: lt.bootstrapConfig}
	if gOpts.ServerConfig != nil {
		if bootstrapSC, ok := lt.serverConfigMap[*gOpts.ServerConfig]; ok {
			opts.ServerConfig = bootstrapSC
		} else {
			return nil, fmt.Errorf("xdsresource: server config %v not found in map", *gOpts.ServerConfig)
		}
	}

	internalResult, err := lt.Decode(opts, anyProto)
	if err != nil {
		if internalResult != nil {
			return &xdsclient.DecodeResult{Name: internalResult.Name}, err
		}
		return nil, err
	}
	if internalResult == nil {
		return nil, fmt.Errorf("xdsresource: internal decode returned nil result but no error")
	}

	xdsClientResourceData, ok := internalResult.Resource.(xdsclient.ResourceData)
	if !ok {
		return nil, fmt.Errorf("xdsresource: internal resource of type %T does not implement xdsclient.ResourceData", internalResult.Resource)
	}

	return &xdsclient.DecodeResult{
		Name:     internalResult.Name,
		Resource: xdsClientResourceData,
	}, nil
}

// Placeholder for Listener validation logic.
func validateListener(l *v3listenerpb.Listener) error {
	if l.GetName() == "" {
		return fmt.Errorf("listener name cannot be empty")
	}
	return nil
}

func NewEndPointsDecoder(bootstrapConfig *bootstrap.Config, m map[xdsclient.ServerConfig]*bootstrap.ServerConfig) *listenerTypeImpl {
	val := listenerType
	val.bootstrapConfig = bootstrapConfig
	val.serverConfigMap = m
	return &val
}

// RouteConfig represents the internal data structure for a RouteConfiguration resource.
type RouteConfig struct {
	*v3routepb.RouteConfiguration
}

func (rc *RouteConfig) Bytes() []byte {
	b, _ := proto.Marshal(rc.RouteConfiguration)
	return b
}
func (rc *RouteConfig) RawEqual(other ResourceData) bool {
	o, ok := other.(*RouteConfig)
	if !ok {
		return false
	}
	return proto.Equal(rc.RouteConfiguration, o.RouteConfiguration)
}
func (rc *RouteConfig) Raw() *anypb.Any {
	any, _ := anypb.New(rc.RouteConfiguration)
	return any
}
func (rc *RouteConfig) Equal(other xdsclient.ResourceData) bool {
	o, ok := other.(*RouteConfig)
	if !ok {
		return false
	}
	return rc.RawEqual(o)
}

func (rc *RouteConfig) ToJSON() string { return rc.RouteConfiguration.String() }

type routeConfigTypeImpl struct {
	resourceTypeState
	bootstrapConfig *bootstrap.Config
	serverConfigMap map[xdsclient.ServerConfig]*bootstrap.ServerConfig
}

func (rt routeConfigTypeImpl) TypeURL() string  { return rt.resourceTypeState.TypeURL() }
func (rt routeConfigTypeImpl) TypeName() string { return rt.resourceTypeState.TypeName() }
func (rt routeConfigTypeImpl) AllResourcesRequiredInSotW() bool {
	return rt.resourceTypeState.AllResourcesRequiredInSotW()
}

func (rt routeConfigTypeImpl) Decode(opts *DecodeOptions, anyProto *anypb.Any) (*DecodeResult, error) {
	var rcProto v3routepb.RouteConfiguration
	if err := anyProto.UnmarshalTo(&rcProto); err != nil {
		return nil, fmt.Errorf("xdsresource: failed to unmarshal RouteConfiguration: %v", err)
	}
	// Placeholder validation
	return &DecodeResult{Name: rcProto.GetName(), Resource: &RouteConfig{RouteConfiguration: &rcProto}}, nil
}

// XDSClientDecode implements xdsclient.Decoder for RouteConfig.
func (rt routeConfigTypeImpl) XDSClientDecode(resource xdsclient.AnyProto, gOpts xdsclient.DecodeOptions) (*xdsclient.DecodeResult, error) {
	anyProto := &anypb.Any{TypeUrl: resource.TypeURL, Value: resource.Value}

	opts := &DecodeOptions{BootstrapConfig: rt.bootstrapConfig}

	if gOpts.ServerConfig != nil {
		if bootstrapSC, ok := rt.serverConfigMap[*gOpts.ServerConfig]; ok {
			opts.ServerConfig = bootstrapSC
		} else {
			return nil, fmt.Errorf("xdsresource: server config %v not found in map", *gOpts.ServerConfig)
		}
	}
	internalResult, err := rt.Decode(opts, anyProto)
	if err != nil {
		if internalResult != nil {
			return &xdsclient.DecodeResult{Name: internalResult.Name}, err
		}
		return nil, err
	}
	if internalResult == nil {
		return nil, fmt.Errorf("xdsresource: internal decode returned nil result but no error")
	}

	xdsClientResourceData, ok := internalResult.Resource.(xdsclient.ResourceData)
	if !ok {
		return nil, fmt.Errorf("xdsresource: internal resource of type %T does not implement xdsclient.ResourceData", internalResult.Resource)
	}

	return &xdsclient.DecodeResult{Name: internalResult.Name, Resource: xdsClientResourceData}, nil
}

// Cluster represents the internal data structure for a Cluster resource.
type Cluster struct {
	*v3clusterpb.Cluster
}

func (c *Cluster) Bytes() []byte {
	b, _ := proto.Marshal(c.Cluster)
	return b
}
func (c *Cluster) RawEqual(other ResourceData) bool {
	o, ok := other.(*Cluster)
	if !ok {
		return false
	}
	return proto.Equal(c.Cluster, o.Cluster)
}
func (c *Cluster) ToJSON() string { return c.Cluster.String() }
func (c *Cluster) Raw() *anypb.Any {
	any, _ := anypb.New(c.Cluster)
	return any
}

func (c *Cluster) Equal(other xdsclient.ResourceData) bool {
	o, ok := other.(*Cluster)
	if !ok {
		return false
	}
	return c.RawEqual(o)
}

type clusterTypeImpl struct {
	resourceTypeState
	bootstrapConfig *bootstrap.Config
	serverConfigMap map[xdsclient.ServerConfig]*bootstrap.ServerConfig
}

func (ct clusterTypeImpl) TypeURL() string  { return ct.resourceTypeState.TypeURL() }
func (ct clusterTypeImpl) TypeName() string { return ct.resourceTypeState.TypeName() }
func (ct clusterTypeImpl) AllResourcesRequiredInSotW() bool {
	return ct.resourceTypeState.AllResourcesRequiredInSotW()
}

func (ct clusterTypeImpl) Decode(opts *DecodeOptions, anyProto *anypb.Any) (*DecodeResult, error) {
	var cProto v3clusterpb.Cluster
	if err := anyProto.UnmarshalTo(&cProto); err != nil {
		return nil, fmt.Errorf("xdsresource: failed to unmarshal Cluster: %v", err)
	}
	// Placeholder validation
	return &DecodeResult{Name: cProto.GetName(), Resource: &Cluster{Cluster: &cProto}}, nil
}

// XDSClientDecode implements xdsclient.Decoder for Endpoints.
func (et endpointsTypeImpl) XDSClientDecode(resource xdsclient.AnyProto, gOpts xdsclient.DecodeOptions) (*xdsclient.DecodeResult, error) {
	anyProto := &anypb.Any{TypeUrl: resource.TypeURL, Value: resource.Value}

	opts := &DecodeOptions{BootstrapConfig: et.bootstrapConfig}

	if gOpts.ServerConfig != nil {
		if bootstrapSC, ok := et.serverConfigMap[*gOpts.ServerConfig]; ok {
			opts.ServerConfig = bootstrapSC
		} else {
			return nil, fmt.Errorf("xdsresource: server config %v not found in map", *gOpts.ServerConfig)
		}
	}
	internalResult, err := et.Decode(opts, anyProto)
	if err != nil {
		if internalResult != nil {
			return &xdsclient.DecodeResult{Name: internalResult.Name}, err
		}
		return nil, err
	}
	if internalResult == nil {
		return nil, fmt.Errorf("xdsresource: internal decode returned nil result but no error")
	}

	xdsClientResourceData, ok := internalResult.Resource.(xdsclient.ResourceData)
	if !ok {
		return nil, fmt.Errorf("xdsresource: internal resource of type %T does not implement xdsclient.ResourceData", internalResult.Resource)
	}
	return &xdsclient.DecodeResult{Name: internalResult.Name, Resource: xdsClientResourceData}, nil
}

type Endpoints struct {
	*v3endpointpb.ClusterLoadAssignment
}

// RawEqual implements xdsresource.ResourceData.
func (e *Endpoints) RawEqual(other ResourceData) bool {
	o, ok := other.(*Endpoints)
	if !ok {
		return false
	}
	return proto.Equal(e.ClusterLoadAssignment, o.ClusterLoadAssignment)
}

// ToJSON implements xdsresource.ResourceData.
func (e *Endpoints) ToJSON() string {
	return e.ClusterLoadAssignment.String()
}

// Raw implements xdsresource.ResourceData.
func (e *Endpoints) Raw() *anypb.Any {
	any, _ := anypb.New(e.ClusterLoadAssignment)
	return any
}

// Bytes implements xdsresource.ResourceData and xdsclient.ResourceData.
func (e *Endpoints) Bytes() []byte {
	b, _ := proto.Marshal(e.ClusterLoadAssignment)
	return b
}

func (e *Endpoints) Equal(other xdsclient.ResourceData) bool {
	o, ok := other.(*Endpoints)
	if !ok {
		return false
	}
	return e.RawEqual(o)
}

type endpointsTypeImpl struct {
	resourceTypeState
	bootstrapConfig *bootstrap.Config
	serverConfigMap map[xdsclient.ServerConfig]*bootstrap.ServerConfig
}

func (et endpointsTypeImpl) TypeURL() string  { return et.resourceTypeState.TypeURL() }
func (et endpointsTypeImpl) TypeName() string { return et.resourceTypeState.TypeName() }
func (et endpointsTypeImpl) AllResourcesRequiredInSotW() bool {
	return et.resourceTypeState.AllResourcesRequiredInSotW()
}

func (et endpointsTypeImpl) Decode(opts *DecodeOptions, anyProto *anypb.Any) (*DecodeResult, error) {
	var epProto v3endpointpb.ClusterLoadAssignment
	if err := anyProto.UnmarshalTo(&epProto); err != nil {
		return nil, fmt.Errorf("xdsresource: failed to unmarshal Endpoints: %v", err)
	}
	// Placeholder validation
	return &DecodeResult{Name: epProto.GetClusterName(), Resource: &Endpoints{ClusterLoadAssignment: &epProto}}, nil
}
