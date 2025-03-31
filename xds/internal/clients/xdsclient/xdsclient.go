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
	"sync"
	"time"

	v3statuspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/xds/internal/clients"
	"google.golang.org/grpc/xds/internal/clients/internal/syncutil"
	"google.golang.org/protobuf/proto"
)

// XDSClient is a client which queries a set of discovery APIs (collectively
// termed as xDS) on a remote management server, to discover
// various dynamic resources.
type XDSClient struct {
	// The following fields are initialized at creation time and are read-only
	// after that, and therefore can be accessed without a mutex.
	done               *syncutil.Event              // Fired when the client is closed.
	topLevelAuthority  *authority                   // The top-level authority, used only for old-style names without an authority.
	authorities        map[string]*authority        // Map from authority names in config to authority struct.
	config             *Config                      // Complete xDS client configuration.
	watchExpiryTimeout time.Duration                // Expiry timeout for ADS watch.
	backoff            func(int) time.Duration      // Backoff for ADS and LRS stream failures.
	transportBuilder   clients.TransportBuilder     // Builder to create transports to xDS server.
	resourceTypes      *resourceTypeRegistry        // Registry of resource types, for parsing incoming ADS responses.
	serializer         *syncutil.CallbackSerializer // Serializer for invoking resource watcher callbacks.
	serializerClose    func()                       // Function to close the serializer.
	logger             *grpclog.PrefixLogger        // Logger for this client.
	target             string                       // The gRPC target for this client.

	// The XDSClient owns a bunch of channels to individual xDS servers
	// specified in the xDS client configuration. Authorities acquire references
	// to these channels based on server configs within the authority config.
	// The XDSClient maintains a list of interested authorities for each of
	// these channels, and forwards updates from the channels to each of these
	// authorities.
	//
	// Once all references to a channel are dropped, the channel is closed.
	channelsMu        sync.Mutex
	xdsActiveChannels map[ServerConfig]*channelState // Map from server config to in-use xdsChannels.
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

	client, err := newClient(&config, defaultWatchExpiryTimeout, defaultExponentialBackoff, "xds-client")
	if err != nil {
		return nil, err
	}
	return client, nil
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
	return c.watchResource(typeURL, name, watcher)
}

// Close closes the xDS client.
func (c *XDSClient) Close() error {
	c.close()
	return nil
}

// DumpResources returns the status and contents of all xDS resources being
// watched by the xDS client.
func (c *XDSClient) DumpResources() ([]byte, error) {
	resp := &v3statuspb.ClientStatusResponse{}
	cfg := c.dumpResources()
	resp.Config = append(resp.Config, cfg)
	return proto.Marshal(resp)
}

// SetWatchExpiryTimeoutForTesting override the default watch expiry timeout
// with provided timeout value.
func (c *XDSClient) SetWatchExpiryTimeoutForTesting(watchExpiryTimeout time.Duration) {
	c.watchExpiryTimeout = watchExpiryTimeout
}
