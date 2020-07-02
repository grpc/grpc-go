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

type watcherInfoWithUpdate struct {
	wi     *watchInfo
	update interface{}
	err    error
}

// scheduleCallback should only be called by methods of watchInfo, which checks
// for watcher states and maintain consistency.
func (c *Client) scheduleCallback(wi *watchInfo, update interface{}, err error) {
	c.updateCh.Put(&watcherInfoWithUpdate{
		wi:     wi,
		update: update,
		err:    err,
	})
}

func (c *Client) callCallback(wiu *watcherInfoWithUpdate) {
	c.mu.Lock()
	// Use a closure to capture the callback and type assertion, to save one
	// more switch case.
	//
	// The callback must be called without c.mu. Otherwise if the callback calls
	// another watch() inline, it will cause a deadlock. This leaves a small
	// window that a watcher's callback could be called after the watcher is
	// canceled, and the user needs to take care of it.
	var ccb func()
	switch wiu.wi.typeURL {
	case ldsURL:
		if s, ok := c.ldsWatchers[wiu.wi.target]; ok && s[wiu.wi] {
			ccb = func() { wiu.wi.ldsCallback(wiu.update.(ldsUpdate), wiu.err) }
		}
	case rdsURL:
		if s, ok := c.rdsWatchers[wiu.wi.target]; ok && s[wiu.wi] {
			ccb = func() { wiu.wi.rdsCallback(wiu.update.(rdsUpdate), wiu.err) }
		}
	case cdsURL:
		if s, ok := c.cdsWatchers[wiu.wi.target]; ok && s[wiu.wi] {
			ccb = func() { wiu.wi.cdsCallback(wiu.update.(ClusterUpdate), wiu.err) }
		}
	case edsURL:
		if s, ok := c.edsWatchers[wiu.wi.target]; ok && s[wiu.wi] {
			ccb = func() { wiu.wi.edsCallback(wiu.update.(EndpointsUpdate), wiu.err) }
		}
	}
	c.mu.Unlock()

	if ccb != nil {
		ccb()
	}
}

// newLDSUpdate is called by the underlying xdsv2Client when it receives an xDS
// response.
//
// A response can contain multiple resources. They will be parsed and put in a
// map from resource name to the resource content.
func (c *Client) newLDSUpdate(updates map[string]ldsUpdate) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for name, update := range updates {
		if s, ok := c.ldsWatchers[name]; ok {
			for wi := range s {
				wi.newUpdate(update)
			}
			// Sync cache.
			c.logger.Debugf("LDS resource with name %v, value %+v added to cache", name, update)
			c.ldsCache[name] = update
		}
	}
	for name := range c.ldsCache {
		if _, ok := updates[name]; !ok {
			// If resource exists in cache, but not in the new update, delete it
			// from cache, and also send an resource not found error to indicate
			// resource removed.
			delete(c.ldsCache, name)
			for wi := range c.ldsWatchers[name] {
				wi.resourceNotFound()
			}
		}
	}
	// When LDS resource is removed, we don't delete corresponding RDS cached
	// data. The RDS watch will be canceled, and cache entry is removed when the
	// last watch is canceled.
}

// newRDSUpdate is called by the underlying xdsv2Client when it receives an xDS
// response.
//
// A response can contain multiple resources. They will be parsed and put in a
// map from resource name to the resource content.
func (c *Client) newRDSUpdate(updates map[string]rdsUpdate) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for name, update := range updates {
		if s, ok := c.rdsWatchers[name]; ok {
			for wi := range s {
				wi.newUpdate(update)
			}
			// Sync cache.
			c.logger.Debugf("RDS resource with name %v, value %+v added to cache", name, update)
			c.rdsCache[name] = update
		}
	}
}

// newCDSUpdate is called by the underlying xdsv2Client when it receives an xDS
// response.
//
// A response can contain multiple resources. They will be parsed and put in a
// map from resource name to the resource content.
func (c *Client) newCDSUpdate(updates map[string]ClusterUpdate) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for name, update := range updates {
		if s, ok := c.cdsWatchers[name]; ok {
			for wi := range s {
				wi.newUpdate(update)
			}
			// Sync cache.
			c.logger.Debugf("CDS resource with name %v, value %+v added to cache", name, update)
			c.cdsCache[name] = update
		}
	}
	for name := range c.cdsCache {
		if _, ok := updates[name]; !ok {
			// If resource exists in cache, but not in the new update, delete it
			// from cache, and also send an resource not found error to indicate
			// resource removed.
			delete(c.cdsCache, name)
			for wi := range c.cdsWatchers[name] {
				wi.resourceNotFound()
			}
		}
	}
	// When CDS resource is removed, we don't delete corresponding EDS cached
	// data. The EDS watch will be canceled, and cache entry is removed when the
	// last watch is canceled.
}

// newEDSUpdate is called by the underlying xdsv2Client when it receives an xDS
// response.
//
// A response can contain multiple resources. They will be parsed and put in a
// map from resource name to the resource content.
func (c *Client) newEDSUpdate(updates map[string]EndpointsUpdate) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for name, update := range updates {
		if s, ok := c.edsWatchers[name]; ok {
			for wi := range s {
				wi.newUpdate(update)
			}
			// Sync cache.
			c.logger.Debugf("EDS resource with name %v, value %+v added to cache", name, update)
			c.edsCache[name] = update
		}
	}
}
