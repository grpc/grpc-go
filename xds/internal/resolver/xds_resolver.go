/*
 * Copyright 2019 gRPC authors.
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

// Package resolver implements the xds resolver, that does LDS and RDS to find
// the cluster to use.
package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	xdsinternal "google.golang.org/grpc/internal/credentials/xds"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/xds/internal/balancer/clusterresolver"
	rand "math/rand/v2"
	"sync/atomic"
	"unsafe"

	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/pretty"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/wrr"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/resolver"
	rinternal "google.golang.org/grpc/xds/internal/resolver/internal"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

// Scheme is the xDS resolver's scheme.
//
// TODO(easwars): Rename this package as xdsresolver so that this is accessed as
// xdsresolver.Scheme
const (
	Scheme                   = "xds"
	aggregateClusterMaxDepth = 16
)

var (
	errExceedsMaxDepth = fmt.Errorf("aggregate cluster graph exceeds max depth (%d)", aggregateClusterMaxDepth)
	buildProvider      = buildProviderFunc
)

// newBuilderWithConfigForTesting creates a new xds resolver builder using a
// specific xds bootstrap config, so tests can use multiple xds clients in
// different ClientConns at the same time.
func newBuilderWithConfigForTesting(config []byte) (resolver.Builder, error) {
	return &xdsResolverBuilder{
		newXDSClient: func(name string) (xdsclient.XDSClient, func(), error) {
			return xdsclient.NewForTesting(xdsclient.OptionsForTesting{Name: name, Contents: config})
		},
	}, nil
}

// newBuilderWithClientForTesting creates a new xds resolver builder using the
// specific xDS client, so that tests have complete control over the exact
// specific xDS client being used.
func newBuilderWithClientForTesting(client xdsclient.XDSClient) (resolver.Builder, error) {
	return &xdsResolverBuilder{
		newXDSClient: func(string) (xdsclient.XDSClient, func(), error) {
			// Returning an empty close func here means that the responsibility
			// of closing the client lies with the caller.
			return client, func() {}, nil
		},
	}, nil
}

func init() {
	resolver.Register(&xdsResolverBuilder{})
	internal.NewXDSResolverWithConfigForTesting = newBuilderWithConfigForTesting
	internal.NewXDSResolverWithClientForTesting = newBuilderWithClientForTesting

	rinternal.NewWRR = wrr.NewRandom
	rinternal.NewXDSClient = xdsclient.New
}

type xdsResolverBuilder struct {
	newXDSClient func(string) (xdsclient.XDSClient, func(), error)
}

// Build helps implement the resolver.Builder interface.
//
// The xds bootstrap process is performed (and a new xds client is built) every
// time an xds resolver is built.
func (b *xdsResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (_ resolver.Resolver, retErr error) {
	r := &xdsResolver{
		cc:             cc,
		activeClusters: make(map[string]*clusterInfo),
		channelID:      rand.Uint64(),
	}
	defer func() {
		if retErr != nil {
			r.Close()
		}
	}()
	r.logger = prefixLogger(r)
	r.logger.Infof("Creating resolver for target: %+v", target)

	// Initialize the serializer used to synchronize the following:
	// - updates from the xDS client. This could lead to generation of new
	//   service config if resolution is complete.
	// - completion of an RPC to a removed cluster causing the associated ref
	//   count to become zero, resulting in generation of new service config.
	// - stopping of a config selector that results in generation of new service
	//   config.
	ctx, cancel := context.WithCancel(context.Background())
	r.serializer = grpcsync.NewCallbackSerializer(ctx)
	r.serializerCancel = cancel

	// Initialize the xDS client.
	newXDSClient := rinternal.NewXDSClient.(func(string) (xdsclient.XDSClient, func(), error))
	if b.newXDSClient != nil {
		newXDSClient = b.newXDSClient
	}
	client, closeFn, err := newXDSClient(target.String())
	if err != nil {
		return nil, fmt.Errorf("xds: failed to create xds-client: %v", err)
	}
	r.xdsClient = client
	r.xdsClientClose = closeFn

	// Determine the listener resource name and start a watcher for it.
	template, err := r.sanityChecksOnBootstrapConfig(target, opts, r.xdsClient)
	if err != nil {
		return nil, err
	}
	r.dataplaneAuthority = opts.Authority
	r.ldsResourceName = bootstrap.PopulateResourceTemplate(template, target.Endpoint())
	r.listenerWatcher = newListenerWatcher(r.ldsResourceName, r)
	hi := xdsinternal.NewHandshakeInfo(nil, nil, nil, false)
	xdsHIPtr := unsafe.Pointer(hi)
	r.xdsHIPtr = &xdsHIPtr

	var creds credentials.TransportCredentials
	switch {
	case opts.DialCreds != nil:
		creds = opts.DialCreds
	case opts.CredsBundle != nil:
		creds = opts.CredsBundle.TransportCredentials()
	}
	if xc, ok := creds.(interface{ UsesXDS() bool }); ok && xc.UsesXDS() {
		r.xdsCredsInUse = true
	}
	return r, nil
}

// Performs the following sanity checks:
//   - Verifies that the bootstrap configuration is not empty.
//   - Verifies that if xDS credentials are specified by the user, the
//     bootstrap configuration contains certificate providers.
//   - Verifies that if the provided dial target contains an authority, the
//     bootstrap configuration contains server config for that authority.
//
// Returns the listener resource name template to use. If any of the above
// validations fail, a non-nil error is returned.
func (r *xdsResolver) sanityChecksOnBootstrapConfig(target resolver.Target, _ resolver.BuildOptions, client xdsclient.XDSClient) (string, error) {
	bootstrapConfig := client.BootstrapConfig()
	if bootstrapConfig == nil {
		// This is never expected to happen after a successful xDS client
		// creation. Defensive programming.
		return "", fmt.Errorf("xds: bootstrap configuration is empty")
	}

	// Find the client listener template to use from the bootstrap config:
	// - If authority is not set in the target, use the top level template
	// - If authority is set, use the template from the authority map.
	template := bootstrapConfig.ClientDefaultListenerResourceNameTemplate()
	if authority := target.URL.Host; authority != "" {
		authorities := bootstrapConfig.Authorities()
		if authorities == nil {
			return "", fmt.Errorf("xds: authority %q specified in dial target %q is not found in the bootstrap file", authority, target)
		}
		a := authorities[authority]
		if a == nil {
			return "", fmt.Errorf("xds: authority %q specified in dial target %q is not found in the bootstrap file", authority, target)
		}
		if a.ClientListenerResourceNameTemplate != "" {
			// This check will never be false, because
			// ClientListenerResourceNameTemplate is required to start with
			// xdstp://, and has a default value (not an empty string) if unset.
			template = a.ClientListenerResourceNameTemplate
		}
	}
	return template, nil
}

// Name helps implement the resolver.Builder interface.
func (*xdsResolverBuilder) Scheme() string {
	return Scheme
}

// xdsResolver implements the resolver.Resolver interface.
//
// It registers a watcher for ServiceConfig updates with the xdsClient object
// (which performs LDS/RDS queries for the same), and passes the received
// updates to the ClientConn.
type xdsResolver struct {
	cc     resolver.ClientConn
	logger *grpclog.PrefixLogger
	// The underlying xdsClient which performs all xDS requests and responses.
	xdsClient      xdsclient.XDSClient
	xdsClientClose func()
	//depManager     *XdsDependencyManager
	// A random number which uniquely identifies the channel which owns this
	// resolver.
	channelID uint64

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

	ldsResourceName     string
	listenerWatcher     *listenerWatcher
	listenerUpdateRecvd bool
	currentListener     xdsresource.ListenerUpdate

	rdsResourceName        string
	routeConfigWatcher     *routeConfigWatcher
	routeConfigUpdateRecvd bool
	currentRouteConfig     xdsresource.RouteConfigUpdate
	currentVirtualHost     *xdsresource.VirtualHost // Matched virtual host for quick access.

	// activeClusters is a map from cluster name to information about the
	// cluster that includes a ref count and load balancing configuration.
	activeClusters map[string]*clusterInfo
	watchers       map[string]*watcherState // Set of watchers and associated state, keyed by cluster name.

	// The certificate providers are cached here to that they can be closed when
	// a new provider is to be created.
	cachedRoot     certprovider.Provider
	cachedIdentity certprovider.Provider

	xdsCredsInUse bool

	xdsHIPtr *unsafe.Pointer // Accessed atomically.

	curConfigSelector *configSelector
}

// ResolveNow is a no-op at this point.
func (*xdsResolver) ResolveNow(resolver.ResolveNowOptions) {}

func (r *xdsResolver) Close() {
	// Cancel the context passed to the serializer and wait for any scheduled
	// callbacks to complete. Canceling the context ensures that no new
	// callbacks will be scheduled.
	r.serializerCancel()
	<-r.serializer.Done()

	// Note that Close needs to check for nils even if some of them are always
	// set in the constructor. This is because the constructor defers Close() in
	// error cases, and the fields might not be set when the error happens.

	if r.listenerWatcher != nil {
		r.listenerWatcher.stop()
	}
	if r.routeConfigWatcher != nil {
		r.routeConfigWatcher.stop()
	}
	if r.xdsClientClose != nil {
		r.xdsClientClose()
	}
	r.logger.Infof("Shutdown")
}

// sendNewServiceConfig prunes active clusters, generates a new service config
// based on the current set of active clusters, and sends an update to the
// channel with that service config and the provided config selector.  Returns
// false if an error occurs while generating the service config and the update
// cannot be sent.
//
// Only executed in the context of a serializer callback.
func (r *xdsResolver) sendNewServiceConfig(cs *configSelector) bool {
	// Delete entries from r.activeClusters with zero references;
	// otherwise serviceConfigJSON will generate a config including
	// them.
	r.pruneActiveClusters()

	if cs == nil && len(r.activeClusters) == 0 {
		// There are no clusters and we are sending a failing configSelector.
		// Send an empty config, which picks pick-first, with no address, and
		// puts the ClientConn into transient failure.
		r.cc.UpdateState(resolver.State{ServiceConfig: r.cc.ParseServiceConfig("{}")})
		return true
	}

	sc, err := serviceConfigJSON(r.activeClusters)
	if err != nil {
		// JSON marshal error; should never happen.
		r.logger.Errorf("For Listener resource %q and RouteConfiguration resource %q, failed to marshal newly built service config: %v", r.ldsResourceName, r.rdsResourceName, err)
		r.cc.ReportError(err)
		return false
	}
	r.logger.Infof("For Listener resource %q and RouteConfiguration resource %q, generated service config: %v", r.ldsResourceName, r.rdsResourceName, pretty.FormatJSON(sc))

	// Send the update to the ClientConn.
	state := iresolver.SetConfigSelector(resolver.State{
		ServiceConfig: r.cc.ParseServiceConfig(string(sc)),
	}, cs)
	r.cc.UpdateState(xdsclient.SetClient(state, r.xdsClient))
	return true
}

// newConfigSelector creates a new config selector using the most recently
// received listener and route config updates. May add entries to
// r.activeClusters for previously-unseen clusters.
//
// Only executed in the context of a serializer callback.
func (r *xdsResolver) newConfigSelector() (*configSelector, error) {
	cs := &configSelector{
		r: r,
		virtualHost: virtualHost{
			httpFilterConfigOverride: r.currentVirtualHost.HTTPFilterConfigOverride,
			retryConfig:              r.currentVirtualHost.RetryConfig,
		},
		routes:           make([]route, len(r.currentVirtualHost.Routes)),
		clusters:         make(map[string]*clusterInfo),
		httpFilterConfig: r.currentListener.HTTPFilters,
	}

	for i, rt := range r.currentVirtualHost.Routes {
		clusters := rinternal.NewWRR.(func() wrr.WRR)()
		if rt.ClusterSpecifierPlugin != "" {
			clusterName := clusterSpecifierPluginPrefix + rt.ClusterSpecifierPlugin
			clusters.Add(&routeCluster{
				name: clusterName,
			}, 1)
			ci := r.addOrGetActiveClusterInfo(clusterName)
			ci.cfg = xdsChildConfig{ChildPolicy: balancerConfig(r.currentRouteConfig.ClusterSpecifierPlugins[rt.ClusterSpecifierPlugin])}
			cs.clusters[clusterName] = ci
		} else {
			for cluster, wc := range rt.WeightedClusters {
				clusterName := clusterPrefix + cluster
				clusters.Add(&routeCluster{
					name:                     clusterName,
					httpFilterConfigOverride: wc.HTTPFilterConfigOverride,
				}, int64(wc.Weight))
				ci := r.addOrGetActiveClusterInfo(clusterName)
				ci.cfg = xdsChildConfig{ChildPolicy: newBalancerConfig(cdsName, cdsBalancerConfig{Cluster: cluster})}
				cs.clusters[clusterName] = ci
			}
		}
		cs.routes[i].clusters = clusters

		var err error
		cs.routes[i].m, err = xdsresource.RouteToMatcher(rt)
		if err != nil {
			return nil, err
		}
		cs.routes[i].actionType = rt.ActionType
		if rt.MaxStreamDuration == nil {
			cs.routes[i].maxStreamDuration = r.currentListener.MaxStreamDuration
		} else {
			cs.routes[i].maxStreamDuration = *rt.MaxStreamDuration
		}

		cs.routes[i].httpFilterConfigOverride = rt.HTTPFilterConfigOverride
		cs.routes[i].retryConfig = rt.RetryConfig
		cs.routes[i].hashPolicies = rt.HashPolicies
	}

	// Account for this config selector's clusters.  Do this after no further
	// errors may occur.  Note: cs.clusters are pointers to entries in
	// activeClusters.
	for _, ci := range cs.clusters {
		atomic.AddInt32(&ci.refCount, 1)
	}

	return cs, nil
}

// pruneActiveClusters deletes entries in r.activeClusters with zero
// references.
func (r *xdsResolver) pruneActiveClusters() {
	for cluster, ci := range r.activeClusters {
		if atomic.LoadInt32(&ci.refCount) == 0 {
			delete(r.activeClusters, cluster)
		}
	}
}

func (r *xdsResolver) addOrGetActiveClusterInfo(name string) *clusterInfo {
	ci := r.activeClusters[name]
	if ci != nil {
		return ci
	}

	ci = &clusterInfo{refCount: 0}
	r.activeClusters[name] = ci
	return ci
}

type clusterInfo struct {
	// number of references to this cluster; accessed atomically
	refCount int32
	// cfg is the child configuration for this cluster, containing either the
	// csp config or the cds cluster config.
	cfg xdsChildConfig
}

// Determines if the xdsResolver has received all required configuration, i.e
// Listener and RouteConfiguration resources, from the management server, and
// whether a matching virtual host was found in the RouteConfiguration resource.
func (r *xdsResolver) resolutionComplete() bool {
	return r.listenerUpdateRecvd && r.routeConfigUpdateRecvd && r.currentVirtualHost != nil
}

// onResolutionComplete performs the following actions when resolution is
// complete, i.e Listener and RouteConfiguration resources have been received
// from the management server and a matching virtual host is found in the
// latter.
//   - creates a new config selector (this involves incrementing references to
//     clusters owned by this config selector).
//   - stops the old config selector (this involves decrementing references to
//     clusters owned by this config selector).
//   - prunes active clusters and pushes a new service config to the channel.
//   - updates the current config selector used by the resolver.
//
// Only executed in the context of a serializer callback.
func (r *xdsResolver) onResolutionComplete() {
	if !r.resolutionComplete() {
		return
	}

	cs, err := r.newConfigSelector()
	if err != nil {
		r.logger.Warningf("Failed to build a config selector for resource %q: %v", r.ldsResourceName, err)
		r.cc.ReportError(err)
		return
	}

	if !r.sendNewServiceConfig(cs) {
		// JSON error creating the service config (unexpected); erase
		// this config selector and ignore this update, continuing with
		// the previous config selector.
		cs.stop()
		return
	}

	if envconfig.NewXdsResolverEnabled {
		// Start watching clusters referenced in the config selector
		for clusterName := range cs.clusters {
			newClusterConfigWatcher(clusterName, r)
		}
	}

	r.curConfigSelector.stop()
	r.curConfigSelector = cs
}

func (r *xdsResolver) applyRouteConfigUpdate(update xdsresource.RouteConfigUpdate) {
	matchVh := xdsresource.FindBestMatchingVirtualHost(r.dataplaneAuthority, update.VirtualHosts)
	if matchVh == nil {
		r.onError(fmt.Errorf("no matching virtual host found for %q", r.dataplaneAuthority))
		return
	}
	r.currentRouteConfig = update
	r.currentVirtualHost = matchVh
	r.routeConfigUpdateRecvd = true

	r.onResolutionComplete()
}

// onError propagates the error up to the channel. And since this is invoked
// only for non resource-not-found errors, we don't have to update resolver
// state and we can keep using the old config.
//
// Only executed in the context of a serializer callback.
func (r *xdsResolver) onError(err error) {
	r.cc.ReportError(err)
}

// Contains common functionality to be executed when resources of either type
// are removed.
//
// Only executed in the context of a serializer callback.
func (r *xdsResolver) onResourceNotFound() {
	// We cannot remove clusters from the service config that have ongoing RPCs.
	// Instead, what we can do is to send an erroring (nil) config selector
	// along with normal service config. This will ensure that new RPCs will
	// fail, and once the active RPCs complete, the reference counts on the
	// clusters will come down to zero. At that point, we will send an empty
	// service config with no addresses. This results in the pick-first
	// LB policy being configured on the channel, and since there are no
	// address, pick-first will put the channel in TRANSIENT_FAILURE.
	r.sendNewServiceConfig(nil)

	// Stop and dereference the active config selector, if one exists.
	r.curConfigSelector.stop()
	r.curConfigSelector = nil
}

// Only executed in the context of a serializer callback.
func (r *xdsResolver) onListenerResourceUpdate(update xdsresource.ListenerUpdate) {
	if r.logger.V(2) {
		r.logger.Infof("Received update for Listener resource %q: %v", r.ldsResourceName, pretty.ToJSON(update))
	}

	r.currentListener = update
	r.listenerUpdateRecvd = true

	if update.InlineRouteConfig != nil {
		// If there was a previous route config watcher because of a non-inline
		// route configuration, cancel it.
		r.rdsResourceName = ""
		if r.routeConfigWatcher != nil {
			r.routeConfigWatcher.stop()
			r.routeConfigWatcher = nil
		}

		r.applyRouteConfigUpdate(*update.InlineRouteConfig)
		return
	}

	// We get here only if there was no inline route configuration.

	// If the route config name has not changed, send an update with existing
	// route configuration and the newly received listener configuration.
	if r.rdsResourceName == update.RouteConfigName {
		r.onResolutionComplete()
		return
	}

	// If the route config name has changed, cancel the old watcher and start a
	// new one. At this point, since we have not yet resolved the new route
	// config name, we don't send an update to the channel, and therefore
	// continue using the old route configuration (if received) until the new
	// one is received.
	r.rdsResourceName = update.RouteConfigName
	if r.routeConfigWatcher != nil {
		r.routeConfigWatcher.stop()
		r.currentVirtualHost = nil
		r.routeConfigUpdateRecvd = false
	}
	r.routeConfigWatcher = newRouteConfigWatcher(r.rdsResourceName, r)
}

func (r *xdsResolver) onListenerResourceError(err error) {
	if r.logger.V(2) {
		r.logger.Infof("Received error for Listener resource %q: %v", r.ldsResourceName, err)
	}
	r.onError(err)
}

// Only executed in the context of a serializer callback.
func (r *xdsResolver) onListenerResourceNotFound() {
	if r.logger.V(2) {
		r.logger.Infof("Received resource-not-found-error for Listener resource %q", r.ldsResourceName)
	}

	r.listenerUpdateRecvd = false

	if r.routeConfigWatcher != nil {
		r.routeConfigWatcher.stop()
	}
	r.rdsResourceName = ""
	r.currentVirtualHost = nil
	r.routeConfigUpdateRecvd = false
	r.routeConfigWatcher = nil

	r.onResourceNotFound()
}

// Only executed in the context of a serializer callback.
func (r *xdsResolver) onRouteConfigResourceUpdate(name string, update xdsresource.RouteConfigUpdate) {
	if r.logger.V(2) {
		r.logger.Infof("Received update for RouteConfiguration resource %q: %v", name, pretty.ToJSON(update))
	}

	if r.rdsResourceName != name {
		// Drop updates from canceled watchers.
		return
	}

	r.applyRouteConfigUpdate(update)
}

// Only executed in the context of a serializer callback.
func (r *xdsResolver) onRouteConfigResourceError(name string, err error) {
	if r.logger.V(2) {
		r.logger.Infof("Received error for RouteConfiguration resource %q: %v", name, err)
	}
	r.onError(err)
}

// Only executed in the context of a serializer callback.
func (r *xdsResolver) onRouteConfigResourceNotFound(name string) {
	if r.logger.V(2) {
		r.logger.Infof("Received resource-not-found-error for RouteConfiguration resource %q", name)
	}

	if r.rdsResourceName != name {
		return
	}
	r.onResourceNotFound()
}

// Only executed in the context of a serializer callback.
func (r *xdsResolver) onClusterRefDownToZero() {
	r.sendNewServiceConfig(r.curConfigSelector)
}

// Handles an error Cluster update from the xDS client. Propagates the error
// down to the child policy if one exists, or puts the channel in
// TRANSIENT_FAILURE.
//
// Only executed in the context of a serializer callback.
func (r *xdsResolver) onClusterError(name string, err error) {
	r.logger.Warningf("Cluster resource %q received error update: %v", name, err)

	r.onError(err)
}

// Handles a resource-not-found error from the xDS client. Propagates the error
// down to the child policy if one exists, or puts the channel in
// TRANSIENT_FAILURE.
//
// Only executed in the context of a serializer callback.
func (r *xdsResolver) onClusterResourceNotFound(name string) {
	if r.logger.V(2) {
		r.logger.Infof("Received resource-not-found-error for ClusterConfiguration resource %q", name)
	}

	r.onResourceNotFound()
}

func (r *xdsResolver) onClusterUpdate(name string, update xdsresource.ClusterUpdate) {
	// Update the state with the latest cluster update
	r.watchers[name].lastUpdate = &update

	r.logger.Infof("Received Cluster resource update: %s", pretty.ToJSON(update))

	if err := r.handleSecurityConfig(update.SecurityCfg); err != nil {
		r.onClusterError(name, fmt.Errorf("invalid security config: %v", err))
		return
	}

	// Generate discovery mechanisms for the cluster
	clustersSeen := make(map[string]bool)
	_, _, err := r.generateDMsForCluster(name, 0, nil, clustersSeen)
	if err != nil {
		r.onClusterError(name, fmt.Errorf("failed to generate discovery mechanisms: %v", err))
		return
	}

	// TODO(aranjans): DMS will be used to figure out the endpoints.

	// Clean up watchers for clusters that were not seen in this update
	for cluster := range clustersSeen {
		if _, ok := r.activeClusters[cluster]; !ok {
			r.watchers[cluster].cancelWatch()
			delete(r.activeClusters, cluster)
		}
	}
}

// Generates discovery mechanisms for the cluster graph rooted at `name`. This
// method is called recursively if `name` corresponds to an aggregate cluster,
// with the base case for recursion being a leaf cluster. If a new cluster is
// encountered when traversing the graph, a watcher is created for it.
//
// Inputs:
// - name: name of the cluster to start from
// - depth: recursion depth of the current cluster, starting from root
// - dms: prioritized list of current discovery mechanisms
// - clustersSeen: cluster names seen so far in the graph traversal
//
// Outputs:
//   - new prioritized list of discovery mechanisms
//   - boolean indicating if traversal of the aggregate cluster graph is
//     complete. If false, the above list of discovery mechanisms is ignored.
//   - error indicating if any error was encountered as part of the graph
//     traversal. If error is non-nil, the other return values are ignored.
//
// Only executed in the context of a serializer callback.
func (r *xdsResolver) generateDMsForCluster(name string, depth int, dms []clusterresolver.DiscoveryMechanism, clustersSeen map[string]bool) ([]clusterresolver.DiscoveryMechanism, bool, error) {
	if depth >= aggregateClusterMaxDepth {
		return dms, false, errExceedsMaxDepth
	}

	if clustersSeen[name] {
		// Discovery mechanism already seen through a different branch.
		return dms, true, nil
	}
	clustersSeen[name] = true

	state, ok := r.watchers[name]
	if !ok {
		// If we have not seen this cluster so far, create a watcher for it, add
		// it to the map, start the watch and return.
		newClusterConfigWatcher(name, r)

		// And since we just created the watcher, we know that we haven't
		// resolved the cluster graph yet.
		return dms, false, nil
	}

	// A watcher exists, but no update has been received yet.
	if state.lastUpdate == nil {
		return dms, false, nil
	}

	var dm clusterresolver.DiscoveryMechanism
	cluster := state.lastUpdate
	switch cluster.ClusterType {
	case xdsresource.ClusterTypeAggregate:
		// This boolean is used to track if any of the clusters in the graph is
		// not yet completely resolved or returns errors, thereby allowing us to
		// traverse as much of the graph as possible (and start the associated
		// watches where required) to ensure that clustersSeen contains all
		// clusters in the graph that we can traverse to.
		missingCluster := false
		var err error
		for _, child := range cluster.PrioritizedClusterNames {
			var ok bool
			dms, ok, err = r.generateDMsForCluster(child, depth+1, dms, clustersSeen)
			if err != nil || !ok {
				missingCluster = true
			}
		}
		return dms, !missingCluster, err
	case xdsresource.ClusterTypeEDS:
		dm = clusterresolver.DiscoveryMechanism{
			Type:                  clusterresolver.DiscoveryMechanismTypeEDS,
			Cluster:               cluster.ClusterName,
			EDSServiceName:        cluster.EDSServiceName,
			MaxConcurrentRequests: cluster.MaxRequests,
			LoadReportingServer:   cluster.LRSServerConfig,
		}
	case xdsresource.ClusterTypeLogicalDNS:
		dm = clusterresolver.DiscoveryMechanism{
			Type:        clusterresolver.DiscoveryMechanismTypeLogicalDNS,
			Cluster:     cluster.ClusterName,
			DNSHostname: cluster.DNSHostName,
		}
	}
	odJSON := cluster.OutlierDetection
	// "In the cds LB policy, if the outlier_detection field is not set in
	// the Cluster resource, a "no-op" outlier_detection config will be
	// generated in the corresponding DiscoveryMechanism config, with all
	// fields unset." - A50
	if odJSON == nil {
		// This will pick up top level defaults in Cluster Resolver
		// ParseConfig, but sre and fpe will be nil still so still a
		// "no-op" config.
		odJSON = json.RawMessage(`{}`)
	}
	dm.OutlierDetection = odJSON

	dm.TelemetryLabels = cluster.TelemetryLabels

	return append(dms, dm), true, nil
}

// handleSecurityConfig processes the security configuration received from the
// management server, creates appropriate certificate provider plugins, and
// updates the HandshakeInfo which is added as an address attribute in
// NewSubConn() calls.
//
// Only executed in the context of a serializer callback.
func (r *xdsResolver) handleSecurityConfig(config *xdsresource.SecurityConfig) error {
	// If xdsCredentials are not in use, i.e, the user did not want to get
	// security configuration from an xDS server, we should not be acting on the
	// received security config here. Doing so poses a security threat.
	if !r.xdsCredsInUse {
		return nil
	}
	var xdsHI *xdsinternal.HandshakeInfo

	// Security config being nil is a valid case where the management server has
	// not sent any security configuration. The xdsCredentials implementation
	// handles this by delegating to its fallback credentials.
	if config == nil {
		// We need to explicitly set the fields to nil here since this might be
		// a case of switching from a good security configuration to an empty
		// one where fallback credentials are to be used.
		xdsHI = xdsinternal.NewHandshakeInfo(nil, nil, nil, false)
		atomic.StorePointer(r.xdsHIPtr, unsafe.Pointer(xdsHI))
		return nil
	}

	// A root provider is required whether we are using TLS or mTLS.
	cpc := r.xdsClient.BootstrapConfig().CertProviderConfigs()
	rootProvider, err := buildProvider(cpc, config.RootInstanceName, config.RootCertName, false, true)
	if err != nil {
		return err
	}

	// The identity provider is only present when using mTLS.
	var identityProvider certprovider.Provider
	if name, cert := config.IdentityInstanceName, config.IdentityCertName; name != "" {
		var err error
		identityProvider, err = buildProvider(cpc, name, cert, true, false)
		if err != nil {
			return err
		}
	}

	// Close the old providers and cache the new ones.
	if r.cachedRoot != nil {
		r.cachedRoot.Close()
	}
	if r.cachedIdentity != nil {
		r.cachedIdentity.Close()
	}
	r.cachedRoot = rootProvider
	r.cachedIdentity = identityProvider
	xdsHI = xdsinternal.NewHandshakeInfo(rootProvider, identityProvider, config.SubjectAltNameMatchers, false)
	atomic.StorePointer(r.xdsHIPtr, unsafe.Pointer(xdsHI))
	return nil
}

func buildProviderFunc(configs map[string]*certprovider.BuildableConfig, instanceName, certName string, wantIdentity, wantRoot bool) (certprovider.Provider, error) {
	cfg, ok := configs[instanceName]
	if !ok {
		// Defensive programming. If a resource received from the management
		// server contains a certificate provider instance name that is not
		// found in the bootstrap, the resource is NACKed by the xDS client.
		return nil, fmt.Errorf("certificate provider instance %q not found in bootstrap file", instanceName)
	}
	provider, err := cfg.Build(certprovider.BuildOptions{
		CertName:     certName,
		WantIdentity: wantIdentity,
		WantRoot:     wantRoot,
	})
	if err != nil {
		// This error is not expected since the bootstrap process parses the
		// config and makes sure that it is acceptable to the plugin. Still, it
		// is possible that the plugin parses the config successfully, but its
		// Build() method errors out.
		return nil, fmt.Errorf("xds: failed to get security plugin instance (%+v): %v", cfg, err)
	}
	return provider, nil
}
