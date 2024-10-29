/*
 *
 * Copyright 2022 gRPC authors.
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
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/internal/cache"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/transport"
	"google.golang.org/grpc/xds/internal/xdsclient/transport/ads"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

var _ XDSClient = &clientImpl{}

// ErrClientClosed is returned when the xDS client is closed.
var ErrClientClosed = errors.New("xds: the xDS client is closed")

// clientImpl is the real implementation of the xds client. The exported Client
// is a wrapper of this struct with a ref count.
type clientImpl struct {
	// The following fields are initialized at creation time and are read-only
	// after that, and therefore can be accessed without a mutex.
	done               *grpcsync.Event              // Fired when the client is closed.
	topLevelAuthority  *authority                   // The top-level authority, used only for old-style names without an authority.
	authorities        map[string]*authority        // Map from authority names in bootstrap to authority struct.
	config             *bootstrap.Config            // Complete bootstrap configuration.
	watchExpiryTimeout time.Duration                // Expiry timeout for ADS watch.
	backoff            func(int) time.Duration      // Backoff for ADS and LRS stream failures.
	transportBuilder   transport.Builder            // Builder to create transports to the xDS server.
	resourceTypes      *resourceTypeRegistry        // Registry of resource types, for parsing incoming ADS responses.
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

// channelState represents the state of an xDS channel. It tracks the number of
// LRS references, the authorities interested in the channel, and the server
// configuration used for the channel.
//
// It receives callbacks for events on the underlying ADS stream and invokes
// corresponding callbacks on interested authoririties.
type channelState struct {
	parent       *clientImpl
	serverConfig *bootstrap.ServerConfig

	// Access to the following fields should be protected by the parent's
	// channelsMu.
	channel               *xdsChannel
	lrsRefs               int
	interestedAuthorities map[*authority]bool
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

// BootstrapConfig returns the configuration read from the bootstrap file.
// Callers must treat the return value as read-only.
func (c *clientImpl) BootstrapConfig() *bootstrap.Config {
	return c.config
}

// close closes the xDS client and releases all resources.
func (c *clientImpl) close() {
	if c.done.HasFired() {
		return
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
	// channel to be closed.
	var channelsToClose []*xdsChannel
	c.channelsMu.Lock()
	for _, cs := range c.xdsActiveChannels {
		channelsToClose = append(channelsToClose, cs.channel)
	}
	c.xdsActiveChannels = nil
	c.channelsMu.Unlock()

	var wg sync.WaitGroup
	for _, c := range channelsToClose {
		wg.Add(1)
		go func(c *xdsChannel) {
			defer wg.Done()
			c.close()
		}(c)
	}
	wg.Wait()

	// Similarly, closing idle channels cannot be done with the lock held, for
	// the same reason as described above.  So, we clear the idle cache in a
	// goroutine and use a condition variable to wait on the condition that the
	// idle cache has zero entries. The Wait() method on the condition variable
	// release the lock and blocks the goroutine until signaled (which happens
	// when an idle channel is removed from the cache and closed).
	c.channelsMu.Lock()
	c.closeCond = sync.NewCond(&c.channelsMu)
	go c.xdsIdleChannels.Clear(true)
	for c.xdsIdleChannels.Len() > 0 {
		c.closeCond.Wait()
	}
	c.channelsMu.Unlock()

	c.serializerClose()
	<-c.serializer.Done()

	for _, s := range c.config.XDSServers() {
		for _, f := range s.Cleanups() {
			f()
		}
	}
	for _, a := range c.config.Authorities() {
		for _, s := range a.XDSServers {
			for _, f := range s.Cleanups() {
				f()
			}
		}
	}
	c.logger.Infof("Shutdown")
}

func (c *clientImpl) getChannelForADS(serverConfig *bootstrap.ServerConfig, callingAuthority *authority) (*xdsChannel, func(), error) {
	if c.done.HasFired() {
		return nil, nil, ErrClientClosed
	}

	initLocked := func(s *channelState) {
		if c.logger.V(2) {
			c.logger.Infof("Adding authority %q to the set of interested authorities for channel [%p]", callingAuthority.name, s.channel)
		}
		s.interestedAuthorities[callingAuthority] = true
	}
	deInitLocked := func(s *channelState) {
		if c.logger.V(2) {
			c.logger.Infof("Removing authority %q from the set of interested authorities for channel [%p]", callingAuthority.name, s.channel)
		}
		delete(s.interestedAuthorities, callingAuthority)
	}

	return c.getOrCreateChannel(serverConfig, initLocked, deInitLocked)
}

func (c *clientImpl) getChannelForLRS(serverConfig *bootstrap.ServerConfig) (*xdsChannel, func(), error) {
	if c.done.HasFired() {
		return nil, nil, ErrClientClosed
	}

	initLocked := func(s *channelState) { s.lrsRefs++ }
	deInitLocked := func(s *channelState) { s.lrsRefs-- }

	return c.getOrCreateChannel(serverConfig, initLocked, deInitLocked)
}

func (c *clientImpl) getOrCreateChannel(serverConfig *bootstrap.ServerConfig, initLocked, deInitLocked func(*channelState)) (*xdsChannel, func(), error) {
	c.channelsMu.Lock()
	defer c.channelsMu.Unlock()

	if c.logger.V(2) {
		c.logger.Infof("Received request for a reference to an xdsChannel for server config %q", serverConfig)
	}

	// Use an active channel, if one exists for this server config.
	if state, ok := c.xdsActiveChannels[serverConfig.String()]; ok {
		if c.logger.V(2) {
			c.logger.Infof("Reusing an active xdsChannel for server config %q", serverConfig)
		}
		initLocked(state)
		return state.channel, c.releaseChannel(serverConfig, state, deInitLocked), nil
	}

	// If an idle channel exists for this server config, remove it from the
	// idle cache and add it to the map of active channels, add return it.
	if state, ok := c.xdsIdleChannels.Remove(serverConfig.String()); ok {
		if c.logger.V(2) {
			c.logger.Infof("Reviving an xdsChannel from the idle cache for server config %q", serverConfig)
		}
		state, _ := state.(*channelState)
		c.xdsActiveChannels[serverConfig.String()] = state
		initLocked(state)
		return state.channel, c.releaseChannel(serverConfig, state, deInitLocked), nil
	}

	if c.logger.V(2) {
		c.logger.Infof("Creating a new xdsChannel for server config %q", serverConfig)
	}

	// Create a new transport and create a new xdsChannel, and add it to the
	// map of xdsChannels.
	tr, err := c.transportBuilder.Build(transport.BuildOptions{ServerConfig: serverConfig})
	if err != nil {
		return nil, func() {}, fmt.Errorf("failed to create transport for server config %s: %v", serverConfig, err)
	}
	state := &channelState{
		parent:                c,
		serverConfig:          serverConfig,
		interestedAuthorities: make(map[*authority]bool),
	}
	channel, err := newXDSChannel(xdsChannelOpts{
		transport:          tr,
		serverConfig:       serverConfig,
		bootstrapConfig:    c.config,
		resourceTypeGetter: c.resourceTypes.get,
		eventHandler:       state,
		backoff:            c.backoff,
		watchExpiryTimeout: c.watchExpiryTimeout,
		logPrefix:          clientPrefix(c),
	})
	if err != nil {
		return nil, func() {}, fmt.Errorf("failed to create xdsChannel for server config %s: %v", serverConfig, err)
	}
	state.channel = channel
	c.xdsActiveChannels[serverConfig.String()] = state
	initLocked(state)
	return state.channel, c.releaseChannel(serverConfig, state, deInitLocked), nil
}

func (c *clientImpl) releaseChannel(serverConfig *bootstrap.ServerConfig, state *channelState, deInitLocked func(*channelState)) func() {
	return grpcsync.OnceFunc(func() {
		c.channelsMu.Lock()
		defer c.channelsMu.Unlock()

		if c.logger.V(2) {
			c.logger.Infof("Received request to release a reference to an xdsChannel for server config %q", serverConfig)
		}
		deInitLocked(state)

		// The channel has active users. Do nothing and return.
		if state.lrsRefs != 0 || len(state.interestedAuthorities) != 0 {
			if c.logger.V(2) {
				c.logger.Infof("xdsChannel %p has other active references", state.channel)
			}
			return
		}

		// Move the channel to the idle cache instead of closing
		// immediately. If the channel remains in the idle cache for
		// the configured duration, it will get closed.
		delete(c.xdsActiveChannels, serverConfig.String())
		if c.logger.V(2) {
			c.logger.Infof("Moving xdsChannel [%p] for server config %s to the idle cache", state.channel, serverConfig)
		}

		// The idle cache expiry timeout results in the channel getting
		// closed in another serializer callback.
		c.xdsIdleChannels.Add(serverConfig.String(), state, grpcsync.OnceFunc(func() {
			c.channelsMu.Lock()
			channelToClose := state.channel
			c.channelsMu.Unlock()

			if c.logger.V(2) {
				c.logger.Infof("Idle cache expiry timeout fired for xdsChannel [%p] for server config %s", state.channel, serverConfig)
			}
			channelToClose.close()

			// If the channel is being closed as a result of the xDS client
			// being closed, closeCond is non-nil and we need to signal from
			// here to unblock Close().  Holding the lock is not necessary
			// to call Signal() on a condition variable. But the field
			// `c.closeCond` needs to guarded by the lock, which is why we
			// acquire it here.
			c.channelsMu.Lock()
			if c.closeCond != nil {
				c.closeCond.Signal()
			}
			c.channelsMu.Unlock()
		}))
	})
}
