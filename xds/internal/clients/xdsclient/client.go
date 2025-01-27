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

// Package xdsclient provides implementation of the xDS client for enabling
// applications to communicate with xDS management servers.
//
// It allows applications to:
//   - Create xDS client instance with in-memory configurations.
//   - Register watches for named resources.
//   - Receive resources via the ADS (Aggregated Discovery Service) stream.
//
// This enables applications to dynamically discover and configure resources
// such as listeners, routes, clusters, and endpoints from an xDS management
// server.
package xdsclient

import (
	"errors"

	v3statuspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
)

// XDSClient is a full fledged client which queries a set of discovery APIs
// (collectively termed as xDS) on a remote management server, to discover
// various dynamic resources.
type XDSClient struct {
}

// New returns a new xDS Client configured with provided config.
func New(_ Config) (*XDSClient, error) {
	return nil, errors.New("xds: xDS client is not yet implemented")
}

// WatchResource uses xDS to discover the resource associated with the provided
// resource name. The resource type url look up the resource type
// implementation which determines how xDS responses are received, are
// deserialized and validated. Upon receipt of a response from the management
// server, an appropriate callback on the watcher is invoked.
//
// During a race (e.g. an xDS response is received while the user is calling
// cancel()), there's a small window where the callback can be called after
// the watcher is canceled. Callers need to handle this case.
func (c *XDSClient) WatchResource(_ string, _ string, _ ResourceWatcher) (cancel func()) {
	return nil
}

// Close closes the xDS client and releases all resources. The caller is
// expected to invoke it once they are done using the client.
func (c *XDSClient) Close() error {
	return nil
}

// DumpResources returns the status and contents of all xDS resources from the
// xDS client.
func (c *XDSClient) DumpResources() *v3statuspb.ClientStatusResponse {
	return nil
}
