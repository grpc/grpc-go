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

// Package fakeclient provides a fake implementation of an xDS client.
package fakeclient

import (
	"context"

	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/load"
)

// Client is a fake implementation of an xds client. It exposes a bunch of
// channels to signal the occurrence of various events.
type Client struct {
	name         string
	ldsWatchCh   *testutils.Channel
	rdsWatchCh   *testutils.Channel
	cdsWatchCh   *testutils.Channel
	edsWatchCh   *testutils.Channel
	ldsCancelCh  *testutils.Channel
	rdsCancelCh  *testutils.Channel
	cdsCancelCh  *testutils.Channel
	edsCancelCh  *testutils.Channel
	loadReportCh *testutils.Channel
	loadStore    *load.Store
	bootstrapCfg *bootstrap.Config

	ldsCb  func(xdsclient.ListenerUpdate, error)
	rdsCb  func(xdsclient.RouteConfigUpdate, error)
	cdsCbs map[string]func(xdsclient.ClusterUpdate, error)
	edsCb  func(xdsclient.EndpointsUpdate, error)
}

// WatchListener registers a LDS watch.
func (c *Client) WatchListener(serviceName string, callback func(xdsclient.ListenerUpdate, error)) func() {
	c.ldsCb = callback
	c.ldsWatchCh.Send(serviceName)
	return func() {
		c.ldsCancelCh.Send(nil)
	}
}

// WaitForWatchListener waits for WatchCluster to be invoked on this client and
// returns the serviceName being watched.
func (c *Client) WaitForWatchListener(ctx context.Context) (string, error) {
	val, err := c.ldsWatchCh.Receive(ctx)
	if err != nil {
		return "", err
	}
	return val.(string), err
}

// InvokeWatchListenerCallback invokes the registered ldsWatch callback.
//
// Not thread safe with WatchListener. Only call this after
// WaitForWatchListener.
func (c *Client) InvokeWatchListenerCallback(update xdsclient.ListenerUpdate, err error) {
	c.ldsCb(update, err)
}

// WaitForCancelListenerWatch waits for a LDS watch to be cancelled  and returns
// context.DeadlineExceeded otherwise.
func (c *Client) WaitForCancelListenerWatch(ctx context.Context) error {
	_, err := c.ldsCancelCh.Receive(ctx)
	return err
}

// WatchRouteConfig registers a RDS watch.
func (c *Client) WatchRouteConfig(routeName string, callback func(xdsclient.RouteConfigUpdate, error)) func() {
	c.rdsCb = callback
	c.rdsWatchCh.Send(routeName)
	return func() {
		c.rdsCancelCh.Send(nil)
	}
}

// WaitForWatchRouteConfig waits for WatchCluster to be invoked on this client and
// returns the routeName being watched.
func (c *Client) WaitForWatchRouteConfig(ctx context.Context) (string, error) {
	val, err := c.rdsWatchCh.Receive(ctx)
	if err != nil {
		return "", err
	}
	return val.(string), err
}

// InvokeWatchRouteConfigCallback invokes the registered rdsWatch callback.
//
// Not thread safe with WatchRouteConfig. Only call this after
// WaitForWatchRouteConfig.
func (c *Client) InvokeWatchRouteConfigCallback(update xdsclient.RouteConfigUpdate, err error) {
	c.rdsCb(update, err)
}

// WaitForCancelRouteConfigWatch waits for a RDS watch to be cancelled  and returns
// context.DeadlineExceeded otherwise.
func (c *Client) WaitForCancelRouteConfigWatch(ctx context.Context) error {
	_, err := c.rdsCancelCh.Receive(ctx)
	return err
}

// WatchCluster registers a CDS watch.
func (c *Client) WatchCluster(clusterName string, callback func(xdsclient.ClusterUpdate, error)) func() {
	// Due to the tree like structure of aggregate clusters, there can be multiple callbacks persisted for each cluster
	// node. However, the client doesn't care about the parent child relationship between the nodes, only that it invokes
	// the right callback for a particular cluster.
	c.cdsCbs[clusterName] = callback
	c.cdsWatchCh.Send(clusterName)
	return func() {
		c.cdsCancelCh.Send(clusterName)
	}
}

// WaitForWatchCluster waits for WatchCluster to be invoked on this client and
// returns the clusterName being watched.
func (c *Client) WaitForWatchCluster(ctx context.Context) (string, error) {
	val, err := c.cdsWatchCh.Receive(ctx)
	if err != nil {
		return "", err
	}
	return val.(string), err
}

// InvokeWatchClusterCallback invokes the registered cdsWatch callback.
//
// Not thread safe with WatchCluster. Only call this after
// WaitForWatchCluster.
func (c *Client) InvokeWatchClusterCallback(update xdsclient.ClusterUpdate, err error) {
	// Keeps functionality with previous usage of this, if single callback call that callback.
	if len(c.cdsCbs) == 1 {
		var clusterName string
		for cluster := range c.cdsCbs {
			clusterName = cluster
		}
		c.cdsCbs[clusterName](update, err)
	} else {
		// Have what callback you call with the update determined by the service name in the ClusterUpdate. Left up to the
		// caller to make sure the cluster update matches with a persisted callback.
		c.cdsCbs[update.ClusterName](update, err)
	}
}

// WaitForCancelClusterWatch waits for a CDS watch to be cancelled  and returns
// context.DeadlineExceeded otherwise.
func (c *Client) WaitForCancelClusterWatch(ctx context.Context) (string, error) {
	clusterNameReceived, err := c.cdsCancelCh.Receive(ctx)
	if err != nil {
		return "", err
	}
	return clusterNameReceived.(string), err
}

// WatchEndpoints registers an EDS watch for provided clusterName.
func (c *Client) WatchEndpoints(clusterName string, callback func(xdsclient.EndpointsUpdate, error)) (cancel func()) {
	c.edsCb = callback
	c.edsWatchCh.Send(clusterName)
	return func() {
		c.edsCancelCh.Send(nil)
	}
}

// WaitForWatchEDS waits for WatchEndpoints to be invoked on this client and
// returns the clusterName being watched.
func (c *Client) WaitForWatchEDS(ctx context.Context) (string, error) {
	val, err := c.edsWatchCh.Receive(ctx)
	if err != nil {
		return "", err
	}
	return val.(string), err
}

// InvokeWatchEDSCallback invokes the registered edsWatch callback.
//
// Not thread safe with WatchEndpoints. Only call this after
// WaitForWatchEDS.
func (c *Client) InvokeWatchEDSCallback(update xdsclient.EndpointsUpdate, err error) {
	c.edsCb(update, err)
}

// WaitForCancelEDSWatch waits for a EDS watch to be cancelled and returns
// context.DeadlineExceeded otherwise.
func (c *Client) WaitForCancelEDSWatch(ctx context.Context) error {
	_, err := c.edsCancelCh.Receive(ctx)
	return err
}

// ReportLoadArgs wraps the arguments passed to ReportLoad.
type ReportLoadArgs struct {
	// Server is the name of the server to which the load is reported.
	Server string
}

// ReportLoad starts reporting load about clusterName to server.
func (c *Client) ReportLoad(server string) (loadStore *load.Store, cancel func()) {
	c.loadReportCh.Send(ReportLoadArgs{Server: server})
	return c.loadStore, func() {}
}

// LoadStore returns the underlying load data store.
func (c *Client) LoadStore() *load.Store {
	return c.loadStore
}

// WaitForReportLoad waits for ReportLoad to be invoked on this client and
// returns the arguments passed to it.
func (c *Client) WaitForReportLoad(ctx context.Context) (ReportLoadArgs, error) {
	val, err := c.loadReportCh.Receive(ctx)
	return val.(ReportLoadArgs), err
}

// Close is a nop.
func (c *Client) Close() {}

// BootstrapConfig returns the bootstrap config.
func (c *Client) BootstrapConfig() *bootstrap.Config {
	return c.bootstrapCfg
}

// SetBootstrapConfig updates the bootstrap config.
func (c *Client) SetBootstrapConfig(cfg *bootstrap.Config) {
	c.bootstrapCfg = cfg
}

// Name returns the name of the xds client.
func (c *Client) Name() string {
	return c.name
}

// NewClient returns a new fake xds client.
func NewClient() *Client {
	return NewClientWithName("")
}

// NewClientWithName returns a new fake xds client with the provided name. This
// is used in cases where multiple clients are created in the tests and we need
// to make sure the client is created for the expected balancer name.
func NewClientWithName(name string) *Client {
	return &Client{
		name:         name,
		ldsWatchCh:   testutils.NewChannel(),
		rdsWatchCh:   testutils.NewChannel(),
		cdsWatchCh:   testutils.NewChannelWithSize(10),
		edsWatchCh:   testutils.NewChannel(),
		ldsCancelCh:  testutils.NewChannel(),
		rdsCancelCh:  testutils.NewChannel(),
		cdsCancelCh:  testutils.NewChannelWithSize(10),
		edsCancelCh:  testutils.NewChannel(),
		loadReportCh: testutils.NewChannel(),
		loadStore:    load.NewStore(),
		cdsCbs:       make(map[string]func(xdsclient.ClusterUpdate, error)),
	}
}
