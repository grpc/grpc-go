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
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/internal/cache"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/xds/clients"
	"google.golang.org/grpc/xds/clients/xdsclient/xdsresource"
)

// XDSClient is the real implementation of the xds client. The exported Client
// is a wrapper of this struct.
type XDSClient struct {
	// The following fields are initialized at creation time and are read-only
	// after that, and therefore can be accessed without a mutex.
	done               *grpcsync.Event              // Fired when the client is closed.
	topLevelAuthority  *authorityState              // The top-level authority, used only for old-style names without an authority.
	authorities        map[string]*authorityState   // Map from authority names in bootstrap to authority struct.
	config             *Config                      // Complete configuration.
	watchExpiryTimeout time.Duration                // Expiry timeout for ADS watch.
	backoff            func(int) time.Duration      // Backoff for ADS and LRS stream failures.
	transportBuilder   clients.TransportBuilder     // Builder to create transports to xDS server.
	resourceTypes      map[string]ResourceType      // Registry of resource types, for parsing incoming ADS responses.
	serializer         *grpcsync.CallbackSerializer // Serializer for invoking resource watcher callbacks.
	serializerClose    func()                       // Function to close the serializer.
	logger             *grpclog.PrefixLogger        // Logger for this client.

	// The clientImpl owns a bunch of channels to individual xDS servers
	// specified in the bootstrap configuration. Authorities acquire references
	// to these channels based on server configs within the authority config.
	// The clientImpl maintains a list of interested authorities for each of
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

// New returns an xDS Client configured with provided config.
func New(config Config) (*XDSClient, error) {
	return newXDSClient(&config)
}

// WatchResource uses xDS to discover the resource associated
// with the provided resource name. The resource type implementation
// determines how xDS responses are received, are deserialized
// and validated. Upon receipt of a response from the management
// server, an appropriate callback on the watcher is invoked.
func (c *XDSClient) WatchResource(rType ResourceType, resourceName string, watcher ResourceWatcher) (cancel func()) {
	// Return early if the client is already closed.
	if c == nil || c.done.HasFired() {
		logger.Warningf("Watch registered for name %q of type %q, but client is closed", rType.TypeName(), resourceName)
		return func() {}
	}

	if _, ok := c.resourceTypes[rType.TypeURL()]; !ok {
		logger.Warningf("Watch registered for name %q of type %q, but resource type implementation is not present", rType.TypeName(), resourceName)
		c.serializer.TrySchedule(func(context.Context) {
			watcher.OnError(fmt.Errorf("resource type %q not found in provided resource type implementations", rType.TypeURL()), func() {})
		})
		return func() {}
	}

	n := xdsresource.ParseName(resourceName)
	a := c.getAuthorityForResource(n)
	if a == nil {
		logger.Warningf("Watch registered for name %q of type %q, authority %q is not found", rType.TypeName(), resourceName, n.Authority)
		c.serializer.TrySchedule(func(context.Context) {
			watcher.OnError(fmt.Errorf("authority %q not found in bootstrap config for resource %q", n.Authority, resourceName), func() {})
		})
		return func() {}
	}
	// The watchResource method on the authority is invoked with n.String()
	// instead of resourceName because n.String() canonicalizes the given name.
	// So, two resource names which don't differ in the query string, but only
	// differ in the order of context params will result in the same resource
	// being watched by the authority.
	return a.watchResource(rType, n.String(), watcher)
}

// Close closes the xDS client and releases all resources.
// The caller is expected to invoke it once they are done
// using the client
func (c *XDSClient) Close() error {
	if c.done.HasFired() {
		return nil
	}
	c.done.Fire()

	c.topLevelAuthority.close()
	for _, a := range c.authorities {
		a.close()
	}

	// Channel close cannot be invoked with the lock held, because it can race
	// with stream failure happening at the same time. The latter will callback
	// into the clientImpl and will attempt to grab the lock. This will result
	// in a deadlock. So instead, we release the lock and wait for all active
	// channels to be closed.
	var channelsToClose []*xdsChannel
	c.channelsMu.Lock()
	for _, cs := range c.xdsActiveChannels {
		channelsToClose = append(channelsToClose, cs.channel)
	}
	c.xdsActiveChannels = nil
	c.channelsMu.Unlock()
	for _, c := range channelsToClose {
		c.close()
	}

	// Similarly, closing idle channels cannot be done with the lock held, for
	// the same reason as described above.  So, we clear the idle cache in a
	// goroutine and use a condition variable to wait on the condition that the
	// idle cache has zero entries. The Wait() method on the condition variable
	// releases the lock and blocks the goroutine until signaled (which happens
	// when an idle channel is removed from the cache and closed), and grabs the
	// lock before returning.
	c.channelsMu.Lock()
	c.closeCond = sync.NewCond(&c.channelsMu)
	go c.xdsIdleChannels.Clear(true)
	for c.xdsIdleChannels.Len() > 0 {
		c.closeCond.Wait()
	}
	c.channelsMu.Unlock()

	c.serializerClose()
	<-c.serializer.Done()

	c.logger.Infof("Shutdown")
	return nil
}
