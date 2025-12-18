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
 */

// Package cdsbalancer implements a balancer to handle CDS responses.
package cdsbalancer

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"unsafe"

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal/balancer/nop"
	xdsinternal "google.golang.org/grpc/internal/credentials/xds"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/pretty"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/xds/balancer/outlierdetection"
	"google.golang.org/grpc/internal/xds/balancer/priority"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/internal/xds/xdsdepmgr"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

const (
	cdsName = "cds_experimental"
)

var (
	errBalancerClosed = fmt.Errorf("cds_experimental LB policy is closed")

	// newChildBalancer is a helper function to build a new priority balancer
	// and will be overridden in unittests.
	newChildBalancer = func(cc balancer.ClientConn, opts balancer.BuildOptions) (balancer.Balancer, error) {
		builder := balancer.Get(priority.Name)
		if builder == nil {
			return nil, fmt.Errorf("xds: no balancer builder with name %v", priority.Name)
		}
		// We directly pass the parent clientConn to the underlying priority
		// balancer because the cdsBalancer does not deal with subConns.
		return builder.Build(cc, opts), nil
	}
	buildProvider = buildProviderFunc

	// x509SystemCertPoolFunc is used for mocking the system cert pool for
	// tests.
	x509SystemCertPoolFunc = x509.SystemCertPool
)

func init() {
	balancer.Register(bb{})
}

// bb implements the balancer.Builder interface to help build a cdsBalancer.
// It also implements the balancer.ConfigParser interface to help parse the
// JSON service config, to be passed to the cdsBalancer.
type bb struct{}

// Build creates a new CDS balancer with the ClientConn.
func (bb) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	builder := balancer.Get(priority.Name)
	if builder == nil {
		// Shouldn't happen, registered through imported priority balancer
		// package, but defensive programming.
		logger.Errorf("%q LB policy is needed but not registered", priority.Name)
		return nop.NewBalancer(cc, fmt.Errorf("%q LB policy is needed but not registered", priority.Name))
	}
	parser, ok := builder.(balancer.ConfigParser)
	if !ok {
		// Shouldn't happen, imported Priority builder has this method.
		logger.Errorf("%q LB policy does not implement a config parser", priority.Name)
		return nop.NewBalancer(cc, fmt.Errorf("%q LB policy does not implement a config parser", priority.Name))
	}

	ctx, cancel := context.WithCancel(context.Background())
	hi := xdsinternal.NewHandshakeInfo(nil, nil, nil, false)
	xdsHIPtr := unsafe.Pointer(hi)
	b := &cdsBalancer{
		bOpts:             opts,
		childConfigParser: parser,
		serializer:        grpcsync.NewCallbackSerializer(ctx),
		serializerCancel:  cancel,
		xdsHIPtr:          &xdsHIPtr,
		clusterConfig:     make(map[string]*xdsresource.ClusterResult),
		priorityConfig:    make(map[discoveryMechanismKey]priorityConfig),
	}
	b.ccw = &ccWrapper{
		ClientConn: cc,
		xdsHIPtr:   b.xdsHIPtr,
	}
	b.logger = prefixLogger(b)
	b.logger.Infof("Created")

	var creds credentials.TransportCredentials
	switch {
	case opts.DialCreds != nil:
		creds = opts.DialCreds
	case opts.CredsBundle != nil:
		creds = opts.CredsBundle.TransportCredentials()
	}
	if xc, ok := creds.(interface{ UsesXDS() bool }); ok && xc.UsesXDS() {
		b.xdsCredsInUse = true
	}
	b.logger.Infof("xDS credentials in use: %v", b.xdsCredsInUse)
	return b
}

// Name returns the name of balancers built by this builder.
func (bb) Name() string {
	return cdsName
}

// lbConfig represents the loadBalancingConfig section of the service config
// for the cdsBalancer.
type lbConfig struct {
	serviceconfig.LoadBalancingConfig
	ClusterName string `json:"Cluster"`
	IsDynamic   bool   `json:"Is_Dynamic"`
}

// ParseConfig parses the JSON load balancer config provided into an
// internal form or returns an error if the config is invalid.
func (bb) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	var cfg lbConfig
	if err := json.Unmarshal(c, &cfg); err != nil {
		return nil, fmt.Errorf("xds: unable to unmarshal lbconfig: %s, error: %v", string(c), err)
	}
	return &cfg, nil
}

// cdsBalancer implements a CDS based LB policy. It instantiates a priority
// balancer. Implements the balancer.Balancer interface which is exposed to gRPC
// and implements the balancer.ClientConn interface which is exposed to the
// cluster_resolver balancer.
type cdsBalancer struct {
	// The following fields are initialized at build time and are either
	// read-only after that or provide their own synchronization, and therefore
	// do not need to be guarded by a mutex.
	ccw               *ccWrapper            // ClientConn interface passed to child LB.
	bOpts             balancer.BuildOptions // BuildOptions passed to child LB.
	childConfigParser balancer.ConfigParser // Config parser for cluster_resolver LB policy.
	logger            *grpclog.PrefixLogger // Prefix logger for all logging.
	xdsCredsInUse     bool

	xdsHIPtr *unsafe.Pointer // Accessed atomically.

	// The serializer and its cancel func are initialized at build time, and the
	// rest of the fields here are only accessed from serializer callbacks (or
	// from balancer.Balancer methods, which themselves are guaranteed to be
	// mutually exclusive) and hence do not need to be guarded by a mutex.
	serializer       *grpcsync.CallbackSerializer // Serializes updates from gRPC and xDS client.
	serializerCancel context.CancelFunc           // Stops the above serializer.
	childLB          balancer.Balancer            // Child policy, built upon resolution of the cluster graph.
	xdsClient        xdsclient.XDSClient          // xDS client to watch Cluster resources.
	attrsWithClient  *attributes.Attributes       // Attributes with xdsClient attached to be passed to the child policies.
	clusterConfig    map[string]*xdsresource.ClusterResult
	clusterSubs      *xdsdepmgr.ClusterRef                // Cluster resource subscription for dynamic clusters.
	lbCfg            *lbConfig                            // Current load balancing configuration.
	xdsLBPolicy      internalserviceconfig.BalancerConfig // Stores the locality and endpoint picking policy.
	priorityConfig   map[discoveryMechanismKey]priorityConfig
	// Each new discovery mechanism needs a child name generator to reuse child
	// policy names. But to make sure the names across discover mechanism
	// doesn't conflict, we need a seq ID. This ID is incremented for each new
	// discover mechanism.
	childNameGeneratorSeqID uint64
	priorities              []priorityConfig
	configRaw               *serviceconfig.ParseResult
	// The certificate providers are cached here to that they can be closed when
	// a new provider is to be created.
	cachedRoot     certprovider.Provider
	cachedIdentity certprovider.Provider
}

// handleSecurityConfig processes the security configuration received from the
// management server, creates appropriate certificate provider plugins, and
// updates the HandshakeInfo which is added as an address attribute in
// NewSubConn() calls.
//
// Only executed in the context of a serializer callback.
func (b *cdsBalancer) handleSecurityConfig(config *xdsresource.SecurityConfig) error {
	// If xdsCredentials are not in use, i.e, the user did not want to get
	// security configuration from an xDS server, we should not be acting on the
	// received security config here. Doing so poses a security threat.
	if !b.xdsCredsInUse {
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
		atomic.StorePointer(b.xdsHIPtr, unsafe.Pointer(xdsHI))
		return nil

	}

	// A root provider is required whether we are using TLS or mTLS.
	cpc := b.xdsClient.BootstrapConfig().CertProviderConfigs()
	var rootProvider certprovider.Provider
	if config.UseSystemRootCerts {
		rootProvider = systemRootCertsProvider{}
	} else {
		rp, err := buildProvider(cpc, config.RootInstanceName, config.RootCertName, false, true)
		if err != nil {
			return err
		}
		rootProvider = rp
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
	if b.cachedRoot != nil {
		b.cachedRoot.Close()
	}
	if b.cachedIdentity != nil {
		b.cachedIdentity.Close()
	}
	b.cachedRoot = rootProvider
	b.cachedIdentity = identityProvider
	xdsHI = xdsinternal.NewHandshakeInfo(rootProvider, identityProvider, config.SubjectAltNameMatchers, false)
	atomic.StorePointer(b.xdsHIPtr, unsafe.Pointer(xdsHI))
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

// updateChildConfig builds child policy configuration using XDSConfig.
//
// A child policy is created if one doesn't already exist. The newly built
// configuration is then pushed to the child policy.
func (b *cdsBalancer) updateChildConfig() {
	if b.childLB == nil {
		childLB, err := newChildBalancer(b.ccw, b.bOpts)
		if err != nil {
			b.logger.Errorf("Failed to create child policy of type %s: %v", priority.Name, err)
			return
		}
		b.childLB = childLB

	}

	childCfgBytes, endpoints, err := buildPriorityConfigJSON(b.priorities, &b.xdsLBPolicy)
	if err != nil {
		b.logger.Warningf("Failed to build child policy config: %v", err)
		return
	}
	childCfg, err := b.childConfigParser.ParseConfig(childCfgBytes)
	if err != nil {
		b.logger.Warningf("Failed to parse child policy config. This should never happen because the config was generated: %v", err)
		return
	}
	if b.logger.V(2) {
		b.logger.Infof("Built child policy config: %s", pretty.ToJSON(childCfg))
	}

	flattenedAddrs := make([]resolver.Address, len(endpoints))
	for i := range endpoints {
		for j := range endpoints[i].Addresses {
			addr := endpoints[i].Addresses[j]
			addr.BalancerAttributes = endpoints[i].Attributes
			// If the endpoint has multiple addresses, only the first is added
			// to the flattened address list. This ensures that LB policies
			// that don't support endpoints create only one subchannel to a
			// backend.
			if j == 0 {
				flattenedAddrs[i] = addr
			}
			// BalancerAttributes need to be present in endpoint addresses. This
			// temporary workaround is required to make load reporting work
			// with the old pickfirst policy which creates SubConns with multiple
			// addresses. Since the addresses can be from different localities,
			// an Address.BalancerAttribute is used to identify the locality of the
			// address used by the transport. This workaround can be removed once
			// the old pickfirst is removed.
			// See https://github.com/grpc/grpc-go/issues/7339
			endpoints[i].Addresses[j] = addr
		}
	}
	if err := b.childLB.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Endpoints:     endpoints,
			Addresses:     flattenedAddrs,
			ServiceConfig: b.configRaw,
			Attributes:    b.attrsWithClient,
		},
		BalancerConfig: childCfg,
	}); err != nil {
		b.logger.Warningf("Failed to push config to child policy: %v", err)
	}
}

// UpdateClientConnState receives the serviceConfig (which contains the
// clusterName) , XDSConfig, Dependency Manager and the xdsClient object from
// the xdsResolver.
func (b *cdsBalancer) UpdateClientConnState(state balancer.ClientConnState) error {
	if b.xdsClient == nil {
		c := xdsclient.FromResolverState(state.ResolverState)
		if c == nil {
			b.logger.Warningf("Received balancer config with no xDS client")
			return balancer.ErrBadResolverState
		}
		b.xdsClient = c
	}
	b.logger.Infof("Received balancer config update: %s", pretty.ToJSON(state.BalancerConfig))

	// The errors checked here should ideally never happen because the
	// ServiceConfig in this case is prepared by the xdsResolver and is not
	// something that is received on the wire.
	lbCfg, ok := state.BalancerConfig.(*lbConfig)
	if !ok {
		b.logger.Warningf("Received unexpected balancer config type: %T", state.BalancerConfig)
		return balancer.ErrBadResolverState
	}
	if lbCfg.ClusterName == "" {
		b.logger.Warningf("Received balancer config with no cluster name")
		return balancer.ErrBadResolverState
	}
	b.configRaw = state.ResolverState.ServiceConfig
	xdsConfig := xdsresource.XDSConfigFromResolverState(state.ResolverState)
	if xdsConfig == nil {
		b.logger.Warningf("Received balancer config with no xDS config")
		return balancer.ErrBadResolverState
	}
	b.attrsWithClient = state.ResolverState.Attributes
	b.clusterConfig = xdsConfig.Clusters
	b.lbCfg = lbCfg

	// Handle the update in a blocking fashion.
	errCh := make(chan error, 1)
	callback := func(context.Context) {
		b.processConfigUpdate()
		errCh <- nil
	}
	onFailure := func() {
		// The call to Schedule returns false *only* if the serializer has been
		// closed, which happens only when we receive an update after close.
		errCh <- errBalancerClosed
	}
	b.serializer.ScheduleOr(callback, onFailure)
	return <-errCh
}

// processConfigUpdate processes the update from the xDS resolver.
func (b *cdsBalancer) processConfigUpdate() {
	// If the cluster is dynamic and we dont have a subscription yet, create
	// one.
	if b.lbCfg.IsDynamic && b.clusterSubs == nil {
		xdsdepmngr := xdsdepmgr.DependencyManagerFromResolverState(resolver.State{Attributes: b.attrsWithClient})
		if xdsdepmngr == nil {
			// This should ideally strictly exist as we are using the new resolver
			b.logger.Errorf("No dependency manager passed in resolver state")
			return
		}
		b.clusterSubs = xdsdepmngr.Clustersubscription(b.lbCfg.ClusterName)
		return
	}

	if _, ok := b.clusterConfig[b.lbCfg.ClusterName]; !ok {
		// If the cluster is dynamic, could be that we just subscribed and
		// have not yet received the cluster update yet.
		if b.lbCfg.IsDynamic {
			return
		}
		b.onClusterResourceError(b.lbCfg.ClusterName, b.annotateErrorWithNodeID(fmt.Errorf("did not find the static cluster in xdsConfig")))
		return
	}

	// If the cluster resource has an error report transient failure
	if b.clusterConfig[b.lbCfg.ClusterName].Err != nil {
		b.onClusterResourceError(b.lbCfg.ClusterName, b.clusterConfig[b.lbCfg.ClusterName].Err)
		return
	}

	if err := b.handleSecurityConfig(b.clusterConfig[b.lbCfg.ClusterName].Config.Cluster.SecurityCfg); err != nil {
		// If the security config is invalid, for example, if the provider
		// instance is not found in the bootstrap config, we need to put the
		// channel in transient failure.
		b.onClusterResourceError(b.lbCfg.ClusterName, b.annotateErrorWithNodeID(fmt.Errorf("received Cluster resource contains invalid security config: %v", err)))
		return
	}
	b.handleClusterUpdate()
}

// ResolverError handles errors reported by the xdsResolver.
func (b *cdsBalancer) ResolverError(err error) {
	b.serializer.TrySchedule(func(context.Context) {
		// Missing Listener or RouteConfiguration on the management server
		// results in a 'resource not found' error from the xDS resolver.
		if xdsresource.ErrType(err) == xdsresource.ErrorTypeResourceNotFound {
			b.closeChildPolicyAndReportTF(err)
			return
		}
		var root string
		if b.lbCfg != nil {
			root = b.lbCfg.ClusterName
		}
		b.onClusterResourceError(root, err)
	})
}

// UpdateSubConnState handles subConn updates from gRPC.
func (b *cdsBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	b.logger.Errorf("UpdateSubConnState(%v, %+v) called unexpectedly", sc, state)
}

// closeChildPolicyAndReportTF closes the child policy, if it exists, and
// updates the connectivity state of the channel to TransientFailure with an
// error picker.
//
// Only executed in the context of a serializer callback.
func (b *cdsBalancer) closeChildPolicyAndReportTF(err error) {
	if b.childLB != nil {
		b.childLB.Close()
		b.childLB = nil
	}
	b.ccw.UpdateState(balancer.State{
		ConnectivityState: connectivity.TransientFailure,
		Picker:            base.NewErrPicker(err),
	})
}

// Close cancels the CDS watch, closes the child policy and closes the
// cdsBalancer.
func (b *cdsBalancer) Close() {
	b.serializer.TrySchedule(func(context.Context) {

		if b.childLB != nil {
			b.childLB.Close()
			b.childLB = nil
		}
		if b.cachedRoot != nil {
			b.cachedRoot.Close()
		}
		if b.cachedIdentity != nil {
			b.cachedIdentity.Close()
		}
		if b.clusterSubs != nil {
			b.clusterSubs.Unsubscribe()
		}
		b.logger.Infof("Shutdown")
	})
	b.serializerCancel()
	<-b.serializer.Done()
}

func (b *cdsBalancer) ExitIdle() {
	b.serializer.TrySchedule(func(context.Context) {
		if b.childLB == nil {
			b.logger.Warningf("Received ExitIdle with no child policy")
			return
		}
		// This implementation assumes the child balancer supports
		// ExitIdle (but still checks for the interface's existence to
		// avoid a panic if not).  If the child does not, no subconns
		// will be connected.
		b.childLB.ExitIdle()
	})
}

// Node ID needs to be manually added to errors generated in the following
// scenarios:
//   - resource-does-not-exist: since the xDS watch API uses a separate callback
//     instead of returning an error value. TODO(gRFC A88): Once A88 is
//     implemented, the xDS client will be able to add the node ID to
//     resource-does-not-exist errors as well, and we can get rid of this
//     special handling.
//   - received a good update from the xDS client, but the update either contains
//     an invalid security configuration or contains invalid aggragate cluster
//     config.
func (b *cdsBalancer) annotateErrorWithNodeID(err error) error {
	nodeID := b.xdsClient.BootstrapConfig().Node().GetId()
	return fmt.Errorf("[xDS node id: %v]: %w", nodeID, err)
}

// Handles a good XDSConfig update from the xDS resolver. Builds the child
// policy config and pushes it down.
//
// Only executed in the context of a serializer callback.
func (b *cdsBalancer) handleClusterUpdate() {
	clusterName := b.lbCfg.ClusterName
	state := b.clusterConfig[clusterName]

	var newPriorities []priorityConfig

	switch state.Config.Cluster.ClusterType {
	case xdsresource.ClusterTypeEDS, xdsresource.ClusterTypeLogicalDNS:
		p, err := b.updatePriority(clusterName)
		if err != nil {
			b.logger.Warningf("failed to update priority for cluster %q: %v", clusterName, err)
			return
		}
		newPriorities = append(newPriorities, p)

	case xdsresource.ClusterTypeAggregate:
		for _, leaf := range state.Config.AggregateConfig.LeafClusters {
			if leafConfig, ok := b.clusterConfig[leaf]; ok {
				// Only consider EDS and LogicalDNS leaf clusters
				if leafConfig.Config.Cluster.ClusterType == xdsresource.ClusterTypeEDS ||
					leafConfig.Config.Cluster.ClusterType == xdsresource.ClusterTypeLogicalDNS {
					p, err := b.updatePriority(leaf)
					if err != nil {
						b.logger.Warningf("failed to update priority for cluster %q: %v", clusterName, err)
						return
					}
					newPriorities = append(newPriorities, p)
				}
			}
		}
	}
	b.priorities = newPriorities

	b.updateOutlierDetectionAndTelemetry()

	if err := json.Unmarshal(b.clusterConfig[b.lbCfg.ClusterName].Config.Cluster.LBPolicy, &b.xdsLBPolicy); err != nil {
		b.logger.Errorf("cds_balancer: error unmarshalling xDS LB Policy: %v", err)
		return
	}
	b.updateChildConfig()
}

func (b *cdsBalancer) updatePriority(clusterName string) (priorityConfig, error) {
	dm, err := b.createDiscoveryMechanism(clusterName)
	if err != nil {
		b.logger.Warningf("failed to create discovery mechanism: %v", err)
		return priorityConfig{}, err
	}
	pc, err := b.updatePriorityConfig(dm)
	if err != nil {
		b.logger.Warningf("failed to update priority config: %v", err)
		return priorityConfig{}, err
	}
	return pc, nil
}

// updateOutlierDetectionAndTelemetry updates Outlier Detection configs and Telemetry Labels for all priorities.
func (b *cdsBalancer) updateOutlierDetectionAndTelemetry() {
	for i := range b.priorities {
		cluster := b.clusterConfig[b.priorities[i].mechanism.Cluster].Config.Cluster
		odJSON := cluster.OutlierDetection
		if odJSON == nil {
			odJSON = json.RawMessage(`{}`)
		}
		b.priorities[i].mechanism.OutlierDetection = odJSON
		b.priorities[i].mechanism.TelemetryLabels = cluster.TelemetryLabels

		odBuilder := balancer.Get(outlierdetection.Name)
		if odBuilder == nil {
			b.logger.Errorf("%q LB policy is needed but not registered", outlierdetection.Name)
			return
		}
		odParser, ok := odBuilder.(balancer.ConfigParser)
		if !ok {
			b.logger.Errorf("%q LB policy does not implement a config parser", outlierdetection.Name)
			return
		}
		lbCfg, err := odParser.ParseConfig(b.priorities[i].mechanism.OutlierDetection)
		if err != nil {
			b.logger.Errorf("error parsing Outlier Detection config %v: %v", b.priorities[i].mechanism.OutlierDetection, err)
			return
		}
		odCfg, ok := lbCfg.(*outlierdetection.LBConfig)
		if !ok {
			b.logger.Errorf("odParser returned config with unexpected type %T: %v", lbCfg, lbCfg)
			return
		}
		b.priorities[i].mechanism.outlierDetection = *odCfg
	}
}

// createDiscoveryMechanism creates a DiscoveryMechanism for the given cluster name.
func (b *cdsBalancer) createDiscoveryMechanism(clusterName string) (DiscoveryMechanism, error) {
	state := b.clusterConfig[clusterName]
	cluster := state.Config.Cluster
	dm := DiscoveryMechanism{
		Cluster:               clusterName,
		MaxConcurrentRequests: cluster.MaxRequests,
		LoadReportingServer:   cluster.LRSServerConfig,
		TelemetryLabels:       cluster.TelemetryLabels,
	}

	switch cluster.ClusterType {
	case xdsresource.ClusterTypeEDS:
		dm.Type = DiscoveryMechanismTypeEDS
		dm.EDSServiceName = cluster.EDSServiceName
	case xdsresource.ClusterTypeLogicalDNS:
		dm.Type = DiscoveryMechanismTypeLogicalDNS
		dm.DNSHostname = cluster.DNSHostName
	}
	return dm, nil
}

// updatePriorityConfig updates the priority configuration for a given discovery mechanism.
func (b *cdsBalancer) updatePriorityConfig(dm DiscoveryMechanism) (priorityConfig, error) {
	key := discoveryMechanismToKey(dm)
	pc, ok := b.priorityConfig[key]
	if !ok {
		pc = priorityConfig{
			mechanism:    dm,
			childNameGen: newNameGenerator(b.childNameGeneratorSeqID),
		}
		b.childNameGeneratorSeqID++
	} else {
		pc.mechanism = dm
	}

	clusterRes := b.clusterConfig[dm.Cluster]

	switch dm.Type {
	case DiscoveryMechanismTypeEDS:
		if clusterRes.Config.EndpointConfig == nil {
			return priorityConfig{}, fmt.Errorf("missing endpoint config for EDS cluster %q", dm.Cluster)
		}
		if clusterRes.Config.EndpointConfig.EDSUpdate != nil {
			pc.edsResp = *clusterRes.Config.EndpointConfig.EDSUpdate
		} else {
			pc.edsResp = xdsresource.EndpointsUpdate{}
		}
	case DiscoveryMechanismTypeLogicalDNS:
		if clusterRes.Config.EndpointConfig != nil && clusterRes.Config.EndpointConfig.DNSEndpoints != nil {
			pc.endpoints = clusterRes.Config.EndpointConfig.DNSEndpoints.Endpoints
		} else {
			pc.endpoints = nil
		}
	}

	b.priorityConfig[key] = pc
	return pc, nil
}

// Handles an error in the Cluster update from the xDS resolver to stop using
// the previously seen resource. Propagates the error down to the child policy
// if one exists, and puts the channel in TRANSIENT_FAILURE.
//
// Only executed in the context of a serializer callback.
func (b *cdsBalancer) onClusterResourceError(name string, err error) {
	b.logger.Warningf("CDS watch for resource %q reported resource error", name)
	b.closeChildPolicyAndReportTF(err)
}

// ccWrapper wraps the balancer.ClientConn passed to the CDS balancer at
// creation and intercepts the NewSubConn() and UpdateAddresses() call from the
// child policy to add security configuration required by xDS credentials.
//
// Other methods of the balancer.ClientConn interface are not overridden and
// hence get the original implementation.
type ccWrapper struct {
	balancer.ClientConn

	xdsHIPtr *unsafe.Pointer
}

// NewSubConn intercepts NewSubConn() calls from the child policy and adds an
// address attribute which provides all information required by the xdsCreds
// handshaker to perform the TLS handshake.
func (ccw *ccWrapper) NewSubConn(addrs []resolver.Address, opts balancer.NewSubConnOptions) (balancer.SubConn, error) {
	newAddrs := make([]resolver.Address, len(addrs))
	for i, addr := range addrs {
		newAddrs[i] = xdsinternal.SetHandshakeInfo(addr, ccw.xdsHIPtr)
	}

	// No need to override opts.StateListener; just forward all calls to the
	// child that created the SubConn.
	return ccw.ClientConn.NewSubConn(newAddrs, opts)
}

func (ccw *ccWrapper) UpdateAddresses(sc balancer.SubConn, addrs []resolver.Address) {
	newAddrs := make([]resolver.Address, len(addrs))
	for i, addr := range addrs {
		newAddrs[i] = xdsinternal.SetHandshakeInfo(addr, ccw.xdsHIPtr)
	}
	ccw.ClientConn.UpdateAddresses(sc, newAddrs)
}

// systemRootCertsProvider implements a certprovider.Provider that returns the
// system default root certificates for validation.
type systemRootCertsProvider struct{}

func (systemRootCertsProvider) Close() {}

func (systemRootCertsProvider) KeyMaterial(context.Context) (*certprovider.KeyMaterial, error) {
	rootCAs, err := x509SystemCertPoolFunc()
	if err != nil {
		return nil, err
	}
	return &certprovider.KeyMaterial{
		Roots: rootCAs,
	}, nil
}
