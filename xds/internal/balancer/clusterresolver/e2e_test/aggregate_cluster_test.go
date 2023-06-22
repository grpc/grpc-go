/*
 * Copyright 2023 gRPC authors.
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

package e2e_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3aggregateclusterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/clusters/aggregate/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

// makeAggregateClusterResource returns an aggregate cluster resource with the
// given name and list of child names.
func makeAggregateClusterResource(name string, childNames []string) *v3clusterpb.Cluster {
	return &v3clusterpb.Cluster{
		Name: name,
		ClusterDiscoveryType: &v3clusterpb.Cluster_ClusterType{
			ClusterType: &v3clusterpb.Cluster_CustomClusterType{
				Name: "envoy.clusters.aggregate",
				TypedConfig: testutils.MarshalAny(&v3aggregateclusterpb.ClusterConfig{
					Clusters: childNames,
				}),
			},
		},
		LbPolicy: v3clusterpb.Cluster_ROUND_ROBIN,
	}
}

// makeLogicalDNSClusterResource returns a LOGICAL_DNS cluster resource with the
// given name and given DNS host and port.
func makeLogicalDNSClusterResource(name, dnsHost string, dnsPort uint32) *v3clusterpb.Cluster {
	return &v3clusterpb.Cluster{
		Name:                 name,
		ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_LOGICAL_DNS},
		LbPolicy:             v3clusterpb.Cluster_ROUND_ROBIN,
		LoadAssignment: &v3endpointpb.ClusterLoadAssignment{
			Endpoints: []*v3endpointpb.LocalityLbEndpoints{{
				LbEndpoints: []*v3endpointpb.LbEndpoint{{
					HostIdentifier: &v3endpointpb.LbEndpoint_Endpoint{
						Endpoint: &v3endpointpb.Endpoint{
							Address: &v3corepb.Address{
								Address: &v3corepb.Address_SocketAddress{
									SocketAddress: &v3corepb.SocketAddress{
										Address: dnsHost,
										PortSpecifier: &v3corepb.SocketAddress_PortValue{
											PortValue: dnsPort,
										},
									},
								},
							},
						},
					},
				}},
			}},
		},
	}
}

// setupDNS unregisters the DNS resolver and registers a manual resolver for the
// same scheme. This allows the test to mock the DNS resolution by supplying the
// addresses of the test backends.
//
// Returns the following:
//   - a channel onto which the DNS target being resolved is written to by the
//     mock DNS resolver
//   - a channel to notify close of the DNS resolver
//   - a channel to notify re-resolution requests to the DNS resolver
//   - a manual resolver which is used to mock the actual DNS resolution
//   - a cleanup function which re-registers the original DNS resolver
func setupDNS() (chan resolver.Target, chan struct{}, chan resolver.ResolveNowOptions, *manual.Resolver, func()) {
	targetCh := make(chan resolver.Target, 1)
	closeCh := make(chan struct{}, 1)
	resolveNowCh := make(chan resolver.ResolveNowOptions, 1)

	mr := manual.NewBuilderWithScheme("dns")
	mr.BuildCallback = func(target resolver.Target, _ resolver.ClientConn, _ resolver.BuildOptions) { targetCh <- target }
	mr.CloseCallback = func() { closeCh <- struct{}{} }
	mr.ResolveNowCallback = func(opts resolver.ResolveNowOptions) { resolveNowCh <- opts }

	dnsResolverBuilder := resolver.Get("dns")
	resolver.UnregisterForTesting("dns")
	resolver.Register(mr)

	return targetCh, closeCh, resolveNowCh, mr, func() { resolver.Register(dnsResolverBuilder) }
}

// TestAggregateCluster_WithTwoEDSClusters tests the case where the top-level
// cluster resource is an aggregate cluster. It verifies that RPCs fail when the
// management server has not responded to all requested EDS resources, and also
// that RPCs are routed to the highest priority cluster once all requested EDS
// resources have been sent by the management server.
func (s) TestAggregateCluster_WithTwoEDSClusters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server that pushes the EDS resource names onto a
	// channel when requested.
	edsResourceNameCh := make(chan []string, 1)
	managementServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() != version.V3EndpointsURL {
				return nil
			}
			if len(req.GetResourceNames()) == 0 {
				// This is the case for ACKs. Do nothing here.
				return nil
			}
			select {
			case edsResourceNameCh <- req.GetResourceNames():
			case <-ctx.Done():
			}
			return nil
		},
		AllowResourceSubset: true,
	})
	defer cleanup()

	// Start two test backends and extract their host and port. The first
	// backend belongs to EDS cluster "cluster-1", while the second backend
	// belongs to EDS cluster "cluster-2".
	servers, cleanup2 := startTestServiceBackends(t, 2)
	defer cleanup2()
	addrs, ports := backendAddressesAndPorts(t, servers)

	// Configure an aggregate cluster, two EDS clusters and only one endpoints
	// resource (corresponding to the first EDS cluster) in the management
	// server.
	const clusterName1 = clusterName + "-cluster-1"
	const clusterName2 = clusterName + "-cluster-2"
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterName, []string{clusterName1, clusterName2}),
			e2e.DefaultCluster(clusterName1, "", e2e.SecurityLevelNone),
			e2e.DefaultCluster(clusterName2, "", e2e.SecurityLevelNone),
		},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(clusterName1, "localhost", []uint32{uint32(ports[0])})},
		SkipValidation: true,
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create xDS client, configure cds_experimental LB policy with a manual
	// resolver, and dial the test backends.
	cc, cleanup := setupAndDial(t, bootstrapContents)
	defer cleanup()

	// Wait for both EDS resources to be requested.
	func() {
		for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
			select {
			case names := <-edsResourceNameCh:
				// Copy and sort the sortedNames to avoid racing with an
				// OnStreamRequest call.
				sortedNames := make([]string, len(names))
				copy(sortedNames, names)
				sort.Strings(sortedNames)
				if cmp.Equal(sortedNames, []string{clusterName1, clusterName2}) {
					return
				}
			default:
			}
		}
	}()
	if ctx.Err() != nil {
		t.Fatalf("Timeout when waiting for all EDS resources %v to be requested", []string{clusterName1, clusterName2})
	}

	// Make an RPC with a short deadline. We expect this RPC to not succeed
	// because the management server has not responded with all EDS resources
	// requested.
	client := testgrpc.NewTestServiceClient(cc)
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := client.EmptyCall(sCtx, &testpb.Empty{}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() code %s, want %s", status.Code(err), codes.DeadlineExceeded)
	}

	// Update the management server with the second EDS resource.
	resources.Endpoints = append(resources.Endpoints, e2e.DefaultEndpoint(clusterName2, "localhost", []uint32{uint32(ports[1])}))
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Make an RPC and ensure that it gets routed to cluster-1, implicitly
	// higher priority than cluster-2.
	peer := &peer.Peer{}
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peer), grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if peer.Addr.String() != addrs[0].Addr {
		t.Fatalf("EmptyCall() routed to backend %q, want %q", peer.Addr, addrs[0].Addr)
	}
}

// TestAggregateCluster_WithTwoEDSClusters_PrioritiesChange tests the case where
// the top-level cluster resource is an aggregate cluster. It verifies that RPCs
// are routed to the highest priority EDS cluster.
func (s) TestAggregateCluster_WithTwoEDSClusters_PrioritiesChange(t *testing.T) {
	// Start an xDS management server.
	managementServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer cleanup()

	// Start two test backends and extract their host and port. The first
	// backend belongs to EDS cluster "cluster-1", while the second backend
	// belongs to EDS cluster "cluster-2".
	servers, cleanup2 := startTestServiceBackends(t, 2)
	defer cleanup2()
	addrs, ports := backendAddressesAndPorts(t, servers)

	// Configure an aggregate cluster, two EDS clusters and the corresponding
	// endpoints resources in the management server.
	const clusterName1 = clusterName + "cluster-1"
	const clusterName2 = clusterName + "cluster-2"
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterName, []string{clusterName1, clusterName2}),
			e2e.DefaultCluster(clusterName1, "", e2e.SecurityLevelNone),
			e2e.DefaultCluster(clusterName2, "", e2e.SecurityLevelNone),
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{
			e2e.DefaultEndpoint(clusterName1, "localhost", []uint32{uint32(ports[0])}),
			e2e.DefaultEndpoint(clusterName2, "localhost", []uint32{uint32(ports[1])}),
		},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create xDS client, configure cds_experimental LB policy with a manual
	// resolver, and dial the test backends.
	cc, cleanup := setupAndDial(t, bootstrapContents)
	defer cleanup()

	// Make an RPC and ensure that it gets routed to cluster-1, implicitly
	// higher priority than cluster-2.
	client := testgrpc.NewTestServiceClient(cc)
	peer := &peer.Peer{}
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peer), grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if peer.Addr.String() != addrs[0].Addr {
		t.Fatalf("EmptyCall() routed to backend %q, want %q", peer.Addr, addrs[0].Addr)
	}

	// Swap the priorities of the EDS clusters in the aggregate cluster.
	resources.Clusters = []*v3clusterpb.Cluster{
		makeAggregateClusterResource(clusterName, []string{clusterName2, clusterName1}),
		e2e.DefaultCluster(clusterName1, "", e2e.SecurityLevelNone),
		e2e.DefaultCluster(clusterName2, "", e2e.SecurityLevelNone),
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for RPCs to get routed to cluster-2, which is now implicitly higher
	// priority than cluster-1, after the priority switch above.
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peer), grpc.WaitForReady(true)); err != nil {
			t.Fatalf("EmptyCall() failed: %v", err)
		}
		if peer.Addr.String() == addrs[1].Addr {
			break
		}
	}
	if ctx.Err() != nil {
		t.Fatal("Timeout waiting for RPCs to be routed to cluster-2 after priority switch")
	}
}

// TestAggregateCluster_WithOneDNSCluster tests the case where the top-level
// cluster resource is an aggregate cluster that resolves to a single
// LOGICAL_DNS cluster. The test verifies that RPCs can be made to backends that
// make up the LOGICAL_DNS cluster.
func (s) TestAggregateCluster_WithOneDNSCluster(t *testing.T) {
	dnsTargetCh, _, _, dnsR, cleanup1 := setupDNS()
	defer cleanup1()

	// Start an xDS management server.
	managementServer, nodeID, bootstrapContents, _, cleanup2 := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer cleanup2()

	// Start two test backends.
	servers, cleanup3 := startTestServiceBackends(t, 2)
	defer cleanup3()
	addrs, _ := backendAddressesAndPorts(t, servers)

	// Configure an aggregate cluster pointing to a single LOGICAL_DNS cluster.
	const (
		dnsClusterName = clusterName + "-dns"
		dnsHostName    = "dns_host"
		dnsPort        = uint32(8080)
	)
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterName, []string{dnsClusterName}),
			makeLogicalDNSClusterResource(dnsClusterName, dnsHostName, dnsPort),
		},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create xDS client, configure cds_experimental LB policy with a manual
	// resolver, and dial the test backends.
	cc, cleanup := setupAndDial(t, bootstrapContents)
	defer cleanup()

	// Ensure that the DNS resolver is started for the expected target.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for DNS resolver to be started")
	case target := <-dnsTargetCh:
		got, want := target.Endpoint(), fmt.Sprintf("%s:%d", dnsHostName, dnsPort)
		if got != want {
			t.Fatalf("DNS resolution started for target %q, want %q", got, want)
		}
	}

	// Update DNS resolver with test backend addresses.
	dnsR.UpdateState(resolver.State{Addresses: addrs})

	// Make an RPC and ensure that it gets routed to the first backend since the
	// child policy for a LOGICAL_DNS cluster is pick_first by default.
	client := testgrpc.NewTestServiceClient(cc)
	peer := &peer.Peer{}
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peer), grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if peer.Addr.String() != addrs[0].Addr {
		t.Fatalf("EmptyCall() routed to backend %q, want %q", peer.Addr, addrs[0].Addr)
	}
}

// TestAggregateCluster_WithEDSAndDNS tests the case where the top-level cluster
// resource is an aggregate cluster that resolves to an EDS and a LOGICAL_DNS
// cluster. The test verifies that RPCs fail until both clusters are resolved to
// endpoints, and RPCs are routed to the higher priority EDS cluster.
func (s) TestAggregateCluster_WithEDSAndDNS(t *testing.T) {
	dnsTargetCh, _, _, dnsR, cleanup1 := setupDNS()
	defer cleanup1()

	// Start an xDS management server that pushes the name of the requested EDS
	// resource onto a channel.
	edsResourceCh := make(chan string, 1)
	managementServer, nodeID, bootstrapContents, _, cleanup2 := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() != version.V3EndpointsURL {
				return nil
			}
			if len(req.GetResourceNames()) > 0 {
				select {
				case edsResourceCh <- req.GetResourceNames()[0]:
				default:
				}
			}
			return nil
		},
		AllowResourceSubset: true,
	})
	defer cleanup2()

	// Start two test backends and extract their host and port. The first
	// backend is used for the EDS cluster and the second backend is used for
	// the LOGICAL_DNS cluster.
	servers, cleanup3 := startTestServiceBackends(t, 2)
	defer cleanup3()
	addrs, ports := backendAddressesAndPorts(t, servers)

	// Configure an aggregate cluster pointing to an EDS and DNS cluster. Also
	// configure an endpoints resource for the EDS cluster.
	const (
		edsClusterName = clusterName + "-eds"
		dnsClusterName = clusterName + "-dns"
		dnsHostName    = "dns_host"
		dnsPort        = uint32(8080)
	)
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterName, []string{edsClusterName, dnsClusterName}),
			e2e.DefaultCluster(edsClusterName, "", e2e.SecurityLevelNone),
			makeLogicalDNSClusterResource(dnsClusterName, dnsHostName, dnsPort),
		},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(edsClusterName, "localhost", []uint32{uint32(ports[0])})},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create xDS client, configure cds_experimental LB policy with a manual
	// resolver, and dial the test backends.
	cc, cleanup := setupAndDial(t, bootstrapContents)
	defer cleanup()

	// Ensure that an EDS request is sent for the expected resource name.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for EDS request to be received on the management server")
	case name := <-edsResourceCh:
		if name != edsClusterName {
			t.Fatalf("Received EDS request with resource name %q, want %q", name, edsClusterName)
		}
	}

	// Ensure that the DNS resolver is started for the expected target.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for DNS resolver to be started")
	case target := <-dnsTargetCh:
		got, want := target.Endpoint(), fmt.Sprintf("%s:%d", dnsHostName, dnsPort)
		if got != want {
			t.Fatalf("DNS resolution started for target %q, want %q", got, want)
		}
	}

	// Make an RPC with a short deadline. We expect this RPC to not succeed
	// because the DNS resolver has not responded with endpoint addresses.
	client := testgrpc.NewTestServiceClient(cc)
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := client.EmptyCall(sCtx, &testpb.Empty{}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() code %s, want %s", status.Code(err), codes.DeadlineExceeded)
	}

	// Update DNS resolver with test backend addresses.
	dnsR.UpdateState(resolver.State{Addresses: addrs[1:]})

	// Make an RPC and ensure that it gets routed to the first backend since the
	// EDS cluster is of higher priority than the LOGICAL_DNS cluster.
	peer := &peer.Peer{}
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peer), grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if peer.Addr.String() != addrs[0].Addr {
		t.Fatalf("EmptyCall() routed to backend %q, want %q", peer.Addr, addrs[0].Addr)
	}
}

// TestAggregateCluster_SwitchEDSAndDNS tests the case where the top-level
// cluster resource is an aggregate cluster. It initially resolves to a single
// EDS cluster. The test verifies that RPCs are routed to backends in the EDS
// cluster. Subsequently, the aggregate cluster resolves to a single DNS
// cluster. The test verifies that RPCs are successful, this time to backends in
// the DNS cluster.
func (s) TestAggregateCluster_SwitchEDSAndDNS(t *testing.T) {
	dnsTargetCh, _, _, dnsR, cleanup1 := setupDNS()
	defer cleanup1()

	// Start an xDS management server.
	managementServer, nodeID, bootstrapContents, _, cleanup2 := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer cleanup2()

	// Start two test backends and extract their host and port. The first
	// backend is used for the EDS cluster and the second backend is used for
	// the LOGICAL_DNS cluster.
	servers, cleanup3 := startTestServiceBackends(t, 2)
	defer cleanup3()
	addrs, ports := backendAddressesAndPorts(t, servers)

	// Configure an aggregate cluster pointing to a single EDS cluster. Also,
	// configure the underlying EDS cluster (and the corresponding endpoints
	// resource) and DNS cluster (will be used later in the test).
	const (
		dnsClusterName = clusterName + "-dns"
		dnsHostName    = "dns_host"
		dnsPort        = uint32(8080)
	)
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterName, []string{edsServiceName}),
			e2e.DefaultCluster(edsServiceName, "", e2e.SecurityLevelNone),
			makeLogicalDNSClusterResource(dnsClusterName, dnsHostName, dnsPort),
		},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(edsServiceName, "localhost", []uint32{uint32(ports[0])})},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create xDS client, configure cds_experimental LB policy with a manual
	// resolver, and dial the test backends.
	cc, cleanup := setupAndDial(t, bootstrapContents)
	defer cleanup()

	// Ensure that the RPC is routed to the appropriate backend.
	client := testgrpc.NewTestServiceClient(cc)
	peer := &peer.Peer{}
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peer), grpc.WaitForReady(true)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if peer.Addr.String() != addrs[0].Addr {
		t.Fatalf("EmptyCall() routed to backend %q, want %q", peer.Addr, addrs[0].Addr)
	}

	// Update the aggregate cluster to point to a single DNS cluster.
	resources.Clusters = []*v3clusterpb.Cluster{
		makeAggregateClusterResource(clusterName, []string{dnsClusterName}),
		e2e.DefaultCluster(edsServiceName, "", e2e.SecurityLevelNone),
		makeLogicalDNSClusterResource(dnsClusterName, dnsHostName, dnsPort),
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Ensure that the DNS resolver is started for the expected target.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for DNS resolver to be started")
	case target := <-dnsTargetCh:
		got, want := target.Endpoint(), fmt.Sprintf("%s:%d", dnsHostName, dnsPort)
		if got != want {
			t.Fatalf("DNS resolution started for target %q, want %q", got, want)
		}
	}

	// Update DNS resolver with test backend addresses.
	dnsR.UpdateState(resolver.State{Addresses: addrs[1:]})

	// Ensure that start getting routed to the backend corresponding to the
	// LOGICAL_DNS cluster.
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peer))
		if peer.Addr.String() == addrs[1].Addr {
			break
		}
	}
	if ctx.Err() != nil {
		t.Fatalf("Timeout when waiting for RPCs to be routed to backend %q in the DNS cluster", addrs[1].Addr)
	}
}

// TestAggregateCluster_BadEDS_GoodToBadDNS tests the case where the top-level
// cluster is an aggregate cluster that resolves to an EDS and LOGICAL_DNS
// cluster. The test first asserts that no RPCs can be made after receiving an
// EDS response with zero endpoints because no update has been received from the
// DNS resolver yet. Once the DNS resolver pushes an update, the test verifies
// that we switch to the DNS cluster and can make a successful RPC. At this
// point when the DNS cluster returns an error, the test verifies that RPCs are
// still successful. This is the expected behavior because pick_first (the leaf
// policy) ignores resolver errors when it is not in TransientFailure.
func (s) TestAggregateCluster_BadEDS_GoodToBadDNS(t *testing.T) {
	dnsTargetCh, _, _, dnsR, cleanup1 := setupDNS()
	defer cleanup1()

	// Start an xDS management server.
	managementServer, nodeID, bootstrapContents, _, cleanup2 := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer cleanup2()

	// Start two test backends.
	servers, cleanup3 := startTestServiceBackends(t, 2)
	defer cleanup3()
	addrs, _ := backendAddressesAndPorts(t, servers)

	// Configure an aggregate cluster pointing to an EDS and LOGICAL_DNS
	// cluster. Also configure an empty endpoints resource for the EDS cluster
	// that contains no endpoints.
	const (
		edsClusterName = clusterName + "-eds"
		dnsClusterName = clusterName + "-dns"
		dnsHostName    = "dns_host"
		dnsPort        = uint32(8080)
	)
	emptyEndpointResource := e2e.DefaultEndpoint(edsServiceName, "localhost", nil)
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterName, []string{edsClusterName, dnsClusterName}),
			e2e.DefaultCluster(edsClusterName, edsServiceName, e2e.SecurityLevelNone),
			makeLogicalDNSClusterResource(dnsClusterName, dnsHostName, dnsPort),
		},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{emptyEndpointResource},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create xDS client, configure cds_experimental LB policy with a manual
	// resolver, and dial the test backends.
	cc, cleanup := setupAndDial(t, bootstrapContents)
	defer cleanup()

	// Make an RPC with a short deadline. We expect this RPC to not succeed
	// because the EDS resource came back with no endpoints, and we are yet to
	// push an update through the DNS resolver.
	client := testgrpc.NewTestServiceClient(cc)
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := client.EmptyCall(sCtx, &testpb.Empty{}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() code %s, want %s", status.Code(err), codes.DeadlineExceeded)
	}

	// Ensure that the DNS resolver is started for the expected target.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for DNS resolver to be started")
	case target := <-dnsTargetCh:
		got, want := target.Endpoint(), fmt.Sprintf("%s:%d", dnsHostName, dnsPort)
		if got != want {
			t.Fatalf("DNS resolution started for target %q, want %q", got, want)
		}
	}

	// Update DNS resolver with test backend addresses.
	dnsR.UpdateState(resolver.State{Addresses: addrs})

	// Ensure that RPCs start getting routed to the first backend since the
	// child policy for a LOGICAL_DNS cluster is pick_first by default.
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		peer := &peer.Peer{}
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peer)); err != nil {
			t.Logf("EmptyCall() failed: %v", err)
			continue
		}
		if peer.Addr.String() == addrs[0].Addr {
			break
		}
	}
	if ctx.Err() != nil {
		t.Fatalf("Timeout when waiting for RPCs to be routed to backend %q in the DNS cluster", addrs[0].Addr)
	}

	// Push an error from the DNS resolver as well.
	dnsErr := fmt.Errorf("DNS error")
	dnsR.ReportError(dnsErr)

	// Ensure that RPCs continue to succeed for the next second.
	for end := time.Now().Add(time.Second); time.Now().Before(end); <-time.After(defaultTestShortTimeout) {
		peer := &peer.Peer{}
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peer)); err != nil {
			t.Fatalf("EmptyCall() failed: %v", err)
		}
		if peer.Addr.String() != addrs[0].Addr {
			t.Fatalf("EmptyCall() routed to backend %q, want %q", peer.Addr, addrs[0].Addr)
		}
	}
}

// TestAggregateCluster_BadEDS_BadDNS tests the case where the top-level cluster
// is an aggregate cluster that resolves to an EDS and LOGICAL_DNS cluster. When
// the EDS request returns a resource that contains no endpoints, the test
// verifies that we switch to the DNS cluster. When the DNS cluster returns an
// error, the test verifies that RPCs fail with the error returned by the DNS
// resolver, and thus, ensures that pick_first (the leaf policy) does not ignore
// resolver errors.
func (s) TestAggregateCluster_BadEDS_BadDNS(t *testing.T) {
	dnsTargetCh, _, _, dnsR, cleanup1 := setupDNS()
	defer cleanup1()

	// Start an xDS management server.
	managementServer, nodeID, bootstrapContents, _, cleanup2 := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer cleanup2()

	// Configure an aggregate cluster pointing to an EDS and LOGICAL_DNS
	// cluster. Also configure an empty endpoints resource for the EDS cluster
	// that contains no endpoints.
	const (
		edsClusterName = clusterName + "-eds"
		dnsClusterName = clusterName + "-dns"
		dnsHostName    = "dns_host"
		dnsPort        = uint32(8080)
	)
	emptyEndpointResource := e2e.DefaultEndpoint(edsServiceName, "localhost", nil)
	resources := e2e.UpdateOptions{
		NodeID: nodeID,
		Clusters: []*v3clusterpb.Cluster{
			makeAggregateClusterResource(clusterName, []string{edsClusterName, dnsClusterName}),
			e2e.DefaultCluster(edsClusterName, edsServiceName, e2e.SecurityLevelNone),
			makeLogicalDNSClusterResource(dnsClusterName, dnsHostName, dnsPort),
		},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{emptyEndpointResource},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create xDS client, configure cds_experimental LB policy with a manual
	// resolver, and dial the test backends.
	cc, cleanup := setupAndDial(t, bootstrapContents)
	defer cleanup()

	// Make an RPC with a short deadline. We expect this RPC to not succeed
	// because the EDS resource came back with no endpoints, and we are yet to
	// push an update through the DNS resolver.
	client := testgrpc.NewTestServiceClient(cc)
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := client.EmptyCall(sCtx, &testpb.Empty{}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("EmptyCall() code %s, want %s", status.Code(err), codes.DeadlineExceeded)
	}

	// Ensure that the DNS resolver is started for the expected target.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for DNS resolver to be started")
	case target := <-dnsTargetCh:
		got, want := target.Endpoint(), fmt.Sprintf("%s:%d", dnsHostName, dnsPort)
		if got != want {
			t.Fatalf("DNS resolution started for target %q, want %q", got, want)
		}
	}

	// Push an error from the DNS resolver as well.
	dnsErr := fmt.Errorf("DNS error")
	dnsR.ReportError(dnsErr)

	// Ensure that the error returned from the DNS resolver is reported to the
	// caller of the RPC.
	_, err := client.EmptyCall(ctx, &testpb.Empty{})
	if code := status.Code(err); code != codes.Unavailable {
		t.Fatalf("EmptyCall() failed with code %s, want %s", code, codes.Unavailable)
	}
	if err == nil || !strings.Contains(err.Error(), dnsErr.Error()) {
		t.Fatalf("EmptyCall() failed with error %v, want %v", err, dnsErr)
	}
}
