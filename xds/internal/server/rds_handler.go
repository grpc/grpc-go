package server

import (
	"sync"

	"google.golang.org/grpc/xds/internal/xdsclient"
)

// rdsHandlerUpdate wraps the full RouteConfigUpdate that are dynamically queried for a given
// server side listener.
type rdsHandlerUpdate struct {
	rdsUpdates map[string]xdsclient.RouteConfigUpdate // Should this have inline as well?
	err        error
}

// rdsHandler handles any RDS queries that need to be started for a given server
// side listeners Filter Chains (i.e. not inline).
type rdsHandler struct {
	parent *listenerWrapper

	rdsMutex sync.Mutex

	routeNamesToWatch map[string]bool
	rdsUpdates        map[string]xdsclient.RouteConfigUpdate
	rdsCancels        map[string]func()

	updateChannel chan rdsHandlerUpdate
}

// newRdsHandler is expected to called once on instantiation of a wrapped
// listener. On any LDS updates the wrapped listener receives, the listener
// should update the handler with the route names (which specify dynamic RDS)
// using the function below.
func newRdsHandler(parent *listenerWrapper) *rdsHandler { // Tell Easwar routeNamesToWatch will be built in FilterChainManager in PR
	return &rdsHandler{
		parent:            parent,
		updateChannel:     make(chan rdsHandlerUpdate, 1),
		routeNamesToWatch: make(map[string]bool),
		rdsUpdates:        make(map[string]xdsclient.RouteConfigUpdate),
		rdsCancels:        make(map[string]func()),
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
		if _, inRHAlready := rh.routeNamesToWatch[routeName]; !inRHAlready {
			rh.routeNamesToWatch[routeName] = true
			rh.rdsCancels[routeName] = rh.parent.xdsC.WatchRouteConfig(routeName, rh.handleRouteUpdate)
		}
	}

	// Delete and cancel watches for any routes from persisted routeNamesToWatch
	// that are no longer present.
	for routeName := range rh.routeNamesToWatch {
		if _, stillRDS := routeNamesToWatch[routeName]; !stillRDS {
			rh.rdsCancels[routeName]()
			delete(rh.rdsCancels, routeName)
			delete(rh.routeNamesToWatch, routeName)
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
	if err != nil {
		// For a rdsHandler update, the only update wrapped listener cares about
		// is most recent one, so opportunistically drain the update before
		// sending the new update.
		select {
		case <-rh.updateChannel:
		default:
		}
		rh.updateChannel <- rdsHandlerUpdate{err: err}
		// Should we delete Route Update here? Or use successful Route Update if previously received?
	}
	rh.rdsUpdates[update.RouteConfigName] = update

	// If the full list (determined by length) of rdsUpdates have successfully updated,
	// the listener is ready to be updated.
	if len(rh.rdsUpdates) == len(rh.routeNamesToWatch) {
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

// Watch Service LDS received, starts RDS, in handle RDS is where stuff happens (i.e. logic iterates), similar to what we need
// here with Serving state.

// (ADDING ROUTE CONFIG NAME WILL BE A SEPERATE PR)..., also maybe should add (list of RDS Names to start a watch for to PR in flight rn)
