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
 *
 */

package xdsclient

import (
	"google.golang.org/grpc/xds"
	"google.golang.org/protobuf/types/known/anypb"
)

// ResourceType wraps all resource-type specific functionality. Each supported
// resource type will provide an implementation of this interface.
type ResourceType interface {
	// TypeURL is the xDS type URL of this resource type for v3 transport.
	TypeURL() string

	// TypeName identifies resources in a transport protocol agnostic way.
	TypeName() string

	// AllResourcesRequiredInSotW indicates whether this resource type requires
	// that all resources be present in every SotW response from the server. If
	// true, a response that does not include a previously seen resource will
	// be interpreted as a deletion of that resource.
	AllResourcesRequiredInSotW() bool

	// Decode deserializes and validates an xDS resource serialized inside the
	// provided `any`, as received from the xDS management server.
	Decode(*DecodeOptions, any) (*DecodeResult, error)
}

// DecodeOptions wraps the options required by ResourceType implementation for
// decoding configuration received from the xDS management server.
type DecodeOptions struct {
	Config       *Config
	ServerConfig *xds.ServerConfig
}

// DecodeResult is the result of a decode operation.
type DecodeResult struct {
	// Name is the name of the resource being watched.
	Name string
	// Resource contains the configuration associated with the resource being
	// watched.
	Resource ResourceData
}

// ResourceData contains the configuration data sent by the xDS management
// server, associated with the resource being watched. Every resource type must
// provide an implementation of this interface to represent the configuration
// received from the xDS management server.
type ResourceData interface {
	// RawEqual returns true if the passed in resource data is equal to that of
	// the receiver, based on the underlying raw protobuf message.
	RawEqual(ResourceData) bool

	// Raw returns the underlying raw protobuf form of the resource.
	Raw() *anypb.Any
}
