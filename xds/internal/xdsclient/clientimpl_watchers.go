/*
 *
 * Copyright 2020 gRPC authors.
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
 */

package xdsclient

import (
	"fmt"
	"sync"

	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

// This is only required temporarily, while we modify the
// clientImpl.WatchListener API to be implemented via the wrapper
// WatchListener() API which calls the WatchResource() API.
type listenerWatcher struct {
	resourceName string
	cb           func(xdsresource.ListenerUpdate, error)
}

func (l *listenerWatcher) OnResourceChanged(update xdsresource.ResourceData) {
	lu := update.(*listenerResourceData)
	l.cb(lu.Resource, nil)
}

func (l *listenerWatcher) OnError(err error) {
	l.cb(xdsresource.ListenerUpdate{}, err)
}

func (l *listenerWatcher) OnResourceDoesNotExist() {
	err := xdsresource.NewErrorf(xdsresource.ErrorTypeResourceNotFound, "Resource name %q of type Listener not found in received response", l.resourceName)
	l.cb(xdsresource.ListenerUpdate{}, err)
}

// WatchListener uses LDS to discover information about the Listener resource
// identified by resourceName.
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *clientImpl) WatchListener(resourceName string, cb func(xdsresource.ListenerUpdate, error)) (cancel func()) {
	watcher := &listenerWatcher{resourceName: resourceName, cb: cb}
	return WatchListener(c, resourceName, watcher)
}

// This is only required temporarily, while we modify the
// clientImpl.WatchRouteConfig API to be implemented via the wrapper
// WatchRouteConfig() API which calls the WatchResource() API.
type routeConfigWatcher struct {
	resourceName string
	cb           func(xdsresource.RouteConfigUpdate, error)
}

func (r *routeConfigWatcher) OnResourceChanged(update xdsresource.ResourceData) {
	ru := update.(*routeConfigResourceData)
	r.cb(ru.Resource, nil)
}

func (r *routeConfigWatcher) OnError(err error) {
	r.cb(xdsresource.RouteConfigUpdate{}, err)
}

func (r *routeConfigWatcher) OnResourceDoesNotExist() {
	err := xdsresource.NewErrorf(xdsresource.ErrorTypeResourceNotFound, "Resource name %q of type RouteConfiguration not found in received response", r.resourceName)
	r.cb(xdsresource.RouteConfigUpdate{}, err)
}

// WatchRouteConfig uses RDS to discover information about the
// RouteConfiguration resource identified by resourceName.
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *clientImpl) WatchRouteConfig(resourceName string, cb func(xdsresource.RouteConfigUpdate, error)) (cancel func()) {
	watcher := &routeConfigWatcher{resourceName: resourceName, cb: cb}
	return WatchRouteConfig(c, resourceName, watcher)
}

// This is only required temporarily, while we modify the
// clientImpl.WatchCluster API to be implemented via the wrapper WatchCluster()
// API which calls the WatchResource() API.
type clusterWatcher struct {
	resourceName string
	cb           func(xdsresource.ClusterUpdate, error)
}

func (c *clusterWatcher) OnResourceChanged(update xdsresource.ResourceData) {
	cu := update.(*clusterResourceData)
	c.cb(cu.Resource, nil)
}

func (c *clusterWatcher) OnError(err error) {
	c.cb(xdsresource.ClusterUpdate{}, err)
}

func (c *clusterWatcher) OnResourceDoesNotExist() {
	err := xdsresource.NewErrorf(xdsresource.ErrorTypeResourceNotFound, "Resource name %q of type Cluster not found in received response", c.resourceName)
	c.cb(xdsresource.ClusterUpdate{}, err)
}

// WatchCluster uses CDS to discover information about the Cluster resource
// identified by resourceName.
//
// WatchCluster can be called multiple times, with same or different
// clusterNames. Each call will start an independent watcher for the resource.
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *clientImpl) WatchCluster(resourceName string, cb func(xdsresource.ClusterUpdate, error)) (cancel func()) {
	watcher := &clusterWatcher{resourceName: resourceName, cb: cb}
	return WatchCluster(c, resourceName, watcher)
}

// This is only required temporarily, while we modify the
// clientImpl.WatchEndpoints API to be implemented via the wrapper
// WatchEndpoints() API which calls the WatchResource() API.
type endpointsWatcher struct {
	resourceName string
	cb           func(xdsresource.EndpointsUpdate, error)
}

func (c *endpointsWatcher) OnResourceChanged(update xdsresource.ResourceData) {
	eu := update.(*endpointsResourceData)
	c.cb(eu.Resource, nil)
}

func (c *endpointsWatcher) OnError(err error) {
	c.cb(xdsresource.EndpointsUpdate{}, err)
}

func (c *endpointsWatcher) OnResourceDoesNotExist() {
	err := xdsresource.NewErrorf(xdsresource.ErrorTypeResourceNotFound, "Resource name %q of type Endpoints not found in received response", c.resourceName)
	c.cb(xdsresource.EndpointsUpdate{}, err)
}

// WatchEndpoints uses EDS to discover information about the
// ClusterLoadAssignment resource identified by resourceName.
//
// WatchEndpoints can be called multiple times, with same or different
// clusterNames. Each call will start an independent watcher for the resource.
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *clientImpl) WatchEndpoints(resourceName string, cb func(xdsresource.EndpointsUpdate, error)) (cancel func()) {
	watcher := &endpointsWatcher{resourceName: resourceName, cb: cb}
	return WatchEndpoints(c, resourceName, watcher)
}

// WatchResource uses xDS to discover the provided resource name. The resource
// type implementation determines how the xDS request is sent out and how a
// response is deserialized and validated. Upon receipt of a response from the
// management server, appropriate callback on the watcher is invoked.
//
// Most callers will not use this API directly but rather via a
// resource-type-specific wrapper API provided by the relevant resource type
// implementation.
func (c *clientImpl) WatchResource(rType xdsresource.Type, resourceName string, watcher ResourceWatcher) (cancel func()) {
	if err := c.resourceTypes.maybeRegister(rType); err != nil {
		c.serializer.Schedule(func() { watcher.OnError(err) })
		return func() {}
	}

	// TODO: Make ParseName return an error if parsing fails, and
	// schedule the OnError callback in that case.
	n := xdsresource.ParseName(resourceName)
	a, unref, err := c.findAuthority(n)
	if err != nil {
		c.serializer.Schedule(func() { watcher.OnError(err) })
		return func() {}
	}
	cancelF := a.watchResource(rType, n.String(), watcher)
	return func() {
		cancelF()
		unref()
	}
}

// A registry of xdsresource.Type implementations indexed by their corresponding
// type URLs. Registration for a xdsresource\.Type happens the first time a watch
// for a resource of this type is invoked.
type resourceTypeRegistry struct {
	mu    sync.Mutex
	types map[string]xdsresource.Type
}

func newResourceTypeRegistry() *resourceTypeRegistry {
	return &resourceTypeRegistry{types: make(map[string]xdsresource.Type)}
}

func (r *resourceTypeRegistry) get(url string) xdsresource.Type {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.types[url]
}

func (r *resourceTypeRegistry) maybeRegister(rType xdsresource.Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	urls := []string{rType.V2TypeURL(), rType.V3TypeURL()}
	for _, u := range urls {
		if u == "" {
			// Silently ignore unsupported versions of the resource.
			continue
		}
		typ, ok := r.types[u]
		if ok && typ != rType {
			return fmt.Errorf("attempt to re-register a resource type implementation for %v", rType.TypeEnum())
		}
		r.types[u] = rType
	}
	return nil
}
