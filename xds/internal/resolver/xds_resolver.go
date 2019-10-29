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
	"fmt"
	"sync"

	"google.golang.org/grpc/resolver"

	xdsinternal "google.golang.org/grpc/xds/internal"
	xdsclient "google.golang.org/grpc/xds/internal/client"
)

const (
	// xDS balancer name is xds_experimental while resolver scheme is
	// xds-experimental since "_" is not a valid character in the URL.
	xdsScheme = "xds-experimental"
)

// NewBuilder creates a new implementation of the resolver.Builder interface
// for the xDS resolver.
func NewBuilder() resolver.Builder {
	return &xdsBuilder{}
}

type xdsBuilder struct{}

// Build helps implement the resolver.Builder interface.
func (b *xdsBuilder) Build(t resolver.Target, cc resolver.ClientConn, o resolver.BuildOption) (resolver.Resolver, error) {
	client, err := xdsclient.NewClient(xdsclient.WithResolverBuildOption(o))
	if err != nil {
		return nil, err
	}

	r := &xdsResolver{
		target: t,
		cc:     cc,
		client: client,
		// TODO: switch this to an unbounded channel.
		updateCh: make(chan *xdsclient.ServiceUpdate, 1),
	}
	r.ctx, r.cancelCtx = context.WithCancel(context.Background())
	go r.run()

	return r, nil
}

// Name helps implement the resolver.Builder interface.
func (*xdsBuilder) Scheme() string {
	return xdsScheme
}

// xdsResolver implements the resolver.Resolver interface.
//
// It registers a watcher for ServiceConfig updates with the xdsClient object
// (which performs LDS/RDS queries for the same), and passes the received
// updates to the ClientConn.
type xdsResolver struct {
	ctx       context.Context
	cancelCtx context.CancelFunc

	target resolver.Target
	cc     resolver.ClientConn
	client *xdsclient.Client

	// A channel for the watch API callback to write service updates on to. The
	// updates are read by the run goroutine and passed on to the ClientConn.
	updateCh chan *xdsclient.ServiceUpdate

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
		case su := <-r.updateCh:
			sc := fmt.Sprintf(jsonFormatSC, su.Cluster)
			r.cc.UpdateState(resolver.State{
				ServiceConfig: r.cc.ParseServiceConfig(sc),
				BalancerInfo:  xdsinternal.BalancerInfoCDS{ClientID: r.client.RegistryID()},
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
	r.updateCh <- &su
}

// ResolveNow is a no-op at this point.
func (*xdsResolver) ResolveNow(o resolver.ResolveNowOption) {}

// Close closes the resolver, and also closes the underlying xdsClient.
func (r *xdsResolver) Close() {
	r.mu.Lock()
	if r.cancelWatch != nil {
		r.cancelWatch()
	}
	r.mu.Unlock()

	r.cancelCtx()
	r.client.Close()
}
