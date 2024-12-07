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

	"google.golang.org/grpc/xds/clients"
)

// Config contains xDS fields applicable to xDS client.
// Config can be extended with more attributes in future.
type Config struct {
	XDSServers       []clients.ServerConfig       // Required
	Authorities      map[string]clients.Authority // Required
	Node             clients.Node                 // Required
	TransportBuilder clients.TransportBuilder     // Required

	Extensions any

	// Below values will have default values but can be overridden for testing
	watchExpiryTimeOut       time.Duration
	idleChannelExpiryTimeout time.Duration
	streamBackOffTimeout     time.Duration
}

// NewConfig returns an xDS config configured with mandatory parameters.
func NewConfig(xDSServers []clients.ServerConfig, authorities map[string]clients.Authority, node clients.Node, transport clients.TransportBuilder) (*Config, error) {
	return &Config{XDSServers: xDSServers, Authorities: authorities, Node: node, TransportBuilder: transport}, nil
}

func (c *Config) SetWatchExpiryTimeoutForTesting(d time.Duration) {
	c.watchExpiryTimeOut = d
}

func (c *Config) SetIdleChannelExpiryTimeoutForTesting(d time.Duration) {
	c.idleChannelExpiryTimeout = d
}

func (c *Config) SetStreamBackOffTimeoutForTesting(d time.Duration) {
	c.streamBackOffTimeout = d
}
