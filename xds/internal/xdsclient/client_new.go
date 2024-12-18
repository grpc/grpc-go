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
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/xds/bootstrap"
	xdsclientinternal "google.golang.org/grpc/xds/internal/xdsclient/internal"
	"google.golang.org/grpc/xds/internal/xdsclient/transport/ads"
	"google.golang.org/grpc/xds/internal/xdsclient/transport/grpctransport"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

// NameForServer represents the value to be passed as name when creating an xDS
// client from xDS-enabled gRPC servers. This is a well-known dedicated key
// value, and is defined in gRFC A71.
const NameForServer = "#server"

// newClientImpl returns a new xdsClient with the given config.
func newClientImpl(config *bootstrap.Config, watchExpiryTimeout time.Duration, streamBackoff func(int) time.Duration) (*clientImpl, error) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &clientImpl{
		done:               grpcsync.NewEvent(),
		authorities:        make(map[string]*authority),
		config:             config,
		watchExpiryTimeout: watchExpiryTimeout,
		backoff:            streamBackoff,
		serializer:         grpcsync.NewCallbackSerializer(ctx),
		serializerClose:    cancel,
		transportBuilder:   &grpctransport.Builder{},
		resourceTypes:      newResourceTypeRegistry(),
		xdsActiveChannels:  make(map[string]*channelState),
	}

	for name, cfg := range config.Authorities() {
		// If server configs are specified in the authorities map, use that.
		// Else, use the top-level server configs.
		serverCfg := config.XDSServers()
		if len(cfg.XDSServers) >= 1 {
			serverCfg = cfg.XDSServers
		}
		c.authorities[name] = newAuthority(authorityBuildOptions{
			serverConfigs:    serverCfg,
			name:             name,
			serializer:       c.serializer,
			getChannelForADS: c.getChannelForADS,
			logPrefix:        clientPrefix(c),
		})
	}
	c.topLevelAuthority = newAuthority(authorityBuildOptions{
		serverConfigs:    config.XDSServers(),
		name:             "",
		serializer:       c.serializer,
		getChannelForADS: c.getChannelForADS,
		logPrefix:        clientPrefix(c),
	})
	c.logger = prefixLogger(c)
	return c, nil
}

// OptionsForTesting contains options to configure xDS client creation for
// testing purposes only.
type OptionsForTesting struct {
	// Name is a unique name for this xDS client.
	Name string

	// Contents contain a JSON representation of the bootstrap configuration to
	// be used when creating the xDS client.
	Contents []byte

	// WatchExpiryTimeout is the timeout for xDS resource watch expiry. If
	// unspecified, uses the default value used in non-test code.
	WatchExpiryTimeout time.Duration

	// StreamBackoffAfterFailure is the backoff function used to determine the
	// backoff duration after stream failures.
	// If unspecified, uses the default value used in non-test code.
	StreamBackoffAfterFailure func(int) time.Duration
}

func init() {
	internal.TriggerXDSResourceNotFoundForTesting = triggerXDSResourceNotFoundForTesting
	xdsclientinternal.ResourceWatchStateForTesting = resourceWatchStateForTesting
	DefaultPool = &Pool{clients: make(map[string]*clientRefCounted)}
	config, err := bootstrap.GetConfiguration()
	if err != nil {
		logger.Warningf("Failed to read xDS bootstrap config from env vars:  %v", err)
		return
	}
	DefaultPool.config = config
}

func triggerXDSResourceNotFoundForTesting(client XDSClient, typ xdsresource.Type, name string) error {
	crc, ok := client.(*clientRefCounted)
	if !ok {
		return fmt.Errorf("xDS client is of type %T, want %T", client, &clientRefCounted{})
	}
	return crc.clientImpl.triggerResourceNotFoundForTesting(typ, name)
}

func resourceWatchStateForTesting(client XDSClient, typ xdsresource.Type, name string) (ads.ResourceWatchState, error) {
	crc, ok := client.(*clientRefCounted)
	if !ok {
		return ads.ResourceWatchState{}, fmt.Errorf("xDS client is of type %T, want %T", client, &clientRefCounted{})
	}
	return crc.clientImpl.resourceWatchStateForTesting(typ, name)
}
