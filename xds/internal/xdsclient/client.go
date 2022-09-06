/*
 *
 * Copyright 2019 gRPC authors.
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
 *
 */

// Package xdsclient implements a full fledged gRPC client for the xDS API used
// by the xds resolver and balancer implementations.
package xdsclient

import (
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/load"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

// XDSClient is a full fledged gRPC client which queries a set of discovery APIs
// (collectively termed as xDS) on a remote management server, to discover
// various dynamic resources.
type XDSClient interface {
	WatchListener(string, func(xdsresource.ListenerUpdate, error)) func()
	WatchRouteConfig(string, func(xdsresource.RouteConfigUpdate, error)) func()
	WatchCluster(string, func(xdsresource.ClusterUpdate, error)) func()
	WatchEndpoints(string, func(xdsresource.EndpointsUpdate, error)) func()

	// WatchResource uses xDS to discover the provided resource name. The
	// ResourceType implementation determines how the xDS request is sent out
	// and how a response is deserialized and validated. Upon receipt of a
	// response from the management server, appropriate callback on the watcher
	// is invoked.
	//
	// Most callers will not use this API directly but rather via a
	// resource-type-specific wrapper API provided by the relevant ResourceType
	// implementation.
	//
	// TODO: Once this generic client API is fully implemented and integrated,
	// delete the resource type specific watch APIs on this interface.
	WatchResource(rType xdsresource.Type, resourceName string, watcher ResourceWatcher) (cancel func())

	ReportLoad(*bootstrap.ServerConfig) (*load.Store, func())

	DumpResources() map[xdsresource.Type]map[string]xdsresource.UpdateWithMD

	BootstrapConfig() *bootstrap.Config
	Close()
}

// ResourceWatcher wraps the callbacks invoked by the GenericClient for
// different events corresponding to the resource being watched. This is to be
// implemented by the entity watching the xDS resource. Examples of such
// entities include resolvers and balancers which consume configuration sent by
// the xDS management server.
type ResourceWatcher interface {
	// OnResourceChanged is invoked when an update for the resource being
	// watched is received from the management server. The ResourceData
	// parameter needs to be type asserted to the appropriate type for the
	// resource being watched.
	OnResourceChanged(xdsresource.ResourceData)

	// OnError is invoked under different error conditions including but not
	// limited to the following:
	// - authority mentioned in the resource is not found
	// - resource name parsing error
	// - resource deserialization error
	// - resource validation error
	OnError(error)

	// OnResourceDoesNotExist is invoked for a specific error condition where
	// the requested resource is not found on the xDS management server.
	OnResourceDoesNotExist()
}
