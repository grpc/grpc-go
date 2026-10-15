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
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpclog"
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
	// newChildBalancer is a helper function to build a new child balancer
	// and its config parser, and will be overridden in unittests.
	newChildBalancer = func(name string, cc balancer.ClientConn, opts balancer.BuildOptions) (balancer.Balancer, balancer.ConfigParser, error) {
		builder := balancer.Get(name)
		if builder == nil {
			return nil, nil, fmt.Errorf("no balancer builder with name %q", name)
		}
		parser, ok := builder.(balancer.ConfigParser)
		if !ok {
			return nil, nil, fmt.Errorf("balancer builder for %q does not implement ConfigParser", name)
		}
		// We directly pass the parent clientConn to the underlying child
		// balancer because the cdsBalancer does not deal with subConns.
		return builder.Build(cc, opts), parser, nil
	}
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
	b := &cdsBalancer{
		bOpts:          opts,
		clusterConfigs: make(map[string]*xdsresource.ClusterResult),
		leafConfigs:    make(map[string]*leafClusterConfig),
		cc:             cc,
	}
	b.logger = prefixLogger(b)
	b.logger.Infof("Created")
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

// cdsBalancer implements a CDS based LB policy. It builds and manages a child
// LB policy using the cluster and endpoint configuration received in XDSConfig.
//
// Per gRFC A75:
//   - For a leaf (EDS or LOGICAL_DNS) cluster, the child policy is
//     outlier_detection -> cluster_impl -> priority -> xDS LB policy.
//   - For an aggregate cluster, the child policy is a priority policy with one
//     cds child policy per underlying leaf cluster.
type cdsBalancer struct {
	// The following fields are initialized at build time and are either
	// read-only after that or provide their own synchronization, and therefore
	// do not need to be guarded by a mutex.
	cc     balancer.ClientConn   // ClientConn interface passed to child LB.
	bOpts  balancer.BuildOptions // BuildOptions passed to child LB.
	logger *grpclog.PrefixLogger // Prefix logger for all logging.

	// All fields below are accessed only from methods implementing the
	// balancer.Balancer interface. Since gRPC guarantees that these methods are
	// never invoked concurrently, no additional synchronization is required to
	// protect access to these fields.
	xdsClient         xdsclient.XDSClient
	childLB           balancer.Balancer                     // Child policy, built upon resolution of the cluster graph.
	childLBName       string                                // Name of the child policy.
	childConfigParser balancer.ConfigParser                 // Config parser for child policy.
	clusterConfigs    map[string]*xdsresource.ClusterResult // Cluster name to the last received result for that cluster.
	leafConfigs       map[string]*leafClusterConfig         // Hostname to config for that leaf cluster.
	lbCfg             *lbConfig                             // Current load balancing configuration.
	leafCluster       *leafClusterConfig                    // Config of the current leaf cluster; nil for aggregate clusters.
	unsubscribe       func()                                // For dynamic cluster unsubscription.
	isSubscribed      bool                                  // True if a dynamic cluster has been subscribed to.
	clusterSubscriber xdsdepmgr.ClusterSubscriber           // To subscribe to dynamic cluster resource.
	attributes        *attributes.Attributes                // Attributes from resolver state.
	// Each new leaf cluster needs a child name generator to reuse child policy
	// names. But to make sure the names across leaf clusters doesn't conflict,
	// we need a seq ID. This ID is incremented for each new cluster.
	childNameGeneratorSeqID uint64
}

// UpdateClientConnState receives the xdsConfig, ClusterSubscriber and the
// xdsClient object from the xdsResolver. If an error is encountered, the
// parent (clustermanager) sets the corresponding cluster’s picker to
// transient_failure. Otherwise, the received configuration is processed and
// forwarded to the appropriate child policy.
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
	b.attributes = state.ResolverState.Attributes
	return b.handleXDSConfigUpdate()
}

// handleXDSConfigUpdate processes the XDSConfig update from the xDS resolver.
func (b *cdsBalancer) handleXDSConfigUpdate() error {
	clusterName := b.lbCfg.ClusterName

	// If the cluster is dynamic and we dont have a subscription yet, create
	// one.
	if b.lbCfg.IsDynamic && !b.isSubscribed {
		b.unsubscribe = b.clusterSubscriber.SubscribeToCluster(clusterName)
		b.isSubscribed = true
		return nil
	}

	clusterUpdate, ok := b.clusterConfigs[clusterName]
	if !ok {
		// If the cluster is missing from the config, check if it is dynamic.
		// For dynamic clusters, the xDS config may be updated before the
		// corresponding cluster resource is received. This should never occur
		// for static clusters.
		if b.lbCfg.IsDynamic {
			return nil
		}
		return b.annotateErrorWithNodeID(fmt.Errorf("did not find the cluster %q in XDSConfig", clusterName))
	}
	// If the cluster resource has an error, return the error.
	if clusterUpdate.Err != nil {
		return clusterUpdate.Err
	}
	return b.handleClusterUpdate()
}

// handleClusterUpdate handles a good XDSConfig update from the xDS resolver.
// Builds the child policy config and pushes it down.
func (b *cdsBalancer) handleClusterUpdate() error {
	clusterName := b.lbCfg.ClusterName
	clusterConfig := b.clusterConfigs[clusterName].Config

	switch clusterConfig.Cluster.ClusterType {
	case xdsresource.ClusterTypeEDS, xdsresource.ClusterTypeLogicalDNS:
		leaf := b.updateLeafClusterConfig(clusterName, &clusterConfig)
		lbPolicy, err := b.leafLBPolicy(clusterConfig.Cluster)
		if err != nil {
			return b.annotateErrorWithNodeID(err)
		}
		leaf.lbPolicy = lbPolicy
		b.leafCluster = leaf
	case xdsresource.ClusterTypeAggregate:
		// Leaf clusters of an aggregate cluster are handled by their own
		// child cds policies.
		b.leafCluster = nil
	}

	if err := b.updateChildConfig(); err != nil {
		return b.annotateErrorWithNodeID(err)
	}
	return nil
}

// leafLBPolicy returns the locality and endpoint picking policy for the given
// leaf cluster.
//
// Per gRFC A75, a leaf cluster uses the LB policy configured in the leaf
// cluster itself, and the LB policy of the parent aggregate cluster (if any)
// is ignored. As a temporary migration mechanism, if
// GRPC_XDS_AGGREGATE_CLUSTER_BACKWARD_COMPAT is enabled, the LB policy
// configured in the parent aggregate cluster is used instead.
func (b *cdsBalancer) leafLBPolicy(cluster *xdsresource.ClusterUpdate) (*internalserviceconfig.BalancerConfig, error) {
	lbPolicyJSON := cluster.LBPolicy
	// TODO: Remove this block once GRPC_XDS_AGGREGATE_CLUSTER_BACKWARD_COMPAT
	// is removed.
	if envconfig.XDSAggregateClusterBackwardCompat {
		if aggName, ok := aggregateClusterNameFromAttributes(b.attributes); ok {
			aggCluster, ok := b.clusterConfigs[aggName]
			if !ok || aggCluster.Err != nil {
				// Should never happen: the cds policy of the aggregate cluster
				// only pushes configuration to its children when its own
				// cluster config is present and valid.
				return nil, fmt.Errorf("xDS config has no valid entry for aggregate cluster %q", aggName)
			}
			lbPolicyJSON = aggCluster.Config.Cluster.LBPolicy
		}
	}
	lbPolicy := &internalserviceconfig.BalancerConfig{}
	if err := json.Unmarshal(lbPolicyJSON, lbPolicy); err != nil {
		return nil, fmt.Errorf("error unmarshalling xDS LB Policy for cluster %q: %v", cluster.ClusterName, err)
	}
	return lbPolicy, nil
}

// aggregateClusterNameKey is the resolver state attributes key used by the cds
// policy of an aggregate cluster to pass its cluster name to the cds policies
// of its leaf clusters. It is used only to implement
// GRPC_XDS_AGGREGATE_CLUSTER_BACKWARD_COMPAT.
//
// TODO: Remove once GRPC_XDS_AGGREGATE_CLUSTER_BACKWARD_COMPAT is removed.
type aggregateClusterNameKey struct{}

// setAggregateClusterName returns a copy of attrs with the given aggregate
// cluster name set.
//
// TODO: Remove once GRPC_XDS_AGGREGATE_CLUSTER_BACKWARD_COMPAT is removed.
func setAggregateClusterName(attrs *attributes.Attributes, name string) *attributes.Attributes {
	return attrs.WithValue(aggregateClusterNameKey{}, name)
}

// aggregateClusterNameFromAttributes returns the aggregate cluster name stored
// in attrs, if any.
//
// TODO: Remove once GRPC_XDS_AGGREGATE_CLUSTER_BACKWARD_COMPAT is removed.
func aggregateClusterNameFromAttributes(attrs *attributes.Attributes) (string, bool) {
	name, ok := attrs.Value(aggregateClusterNameKey{}).(string)
	return name, ok
}

// updateChildConfig builds child policy configuration using endpoint addresses
// returned from the XDSConfig and child policy configuration.
//
// A child policy is created if one doesn't already exist. The newly built
// configuration is then pushed to the child policy.
func (b *cdsBalancer) updateChildConfig() error {
	clusterName := b.lbCfg.ClusterName
	clusterConfig := b.clusterConfigs[clusterName].Config
	isAggregate := clusterConfig.Cluster.ClusterType == xdsresource.ClusterTypeAggregate

	var childPolicyName string
	if isAggregate {
		childPolicyName = priority.Name
	} else {
		childPolicyName = outlierdetection.Name
	}

	if b.childLB != nil && b.childLBName != childPolicyName {
		b.closeChildPolicy()
	}

	if b.childLB == nil {
		childLB, parser, err := newChildBalancer(childPolicyName, b.cc, b.bOpts)
		if err != nil {
			return fmt.Errorf("xds: failed to create child policy of type %s: %v", childPolicyName, err)
		}
		b.childLB = childLB
		b.childLBName = childPolicyName
		b.childConfigParser = parser
	}

	var childCfgBytes []byte
	var endpoints []resolver.Endpoint
	var err error
	attrs := b.attributes

	if isAggregate {
		// The child cds policies build their own configuration and
		// endpoints from the XDSConfig passed down in the resolver state
		// attributes, so no endpoints are passed down here.
		childCfgBytes, err = buildAggregateClusterConfigJSON(clusterConfig.AggregateConfig.LeafClusters)
		// TODO: Remove once GRPC_XDS_AGGREGATE_CLUSTER_BACKWARD_COMPAT is
		// removed.
		if envconfig.XDSAggregateClusterBackwardCompat {
			attrs = setAggregateClusterName(attrs, clusterName)
		}
	} else {
		childCfgBytes, endpoints, err = buildLeafClusterConfigJSON(b.leafCluster, b.leafCluster.lbPolicy)
	}
	if err != nil {
		return fmt.Errorf("xds: failed to build child policy config: %v", err)
	}

	childCfg, err := b.childConfigParser.ParseConfig(childCfgBytes)
	if err != nil {
		return fmt.Errorf("xds: failed to parse child policy config. This should never happen because the config was generated: %v", err)
	}
	if b.logger.V(2) {
		b.logger.Infof("Built child policy config: %s", pretty.ToJSON(childCfg))
	}

	for i := range endpoints {
		for j := range endpoints[i].Addresses {
			addr := endpoints[i].Addresses[j]
			addr.BalancerAttributes = endpoints[i].Attributes
			// BalancerAttributes are used for the following:
			// * Authority Override.
			// * grpc.lb.backend_service metric label propagation.
			// See https://github.com/grpc/grpc-go/issues/6472
			endpoints[i].Addresses[j] = addr
		}
	}
	if err := b.childLB.UpdateClientConnState(balancer.ClientConnState{
		ResolverState: resolver.State{
			Endpoints:  endpoints,
			Attributes: attrs,
		},
		BalancerConfig: childCfg,
	}); err != nil {
		return fmt.Errorf("xds: failed to push config to child policy: %v", err)
	}
	return nil
}

// updateLeafClusterConfig updates the leaf cluster configuration for the
// specified EDS or DNS cluster, creating it if it does not already exist.
func (b *cdsBalancer) updateLeafClusterConfig(clusterName string, clusterConfig *xdsresource.ClusterConfig) *leafClusterConfig {
	name := hostName(clusterName, *clusterConfig.Cluster)
	leaf, ok := b.leafConfigs[name]
	if !ok {
		leaf = &leafClusterConfig{
			childNameGen: newNameGenerator(b.childNameGeneratorSeqID),
		}
		b.leafConfigs[name] = leaf
		// Increment the seq ID for the next new cluster. This is done to make
		// sure that the child policy names generated for different clusters
		// don't conflict with each other.
		b.childNameGeneratorSeqID++
	}
	leaf.clusterConfig = clusterConfig
	return leaf
}

// ResolverError handles errors reported by the xdsResolver.
func (b *cdsBalancer) ResolverError(err error) {
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
}

// UpdateSubConnState handles subConn updates from gRPC.
func (b *cdsBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	b.logger.Errorf("UpdateSubConnState(%v, %+v) called unexpectedly", sc, state)
}

// Closes the child policy, if it exists, and resets child policy fields.
func (b *cdsBalancer) closeChildPolicy() {
	if b.childLB != nil {
		b.childLB.Close()
		b.childLB = nil
		b.childLBName = ""
		b.childConfigParser = nil
	}
}

// closeChildPolicyAndReportTF closes the child policy, if it exists, and
// updates the connectivity state of the channel to TransientFailure with an
// error picker.
func (b *cdsBalancer) closeChildPolicyAndReportTF(err error) {
	b.closeChildPolicy()
	b.cc.UpdateState(balancer.State{
		ConnectivityState: connectivity.TransientFailure,
		Picker:            base.NewErrPicker(err),
	})
}

// Close closes the child policy, unsubscribes to the dynamic cluster, and
// closes the cdsBalancer.
func (b *cdsBalancer) Close() {
	b.closeChildPolicy()
	if b.unsubscribe != nil {
		b.unsubscribe()
	}
	b.logger.Infof("Shutdown")
}

func (b *cdsBalancer) ExitIdle() {
	if b.childLB == nil {
		b.logger.Warningf("Received ExitIdle with no child policy")
		return
	}
	// This implementation assumes the child balancer supports
	// ExitIdle (but still checks for the interface's existence to
	// avoid a panic if not). If the child does not, no subconns
	// will be connected.
	b.childLB.ExitIdle()
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

// onClusterAmbientError handles an ambient error, if a childLB already has a
// good update, it should continue using that.
func (b *cdsBalancer) onClusterAmbientError(name string, err error) {
	b.logger.Warningf("Cluster resource %q received ambient error update: %v", name, err)

	if xdsresource.ErrType(err) != xdsresource.ErrorTypeConnection && b.childLB != nil {
		// Connection errors will be sent to the child balancers directly.
		// There's no need to forward them.
		b.childLB.ResolverError(err)
	}
}

// onClusterResourceError handles errors to stop using the previously seen
// resource. Propagates the error down to the child policy if one exists, and
// puts the channel in TRANSIENT_FAILURE.
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
