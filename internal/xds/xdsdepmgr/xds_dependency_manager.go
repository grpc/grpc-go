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
	"net/url"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/grpclog"
	internalgrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
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

// xdsResourceState is a generic struct to hold the state of a watched xDS
// resource.
type xdsResourceState[T any, U any] struct {
	lastUpdate     *T
	lastErr        error
	updateReceived bool
	stop           func()
	extras         U // to store any additional state specific to the watcher
}

func (x *xdsResourceState[T, U]) setLastUpdate(update *T) {
	x.lastUpdate = update
	x.updateReceived = true
	x.lastErr = nil
}

func (x *xdsResourceState[T, U]) setLastError(err error) {
	x.lastErr = err
	x.updateReceived = true
	x.lastUpdate = nil
}

func (x *xdsResourceState[T, U]) updateLastError(err error) {
	x.lastErr = err
}

type dnsExtras struct {
	dnsR resolver.Resolver
}

type routeExtras struct {
	virtualHost *xdsresource.VirtualHost
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

	// Used to serialize callbacks from DNS resolvers to avoid deadlocks. Since
	// the resolver's Build() is called with the dependency manager lock held,
	// direct callbacks to ClientConn (which also require that lock) would
	// deadlock.
	dnsSerializer       *grpcsync.CallbackSerializer
	dnsSerializerCancel func()

	// All the fields below are protected by mu.
	mu                      sync.Mutex
	stopped                 bool
	listenerWatcher         *xdsResourceState[xdsresource.ListenerUpdate, struct{}]
	rdsResourceName         string
	routeConfigWatcher      *xdsResourceState[xdsresource.RouteConfigUpdate, routeExtras]
	clustersFromRouteConfig map[string]bool
	clusterWatchers         map[string]*xdsResourceState[xdsresource.ClusterUpdate, struct{}]
	endpointWatchers        map[string]*xdsResourceState[xdsresource.EndpointsUpdate, struct{}]
	dnsResolvers            map[string]*xdsResourceState[xdsresource.DNSUpdate, dnsExtras]
	clusterSubscriptions    map[string]*ClusterRef
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
	ctx, cancel := context.WithCancel(context.Background())
	dm := &DependencyManager{
		ldsResourceName:         listenerName,
		dataplaneAuthority:      dataplaneAuthority,
		xdsClient:               xdsClient,
		watcher:                 watcher,
		nodeID:                  xdsClient.BootstrapConfig().Node().GetId(),
		dnsSerializer:           grpcsync.NewCallbackSerializer(ctx),
		dnsSerializerCancel:     cancel,
		clustersFromRouteConfig: make(map[string]bool),
		endpointWatchers:        make(map[string]*xdsResourceState[xdsresource.EndpointsUpdate, struct{}]),
		dnsResolvers:            make(map[string]*xdsResourceState[xdsresource.DNSUpdate, dnsExtras]),
		clusterWatchers:         make(map[string]*xdsResourceState[xdsresource.ClusterUpdate, struct{}]),
		clusterSubscriptions:    make(map[string]*ClusterRef),
	}
	dm.logger = prefixLogger(dm)

	// Start the listener watch. Listener watch will start the other resource
	// watches as needed.
	dm.listenerWatcher = &xdsResourceState[xdsresource.ListenerUpdate, struct{}]{}
	lw := &xdsResourceWatcher[xdsresource.ListenerUpdate]{
		onUpdate: func(update *xdsresource.ListenerUpdate, onDone func()) {
			dm.onListenerResourceUpdate(update, onDone)
		},
		onError: func(err error, onDone func()) {
			dm.onListenerResourceError(err, onDone)
		},
		onAmbientError: func(err error, onDone func()) {
			dm.onListenerResourceAmbientError(err, onDone)
		},
	}
	dm.listenerWatcher.stop = xdsresource.WatchListener(dm.xdsClient, listenerName, lw)
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
	m.listenerWatcher.stop()
	if m.routeConfigWatcher != nil {
		m.routeConfigWatcher.stop()
	}
	for name, cluster := range m.clusterWatchers {
		cluster.stop()
		delete(m.clusterWatchers, name)
	}

	for name, endpoint := range m.endpointWatchers {
		endpoint.stop()
		delete(m.endpointWatchers, name)
	}

	// We cannot wait for the dns serializer to finish here, as the callbacks
	// try to grab the dependency manager lock, which is already held here.
	m.dnsSerializerCancel()
	for name, dnsResolver := range m.dnsResolvers {
		if dnsResolver.extras.dnsR != nil {
			dnsResolver.stop()
		}
		delete(m.dnsResolvers, name)
	}
}

// annotateErrorWithNodeID annotates the given error with the provided xDS node
// ID.
func (m *DependencyManager) annotateErrorWithNodeID(err error) error {
	return fmt.Errorf("[xDS node id: %v]: %v", m.nodeID, err)
}

// maybeSendUpdateLocked verifies that all expected resources have been
// received, and if so, delivers the complete xDS configuration to the watcher.
func (m *DependencyManager) maybeSendUpdateLocked() {
	if m.listenerWatcher.lastUpdate == nil || m.routeConfigWatcher == nil || m.routeConfigWatcher.lastUpdate == nil {
		return
	}
	config := &xdsresource.XDSConfig{
		Listener:    m.listenerWatcher.lastUpdate,
		RouteConfig: m.routeConfigWatcher.lastUpdate,
		VirtualHost: m.routeConfigWatcher.extras.virtualHost,
		Clusters:    make(map[string]*xdsresource.ClusterResult),
	}

	edsResourcesSeen := make(map[string]bool)
	dnsResourcesSeen := make(map[string]bool)
	clusterResourcesSeen := make(map[string]bool)
	haveAllResources := true
	// Get all clusters to be watched: from route config and from cluster
	// subscriptions, the subscriptions can be from RPC referencing the cluster
	// or from balancer for cluster specifier plugins.
	clusterToWatch := make(map[string]bool)
	for cluster := range m.clustersFromRouteConfig {
		clusterToWatch[cluster] = true
	}
	for cluster := range m.clusterSubscriptions {
		clusterToWatch[cluster] = true
	}
	for cluster := range clusterToWatch {
		ok, leafClusters, err := m.populateClusterConfigLocked(cluster, 0, config.Clusters, edsResourcesSeen, dnsResourcesSeen, clusterResourcesSeen)
		if !ok {
			haveAllResources = false
		}
		// If there are no leaf clusters, add that as error.
		if ok && len(leafClusters) == 0 {
			config.Clusters[cluster] = &xdsresource.ClusterResult{Err: m.annotateErrorWithNodeID(fmt.Errorf("aggregate cluster graph has no leaf clusters"))}
		}
		if err != nil {
			config.Clusters[cluster] = &xdsresource.ClusterResult{Err: err}
		}
	}

	// Cancel resources not seen in the tree.
	for name, ep := range m.endpointWatchers {
		if _, ok := edsResourcesSeen[name]; !ok {
			ep.stop()
			delete(m.endpointWatchers, name)
		}
	}
	for name, dr := range m.dnsResolvers {
		if _, ok := dnsResourcesSeen[name]; !ok {
			dr.stop()
			delete(m.dnsResolvers, name)
		}
	}
	for name, cluster := range m.clusterWatchers {
		if _, ok := clusterResourcesSeen[name]; !ok {
			cluster.stop()
			delete(m.clusterWatchers, name)
		}
	}
	if haveAllResources {
		m.watcher.Update(config)
	}
}

// populateClusterConfigLocked resolves and populates the
// configuration for the given cluster and its children, including its
// associated endpoint or aggregate children. For aggregate clusters, it
// recursively resolves the configuration for its child clusters.
//
// This function traverses the cluster dependency graph (e.g., from an Aggregate
// cluster down to its leaf clusters and their endpoints/DNS resources) to
// ensure all necessary xDS resources are watched and fully resolved before
// configuration is considered ready.
//
// Parameters:
//
//	clusterName:          The name of the cluster resource to resolve.
//	depth:                The current recursion depth.
//	clusterConfigs:       Map to store the resolved cluster configuration.
//	endpointResourcesSeen: Stores which EDS resource names have been encountered.
//	dnsResourcesSeen:      Stores which DNS resource names have been encountered.
//	clustersSeen:         Stores which cluster resource names have been encountered.
//
// Returns:
//
//	bool:                 Returns true if the cluster configuration (and all its
//	                      dependencies) is fully resolved (i.e either update or
//	                      error has been received).
//	[]string:             A slice of all "leaf" cluster names discovered in the
//	                      traversal starting from `clusterName`. For
//	                      non-aggregate clusters, this will contain only `clusterName`.
//	error:                Error that needs to be propogated up the tree (like
//	                      max depth exceeded or an error propagated from a
//	                      child cluster).
func (m *DependencyManager) populateClusterConfigLocked(clusterName string, depth int, clusterConfigs map[string]*xdsresource.ClusterResult, endpointResourcesSeen, dnsResourcesSeen, clustersSeen map[string]bool) (bool, []string, error) {
	const aggregateClusterMaxDepth = 16
	clustersSeen[clusterName] = true

	if depth >= aggregateClusterMaxDepth {
		err := m.annotateErrorWithNodeID(fmt.Errorf("aggregate cluster graph exceeds max depth (%d)", aggregateClusterMaxDepth))
		clusterConfigs[clusterName] = &xdsresource.ClusterResult{Err: err}
		return true, nil, err
	}

	// If cluster is already seen in the tree, return.
	if _, ok := clusterConfigs[clusterName]; ok {
		return true, nil, nil
	}

	// If cluster watcher does not exist, create one.
	state, ok := m.clusterWatchers[clusterName]
	if !ok {
		m.clusterWatchers[clusterName] = newClusterWatcher(clusterName, m)
		return false, nil, nil
	}

	// If a watch exists but no update received yet, return.
	if !state.updateReceived {
		return false, nil, nil
	}

	// If there was a resource error, propagate it up.
	if state.lastErr != nil {
		return true, nil, state.lastErr
	}

	clusterConfigs[clusterName] = &xdsresource.ClusterResult{
		Config: xdsresource.ClusterConfig{
			Cluster: state.lastUpdate,
		},
	}
	update := state.lastUpdate

	switch update.ClusterType {
	case xdsresource.ClusterTypeEDS:
		return m.populateEDSClusterLocked(clusterName, update, clusterConfigs, endpointResourcesSeen)
	case xdsresource.ClusterTypeLogicalDNS:
		return m.populateLogicalDNSClusterLocked(clusterName, update, clusterConfigs, dnsResourcesSeen)
	case xdsresource.ClusterTypeAggregate:
		return m.populateAggregateClusterLocked(clusterName, update, depth, clusterConfigs, endpointResourcesSeen, dnsResourcesSeen, clustersSeen)
	default:
		clusterConfigs[clusterName] = &xdsresource.ClusterResult{Err: m.annotateErrorWithNodeID(fmt.Errorf("cluster type %v of cluster %s not supported", update.ClusterType, clusterName))}
		return true, nil, nil
	}
}

func (m *DependencyManager) populateEDSClusterLocked(clusterName string, update *xdsresource.ClusterUpdate, clusterConfigs map[string]*xdsresource.ClusterResult, endpointResourcesSeen map[string]bool) (bool, []string, error) {
	edsName := clusterName
	if update.EDSServiceName != "" {
		edsName = update.EDSServiceName
	}
	endpointResourcesSeen[edsName] = true

	// If endpoint watcher does not exist, create one.
	if _, ok := m.endpointWatchers[edsName]; !ok {
		m.endpointWatchers[edsName] = newEndpointWatcher(edsName, m)
		return false, nil, nil
	}
	endpointState := m.endpointWatchers[edsName]

	// If the resource does not have any update yet, return.
	if !endpointState.updateReceived {
		return false, nil, nil
	}

	// Store the update and error.
	clusterConfigs[clusterName].Config.EndpointConfig = &xdsresource.EndpointConfig{
		EDSUpdate:      endpointState.lastUpdate,
		ResolutionNote: endpointState.lastErr,
	}
	return true, []string{clusterName}, nil
}

func (m *DependencyManager) populateLogicalDNSClusterLocked(clusterName string, update *xdsresource.ClusterUpdate, clusterConfigs map[string]*xdsresource.ClusterResult, dnsResourcesSeen map[string]bool) (bool, []string, error) {
	target := update.DNSHostName
	dnsResourcesSeen[target] = true

	// If dns resolver does not exist, create one.
	if _, ok := m.dnsResolvers[target]; !ok {
		state := m.newDNSResolver(target)
		if state == nil {
			return false, nil, nil
		}
		m.dnsResolvers[target] = state
		return false, nil, nil
	}
	dnsState := m.dnsResolvers[target]

	// If no update received, return false.
	if !dnsState.updateReceived {
		return false, nil, nil
	}

	clusterConfigs[clusterName].Config.EndpointConfig = &xdsresource.EndpointConfig{
		DNSEndpoints:   dnsState.lastUpdate,
		ResolutionNote: dnsState.lastErr,
	}
	return true, []string{clusterName}, nil
}

func (m *DependencyManager) populateAggregateClusterLocked(clusterName string, update *xdsresource.ClusterUpdate, depth int, clusterConfigs map[string]*xdsresource.ClusterResult, endpointResourcesSeen, dnsResourcesSeen, clustersSeen map[string]bool) (bool, []string, error) {
	var leafClusters []string
	haveAllResources := true
	for _, child := range update.PrioritizedClusterNames {
		ok, childLeafClusters, err := m.populateClusterConfigLocked(child, depth+1, clusterConfigs, endpointResourcesSeen, dnsResourcesSeen, clustersSeen)
		if !ok {
			haveAllResources = false
		}
		if err != nil {
			clusterConfigs[clusterName] = &xdsresource.ClusterResult{Err: err}
			return true, leafClusters, err
		}
		leafClusters = append(leafClusters, childLeafClusters...)
	}
	if !haveAllResources {
		return false, leafClusters, nil
	}
	if haveAllResources && len(leafClusters) == 0 {
		clusterConfigs[clusterName] = &xdsresource.ClusterResult{Err: m.annotateErrorWithNodeID(fmt.Errorf("aggregate cluster graph has no leaf clusters"))}
		return true, leafClusters, nil
	}
	clusterConfigs[clusterName].Config.AggregateConfig = &xdsresource.AggregateConfig{
		LeafClusters: leafClusters,
	}
	return true, leafClusters, nil
}

func (m *DependencyManager) applyRouteConfigUpdateLocked(update *xdsresource.RouteConfigUpdate) {
	matchVH := xdsresource.FindBestMatchingVirtualHost(m.dataplaneAuthority, update.VirtualHosts)
	if matchVH == nil {
		err := m.annotateErrorWithNodeID(fmt.Errorf("could not find VirtualHost for %q", m.dataplaneAuthority))
		m.routeConfigWatcher.setLastError(err)
		m.watcher.Error(err)
		return
	}
	m.routeConfigWatcher.setLastUpdate(update)
	m.routeConfigWatcher.extras.virtualHost = matchVH

	// Get the clusters to be watched from the routes in the virtual host.
	// If the ClusterSpecifierField is set, we ignore it for now as the
	// clusters will be determined dynamically for it.
	newClusters := make(map[string]bool)

	for _, rt := range matchVH.Routes {
		for _, cluster := range rt.WeightedClusters {
			newClusters[cluster.Name] = true
		}
	}

	// Watch for new clusters is started in populateClusterConfigLocked to
	// avoid repeating the code.
	m.clustersFromRouteConfig = newClusters
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

	m.listenerWatcher.setLastUpdate(update)

	if update.InlineRouteConfig != nil {
		// If there was a previous route config watcher because of a non-inline
		// route configuration, cancel it.
		m.rdsResourceName = ""
		if m.routeConfigWatcher != nil {
			m.routeConfigWatcher.stop()
		}
		m.routeConfigWatcher = &xdsResourceState[xdsresource.RouteConfigUpdate, routeExtras]{stop: func() {}}
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
	rw := &xdsResourceWatcher[xdsresource.RouteConfigUpdate]{
		onUpdate: func(update *xdsresource.RouteConfigUpdate, onDone func()) {
			m.onRouteConfigResourceUpdate(m.rdsResourceName, update, onDone)
		},
		onError: func(err error, onDone func()) {
			m.onRouteConfigResourceError(m.rdsResourceName, err, onDone)
		},
		onAmbientError: func(err error, onDone func()) {
			m.onRouteConfigResourceAmbientError(m.rdsResourceName, err, onDone)
		},
	}
	if m.routeConfigWatcher != nil {
		m.routeConfigWatcher.stop = xdsresource.WatchRouteConfig(m.xdsClient, m.rdsResourceName, rw)
	} else {
		m.routeConfigWatcher = &xdsResourceState[xdsresource.RouteConfigUpdate, routeExtras]{
			stop: xdsresource.WatchRouteConfig(m.xdsClient, m.rdsResourceName, rw),
		}
	}
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
	m.listenerWatcher.setLastError(err)
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
	m.routeConfigWatcher.setLastError(err)
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

func newClusterWatcher(resourceName string, depMgr *DependencyManager) *xdsResourceState[xdsresource.ClusterUpdate, struct{}] {
	w := &xdsResourceWatcher[xdsresource.ClusterUpdate]{
		onUpdate: func(u *xdsresource.ClusterUpdate, onDone func()) {
			depMgr.onClusterResourceUpdate(resourceName, u, onDone)
		},
		onError: func(err error, onDone func()) {
			depMgr.onClusterResourceError(resourceName, err, onDone)
		},
		onAmbientError: func(err error, onDone func()) {
			depMgr.onClusterAmbientError(resourceName, err, onDone)
		},
	}
	return &xdsResourceState[xdsresource.ClusterUpdate, struct{}]{
		stop: xdsresource.WatchCluster(depMgr.xdsClient, resourceName, w),
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
	m.clusterWatchers[resourceName].setLastUpdate(update)
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
	m.clusterWatchers[resourceName].setLastError(err)
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

func newEndpointWatcher(resourceName string, depMgr *DependencyManager) *xdsResourceState[xdsresource.EndpointsUpdate, struct{}] {
	w := &xdsResourceWatcher[xdsresource.EndpointsUpdate]{
		onUpdate: func(u *xdsresource.EndpointsUpdate, onDone func()) {
			depMgr.onEndpointUpdate(resourceName, u, onDone)
		},
		onError: func(err error, onDone func()) {
			depMgr.onEndpointResourceError(resourceName, err, onDone)
		},
		onAmbientError: func(err error, onDone func()) {
			depMgr.onEndpointAmbientError(resourceName, err, onDone)
		},
	}
	return &xdsResourceState[xdsresource.EndpointsUpdate, struct{}]{
		stop: xdsresource.WatchEndpoints(depMgr.xdsClient, resourceName, w),
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
	m.endpointWatchers[resourceName].setLastUpdate(update)
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
	m.endpointWatchers[resourceName].setLastError(err)
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
	m.endpointWatchers[resourceName].updateLastError(err)
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

	m.dnsResolvers[resourceName].setLastUpdate(&xdsresource.DNSUpdate{Endpoints: endpoints})
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

	err = fmt.Errorf("dns resolver error for target %q: %v", resourceName, m.annotateErrorWithNodeID(err))
	m.logger.Warningf("%v", err)
	state := m.dnsResolvers[resourceName]
	if state.updateReceived {
		state.updateLastError(err)
		return
	}

	state.setLastError(err)
	m.maybeSendUpdateLocked()
}

// RequestDNSReresolution calls all the the DNS resolver's ResolveNow.
func (m *DependencyManager) RequestDNSReresolution(opt resolver.ResolveNowOptions) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, res := range m.dnsResolvers {
		if res.extras.dnsR != nil {
			res.extras.dnsR.ResolveNow(opt)
		}
	}
}

type resolverClientConn struct {
	target string
	depMgr *DependencyManager
}

func (rcc *resolverClientConn) UpdateState(state resolver.State) error {
	rcc.depMgr.dnsSerializer.TrySchedule(func(context.Context) {
		rcc.depMgr.onDNSUpdate(rcc.target, &state)
	})
	return nil
}

func (rcc *resolverClientConn) ReportError(err error) {
	rcc.depMgr.dnsSerializer.TrySchedule(func(context.Context) {
		rcc.depMgr.onDNSError(rcc.target, err)
	})
}

func (rcc *resolverClientConn) NewAddress(addresses []resolver.Address) {
	rcc.UpdateState(resolver.State{Addresses: addresses})
}

func (rcc *resolverClientConn) ParseServiceConfig(string) *serviceconfig.ParseResult {
	return &serviceconfig.ParseResult{Err: fmt.Errorf("service config not supported")}
}

func (m *DependencyManager) newDNSResolver(target string) *xdsResourceState[xdsresource.DNSUpdate, dnsExtras] {
	rcc := &resolverClientConn{
		target: target,
		depMgr: m,
	}
	u, err := url.Parse("dns:///" + target)
	if err != nil {
		err := fmt.Errorf("failed to parse DNS target %q: %v", target, m.annotateErrorWithNodeID(err))
		m.logger.Warningf("%v", err)
		rcc.ReportError(err)
		return &xdsResourceState[xdsresource.DNSUpdate, dnsExtras]{}
	}

	r, err := resolver.Get("dns").Build(resolver.Target{URL: *u}, rcc, resolver.BuildOptions{})
	if err != nil {
		rcc.ReportError(err)
		err := fmt.Errorf("failed to build DNS resolver for target %q: %v", target, m.annotateErrorWithNodeID(err))
		m.logger.Warningf("%v", err)
		return nil
	}

	return &xdsResourceState[xdsresource.DNSUpdate, dnsExtras]{
		extras: dnsExtras{dnsR: r},
		stop:   r.Close,
	}
}

// xdsResourceWatcher is a generic implementation of the xdsresource.Watcher
// interface.
type xdsResourceWatcher[T any] struct {
	onUpdate       func(*T, func())
	onError        func(error, func())
	onAmbientError func(error, func())
}

func (x *xdsResourceWatcher[T]) ResourceChanged(update *T, onDone func()) {
	x.onUpdate(update, onDone)
}

func (x *xdsResourceWatcher[T]) ResourceError(err error, onDone func()) {
	x.onError(err, onDone)
}

func (x *xdsResourceWatcher[T]) AmbientError(err error, onDone func()) {
	x.onAmbientError(err, onDone)
}

// DependencyManagerKey is the type used as the key to store DependencyManager
// in the Attributes field of resolver.states.
type DependencyManagerKey struct{}

// SetDependencyManager returns a copy of state in which the Attributes field is
// updated with the DependencyManager.
func SetDependencyManager(state resolver.State, depmngr *DependencyManager) resolver.State {
	state.Attributes = state.Attributes.WithValue(DependencyManagerKey{}, depmngr)
	return state
}

// DependencyManagerFromResolverState returns DependencyManager stored as an
// attribute in the resolver state.
func DependencyManagerFromResolverState(state resolver.State) *DependencyManager {
	if v := state.Attributes.Value(DependencyManagerKey{}); v != nil {
		return v.(*DependencyManager)
	}
	return nil
}

// ClusterRef represents a reference to a cluster being used by some component.
// It maintains a reference count of the number of users of the cluster. It has
// a method to unsubscribe from the cluster, which decrements the reference
// count and removes the cluster from the clusterSubscriptions map in the
// DependencyManager if the reference count reaches zero.
type ClusterRef struct {
	name     string
	refCount int32
	m        *DependencyManager
}

// CreateClusterRef creates a new ClusterRef with a reference count of 1.
func CreateClusterRef(name string, refCount int32, m *DependencyManager) *ClusterRef {
	cr := &ClusterRef{
		name:     name,
		refCount: refCount,
		m:        m,
	}
	return cr
}

// ClusterSubscription increments the reference count for the cluster and
// returns the ClusterRef. If the cluster is not already being tracked, it adds
// it to the ClusterSubs map.
func (m *DependencyManager) ClusterSubscription(name string) *ClusterRef {
	m.mu.Lock()
	defer m.mu.Unlock()
	subs, ok := m.clusterSubscriptions[name]
	if ok {
		ref := &subs.refCount
		atomic.AddInt32(ref, 1)
		return subs
	}
	m.clusterSubscriptions[name] = CreateClusterRef(name, 1, m)
	if _, ok := m.clustersFromRouteConfig[name]; !ok {
		m.maybeSendUpdateLocked()
	}
	return m.clusterSubscriptions[name]
}

// Unsubscribe decrements the reference count for the cluster. If the reference
// count reaches zero, it removes the cluster from the clusterSubscriptions map
// in the DependencyManager.
func (c *ClusterRef) Unsubscribe() {
	c.m.mu.Lock()
	defer c.m.mu.Unlock()
	ref := atomic.AddInt32(&c.refCount, -1)
	if ref <= 0 {
		delete(c.m.clusterSubscriptions, c.name)
		// This cluster is no longer in the route config, and it has no more
		// references. Now is the time to cancel the watch.
		if _, ok := c.m.clustersFromRouteConfig[c.name]; !ok {
			c.m.maybeSendUpdateLocked()
		}
	}
}

// GetRefCount returns the reference count for a particluar cluster.
func (c *ClusterRef) GetRefCount() int32 {
	return atomic.LoadInt32(&c.refCount)
}

// AddRefCount increases the reference of a cluster by 1.
func (c *ClusterRef) AddRefCount() {
	atomic.AddInt32(&c.refCount, 1)
}
