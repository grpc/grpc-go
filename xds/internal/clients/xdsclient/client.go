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

// Package xdsclient provides an implementation of the xDS client to
// communicate with xDS management servers.
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

// New returns a new xDS Client configured with provided config.
func New(config Config) (*XDSClient, error) {
	panic("unimplemented")
}

// WatchResource uses xDS to discover the resource associated with the provided
// resource name.
//   - resourceTypeURL is used to look up the resource type implementation
//     provided in the ResourceTypes map of the config which determines how
//     xDS responses are deserialized and validated after receiving from the xDS
//     management server.
//   - resourceName is the name of the resource to watch.
//   - resourceWatcher is used to notify the caller about updates to the resource
//     being watched. Upon receipt of a response from the management server, an
//     appropriate callback on the resourceWatcher is invoked.
func (c *XDSClient) WatchResource(resourceTypeURL string, resourceName string, resourceWatcher ResourceWatcher) (cancel func()) {
	panic("unimplemented")
}

// Close closes the xDS client and releases all resources. The caller is
// expected to invoke it once they are done using the client.
func (c *XDSClient) Close() error {
	panic("unimplemented")
}

// DumpResources returns the status and contents of all xDS resources from the
// xDS client.
func (c *XDSClient) DumpResources() *v3statuspb.ClientStatusResponse {
	panic("unimplemented")
}
