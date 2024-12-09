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
	"time"

	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/xds/clients"
)

const (
	defaultWatchExpiryTimeout       = 15 * time.Second
	defaultIdleChannelExpiryTimeout = 5 * time.Minute
)

var (
	defaultStreamBackoffFunc = backoff.DefaultExponential.Backoff
)

// Config contains xDS fields applicable to xDS client.
type Config struct {
	DefaultAuthority clients.Authority            // Required
	Authorities      map[string]clients.Authority // Required
	Node             clients.Node                 // Required
	TransportBuilder clients.TransportBuilder     // Required
	ResourceTypes    map[string]ResourceType      // Required

	// Below values will have default values but can be overridden for testing
	watchExpiryTimeOut       time.Duration
	idleChannelExpiryTimeout time.Duration
	streamBackOffTimeout     func(int) time.Duration
}

// NewConfig returns an xDS config configured with mandatory parameters.
func NewConfig(defaultAuthority clients.Authority, authorities map[string]clients.Authority, node clients.Node, transport clients.TransportBuilder, resourceTypes map[string]ResourceType) *Config {
	c := &Config{DefaultAuthority: defaultAuthority, Authorities: authorities, Node: node, TransportBuilder: transport, ResourceTypes: resourceTypes}
	c.idleChannelExpiryTimeout = defaultIdleChannelExpiryTimeout
	c.watchExpiryTimeOut = defaultWatchExpiryTimeout
	c.streamBackOffTimeout = defaultStreamBackoffFunc
	return c
}

func (c *Config) SetWatchExpiryTimeoutForTesting(d time.Duration) {
	c.watchExpiryTimeOut = d
}

func (c *Config) SetIdleChannelExpiryTimeoutForTesting(d time.Duration) {
	c.idleChannelExpiryTimeout = d
}

func (c *Config) SetStreamBackOffTimeoutForTesting(d func(int) time.Duration) {
	c.streamBackOffTimeout = d
}
