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

const cdsName = "cds_experimental"

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
		// Shouldn't happen, registered through imported Priority builder. Still,
		// defensive programming.
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
		clusterConfigs:    make(map[string]*xdsresource.ClusterResult),
		priorityConfigs:   make(map[string]priorityConfig),
	}
	b.logger = prefixLogger(b)
	b.ccw = &ccWrapper{
		ClientConn: cc,
		xdsHIPtr:   b.xdsHIPtr,
		logger:     b.logger,
	}
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
	ClusterName string `json:"cluster"`
	IsDynamic   bool   `json:"isDynamic"`
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

// cdsBalancer implements a CDS based LB policy. It instantiates a
// cluster_resolver balancer to further resolve the serviceName received from
// CDS, into localities and endpoints. Implements the balancer.Balancer
// interface which is exposed to gRPC and implements the balancer.ClientConn
// interface which is exposed to the cluster_resolver balancer.
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
	serializer        *grpcsync.CallbackSerializer          // Serializes updates from gRPC and xDS client.
	serializerCancel  context.CancelFunc                    // Stops the above serializer.
	childLB           balancer.Balancer                     // Child policy, built upon resolution of the cluster graph.
	xdsClient         xdsclient.XDSClient                   // xDS client to watch Cluster resources.
	clusterConfigs    map[string]*xdsresource.ClusterResult // Map of cluster name to the last received result for that cluster.
	priorityConfigs   map[string]priorityConfig             // Map of priority config for each leaf cluster. Key is host name i.e. EDSServiceName(or clusterName if not present) or DNSHostName.
	lbCfg             *lbConfig                             // Current load balancing configuration.
	priorities        []priorityConfig                      // List of priorities in the order.
	unsubscribe       func()                                // Function to unsubscribe to a cluster in case of dynamic clusters.
	clusterSubscriber xdsdepmgr.ClusterSubscriber           // To subscribe to dynamic cluster resource.
	xdsLBPolicy       internalserviceconfig.BalancerConfig  // Stores the locality and endpoint picking policy.
	serviceConfig     *serviceconfig.ParseResult
	attributes        *attributes.Attributes // Attributes from resolver state.
	// Each new leaf cluster needs a child name generator to reuse child policy
	// names. But to make sure the names across leaf clusters doesn't conflict,
	// we need a seq ID. This ID is incremented for each new cluster.
	childNameGeneratorSeqID uint64

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

// UpdateClientConnState receives the serviceConfig, xdsConfig,
// ClusterSubscriber and the xdsClient object from the xdsResolver.
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

	xdsConfig := xdsresource.XDSConfigFromResolverState(state.ResolverState)
	if xdsConfig == nil {
		b.logger.Warningf("Received balancer config with no xDS config")
		return balancer.ErrBadResolverState
	}
	b.clusterConfigs = xdsConfig.Clusters
	b.clusterSubscriber = xdsdepmgr.XDSClusterSubscriberFromResolverState(state.ResolverState)
	if b.clusterSubscriber == nil {
		b.logger.Warningf("Received balancer config with no cluster subscriber")
		return balancer.ErrBadResolverState
	}
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

	b.lbCfg = lbCfg
	b.serviceConfig = state.ResolverState.ServiceConfig
	b.attributes = state.ResolverState.Attributes

	// Handle the update in a blocking fashion.
	errCh := make(chan error, 1)
	callback := func(context.Context) {
		b.handleXDSConfigUpdate()
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

// handleXDSConfigUpdate processes the update from the xDS resolver.
func (b *cdsBalancer) handleXDSConfigUpdate() {
	clusterName := b.lbCfg.ClusterName

	// If the cluster is dynamic and we dont have a subscription yet, create
	// one.
	if b.lbCfg.IsDynamic && b.unsubscribe == nil {
		b.unsubscribe = b.clusterSubscriber.SubscribeToCluster(clusterName)
		return
	}

	clusterUpdate, ok := b.clusterConfigs[clusterName]

	// If the cluster is not found in the config, check if it is a dynamic
	// cluster. There is a possibility to get XDSConfig without the cluster
	// update incase of dynamic clusters. This should never happen for static
	// clusters.
	if !ok {
		if b.lbCfg.IsDynamic {
			return
		}
		b.onClusterError(clusterName, b.annotateErrorWithNodeID(fmt.Errorf("did not find the static cluster in xdsConfig")))
		return
	}

	// If the cluster resource has an error, report transient failure.
	if clusterUpdate.Err != nil {
		b.onClusterResourceError(clusterName, clusterUpdate.Err)
		return
	}

	if err := b.handleSecurityConfig(clusterUpdate.Config.Cluster.SecurityCfg); err != nil {
		// If the security config is invalid, for example, if the provider
		// instance is not found in the bootstrap config, we need to put the
		// channel in transient failure.
		b.onClusterError(b.lbCfg.ClusterName, b.annotateErrorWithNodeID(fmt.Errorf("received Cluster resource that contains invalid security config: %v", err)))
		return
	}
	b.handleClusterUpdate()
}

// Handles a good XDSConfig update from the xDS resolver. Builds the child
// policy config and pushes it down.
//
// Only executed in the context of a serializer callback.
func (b *cdsBalancer) handleClusterUpdate() {
	clusterName := b.lbCfg.ClusterName
	clusterConfig := b.clusterConfigs[clusterName].Config

	var newPriorities []priorityConfig

	switch clusterConfig.Cluster.ClusterType {
	case xdsresource.ClusterTypeEDS, xdsresource.ClusterTypeLogicalDNS:
		p, err := b.updatePriorityConfig(clusterName, &clusterConfig)
		if err != nil {
			b.logger.Warningf("failed to update priority for cluster %q: %v", clusterName, err)
			return
		}
		newPriorities = append(newPriorities, p)

	case xdsresource.ClusterTypeAggregate:
		for _, leaf := range clusterConfig.AggregateConfig.LeafClusters {
			if _, ok := b.clusterConfigs[leaf]; ok {
				if b.clusterConfigs[leaf].Err != nil {
					b.logger.Warningf("skipping leaf cluster %q for aggregate cluster %q due to error: %v", leaf, clusterName, b.clusterConfigs[leaf].Err)
					continue
				}
				// Only consider EDS and LogicalDNS leaf clusters
				p, err := b.updatePriorityConfig(leaf, &b.clusterConfigs[leaf].Config)
				if err != nil {
					b.logger.Warningf("failed to update priority for cluster %q: %v", clusterName, err)
					return
				}
				newPriorities = append(newPriorities, p)
			}
		}
	}
	b.priorities = newPriorities

	b.updateOutlierDetectionAndTelemetry()

	// The LB policy is configured by the root cluster.
	if err := json.Unmarshal(b.clusterConfigs[b.lbCfg.ClusterName].Config.Cluster.LBPolicy, &b.xdsLBPolicy); err != nil {
		b.logger.Errorf("cds_balancer: error unmarshalling xDS LB Policy: %v", err)
		return
	}
	b.updateChildConfig()
}

// updateChildConfig builds child policy configuration using endpoint addresses
// returned by the resource resolver and child policy configuration.
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

	for i := range endpoints {
		for j := range endpoints[i].Addresses {
			addr := endpoints[i].Addresses[j]
			addr.BalancerAttributes = endpoints[i].Attributes
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
			ServiceConfig: b.serviceConfig,
			Attributes:    b.attributes,
		},
		BalancerConfig: childCfg,
	}); err != nil {
		b.logger.Warningf("Failed to push config to child policy: %v", err)
	}
}

// updatePriorityConfig updates the priority config for a given EDS/DNS cluster.
// If the priority config for the cluster doesn't exist, it creates a new one.
// It returns the updated/created priority config.
func (b *cdsBalancer) updatePriorityConfig(clusterName string, clusterConfig *xdsresource.ClusterConfig) (priorityConfig, error) {
	if clusterConfig.EndpointConfig == nil {
		return priorityConfig{}, fmt.Errorf("missing endpoint config for EDS cluster %q", clusterName)
	}
	update := *clusterConfig.Cluster
	name := getHostName(clusterName, update)
	pc, ok := b.priorityConfigs[name]
	// If this is the first time we are seeing this cluster, we need to create a
	// new priority config for it. Otherwise, we just update the existing
	// priority config with the new cluster update and endpoints.
	if !ok {
		pc.childNameGen = newNameGenerator(b.childNameGeneratorSeqID)
		// Increment the seq ID for the next new cluster. This is done to make
		// sure that the child policy names generated for different clusters
		// don't conflict with each other.
		b.childNameGeneratorSeqID++
	}
	pc.clusterName = clusterName
	pc.clusterUpdate = update

	// Depending on the cluster type, we update the endpoints or edsResp in the
	// priority config.
	switch update.ClusterType {
	case xdsresource.ClusterTypeEDS:
		pc.edsResp = *clusterConfig.EndpointConfig.EDSUpdate
	case xdsresource.ClusterTypeLogicalDNS:
		pc.endpoints = clusterConfig.EndpointConfig.DNSEndpoints.Endpoints
	}

	// Update the priority config in the map and return it.
	b.priorityConfigs[name] = pc
	return pc, nil
}

// updateOutlierDetectionAndTelemetry updates Outlier Detection configs and
// Telemetry Labels for all priorities.
func (b *cdsBalancer) updateOutlierDetectionAndTelemetry() {
	for i := range b.priorities {
		clusterName := b.priorities[i].clusterName
		// Update Telemetry Labels.
		b.priorities[i].clusterUpdate.TelemetryLabels = b.clusterConfigs[clusterName].Config.Cluster.TelemetryLabels
		// Update Outlier Detection Config.
		odJSON := b.clusterConfigs[clusterName].Config.Cluster.OutlierDetection
		if odJSON == nil {
			odJSON = json.RawMessage(`{}`)
		}
		b.priorities[i].clusterUpdate.OutlierDetection = odJSON
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
		lbCfg, err := odParser.ParseConfig(odJSON)
		if err != nil {
			b.logger.Errorf("error parsing Outlier Detection config %v: %v", odJSON, err)
			return
		}
		odCfg, ok := lbCfg.(*outlierdetection.LBConfig)
		if !ok {
			b.logger.Errorf("odParser returned config with unexpected type %T: %v", lbCfg, lbCfg)
			return
		}
		b.priorities[i].outlierDetection = *odCfg
	}
}

// ResolverError handles errors reported by the xdsResolver.
func (b *cdsBalancer) ResolverError(err error) {
	b.serializer.TrySchedule(func(context.Context) {
		// Missing Listener or RouteConfiguration on the management server
		// results in a 'resource not found' error from the xDS resolver. In
		// these cases, we should report transient failure.
		if xdsresource.ErrType(err) == xdsresource.ErrorTypeResourceNotFound {
			b.closeChildPolicyAndReportTF(err)
			return
		}
		var root string
		if b.lbCfg != nil {
			root = b.lbCfg.ClusterName
		}
		b.onClusterError(root, err)
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

// Handles an ambient error Cluster update from the xDS client to not stop
// using the previously seen resource.
//
// Only executed in the context of a serializer callback.
func (b *cdsBalancer) onClusterAmbientError(name string, err error) {
	b.logger.Warningf("Cluster resource %q received ambient error update: %v", name, err)

	if xdsresource.ErrType(err) != xdsresource.ErrorTypeConnection && b.childLB != nil {
		// Connection errors will be sent to the child balancers directly.
		// There's no need to forward them.
		b.childLB.ResolverError(err)
	}
}

// Handles an error Cluster update from the xDS client to stop using the
// previously seen resource. Propagates the error down to the child policy
// if one exists, and puts the channel in TRANSIENT_FAILURE.
//
// Only executed in the context of a serializer callback.
func (b *cdsBalancer) onClusterResourceError(name string, err error) {
	b.logger.Warningf("CDS watch for resource %q reported resource error", name)
	b.closeChildPolicyAndReportTF(err)
}

func (b *cdsBalancer) onClusterError(name string, err error) {
	if b.childLB != nil {
		b.onClusterAmbientError(name, err)
	} else {
		b.onClusterResourceError(name, err)
	}
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
	logger   *grpclog.PrefixLogger
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

func (ccw *ccWrapper) UpdateAddresses(sc balancer.SubConn, _ []resolver.Address) {
	ccw.logger.Errorf("UpdateAddresses(%v) called unexpectedly", sc)
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
