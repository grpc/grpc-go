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

	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
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

// Interface to be overridden in tests.
type xdsv2Client interface {
	addWatch(resourceType, resourceName string)
	removeWatch(resourceType, resourceName string)
	close()
}

// Function to be overridden in tests.
var newXDSV2Client = func(parent *Client, cc *grpc.ClientConn, nodeProto *corepb.Node, backoff func(int) time.Duration, logger *grpclog.PrefixLogger) xdsv2Client {
	return newV2Client(parent, cc, nodeProto, backoff, logger)
}

// Client is a full fledged gRPC client which queries a set of discovery APIs
// (collectively termed as xDS) on a remote management server, to discover
// various dynamic resources.
//
// A single client object will be shared by the xds resolver and balancer
// implementations. But the same client can only be shared by the same parent
// ClientConn.
type Client struct {
	done *grpcsync.Event
	opts Options
	cc   *grpc.ClientConn // Connection to the xDS server
	v2c  xdsv2Client      // Actual xDS client implementation using the v2 API

	logger *grpclog.PrefixLogger

	updateCh    *buffer.Unbounded // chan *watcherInfoWithUpdate
	mu          sync.Mutex
	ldsWatchers map[string]map[*watchInfo]bool
	ldsCache    map[string]ldsUpdate
	rdsWatchers map[string]map[*watchInfo]bool
	rdsCache    map[string]rdsUpdate
	cdsWatchers map[string]map[*watchInfo]bool
	cdsCache    map[string]ClusterUpdate
	edsWatchers map[string]map[*watchInfo]bool
	edsCache    map[string]EndpointsUpdate
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

	c := &Client{
		done: grpcsync.NewEvent(),
		opts: opts,

		updateCh:    buffer.NewUnbounded(),
		ldsWatchers: make(map[string]map[*watchInfo]bool),
		ldsCache:    make(map[string]ldsUpdate),
		rdsWatchers: make(map[string]map[*watchInfo]bool),
		rdsCache:    make(map[string]rdsUpdate),
		cdsWatchers: make(map[string]map[*watchInfo]bool),
		cdsCache:    make(map[string]ClusterUpdate),
		edsWatchers: make(map[string]map[*watchInfo]bool),
		edsCache:    make(map[string]EndpointsUpdate),
	}

	cc, err := grpc.Dial(opts.Config.BalancerName, dopts...)
	if err != nil {
		// An error from a non-blocking dial indicates something serious.
		return nil, fmt.Errorf("xds: failed to dial balancer {%s}: %v", opts.Config.BalancerName, err)
	}
	c.cc = cc
	c.logger = grpclog.NewPrefixLogger(loggingPrefix(c))
	c.logger.Infof("Created ClientConn to xDS server: %s", opts.Config.BalancerName)

	c.v2c = newXDSV2Client(c, cc, opts.Config.NodeProto, backoff.DefaultExponential.Backoff, c.logger)
	c.logger.Infof("Created")
	go c.run()
	return c, nil
}

// run is a goroutine for all the callbacks.
//
// Callback can be called in watch(), if an item is found in cache. Without this
// goroutine, the callback will be called inline, which might cause a deadlock
// in user's code. Callbacks also cannot be simple `go callback()` because the
// order matters.
func (c *Client) run() {
	for {
		select {
		case t := <-c.updateCh.Get():
			c.updateCh.Load()
			if c.done.HasFired() {
				return
			}
			c.callCallback(t.(*watcherInfoWithUpdate))
		case <-c.done.Done():
			return
		}
	}
}

// Close closes the gRPC connection to the xDS server.
func (c *Client) Close() {
	if c.done.HasFired() {
		return
	}
	c.done.Fire()
	// TODO: Should we invoke the registered callbacks here with an error that
	// the client is closed?
	c.v2c.close()
	c.cc.Close()
	c.logger.Infof("Shutdown")
}
