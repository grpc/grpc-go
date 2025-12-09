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
	"fmt"
	"sync"

	"google.golang.org/grpc/grpclog"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/resolver"
)

const (
	aggregateClusterMaxDepth = 16
	prefix                   = "[xdsdepmgr %p] "
)

var logger = grpclog.Component("xds")

// EnableClusterAndEndpointsWatch is a flag used to control whether the CDS/EDS
// watchers in the dependency manager should be used.
var EnableClusterAndEndpointsWatch = false

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

	// All the fields below are protected by mu.
	mu                      sync.Mutex
	stopped                 bool
	listenerWatcher         *listenerWatcher
	currentListenerUpdate   *xdsresource.ListenerUpdate
	routeConfigWatcher      *routeConfigWatcher
	rdsResourceName         string
	currentRouteConfig      *xdsresource.RouteConfigUpdate
	currentVirtualHost      *xdsresource.VirtualHost
	clustersFromRouteConfig map[string]bool
	clusterWatchers         map[string]*clusterWatcherState
	endpointWatchers        map[string]*endpointWatcherState
	dnsResolvers            map[string]*dnsResolverState
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
		ldsResourceName:         listenerName,
		dataplaneAuthority:      dataplaneAuthority,
		xdsClient:               xdsClient,
		watcher:                 watcher,
		nodeID:                  xdsClient.BootstrapConfig().Node().GetId(),
		clustersFromRouteConfig: make(map[string]bool),
		endpointWatchers:        make(map[string]*endpointWatcherState),
		dnsResolvers:            make(map[string]*dnsResolverState),
		clusterWatchers:         make(map[string]*clusterWatcherState),
	}
	dm.logger = prefixLogger(dm)

	// Start the listener watch. Listener watch will start the other resource
	// watches as needed.
	dm.listenerWatcher = newListenerWatcher(listenerName, dm)
	return dm
}

// Close cancels all registered resource watches.
func (m *DependencyManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopped {
		return
	}

	m.stopped = true
	if m.listenerWatcher != nil {
		m.listenerWatcher.stop()
	}
	if m.routeConfigWatcher != nil {
		m.routeConfigWatcher.stop()
	}
	for name, cluster := range m.clusterWatchers {
		cluster.cancelWatch()
		delete(m.clusterWatchers, name)
	}

	for name, endpoint := range m.endpointWatchers {
		endpoint.cancelWatch()
		delete(m.endpointWatchers, name)
	}

	for name, dnsResolver := range m.dnsResolvers {
		dnsResolver.cancelResolver()
		delete(m.dnsResolvers, name)
	}
}

// annotateErrorWithNodeID annotates the given error with the provided xDS node
// ID.
func (m *DependencyManager) annotateErrorWithNodeID(err error) error {
	return fmt.Errorf("[xDS node id: %v]: %v", m.nodeID, err)
}

// maybeSendUpdateLocked checks that all the resources have been received and
// sends the current aggregated xDS configuration to the watcher if all the
// updates are available.
func (m *DependencyManager) maybeSendUpdateLocked() {
	if m.currentListenerUpdate == nil || m.currentRouteConfig == nil {
		return
	}
	config := &xdsresource.XDSConfig{
		Listener:    m.currentListenerUpdate,
		RouteConfig: m.currentRouteConfig,
		VirtualHost: m.currentVirtualHost,
		Clusters:    make(map[string]*xdsresource.ClusterResult),
	}
	if !EnableClusterAndEndpointsWatch {
		m.watcher.Update(config)
		return
	}
	edsResourcesSeen := make(map[string]bool)
	dnsResourcesSeen := make(map[string]bool)
	clusterResourceSeen := make(map[string]bool)
	haveAllResources := true
	for cluster := range m.clustersFromRouteConfig {
		gotAllResource, _, err := m.populateClusterConfigLocked(cluster, 0, config.Clusters, edsResourcesSeen, dnsResourcesSeen, clusterResourceSeen)
		if !gotAllResource {
			haveAllResources = false
		}
		if err != nil {
			config.Clusters[cluster] = &xdsresource.ClusterResult{
				Err: err,
			}
		}
	}

	// Cancel resources not seen in the tree.
	for name, endpoint := range m.endpointWatchers {
		if _, ok := edsResourcesSeen[name]; !ok {
			endpoint.cancelWatch()
			delete(m.endpointWatchers, name)
		}
	}
	for name, dnsResolver := range m.dnsResolvers {
		if _, ok := dnsResourcesSeen[name]; !ok {
			dnsResolver.cancelResolver()
			delete(m.dnsResolvers, name)
		}
	}
	for clusterName, cluster := range m.clusterWatchers {
		if _, ok := clusterResourceSeen[clusterName]; !ok {
			cluster.cancelWatch()
			delete(m.clusterWatchers, clusterName)
		}
	}
	if haveAllResources {
		m.watcher.Update(config)
		return
	}
	if m.logger.V(2) {
		m.logger.Infof("Not sending update to watcher as not all resources are available")
	}
}

func (m *DependencyManager) populateClusterConfigLocked(name string, depth int, clusterMap map[string]*xdsresource.ClusterResult, edsSeen, dnsSeen, clusterSeen map[string]bool) (bool, []string, error) {
	clusterSeen[name] = true

	if depth > aggregateClusterMaxDepth {
		m.logger.Warningf("aggregate cluster graph exceeds max depth (%d)", aggregateClusterMaxDepth)
		return true, nil, m.annotateErrorWithNodeID(fmt.Errorf("aggregate cluster graph exceeds max depth (%d)", aggregateClusterMaxDepth))
	}

	// If cluster is already seen in the tree, return.
	if _, ok := clusterMap[name]; ok {
		return true, nil, nil
	}

	// If cluster watcher does not exist, create one.
	if _, ok := m.clusterWatchers[name]; !ok {
		m.clusterWatchers[name] = newClusterWatcher(name, m)
		return false, nil, nil
	}

	state := m.clusterWatchers[name]

	// If update is not yet received for this cluster, return.
	if state.lastUpdate == nil && state.lastErr == nil {
		return false, nil, nil
	}

	// If resource error is seen for this cluster, save it in the cluster map
	// and return.
	if state.lastUpdate == nil && state.lastErr != nil {
		clusterMap[name] = &xdsresource.ClusterResult{Err: state.lastErr}
		return true, nil, nil
	}

	update := state.lastUpdate
	clusterMap[name] = &xdsresource.ClusterResult{
		Config: xdsresource.ClusterConfig{
			Cluster: update,
		},
	}

	switch update.ClusterType {
	case xdsresource.ClusterTypeEDS:
		var edsName string
		if update.EDSServiceName == "" {
			edsName = name
		} else {
			edsName = update.EDSServiceName
		}
		edsSeen[edsName] = true
		// If endpoint watcher does not exist, create one.
		if _, ok := m.endpointWatchers[edsName]; !ok {
			m.endpointWatchers[edsName] = newEndpointWatcher(edsName, m)
			return false, nil, nil
		}
		endpointState := m.endpointWatchers[edsName]

		// If the resource does not have any update yet, return.
		if endpointState.lastUpdate == nil && endpointState.lastErr == nil {
			return false, nil, nil
		}

		// Store the update and error.
		clusterMap[name].Config.EndpointConfig = &xdsresource.EndpointConfig{
			EDSUpdate:      endpointState.lastUpdate,
			ResolutionNote: endpointState.lastErr,
		}
		return true, []string{name}, nil

	case xdsresource.ClusterTypeLogicalDNS:
		target := update.DNSHostName
		dnsSeen[target] = true
		// If dns resolver does not exist, create one.
		if _, ok := m.dnsResolvers[target]; !ok {
			m.dnsResolvers[target] = newDNSResolver(target, m)
			return false, nil, nil
		}
		dnsState := m.dnsResolvers[target]
		// If no update received, return false.
		if !dnsState.updateReceived {
			return false, nil, nil
		}

		clusterMap[name].Config.EndpointConfig = &xdsresource.EndpointConfig{
			DNSEndpoints:   dnsState.lastUpdate,
			ResolutionNote: dnsState.err,
		}
		return true, []string{name}, nil

	case xdsresource.ClusterTypeAggregate:
		var leafClusters []string
		haveAllResources := true
		for _, child := range update.PrioritizedClusterNames {
			gotAll, childLeafCluster, err := m.populateClusterConfigLocked(child, depth+1, clusterMap, edsSeen, dnsSeen, clusterSeen)
			if !gotAll {
				haveAllResources = false
			}
			if err != nil {
				clusterMap[name] = &xdsresource.ClusterResult{
					Err: err,
				}
				return true, leafClusters, err
			}
			leafClusters = append(leafClusters, childLeafCluster...)
		}
		if !haveAllResources {
			return false, leafClusters, nil
		}
		if haveAllResources && len(leafClusters) == 0 {
			clusterMap[name] = &xdsresource.ClusterResult{Err: m.annotateErrorWithNodeID(fmt.Errorf("no leaf clusters found for aggregate cluster"))}
			return true, leafClusters, m.annotateErrorWithNodeID(fmt.Errorf("no leaf clusters found for aggregate cluster"))
		}
		clusterMap[name].Config.AggregateConfig = &xdsresource.AggregateConfig{
			LeafClusters: leafClusters,
		}
		return true, leafClusters, nil
	}
	clusterMap[name] = &xdsresource.ClusterResult{Err: m.annotateErrorWithNodeID(fmt.Errorf("no cluster data yet for cluster %s", name))}
	return false, nil, nil
}

func (m *DependencyManager) applyRouteConfigUpdateLocked(update *xdsresource.RouteConfigUpdate) {
	matchVH := xdsresource.FindBestMatchingVirtualHost(m.dataplaneAuthority, update.VirtualHosts)
	if matchVH == nil {
		m.watcher.Error(m.annotateErrorWithNodeID(fmt.Errorf("could not find VirtualHost for %q", m.dataplaneAuthority)))
		return
	}
	m.currentRouteConfig = update
	m.currentVirtualHost = matchVH

	if EnableClusterAndEndpointsWatch {
		// Get the clusters to be watched from the routes in the virtual host.
		// If the CLusterSpecifierField is set, we ignore it for now as the
		// clusters will be determined dynamically for it.
		newClusters := make(map[string]bool)

		for _, rt := range matchVH.Routes {
			for _, cluster := range rt.WeightedClusters {
				newClusters[cluster.Name] = true
			}
		}
		// Cancel watch for clusters not seen in route config
		for name := range m.clustersFromRouteConfig {
			if _, ok := newClusters[name]; !ok {
				m.clusterWatchers[name].cancelWatch()
				delete(m.clusterWatchers, name)
			}
		}

		m.clustersFromRouteConfig = newClusters
	}
	m.maybeSendUpdateLocked()
}

func (m *DependencyManager) onListenerResourceUpdate(update *xdsresource.ListenerUpdate, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped {
		return
	}

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
	}
	m.routeConfigWatcher = newRouteConfigWatcher(m.rdsResourceName, m)

}

func (m *DependencyManager) onListenerResourceError(err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped {
		return
	}

	m.logger.Warningf("Received resource error for Listener resource %q: %v", m.ldsResourceName, m.annotateErrorWithNodeID(err))

	if m.routeConfigWatcher != nil {
		m.routeConfigWatcher.stop()
	}
	m.currentListenerUpdate = nil
	m.rdsResourceName = ""
	m.routeConfigWatcher = nil
	m.watcher.Error(fmt.Errorf("listener resource error: %v", m.annotateErrorWithNodeID(err)))
}

// onListenerResourceAmbientError handles ambient errors received from the
// listener resource watcher. Since ambient errors do not impact the current
// state of the resource, no change is made to the current configuration and the
// errors are only logged for visibility.
func (m *DependencyManager) onListenerResourceAmbientError(err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped {
		return
	}

	m.logger.Warningf("Listener resource ambient error: %v", m.annotateErrorWithNodeID(err))
}

func (m *DependencyManager) onRouteConfigResourceUpdate(resourceName string, update *xdsresource.RouteConfigUpdate, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.rdsResourceName != resourceName {
		return
	}

	if m.logger.V(2) {
		m.logger.Infof("Received update for RouteConfiguration resource %q: %+v", resourceName, update)
	}
	m.applyRouteConfigUpdateLocked(update)
}

func (m *DependencyManager) onRouteConfigResourceError(resourceName string, err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.rdsResourceName != resourceName {
		return
	}
	m.currentRouteConfig = nil
	m.logger.Warningf("Received resource error for RouteConfiguration resource %q: %v", resourceName, m.annotateErrorWithNodeID(err))
	m.watcher.Error(fmt.Errorf("route resource error: %v", m.annotateErrorWithNodeID(err)))
}

// onRouteResourceAmbientError handles ambient errors received from the route
// resource watcher. Since ambient errors do not impact the current state of the
// resource, no change is made to the current configuration and the errors are
// only logged for visibility.
func (m *DependencyManager) onRouteConfigResourceAmbientError(resourceName string, err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.rdsResourceName != resourceName {
		return
	}

	m.logger.Warningf("Route resource ambient error %q: %v", resourceName, m.annotateErrorWithNodeID(err))
}

type clusterWatcherState struct {
	cancelWatch func()
	lastUpdate  *xdsresource.ClusterUpdate
	lastErr     error
}

func newClusterWatcher(resourceName string, depMgr *DependencyManager) *clusterWatcherState {
	w := &clusterWatcher{resourceName: resourceName, depMgr: depMgr}
	return &clusterWatcherState{
		cancelWatch: xdsresource.WatchCluster(depMgr.xdsClient, resourceName, w),
	}
}

// Records a successful Cluster resource update, clears any previous error.
func (m *DependencyManager) onClusterResourceUpdate(resourceName string, update *xdsresource.ClusterUpdate, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.clusterWatchers[resourceName] == nil {
		return
	}

	if m.logger.V(2) {
		m.logger.Infof("Received update for Cluster resource %q: %+v", resourceName, update)
	}
	state := m.clusterWatchers[resourceName]
	state.lastUpdate = update
	state.lastErr = nil
	m.maybeSendUpdateLocked()

}

// Records a resource error for a Cluster resource, clears the last successful
// update since we want to stop using the resource if we get a resource error.
func (m *DependencyManager) onClusterResourceError(resourceName string, err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.clusterWatchers[resourceName] == nil {
		return
	}
	m.logger.Warningf("Received resource error for Cluster resource %q: %v", resourceName, m.annotateErrorWithNodeID(err))
	state := m.clusterWatchers[resourceName]
	state.lastErr = err
	state.lastUpdate = nil
	m.maybeSendUpdateLocked()
}

// Records the error in the state. The last successful update is retained
// because it should continue to be used as an amnbient error is received.
func (m *DependencyManager) onClusterAmbientError(resourceName string, err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.clusterWatchers[resourceName] == nil {
		return
	}
	m.logger.Warningf("Cluster resource ambient error %q: %v", resourceName, m.annotateErrorWithNodeID(err))
}

type endpointWatcherState struct {
	cancelWatch func()
	lastUpdate  *xdsresource.EndpointsUpdate
	lastErr     error
}

func newEndpointWatcher(resourceName string, depMgr *DependencyManager) *endpointWatcherState {
	w := &endpointsWatcher{resourceName: resourceName, depMgr: depMgr}
	return &endpointWatcherState{
		cancelWatch: xdsresource.WatchEndpoints(depMgr.xdsClient, resourceName, w),
	}
}

// Records a successful Endpoint resource update, clears any previous error from
// the state.
func (m *DependencyManager) onEndpointUpdate(resourceName string, update *xdsresource.EndpointsUpdate, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.endpointWatchers[resourceName] == nil {
		return
	}

	if m.logger.V(2) {
		m.logger.Infof("Received update for Endpoint resource %q: %+v", resourceName, update)
	}
	state := m.endpointWatchers[resourceName]
	state.lastUpdate = update
	state.lastErr = nil
	m.maybeSendUpdateLocked()
}

// Records a resource error and clears the last successful update since the
// endpoints should not be used after getting a resource error.
func (m *DependencyManager) onEndpointResourceError(resourceName string, err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.endpointWatchers[resourceName] == nil {
		return
	}
	m.logger.Warningf("Received resource error for Endpoint resource %q: %v", resourceName, m.annotateErrorWithNodeID(err))
	state := m.endpointWatchers[resourceName]
	state.lastErr = err
	state.lastUpdate = nil
	m.maybeSendUpdateLocked()
}

// Records the ambient error without clearing the last successful update, as the
// endpoints should continue to be used.
func (m *DependencyManager) onEndpointAmbientError(resourceName string, err error, onDone func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer onDone()
	if m.stopped || m.endpointWatchers[resourceName] == nil {
		return
	}

	m.logger.Warningf("Endpoint resource ambient error %q: %v", resourceName, m.annotateErrorWithNodeID(err))
	state := m.endpointWatchers[resourceName]
	state.lastErr = err
	m.maybeSendUpdateLocked()
}

// Converts the DNS resolver state to an internal update, handling address-only
// updates by wrapping them into endpoints. It records the update and clears any
// previous error.
func (m *DependencyManager) onDNSUpdate(resourceName string, update *resolver.State) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped || m.dnsResolvers[resourceName] == nil {
		return
	}

	if m.logger.V(2) {
		m.logger.Infof("Received update from DNS resolver for resource %q: %+v", resourceName, update)
	}
	var endpoints []resolver.Endpoint
	if len(update.Endpoints) == 0 {
		endpoints = make([]resolver.Endpoint, len(update.Addresses))
		for i, a := range update.Addresses {
			endpoints[i] = resolver.Endpoint{Addresses: []resolver.Address{a}}
			endpoints[i].Attributes = a.BalancerAttributes
		}
	}

	state := m.dnsResolvers[resourceName]
	state.lastUpdate = &xdsresource.DNSUpdate{Endpoints: endpoints}
	state.updateReceived = true
	state.err = nil
	m.maybeSendUpdateLocked()
}

// Records a DNS resolver error. It clears the last update only if no successful
// update has been received yet, then triggers a dependency update.
//
// If a previous good update was received, the error is recorded but the
// previous update is retained for continued use. Errors are suppressed if a
// resource error was already received, as further propagation would have no
// downstream effect.
func (m *DependencyManager) onDNSError(resourceName string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopped || m.dnsResolvers[resourceName] == nil {
		return
	}

	state := m.dnsResolvers[resourceName]
	// If a previous good update was received, record the error and continue
	// using the previous update. If RPCs were succeeding prior to this, they
	// will continue to do so. Also suppress errors if we previously received an
	// error, since there will be no downstream effects of propagating this
	// error.
	state.err = err
	if !state.updateReceived {
		state.lastUpdate = nil
		state.updateReceived = true
	}
	m.logger.Warningf("DNS resolver error %q: %v", resourceName, m.annotateErrorWithNodeID(err))
	m.maybeSendUpdateLocked()
}
