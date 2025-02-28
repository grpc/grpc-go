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

// Package xdsclient provides an xDS (* Discovery Service) client.
//
// It allows applications to:
//   - Create xDS client instances with in-memory configurations.
//   - Register watches for named resources.
//   - Receive resources via an ADS (Aggregated Discovery Service) stream.
//   - Register watches for named resources (e.g. listeners, routes, or
//     clusters).
//
// This enables applications to dynamically discover and configure resources
// such as listeners, routes, clusters, and endpoints from an xDS management
// server.
package xdsclient

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
// typeURL specifies the resource type implementation to use. The watch fails
// if there is no resource type implementation for the given typeURL. See the
// ResourceTypes field in the Config struct used to create the XDSClient.
//
// The returned function cancels the watch and prevents future calls to the
// watcher.
func (c *XDSClient) WatchResource(typeURL, name string, watcher ResourceWatcher) (cancel func()) {
	panic("unimplemented")
}

// Close closes the xDS client.
func (c *XDSClient) Close() error {
	panic("unimplemented")
}

// DumpResources returns the status and contents of all xDS resources being
// watched by the xDS client.
func (c *XDSClient) DumpResources() []byte {
	panic("unimplemented")
}
