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
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
)

// Options provides all parameters required for the creation of an xDS client.
type Options struct {
	// Config contains a fully populated bootstrap config. It is the
	// responsibility of the caller to use some sane defaults here if the
	// bootstrap process returned with certain fields left unspecified.
	Config bootstrap.Config
	// DialOpts contains dial options to be used when dialing the xDS server.
	DialOpts []grpc.DialOption
	// TargetName is the target of the parent ClientConn.
	TargetName string
}

// Client is a full fledged gRPC client which queries a set of discovery APIs
// (collectively termed as xDS) on a remote management server, to discover
// various dynamic resources.
//
// A single client object will be shared by the xds resolver and balancer
// implementations. But the same client can only be shared by the same parent
// ClientConn.
type Client struct {
	opts Options
	cc   *grpc.ClientConn // Connection to the xDS server
	v2c  *v2Client        // Actual xDS client implementation using the v2 API

	logger *grpclog.PrefixLogger

	mu              sync.Mutex
	serviceCallback func(ServiceUpdate, error)
	ldsCancel       func()
	rdsCancel       func()
}

// New returns a new xdsClient configured with opts.
func New(opts Options) (*Client, error) {
	switch {
	case opts.Config.BalancerName == "":
		return nil, errors.New("xds: no xds_server name provided in options")
	case opts.Config.Creds == nil:
		return nil, errors.New("xds: no credentials provided in options")
	case opts.Config.NodeProto == nil:
		return nil, errors.New("xds: no node_proto provided in options")
	}

	dopts := []grpc.DialOption{
		opts.Config.Creds,
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    5 * time.Minute,
			Timeout: 20 * time.Second,
		}),
	}
	dopts = append(dopts, opts.DialOpts...)

	c := &Client{opts: opts}

	cc, err := grpc.Dial(opts.Config.BalancerName, dopts...)
	if err != nil {
		// An error from a non-blocking dial indicates something serious.
		return nil, fmt.Errorf("xds: failed to dial balancer {%s}: %v", opts.Config.BalancerName, err)
	}
	c.cc = cc
	c.logger = grpclog.NewPrefixLogger(loggingPrefix(c))
	c.logger.Infof("Created ClientConn to xDS server: %s", opts.Config.BalancerName)

	c.v2c = newV2Client(cc, opts.Config.NodeProto, backoff.DefaultExponential.Backoff, c.logger)
	c.logger.Infof("Created")
	return c, nil
}

// Close closes the gRPC connection to the xDS server.
func (c *Client) Close() {
	// TODO: Should we invoke the registered callbacks here with an error that
	// the client is closed?
	c.v2c.close()
	c.cc.Close()
	c.logger.Infof("Shutdown")
}

// ServiceUpdate contains update about the service.
type ServiceUpdate struct {
	Cluster string
}

// handleLDSUpdate is the LDS watcher callback we registered with the v2Client.
func (c *Client) handleLDSUpdate(u ldsUpdate, err error) {
	c.logger.Infof("xds: client received LDS update: %+v, err: %v", u, err)
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

// handleRDSUpdate is the RDS watcher callback we registered with the v2Client.
func (c *Client) handleRDSUpdate(u rdsUpdate, err error) {
	c.logger.Infof("xds: client received RDS update: %+v, err: %v", u, err)
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
		c.serviceCallback(ServiceUpdate{Cluster: u.clusterName}, nil)
	}
	c.mu.Unlock()
}

// WatchService uses LDS and RDS protocols to discover information about the
// provided serviceName.
func (c *Client) WatchService(serviceName string, callback func(ServiceUpdate, error)) (cancel func()) {
	// TODO: Error out early if the client is closed. Ideally, this should
	// never be called after the client is closed though.
	c.mu.Lock()
	c.serviceCallback = callback
	c.ldsCancel = c.v2c.watchLDS(serviceName, c.handleLDSUpdate)
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

// WatchCluster uses CDS to discover information about the provided
// clusterName.
func (c *Client) WatchCluster(clusterName string, cdsCb func(CDSUpdate, error)) (cancel func()) {
	return c.v2c.watchCDS(clusterName, cdsCb)
}

// WatchEndpoints uses EDS to discover information about the endpoints in the
// provided clusterName.
func (c *Client) WatchEndpoints(clusterName string, edsCb func(*EDSUpdate, error)) (cancel func()) {
	return c.v2c.watchEDS(clusterName, edsCb)
}
