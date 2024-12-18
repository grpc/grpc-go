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
	"errors"
	"fmt"
	"sync"

	"google.golang.org/grpc/internal/cache"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/xds/clients"
	"google.golang.org/grpc/xds/clients/xdsclient/xdsresource"
)

// ErrClientClosed is returned when the xDS client is closed.
var ErrClientClosed = errors.New("xds: the xDS client is closed")

var c *XDSClient
var mu sync.Mutex

// newClientImpl returns a new xdsClient with the given config.
func newXDSClient(config *Config) (*XDSClient, error) {
	ctx, cancel := context.WithCancel(context.Background())
	c = &XDSClient{
		done:               grpcsync.NewEvent(),
		authorities:        make(map[string]*authorityState),
		config:             config,
		watchExpiryTimeout: config.watchExpiryTimeOut,
		backoff:            config.streamBackOffTimeout,
		serializer:         grpcsync.NewCallbackSerializer(ctx),
		serializerClose:    cancel,
		transportBuilder:   config.TransportBuilder,
		resourceTypes:      config.ResourceTypes,
		xdsActiveChannels:  make(map[string]*channelState),
		xdsIdleChannels:    cache.NewTimeoutCache(config.idleChannelExpiryTimeout),
	}

	for name, cfg := range config.Authorities {
		// If server configs are specified in the authorities map, use that.
		// Else, use the top-level server configs.

		serverCfg := config.DefaultAuthority.XDSServers
		if len(cfg.XDSServers) >= 1 {
			serverCfg = cfg.XDSServers
		}
		serverCfgPtrs := make([]*clients.ServerConfig, len(serverCfg))
		for i, sc := range serverCfg {
			serverCfgPtrs[i] = &sc
		}
		c.authorities[name] = newAuthorityState(authorityBuildOptions{
			serverConfigs:    serverCfgPtrs,
			name:             name,
			serializer:       c.serializer,
			getChannelForADS: c.getChannelForADS,
			logPrefix:        clientPrefix(c),
		})
	}
	serverCfgPtrs := make([]*clients.ServerConfig, len(config.DefaultAuthority.XDSServers))
	for i, sc := range config.DefaultAuthority.XDSServers {
		serverCfgPtrs[i] = &sc
	}
	c.topLevelAuthority = newAuthorityState(authorityBuildOptions{
		serverConfigs:    serverCfgPtrs,
		name:             "",
		serializer:       c.serializer,
		getChannelForADS: c.getChannelForADS,
		logPrefix:        clientPrefix(c),
	})
	c.logger = prefixLogger(c)
	return c, nil
}

// getChannelForADS returns an xdsChannel for the given server configuration.
//
// If an xdsChannel exists for the given server configuration, it is returned.
// Else a new one is created. It also ensures that the calling authority is
// added to the set of interested authorities for the returned channel.
//
// It returns the xdsChannel and a function to release the calling authority's
// reference on the channel. The caller must call the cancel function when it is
// no longer interested in this channel.
//
// A non-nil error is returned if an xdsChannel was not created.
func (c *XDSClient) getChannelForADS(serverConfig *clients.ServerConfig, callingAuthorityState *authorityState) (*xdsChannel, func(), error) {
	if c.done.HasFired() {
		return nil, nil, ErrClientClosed
	}

	initLocked := func(s *channelState) {
		if c.logger.V(2) {
			c.logger.Infof("Adding authority %q to the set of interested authorities for channel [%p]", callingAuthorityState, s.channel)
		}
		s.interestedAuthorities[callingAuthorityState.name] = callingAuthorityState
	}
	deInitLocked := func(s *channelState) {
		if c.logger.V(2) {
			c.logger.Infof("Removing authority %q from the set of interested authorities for channel [%p]", callingAuthorityState, s.channel)
		}
		delete(s.interestedAuthorities, callingAuthorityState.name)
	}

	return c.getOrCreateChannel(serverConfig, initLocked, deInitLocked)
}

// getOrCreateChannel returns an xdsChannel for the given server configuration.
//
// If an active xdsChannel exists for the given server configuration, it is
// returned. If an idle xdsChannel exists for the given server configuration, it
// is revived from the idle cache and returned. Else a new one is created.
//
// The initLocked function runs some initialization logic before the channel is
// returned. This includes adding the calling authority to the set of interested
// authorities for the channel or incrementing the count of the number of LRS
// calls on the channel.
//
// The deInitLocked function runs some cleanup logic when the returned cleanup
// function is called. This involves removing the calling authority from the set
// of interested authorities for the channel or decrementing the count of the
// number of LRS calls on the channel.
//
// Both initLocked and deInitLocked are called with the c.channelsMu held.
//
// Returns the xdsChannel and a cleanup function to be invoked when the channel
// is no longer required. A non-nil error is returned if an xdsChannel was not
// created.
func (c *XDSClient) getOrCreateChannel(serverConfig *clients.ServerConfig, initLocked, deInitLocked func(*channelState)) (*xdsChannel, func(), error) {
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
	// idle cache and add it to the map of active channels, and return it.
	if s, ok := c.xdsIdleChannels.Remove(serverConfig.String()); ok {
		if c.logger.V(2) {
			c.logger.Infof("Reviving an xdsChannel from the idle cache for server config %q", serverConfig)
		}
		state := s.(*channelState)
		c.xdsActiveChannels[serverConfig.String()] = state
		initLocked(state)
		return state.channel, c.releaseChannel(serverConfig, state, deInitLocked), nil
	}

	if c.logger.V(2) {
		c.logger.Infof("Creating a new xdsChannel for server config %q", serverConfig)
	}

	// Create a new transport and create a new xdsChannel, and add it to the
	// map of xdsChannels.
	tr, err := c.transportBuilder.Build(*serverConfig)
	if err != nil {
		return nil, func() {}, fmt.Errorf("failed to create transport for server config %s: %v", serverConfig, err)
	}
	state := &channelState{
		parent:                c,
		serverConfig:          serverConfig,
		interestedAuthorities: map[string]*authorityState{},
	}
	channel, err := newXDSChannel(xdsChannelOpts{
		transport:          tr,
		serverConfig:       serverConfig,
		config:             c.config,
		resourceTypes:      c.resourceTypes,
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

// releaseChannel is a function that is called when a reference to an xdsChannel
// needs to be released. It handles the logic of moving the channel to an idle
// cache if there are no other active references, and closing the channel if it
// remains in the idle cache for the configured duration.
//
// The function takes the following parameters:
// - serverConfig: the server configuration for the xdsChannel
// - state: the state of the xdsChannel
// - deInitLocked: a function that performs any necessary cleanup for the xdsChannel
//
// The function returns another function that can be called to release the
// reference to the xdsChannel. This returned function is idempotent, meaning
// it can be called multiple times without any additional effect.
func (c *XDSClient) releaseChannel(serverConfig *clients.ServerConfig, state *channelState, deInitLocked func(*channelState)) func() {
	return grpcsync.OnceFunc(func() {
		c.channelsMu.Lock()
		defer c.channelsMu.Unlock()

		if c.logger.V(2) {
			c.logger.Infof("Received request to release a reference to an xdsChannel for server config %q", serverConfig)
		}
		deInitLocked(state)

		// The channel has active users. Do nothing and return.
		if len(state.interestedAuthorities) != 0 {
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

// Gets the authority for the given resource name.
//
// See examples in this section of the gRFC:
// https://github.com/grpc/proposal/blob/master/A47-xds-federation.md#bootstrap-config-changes
func (c *XDSClient) getAuthorityForResource(name *xdsresource.Name) *authorityState {
	// For new-style resource names, always lookup the authorities map. If the
	// name does not specify an authority, we will end up looking for an entry
	// in the map with the empty string as the key.
	if name.Scheme == xdsresource.FederationScheme {
		return c.authorities[name.Authority]
	}

	// For old-style resource names, we use the top-level authority if the name
	// does not specify an authority.
	if name.Authority == "" {
		return c.topLevelAuthority
	}
	return c.authorities[name.Authority]
}
