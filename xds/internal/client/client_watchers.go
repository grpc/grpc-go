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
 *
 */

package client

import (
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/xds/internal/version"
)

// The value chosen here is based on the default value of the
// initial_fetch_timeout field in corepb.ConfigSource proto.
var defaultWatchExpiryTimeout = 15 * time.Second

type watchInfoState int

const (
	watchInfoStateStarted watchInfoState = iota
	watchInfoStateRespReceived
	watchInfoStateTimeout
	watchInfoStateCanceled
)

// watchInfo holds all the information from a watch() call.
type watchInfo struct {
	c       *Client
	typeURL string
	target  string

	ldsCallback func(ListenerUpdate, error)
	rdsCallback func(RouteConfigUpdate, error)
	cdsCallback func(ClusterUpdate, error)
	edsCallback func(EndpointsUpdate, error)

	expiryTimer *time.Timer

	// mu protects state, and c.scheduleCallback().
	// - No callback should be scheduled after watchInfo is canceled.
	// - No timeout error should be scheduled after watchInfo is resp received.
	mu    sync.Mutex
	state watchInfoState
}

func (wi *watchInfo) newUpdate(update interface{}) {
	wi.mu.Lock()
	defer wi.mu.Unlock()
	if wi.state == watchInfoStateCanceled {
		return
	}
	wi.state = watchInfoStateRespReceived
	wi.expiryTimer.Stop()
	wi.c.scheduleCallback(wi, update, nil)
}

func (wi *watchInfo) resourceNotFound() {
	wi.mu.Lock()
	defer wi.mu.Unlock()
	if wi.state == watchInfoStateCanceled {
		return
	}
	wi.state = watchInfoStateRespReceived
	wi.expiryTimer.Stop()
	wi.sendErrorLocked(NewErrorf(ErrorTypeResourceNotFound, "xds: %s target %s not found in received response", wi.typeURL, wi.target))
}

func (wi *watchInfo) timeout() {
	wi.mu.Lock()
	defer wi.mu.Unlock()
	if wi.state == watchInfoStateCanceled || wi.state == watchInfoStateRespReceived {
		return
	}
	wi.state = watchInfoStateTimeout
	wi.sendErrorLocked(fmt.Errorf("xds: %s target %s not found, watcher timeout", wi.typeURL, wi.target))
}

// Caller must hold wi.mu.
func (wi *watchInfo) sendErrorLocked(err error) {
	var (
		u interface{}
	)
	switch wi.typeURL {
	case version.V2ListenerURL:
		u = ListenerUpdate{}
	case version.V2RouteConfigURL:
		u = RouteConfigUpdate{}
	case version.V2ClusterURL:
		u = ClusterUpdate{}
	case version.V2EndpointsURL:
		u = EndpointsUpdate{}
	}
	wi.c.scheduleCallback(wi, u, err)
}

func (wi *watchInfo) cancel() {
	wi.mu.Lock()
	defer wi.mu.Unlock()
	if wi.state == watchInfoStateCanceled {
		return
	}
	wi.expiryTimer.Stop()
	wi.state = watchInfoStateCanceled
}

func (c *Client) watch(wi *watchInfo) (cancel func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logger.Debugf("new watch for type %v, resource name %v", wi.typeURL, wi.target)
	var watchers map[string]map[*watchInfo]bool
	switch wi.typeURL {
	case version.V2ListenerURL:
		watchers = c.ldsWatchers
	case version.V2RouteConfigURL:
		watchers = c.rdsWatchers
	case version.V2ClusterURL:
		watchers = c.cdsWatchers
	case version.V2EndpointsURL:
		watchers = c.edsWatchers
	}

	resourceName := wi.target
	s, ok := watchers[wi.target]
	if !ok {
		// If this is a new watcher, will ask lower level to send a new request with
		// the resource name.
		//
		// If this type+name is already being watched, will not notify the
		// underlying xdsv2Client.
		c.logger.Debugf("first watch for type %v, resource name %v, will send a new xDS request", wi.typeURL, wi.target)
		s = make(map[*watchInfo]bool)
		watchers[resourceName] = s
		c.apiClient.AddWatch(wi.typeURL, resourceName)
	}
	// No matter what, add the new watcher to the set, so it's callback will be
	// call for new responses.
	s[wi] = true

	// If the resource is in cache, call the callback with the value.
	switch wi.typeURL {
	case version.V2ListenerURL:
		if v, ok := c.ldsCache[resourceName]; ok {
			c.logger.Debugf("LDS resource with name %v found in cache: %+v", wi.target, v)
			wi.newUpdate(v)
		}
	case version.V2RouteConfigURL:
		if v, ok := c.rdsCache[resourceName]; ok {
			c.logger.Debugf("RDS resource with name %v found in cache: %+v", wi.target, v)
			wi.newUpdate(v)
		}
	case version.V2ClusterURL:
		if v, ok := c.cdsCache[resourceName]; ok {
			c.logger.Debugf("CDS resource with name %v found in cache: %+v", wi.target, v)
			wi.newUpdate(v)
		}
	case version.V2EndpointsURL:
		if v, ok := c.edsCache[resourceName]; ok {
			c.logger.Debugf("EDS resource with name %v found in cache: %+v", wi.target, v)
			wi.newUpdate(v)
		}
	}

	return func() {
		c.logger.Debugf("watch for type %v, resource name %v canceled", wi.typeURL, wi.target)
		wi.cancel()
		c.mu.Lock()
		defer c.mu.Unlock()
		if s := watchers[resourceName]; s != nil {
			// Remove this watcher, so it's callback will not be called in the
			// future.
			delete(s, wi)
			if len(s) == 0 {
				c.logger.Debugf("last watch for type %v, resource name %v canceled, will send a new xDS request", wi.typeURL, wi.target)
				// If this was the last watcher, also tell xdsv2Client to stop
				// watching this resource.
				delete(watchers, resourceName)
				c.apiClient.RemoveWatch(wi.typeURL, resourceName)
				// Remove the resource from cache. When a watch for this
				// resource is added later, it will trigger a xDS request with
				// resource names, and client will receive new xDS responses.
				switch wi.typeURL {
				case version.V2ListenerURL:
					delete(c.ldsCache, resourceName)
				case version.V2RouteConfigURL:
					delete(c.rdsCache, resourceName)
				case version.V2ClusterURL:
					delete(c.cdsCache, resourceName)
				case version.V2EndpointsURL:
					delete(c.edsCache, resourceName)
				}
			}
		}
	}
}

// watchLDS starts a listener watcher for the service..
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *Client) watchLDS(serviceName string, cb func(ListenerUpdate, error)) (cancel func()) {
	wi := &watchInfo{
		c:           c,
		typeURL:     version.V2ListenerURL,
		target:      serviceName,
		ldsCallback: cb,
	}

	wi.expiryTimer = time.AfterFunc(defaultWatchExpiryTimeout, func() {
		wi.timeout()
	})
	return c.watch(wi)
}

// watchRDS starts a listener watcher for the service..
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *Client) watchRDS(routeName string, cb func(RouteConfigUpdate, error)) (cancel func()) {
	wi := &watchInfo{
		c:           c,
		typeURL:     version.V2RouteConfigURL,
		target:      routeName,
		rdsCallback: cb,
	}

	wi.expiryTimer = time.AfterFunc(defaultWatchExpiryTimeout, func() {
		wi.timeout()
	})
	return c.watch(wi)
}

// WatchService uses LDS and RDS to discover information about the provided
// serviceName.
//
// WatchService can only be called once. The second call will not start a
// watcher and the callback will get an error. It's this case because an xDS
// client is expected to be used only by one ClientConn.
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *Client) WatchService(serviceName string, cb func(ServiceUpdate, error)) (cancel func()) {
	c.mu.Lock()
	if len(c.ldsWatchers) != 0 {
		go cb(ServiceUpdate{}, fmt.Errorf("unexpected WatchService when there's another service being watched"))
		c.mu.Unlock()
		return func() {}
	}
	c.mu.Unlock()

	w := &serviceUpdateWatcher{c: c, serviceCb: cb}
	w.ldsCancel = c.watchLDS(serviceName, w.handleLDSResp)

	return w.close
}

// serviceUpdateWatcher handles LDS and RDS response, and calls the service
// callback at the right time.
type serviceUpdateWatcher struct {
	c         *Client
	ldsCancel func()
	serviceCb func(ServiceUpdate, error)

	mu        sync.Mutex
	closed    bool
	rdsName   string
	rdsCancel func()
}

func (w *serviceUpdateWatcher) handleLDSResp(update ListenerUpdate, err error) {
	w.c.logger.Infof("xds: client received LDS update: %+v, err: %v", update, err)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if err != nil {
		// We check the error type and do different things. For now, the only
		// type we check is ResourceNotFound, which indicates the LDS resource
		// was removed, and besides sending the error to callback, we also
		// cancel the RDS watch.
		if ErrType(err) == ErrorTypeResourceNotFound && w.rdsCancel != nil {
			w.rdsCancel()
			w.rdsName = ""
			w.rdsCancel = nil
		}
		// The other error cases still return early without canceling the
		// existing RDS watch.
		w.serviceCb(ServiceUpdate{}, err)
		return
	}

	if w.rdsName == update.RouteConfigName {
		// If the new RouteConfigName is same as the previous, don't cancel and
		// restart the RDS watch.
		return
	}
	w.rdsName = update.RouteConfigName
	if w.rdsCancel != nil {
		w.rdsCancel()
	}
	w.rdsCancel = w.c.watchRDS(update.RouteConfigName, w.handleRDSResp)
}

func (w *serviceUpdateWatcher) handleRDSResp(update RouteConfigUpdate, err error) {
	w.c.logger.Infof("xds: client received RDS update: %+v, err: %v", update, err)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if w.rdsCancel == nil {
		// This mean only the RDS watch is canceled, can happen if the LDS
		// resource is removed.
		return
	}
	if err != nil {
		w.serviceCb(ServiceUpdate{}, err)
		return
	}
	w.serviceCb(ServiceUpdate(update), nil)
}

func (w *serviceUpdateWatcher) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	w.ldsCancel()
	if w.rdsCancel != nil {
		w.rdsCancel()
		w.rdsCancel = nil
	}
}

// WatchCluster uses CDS to discover information about the provided
// clusterName.
//
// WatchCluster can be called multiple times, with same or different
// clusterNames. Each call will start an independent watcher for the resource.
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *Client) WatchCluster(clusterName string, cb func(ClusterUpdate, error)) (cancel func()) {
	wi := &watchInfo{
		c:           c,
		typeURL:     version.V2ClusterURL,
		target:      clusterName,
		cdsCallback: cb,
	}

	wi.expiryTimer = time.AfterFunc(defaultWatchExpiryTimeout, func() {
		wi.timeout()
	})
	return c.watch(wi)
}

// WatchEndpoints uses EDS to discover endpoints in the provided clusterName.
//
// WatchEndpoints can be called multiple times, with same or different
// clusterNames. Each call will start an independent watcher for the resource.
//
// Note that during race (e.g. an xDS response is received while the user is
// calling cancel()), there's a small window where the callback can be called
// after the watcher is canceled. The caller needs to handle this case.
func (c *Client) WatchEndpoints(clusterName string, cb func(EndpointsUpdate, error)) (cancel func()) {
	wi := &watchInfo{
		c:           c,
		typeURL:     version.V2EndpointsURL,
		target:      clusterName,
		edsCallback: cb,
	}

	wi.expiryTimer = time.AfterFunc(defaultWatchExpiryTimeout, func() {
		wi.timeout()
	})
	return c.watch(wi)
}
