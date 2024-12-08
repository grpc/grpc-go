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

package xdsclient

import (
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/internal/cache"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/xds/clients"
	clientsinternal "google.golang.org/grpc/xds/clients/internal"
	"google.golang.org/grpc/xds/internal/xdsclient/transport/ads"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

// XDSClient represents an xDS client.
type XDSClient struct {
	config *Config

	done            *grpcsync.Event              // Fired when the client is closed.
	serializer      *grpcsync.CallbackSerializer // Serializer for invoking resource watcher callbacks.
	serializerClose func()                       // Function to close the serializer.
	logger          *grpclog.PrefixLogger        // Logger for this client.

	// The XDSClient owns a bunch of channels to individual xDS servers
	// specified in the config. Authorities acquire references
	// to these channels based on server configs within the authority config.
	// The XDSClient maintains a list of interested authorities for each of
	// these channels, and forwards updates from the channels to each of these
	// authorities.
	//
	// Once all references to a channel are dropped, the channel is moved to the
	// idle cache where it lives for a configured duration before being closed.
	// If the channel is required before the idle timeout fires, it is revived
	// from the idle cache and used.
	channelsMu        sync.Mutex
	xdsActiveChannels map[string]*channelState // Map from server config to in-use xdsChannels.
	xdsIdleChannels   *cache.TimeoutCache      // Map from server config to idle xdsChannels.
	closeCond         *sync.Cond
}

// channelState represents the state of an xDS channel.
//
// It receives callbacks for events on the underlying ADS stream and invokes
// corresponding callbacks on interested authoririties.
type channelState struct {
	parent       *XDSClient
	serverConfig *clients.ServerConfig

	// Access to the following fields should be protected by the parent's
	// channelsMu.
	channel               *clientsinternal.XDSChannel
	interestedAuthorities map[*clients.Authority]bool
}

func (cs *channelState) adsStreamFailure(err error) {
	if cs.parent.done.HasFired() {
		return
	}

	cs.parent.channelsMu.Lock()
	defer cs.parent.channelsMu.Unlock()
	for authority := range cs.interestedAuthorities {
		authority.adsStreamFailure(cs.serverConfig, err)
	}
}

func (cs *channelState) adsResourceUpdate(typ xdsresource.Type, updates map[string]ads.DataAndErrTuple, md xdsresource.UpdateMetadata, onDone func()) {
	if cs.parent.done.HasFired() {
		return
	}

	cs.parent.channelsMu.Lock()
	defer cs.parent.channelsMu.Unlock()

	if len(cs.interestedAuthorities) == 0 {
		onDone()
		return
	}

	authorityCnt := new(atomic.Int64)
	authorityCnt.Add(int64(len(cs.interestedAuthorities)))
	done := func() {
		if authorityCnt.Add(-1) == 0 {
			onDone()
		}
	}
	for authority := range cs.interestedAuthorities {
		authority.adsResourceUpdate(cs.serverConfig, typ, updates, md, done)
	}
}

func (cs *channelState) adsResourceDoesNotExist(typ xdsresource.Type, resourceName string) {
	if cs.parent.done.HasFired() {
		return
	}

	cs.parent.channelsMu.Lock()
	defer cs.parent.channelsMu.Unlock()
	for authority := range cs.interestedAuthorities {
		authority.adsResourceDoesNotExist(typ, resourceName)
	}
}

// New returns an xDS Client configured with provided config.
func New(config Config) (*XDSClient, error) {
	return &XDSClient{config: &config}, nil
}

// WatchResource uses xDS to discover the resource associated
// with the provided resource name. The resource type implementation
// determines how xDS responses are received, are deserialized
// and validated. Upon receipt of a response from the management
// server, an appropriate callback on the watcher is invoked.
func (c *XDSClient) WatchResource(rType ResourceType, resourceName string, watcher ResourceWatcher) (cancel func()) {
	return func() {}
}

// Close closes the xDS client and releases all resources.
// The caller is expected to invoke it once they are done
// using the client
func Close() error {
	return nil
}
