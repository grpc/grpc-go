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
)

// ServiceUpdate contains update about the service.
type ServiceUpdate struct {
	// WeightedCluster is a map from cluster names (CDS resource to watch) to
	// their weights.
	WeightedCluster map[string]uint32
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

func (w *serviceUpdateWatcher) handleLDSResp(update ldsUpdate, err error) {
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

	if w.rdsName == update.routeName {
		// If the new routeName is same as the previous, don't cancel and
		// restart the RDS watch.
		return
	}
	w.rdsName = update.routeName
	if w.rdsCancel != nil {
		w.rdsCancel()
	}
	w.rdsCancel = w.c.watchRDS(update.routeName, w.handleRDSResp)
}

func (w *serviceUpdateWatcher) handleRDSResp(update rdsUpdate, err error) {
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
	w.serviceCb(ServiceUpdate{
		WeightedCluster: update.weightedCluster,
	}, nil)
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
