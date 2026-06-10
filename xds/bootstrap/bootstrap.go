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
 *
 */

// Package bootstrap provides the functionality to register possible options
// for aspects of the xDS client through the bootstrap file.
//
// # Experimental
//
// Notice: This package is EXPERIMENTAL and may be changed or removed
// in a later release.
package bootstrap

import (
	"encoding/json"

	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
)

// channelCredsRegistry is a map from channel credential type name to
// ChannelCredential builder.
var channelCredsRegistry = make(map[string]ChannelCredentials)

// callCredsRegistry is a map from call credential type name to
// ChannelCredential builder.
var callCredsRegistry = make(map[string]CallCredentials)

var channelCredsByProtoURL = make(map[string]ChannelCredentialsWithProto)
var callCredsByProtoURL = make(map[string]CallCredentialsWithProto)

// ChannelCredentials interface encapsulates a credentials.Bundle builder
// that can be used for communicating with the xDS Management server.
type ChannelCredentials interface {
	// Build returns a credential bundle associated with this credential, and a
	// function to clean up any additional resources associated with this bundle
	// when it is no longer needed.
	Build(config json.RawMessage) (credentials.Bundle, func(), error)
	// Name returns the credential name associated with this credential.
	Name() string
}

// ChannelCredentialsWithProto extends ChannelCredentials to support
// protobuf config.
type ChannelCredentialsWithProto interface {
	ChannelCredentials
	// TypeURL returns the protobuf type URL this builder supports.
	TypeURL() string
	// NewProtoConfig returns a new empty proto config message instance.
	NewProtoConfig() proto.Message
	// MarshalProtoConfig marshals the protobuf configuration to its JSON format.
	MarshalProtoConfig(config proto.Message) (json.RawMessage, error)
}

// RegisterChannelCredentials registers ChannelCredentials used for connecting
// to the xDS management server.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple credentials are
// registered with the same name, the one registered last will take effect.
func RegisterChannelCredentials(c ChannelCredentials) {
	channelCredsRegistry[c.Name()] = c
	if pc, ok := c.(ChannelCredentialsWithProto); ok {
		channelCredsByProtoURL[pc.TypeURL()] = pc
	}
}

// GetChannelCredentials returns the credentials associated with a given name.
// If no credentials are registered with the name, nil will be returned.
func GetChannelCredentials(name string) ChannelCredentials {
	if c, ok := channelCredsRegistry[name]; ok {
		return c
	}

	return nil
}

// GetChannelCredentialsByProtoURL returns the credentials builder associated
// with a given proto type URL.
func GetChannelCredentialsByProtoURL(typeURL string) ChannelCredentialsWithProto {
	return channelCredsByProtoURL[typeURL]
}

// CallCredentials interface encapsulates a credentials.PerRPCCredentials
// builder that can be used for communicating with the xDS Management server.
type CallCredentials interface {
	// Build returns a PerRPCCredentials created from the provided
	// configuration, and a function to clean up any additional resources
	// associated with them when they are no longer needed.
	Build(config json.RawMessage) (credentials.PerRPCCredentials, func(), error)
	// Name returns the credential name associated with this credential.
	Name() string
}

// CallCredentialsWithProto extends CallCredentials to support protobuf config.
type CallCredentialsWithProto interface {
	CallCredentials
	// TypeURL returns the protobuf type URL this builder supports.
	TypeURL() string
	// NewProtoConfig returns a new empty proto config message instance.
	NewProtoConfig() proto.Message
	// MarshalProtoConfig marshals the protobuf configuration to its JSON format.
	MarshalProtoConfig(config proto.Message) (json.RawMessage, error)
}

// RegisterCallCredentials registers CallCredentials used for connecting
// to the xDS management server.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple credentials are
// registered with the same name, the one registered last will take effect.
func RegisterCallCredentials(c CallCredentials) {
	callCredsRegistry[c.Name()] = c
	if pc, ok := c.(CallCredentialsWithProto); ok {
		callCredsByProtoURL[pc.TypeURL()] = pc
	}
}

// GetCallCredentials returns the credentials associated with a given name.
// If no credentials are registered with the name, nil will be returned.
func GetCallCredentials(name string) CallCredentials {
	if c, ok := callCredsRegistry[name]; ok {
		return c
	}

	return nil
}

// GetCallCredentialsByProtoURL returns the credentials builder associated
// with a given proto type URL.
func GetCallCredentialsByProtoURL(typeURL string) CallCredentialsWithProto {
	return callCredsByProtoURL[typeURL]
}
