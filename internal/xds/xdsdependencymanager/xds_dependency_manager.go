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

// Package xdsdepmgr provides the implementation of the xDS dependency manager
// that manages all the xDS watches and resources as described in gRFC A74.
package xdsdepmgr

import (
	"context"
	"fmt"

	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
)

// ConfigWatcher is the interface for consumers of aggregated xDS configuration
// from the DependencyManager. The only consumer of this configuration is
// currently the xDS resolver.
type ConfigWatcher interface {
	// OnUpdate is invoked by the dependency manager when a new, validated xDS
	// configuration is available.
	//
	// Implementations must treat the received config as read-only and should
	// not modify it.
	OnUpdate(*xdsresource.XDSConfig)

	// OnError is invoked when an error is received in listener or route
	// resource. This includes cases where:
	//  - The listener or route resource watcher reports a resource error.
	//  - The received listener resource is a socket listener, not an API listener - TODO :  This is not yet implemented, tracked here #8114
	//  - The received route configuration does not contain a virtual host
	//  matching the channel's default authority.
	OnError(error)
}

// DependencyManager registers watches on the xDS client for all required xDS
// resources, resolves dependencies between them, and returns a complete
// configuration to the xDS resolver.
type DependencyManager struct {
	xdsClient xdsclient.XDSClient

	watcher ConfigWatcher

	serializer       *grpcsync.CallbackSerializer
	serializerCancel context.CancelFunc

	logger *grpclog.PrefixLogger

	// All the fields below are accessed only from within the context of
	// serialized callbacks.

	// dataplaneAuthority is the authority used for the data plane connections,
	// which is also used to select the VirtualHost within the xDS
	// RouteConfiguration.  This is %-encoded to match with VirtualHost Domain
	// in xDS RouteConfiguration.
	dataplaneAuthority string

	ldsResourceName       string
	listenerWatcher       *listenerWatcher
	currentListenerUpdate *xdsresource.ListenerUpdate

	rdsResourceName    string
	currentRouteConfig *xdsresource.RouteConfigUpdate
	routeConfigWatcher *routeConfigWatcher
	currentVirtualHost *xdsresource.VirtualHost
}

// New creates a new DependencyManager.
//
//   - listenerName is the name of the Listener resource to request from the
//     management server.
//   - dataplaneAuthority is used to select the best matching virtual host from
//     the route configuration received from the management server.
//   - xdsClient is the xDS client to use to register resource watches.
//   - watcher is the ConfigWatcher interface that will receive the aggregated XDSConfig.
//
// Configuration updates and/or errors are delivered to the watcher.
func New(listenerName, dataplaneAuthority string, xdsClient xdsclient.XDSClient, watcher ConfigWatcher) *DependencyManager {
	dm := &DependencyManager{
		ldsResourceName:    listenerName,
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
	dm.listenerWatcher = newListenerWatcher(listenerName, dm)
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

// annotateErrorWithNodeID annotates the given error with the provided xDS node
// ID.
func (m *DependencyManager) annotateErrorWithNodeID(err error) error {
	nodeID := m.xdsClient.BootstrapConfig().Node().GetId()
	return fmt.Errorf("[xDS node id: %v]: %w", nodeID, err)
}

// maybeSendUpdate checks that all the resources have been received and sends
// the current aggregated xDS configuration to the watcher if all the  updates
// are available.
func (m *DependencyManager) maybeSendUpdate() {
	m.watcher.OnUpdate(&xdsresource.XDSConfig{
		Listener:    m.currentListenerUpdate,
		RouteConfig: m.currentRouteConfig,
		VirtualHost: m.currentVirtualHost,
	})
}

func (m *DependencyManager) applyRouteConfigUpdate(update *xdsresource.RouteConfigUpdate) {
	matchVH := xdsresource.FindBestMatchingVirtualHost(m.dataplaneAuthority, update.VirtualHosts)
	if matchVH == nil {
		m.watcher.OnError(fmt.Errorf("could not find VirtualHost for %q", m.dataplaneAuthority))
		return
	}
	m.currentRouteConfig = update
	m.currentVirtualHost = matchVH
	m.maybeSendUpdate()
}

// Only executed in the context of a serializer callback.
func (m *DependencyManager) onListenerResourceUpdate(update *xdsresource.ListenerUpdate) {
	if m.logger.V(2) {
		m.logger.Infof("Received update for Listener resource %q: %+v", m.ldsResourceName, update)
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
		m.applyRouteConfigUpdate(update.InlineRouteConfig)
		return
	}

	// We get here only if there was no inline route configuration. If the route
	// config name has not changed, send an update with existing route
	// configuration and the newly received listener configuration.
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
		m.logger.Infof("Received resource error for Listener resource %q: %v", m.ldsResourceName, m.annotateErrorWithNodeID(err))
	}

	if m.routeConfigWatcher != nil {
		m.routeConfigWatcher.stop()
	}
	m.rdsResourceName = ""
	m.currentVirtualHost = nil
	m.routeConfigWatcher = nil
	m.watcher.OnError(fmt.Errorf("listener resource error : %v", err))
}

// Only executed in the context of a serializer callback.
func (m *DependencyManager) onListenerResourceAmbientError(err error) {
	m.logger.Warningf("Listener resource ambient error: %v", m.annotateErrorWithNodeID(err))
}

// Only executed in the context of a serializer callback.
func (m *DependencyManager) onRouteConfigResourceUpdate(resourceName string, update *xdsresource.RouteConfigUpdate) {
	if m.logger.V(2) {
		m.logger.Infof("Received update for RouteConfiguration resource %q: %+v", resourceName, update)
	}
	m.applyRouteConfigUpdate(update)
}

// Only executed in the context of a serializer callback.
func (m *DependencyManager) onRouteConfigResourceError(resourceName string, err error) {
	if m.logger.V(2) {
		m.logger.Infof("Received resource error for RouteConfiguration resource %q: %v", resourceName, m.annotateErrorWithNodeID(err))
	}
	m.watcher.OnError(fmt.Errorf("route resource error : %v", err))
}

// Only executed in the context of a serializer callback.
func (m *DependencyManager) onRouteConfigResourceAmbientError(resourceName string, err error) {
	m.logger.Warningf("Route resource ambient error %q: %v", resourceName, m.annotateErrorWithNodeID(err))
}
