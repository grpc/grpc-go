/*
 *
 * Copyright 2024 gRPC authors.
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
	"time"

	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc/xds/clients/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// updateValidatorFunc performs validations on update structs using
// context/logic available at the xdsClient layer. Since these validation are
// performed on internal update structs, they can be shared between different
// API clients.
type updateValidatorFunc func(any) error

// updateMetadata contains the metadata for each update, including timestamp,
// raw message, and so on.
type updateMetadata struct {
	// Status is the status of this resource, e.g. ACKed, NACKed, or
	// Not_exist(removed).
	status serviceStatus
	// Version is the version of the xds response. Note that this is the version
	// of the resource in use (previous ACKed). If a response is NACKed, the
	// NACKed version is in ErrState.
	version string
	// Timestamp is when the response is received.
	timestamp time.Time
	// ErrState is set when the update is NACKed.
	errState *updateErrorMetadata
}

// isListenerResource returns true if the provider URL corresponds to an xDS
// Listener resource.
func isListenerResource(url string) bool {
	return url == version.V3ListenerURL
}

// isHTTPConnManagerResource returns true if the provider URL corresponds to an xDS
// HTTPConnManager resource.
func isHTTPConnManagerResource(url string) bool {
	return url == version.V3HTTPConnManagerURL
}

// isRouteConfigResource returns true if the provider URL corresponds to an xDS
// RouteConfig resource.
func isRouteConfigResource(url string) bool {
	return url == version.V3RouteConfigURL
}

// isClusterResource returns true if the provider URL corresponds to an xDS
// Cluster resource.
func isClusterResource(url string) bool {
	return url == version.V3ClusterURL
}

// IsEndpointsResource returns true if the provider URL corresponds to an xDS
// Endpoints resource.
func isEndpointsResource(url string) bool {
	return url == version.V3EndpointsURL
}

// unwrapResource unwraps and returns the inner resource if it's in a resource
// wrapper. The original resource is returned if it's not wrapped.
func unwrapResource(r *anypb.Any) (*anypb.Any, error) {
	url := r.GetTypeUrl()
	if url != version.V3ResourceWrapperURL {
		// Not wrapped.
		return r, nil
	}
	inner := &v3discoverypb.Resource{}
	if err := proto.Unmarshal(r.GetValue(), inner); err != nil {
		return nil, err
	}
	return inner.Resource, nil
}

// serviceStatus is the status of the update.
type serviceStatus int

const (
	// serviceStatusUnknown is the default state, before a watch is started for
	// the resource.
	serviceStatusUnknown serviceStatus = iota
	// serviceStatusRequested is when the watch is started, but before and
	// response is received.
	serviceStatusRequested
	// ServiceStatusNotExist is when the resource doesn't exist in
	// state-of-the-world responses (e.g. LDS and CDS), which means the resource
	// is removed by the management server.
	serviceStatusNotExist // Resource is removed in the server, in LDS/CDS.
	// ServiceStatusACKed is when the resource is ACKed.
	serviceStatusACKed
	// ServiceStatusNACKed is when the resource is NACKed.
	serviceStatusNACKed
)

// updateErrorMetadata is part of UpdateMetadata. It contains the error state
// when a response is NACKed.
type updateErrorMetadata struct {
	// Version is the version of the NACKed response.
	version string
	// Err contains why the response was NACKed.
	err error
	// Timestamp is when the NACKed response was received.
	timestamp time.Time
}

// updateWithMD contains the raw message of the update and the metadata,
// including version, raw message, timestamp.
//
// This is to be used for config dump and CSDS, not directly by users (like
// resolvers/balancers).
type updateWithMD struct {
	md  updateMetadata
	raw *anypb.Any
}
