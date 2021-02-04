/*
 *
 * Copyright 2020 gRPC authors.
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

package client

import (
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/xds/internal/client/bootstrap"
)

const defaultWatchExpiryTimeout = 15 * time.Second

// This is the Client returned by New(). It contains one client implementation,
// and maintains the refcount.
var singletonClient = &Client{}

// To override in tests.
var bootstrapNewConfig = bootstrap.NewConfig

// Client is a full fledged gRPC client which queries a set of discovery APIs
// (collectively termed as xDS) on a remote management server, to discover
// various dynamic resources.
//
// The xds client is a singleton. It will be shared by the xds resolver and
// balancer implementations, across multiple ClientConns and Servers.
type Client struct {
	*clientImpl

	// This mu protects all the fields, including the embedded clientImpl above.
	mu       sync.Mutex
	refCount int
}

// New returns a new xdsClient configured by the bootstrap file specified in env
// variable GRPC_XDS_BOOTSTRAP.
func New() (*Client, error) {
	singletonClient.mu.Lock()
	defer singletonClient.mu.Unlock()
	// If the client implementation was created, increment ref count and return
	// the client.
	if singletonClient.clientImpl != nil {
		singletonClient.refCount++
		return singletonClient, nil
	}

	// Create the new client implementation.
	config, err := bootstrapNewConfig()
	if err != nil {
		return nil, fmt.Errorf("xds: failed to read bootstrap file: %v", err)
	}
	c, err := newWithConfig(config, defaultWatchExpiryTimeout)
	if err != nil {
		return nil, err
	}

	singletonClient.clientImpl = c
	singletonClient.refCount++
	return singletonClient, nil
}

// Close closes the client. It does ref count of the xds client implementation,
// and closes the gRPC connection to the management server when ref count
// reaches 0.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refCount--
	if c.refCount == 0 {
		c.clientImpl.Close()
		// Set clientImpl back to nil. So if New() is called after this, a new
		// implementation will be created.
		c.clientImpl = nil
	}
}

// NewWithConfigForTesting is exported for testing only.
func NewWithConfigForTesting(config *bootstrap.Config, watchExpiryTimeout time.Duration) (*Client, error) {
	cl, err := newWithConfig(config, watchExpiryTimeout)
	if err != nil {
		return nil, err
	}
	return &Client{clientImpl: cl, refCount: 1}, nil
}
