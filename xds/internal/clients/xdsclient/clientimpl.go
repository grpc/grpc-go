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

package xdsclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	v3statuspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	clientsinternal "google.golang.org/grpc/xds/internal/clients/internal"
	"google.golang.org/grpc/xds/internal/clients/internal/backoff"
	"google.golang.org/grpc/xds/internal/clients/internal/syncutil"
	"google.golang.org/grpc/xds/internal/clients/xdsclient/internal/xdsresource"
	"google.golang.org/protobuf/proto"
)

const (
	// NameForServer represents the value to be passed as name when creating an xDS
	// client from xDS-enabled gRPC servers. This is a well-known dedicated key
	// value, and is defined in gRFC A71.
	NameForServer = "#server"

	defaultWatchExpiryTimeout = 15 * time.Second
	name                      = "xds-client"
)

var (
	// ErrClientClosed is returned when the xDS client is closed.
	ErrClientClosed = errors.New("xds: the xDS client is closed")

	defaultExponentialBackoff = backoff.DefaultExponential.Backoff
)

// newClient returns a new XDSClient with the given config.
func newClient(config *Config, watchExpiryTimeout time.Duration, streamBackoff func(int) time.Duration, target string) (*XDSClient, error) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &XDSClient{
		target:             target,
		done:               syncutil.NewEvent(),
		authorities:        make(map[string]*authority),
		config:             config,
		watchExpiryTimeout: watchExpiryTimeout,
		backoff:            streamBackoff,
		serializer:         syncutil.NewCallbackSerializer(ctx),
		serializerClose:    cancel,
		transportBuilder:   config.TransportBuilder,
		resourceTypes:      newResourceTypeRegistry(config.ResourceTypes),
		xdsActiveChannels:  make(map[ServerConfig]*channelState),
	}

	for name, cfg := range config.Authorities {
		// If server configs are specified in the authorities map, use that.
		// Else, use the top-level server configs.
		serverCfg := config.Servers
		if len(cfg.XDSServers) >= 1 {
			serverCfg = cfg.XDSServers
		}
		c.authorities[name] = newAuthority(authorityBuildOptions{
			serverConfigs:    serverCfg,
			name:             name,
			serializer:       c.serializer,
			getChannelForADS: c.getChannelForADS,
			logPrefix:        clientPrefix(c),
			target:           target,
		})
	}
	c.topLevelAuthority = newAuthority(authorityBuildOptions{
		serverConfigs:    config.Servers,
		name:             "",
		serializer:       c.serializer,
		getChannelForADS: c.getChannelForADS,
		logPrefix:        clientPrefix(c),
		target:           target,
	})
	c.logger = prefixLogger(c)
	return c, nil
}

// Close closes the xDS client and releases all resources.
func (c *XDSClient) Close() {
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
	// into the XDSClient and will attempt to grab the lock. This will result
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

	c.serializerClose()
	<-c.serializer.Done()

	c.logger.Infof("Shutdown")
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
func (c *XDSClient) getChannelForADS(serverConfig *ServerConfig, callingAuthority *authority) (*xdsChannel, func(), error) {
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
func (c *XDSClient) getOrCreateChannel(serverConfig *ServerConfig, initLocked, deInitLocked func(*channelState)) (*xdsChannel, func(), error) {
	c.channelsMu.Lock()
	defer c.channelsMu.Unlock()

	if c.logger.V(2) {
		c.logger.Infof("Received request for a reference to an xdsChannel for server config %q", serverConfig)
	}

	// Use an existing channel, if one exists for this server config.
	if st, ok := c.xdsActiveChannels[*serverConfig]; ok {
		if c.logger.V(2) {
			c.logger.Infof("Reusing an existing xdsChannel for server config %q", serverConfig)
		}
		initLocked(st)
		return st.channel, c.releaseChannel(serverConfig, st, deInitLocked), nil
	}

	if c.logger.V(2) {
		c.logger.Infof("Creating a new xdsChannel for server config %q", serverConfig)
	}

	// Create a new transport and create a new xdsChannel, and add it to the
	// map of xdsChannels.
	tr, err := c.transportBuilder.Build(serverConfig.ServerIdentifier)
	if err != nil {
		return nil, func() {}, fmt.Errorf("xds: failed to create transport for server config %v: %v", serverConfig, err)
	}
	state := &channelState{
		parent:                c,
		serverConfig:          serverConfig,
		interestedAuthorities: make(map[*authority]bool),
	}
	channel, err := newXDSChannel(xdsChannelOpts{
		transport:          tr,
		serverConfig:       serverConfig,
		clientConfig:       c.config,
		eventHandler:       state,
		backoff:            c.backoff,
		watchExpiryTimeout: c.watchExpiryTimeout,
		logPrefix:          clientPrefix(c),
	})
	if err != nil {
		return nil, func() {}, fmt.Errorf("xds: failed to create a new channel for server config %v: %v", serverConfig, err)
	}
	state.channel = channel
	c.xdsActiveChannels[*serverConfig] = state
	initLocked(state)
	return state.channel, c.releaseChannel(serverConfig, state, deInitLocked), nil
}

// releaseChannel is a function that is called when a reference to an xdsChannel
// needs to be released. It handles closing channels with no active references.
//
// The function takes the following parameters:
// - serverConfig: the server configuration for the xdsChannel
// - state: the state of the xdsChannel
// - deInitLocked: a function that performs any necessary cleanup for the xdsChannel
//
// The function returns another function that can be called to release the
// reference to the xdsChannel. This returned function is idempotent, meaning
// it can be called multiple times without any additional effect.
func (c *XDSClient) releaseChannel(serverConfig *ServerConfig, state *channelState, deInitLocked func(*channelState)) func() {
	return sync.OnceFunc(func() {
		c.channelsMu.Lock()

		if c.logger.V(2) {
			c.logger.Infof("Received request to release a reference to an xdsChannel for server config %q", serverConfig)
		}
		deInitLocked(state)

		// The channel has active users. Do nothing and return.
		if state.lrsRefs != 0 || len(state.interestedAuthorities) != 0 {
			if c.logger.V(2) {
				c.logger.Infof("xdsChannel %p has other active references", state.channel)
			}
			c.channelsMu.Unlock()
			return
		}

		delete(c.xdsActiveChannels, *serverConfig)
		if c.logger.V(2) {
			c.logger.Infof("Closing xdsChannel [%p] for server config %s", state.channel, serverConfig)
		}
		channelToClose := state.channel
		c.channelsMu.Unlock()

		channelToClose.close()
	})
}

// DumpResources returns the status and contents of all xDS resources being
// watched by the xDS client.
func (c *XDSClient) DumpResources() ([]byte, error) {
	retCfg := c.topLevelAuthority.dumpResources()
	for _, a := range c.authorities {
		retCfg = append(retCfg, a.dumpResources()...)
	}

	nodeProto := clientsinternal.NodeProto(c.config.Node)
	nodeProto.ClientFeatures = []string{clientFeatureNoOverprovisioning, clientFeatureResourceWrapper}
	resp := &v3statuspb.ClientStatusResponse{}
	resp.Config = append(resp.Config, &v3statuspb.ClientConfig{
		Node:              nodeProto,
		GenericXdsConfigs: retCfg,
	})
	return proto.Marshal(resp)
}

// channelState represents the state of an xDS channel. It tracks the number of
// LRS references, the authorities interested in the channel, and the server
// configuration used for the channel.
//
// It receives callbacks for events on the underlying ADS stream and invokes
// corresponding callbacks on interested authorities.
type channelState struct {
	parent       *XDSClient
	serverConfig *ServerConfig

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

func (cs *channelState) adsResourceUpdate(typ ResourceType, updates map[string]dataAndErrTuple, md xdsresource.UpdateMetadata, onDone func()) {
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

func (cs *channelState) adsResourceDoesNotExist(typ ResourceType, resourceName string) {
	if cs.parent.done.HasFired() {
		return
	}

	cs.parent.channelsMu.Lock()
	defer cs.parent.channelsMu.Unlock()
	for authority := range cs.interestedAuthorities {
		authority.adsResourceDoesNotExist(typ, resourceName)
	}
}
