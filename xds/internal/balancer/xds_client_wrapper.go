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

package balancer

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/grpclog"
	xdsinternal "google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/balancer/lrs"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
)

// xdsClientInterface contains only the xds_client methods needed by EDS
// balancer. It's defined so we can override xdsclientNew function in tests.
type xdsClientInterface interface {
	WatchEDS(clusterName string, edsCb func(*xdsclient.EDSUpdate, error)) (cancel func())
	ReportLoad(server string, clusterName string, loadStore lrs.Store) (cancel func())
	Close()
}

var (
	xdsclientNew = func(opts xdsclient.Options) (xdsClientInterface, error) {
		return xdsclient.New(opts)
	}
	bootstrapConfigNew = bootstrap.NewConfig
)

// xdsclientWrapper is responsible for getting the xds client from attributes or
// creating a new xds client, and start watching EDS. The given callbacks will
// be called with EDS updates or errors.
type xdsclientWrapper struct {
	newEDSUpdate func(*xdsclient.EDSUpdate) error
	loseContact  func()
	bbo          balancer.BuildOptions
	loadStore    lrs.Store

	balancerName string
	// xdsclient could come from attributes, or created with balancerName.
	xdsclient xdsClientInterface

	// edsServiceName is the name from service config. It can be "". When it's
	// "", user's dial target is used to watch EDS.
	//
	// TODO: remove the empty string related behavior, when we switch to always
	// do CDS.
	edsServiceName   string
	cancelEDSWatch   func()
	loadReportServer string
	cancelLoadReport func()
}

// newXDSClientWrapper creates an empty xds_client wrapper that does nothing. It
// can accept xds_client configs, to new/switch xds_client to use.
//
// The given callbacks won't be called until the underlying xds_client is
// working and sends updates.
func newXDSClientWrapper(newEDSUpdate func(*xdsclient.EDSUpdate) error, loseContact func(), bbo balancer.BuildOptions, loadStore lrs.Store) *xdsclientWrapper {
	return &xdsclientWrapper{
		newEDSUpdate: newEDSUpdate,
		loseContact:  loseContact,
		bbo:          bbo,
		loadStore:    loadStore,
	}
}

// Either pick from attributes, or create a new one. It returns false if
// xdsclient is not changed.
//
// If a new client is created, the old one will be closed.
func (c *xdsclientWrapper) updateXDSClient(config *XDSConfig, attr *attributes.Attributes) bool {
	var clientFromAttr xdsClientInterface
	if attr != nil {
		clientFromAttr, _ = attr.Value(xdsinternal.XDSClientID).(xdsClientInterface)
	}

	// Client is found in attributes, it will be used, but we also need to
	// decide whether to close the old client.
	// - if old client was created locally (balancerName is not ""), close it
	// and replace it
	// - if old client was from previous attributes, only replace it, but don't
	// close it
	if clientFromAttr != nil {
		oldBalancerName := c.balancerName
		c.balancerName = "" // Clear balancerName, to indicate that client is from attributes.
		oldClient := c.xdsclient
		c.xdsclient = clientFromAttr
		if oldBalancerName != "" {
			// Only close the old client if it's not from attributes.
			oldClient.Close()
		}
		return c.xdsclient != oldClient
	}

	// If client is not found in attributes, will need to create a new one only
	// if the balancerName (from bootstrap file or from service config) changed.
	// - if balancer names are the same
	//   - if new balancerName is an empty string, and this is the first
	//   update, we still create a client for it, even though it's unlikely to
	//   work
	//   - else, do nothing, and return clientChanged = false
	// - if balancer names are different
	//     - create new one, and return clientChanged = true

	clientConfig := bootstrapConfigNew()
	if clientConfig.BalancerName == "" {
		clientConfig.BalancerName = config.BalancerName
	}

	if c.balancerName == clientConfig.BalancerName {
		if c.balancerName != "" || c.xdsclient != nil {
			return false
		}
		grpclog.Warningf("eds: creating an xds_client with empty string as server name, this is very unlikey to work")
		// If balancerName is empty string, and c.xdsclient is nil,
		// this is the first time. We try to create a new xdsclient
		// with empty string as server name, even though it's very
		// unlikely to work.
	}

	if clientConfig.Creds == nil {
		// TODO: Once we start supporting a mechanism to register credential
		// types, a failure to find the credential type mentioned in the
		// bootstrap file should result in a failure, and not in using
		// credentials from the parent channel (passed through the
		// resolver.BuildOptions).
		clientConfig.Creds = defaultDialCreds(clientConfig.BalancerName, c.bbo)
	}
	var dopts []grpc.DialOption
	if dialer := c.bbo.Dialer; dialer != nil {
		dopts = []grpc.DialOption{grpc.WithContextDialer(dialer)}
	}

	newClient, err := xdsclientNew(xdsclient.Options{Config: *clientConfig, DialOpts: dopts})
	if err != nil {
		// This should never fail. xdsclientnew does a non-blocking dial, and
		// all the config passed in should be validated.
		grpclog.Fatalf("failed to create xdsclient, error: %v", err)
	}
	oldBalancerName := c.balancerName
	c.balancerName = clientConfig.BalancerName
	oldClient := c.xdsclient
	c.xdsclient = newClient
	if oldBalancerName != "" {
		// Only close the old client if it's not from attributes.
		oldClient.Close()
	}
	return true
}

// startEDSWatch starts the EDS watch. If there's already a watch in progress, it
// cancels that.
//
// Caller can call this when the xds_client is updated, or the edsServiceName is updated.
//
// It also (re)starts the load report.
func (c *xdsclientWrapper) startEDSWatch(edsServiceName string, loadReportServer string) {
	// The clusterName to watch should come from CDS response, via service
	// config. If it's an empty string, fallback user's dial target.
	c.edsServiceName = edsServiceName
	nameToWatch := edsServiceName
	if nameToWatch == "" {
		grpclog.Warningf("eds: cluster name to watch is an empty string. Fallback to user's dial target")
		nameToWatch = c.bbo.Target.Endpoint
	}
	if c.cancelEDSWatch != nil {
		c.cancelEDSWatch()
	}

	c.cancelEDSWatch = c.xdsclient.WatchEDS(nameToWatch, func(update *xdsclient.EDSUpdate, err error) {
		if err != nil {
			c.loseContact()
			return
		}
		if err := c.newEDSUpdate(update); err != nil {
			grpclog.Warningf("xds: processing new EDS update failed due to %v.", err)
		}
	})

	if c.loadStore != nil {
		if c.cancelLoadReport != nil {
			c.cancelLoadReport()
		}
		c.loadReportServer = loadReportServer
		c.cancelLoadReport = c.xdsclient.ReportLoad(c.loadReportServer, nameToWatch, c.loadStore)
	}
	return
}

// startLoadReport starts load reporting. If there's already a load reporting in
// progress, it cancels that.
//
// Caller can cal this when the loadReportServer name changes, but
// edsServiceName doens't (so we only need to restart load reporting, not EDS
// watch).
func (c *xdsclientWrapper) startLoadReport(edsServiceName string, loadReportServer string) {
	if c.loadStore != nil {
		nameToWatch := edsServiceName
		if nameToWatch == "" {
			nameToWatch = c.bbo.Target.Endpoint
		}
		c.loadReportServer = loadReportServer
		if c.cancelLoadReport != nil {
			c.cancelLoadReport()
		}
		c.cancelLoadReport = c.xdsclient.ReportLoad(c.loadReportServer, nameToWatch, c.loadStore)
	}
}

func (c *xdsclientWrapper) handleUpdate(config *XDSConfig, attr *attributes.Attributes) {
	clientChanged := c.updateXDSClient(config, attr)

	var (
		restartWatchEDS   bool
		restartLoadReport bool
	)
	// Need to restart EDS watch when one of the following happens:
	// - the xds_client is updated
	// - the xds_client didn't change, but the edsServiceName changed
	//
	// Only need to restart load reporting when:
	// - no need to restart EDS, but loadReportServer name changed
	if clientChanged {
		restartWatchEDS = true
	} else if c.edsServiceName != config.EDSServiceName {
		restartWatchEDS = true
	} else if c.loadReportServer != config.LrsLoadReportingServerName {
		restartLoadReport = true
	}

	if restartWatchEDS {
		c.startEDSWatch(config.EDSServiceName, config.LrsLoadReportingServerName)
		return
	}

	if restartLoadReport {
		c.startLoadReport(config.EDSServiceName, config.LrsLoadReportingServerName)
	}
}

func (c *xdsclientWrapper) close() {
	if c.xdsclient != nil && c.balancerName != "" {
		// Only close xdsclient if it's not from attributes.
		c.xdsclient.Close()
	}

	if c.cancelLoadReport != nil {
		c.cancelLoadReport()
	}
	if c.cancelEDSWatch != nil {
		c.cancelEDSWatch()
	}
}

// defaultDialCreds builds a DialOption containing the credentials to be used
// while talking to the xDS server (this is done only if the xds bootstrap
// process does not return any credentials to use). If the parent channel
// contains DialCreds, we use it as is. If it contains a CredsBundle, we use
// just the transport credentials from the bundle. If we don't find any
// credentials on the parent channel, we resort to using an insecure channel.
func defaultDialCreds(balancerName string, rbo balancer.BuildOptions) grpc.DialOption {
	switch {
	case rbo.DialCreds != nil:
		if err := rbo.DialCreds.OverrideServerName(balancerName); err != nil {
			grpclog.Warningf("xds: failed to override server name in credentials: %v, using Insecure", err)
			return grpc.WithInsecure()
		}
		return grpc.WithTransportCredentials(rbo.DialCreds)
	case rbo.CredsBundle != nil:
		return grpc.WithTransportCredentials(rbo.CredsBundle.TransportCredentials())
	default:
		grpclog.Warning("xds: no credentials available, using Insecure")
		return grpc.WithInsecure()
	}
}
