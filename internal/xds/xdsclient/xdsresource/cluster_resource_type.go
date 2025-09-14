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
	"google.golang.org/grpc/internal/xds/bootstrap"
	xdsclient "google.golang.org/grpc/internal/xds/clients/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	// ClusterResourceTypeName represents the transport agnostic name for the
	// cluster resource.
	ClusterResourceTypeName = "ClusterResource"
)

var (
	// Compile time interface checks.
	_ xdsclient.Decoder      = clusterResourceType{}
	_ xdsclient.ResourceData = (*ClusterResourceData)(nil)

	// ClusterResource is a singleton instance of xdsclient.ResourceType
	// that defines the configuration for the Listener resource.
	ClusterResource = xdsclient.ResourceType{
		TypeURL:                    version.V3ClusterURL,
		TypeName:                   ClusterResourceTypeName,
		AllResourcesRequiredInSotW: true,
	}
)

// clusterResourceType provides the resource-type specific functionality for a
// Cluster resource.
//
// Implements the Type interface.
type clusterResourceType struct {
	resourceTypeState
	BootstrapConfig *bootstrap.Config
	ServerConfigMap map[xdsclient.ServerConfig]*bootstrap.ServerConfig
}

// Decode deserializes and validates an xDS resource serialized inside the
// provided `Any` proto, as received from the xDS management server.
func (ct clusterResourceType) Decode(resource xdsclient.AnyProto, gOpts xdsclient.DecodeOptions) (*xdsclient.DecodeResult, error) {
	// Convert generic AnyProto -> anypb.Any
	a := &anypb.Any{
		TypeUrl: resource.TypeURL,
		Value:   resource.Value,
	}

	// Map generic decode options to internal options
	internalOpts := &DecodeOptions{BootstrapConfig: ct.BootstrapConfig}
	if gOpts.ServerConfig != nil && ct.ServerConfigMap != nil {
		if sc, ok := ct.ServerConfigMap[*gOpts.ServerConfig]; ok {
			internalOpts.ServerConfig = sc
		}
	}

	name, cluster, err := unmarshalClusterResource(a, internalOpts.ServerConfig)
	switch {
	case name == "":
		// Name unset => protobuf deserialization failure.
		return nil, err
	case err != nil:
		return &xdsclient.DecodeResult{
			Name:     name,
			Resource: &ClusterResourceData{Resource: ClusterUpdate{}},
		}, err
	}

	if err := securityConfigValidator(internalOpts.BootstrapConfig, cluster.SecurityCfg); err != nil {
		return &xdsclient.DecodeResult{
			Name:     name,
			Resource: &ClusterResourceData{Resource: ClusterUpdate{}},
		}, err
	}

	return &xdsclient.DecodeResult{
		Name:     name,
		Resource: &ClusterResourceData{Resource: cluster},
	}, nil
}

// ClusterResourceData wraps the configuration of a Cluster resource as received
// from the management server.
//
// Implements the ResourceData interface.
type ClusterResourceData struct {
	ResourceData

	// TODO: We have always stored update structs by value. See if this can be
	// switched to a pointer?
	Resource ClusterUpdate
}

// RawEqual returns true if other is equal to r.
func (c *ClusterResourceData) RawEqual(other ResourceData) bool {
	if c == nil && other == nil {
		return true
	}
	if (c == nil) != (other == nil) {
		return false
	}
	return proto.Equal(c.Resource.Raw, other.Raw())
}

// ToJSON returns a JSON string representation of the resource data.
func (c *ClusterResourceData) ToJSON() string {
	return pretty.ToJSON(c.Resource)
}

// Raw returns the underlying raw protobuf form of the cluster resource.
func (c *ClusterResourceData) Raw() *anypb.Any {
	return c.Resource.Raw
}

// Equal returns true if other is equal to c
func (c *ClusterResourceData) Equal(other xdsclient.ResourceData) bool {
	if c == nil && other == nil {
		return true
	}
	if (c == nil) != (other == nil) {
		return false
	}
	if otherCRD, ok := other.(*ClusterResourceData); ok {
		return c.RawEqual(otherCRD)
	}
	return bytes.Equal(c.Bytes(), other.Bytes())
}

// Bytes returns the underlying raw bytes of the clustered resource.
func (c *ClusterResourceData) Bytes() []byte {
	raw := c.Raw()
	if raw == nil {
		return nil
	}
	return raw.Value
}

// ClusterWatcher wraps the callbacks to be invoked for different events
// corresponding to the cluster resource being watched. gRFC A88 contains an
// exhaustive list of what method is invoked under what conditions.
type ClusterWatcher interface {
	// ResourceChanged indicates a new version of the resource is available.
	ResourceChanged(resource *ClusterResourceData, done func())

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

type delegatingClusterWatcher struct {
	watcher ClusterWatcher
}

func (d *delegatingClusterWatcher) ResourceChanged(gData xdsclient.ResourceData, onDone func()) {
	if gData == nil {
		d.watcher.ResourceError(fmt.Errorf("cluster resource missing"), onDone)
		return
	}
	c, ok := gData.(*ClusterResourceData)
	if !ok {
		d.watcher.ResourceError(fmt.Errorf("delegatingClusterWatcher: unexpected resource data type %T", gData), onDone)
		return
	}
	d.watcher.ResourceChanged(c, onDone)
}

func (d *delegatingClusterWatcher) ResourceError(err error, onDone func()) {
	d.watcher.ResourceError(err, onDone)
}

func (d *delegatingClusterWatcher) AmbientError(err error, onDone func()) {
	d.watcher.AmbientError(err, onDone)
}

// WatchCluster uses xDS to discover the configuration associated with the
// provided cluster resource name.
func WatchCluster(p Producer, name string, w ClusterWatcher) (cancel func()) {
	var gw xdsclient.ResourceWatcher
	if w != nil {
		gw = &delegatingClusterWatcher{watcher: w}
	}
	return p.WatchResource(ClusterResource, name, gw)
}

// NewClusterResourceTypeDecoder returns a xdsclient.Decoder that has access to
// bootstrap config and server config mapping for decoding.
func NewClusterResourceTypeDecoder(bc *bootstrap.Config, gServerCfgMap map[xdsclient.ServerConfig]*bootstrap.ServerConfig) xdsclient.Decoder {
	return &clusterResourceType{
		resourceTypeState: resourceTypeState{
			typeURL:                    version.V3ClusterURL,
			typeName:                   ClusterResourceTypeName,
			allResourcesRequiredInSotW: true,
		},
		BootstrapConfig: bc,
		ServerConfigMap: gServerCfgMap,
	}
}
