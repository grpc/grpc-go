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
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/grpclog"
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

// xdsclientWpapper is responsible for getting the xds client from attributes or
// creating a new xds client, and start watching EDS. The given callbacks will
// be called with EDS updates or errors.
type xdsclientWrapper struct {
	ctx              context.Context
	cancel           context.CancelFunc
	cancelWatch      func()
	cancelLoadReport func()
	cleanup          func()

	xdsclient xdsClientInterface
}

func (c *xdsclientWrapper) close() {
	if c.cancelLoadReport != nil {
		c.cancelLoadReport()
	}
	if c.cancelWatch != nil {
		c.cancelWatch()
	}
	if c.xdsclient != nil {
		// TODO: this shouldn't close xdsclient if it's from attributes.
		c.xdsclient.Close()
	}
	c.cleanup()
}

func newXDSClientWrapper(balancerName string, edsServiceName string, bbo balancer.BuildOptions, loadReportServer string, loadStore lrs.Store, newADS func(context.Context, *xdsclient.EDSUpdate) error, loseContact func(ctx context.Context), exitCleanup func()) *xdsclientWrapper {
	ctx, cancel := context.WithCancel(context.Background())
	ret := &xdsclientWrapper{
		ctx:     ctx,
		cancel:  cancel,
		cleanup: exitCleanup,
	}

	// TODO: get xdsclient from Attributes instead of creating a new one.

	config := bootstrapConfigNew()
	if config.BalancerName == "" {
		config.BalancerName = balancerName
	}
	if config.Creds == nil {
		// TODO: Once we start supporting a mechanism to register credential
		// types, a failure to find the credential type mentioned in the
		// bootstrap file should result in a failure, and not in using
		// credentials from the parent channel (passed through the
		// resolver.BuildOptions).
		config.Creds = defaultDialCreds(config.BalancerName, bbo)
	}

	var dopts []grpc.DialOption
	if bbo.Dialer != nil {
		dopts = []grpc.DialOption{grpc.WithContextDialer(bbo.Dialer)}
	}

	c, err := xdsclientNew(xdsclient.Options{Config: *config, DialOpts: dopts})
	if err != nil {
		grpclog.Warningf("failed to create xdsclient, error: %v", err)
		return ret
	}
	ret.xdsclient = c

	// The clusterName to watch should come from CDS response, via service
	// config. If it's an empty string, fallback user's dial target.
	nameToWatch := edsServiceName
	if nameToWatch == "" {
		grpclog.Warningf("eds: cluster name to watch is an empty string. Fallback to user's dial target")
		nameToWatch = bbo.Target.Endpoint
	}
	ret.cancelWatch = ret.xdsclient.WatchEDS(nameToWatch, func(update *xdsclient.EDSUpdate, err error) {
		if err != nil {
			loseContact(ret.ctx)
			return
		}
		if err := newADS(ret.ctx, update); err != nil {
			grpclog.Warningf("xds: processing new EDS update failed due to %v.", err)
		}
	})
	if loadStore != nil {
		ret.cancelLoadReport = ret.xdsclient.ReportLoad(loadReportServer, nameToWatch, loadStore)
	}
	return ret
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
