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
		if s, ok := c.ldsWatchers[wiu.wi.target]; ok && s.has(wiu.wi) {
			ccb = func() { wiu.wi.ldsCallback(wiu.update.(ldsUpdate), wiu.err) }
		}
	case rdsURL:
		if s, ok := c.rdsWatchers[wiu.wi.target]; ok && s.has(wiu.wi) {
			ccb = func() { wiu.wi.rdsCallback(wiu.update.(rdsUpdate), wiu.err) }
		}
	case cdsURL:
		if s, ok := c.cdsWatchers[wiu.wi.target]; ok && s.has(wiu.wi) {
			ccb = func() { wiu.wi.cdsCallback(wiu.update.(ClusterUpdate), wiu.err) }
		}
	case edsURL:
		if s, ok := c.edsWatchers[wiu.wi.target]; ok && s.has(wiu.wi) {
			ccb = func() { wiu.wi.edsCallback(wiu.update.(EndpointsUpdate), wiu.err) }
		}
	}
	c.mu.Unlock()

	if ccb != nil {
		ccb()
	}
}

// newUpdate is called by the underlying xdsv2Client when it receives an xDS
// response.
//
// A response can contain multiple resources. They will be parsed and put in a
// map from resource name to the resource content.
func (c *Client) newUpdate(typeURL string, d map[string]interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var watchers map[string]*watchInfoSet
	switch typeURL {
	case ldsURL:
		watchers = c.ldsWatchers
	case rdsURL:
		watchers = c.rdsWatchers
	case cdsURL:
		watchers = c.cdsWatchers
	case edsURL:
		watchers = c.edsWatchers
	}
	for name, update := range d {
		if s, ok := watchers[name]; ok {
			s.forEach(func(wi *watchInfo) {
				c.scheduleCallback(wi, update, nil)
			})
		}
	}
	// TODO: for LDS and CDS, handle removing resources, which means if a
	// resource exists in the previous update, but not in the new update. This
	// needs the balancers and resolvers to handle errors correctly.
	//
	// var emptyUpdate interface{}
	// switch typeURL {
	// case ldsURL:
	// 	emptyUpdate = ldsUpdate{}
	// case cdsURL:
	// 	emptyUpdate = ClusterUpdate{}
	// }
	// if emptyUpdate != nil {
	// 	for name, s := range watchers {
	// 		if _, ok := d[name]; !ok {
	// 			s.forEach(func(wi *watchInfo) {
	// 				c.scheduleCallback(wi, emptyUpdate, errorf(errTypeResourceRemoved, "resource removed"))
	// 			})
	// 		}
	// 	}
	// }
	c.syncCache(typeURL, d)
}

// syncCache adds updates to the cache. Resource is cached only if there's an
// active watcher for it.
//
// Caller must hold c.mu.
func (c *Client) syncCache(typeURL string, d map[string]interface{}) {
	var f func(name string, update interface{})
	switch typeURL {
	case ldsURL:
		f = func(name string, update interface{}) {
			if _, ok := c.ldsWatchers[name]; ok {
				c.logger.Debugf("LDS resource with name %v, value %+v added to cache", name, update)
				c.ldsCache[name] = update.(ldsUpdate)
			}
		}
	case rdsURL:
		f = func(name string, update interface{}) {
			if _, ok := c.rdsWatchers[name]; ok {
				c.logger.Debugf("RDS resource with name %v, value %+v added to cache", name, update)
				c.rdsCache[name] = update.(rdsUpdate)
			}
		}
	case cdsURL:
		f = func(name string, update interface{}) {
			if _, ok := c.cdsWatchers[name]; ok {
				c.logger.Debugf("CDS resource with name %v, value %+v added to cache", name, update)
				c.cdsCache[name] = update.(ClusterUpdate)
			}
		}
	case edsURL:
		f = func(name string, update interface{}) {
			if _, ok := c.edsWatchers[name]; ok {
				c.logger.Debugf("EDS resource with name %v, value %+v added to cache", name, update)
				c.edsCache[name] = update.(EndpointsUpdate)
			}
		}
	}
	for name, update := range d {
		f(name, update)
	}
	// TODO: remove item from cache for LDS and CDS. For RDS and EDS, remove if
	// the corresponding LDS/CDS data was removed?
}
