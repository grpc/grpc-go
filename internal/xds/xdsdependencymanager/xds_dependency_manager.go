/*
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
 */

// Package xdsdepmgr contains functions, structs, and utilities for working with
// xds dependency manager. It will handle all the xds resources like listener,
// route, cluster and endpoint resources and their dependencies.
package xdsdepmgr

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/internal/status"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
)

// DependencyManager struct stores all the information needed for watching all
// the xds resources.
type DependencyManager struct {
	xdsClient xdsclient.XDSClient

	watcher ConfigWatcher
	// All methods on the xdsResolver type except for the ones invoked by gRPC,
	// i.e ResolveNow() and Close(), are guaranteed to execute in the context of
	// this serializer's callback. And since the serializer guarantees mutual
	// exclusion among these callbacks, we can get by without any mutexes to
	// access all of the below defined state. The only exception is Close(),
	// which does access some of this shared state, but it does so after
	// cancelling the context passed to the serializer.
	serializer       *grpcsync.CallbackSerializer
	serializerCancel context.CancelFunc

	// dataplaneAuthority is the authority used for the data plane connections,
	// which is also used to select the VirtualHost within the xDS
	// RouteConfiguration.  This is %-encoded to match with VirtualHost Domain
	// in xDS RouteConfiguration.
	dataplaneAuthority string

	ldsResourceName       string
	listenerWatcher       *listenerWatcher
	currentListenerUpdate xdsresource.ListenerUpdate

	rdsResourceName    string
	currentRouteConfig xdsresource.RouteConfigUpdate
	routeConfigWatcher *routeConfigWatcher
	currentVirtualHost *xdsresource.VirtualHost

	logger *grpclog.PrefixLogger
}

// New creates a new dependency manager that manages all the xds resources
// including LDS, RDS, CDS and EDS and sends update once we have all the
// resources and sends an error when we get error in listener or route
// resources.
func New(listenername, dataplaneAuthority string, xdsClient xdsclient.XDSClient, watcher ConfigWatcher) *DependencyManager {
	// Builds the dependency manager and starts the listener watch.
	dm := &DependencyManager{
		ldsResourceName:    listenername,
		dataplaneAuthority: dataplaneAuthority,
		xdsClient:          xdsClient,
		watcher:            watcher,
	}
	dm.logger = prefixLogger(dm)

	// Initialize the serializer used to synchronize the updates from the xDS
	// client.
	ctx, cancel := context.WithCancel(context.Background())
	dm.serializer = grpcsync.NewCallbackSerializer(ctx)
	dm.serializerCancel = cancel

	// Start the listener watch. Listener watch will start the other resource
	// watches as needed.
	dm.listenerWatcher = newListenerWatcher(listenername, dm)
	return dm
}

// Close closes the dependency manager and stops all the watchers registered.
func (m *DependencyManager) Close() {
	// Cancel the context passed to the serializer and wait for any scheduled
	// callbacks to complete. Canceling the context ensures that no new
	// callbacks will be scheduled.
	m.serializerCancel()
	<-m.serializer.Done()

	if m.listenerWatcher != nil {
		m.listenerWatcher.stop()
	}
	if m.routeConfigWatcher != nil {
		m.routeConfigWatcher.stop()
	}
}

// ConfigWatcher is notified of the XDSConfig resource updates and errors that
// are received by the xDS client from the management server. It only receives a
// XDSConfig update after all the xds resources have been received.
type ConfigWatcher interface {
	// OnUpdate is invoked by the dependency manager to provide a new,
	// validated xDS configuration to the watcher.
	OnUpdate(xdsresource.XDSConfig)

	// OnError is invoked when an error is received in listener or route
	// resource. This includes cases where:
	//  - The listener or route resource watcher reports a resource error.
	//  - The received listener resource is a socket listener, not an API listener - TODO :  This is not yet implemented, tracked here #8114
	//  - The received route configuration does not contain a virtual host
	//  matching the channel's default authority.
	OnError(error)
}

func (m *DependencyManager) maybeSendUpdate() {
	if m.logger.V(2) {
		m.logger.Infof("Sending update to watcher: Listener: %v, RouteConfig: %v", pretty.ToJSON(m.currentListenerUpdate), pretty.ToJSON(m.currentRouteConfig))
	}
	m.watcher.OnUpdate(xdsresource.XDSConfig{
		Listener:    m.currentListenerUpdate,
		RouteConfig: m.currentRouteConfig,
		VirtualHost: m.currentVirtualHost,
	})
}

func (m *DependencyManager) applyRouteConfigUpdate(update xdsresource.RouteConfigUpdate) {
	matchVh := xdsresource.FindBestMatchingVirtualHost(m.dataplaneAuthority, update.VirtualHosts)
	if matchVh == nil {
		m.watcher.OnError(status.Errorf(codes.Unavailable, "Could not find VirtualHost for %q", m.dataplaneAuthority))
		return
	}
	m.currentRouteConfig = update
	m.currentVirtualHost = matchVh
	m.maybeSendUpdate()
}

// Only executed in the context of a serializer callback.
func (m *DependencyManager) onListenerResourceUpdate(update xdsresource.ListenerUpdate) {
	if m.logger.V(2) {
		m.logger.Infof("Received update for Listener resource %q: %v", m.ldsResourceName, pretty.ToJSON(update))
	}

	m.currentListenerUpdate = update

	if update.InlineRouteConfig != nil {
		// If there was a previous route config watcher because of a non-inline
		// route configuration, cancel it.
		m.rdsResourceName = ""
		if m.routeConfigWatcher != nil {
			m.routeConfigWatcher.stop()
			m.routeConfigWatcher = nil
		}

		m.applyRouteConfigUpdate(*update.InlineRouteConfig)
		return
	}

	// We get here only if there was no inline route configuration.

	// If the route config name has not changed, send an update with existing
	// route configuration and the newly received listener configuration.
	if m.rdsResourceName == update.RouteConfigName {
		m.maybeSendUpdate()
		return
	}

	// If the route config name has changed, cancel the old watcher and start a
	// new one. At this point, since we have not yet resolved the new route
	// config name, we don't send an update to the channel, and therefore
	// continue using the old route configuration (if received) until the new
	// one is received.
	m.rdsResourceName = update.RouteConfigName
	if m.routeConfigWatcher != nil {
		m.routeConfigWatcher.stop()
		m.currentVirtualHost = nil
	}
	m.routeConfigWatcher = newRouteConfigWatcher(m.rdsResourceName, m)
}

// Only executed in the context of a serializer callback.
func (m *DependencyManager) onListenerResourceError(err error) {
	if m.logger.V(2) {
		m.logger.Infof("Received resource error for Listener resource %q: %v", m.ldsResourceName, err)
	}

	if m.routeConfigWatcher != nil {
		m.routeConfigWatcher.stop()
	}
	m.rdsResourceName = ""
	m.currentVirtualHost = nil
	m.routeConfigWatcher = nil
	m.watcher.OnError(status.Errorf(codes.Unavailable, "Listener resource error : %v", err))
}

// Only executed in the context of a serializer callback.
func (m *DependencyManager) onListenerResourceAmbientError(err error) {
	m.logger.Warningf("Listener resource ambient error: %v", err)
}

// Only executed in the context of a serializer callback.
func (m *DependencyManager) onRouteConfigResourceUpdate(resourceName string, update xdsresource.RouteConfigUpdate) {
	if m.logger.V(2) {
		m.logger.Infof("Received update for RouteConfiguration resource %q: %v", resourceName, pretty.ToJSON(update))
	}

	if m.rdsResourceName != resourceName {
		// Drop updates from canceled watchers.
		return
	}
	m.applyRouteConfigUpdate(update)
}

// Only executed in the context of a serializer callback.
func (m *DependencyManager) onRouteConfigResourceError(resourceName string, err error) {
	if m.logger.V(2) {
		m.logger.Infof("Received resource error for RouteConfiguration resource %q: %v", resourceName, err)
	}

	//If update is not for the current watcher
	if m.rdsResourceName != resourceName {
		return
	}
	m.watcher.OnError(status.Errorf(codes.Unavailable, "Route resource error : %v", err))
}

// Only executed in the context of a serializer callback.
func (m *DependencyManager) onRouteConfigResourceAmbientError(resourceName string, err error) {
	m.logger.Warningf("Route resource ambient error %q: %v", resourceName, err)
}
