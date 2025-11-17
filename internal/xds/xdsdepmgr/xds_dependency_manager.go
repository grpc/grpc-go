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

	"google.golang.org/grpc/grpclog"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
)

const prefix = "[xdsdepmgr %p] "

var logger = grpclog.Component("xds")

func prefixLogger(p *DependencyManager) *internalgrpclog.PrefixLogger {
	return internalgrpclog.NewPrefixLogger(logger, fmt.Sprintf(prefix, p))
}

// ConfigWatcher is the interface for consumers of aggregated xDS configuration
// from the DependencyManager. The only consumer of this configuration is
// currently the xDS resolver.
type ConfigWatcher interface {
	// Update is invoked when a new, validated xDS configuration is available.
	//
	// Implementations must treat the received config as read-only and should
	// not modify it.
	Update(*xdsresource.XDSConfig)

	// Error is invoked when an error is received from the listener or route
	// resource watcher. This includes cases where:
	//  - The listener or route resource watcher reports a resource error.
	//  - The received listener resource is a socket listener, not an API
	//    listener. TODO(i/8114): Implement this check.
	//  - The received route configuration does not contain a virtual host
	//    matching the channel's default authority.
	Error(error)
}

// DependencyManager registers watches on the xDS client for all required xDS
// resources, resolves dependencies between them, and returns a complete
// configuration to the xDS resolver.
type DependencyManager struct {
	// The following fields are initialized at creation time and are read-only
	// after that.
	logger             *internalgrpclog.PrefixLogger
	watcher            ConfigWatcher
	xdsClient          xdsclient.XDSClient
	ldsResourceName    string
	dataplaneAuthority string
	nodeID             string

	// All the fields below are accessed only from the callback serializer.
	serializer       *grpcsync.CallbackSerializer
	serializerCancel context.CancelFunc

	listenerWatcher       *listenerWatcher
	currentListenerUpdate *xdsresource.ListenerUpdate
	routeConfigWatcher    *routeConfigWatcher
	rdsResourceName       string
	currentRouteConfig    *xdsresource.RouteConfigUpdate
	currentVirtualHost    *xdsresource.VirtualHost
}

// New creates a new DependencyManager.
//
//   - listenerName is the name of the Listener resource to request from the
//     management server.
//   - dataplaneAuthority is used to select the best matching virtual host from
//     the route configuration received from the management server.
//   - xdsClient is the xDS client to use to register resource watches.
//   - watcher is the ConfigWatcher interface that will receive the aggregated
//     XDSConfig updates and errors.
func New(listenerName, dataplaneAuthority string, xdsClient xdsclient.XDSClient, watcher ConfigWatcher) *DependencyManager {
	dm := &DependencyManager{
		ldsResourceName:    listenerName,
		dataplaneAuthority: dataplaneAuthority,
		xdsClient:          xdsClient,
		watcher:            watcher,
		nodeID:             xdsClient.BootstrapConfig().Node().GetId(),
	}
	dm.logger = prefixLogger(dm)
	ctx, cancel := context.WithCancel(context.Background())
	dm.serializer = grpcsync.NewCallbackSerializer(ctx)
	dm.serializerCancel = cancel

	// Start the listener watch. Listener watch will start the other resource
	// watches as needed.
	dm.listenerWatcher = newListenerWatcher(listenerName, dm)
	return dm
}

// Close cancels all registered resource watches.
func (m *DependencyManager) Close() {
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
	return fmt.Errorf("[xDS node id: %v]: %v", m.nodeID, err)
}

// maybeSendUpdateLocked checks that all the resources have been received and sends
// the current aggregated xDS configuration to the watcher if all the updates
// are available.
func (m *DependencyManager) maybeSendUpdateLocked() {
	m.watcher.Update(&xdsresource.XDSConfig{
		Listener:    m.currentListenerUpdate,
		RouteConfig: m.currentRouteConfig,
		VirtualHost: m.currentVirtualHost,
	})
}

func (m *DependencyManager) applyRouteConfigUpdateLocked(update *xdsresource.RouteConfigUpdate) {
	matchVH := xdsresource.FindBestMatchingVirtualHost(m.dataplaneAuthority, update.VirtualHosts)
	if matchVH == nil {
		m.watcher.Error(m.annotateErrorWithNodeID(fmt.Errorf("could not find VirtualHost for %q", m.dataplaneAuthority)))
		return
	}
	m.currentRouteConfig = update
	m.currentVirtualHost = matchVH
	m.maybeSendUpdateLocked()
}

func (m *DependencyManager) onListenerResourceUpdate(update *xdsresource.ListenerUpdate, onDone func()) {
	m.serializer.ScheduleOr(func(context.Context) {
		defer onDone()
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
			m.applyRouteConfigUpdateLocked(update.InlineRouteConfig)
			return
		}

		// We get here only if there was no inline route configuration. If the route
		// config name has not changed, send an update with existing route
		// configuration and the newly received listener configuration.
		if m.rdsResourceName == update.RouteConfigName {
			m.maybeSendUpdateLocked()
			return
		}

		// If the route config name has changed, cancel the old watcher and start a
		// new one. At this point, since the new route config name has not yet been
		// resolved, no update is sent to the channel, and therefore the old route
		// configuration (if received) is used until the new one is received.
		m.rdsResourceName = update.RouteConfigName
		if m.routeConfigWatcher != nil {
			m.routeConfigWatcher.stop()
			m.currentVirtualHost = nil
		}
		m.routeConfigWatcher = newRouteConfigWatcher(m.rdsResourceName, m)
	}, onDone)
}

func (m *DependencyManager) onListenerResourceError(err error, onDone func()) {
	m.serializer.ScheduleOr(func(context.Context) {
		defer onDone()
		m.logger.Warningf("Received resource error for Listener resource %q: %v", m.ldsResourceName, m.annotateErrorWithNodeID(err))

		if m.routeConfigWatcher != nil {
			m.routeConfigWatcher.stop()
		}
		m.rdsResourceName = ""
		m.currentVirtualHost = nil
		m.routeConfigWatcher = nil
		m.watcher.Error(fmt.Errorf("listener resource error: %v", m.annotateErrorWithNodeID(err)))
	}, onDone)
}

// onListenerResourceAmbientError handles ambient errors received from the
// listener resource watcher. Since ambient errors do not impact the current
// state of the resource, no change is made to the current configuration and the
// errors are only logged for visibility.
func (m *DependencyManager) onListenerResourceAmbientError(err error, onDone func()) {
	m.serializer.ScheduleOr(func(context.Context) {
		defer onDone()
		m.logger.Warningf("Listener resource ambient error: %v", m.annotateErrorWithNodeID(err))
	}, onDone)
}

func (m *DependencyManager) onRouteConfigResourceUpdate(resourceName string, update *xdsresource.RouteConfigUpdate, onDone func()) {
	m.serializer.ScheduleOr(func(context.Context) {
		defer onDone()
		if m.rdsResourceName != resourceName {
			return
		}
		if m.logger.V(2) {
			m.logger.Infof("Received update for RouteConfiguration resource %q: %+v", resourceName, update)
		}
		m.applyRouteConfigUpdateLocked(update)
	}, onDone)
}

func (m *DependencyManager) onRouteConfigResourceError(resourceName string, err error, onDone func()) {
	m.serializer.ScheduleOr(func(context.Context) {
		defer onDone()
		if m.rdsResourceName != resourceName {
			return
		}
		m.logger.Warningf("Received resource error for RouteConfiguration resource %q: %v", resourceName, m.annotateErrorWithNodeID(err))
		m.watcher.Error(fmt.Errorf("route resource error: %v", m.annotateErrorWithNodeID(err)))
	}, onDone)
}

// onRouteResourceAmbientError handles ambient errors received from the route
// resource watcher. Since ambient errors do not impact the current state of the
// resource, no change is made to the current configuration and the errors are
// only logged for visibility.
func (m *DependencyManager) onRouteConfigResourceAmbientError(resourceName string, err error, onDone func()) {
	m.serializer.ScheduleOr(func(context.Context) {
		defer onDone()
		if m.rdsResourceName != resourceName {
			return
		}
		m.logger.Warningf("Route resource ambient error %q: %v", resourceName, m.annotateErrorWithNodeID(err))
	}, onDone)
}
