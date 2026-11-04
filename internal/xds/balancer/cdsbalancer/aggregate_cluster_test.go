/*
 * Copyright 2021 gRPC authors.
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

package cdsbalancer

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer/ringhash"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/pretty"
	iringhash "google.golang.org/grpc/internal/ringhash"
	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/balancer/priority"
	"google.golang.org/grpc/internal/xds/balancer/wrrlocality"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	_ "google.golang.org/grpc/balancer/pickfirst"
	_ "google.golang.org/grpc/internal/xds/httpfilter/router" // Register the router filter.
	_ "google.golang.org/grpc/internal/xds/resolver"          // Register the xds resolver
)

// makeAggregateClusterResource returns an aggregate cluster resource with the
// given name and list of child names.
func makeAggregateClusterResource(name string, childNames []string) *v3clusterpb.Cluster {
	return e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: name,
		Type:        e2e.ClusterTypeAggregate,
		ChildNames:  childNames,
	})
}

// makeLogicalDNSClusterResource returns a LOGICAL_DNS cluster resource with the
// given name and given DNS host and port.
func makeLogicalDNSClusterResource(name, dnsHost string, dnsPort uint32) *v3clusterpb.Cluster {
	return e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: name,
		Type:        e2e.ClusterTypeLogicalDNS,
		DNSHostName: dnsHost,
		DNSPort:     dnsPort,
	})
}

// aggregateClusterPriorityConfig returns the expected priority LB policy
// configuration for an aggregate cluster with the given leaf clusters. Per gRFC
// A75, each child of the priority policy is a cds policy for a leaf cluster.
func aggregateClusterPriorityConfig(leafClusterNames ...string) *priority.LBConfig {
	cfg := &priority.LBConfig{Children: make(map[string]*priority.Child)}
	for _, name := range leafClusterNames {
		cfg.Priorities = append(cfg.Priorities, name)
		cfg.Children[name] = &priority.Child{
			Config: &iserviceconfig.BalancerConfig{
				Name:   cdsName,
				Config: &lbConfig{ClusterName: name},
			},
		}
	}
	return cfg
}

// verifyDNSResolution verifies that the DNS resolver is started for the expected target.
func verifyDNSResolution(ctx context.Context, t *testing.T, dnsTargetCh chan resolver.Target, dnsR *manual.Resolver, host string, port uint32) {
	t.Helper()
	select {
	case <-ctx.Done():
		t.Fatal("Timeout waiting for DNS watch")
	case target := <-dnsTargetCh:
		addr := fmt.Sprintf("%s:%d", host, port)
		if target.Endpoint() != addr {
			t.Fatalf("DNS resolution started for target %q, want %q", target.Endpoint(), addr)
		}
		dnsR.UpdateState(resolver.State{
			Endpoints: []resolver.Endpoint{{Addresses: []resolver.Address{{Addr: addr}}}},
		})
	}
}

// Tests the case where the cluster resource requested is a leaf cluster. The
// management server sends two updates for the same leaf cluster resource. The
// test verifies that the load balancing configuration pushed to the top-level
// LB policy contains the expected configuration corresponding to the leaf
// cluster, on both occasions.
func (s) TestAggregateClusterSuccess_LeafNode(t *testing.T) {
	tests := []struct {
		name                  string
		firstClusterResource  *v3clusterpb.Cluster
		secondClusterResource *v3clusterpb.Cluster
		wantFirstChildCfg     serviceconfig.LoadBalancingConfig
		wantSecondChildCfg    serviceconfig.LoadBalancingConfig
	}{
		{
			name:                  "eds",
			firstClusterResource:  e2e.DefaultCluster(clusterName, serviceName, e2e.SecurityLevelNone),
			secondClusterResource: e2e.DefaultCluster(clusterName, serviceName+"-new", e2e.SecurityLevelNone),
			wantFirstChildCfg:     createLeafClusterConfig(clusterName, "priority-0-0", true),
			wantSecondChildCfg:    createLeafClusterConfig(clusterName, "priority-1-0", true),
		},
		{
			name:                  "dns",
			firstClusterResource:  makeLogicalDNSClusterResource(clusterName, "dns_host", uint32(port)),
			secondClusterResource: makeLogicalDNSClusterResource(clusterName, "dns_host_new", uint32(port)),
			wantFirstChildCfg:     createLeafClusterConfig(clusterName, "priority-0", false),
			wantSecondChildCfg:    createLeafClusterConfig(clusterName, "priority-1", false),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			odStateCh := registerWrappedOutlierDetectionPolicy(ctx, t)
			mgmtServer, nodeID, _ := setupWithManagementServer(t, nil, nil)

			// Push the first cluster resource through the management server and
			// verify the configuration pushed to the child policy.
			resources := e2e.UpdateOptions{
				NodeID:         nodeID,
				Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
				Routes:         []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
				Clusters:       []*v3clusterpb.Cluster{test.firstClusterResource},
				Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, host, []uint32{port})},
				SkipValidation: true,
			}
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatal(err)
			}
			if err := waitForLeafClusterState(ctx, odStateCh, test.wantFirstChildCfg, ""); err != nil {
				t.Fatal(err)
			}

			// Push the second cluster resource through the management server and
			// verify the configuration pushed to the child policy.
			resources.Clusters[0] = test.secondClusterResource
			resources.Endpoints[0] = e2e.DefaultEndpoint(serviceName+"-new", host, []uint32{port})
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatal(err)
			}
			if err := waitForLeafClusterState(ctx, odStateCh, test.wantSecondChildCfg, ""); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// emptyEndpointResource returns an EDS resource with no endpoints for the given
// service name. A leaf cluster using it moves to TRANSIENT_FAILURE right away,
// which makes the priority policy of a parent aggregate cluster fail over to
// the next leaf cluster.
func emptyEndpointResource(serviceName string) *v3endpointpb.ClusterLoadAssignment {
	return e2e.EndpointResourceWithOptions(e2e.EndpointOptions{ClusterName: serviceName})
}

// Tests the case where the cluster resource requested is an aggregate cluster
// root pointing to two child clusters, one of type EDS and the other of type
// LogicalDNS. Verifies that the priority config is pushed only when all child
// clusters are resolved, and that replacing the LogicalDNS cluster creates a
// new cds policy, which builds its config and endpoints from the new DNS
// resolution.
//
// The EDS cluster has no endpoints, which forces an immediate failover to the
// LogicalDNS cluster, since the priority policy only creates the cds policy of
// a lower-priority cluster after failing over to it.
func (s) TestAggregateClusterSuccess_ThenUpdateChildClusters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	dnsTargetCh, dnsR := setupDNS(t)
	lbCfgCh, _, _, _ := registerWrappedPriorityPolicy(ctx, t)
	odStateCh := registerWrappedOutlierDetectionPolicy(ctx, t)
	mgmtServer, nodeID, _ := setupWithManagementServer(t, nil, nil)

	// Configure the management server with the aggregate cluster resource
	// pointing to two child clusters, one EDS and one LogicalDNS. Include the
	// resource corresponding to the EDS cluster here, but don't include
	// resource corresponding to the LogicalDNS cluster yet. The EDS cluster has
	// no endpoints, so that the priority policy fails over to the LogicalDNS
	// cluster and creates its cds policy.
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterName, []string{edsClusterName, dnsClusterName}),
			e2e.DefaultCluster(edsClusterName, serviceName, e2e.SecurityLevelNone),
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{emptyEndpointResource(serviceName)},
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that no configuration is pushed to the child policy yet, because
	// not all clusters making up the aggregate cluster have been resolved yet.
	select {
	case cfg := <-lbCfgCh:
		t.Fatalf("Child policy received configuration when not expected to: %s", pretty.ToJSON(cfg))
	case <-time.After(defaultTestShortTimeout):
	}

	// Now configure the LogicalDNS cluster in the management server. This
	// should result in configuration being pushed down to the child policy.
	resources.Clusters = append(resources.Clusters, makeLogicalDNSClusterResource(dnsClusterName, dnsHostName, dnsPort))
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	verifyDNSResolution(ctx, t, dnsTargetCh, dnsR, dnsHostName, dnsPort)

	wantChildCfg := aggregateClusterPriorityConfig(edsClusterName, dnsClusterName)
	if err := waitForLoadBalancingConfig(ctx, lbCfgCh, wantChildCfg); err != nil {
		t.Fatal(err)
	}
	// Child policy names are scoped to each cds policy, so the single priority
	// of the LogicalDNS cluster is named "priority-0".
	wantDNSLeafCfg := createLeafClusterConfig(dnsClusterName, "priority-0", false)
	wantDNSAddr := fmt.Sprintf("%s:%d", dnsHostName, dnsPort)
	if err := waitForLeafClusterState(ctx, odStateCh, wantDNSLeafCfg, wantDNSAddr); err != nil {
		t.Fatal(err)
	}

	const dnsClusterNameNew = dnsClusterName + "-new"
	const dnsHostNameNew = dnsHostName + "-new"
	resources = e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterName, []string{edsClusterName, dnsClusterNameNew}),
			e2e.DefaultCluster(edsClusterName, serviceName, e2e.SecurityLevelNone),
			makeLogicalDNSClusterResource(dnsClusterName, dnsHostName, dnsPort),
			makeLogicalDNSClusterResource(dnsClusterNameNew, dnsHostNameNew, dnsPort),
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{emptyEndpointResource(serviceName)},
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	verifyDNSResolution(ctx, t, dnsTargetCh, dnsR, dnsHostNameNew, dnsPort)

	wantChildCfg = aggregateClusterPriorityConfig(edsClusterName, dnsClusterNameNew)
	if err := waitForLoadBalancingConfig(ctx, lbCfgCh, wantChildCfg); err != nil {
		t.Fatal(err)
	}
	// A new cds policy is created for the new LogicalDNS cluster, which
	// builds its configuration and endpoints from the new DNS resolution.
	wantDNSLeafCfgNew := createLeafClusterConfig(dnsClusterNameNew, "priority-0", false)
	wantDNSAddrNew := fmt.Sprintf("%s:%d", dnsHostNameNew, dnsPort)
	if err := waitForLeafClusterState(ctx, odStateCh, wantDNSLeafCfgNew, wantDNSAddrNew); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where the cluster resource requested is an aggregate cluster
// root pointing to an EDS and a LogicalDNS child cluster, which is then
// changed to a leaf EDS cluster. Verifies the config and endpoints built by
// the cds policies of both leaf clusters, and then by the root cds policy.
//
// The EDS child cluster has no endpoints, which forces an immediate failover
// to the LogicalDNS cluster, since the priority policy only creates the cds
// policy of a lower-priority cluster after failing over to it. It uses a
// separate service name, so that the leaf EDS cluster with endpoints never
// sees the empty EDS resource.
func (s) TestAggregateClusterSuccess_ThenChangeRootToEDS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	dnsTargetCh, dnsR := setupDNS(t)
	lbCfgCh, _, _, _ := registerWrappedPriorityPolicy(ctx, t)
	odStateCh := registerWrappedOutlierDetectionPolicy(ctx, t)
	mgmtServer, nodeID, _ := setupWithManagementServer(t, nil, nil)

	// Service name of the EDS child cluster without endpoints.
	const emptyServiceName = serviceName + "-empty"

	// Configure the management server with the aggregate cluster resource
	// pointing to two child clusters. The EDS cluster has no endpoints.
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterName, []string{edsClusterName, dnsClusterName}),
			e2e.DefaultCluster(edsClusterName, emptyServiceName, e2e.SecurityLevelNone),
			makeLogicalDNSClusterResource(dnsClusterName, dnsHostName, dnsPort),
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{emptyEndpointResource(emptyServiceName)},
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	verifyDNSResolution(ctx, t, dnsTargetCh, dnsR, dnsHostName, dnsPort)

	wantChildCfg := aggregateClusterPriorityConfig(edsClusterName, dnsClusterName)
	if err := waitForLoadBalancingConfig(ctx, lbCfgCh, wantChildCfg); err != nil {
		t.Fatal(err)
	}
	wantEDSLeafCfg := createLeafClusterConfig(edsClusterName, "priority-0-0", true)
	if err := waitForLeafClusterState(ctx, odStateCh, wantEDSLeafCfg, ""); err != nil {
		t.Fatal(err)
	}
	wantDNSLeafCfg := createLeafClusterConfig(dnsClusterName, "priority-0", false)
	wantDNSAddr := fmt.Sprintf("%s:%d", dnsHostName, dnsPort)
	if err := waitForLeafClusterState(ctx, odStateCh, wantDNSLeafCfg, wantDNSAddr); err != nil {
		t.Fatal(err)
	}

	// Change the root cluster to a leaf EDS cluster, which has endpoints now.
	resources = e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
		Clusters: []*v3clusterpb.Cluster{
			e2e.DefaultCluster(clusterName, serviceName, e2e.SecurityLevelNone),
			makeLogicalDNSClusterResource(dnsClusterName, dnsHostName, dnsPort),
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, host, []uint32{port})},
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	wantLeafCfg := createLeafClusterConfig(clusterName, "priority-0-0", true)
	wantEDSAddr := fmt.Sprintf("%s:%d", host, port)
	if err := waitForLeafClusterState(ctx, odStateCh, wantLeafCfg, wantEDSAddr); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where a requested cluster resource switches from a leaf EDS
// cluster to an aggregate cluster pointing to an EDS and a LogicalDNS child
// cluster, and back. Verifies the config and endpoints of the leaf clusters in
// each phase, and that the root cds policy reuses its priority names.
//
// The EDS child cluster has no endpoints, which forces an immediate failover
// to the LogicalDNS cluster, since the priority policy only creates the cds
// policy of a lower-priority cluster after failing over to it. It uses a
// separate service name, so that the leaf EDS cluster with endpoints never
// sees the empty EDS resource.
func (s) TestAggregatedClusterSuccess_SwitchBetweenLeafAndAggregate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	dnsTargetCh, dnsR := setupDNS(t)
	lbCfgCh, _, _, _ := registerWrappedPriorityPolicy(ctx, t)
	odStateCh := registerWrappedOutlierDetectionPolicy(ctx, t)
	mgmtServer, nodeID, _ := setupWithManagementServer(t, nil, nil)

	// Service name of the EDS child cluster without endpoints.
	const emptyServiceName = serviceName + "-empty"

	// Start off with the requested cluster being a leaf EDS cluster.
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
		Clusters:  []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, serviceName, e2e.SecurityLevelNone)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, host, []uint32{port})},
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	wantLeafCfg := createLeafClusterConfig(clusterName, "priority-0-0", true)
	wantEDSAddr := fmt.Sprintf("%s:%d", host, port)
	if err := waitForLeafClusterState(ctx, odStateCh, wantLeafCfg, wantEDSAddr); err != nil {
		t.Fatal(err)
	}

	// Switch the requested cluster to be an aggregate cluster pointing to two
	// child clusters. The EDS child cluster has no endpoints.
	resources = e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterName, []string{edsClusterName, dnsClusterName}),
			e2e.DefaultCluster(edsClusterName, emptyServiceName, e2e.SecurityLevelNone),
			makeLogicalDNSClusterResource(dnsClusterName, dnsHostName, dnsPort),
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{emptyEndpointResource(emptyServiceName)},
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	verifyDNSResolution(ctx, t, dnsTargetCh, dnsR, dnsHostName, dnsPort)

	wantChildCfg := aggregateClusterPriorityConfig(edsClusterName, dnsClusterName)
	if err := waitForLoadBalancingConfig(ctx, lbCfgCh, wantChildCfg); err != nil {
		t.Fatal(err)
	}
	wantEDSLeafCfg := createLeafClusterConfig(edsClusterName, "priority-0-0", true)
	if err := waitForLeafClusterState(ctx, odStateCh, wantEDSLeafCfg, ""); err != nil {
		t.Fatal(err)
	}
	wantDNSLeafCfg := createLeafClusterConfig(dnsClusterName, "priority-0", false)
	wantDNSAddr := fmt.Sprintf("%s:%d", dnsHostName, dnsPort)
	if err := waitForLeafClusterState(ctx, odStateCh, wantDNSLeafCfg, wantDNSAddr); err != nil {
		t.Fatal(err)
	}

	// Switch the cluster back to a leaf EDS cluster, which has endpoints again.
	resources = e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
		Clusters:  []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, serviceName, e2e.SecurityLevelNone)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, host, []uint32{port})},
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	// Since the service name of the EDS cluster remains the same, the root cds
	// policy reuses the same priority name.
	if err := waitForLeafClusterState(ctx, odStateCh, wantLeafCfg, wantEDSAddr); err != nil {
		t.Fatal(err)
	}
}

// Tests the scenario where an aggregate cluster exceeds the maximum depth,
// which is 16. Verifies that the channel moves to TRANSIENT_FAILURE, and the
// error is propagated to RPC callers. The test then modifies the graph to no
// longer exceed maximum depth, but be at the maximum allowed depth, and
// verifies that an RPC can be made successfully.
func (s) TestAggregatedClusterFailure_ExceedsMaxStackDepth(t *testing.T) {
	mgmtServer, nodeID, cc := setupWithManagementServer(t, nil, nil)

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},

		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterName, []string{clusterName + "-1"}),
			makeAggregateClusterResource(clusterName+"-1", []string{clusterName + "-2"}),
			makeAggregateClusterResource(clusterName+"-2", []string{clusterName + "-3"}),
			makeAggregateClusterResource(clusterName+"-3", []string{clusterName + "-4"}),
			makeAggregateClusterResource(clusterName+"-4", []string{clusterName + "-5"}),
			makeAggregateClusterResource(clusterName+"-5", []string{clusterName + "-6"}),
			makeAggregateClusterResource(clusterName+"-6", []string{clusterName + "-7"}),
			makeAggregateClusterResource(clusterName+"-7", []string{clusterName + "-8"}),
			makeAggregateClusterResource(clusterName+"-8", []string{clusterName + "-9"}),
			makeAggregateClusterResource(clusterName+"-9", []string{clusterName + "-10"}),
			makeAggregateClusterResource(clusterName+"-10", []string{clusterName + "-11"}),
			makeAggregateClusterResource(clusterName+"-11", []string{clusterName + "-12"}),
			makeAggregateClusterResource(clusterName+"-12", []string{clusterName + "-13"}),
			makeAggregateClusterResource(clusterName+"-13", []string{clusterName + "-14"}),
			makeAggregateClusterResource(clusterName+"-14", []string{clusterName + "-15"}),
			makeAggregateClusterResource(clusterName+"-15", []string{clusterName + "-16"}),
			e2e.DefaultCluster(clusterName+"-16", serviceName, e2e.SecurityLevelNone),
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, host, []uint32{port})},
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	const wantErr = "aggregate cluster graph exceeds max depth"
	client := testgrpc.NewTestServiceClient(cc)
	_, err := client.EmptyCall(ctx, &testpb.Empty{})
	if code := status.Code(err); code != codes.Unavailable {
		t.Fatalf("EmptyCall() failed with code: %v, want %v", code, codes.Unavailable)
	}
	if err != nil && !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("EmptyCall() failed with err: %v, want err containing: %v", err, wantErr)
	}

	// Start a test service backend.
	server := stubserver.StartTestService(t, nil)
	t.Cleanup(server.Stop)

	// Update the aggregate cluster resource to no longer exceed max depth, and
	// be at the maximum depth allowed.
	resources = e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterName, []string{clusterName + "-1"}),
			makeAggregateClusterResource(clusterName+"-1", []string{clusterName + "-2"}),
			makeAggregateClusterResource(clusterName+"-2", []string{clusterName + "-3"}),
			makeAggregateClusterResource(clusterName+"-3", []string{clusterName + "-4"}),
			makeAggregateClusterResource(clusterName+"-4", []string{clusterName + "-5"}),
			makeAggregateClusterResource(clusterName+"-5", []string{clusterName + "-6"}),
			makeAggregateClusterResource(clusterName+"-6", []string{clusterName + "-7"}),
			makeAggregateClusterResource(clusterName+"-7", []string{clusterName + "-8"}),
			makeAggregateClusterResource(clusterName+"-8", []string{clusterName + "-9"}),
			makeAggregateClusterResource(clusterName+"-9", []string{clusterName + "-10"}),
			makeAggregateClusterResource(clusterName+"-10", []string{clusterName + "-11"}),
			makeAggregateClusterResource(clusterName+"-11", []string{clusterName + "-12"}),
			makeAggregateClusterResource(clusterName+"-12", []string{clusterName + "-13"}),
			makeAggregateClusterResource(clusterName+"-13", []string{clusterName + "-14"}),
			makeAggregateClusterResource(clusterName+"-14", []string{clusterName + "-15"}),
			e2e.DefaultCluster(clusterName+"-15", serviceName, e2e.SecurityLevelNone),
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, host, []uint32{testutils.ParsePort(t, server.Address)})},
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that a successful RPC can be made.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

// Tests a diamond shaped aggregate cluster (A->[B,C]; B->D; C->D). Verifies
// that the load balancing configuration pushed to the priority LB
// policy specifies cluster D only once. Also verifies that configuration is
// pushed only after all child clusters are resolved.
func (s) TestAggregatedClusterSuccess_DiamondDependency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	lbCfgCh, _, _, _ := registerWrappedPriorityPolicy(ctx, t)
	odStateCh := registerWrappedOutlierDetectionPolicy(ctx, t)
	mgmtServer, nodeID, _ := setupWithManagementServer(t, nil, nil)

	// Configure the management server with an aggregate cluster resource having
	// a diamond dependency pattern, (A->[B,C]; B->D; C->D). Includes resources
	// for cluster A, B and D, but don't include the resource for cluster C yet.
	// This will help us verify that no configuration is pushed to the child
	// policy until the whole cluster graph is resolved.
	const (
		clusterNameA = clusterName // cluster name in cds LB policy config
		clusterNameB = clusterName + "-B"
		clusterNameC = clusterName + "-C"
		clusterNameD = clusterName + "-D"
	)
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterNameA, []string{clusterNameB, clusterNameC}),
			makeAggregateClusterResource(clusterNameB, []string{clusterNameD}),
			e2e.DefaultCluster(clusterNameD, serviceName, e2e.SecurityLevelNone),
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, host, []uint32{port})},
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that no configuration is pushed to the child policy yet, because
	// not all clusters making up the aggregate cluster have been resolved yet.
	select {
	case cfg := <-lbCfgCh:
		t.Fatalf("Child policy received configuration when not expected to: %s", pretty.ToJSON(cfg))
	case <-time.After(defaultTestShortTimeout):
	}

	// Now configure the resource for cluster C in the management server,
	// thereby completing the cluster graph. This should result in configuration
	// being pushed down to the child policy.
	resources.Clusters = append(resources.Clusters, makeAggregateClusterResource(clusterNameC, []string{clusterNameD}))
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	wantChildCfg := aggregateClusterPriorityConfig(clusterNameD)
	if err := waitForLoadBalancingConfig(ctx, lbCfgCh, wantChildCfg); err != nil {
		t.Fatal(err)
	}
	wantLeafCfg := createLeafClusterConfig(clusterNameD, "priority-0-0", true)
	wantAddr := fmt.Sprintf("%s:%d", host, port)
	if err := waitForLeafClusterState(ctx, odStateCh, wantLeafCfg, wantAddr); err != nil {
		t.Fatal(err)
	}
}

// Tests the case where the aggregate cluster graph contains duplicates (A->[B,
// C]; B->[C, D]). Verifies that the priority config is pushed only after all
// child clusters are resolved, contains no duplicates, and gives cluster C a
// higher priority than cluster D.
//
// Cluster C has no endpoints, which forces an immediate failover to cluster D,
// since the priority policy only creates the cds policy of a lower-priority
// cluster after failing over to it.
func (s) TestAggregatedClusterSuccess_IgnoreDups(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	lbCfgCh, _, _, _ := registerWrappedPriorityPolicy(ctx, t)
	odStateCh := registerWrappedOutlierDetectionPolicy(ctx, t)
	mgmtServer, nodeID, _ := setupWithManagementServer(t, nil, nil)

	// Configure the management server with an aggregate cluster resource that
	// has duplicates in the graph, (A->[B, C]; B->[C, D]). Include resources
	// for clusters A, B and D, but don't configure the resource for cluster C
	// yet. This will help us verify that no configuration is pushed to the
	// child policy until the whole cluster graph is resolved.
	const (
		clusterNameA = clusterName // cluster name in cds LB policy config
		clusterNameB = clusterName + "-B"
		clusterNameC = clusterName + "-C"
		clusterNameD = clusterName + "-D"
	)
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterNameA, []string{clusterNameB, clusterNameC}),
			makeAggregateClusterResource(clusterNameB, []string{clusterNameC, clusterNameD}),
			e2e.DefaultCluster(clusterNameD, serviceName, e2e.SecurityLevelNone),
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, host, []uint32{port})},
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that no configuration is pushed to the child policy yet, because
	// not all clusters making up the aggregate cluster have been resolved yet.
	select {
	case cfg := <-lbCfgCh:
		t.Fatalf("Child policy received configuration when not expected to: %s", pretty.ToJSON(cfg))
	case <-time.After(defaultTestShortTimeout):
	}

	// Now configure the resource for cluster C in the management server,
	// thereby completing the cluster graph. This should result in configuration
	// being pushed down to the child policy.
	// Cluster C has no endpoints, so that the priority policy fails over to
	// cluster D.
	resources.Clusters = append(resources.Clusters, e2e.DefaultCluster(clusterNameC, edsClusterName, e2e.SecurityLevelNone))
	resources.Endpoints = append(resources.Endpoints, emptyEndpointResource(edsClusterName))
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	wantChildCfg := aggregateClusterPriorityConfig(clusterNameC, clusterNameD)
	if err := waitForLoadBalancingConfig(ctx, lbCfgCh, wantChildCfg); err != nil {
		t.Fatal(err)
	}
	// The cds policy of cluster C (the highest priority) is created first, and
	// the cds policy of cluster D is created only after failing over to it.
	wantLeafCfgC := createLeafClusterConfig(clusterNameC, "priority-0-0", true)
	if err := waitForLeafClusterState(ctx, odStateCh, wantLeafCfgC, ""); err != nil {
		t.Fatal(err)
	}
	wantLeafCfgD := createLeafClusterConfig(clusterNameD, "priority-0-0", true)
	wantAddrD := fmt.Sprintf("%s:%d", host, port)
	if err := waitForLeafClusterState(ctx, odStateCh, wantLeafCfgD, wantAddrD); err != nil {
		t.Fatal(err)
	}
}

// Tests the scenario where the aggregate cluster graph has a node that has
// child node of itself. The case for this is A -> A, and since there is no base
// cluster (EDS or Logical DNS), no configuration should be pushed to the child
// policy.  The channel is expected to move to TRANSIENT_FAILURE and RPCs are
// expected to fail with code UNAVAILABLE and an error message specifying that
// the aggregate cluster graph has no leaf clusters.  Then the test updates A -> B,
// where B is a leaf EDS cluster. Verifies that configuration is pushed to the
// child policy and that an RPC can be successfully made.
func (s) TestAggregatedCluster_NodeChildOfItself(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	lbCfgCh, _, _, _ := registerWrappedPriorityPolicy(ctx, t)
	odStateCh := registerWrappedOutlierDetectionPolicy(ctx, t)
	mgmtServer, nodeID, cc := setupWithManagementServer(t, nil, nil)

	const (
		clusterNameA = clusterName // cluster name in cds LB policy config
		clusterNameB = clusterName + "-B"
	)
	// Configure the management server with an aggregate cluster resource whose
	// child is itself.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:         []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
		Clusters:       []*v3clusterpb.Cluster{makeAggregateClusterResource(clusterNameA, []string{clusterNameA})},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	select {
	case cfg := <-lbCfgCh:
		t.Fatalf("Child policy received configuration when not expected to: %s", pretty.ToJSON(cfg))
	case <-time.After(defaultTestShortTimeout):
	}

	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	// Verify that the RPC fails with expected code.
	client := testgrpc.NewTestServiceClient(cc)
	_, err := client.EmptyCall(ctx, &testpb.Empty{})
	if gotCode, wantCode := status.Code(err), codes.Unavailable; gotCode != wantCode {
		t.Fatalf("EmptyCall() failed with code: %v, want %v", gotCode, wantCode)
	}
	const wantErr = "aggregate cluster graph has no leaf clusters"
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("EmptyCall() failed with err: %v, want error containing %s", err, wantErr)
	}

	// Start a test service backend.
	server := stubserver.StartTestService(t, nil)
	t.Cleanup(server.Stop)

	// Update the aggregate cluster to point to a leaf EDS cluster.
	resources = e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterNameA, []string{clusterNameB}),
			e2e.DefaultCluster(clusterNameB, serviceName, e2e.SecurityLevelNone),
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, host, []uint32{testutils.ParsePort(t, server.Address)})},
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify the configuration pushed to the child policy.
	wantChildCfg := aggregateClusterPriorityConfig(clusterNameB)
	if err := waitForLoadBalancingConfig(ctx, lbCfgCh, wantChildCfg); err != nil {
		t.Fatal(err)
	}
	wantLeafCfg := createLeafClusterConfig(clusterNameB, "priority-0-0", true)
	wantAddr := fmt.Sprintf("%s:%d", host, testutils.ParsePort(t, server.Address))
	if err := waitForLeafClusterState(ctx, odStateCh, wantLeafCfg, wantAddr); err != nil {
		t.Fatal(err)
	}

	// Verify that a successful RPC can be made.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

// Tests the scenario where the aggregate cluster graph contains a cycle and
// contains no leaf clusters. The case used here is [A -> B, B -> A]. As there
// are no leaf clusters in this graph, no configuration should be pushed to the
// child policy. The channel is expected to move to TRANSIENT_FAILURE and RPCs
// are expected to fail with code UNAVAILABLE and an error message specifying
// that the aggregate cluster graph has no leaf clusters.
func (s) TestAggregatedCluster_CycleWithNoLeafNode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	lbCfgCh, _, _, _ := registerWrappedPriorityPolicy(ctx, t)
	mgmtServer, nodeID, cc := setupWithManagementServer(t, nil, nil)

	const (
		clusterNameA = clusterName // cluster name in cds LB policy config
		clusterNameB = clusterName + "-B"
	)
	// Configure the management server with an aggregate cluster resource graph
	// that contains a cycle and no leaf clusters.
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterNameA, []string{clusterNameB}),
			makeAggregateClusterResource(clusterNameB, []string{clusterNameA}),
		},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	select {
	case cfg := <-lbCfgCh:
		t.Fatalf("Child policy received configuration when not expected to: %s", pretty.ToJSON(cfg))
	case <-time.After(defaultTestShortTimeout):
	}

	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	// Verify that the RPC fails with expected code.
	client := testgrpc.NewTestServiceClient(cc)
	_, err := client.EmptyCall(ctx, &testpb.Empty{})
	if gotCode, wantCode := status.Code(err), codes.Unavailable; gotCode != wantCode {
		t.Fatalf("EmptyCall() failed with code: %v, want %v", gotCode, wantCode)
	}
	const wantErr = "aggregate cluster graph has no leaf clusters"
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("EmptyCall() failed with err: %v, want %s", err, wantErr)
	}
}

// Tests the scenario where the aggregate cluster graph contains a cycle and
// also contains a leaf cluster. The case used here is [A -> B, B -> A, C]. As
// there is a leaf cluster in this graph , configuration should be pushed to the
// child policy and RPCs should get routed to that leaf cluster.
func (s) TestAggregatedCluster_CycleWithLeafNode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	lbCfgCh, _, _, _ := registerWrappedPriorityPolicy(ctx, t)
	odStateCh := registerWrappedOutlierDetectionPolicy(ctx, t)
	mgmtServer, nodeID, cc := setupWithManagementServer(t, nil, nil)

	// Start a test service backend.
	server := stubserver.StartTestService(t, nil)
	t.Cleanup(server.Stop)

	const (
		clusterNameA = clusterName // cluster name in cds LB policy config
		clusterNameB = clusterName + "-B"
		clusterNameC = clusterName + "-C"
	)
	// Configure the management server with an aggregate cluster resource graph
	// that contains a cycle, but also contains a leaf cluster.
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterNameA, []string{clusterNameB}),
			makeAggregateClusterResource(clusterNameB, []string{clusterNameA, clusterNameC}),
			e2e.DefaultCluster(clusterNameC, serviceName, e2e.SecurityLevelNone),
		},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, host, []uint32{testutils.ParsePort(t, server.Address)})},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify the configuration pushed to the child policy.
	wantChildCfg := aggregateClusterPriorityConfig(clusterNameC)
	if err := waitForLoadBalancingConfig(ctx, lbCfgCh, wantChildCfg); err != nil {
		t.Fatal(err)
	}
	wantLeafCfg := createLeafClusterConfig(clusterNameC, "priority-0-0", true)
	wantAddr := fmt.Sprintf("%s:%d", host, testutils.ParsePort(t, server.Address))
	if err := waitForLeafClusterState(ctx, odStateCh, wantLeafCfg, wantAddr); err != nil {
		t.Fatal(err)
	}

	// Verify that a successful RPC can be made.
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

// Tests the scenario where the cluster tree changes, and verifies that the
// watchers for the cds balancer are updated accordingly. That is the cluster
// removed from the tree no longer has a watcher and the new cluster added has a
// new watcher.
func (s) TestWatchers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cdsResourceRequestedCh := make(chan []string, 1)
	onStreamReq := func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
		if req.GetTypeUrl() == version.V3ClusterURL {
			if len(req.GetResourceNames()) > 0 {
				select {
				case cdsResourceRequestedCh <- req.GetResourceNames():
				case <-ctx.Done():
				}
			}
		}
		return nil
	}
	mgmtServer, nodeID, _ := setupWithManagementServer(t, nil, onStreamReq)

	const (
		clusterA = clusterName
		clusterB = clusterName + "-B"
		clusterC = clusterName + "-C"
		clusterD = clusterName + "-D"
	)

	// Initial CDS resources: A -> B, C
	initialResources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterA)},
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterA, []string{clusterB, clusterC}),
		},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, initialResources); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	wantNames := []string{clusterA, clusterB, clusterC}
	if err := waitForResourceNames(ctx, cdsResourceRequestedCh, wantNames); err != nil {
		t.Fatal(err)
	}

	// Update the CDS resources to remove cluster C and add cluster D.
	initialResources.Clusters = []*v3clusterpb.Cluster{
		makeAggregateClusterResource(clusterA, []string{clusterB, clusterD}),
	}
	if err := mgmtServer.Update(ctx, initialResources); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	wantNames = []string{clusterA, clusterB, clusterD}
	if err := waitForResourceNames(ctx, cdsResourceRequestedCh, wantNames); err != nil {
		t.Fatal(err)
	}
}

// Tests that, per gRFC A75, the LB policy configured in an aggregate cluster is
// ignored and each underlying (leaf) cluster uses its own LB policy. It also
// verifies that when GRPC_XDS_AGGREGATE_CLUSTER_BACKWARD_COMPAT is enabled,
// the leaf clusters use the LB policy configured in the aggregate cluster.
//
// Only the EDS cluster is verified, as there is no failover to the LogicalDNS
// cluster, so its cds policy is never created.
func (s) TestAggregateCluster_LeafClusterLBPolicy(t *testing.T) {
	ringHashPolicy := &iserviceconfig.BalancerConfig{
		Name:   ringhash.Name,
		Config: &iringhash.LBConfig{MinRingSize: 100, MaxRingSize: 1000},
	}
	roundRobinPolicy := &iserviceconfig.BalancerConfig{
		Name:   wrrlocality.Name,
		Config: &wrrlocality.LBConfig{ChildPolicy: &iserviceconfig.BalancerConfig{Name: roundrobin.Name}},
	}

	tests := []struct {
		name           string
		backwardCompat bool
		wantEDSPolicy  *iserviceconfig.BalancerConfig
	}{
		{
			name:           "leaf_policy_used",
			backwardCompat: false,
			wantEDSPolicy:  ringHashPolicy,
		},
		{
			// TODO: Remove once GRPC_XDS_AGGREGATE_CLUSTER_BACKWARD_COMPAT is
			// removed.
			name:           "backward_compat_uses_aggregate_policy",
			backwardCompat: true,
			wantEDSPolicy:  roundRobinPolicy,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testutils.SetEnvConfig(t, &envconfig.XDSAggregateClusterBackwardCompat, test.backwardCompat)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			dnsTargetCh, dnsR := setupDNS(t)
			lbCfgCh, _, _, _ := registerWrappedPriorityPolicy(ctx, t)
			odStateCh := registerWrappedOutlierDetectionPolicy(ctx, t)
			mgmtServer, nodeID, _ := setupWithManagementServer(t, nil, nil)

			// The aggregate cluster uses the default round_robin LB policy,
			// the EDS leaf cluster uses ring_hash and the LogicalDNS leaf
			// cluster uses the default round_robin LB policy.
			edsCluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
				ClusterName:   edsClusterName,
				ServiceName:   serviceName,
				SecurityLevel: e2e.SecurityLevelNone,
				Policy:        e2e.LoadBalancingPolicyRingHash,
			})
			edsCluster.LbConfig = &v3clusterpb.Cluster_RingHashLbConfig_{
				RingHashLbConfig: &v3clusterpb.Cluster_RingHashLbConfig{
					MinimumRingSize: wrapperspb.UInt64(100),
					MaximumRingSize: wrapperspb.UInt64(1000),
				},
			}
			resources := e2e.UpdateOptions{
				NodeID:    nodeID,
				Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(target, routeName)},
				Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, target, clusterName)},
				Clusters: []*v3clusterpb.Cluster{
					makeAggregateClusterResource(clusterName, []string{edsClusterName, dnsClusterName}),
					edsCluster,
					makeLogicalDNSClusterResource(dnsClusterName, dnsHostName, dnsPort),
				},
				Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(serviceName, host, []uint32{port})},
			}
			if err := mgmtServer.Update(ctx, resources); err != nil {
				t.Fatal(err)
			}
			verifyDNSResolution(ctx, t, dnsTargetCh, dnsR, dnsHostName, dnsPort)

			wantChildCfg := aggregateClusterPriorityConfig(edsClusterName, dnsClusterName)
			if err := waitForLoadBalancingConfig(ctx, lbCfgCh, wantChildCfg); err != nil {
				t.Fatal(err)
			}

			// Each leaf cluster is handled by its own child cds policy, which
			// builds its own outlier_detection subtree. Only the EDS leaf
			// (highest priority) is verified, since the priority policy does
			// not create the child for the LogicalDNS leaf unless it fails
			// over to it.
			wantODCfg := createLeafClusterConfigWithPolicy(edsClusterName, "priority-0-0", true, test.wantEDSPolicy)
			if err := waitForLeafClusterState(ctx, odStateCh, wantODCfg, ""); err != nil {
				t.Fatal(err)
			}
		})
	}
}
