/*
 *
 * Copyright 2021 gRPC authors.
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

package server

import (
	"sync"

	"google.golang.org/grpc/xds/internal/xdsclient"
)

// rdsHandlerUpdate wraps the full RouteConfigUpdate that are dynamically queried for a given
// server side listener.
type rdsHandlerUpdate struct {
	rdsUpdates map[string]xdsclient.RouteConfigUpdate
	err        error
}

// rdsHandler handles any RDS queries that need to be started for a given server
// side listeners Filter Chains (i.e. not inline).
type rdsHandler struct {
	parent *listenerWrapper

	rdsMutex sync.Mutex

	rdsUpdates map[string]xdsclient.RouteConfigUpdate
	rdsCancels map[string]func()

	updateChannel chan rdsHandlerUpdate
}

// newRDSHandler is expected to called once on instantiation of a wrapped
// listener. On any LDS updates the wrapped listener receives, the listener
// should update the handler with the route names (which specify dynamic RDS)
// using the function below.
func newRDSHandler(parent *listenerWrapper) *rdsHandler {
	return &rdsHandler{
		parent:        parent,
		updateChannel: make(chan rdsHandlerUpdate, 1),
		rdsUpdates:    make(map[string]xdsclient.RouteConfigUpdate),
		rdsCancels:    make(map[string]func()),
	}
}

// updateRouteNamesToWatch handles a list of route names to watch for a given
// server side listener (if a filter chain specifies dynamic RDS configuration).
// This function handles all the logic with respect to any routes that may have
// been added or deleted as compared to what was previously present.
func (rh *rdsHandler) updateRouteNamesToWatch(routeNamesToWatch map[string]bool) {
	rh.rdsMutex.Lock()
	defer rh.rdsMutex.Unlock()
	// Add and start watches for any routes for any new routes in routeNamesToWatch.
	for routeName := range routeNamesToWatch {
		if _, ok := rh.rdsCancels[routeName]; !ok {
			rh.rdsCancels[routeName] = rh.parent.xdsC.WatchRouteConfig(routeName, rh.handleRouteUpdate)
		}
	}

	// Delete and cancel watches for any routes from persisted routeNamesToWatch
	// that are no longer present.
	for routeName := range rh.rdsCancels {
		if _, stillRDS := routeNamesToWatch[routeName]; !stillRDS {
			rh.rdsCancels[routeName]()
			delete(rh.rdsCancels, routeName)
			delete(rh.rdsUpdates, routeName)
		}
	}
}

// handleRouteUpdate persists the route config for a given route name, and also
// sends an update to the Listener Wrapper on an error received or if the rds
// handler has a full collection of updates.
func (rh *rdsHandler) handleRouteUpdate(update xdsclient.RouteConfigUpdate, err error) {
	rh.rdsMutex.Lock()
	defer rh.rdsMutex.Unlock()
	// Note: this doesn't need a check for if name in RouteConfigUpdate is wrong
	// (i.e. not started a watch for). This will be validated in xdsclient.
	if err != nil {
		// For a rdsHandler update, the only update wrapped listener cares about
		// is most recent one, so opportunistically drain the update before
		// sending the new update.
		select {
		case <-rh.updateChannel:
		default:
		}
		rh.updateChannel <- rdsHandlerUpdate{err: err}
		return
	}
	rh.rdsUpdates[update.RouteConfigName] = update

	// If the full list (determined by length) of rdsUpdates have successfully updated,
	// the listener is ready to be updated.
	if len(rh.rdsUpdates) == len(rh.rdsCancels) {
		// For a rdsHandler update, the only update lis wrapper cares about is most recent one,
		// so opportunistically drain the update before sending the new update.
		select {
		case <-rh.updateChannel:
		default:
		}
		rh.updateChannel <- rdsHandlerUpdate{rdsUpdates: rh.rdsUpdates}
	}
}

// close() is meant to be called by wrapped listener when the wrapped listener is closed,
// and it cleans up resources by canceling all the active RDS watches.
func (rh *rdsHandler) close() {
	rh.rdsMutex.Lock()
	defer rh.rdsMutex.Unlock()
	for _, cancel := range rh.rdsCancels {
		cancel()
	}
}
