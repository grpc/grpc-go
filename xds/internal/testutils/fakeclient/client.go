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

	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/load"
)

// Client is a fake implementation of an xds client. It exposes a bunch of
// channels to signal the occurrence of various events.
type Client struct {
	// Embed XDSClient so this fake client implements the interface, but it's
	// never set (it's always nil). This may cause nil panic since not all the
	// methods are implemented.
	xdsclient.XDSClient

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
	lrsCancelCh  *testutils.Channel
	loadStore    *load.Store
	bootstrapCfg *bootstrap.Config

	ldsCb  func(xdsclient.ListenerUpdate, error)
	rdsCb  func(xdsclient.RouteConfigUpdate, error)
	cdsCbs map[string]func(xdsclient.ClusterUpdate, error)
	edsCbs map[string]func(xdsclient.EndpointsUpdate, error)

	Closed *grpcsync.Event // fired when Close is called.
}

// WatchListener registers a LDS watch.
func (xdsC *Client) WatchListener(serviceName string, callback func(xdsclient.ListenerUpdate, error)) func() {
	xdsC.ldsCb = callback
	xdsC.ldsWatchCh.Send(serviceName)
	return func() {
		xdsC.ldsCancelCh.Send(nil)
	}
}

// WaitForWatchListener waits for WatchCluster to be invoked on this client and
// returns the serviceName being watched.
func (xdsC *Client) WaitForWatchListener(ctx context.Context) (string, error) {
	val, err := xdsC.ldsWatchCh.Receive(ctx)
	if err != nil {
		return "", err
	}
	return val.(string), err
}

// InvokeWatchListenerCallback invokes the registered ldsWatch callback.
//
// Not thread safe with WatchListener. Only call this after
// WaitForWatchListener.
func (xdsC *Client) InvokeWatchListenerCallback(update xdsclient.ListenerUpdate, err error) {
	xdsC.ldsCb(update, err)
}

// WaitForCancelListenerWatch waits for a LDS watch to be cancelled  and returns
// context.DeadlineExceeded otherwise.
func (xdsC *Client) WaitForCancelListenerWatch(ctx context.Context) error {
	_, err := xdsC.ldsCancelCh.Receive(ctx)
	return err
}

// WatchRouteConfig registers a RDS watch.
func (xdsC *Client) WatchRouteConfig(routeName string, callback func(xdsclient.RouteConfigUpdate, error)) func() {
	xdsC.rdsCb = callback
	xdsC.rdsWatchCh.Send(routeName)
	return func() {
		xdsC.rdsCancelCh.Send(nil)
	}
}

// WaitForWatchRouteConfig waits for WatchCluster to be invoked on this client and
// returns the routeName being watched.
func (xdsC *Client) WaitForWatchRouteConfig(ctx context.Context) (string, error) {
	val, err := xdsC.rdsWatchCh.Receive(ctx)
	if err != nil {
		return "", err
	}
	return val.(string), err
}

// InvokeWatchRouteConfigCallback invokes the registered rdsWatch callback.
//
// Not thread safe with WatchRouteConfig. Only call this after
// WaitForWatchRouteConfig.
func (xdsC *Client) InvokeWatchRouteConfigCallback(update xdsclient.RouteConfigUpdate, err error) {
	xdsC.rdsCb(update, err)
}

// WaitForCancelRouteConfigWatch waits for a RDS watch to be cancelled  and returns
// context.DeadlineExceeded otherwise.
func (xdsC *Client) WaitForCancelRouteConfigWatch(ctx context.Context) error {
	_, err := xdsC.rdsCancelCh.Receive(ctx)
	return err
}

// WatchCluster registers a CDS watch.
func (xdsC *Client) WatchCluster(clusterName string, callback func(xdsclient.ClusterUpdate, error)) func() {
	// Due to the tree like structure of aggregate clusters, there can be multiple callbacks persisted for each cluster
	// node. However, the client doesn't care about the parent child relationship between the nodes, only that it invokes
	// the right callback for a particular cluster.
	xdsC.cdsCbs[clusterName] = callback
	xdsC.cdsWatchCh.Send(clusterName)
	return func() {
		xdsC.cdsCancelCh.Send(clusterName)
	}
}

// WaitForWatchCluster waits for WatchCluster to be invoked on this client and
// returns the clusterName being watched.
func (xdsC *Client) WaitForWatchCluster(ctx context.Context) (string, error) {
	val, err := xdsC.cdsWatchCh.Receive(ctx)
	if err != nil {
		return "", err
	}
	return val.(string), err
}

// InvokeWatchClusterCallback invokes the registered cdsWatch callback.
//
// Not thread safe with WatchCluster. Only call this after
// WaitForWatchCluster.
func (xdsC *Client) InvokeWatchClusterCallback(update xdsclient.ClusterUpdate, err error) {
	// Keeps functionality with previous usage of this, if single callback call that callback.
	if len(xdsC.cdsCbs) == 1 {
		var clusterName string
		for cluster := range xdsC.cdsCbs {
			clusterName = cluster
		}
		xdsC.cdsCbs[clusterName](update, err)
	} else {
		// Have what callback you call with the update determined by the service name in the ClusterUpdate. Left up to the
		// caller to make sure the cluster update matches with a persisted callback.
		xdsC.cdsCbs[update.ClusterName](update, err)
	}
}

// WaitForCancelClusterWatch waits for a CDS watch to be cancelled  and returns
// context.DeadlineExceeded otherwise.
func (xdsC *Client) WaitForCancelClusterWatch(ctx context.Context) (string, error) {
	clusterNameReceived, err := xdsC.cdsCancelCh.Receive(ctx)
	if err != nil {
		return "", err
	}
	return clusterNameReceived.(string), err
}

// WatchEndpoints registers an EDS watch for provided clusterName.
func (xdsC *Client) WatchEndpoints(clusterName string, callback func(xdsclient.EndpointsUpdate, error)) (cancel func()) {
	xdsC.edsCbs[clusterName] = callback
	xdsC.edsWatchCh.Send(clusterName)
	return func() {
		xdsC.edsCancelCh.Send(clusterName)
	}
}

// WaitForWatchEDS waits for WatchEndpoints to be invoked on this client and
// returns the clusterName being watched.
func (xdsC *Client) WaitForWatchEDS(ctx context.Context) (string, error) {
	val, err := xdsC.edsWatchCh.Receive(ctx)
	if err != nil {
		return "", err
	}
	return val.(string), err
}

// InvokeWatchEDSCallback invokes the registered edsWatch callback.
//
// Not thread safe with WatchEndpoints. Only call this after
// WaitForWatchEDS.
func (xdsC *Client) InvokeWatchEDSCallback(name string, update xdsclient.EndpointsUpdate, err error) {
	if len(xdsC.edsCbs) != 1 {
		// This may panic if name isn't found. But it's fine for tests.
		xdsC.edsCbs[name](update, err)
		return
	}
	// Keeps functionality with previous usage of this, if single callback call
	// that callback.
	for n := range xdsC.edsCbs {
		name = n
	}
	xdsC.edsCbs[name](update, err)
}

// WaitForCancelEDSWatch waits for a EDS watch to be cancelled and returns
// context.DeadlineExceeded otherwise.
func (xdsC *Client) WaitForCancelEDSWatch(ctx context.Context) (string, error) {
	edsNameReceived, err := xdsC.edsCancelCh.Receive(ctx)
	if err != nil {
		return "", err
	}
	return edsNameReceived.(string), err
}

// ReportLoadArgs wraps the arguments passed to ReportLoad.
type ReportLoadArgs struct {
	// Server is the name of the server to which the load is reported.
	Server string
}

// ReportLoad starts reporting load about clusterName to server.
func (xdsC *Client) ReportLoad(server string) (loadStore *load.Store, cancel func()) {
	xdsC.loadReportCh.Send(ReportLoadArgs{Server: server})
	return xdsC.loadStore, func() {
		xdsC.lrsCancelCh.Send(nil)
	}
}

// WaitForCancelReportLoad waits for a load report to be cancelled and returns
// context.DeadlineExceeded otherwise.
func (xdsC *Client) WaitForCancelReportLoad(ctx context.Context) error {
	_, err := xdsC.lrsCancelCh.Receive(ctx)
	return err
}

// LoadStore returns the underlying load data store.
func (xdsC *Client) LoadStore() *load.Store {
	return xdsC.loadStore
}

// WaitForReportLoad waits for ReportLoad to be invoked on this client and
// returns the arguments passed to it.
func (xdsC *Client) WaitForReportLoad(ctx context.Context) (ReportLoadArgs, error) {
	val, err := xdsC.loadReportCh.Receive(ctx)
	if err != nil {
		return ReportLoadArgs{}, err
	}
	return val.(ReportLoadArgs), nil
}

// Close fires xdsC.Closed, indicating it was called.
func (xdsC *Client) Close() {
	xdsC.Closed.Fire()
}

// BootstrapConfig returns the bootstrap config.
func (xdsC *Client) BootstrapConfig() *bootstrap.Config {
	return xdsC.bootstrapCfg
}

// SetBootstrapConfig updates the bootstrap config.
func (xdsC *Client) SetBootstrapConfig(cfg *bootstrap.Config) {
	xdsC.bootstrapCfg = cfg
}

// Name returns the name of the xds client.
func (xdsC *Client) Name() string {
	return xdsC.name
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
		edsWatchCh:   testutils.NewChannelWithSize(10),
		ldsCancelCh:  testutils.NewChannel(),
		rdsCancelCh:  testutils.NewChannel(),
		cdsCancelCh:  testutils.NewChannelWithSize(10),
		edsCancelCh:  testutils.NewChannelWithSize(10),
		loadReportCh: testutils.NewChannel(),
		lrsCancelCh:  testutils.NewChannel(),
		loadStore:    load.NewStore(),
		cdsCbs:       make(map[string]func(xdsclient.ClusterUpdate, error)),
		edsCbs:       make(map[string]func(xdsclient.EndpointsUpdate, error)),
		Closed:       grpcsync.NewEvent(),
	}
}
