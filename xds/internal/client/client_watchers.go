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
)

// The value chosen here is based on the default value of the
// initial_fetch_timeout field in corepb.ConfigSource proto.
var defaultWatchExpiryTimeout = 15 * time.Second

const (
	ldsURL = "type.googleapis.com/envoy.api.v2.Listener"
	rdsURL = "type.googleapis.com/envoy.api.v2.RouteConfiguration"
	cdsURL = "type.googleapis.com/envoy.api.v2.Cluster"
	edsURL = "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment"
)

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

	ldsCallback ldsCallbackFunc
	rdsCallback rdsCallbackFunc
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
	case ldsURL:
		u = ldsUpdate{}
	case rdsURL:
		u = rdsUpdate{}
	case cdsURL:
		u = ClusterUpdate{}
	case edsURL:
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
	case ldsURL:
		watchers = c.ldsWatchers
	case rdsURL:
		watchers = c.rdsWatchers
	case cdsURL:
		watchers = c.cdsWatchers
	case edsURL:
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
		c.v2c.addWatch(wi.typeURL, resourceName)
	}
	// No matter what, add the new watcher to the set, so it's callback will be
	// call for new responses.
	s[wi] = true

	// If the resource is in cache, call the callback with the value.
	switch wi.typeURL {
	case ldsURL:
		if v, ok := c.ldsCache[resourceName]; ok {
			c.logger.Debugf("LDS resource with name %v found in cache: %+v", wi.target, v)
			wi.newUpdate(v)
		}
	case rdsURL:
		if v, ok := c.rdsCache[resourceName]; ok {
			c.logger.Debugf("RDS resource with name %v found in cache: %+v", wi.target, v)
			wi.newUpdate(v)
		}
	case cdsURL:
		if v, ok := c.cdsCache[resourceName]; ok {
			c.logger.Debugf("CDS resource with name %v found in cache: %+v", wi.target, v)
			wi.newUpdate(v)
		}
	case edsURL:
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
				c.v2c.removeWatch(wi.typeURL, resourceName)
				// Remove the resource from cache. When a watch for this
				// resource is added later, it will trigger a xDS request with
				// resource names, and client will receive new xDS responses.
				switch wi.typeURL {
				case ldsURL:
					delete(c.ldsCache, resourceName)
				case rdsURL:
					delete(c.rdsCache, resourceName)
				case cdsURL:
					delete(c.cdsCache, resourceName)
				case edsURL:
					delete(c.edsCache, resourceName)
				}
			}
		}
	}
}
