/*
 *
 * Copyright 2019 gRPC authors.
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

// Package client contains the implementation of the xds client used by
// grpc-lb-v2.
package client

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
)

// Client is a full fledged gRPC client which queries a set of discovery APIs
// (collectively termed as xDS) on a remote management server, to discover
// various dynamic resources. A single client object will be shared by the xds
// resolver and balancer implementations.
type Client struct {
	config     *Config          // Data from xds bootstrap file
	cc         *grpc.ClientConn // Connection to the xDS server
	v2c        *v2Client        // Actual xDS client implementation using the v2 API
	registryID string           // ID of this client in the global registry

	mu              sync.Mutex
	serviceCallback func(ServiceUpdate, error)
	ldsCancel       func()
	rdsCancel       func()
}

// NewClient returns a new xDS client object configured using the provided
// options and by performing the xds bootstrap process.
func NewClient(opts ...Option) (*Client, error) {
	options := defaultOptions()
	for _, opt := range opts {
		opt.apply(&options)
	}

	// TODO: Once we fix the balancer to use the xdsClient provided by the
	// resolver, we should unexport this method, and probably fold the
	// implementation into this file.
	config := NewConfig()
	if config.BalancerName == "" {
		return nil, errors.New("xds: balancerName not found in bootstrap file")
	}
	if config.Creds == nil {
		// TODO: Once we start supporting a mechanism to register credential
		// types, a failure to find the credential type mentioned in the
		// bootstrap file should result in a failure, and not in using
		// credentials from the parent channel (passed through the
		// resolver.BuildOption).
		config.Creds = defaultDialCreds(config.BalancerName, options.rbo)
	}
	// TODO: What do we do if the NodeProto is empty from the bootstrap file?

	dopts := []grpc.DialOption{
		config.Creds,
		grpc.WithBalancerName(grpc.PickFirstBalancerName),
	}
	if options.rbo.Dialer != nil {
		dopts = append(dopts, grpc.WithContextDialer(options.rbo.Dialer))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cc, err := grpc.DialContext(ctx, config.BalancerName, dopts...)
	if err != nil {
		// An error from a non-blocking dial indicates something serious.
		return nil, fmt.Errorf("xds: failed to dial balancer {%s}: %v", config.BalancerName, err)
	}

	c := &Client{
		config: config,
		cc:     cc,
		v2c: &v2Client{
			backoff:   options.backoff.Backoff,
			cc:        cc,
			nodeProto: config.NodeProto,
		},
	}
	c.registryID = registry.add(c)
	c.v2c.start()
	return c, nil
}

// defaultDialCreds builds a DialOption containing the credentials to be used
// while talking to the xDS server. If the parent channel contains DialCreds,
// we use it as is. If it contains a CredsBundle, we use just the transport
// credentials from the bundle. If we don't find any credentials on the parent
// channel, we resort to using an insecure channel.
func defaultDialCreds(balancerName string, o resolver.BuildOption) grpc.DialOption {
	if creds := o.DialCreds; creds != nil {
		if err := creds.OverrideServerName(balancerName); err == nil {
			return grpc.WithTransportCredentials(creds)
		} else {
			grpclog.Warningf("xds: failed to override server name in credentials: %v, using Insecure", err)
			return grpc.WithInsecure()
		}
	} else if bundle := o.CredsBundle; bundle != nil {
		return grpc.WithTransportCredentials(bundle.TransportCredentials())
	}

	grpclog.Warning("xds: no credentials available, using Insecure")
	return grpc.WithInsecure()
}

// Close closes the gRPC connection to the xDS server and all goroutines
// spawned by this client object. It also removes the client from the global
// registry, which is also consulted by the xds balancer to determine the
// client object to use.
func (c *Client) Close() {
	c.v2c.close()
	registry.remove(c.registryID)
}

func (c *Client) RegistryID() string {
	return c.registryID
}

// ServiceUpdate contains update about the service.
type ServiceUpdate struct {
	Cluster string
}

func (c *Client) handleLDSUpdate(u ldsUpdate, err error) {
	if err != nil {
		c.mu.Lock()
		if c.serviceCallback != nil {
			c.serviceCallback(ServiceUpdate{}, err)
		}
		c.mu.Unlock()
		return
	}

	c.mu.Lock()
	c.rdsCancel = c.v2c.watchRDS(u.routeName, c.handleRDSUpdate)
	c.mu.Unlock()
}

func (c *Client) handleRDSUpdate(u rdsUpdate, err error) {
	if err != nil {
		c.mu.Lock()
		if c.serviceCallback != nil {
			c.serviceCallback(ServiceUpdate{}, err)
		}
		c.mu.Unlock()
		return
	}

	c.mu.Lock()
	if c.serviceCallback != nil {
		c.serviceCallback(ServiceUpdate{Cluster: u.cluster}, nil)
	}
	c.mu.Unlock()
}

// WatchForServiceUpdate watches LRS/RDS/VHDS.
func (c *Client) WatchForServiceUpdate(target string, callback func(ServiceUpdate, error)) (cancel func()) {
	c.mu.Lock()
	c.serviceCallback = callback
	c.ldsCancel = c.v2c.watchLDS(target, c.handleLDSUpdate)
	c.mu.Unlock()

	return func() {
		c.mu.Lock()
		c.serviceCallback = nil
		if c.ldsCancel != nil {
			c.ldsCancel()
		}
		if c.rdsCancel != nil {
			c.rdsCancel()
		}
		c.mu.Unlock()
	}
}
