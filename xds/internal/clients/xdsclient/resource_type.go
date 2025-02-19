/*
 *
 * Copyright 2025 gRPC authors.
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
	"google.golang.org/grpc/xds/internal/clients"
	"google.golang.org/protobuf/types/known/anypb"
)

// ResourceType wraps all resource-type specific functionality. Each supported
// resource type needs to provide an implementation of the Decoder.
type ResourceType struct {
	// TypeURL is the xDS type URL of this resource type for the v3 xDS
	// protocol. This URL is used as the key to look up the corresponding
	// ResourceType implementation in the ResourceTypes map provided in the
	// Config.
	TypeURL string

	// TypeName is the shorter representation of the TypeURL to identify the
	// resource type. It can be used for logging/debugging purposes.
	TypeName string

	// AllResourcesRequiredInSotW indicates whether this resource type requires
	// that all resources be present in every SotW response from the server. If
	// true, a response that does not include a previously seen resource will
	// be interpreted as a deletion of that resource.
	AllResourcesRequiredInSotW bool

	// Decoder is used to deserialize and validate an xDS resource received
	// from the xDS management server.
	Decoder Decoder
}

// Decoder wraps the resource-type specific functionality for validation
// and deserialization.
type Decoder interface {
	// Decode deserializes and validates an xDS resource serialized inside the
	// provided `Any` proto, as received from the xDS management server.
	//
	// If protobuf deserialization fails or resource validation fails,
	// returns a non-nil error. Otherwise, returns a fully populated
	// DecodeResult.
	Decode(resource any, options DecodeOptions) (*DecodeResult, error)
}

// DecodeOptions wraps the options required by ResourceType implementation for
// decoding configuration received from the xDS management server.
type DecodeOptions struct {
	// Config contains the complete configuration passed to the xDS client.
	// This contains useful data for resource validation.
	Config *Config

	// ServerConfig contains the server config (from the above bootstrap
	// configuration) of the xDS server from which the current resource, for
	// which Decode() is being invoked, was received.
	ServerConfig *clients.ServerConfig
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
