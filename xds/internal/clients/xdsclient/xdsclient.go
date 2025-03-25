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

import (
	"errors"
	"fmt"
	"time"

	v3statuspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	"google.golang.org/protobuf/proto"
)

// XDSClient is a client which queries a set of discovery APIs (collectively
// termed as xDS) on a remote management server, to discover
// various dynamic resources.
type XDSClient struct {
	client *clientImpl
}

// New returns a new xDS Client configured with the provided config.
func New(config Config) (*XDSClient, error) {
	switch {
	case config.Node.ID == "":
		return nil, errors.New("xdsclient: node ID is empty")
	case config.ResourceTypes == nil:
		return nil, errors.New("xdsclient: resource types map is nil")
	case config.TransportBuilder == nil:
		return nil, errors.New("xdsclient: transport builder is nil")
	case config.Authorities == nil && config.Servers == nil:
		return nil, errors.New("xdsclient: no servers or authorities specified")
	}

	for name, cfg := range config.Authorities {
		serverCfg := config.Servers
		if len(cfg.XDSServers) >= 1 {
			serverCfg = cfg.XDSServers
		}
		for _, sc := range serverCfg {
			if _, ok := sc.ServerIdentifier.Extensions.(interface{ Equal(any) bool }); sc.ServerIdentifier.Extensions != nil && !ok {
				return nil, fmt.Errorf("xdsclient: authority %s has a server config %s with non-nil ServerIdentifier.Extensions without implementing the `func (any) Equal(any) bool` method", name, serverConfigString(&sc))
			}
		}
	}

	for _, sc := range config.Servers {
		if _, ok := sc.ServerIdentifier.Extensions.(interface{ Equal(any) bool }); sc.ServerIdentifier.Extensions != nil && !ok {
			return nil, fmt.Errorf("xdsclient: one of the default servers has a server config %s with non-nil ServerIdentifier.Extensions without implementing the `func (any) Equal(any) bool` method", serverConfigString(&sc))
		}
	}

	clientImpl, err := newClientImpl(&config, defaultWatchExpiryTimeout, defaultExponentialBackoff, "xds-client")
	if err != nil {
		return nil, err
	}
	return &XDSClient{client: clientImpl}, nil
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
	return c.client.watchResource(typeURL, name, watcher)
}

// Close closes the xDS client.
func (c *XDSClient) Close() error {
	c.client.close()
	return nil
}

// DumpResources returns the status and contents of all xDS resources being
// watched by the xDS client.
func (c *XDSClient) DumpResources() ([]byte, error) {
	resp := &v3statuspb.ClientStatusResponse{}
	cfg := c.client.dumpResources()
	resp.Config = append(resp.Config, cfg)
	return proto.Marshal(resp)
}

// SetWatchExpiryTimeoutForTesting override the default watch expiry timeout
// with provided timeout value.
func (c *XDSClient) SetWatchExpiryTimeoutForTesting(watchExpiryTimeout time.Duration) {
	c.client.watchExpiryTimeout = watchExpiryTimeout
}
