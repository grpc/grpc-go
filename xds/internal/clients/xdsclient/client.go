//revive:disable:unused-parameter

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

// Package xdsclient provides an xDS (Extensible Discovery Service) client.
//
// It allows applications to:
//   - Create xDS client instances with in-memory configurations.
//   - Register watches for named resources.
//   - Receive resources via an ADS (Aggregated Discovery Service) stream.
//
// This enables applications to dynamically discover and configure resources
// such as listeners, routes, clusters, and endpoints from an xDS management
// server.
package xdsclient

import (
	v3statuspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
)

// XDSClient is a client which queries a set of discovery APIs (collectively
// termed as xDS) on a remote management server, to discover
// various dynamic resources.
type XDSClient struct {
}

// New returns a new xDS Client configured with the provided config.
func New(config Config) (*XDSClient, error) {
	panic("unimplemented")
}

// WatchResource starts watching the specified resource.
//
// typeURL must be present in the XDSClient's configured ResourceTypes. If
// typeURL is not present, watch will not be started. The ResourceType
// obtained from the ResourceTypes against the typeURL will be used to decode
// the matching resource when it is received, and the watcher will be called
// with the result.
//
// Cancel cancels the watch and prevents future calls to the watcher.
func (c *XDSClient) WatchResource(typeURL string, name string, watcher ResourceWatcher) (cancel func()) {
	panic("unimplemented")
}

// Close closes the xDS client.
func (c *XDSClient) Close() error {
	panic("unimplemented")
}

// DumpResources returns the status and contents of all xDS resources being
// watched by the xDS client.
func (c *XDSClient) DumpResources() *v3statuspb.ClientStatusResponse {
	panic("unimplemented")
}
