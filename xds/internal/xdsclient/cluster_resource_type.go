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
	_ xdsresource.Type         = clusterResourceType{}
	_ xdsresource.ResourceData = &clusterResourceData{}

	// Singleton instantiation of the resource type implementation.
	clusterType = clusterResourceType{}
)

// clusterResourceType TBD.
type clusterResourceType struct {
}

// V2TypeURL is the xDS type URL of this resource type for v2 transport.
func (clusterResourceType) V2TypeURL() string {
	return "type.googleapis.com/envoy.api.v2.Cluster"

}

// V3TypeURL is the xDS type URL of this resource type for v3 transport.
func (clusterResourceType) V3TypeURL() string {
	return "type.googleapis.com/envoy.config.cluster.v3.Cluster"
}

// TypeEnum TBD.
func (clusterResourceType) TypeEnum() xdsresource.ResourceType {
	return xdsresource.ClusterResource
}

// AllResourcesRequiredInSotW TBD.
func (clusterResourceType) AllResourcesRequiredInSotW() bool {
	return true
}

// Decode TBD.
func (clusterResourceType) Decode(opts *xdsresource.DecodeOptions, resource *anypb.Any) (*xdsresource.DecodeResult, error) {
	// TODO: Move all unmarshaling and validating code from xdsresource package
	// into here. And since we have the bootstrap config passed in to this
	// function, we can do the extra validation performed by
	// `UnmarshalOptions.UpdateValidator` right in here.
	name, cluster, err := xdsresource.UnmarshalClusterResource(resource, nil, opts.Logger)
	switch {
	case name == "":
		// Name is unset only when protobuf deserialization fails.
		return nil, err
	case err != nil:
		// Protobuf deserialization succeeded, but resource validation failed.
		return &xdsresource.DecodeResult{Name: name, Resource: &clusterResourceData{Resource: xdsresource.ClusterUpdate{}}}, err
	}

	// Perform extra validation here.
	if err := securityConfigValidator(opts.BootstrapConfig, cluster.SecurityCfg); err != nil {
		return &xdsresource.DecodeResult{Name: name, Resource: &clusterResourceData{Resource: xdsresource.ClusterUpdate{}}}, err
	}

	return &xdsresource.DecodeResult{Name: name, Resource: &clusterResourceData{Resource: cluster}}, nil

}

// clusterResourceData TBD.
// TODO: Move the functionality of xdsresource.ClusterUpdate (rename it to
// clusterResourceData) into this package and have it implement the
// ResourceData interface.
type clusterResourceData struct {
	xdsresource.ResourceData
	Resource xdsresource.ClusterUpdate // TODO: Why do we store this type by value everywhere. Change it to a pointer?
}

// Equal returns true if the passed in resource data is equal to that of the
// receiver.
func (l *clusterResourceData) Equal(other xdsresource.ResourceData) bool {
	if l == nil && other == nil {
		return true
	}
	if (l == nil) != (other == nil) {
		return false
	}
	return proto.Equal(l.Resource.Raw, other.Raw())

}

// ToJSON returns a JSON string representation of the resource data.
func (l *clusterResourceData) ToJSON() string {
	return pretty.ToJSON(l.Resource)
}

// Raw returns the underlying raw protobuf form of the listener resource.
func (l *clusterResourceData) Raw() *anypb.Any {
	return l.Resource.Raw
}

// WatchCluster TDB.
func WatchCluster(c XDSClient, resourceName string, watcher ResourceWatcher) (cancel func()) {
	return c.WatchResource(clusterType, resourceName, watcher)
}
