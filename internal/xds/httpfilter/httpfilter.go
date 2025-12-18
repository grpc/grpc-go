/*
 *
 * Copyright 2021 gRPC authors.
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

// Package httpfilter contains the HTTPFilter interface and a registry for
// storing and retrieving their implementations.
package httpfilter

import (
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/protobuf/proto"
)

// FilterConfig represents an opaque data structure holding configuration for a
// filter.  Embed this interface to implement it.
type FilterConfig interface {
	isFilterConfig()
}

// FilterProvider is responsible for parsing an HTTP filter's configuration
// proto messages and creating new interceptor instances. Every filter must
// implement and register this interface.
//
// A FilterProvider may optionally implement either ClientInterceptorBuilder or
// ServerInterceptorBuilder or both, indicating it is capable of working on the
// client side or server side or both, respectively.
type FilterProvider interface {
	// TypeURLs are the proto message types supported by this provider.
	// A provider will be registered by each of its supported message types.
	TypeURLs() []string
	// ParseFilterConfig parses the provided configuration proto.Message from
	// the LDS configuration of this filter.  This may be an anypb.Any, a
	// udpa.type.v1.TypedStruct, or an xds.type.v3.TypedStruct for filters that
	// do not accept a custom type. The resulting FilterConfig will later be
	// passed to BuildClientInterceptor or BuildServerInterceptor.
	ParseFilterConfig(proto.Message) (FilterConfig, error)
	// ParseFilterConfigOverride parses the provided override configuration
	// proto.Message from the RDS override configuration of this filter.  This
	// may be an anypb.Any, a udpa.type.v1.TypedStruct, or an
	// xds.type.v3.TypedStruct for filters that do not accept a custom type.
	// The resulting FilterConfig will later be passed to BuildClientInterceptor
	// or BuildServerInterceptor.
	ParseFilterConfigOverride(proto.Message) (FilterConfig, error)
	// IsTerminal returns whether interceptors built from this Provider are
	// terminal or not (i.e. it must be last filter in the filter chain).
	IsTerminal() bool
}

// ClientInterceptorBuilder constructs a Client Interceptor.  If this type is
// implemented by a FilterProvider, it is capable of working on a client.
type ClientInterceptorBuilder interface {
	// BuildClientInterceptor uses the given FilterConfigs to produce an HTTP
	// filter interceptor for clients. config will always be non-nil, but
	// override may be nil if no override config exists for the filter.
	//
	// The provider is free to use the name parameter to identify the filter
	// instance being created, and share state across multiple instances if
	// needed.
	//
	// It is valid for this method to return a nil Interceptor and a nil error.
	// In this case, the RPC will not be intercepted by this filter.
	//
	// Callers must ensure that the returned cancel func is called when the
	// interceptor is no longer needed, to allow the filter to clean up any
	// resources.
	BuildClientInterceptor(name string, config, override FilterConfig) (i iresolver.ClientInterceptor, cancel func(), err error)
}

// ServerInterceptorBuilder constructs a Server Interceptor.  If this type is
// implemented by a FilterProvider, it is capable of working on a server.
type ServerInterceptorBuilder interface {
	// BuildServerInterceptor uses the given FilterConfigs to produce
	// an HTTP filter interceptor for servers. config will always be non-nil,
	// but override may be nil if no override config exists for the filter.
	//
	// The provider is free to use the name parameter to identify the filter
	// instance being created, and share state across multiple instances if
	// needed.
	//
	// It is valid for this method to return a nil Interceptor and a nil error.
	// In this case, the RPC will not be intercepted by this filter.
	//
	// Callers must ensure that the returned cancel func is called when the
	// interceptor is no longer needed, to allow the filter to clean up any
	// resources.
	BuildServerInterceptor(name string, config, override FilterConfig) (i iresolver.ServerInterceptor, cancel func(), err error)
}

var (
	// m is a map from scheme to filter provider.
	m = make(map[string]FilterProvider)
)

// Register registers the HTTP filter provider to the filter map. b.TypeURLs()
// will be used as the types for this filter.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple filters are
// registered with the same type URL, the one registered last will take effect.
func Register(b FilterProvider) {
	for _, u := range b.TypeURLs() {
		m[u] = b
	}
}

// UnregisterForTesting unregisters the HTTP Filter for testing purposes.
func UnregisterForTesting(typeURL string) {
	delete(m, typeURL)
}

// Get returns the provider registered with typeURL.
//
// If no provider is registered with typeURL, nil will be returned.
func Get(typeURL string) FilterProvider {
	return m[typeURL]
}
