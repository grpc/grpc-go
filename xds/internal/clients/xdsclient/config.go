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
	"time"

	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/xds/internal/clients"
)

const (
	defaultWatchExpiryTimeout = 15 * time.Second
)

var (
	defaultStreamBackoffFunc = backoff.DefaultExponential.Backoff
)

// Config provides parameters for configuring the xDS client.
type Config struct {
	// Servers specifies a list of xDS servers to connect to. This field should
	// be used only for old-style names without an authority.
	Servers []clients.ServerConfig

	// Authorities is a map of authority names to authority configurations.
	// Each authority configuration contains list of xDS servers to connect to,
	// including fallbacks.
	Authorities map[string]clients.Authority

	// Node is the identity of the xDS client connecting to the xDS
	// management server.
	Node clients.Node

	// TransportBuilder is the implementation to create a communication channel
	// to an xDS management server.
	TransportBuilder clients.TransportBuilder

	// ResourceTypes is a map from resource type URLs to resource type
	// implementations. Each resource type URL uniquely identifies a specific
	// kind of xDS resource, and the corresponding resource type implementation
	// provides logic for parsing, validating, and processing resources of that
	// type.
	ResourceTypes map[string]ResourceType

	// Below values will have default values but can be overridden for testing

	// watchExpiryTimeOut is the duration after which a watch for a resource
	// will expire if no updates are received.
	watchExpiryTimeOut time.Duration

	// streamBackOffTimeout is a function that returns the backoff duration for
	// retrying failed xDS streams.
	streamBackOffTimeout func(int) time.Duration
}

// NewConfig returns a new xDS client config with provided parameters.
func NewConfig(servers []clients.ServerConfig, authorities map[string]clients.Authority, node clients.Node, transport clients.TransportBuilder, resourceTypes map[string]ResourceType) Config {
	c := Config{Servers: servers, Authorities: authorities, Node: node, TransportBuilder: transport, ResourceTypes: resourceTypes}
	c.watchExpiryTimeOut = defaultWatchExpiryTimeout
	c.streamBackOffTimeout = defaultStreamBackoffFunc
	return c
}
