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

// Package client implementation a full fledged gRPC client for the xDS API
// used by the xds resolver and balancer implementations.
package client

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
)

// For overriding in unittests.
var dialFunc = grpc.DialContext

// Options provides all parameters required for the creation of an xDS client.
type Options struct {
	// Config contains a fully populated bootstrap config. It is the
	// responsibility of the caller to use some sane defaults here if the
	// bootstrap process returned with certain fields left unspecified.
	Config bootstrap.Config
	// Backoff is the backoff strategy to be used when reconnecting to the xDS
	// server. If left unspecified, a default exponential backoff strategy
	// would be used.
	// TODO: If we have no reason to use an exponential backoff with
	// non-default values, remove this field, and directly use the appropriate
	// one in v2Client.
	Backoff backoff.Strategy
	// DialOpts contains dial options to be used when dialing the xDS server.
	DialOpts []grpc.DialOption
}

// Client is a full fledged gRPC client which queries a set of discovery APIs
// (collectively termed as xDS) on a remote management server, to discover
// various dynamic resources. A single client object will be shared by the xds
// resolver and balancer implementations.
type Client struct {
	cc  *grpc.ClientConn // Connection to the xDS server
	v2c *v2Client        // Actual xDS client implementation using the v2 API

	mu              sync.Mutex
	serviceCallback func(ServiceUpdate, error)
	ldsCancel       func()
	rdsCancel       func()
}

// NewClient returns a new xdsClient configured with opts.
func NewClient(opts Options) (*Client, error) {
	switch {
	case opts.Config.BalancerName == "":
		return nil, errors.New("xds: no xds_server name provided in options")
	case opts.Config.Creds == nil:
		return nil, errors.New("xds: no credentials provided in options")
	case opts.Config.NodeProto == nil:
		return nil, errors.New("xds: no node_proto provided in options")
	}

	dopts := append([]grpc.DialOption{opts.Config.Creds}, opts.DialOpts...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cc, err := dialFunc(ctx, opts.Config.BalancerName, dopts...)
	if err != nil {
		// An error from a non-blocking dial indicates something serious.
		return nil, fmt.Errorf("xds: failed to dial balancer {%s}: %v", opts.Config.BalancerName, err)
	}

	bs := backoff.DefaultExponential.Backoff
	if opts.Backoff != nil {
		bs = opts.Backoff.Backoff
	}
	c := &Client{
		cc:  cc,
		v2c: newV2Client(cc, opts.Config.NodeProto, bs),
	}
	return c, nil
}

// Close closes the gRPC connection to the xDS server.
func (c *Client) Close() {
	c.v2c.close()
	c.cc.Close()
}

// ServiceUpdate contains update about the service.
type ServiceUpdate struct {
	Cluster string
}

// handleLDSUpdate is the LDS watcher callback we registered with the v2Client.
func (c *Client) handleLDSUpdate(u ldsUpdate, err error) {
	if err != nil {
		c.mu.Lock()
		if c.serviceCallback != nil {
			c.serviceCallback(ServiceUpdate{}, err)
		}
		c.mu.Unlock()
		return
	}

	grpclog.Infof("xds: client received LDS update: %+v, err: %v", u, err)
	c.mu.Lock()
	c.rdsCancel = c.v2c.watchRDS(u.routeName, c.handleRDSUpdate)
	c.mu.Unlock()
}

// handleRDSUpdate is the RDS watcher callback we registered with the v2Client.
func (c *Client) handleRDSUpdate(u rdsUpdate, err error) {
	if err != nil {
		c.mu.Lock()
		if c.serviceCallback != nil {
			c.serviceCallback(ServiceUpdate{}, err)
		}
		c.mu.Unlock()
		return
	}

	grpclog.Infof("xds: client received RDS update: %+v, err: %v", u, err)
	c.mu.Lock()
	if c.serviceCallback != nil {
		c.serviceCallback(ServiceUpdate{Cluster: u.clusterName}, nil)
	}
	c.mu.Unlock()
}

// WatchForServiceUpdate uses LDS and RDS protocols to discover resource
// information for the provided target.
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
