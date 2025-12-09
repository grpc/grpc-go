/*
 *
 * Copyright 2025 gRPC authors.
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

package xdsdepmgr

import (
	"context"
	"fmt"
	"net/url"

	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

type listenerWatcher struct {
	resourceName string
	cancel       func()
	depMgr       *DependencyManager
}

func newListenerWatcher(resourceName string, depMgr *DependencyManager) *listenerWatcher {
	lw := &listenerWatcher{resourceName: resourceName, depMgr: depMgr}
	lw.cancel = xdsresource.WatchListener(depMgr.xdsClient, resourceName, lw)
	return lw
}

func (l *listenerWatcher) ResourceChanged(update *xdsresource.ListenerUpdate, onDone func()) {
	l.depMgr.onListenerResourceUpdate(update, onDone)
}

func (l *listenerWatcher) ResourceError(err error, onDone func()) {
	l.depMgr.onListenerResourceError(err, onDone)
}

func (l *listenerWatcher) AmbientError(err error, onDone func()) {
	l.depMgr.onListenerResourceAmbientError(err, onDone)
}

func (l *listenerWatcher) stop() {
	l.cancel()
	if l.depMgr.logger.V(2) {
		l.depMgr.logger.Infof("Canceling watch on Listener resource %q", l.resourceName)
	}
}

type routeConfigWatcher struct {
	resourceName string
	cancel       func()
	depMgr       *DependencyManager
}

func newRouteConfigWatcher(resourceName string, depMgr *DependencyManager) *routeConfigWatcher {
	rw := &routeConfigWatcher{resourceName: resourceName, depMgr: depMgr}
	rw.cancel = xdsresource.WatchRouteConfig(depMgr.xdsClient, resourceName, rw)
	return rw
}

func (r *routeConfigWatcher) ResourceChanged(u *xdsresource.RouteConfigUpdate, onDone func()) {
	r.depMgr.onRouteConfigResourceUpdate(r.resourceName, u, onDone)
}

func (r *routeConfigWatcher) ResourceError(err error, onDone func()) {
	r.depMgr.onRouteConfigResourceError(r.resourceName, err, onDone)
}

func (r *routeConfigWatcher) AmbientError(err error, onDone func()) {
	r.depMgr.onRouteConfigResourceAmbientError(r.resourceName, err, onDone)
}

func (r *routeConfigWatcher) stop() {
	r.cancel()
	if r.depMgr.logger.V(2) {
		r.depMgr.logger.Infof("Canceling watch on RouteConfiguration resource %q", r.resourceName)
	}
}

type clusterWatcher struct {
	resourceName string
	depMgr       *DependencyManager
}

func (c *clusterWatcher) ResourceChanged(u *xdsresource.ClusterUpdate, onDone func()) {
	c.depMgr.onClusterResourceUpdate(c.resourceName, u, onDone)
}

func (c *clusterWatcher) ResourceError(err error, onDone func()) {
	c.depMgr.onClusterResourceError(c.resourceName, err, onDone)
}

func (c *clusterWatcher) AmbientError(err error, onDone func()) {
	c.depMgr.onClusterAmbientError(c.resourceName, err, onDone)
}

type endpointsWatcher struct {
	resourceName string
	depMgr       *DependencyManager
}

func (e *endpointsWatcher) ResourceChanged(u *xdsresource.EndpointsUpdate, onDone func()) {
	e.depMgr.onEndpointUpdate(e.resourceName, u, onDone)
}

func (e *endpointsWatcher) ResourceError(err error, onDone func()) {
	e.depMgr.onEndpointResourceError(e.resourceName, err, onDone)
}

func (e *endpointsWatcher) AmbientError(err error, onDone func()) {
	e.depMgr.onEndpointAmbientError(e.resourceName, err, onDone)
}

// dnsResolverState watches updates for the given DNS hostname. It implements
// resolver.ClientConn interface to work with the DNS resolver.
type dnsResolver struct {
	target string
	dnsR   resolver.Resolver
	depMgr *DependencyManager

	//serializer is used to make sure that any methods on the resolver can be
	//called from inside th Build function which is a garuntee that
	//implementations of resolver.Clientconn need to maintain.
	serializer       grpcsync.CallbackSerializer
	serializerCancel func()
}

type dnsResolverState struct {
	resolver       *dnsResolver
	updateReceived bool
	err            error
	lastUpdate     *xdsresource.DNSUpdate
	cancelResolver func()
}

// dnsResolverState needs to implement resolver.ClientConn interface to receive
// updates from the real DNS resolver.
func (dr *dnsResolver) UpdateState(state resolver.State) error {
	dr.serializer.TrySchedule(func(context.Context) {
		dr.depMgr.onDNSUpdate(dr.target, &state)
	})
	return nil
}

func (dr *dnsResolver) ReportError(err error) {
	dr.serializer.TrySchedule(func(context.Context) {
		dr.depMgr.onDNSError(dr.target, err)
	})
}

func (dr *dnsResolver) NewAddress(addresses []resolver.Address) {
	dr.UpdateState(resolver.State{Addresses: addresses})
}

func (dr *dnsResolver) ParseServiceConfig(string) *serviceconfig.ParseResult {
	return &serviceconfig.ParseResult{Err: fmt.Errorf("service config not supported")}
}

// NewDNSResolver creates a new DNS resolver for the given target.
func newDNSResolver(target string, depMgr *DependencyManager) *dnsResolverState {
	ctx, cancel := context.WithCancel(context.Background())
	dr := &dnsResolver{target: target, depMgr: depMgr, serializer: *grpcsync.NewCallbackSerializer(ctx), serializerCancel: cancel}
	drState := &dnsResolverState{resolver: dr, cancelResolver: func() {}}
	u, err := url.Parse("dns:///" + target)
	if err != nil {
		drState.updateReceived = true
		drState.err = err
		drState.resolver.depMgr.logger.Warningf("Error while parsing DNS target %q: %v", target, drState.resolver.depMgr.annotateErrorWithNodeID(err))
		return drState
	}
	r, err := resolver.Get("dns").Build(resolver.Target{URL: *u}, dr, resolver.BuildOptions{})
	if err != nil {
		drState.updateReceived = true
		drState.err = err
		drState.resolver.depMgr.logger.Warningf("Error while building DNS resolver for target %q: %v", target, drState.resolver.depMgr.annotateErrorWithNodeID(err))
		return drState
	}
	drState.resolver.dnsR = r
	drState.cancelResolver = func() {
		drState.resolver.serializerCancel()
		<-drState.resolver.serializer.Done()
		r.Close()
	}
	return drState
}
