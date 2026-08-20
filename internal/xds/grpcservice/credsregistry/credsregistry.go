/*
 *
 * Copyright 2026 gRPC authors.
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

// Package credsregistry contains registries of builders for the channel and
// call credentials that may be configured in a GrpcService proto, keyed by
// the proto type URL of their configuration (gRFC A102).
package credsregistry

import (
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/protobuf/types/known/anypb"
)

var (
	// channelCredsBuilders is a map from proto type URL to
	// ChannelCredsBuilder.
	channelCredsBuilders = make(map[string]ChannelCredsBuilder)
	// callCredsBuilders is a map from proto type URL to CallCredsBuilder.
	callCredsBuilders = make(map[string]CallCredsBuilder)
)

// ChannelCredsBuilder builds channel credentials from a GrpcService channel
// credentials plugin config.
type ChannelCredsBuilder interface {
	// Build creates a credentials bundle from the given plugin config. The
	// bootstrap configuration is available to builders that reference
	// resources configured there (e.g. certificate provider instances);
	// builders that do not need it ignore it. The returned function releases
	// the resources held by the bundle when it is no longer needed.
	Build(config *anypb.Any, bc *bootstrap.Config) (credentials.Bundle, func(), error)
}

// CallCredsBuilder builds call credentials from a GrpcService call
// credentials plugin config.
type CallCredsBuilder interface {
	// Build creates per-RPC credentials from the given plugin config. The
	// returned function releases the resources held by the credentials when
	// they are no longer needed.
	Build(config *anypb.Any) (credentials.PerRPCCredentials, func(), error)
}

// RegisterChannelCredsBuilder registers the builder for the given proto type
// URL. Must be called at init time. Not thread safe.
func RegisterChannelCredsBuilder(typeURL string, b ChannelCredsBuilder) {
	channelCredsBuilders[typeURL] = b
}

// GetChannelCredsBuilder returns the builder registered for the given proto
// type URL, or nil if there is none.
func GetChannelCredsBuilder(typeURL string) ChannelCredsBuilder {
	return channelCredsBuilders[typeURL]
}

// RegisterCallCredsBuilder registers the builder for the given proto type
// URL. Must be called at init time. Not thread safe.
func RegisterCallCredsBuilder(typeURL string, b CallCredsBuilder) {
	callCredsBuilders[typeURL] = b
}

// GetCallCredsBuilder returns the builder registered for the given proto type
// URL, or nil if there is none.
func GetCallCredsBuilder(typeURL string) CallCredsBuilder {
	return callCredsBuilders[typeURL]
}
