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
func (c *clientImpl) scheduleCallback(wi *watchInfo, update interface{}, err error) {
	c.updateCh.Put(&watcherInfoWithUpdate{
		wi:     wi,
		update: update,
		err:    err,
	})
}

func (c *clientImpl) callCallback(wiu *watcherInfoWithUpdate) {
	c.mu.Lock()
	// Use a closure to capture the callback and type assertion, to save one
	// more switch case.
	//
	// The callback must be called without c.mu. Otherwise if the callback calls
	// another watch() inline, it will cause a deadlock. This leaves a small
	// window that a watcher's callback could be called after the watcher is
	// canceled, and the user needs to take care of it.
	var ccb func()
	switch wiu.wi.rType {
	case ListenerResource:
		if s, ok := c.ldsWatchers[wiu.wi.target]; ok && s[wiu.wi] {
			ccb = func() { wiu.wi.ldsCallback(wiu.update.(ListenerUpdate), wiu.err) }
		}
	case RouteConfigResource:
		if s, ok := c.rdsWatchers[wiu.wi.target]; ok && s[wiu.wi] {
			ccb = func() { wiu.wi.rdsCallback(wiu.update.(RouteConfigUpdate), wiu.err) }
		}
	case ClusterResource:
		if s, ok := c.cdsWatchers[wiu.wi.target]; ok && s[wiu.wi] {
			ccb = func() { wiu.wi.cdsCallback(wiu.update.(ClusterUpdate), wiu.err) }
		}
	case EndpointsResource:
		if s, ok := c.edsWatchers[wiu.wi.target]; ok && s[wiu.wi] {
			ccb = func() { wiu.wi.edsCallback(wiu.update.(EndpointsUpdate), wiu.err) }
		}
	}
	c.mu.Unlock()

	if ccb != nil {
		ccb()
	}
}

// NewListeners is called by the underlying xdsAPIClient when it receives an
// xDS response.
//
// A response can contain multiple resources. They will be parsed and put in a
// map from resource name to the resource content.
func (c *clientImpl) NewListeners(updates map[string]ListenerUpdate, metadata UpdateMetadata) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if metadata.ErrState != nil {
		// On NACK, update overall version and status to the NACKed resp.
		c.ldsVersion = metadata.ErrState.Version
		c.ldsStatus = ServiceStatusNACKed
		for name := range updates {
			if _, ok := c.ldsWatchers[name]; ok {
				// On error, keep previous version and status. Only update error.
				updateCopy := c.ldsCache[name]
				updateCopy.MD.ErrState = metadata.ErrState
				c.ldsCache[name] = updateCopy
				// TODO: send the NACK error to the watcher.
			}
		}
		return
	}

	// If no error received, the status is ACK.
	c.ldsVersion = metadata.Version
	c.ldsStatus = ServiceStatusACKed
	for name, update := range updates {
		if s, ok := c.ldsWatchers[name]; ok {
			// Only send the update if this is not an error.
			for wi := range s {
				wi.newUpdate(update)
			}
			// Sync cache.
			c.logger.Debugf("LDS resource with name %v, value %+v added to cache", name, update)
			c.ldsCache[name] = LDSUpdateWithMD{
				Update: update,
				MD:     metadata,
			}
		}
	}
	// Resources not in the new update were removed by the server, so delete
	// them.
	for name := range c.ldsCache {
		if _, ok := updates[name]; !ok {
			// If resource exists in cache, but not in the new update, delete it
			// from cache, and also send an resource not found error to indicate
			// resource removed.
			// FIXME: keep metadata in cache, but update state to REMOVED.
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

// NewRouteConfigs is called by the underlying xdsAPIClient when it receives an
// xDS response.
//
// A response can contain multiple resources. They will be parsed and put in a
// map from resource name to the resource content.
func (c *clientImpl) NewRouteConfigs(updates map[string]RouteConfigUpdate, metadata UpdateMetadata) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if metadata.ErrState != nil {
		// On NACK, update overall version and status to the NACKed resp.
		c.rdsVersion = metadata.ErrState.Version
		c.rdsStatus = ServiceStatusNACKed
		for name := range updates {
			if _, ok := c.rdsWatchers[name]; ok {
				// On error, keep previous version and status. Only update error.
				updateCopy := c.rdsCache[name]
				updateCopy.MD.ErrState = metadata.ErrState
				c.rdsCache[name] = updateCopy
				// TODO: send the NACK error to the watcher.
			}
		}
		return
	}

	// If no error received, the status is ACK.
	c.rdsVersion = metadata.Version
	c.rdsStatus = ServiceStatusACKed
	for name, update := range updates {
		if s, ok := c.rdsWatchers[name]; ok {
			// Only send the update if this is not an error.
			for wi := range s {
				wi.newUpdate(update)
			}
			// Sync cache.
			c.logger.Debugf("RDS resource with name %v, value %+v added to cache", name, update)
			c.rdsCache[name] = RDSUpdateWithMD{
				Update: update,
				MD:     metadata,
			}
		}
	}
}

// NewClusters is called by the underlying xdsAPIClient when it receives an xDS
// response.
//
// A response can contain multiple resources. They will be parsed and put in a
// map from resource name to the resource content.
func (c *clientImpl) NewClusters(updates map[string]ClusterUpdate, metadata UpdateMetadata) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if metadata.ErrState != nil {
		// On NACK, update overall version and status to the NACKed resp.
		c.cdsVersion = metadata.ErrState.Version
		c.cdsStatus = ServiceStatusNACKed
		for name := range updates {
			if _, ok := c.cdsWatchers[name]; ok {
				// On error, keep previous version and status. Only update error.
				updateCopy := c.cdsCache[name]
				updateCopy.MD.ErrState = metadata.ErrState
				c.cdsCache[name] = updateCopy
				// TODO: send the NACK error to the watcher.
			}
		}
		return
	}

	// If no error received, the status is ACK.
	c.cdsVersion = metadata.Version
	c.cdsStatus = ServiceStatusACKed
	for name, update := range updates {
		if s, ok := c.cdsWatchers[name]; ok {
			// Only send the update if this is not an error.
			for wi := range s {
				wi.newUpdate(update)
			}
			// Sync cache.
			c.logger.Debugf("CDS resource with name %v, value %+v added to cache", name, update)
			c.cdsCache[name] = CDSUpdateWithMD{
				Update: update,
				MD:     metadata,
			}
		}
	}
	// Resources not in the new update were removed by the server, so delete
	// them.
	c.cdsStatus = ServiceStatusACKed
	for name := range c.cdsCache {
		if _, ok := updates[name]; !ok {
			// If resource exists in cache, but not in the new update, delete it
			// from cache, and also send an resource not found error to indicate
			// resource removed.
			// FIXME: keep metadata in cache, but update state to REMOVED.
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

// NewEndpoints is called by the underlying xdsAPIClient when it receives an
// xDS response.
//
// A response can contain multiple resources. They will be parsed and put in a
// map from resource name to the resource content.
func (c *clientImpl) NewEndpoints(updates map[string]EndpointsUpdate, metadata UpdateMetadata) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if metadata.ErrState != nil {
		// On NACK, update overall version and status to the NACKed resp.
		c.edsVersion = metadata.ErrState.Version
		c.edsStatus = ServiceStatusNACKed
		for name := range updates {
			if _, ok := c.edsWatchers[name]; ok {
				// On error, keep previous version and status. Only update error.
				updateCopy := c.edsCache[name]
				updateCopy.MD.ErrState = metadata.ErrState
				c.edsCache[name] = updateCopy
				// TODO: send the NACK error to the watcher.
			}
		}
		return
	}

	// If no error received, the status is ACK.
	c.edsVersion = metadata.Version
	c.edsStatus = ServiceStatusACKed
	for name, update := range updates {
		if s, ok := c.edsWatchers[name]; ok {
			// Only send the update if this is not an error.
			for wi := range s {
				wi.newUpdate(update)
			}
			// Sync cache.
			c.logger.Debugf("EDS resource with name %v, value %+v added to cache", name, update)
			c.edsCache[name] = EDSUpdateWithMD{
				Update: update,
				MD:     metadata,
			}
		}
	}
}
