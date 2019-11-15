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

// Package resolver implements the xds resolver.
//
// At this point, the resolver is named xds-experimental, and doesn't do very
// much at all, except for returning a hard-coded service config which selects
// the xds_experimental balancer.
package resolver

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/resolver"

	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	"google.golang.org/grpc/xds/internal/registry"
)

// xDS balancer name is xds_experimental while resolver scheme is
// xds-experimental since "_" is not a valid character in the URL.
const xdsScheme = "xds-experimental"

// For overriding in unittests.
var (
	newXDSClient = func(opts xdsclient.Options) (xdsClientInterface, error) {
		return xdsclient.NewClient(opts)
	}
	newXDSConfig = bootstrap.NewConfig
)

func init() {
	resolver.Register(&xdsResolverBuilder{})
}

type xdsResolverBuilder struct{}

// Build helps implement the resolver.Builder interface.
//
// The xds bootstrap process is performed (and a new xds client is built) every
// time an xds resolver is built. The newly created xds client is also added
// into a global registry.
func (b *xdsResolverBuilder) Build(t resolver.Target, cc resolver.ClientConn, rbo resolver.BuildOptions) (resolver.Resolver, error) {
	config := newXDSConfig()
	switch {
	case config.BalancerName == "":
		return nil, errors.New("xds: balancerName not found in bootstrap file")
	case config.Creds == nil:
		// TODO: Once we start supporting a mechanism to register credential
		// types, a failure to find the credential type mentioned in the
		// bootstrap file should result in a failure, and not in using
		// credentials from the parent channel (passed through the
		// resolver.BuildOptions).
		config.Creds = defaultDialCreds(config.BalancerName, rbo)
	case config.NodeProto == nil:
		return nil, errors.New("xds: node proto not found in bootstrap file")
	}

	var dopts []grpc.DialOption
	if rbo.Dialer != nil {
		dopts = []grpc.DialOption{grpc.WithContextDialer(rbo.Dialer)}
	}

	client, err := newXDSClient(xdsclient.Options{Config: *config, DialOpts: dopts})
	if err != nil {
		return nil, err
	}
	r := &xdsResolver{
		target:   t,
		cc:       cc,
		client:   client,
		clientID: registry.AddTo(client),
		updateCh: buffer.NewUnbounded(),
	}
	r.ctx, r.cancelCtx = context.WithCancel(context.Background())
	go r.run()
	return r, nil
}

// defaultDialCreds builds a DialOption containing the credentials to be used
// while talking to the xDS server (this is done only if the xds bootstrap
// process does not return any credentials to use). If the parent channel
// contains DialCreds, we use it as is. If it contains a CredsBundle, we use
// just the transport credentials from the bundle. If we don't find any
// credentials on the parent channel, we resort to using an insecure channel.
func defaultDialCreds(balancerName string, rbo resolver.BuildOptions) grpc.DialOption {
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

// Name helps implement the resolver.Builder interface.
func (*xdsResolverBuilder) Scheme() string {
	return xdsScheme
}

// xdsClientInterface contains methods from xdsClient.Client which are used by
// the resolver. This will be faked out in unittests.
type xdsClientInterface interface {
	WatchForServiceUpdate(string, func(xdsclient.ServiceUpdate, error)) func()
	Close()
}

// xdsResolver implements the resolver.Resolver interface.
//
// It registers a watcher for ServiceConfig updates with the xdsClient object
// (which performs LDS/RDS queries for the same), and passes the received
// updates to the ClientConn.
type xdsResolver struct {
	ctx       context.Context
	cancelCtx context.CancelFunc
	target    resolver.Target
	cc        resolver.ClientConn

	// The underlying xdsClient (which performs all xDS requests and responses)
	// and its registry ID (which is sent to the balancer).
	client   xdsClientInterface
	clientID string

	// A channel for the watch API callback to write service updates on to. The
	// updates are read by the run goroutine and passed on to the ClientConn.
	updateCh *buffer.Unbounded

	// cancelWatch is the cancel function returned by the xdsClient watch API
	// (which is invoked when the resolver is closed). This is set in run(),
	// which is executed in a separate goroutine invoked from Build(). But
	// since Build() returns immediately, it is possible for Close() to be
	// called at anytime, and could race with the write to r.cancelWatch in
	// run(). Hence, needs to be guarded by a mutex.
	mu          sync.Mutex
	cancelWatch func()
}

const jsonFormatSC = `{
    "loadBalancingConfig":[
      {
        "experimental_cds":{
          "Cluster": %s
        }
      }
    ]
  }`

// run is a long running goroutine which registers a watcher for service
// updates. It then blocks on receiving service updates and passes it on the
// ClientConn.
func (r *xdsResolver) run() {
	r.mu.Lock()
	r.cancelWatch = r.client.WatchForServiceUpdate(r.target.Endpoint, r.handleServiceUpdate)
	r.mu.Unlock()

	for {
		select {
		case <-r.ctx.Done():
		case u := <-r.updateCh.Get():
			r.updateCh.Load()
			su := u.(*xdsclient.ServiceUpdate)
			sc := fmt.Sprintf(jsonFormatSC, su.Cluster)
			r.cc.UpdateState(resolver.State{
				ServiceConfig: r.cc.ParseServiceConfig(sc),
				Attributes:    attributes.New(registry.KeyName, r.clientID),
			})
		}
	}
}

// handleServiceUpdate is the callback which handles service updates. It writes
// the received update to the update channel, which is picked by the run
// goroutine.
func (r *xdsResolver) handleServiceUpdate(su xdsclient.ServiceUpdate, err error) {
	if r.ctx.Err() != nil {
		// Do not pass updates to the ClientConn once the resolver is closed.
		return
	}
	if err != nil {
		r.cc.ReportError(err)
		return
	}
	r.updateCh.Put(&su)
}

// ResolveNow is a no-op at this point.
func (*xdsResolver) ResolveNow(o resolver.ResolveNowOptions) {}

// Close closes the resolver, and also closes the underlying xdsClient.
func (r *xdsResolver) Close() {
	r.mu.Lock()
	if r.cancelWatch != nil {
		r.cancelWatch()
	}
	r.mu.Unlock()

	r.cancelCtx()
	r.client.Close()
	registry.RemoveFrom(r.clientID)
}
