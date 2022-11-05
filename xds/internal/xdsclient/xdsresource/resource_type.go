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
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/protobuf/types/known/anypb"
)

// XDSClient wraps the functionality provided by a full fledged xDS client which
// queries a set of discovery APIs on a remote management server, to discover
// various dynamic resources.
//
// The xdsclient package provides a concrete implementation of this interface.
type XDSClient interface {
	// WatchResource uses xDS to discover the resource associated with the
	// provided resource name. The resource type implementation determines how
	// xDS requests are sent out and how responses are deserialized and
	// validated. Upon receipt of a response from the management server, an
	// appropriate callback on the watcher is invoked.
	//
	// Most callers will not have a need to use this API directly. They will
	// instead use a resource-type-specific wrapper API provided by the relevant
	// resource type implementation.
	WatchResource(rType Type, resourceName string, watcher GenericResourceWatcher) (cancel func())
}

// GenericResourceWatcher wraps the callbacks invoked by the xDS client
// implementation for different events corresponding to the resource being
// watched.
//
// Most callers will not have a need to use this API directly. They will instead
// use a resource-type-specific watcher interfaces provided by the relevant
// resource type implementation.
type GenericResourceWatcher interface {
	// OnGenericResourceChanged is invoked when an update for the resource being
	// watched is received from the management server. The ResourceData
	// parameter needs to be type asserted to the appropriate type for the
	// resource being watched.
	OnGenericResourceChanged(ResourceData)

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

// TODO: Once the implementation is complete, rename this interface as
// ResourceType and get rid of the existing ResourceType enum.

// Type wraps all resource-type specific functionality. Each supported resource
// type will provide an implementation of this interface.
type Type interface {
	// V2TypeURL is the xDS type URL of this resource type for v2 transport.
	V2TypeURL() string

	// V3TypeURL is the xDS type URL of this resource type for v3 transport.
	V3TypeURL() string

	// TypeEnum is an enumerated value for this resource type. This can be used
	// for logging/debugging purposes, as well in cases where the resource type
	// is to be uniquely identified but the actual functionality provided by the
	// resource type is not required.
	//
	// TODO: once Type is renamed to ResourceType, rename ResourceType to
	// ResourceTypeEnum.
	TypeEnum() ResourceType

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
	isResourceData()

	// Equal returns true if the passed in resource data is equal to that of the
	// receiver.
	Equal(ResourceData) bool

	// ToJSON returns a JSON string representation of the resource data.
	ToJSON() string

	Raw() *anypb.Any
}

// DecodeOptions wraps the options required by ResourceType implementation for
// decoding configuration received from the xDS management server.
type DecodeOptions struct {
	// BootstrapConfig contains the bootstrap configuration passed to the
	// top-level xdsClient. This contains useful data for resource validation.
	BootstrapConfig *bootstrap.Config
	// Logger is to be used for emitting logs during the Decode operation.
	Logger *grpclog.PrefixLogger
}

// DecodeResult is the result of a decode operation.
type DecodeResult struct {
	// Name is the name of the resource being watched.
	Name string
	// Resource contains the configuration associated with the resource being
	// watched.
	Resource ResourceData
}
