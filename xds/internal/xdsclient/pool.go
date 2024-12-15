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
	"fmt"
	"sync"

	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/xds/bootstrap"
)

var (
	// DefaultPool is the default pool for xDS clients. It is created at the
	// init time by reading bootstrap configuration from env vars.
	DefaultPool *Pool
)

// Pool represents pool of xDS clients who share the same bootstrap
// configuration.
//
// Note that mu should ideally only have to guard clients. But here, we need
// it to guard config as well since SetFallbackBootstrapConfig writes to
// config.
type Pool struct {
	mu      sync.Mutex
	clients map[string]*clientRefCounted
	config  *bootstrap.Config
}

// NewPool creates a new xDS client pool with the given bootstrap contents.
func NewPool(config *bootstrap.Config) *Pool {
	return &Pool{
		clients: make(map[string]*clientRefCounted),
		config:  config,
	}
}

// NewClient returns an xDS client with the given name from the pool. If the
// client doesn't already exist, it creates a new xDS client and adds it to the
// pool.
//
// The second return value represents a close function which the caller is
// expected to invoke once they are done using the client.  It is safe for the
// caller to invoke this close function multiple times.
func (p *Pool) NewClient(name string) (XDSClient, func(), error) {
	if p.config == nil {
		return nil, nil, fmt.Errorf("bootstrap configuration not set in the pool")
	}
	return p.newRefCounted(name, p.config, defaultWatchExpiryTimeout, backoff.DefaultExponential.Backoff)
}

// NewClientForTesting returns an xDS client configured with the provided
// options from the pool. If the client doesn't already exist, it creates a new
// xDS client and adds it to the pool.
//
// The second return value represents a close function which the caller is
// expected to invoke once they are done using the client.  It is safe for the
// caller to invoke this close function multiple times.
//
// # Testing Only
//
// This function should ONLY be used for testing purposes.
func (p *Pool) NewClientForTesting(opts OptionsForTesting) (XDSClient, func(), error) {
	if opts.Name == "" {
		return nil, nil, fmt.Errorf("opts.Name field must be non-empty")
	}
	if opts.WatchExpiryTimeout == 0 {
		opts.WatchExpiryTimeout = defaultWatchExpiryTimeout
	}
	if opts.StreamBackoffAfterFailure == nil {
		opts.StreamBackoffAfterFailure = defaultStreamBackoffFunc
	}
	if opts.Contents != nil {
		config, err := bootstrap.NewConfigForTesting(opts.Contents)
		if err != nil {
			return nil, nil, err
		}
		return p.newRefCounted(opts.Name, config, opts.WatchExpiryTimeout, opts.StreamBackoffAfterFailure)
	}
	return p.newRefCounted(opts.Name, p.config, opts.WatchExpiryTimeout, opts.StreamBackoffAfterFailure)
}

// GetClientForTesting returns an xDS client created earlier using the given
// name from the pool.
//
// The second return value represents a close function which the caller is
// expected to invoke once they are done using the client.  It is safe for the
// caller to invoke this close function multiple times.
//
// # Testing Only
//
// This function should ONLY be used for testing purposes.
func (p *Pool) GetClientForTesting(name string) (XDSClient, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	c, ok := p.clients[name]
	if !ok {
		return nil, nil, fmt.Errorf("xDS client with name %q not found", name)
	}
	c.incrRef()
	return c, grpcsync.OnceFunc(func() { p.clientRefCountedClose(name) }), nil
}

// SetFallbackBootstrapConfig to specify a bootstrap configuration to use a
// fallback when the bootstrap env vars are not specified.
func (p *Pool) SetFallbackBootstrapConfig(config *bootstrap.Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.config = config
	return nil
}
