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
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	"google.golang.org/grpc/xds/internal/client/load"
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
	closeCh      *testutils.Channel
	loadStore    *load.Store
	bootstrapCfg *bootstrap.Config

	ldsCb func(xdsclient.ListenerUpdate, error)
	rdsCb func(xdsclient.RouteConfigUpdate, error)
	cdsCb func(xdsclient.ClusterUpdate, error)
	edsCb func(xdsclient.EndpointsUpdate, error)
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
	xdsC.cdsCb = callback
	xdsC.cdsWatchCh.Send(clusterName)
	return func() {
		xdsC.cdsCancelCh.Send(nil)
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
	xdsC.cdsCb(update, err)
}

// WaitForCancelClusterWatch waits for a CDS watch to be cancelled  and returns
// context.DeadlineExceeded otherwise.
func (xdsC *Client) WaitForCancelClusterWatch(ctx context.Context) error {
	_, err := xdsC.cdsCancelCh.Receive(ctx)
	return err
}

// WatchEndpoints registers an EDS watch for provided clusterName.
func (xdsC *Client) WatchEndpoints(clusterName string, callback func(xdsclient.EndpointsUpdate, error)) (cancel func()) {
	xdsC.edsCb = callback
	xdsC.edsWatchCh.Send(clusterName)
	return func() {
		xdsC.edsCancelCh.Send(nil)
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
func (xdsC *Client) InvokeWatchEDSCallback(update xdsclient.EndpointsUpdate, err error) {
	xdsC.edsCb(update, err)
}

// WaitForCancelEDSWatch waits for a EDS watch to be cancelled and returns
// context.DeadlineExceeded otherwise.
func (xdsC *Client) WaitForCancelEDSWatch(ctx context.Context) error {
	_, err := xdsC.edsCancelCh.Receive(ctx)
	return err
}

// ReportLoadArgs wraps the arguments passed to ReportLoad.
type ReportLoadArgs struct {
	// Server is the name of the server to which the load is reported.
	Server string
}

// ReportLoad starts reporting load about clusterName to server.
func (xdsC *Client) ReportLoad(server string) (loadStore *load.Store, cancel func()) {
	xdsC.loadReportCh.Send(ReportLoadArgs{Server: server})
	return xdsC.loadStore, func() {}
}

// LoadStore returns the underlying load data store.
func (xdsC *Client) LoadStore() *load.Store {
	return xdsC.loadStore
}

// WaitForReportLoad waits for ReportLoad to be invoked on this client and
// returns the arguments passed to it.
func (xdsC *Client) WaitForReportLoad(ctx context.Context) (ReportLoadArgs, error) {
	val, err := xdsC.loadReportCh.Receive(ctx)
	return val.(ReportLoadArgs), err
}

// Close closes the xds client.
func (xdsC *Client) Close() {
	xdsC.closeCh.Send(nil)
}

// WaitForClose waits for Close to be invoked on this client and returns
// context.DeadlineExceeded otherwise.
func (xdsC *Client) WaitForClose(ctx context.Context) error {
	_, err := xdsC.closeCh.Receive(ctx)
	return err
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
		cdsWatchCh:   testutils.NewChannel(),
		edsWatchCh:   testutils.NewChannel(),
		ldsCancelCh:  testutils.NewChannel(),
		rdsCancelCh:  testutils.NewChannel(),
		cdsCancelCh:  testutils.NewChannel(),
		edsCancelCh:  testutils.NewChannel(),
		loadReportCh: testutils.NewChannel(),
		closeCh:      testutils.NewChannel(),
		loadStore:    load.NewStore(),
	}
}
