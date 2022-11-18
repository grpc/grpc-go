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
	_ Type         = clusterResourceType{}
	_ ResourceData = &ClusterResourceData{}

	// Singleton instantiation of the resource type implementation.
	clusterType = clusterResourceType{
		resourceTypeState: resourceTypeState{
			v2TypeURL:                  "type.googleapis.com/envoy.api.v2.Cluster",
			v3TypeURL:                  "type.googleapis.com/envoy.config.cluster.v3.Cluster",
			typeEnum:                   ClusterResource,
			allResourcesRequiredInSotW: true,
		},
	}
)

// clusterResourceType provides the resource-type specific functionality for a
// Cluster resource.
//
// Implements the Type interface.
type clusterResourceType struct {
	resourceTypeState
}

// Decode deserializes and validates an xDS resource serialized inside the
// provided `Any` proto, as received from the xDS management server.
func (clusterResourceType) Decode(opts *DecodeOptions, resource *anypb.Any) (*DecodeResult, error) {
	name, cluster, err := unmarshalClusterResource(resource, nil, opts.Logger)
	switch {
	case name == "":
		// Name is unset only when protobuf deserialization fails.
		return nil, err
	case err != nil:
		// Protobuf deserialization succeeded, but resource validation failed.
		return &DecodeResult{Name: name, Resource: &ClusterResourceData{Resource: ClusterUpdate{}}}, err
	}

	// Perform extra validation here.
	if err := securityConfigValidator(opts.BootstrapConfig, cluster.SecurityCfg); err != nil {
		return &DecodeResult{Name: name, Resource: &ClusterResourceData{Resource: ClusterUpdate{}}}, err
	}

	return &DecodeResult{Name: name, Resource: &ClusterResourceData{Resource: cluster}}, nil

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

// Equal returns true if other is equal to r.
func (l *ClusterResourceData) Equal(other ResourceData) bool {
	if l == nil && other == nil {
		return true
	}
	if (l == nil) != (other == nil) {
		return false
	}
	return proto.Equal(l.Resource.Raw, other.Raw())

}

// ToJSON returns a JSON string representation of the resource data.
func (l *ClusterResourceData) ToJSON() string {
	return pretty.ToJSON(l.Resource)
}

// Raw returns the underlying raw protobuf form of the cluster resource.
func (l *ClusterResourceData) Raw() *anypb.Any {
	return l.Resource.Raw
}

// ClusterResourceWatcher wraps the callbacks to be invoked for different events
// corresponding to the cluster resource being watched.
type ClusterResourceWatcher interface {
	// OnUpdate is invoked to report an update for the resource being watched.
	OnUpdate(*ClusterResourceData)

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

type delegatingClusterWatcher struct {
	watcher ClusterResourceWatcher
}

func (d *delegatingClusterWatcher) OnUpdate(data ResourceData) {
	c := data.(*ClusterResourceData)
	d.watcher.OnUpdate(c)
}

func (d *delegatingClusterWatcher) OnError(err error) {
	d.watcher.OnError(err)
}

func (d *delegatingClusterWatcher) OnResourceDoesNotExist() {
	d.watcher.OnResourceDoesNotExist()
}

// WatchCluster uses xDS to discover the configuration associated with the
// provided cluster resource name.
//
// This is a convenience wrapper around the WatchResource() API and callers are
// encouraged to use this over the latter.
func WatchCluster(c Producer, resourceName string, watcher ClusterResourceWatcher) (cancel func()) {
	delegator := &delegatingClusterWatcher{watcher: watcher}
	return c.WatchResource(clusterType, resourceName, delegator)
}
