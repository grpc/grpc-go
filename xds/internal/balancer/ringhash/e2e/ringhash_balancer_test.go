/*
 *
 * Copyright 2022 gRPC authors.
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

package ringhash_test

import (
	"context"
	"fmt"
	"math"
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3ringhashpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/ring_hash/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/protobuf/types/known/wrapperspb"

	_ "google.golang.org/grpc/xds"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestTimeout = 10 * time.Second

	errorTolerance = .05 // For tests that rely on statistical significance.

	virtualHostName = "test.server"
)

// Tests the case where the ring contains a single subConn, and verifies that
// when the server goes down, the LB policy on the client automatically
// reconnects until the subChannel moves out of TRANSIENT_FAILURE.
func (s) TestRingHash_ReconnectToMoveOutOfTransientFailure(t *testing.T) {
	// Create a restartable listener to simulate server being down.
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)
	srv := stubserver.StartTestService(t, &stubserver.StubServer{
		Listener:   lis,
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
	})
	defer srv.Stop()

	// Create a clientConn with a manual resolver (which is used to push the
	// address of the test backend), and a default service config pointing to
	// the use of the ring_hash_experimental LB policy.
	const ringHashServiceConfig = `{"loadBalancingConfig": [{"ring_hash_experimental":{}}]}`
	r := manual.NewBuilderWithScheme("whatever")
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithDefaultServiceConfig(ringHashServiceConfig),
	}
	cc, err := grpc.NewClient(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	defer cc.Close()

	// Push the address of the test backend through the manual resolver.
	r.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: lis.Addr().String()}}})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// Stopping the server listener will close the transport on the client,
	// which will lead to the channel eventually moving to IDLE. The ring_hash
	// LB policy is not expected to reconnect by itself at this point.
	lis.Stop()

	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Make an RPC to get the ring_hash LB policy to reconnect and thereby move
	// to TRANSIENT_FAILURE upon connection failure.
	client.EmptyCall(ctx, &testpb.Empty{})

	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	// An RPC at this point is expected to fail.
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}); err == nil {
		t.Fatal("EmptyCall RPC succeeded when the channel is in TRANSIENT_FAILURE")
	}

	// Restart the server listener. The ring_hash LB polcy is expected to
	// attempt to reconnect on its own and come out of TRANSIENT_FAILURE, even
	// without an RPC attempt.
	lis.Restart()
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	// An RPC at this point is expected to succeed.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}

// startTestServiceBackends starts num stub servers.
func startTestServiceBackends(t *testing.T, num int) ([]*stubserver.StubServer, func()) {
	t.Helper()

	var servers []*stubserver.StubServer
	for i := 0; i < num; i++ {
		servers = append(servers, stubserver.StartTestService(t, nil))
	}

	return servers, func() {
		for _, server := range servers {
			server.Stop()
		}
	}
}

// backendOptions returns a slice of e2e.BackendOptions for the given stub
// servers.
func backendOptions(t *testing.T, servers []*stubserver.StubServer) []e2e.BackendOptions {
	t.Helper()

	var backendOpts []e2e.BackendOptions
	for _, server := range servers {
		backendOpts = append(backendOpts, e2e.BackendOptions{
			Port:   testutils.ParsePort(t, server.Address),
			Weight: 1,
		})
	}
	return backendOpts
}

// channelIDHashRoute returns a RouteConfiguration with a hash policy that
// hashes based on the channel ID.
func channelIDHashRoute(routeName, virtualHostDomain, clusterName string) *v3routepb.RouteConfiguration {
	route := e2e.DefaultRouteConfig(routeName, virtualHostDomain, clusterName)
	hashPolicy := v3routepb.RouteAction_HashPolicy{
		PolicySpecifier: &v3routepb.RouteAction_HashPolicy_FilterState_{
			FilterState: &v3routepb.RouteAction_HashPolicy_FilterState{
				Key: "io.grpc.channel_id",
			},
		},
	}
	action := route.VirtualHosts[0].Routes[0].Action.(*v3routepb.Route_Route)
	action.Route.HashPolicy = []*v3routepb.RouteAction_HashPolicy{&hashPolicy}
	return route
}

// checkRPCSendOK sends num RPCs to the client. It returns a map of backend
// addresses as keys and number of RPCs sent to this address as value. Abort the
// test if any RPC fails.
func checkRPCSendOK(t *testing.T, ctx context.Context, client testgrpc.TestServiceClient, num int) map[string]int {
	t.Helper()

	backendCount := make(map[string]int)
	for i := 0; i < num; i++ {
		var remote peer.Peer
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&remote)); err != nil {
			t.Fatalf("rpc EmptyCall() failed: %v", err)
		}
		backendCount[remote.Addr.String()]++
	}
	return backendCount
}

// makeNonExistentBackends returns a slice of e2e.BackendOptions with num
// listeners, each of which is closed immediately. Useful to simulate servers
// that are unreachable.
func makeNonExistentBackends(t *testing.T, num int) []e2e.BackendOptions {
	closedListeners := make([]net.Listener, 0, num)
	for i := 0; i < num; i++ {
		lis, err := testutils.LocalTCPListener()
		if err != nil {
			t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
		}
		closedListeners = append(closedListeners, lis)
	}

	// Stop the servers that we want to be unreachable and collect their
	// addresses.
	backendOptions := make([]e2e.BackendOptions, 0, num)
	for _, lis := range closedListeners {
		backendOptions = append(backendOptions, e2e.BackendOptions{
			Port:   testutils.ParsePort(t, lis.Addr().String()),
			Weight: 1,
		})
		lis.Close()
	}
	return backendOptions
}

// Tests that when an aggregate cluster is configured with ring hash policy, and
// the first cluster is in transient failure, all RPCs are sent to the second
// cluster using the ring hash policy.
func (s) TestRingHash_AggregateClusterFallBackFromRingHashAtStartup(t *testing.T) {
	xdsServer, nodeID, _, xdsResolver, stop := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer stop()

	servers, stop := startTestServiceBackends(t, 2)
	defer stop()

	const primaryClusterName = "new_cluster_1"
	const primaryServiceName = "new_eds_service_1"
	const secondaryClusterName = "new_cluster_2"
	const secondaryServiceName = "new_eds_service_2"
	const clusterName = "aggregate_cluster"

	ep1 := e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
		ClusterName: primaryServiceName,
		Localities: []e2e.LocalityOptions{{
			Name:     "locality0",
			Weight:   1,
			Backends: makeNonExistentBackends(t, 2),
		}},
	})
	ep2 := e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
		ClusterName: secondaryServiceName,
		Localities: []e2e.LocalityOptions{{
			Name:     "locality0",
			Weight:   1,
			Backends: backendOptions(t, servers),
		}},
	})
	primaryCluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: primaryClusterName,
		ServiceName: primaryServiceName,
	})
	secundaryCluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: secondaryClusterName,
		ServiceName: secondaryServiceName,
	})
	cluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: clusterName,
		Type:        e2e.ClusterTypeAggregate,
		// TODO: when "A75: xDS Aggregate Cluster Behavior Fixes" is implemented, the
		// policy will have to be set on the child clusters.
		Policy:     e2e.LoadBalancingPolicyRingHash,
		ChildNames: []string{primaryClusterName, secondaryClusterName},
	})
	route := channelIDHashRoute("new_route", virtualHostName, clusterName)
	listener := e2e.DefaultClientListener(virtualHostName, route.Name)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	err := xdsServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{ep1, ep2},
		Clusters:  []*v3clusterpb.Cluster{cluster, primaryCluster, secundaryCluster},
		Routes:    []*v3routepb.RouteConfiguration{route},
		Listeners: []*v3listenerpb.Listener{listener},
	})
	if err != nil {
		t.Fatalf("Failed to update xDS resources: %v", err)
	}

	conn, err := grpc.NewClient("xds:///test.server", grpc.WithResolvers(xdsResolver), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create client: %s", err)
	}
	defer conn.Close()
	client := testgrpc.NewTestServiceClient(conn)

	gotPerBackend := checkRPCSendOK(t, ctx, client, 100)

	// Since this is using ring hash with the channel ID as the key, all RPCs
	// are routed to the same backend of the secondary locality.
	if len(gotPerBackend) != 1 {
		t.Errorf("Got RPCs routed to %v backends, want %v", len(gotPerBackend), 1)
	}

	var backend string
	var got int
	for backend, got = range gotPerBackend {
	}
	found := false
	for _, server := range servers {
		if backend == server.Address {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Got RPCs routed to an unexpected backend: %v, want one of %v", backend, servers)
	}
	if got != 100 {
		t.Errorf("Got %v RPCs routed to a backend, want %v", got, 100)
	}
}

func replaceDNSResolver(t *testing.T) *manual.Resolver {
	mr := manual.NewBuilderWithScheme("dns")

	dnsResolverBuilder := resolver.Get("dns")
	resolver.Register(mr)

	t.Cleanup(func() { resolver.Register(dnsResolverBuilder) })
	return mr
}

// Tests that when an aggregate cluster is configured with ring hash policy, and
// the first is an EDS cluster in transient failure, and the fallback is a
// logical DNS cluster, all RPCs are sent to the second cluster using the ring
// hash policy.
func (s) TestRingHash_AggregateClusterFallBackFromRingHashToLogicalDnsAtStartup(t *testing.T) {
	xdsServer, nodeID, _, xdsResolver, stop := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer stop()

	const edsClusterName = "eds_cluster"
	const logicalDNSClusterName = "logical_dns_cluster"
	const clusterName = "aggregate_cluster"

	backends, stop := startTestServiceBackends(t, 1)
	defer stop()

	endpoints := e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
		ClusterName: edsClusterName,
		Localities: []e2e.LocalityOptions{{
			Name:     "locality0",
			Weight:   1,
			Backends: makeNonExistentBackends(t, 1),
			Priority: 0,
		}},
	})
	edsCluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: edsClusterName,
		ServiceName: edsClusterName,
	})

	logicalDNSCluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		Type:        e2e.ClusterTypeLogicalDNS,
		ClusterName: logicalDNSClusterName,
		// The DNS values are not used because we fake DNS later on, but they
		// are required to be present for the resource to be valid.
		DNSHostName: "server.example.com",
		DNSPort:     443,
	})
	cluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: clusterName,
		Type:        e2e.ClusterTypeAggregate,
		// TODO: when "A75: xDS Aggregate Cluster Behavior Fixes" is merged, the
		// policy will have to be set on the child clusters.
		Policy:     e2e.LoadBalancingPolicyRingHash,
		ChildNames: []string{edsClusterName, logicalDNSClusterName},
	})
	route := channelIDHashRoute("new_route", virtualHostName, clusterName)
	listener := e2e.DefaultClientListener(virtualHostName, route.Name)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	err := xdsServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{endpoints},
		Clusters:  []*v3clusterpb.Cluster{cluster, edsCluster, logicalDNSCluster},
		Routes:    []*v3routepb.RouteConfiguration{route},
		Listeners: []*v3listenerpb.Listener{listener},
	})
	if err != nil {
		t.Fatalf("Failed to update xDS resources: %v", err)
	}

	dnsR := replaceDNSResolver(t)
	dnsR.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backends[0].Address}}})

	conn, err := grpc.NewClient("xds:///test.server", grpc.WithResolvers(xdsResolver), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create client: %s", err)
	}
	defer conn.Close()
	client := testgrpc.NewTestServiceClient(conn)

	gotPerBackend := checkRPCSendOK(t, ctx, client, 1)
	var got string
	for got = range gotPerBackend {
	}
	if want := backends[0].Address; got != want {
		t.Errorf("Got RPCs routed to an unexpected got: %v, want %v", got, want)
	}
}

// Tests that when an aggregate cluster is configured with ring hash policy, and
// it's first child is in transient failure, and the fallback is a logical DNS,
// the later recovers from transient failure when its backend becomes available.
func (s) TestRingHash_AggregateClusterFallBackFromRingHashToLogicalDnsAtStartupNoFailedRPCs(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L225
	xdsServer, nodeID, _, xdsResolver, stop := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer stop()

	const edsClusterName = "eds_cluster"
	const logicalDNSClusterName = "logical_dns_cluster"
	const clusterName = "aggregate_cluster"

	backends, stop := startTestServiceBackends(t, 1)
	defer stop()

	endpoints := e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
		ClusterName: edsClusterName,
		Localities: []e2e.LocalityOptions{{
			Name:     "locality0",
			Weight:   1,
			Backends: makeNonExistentBackends(t, 1),
			Priority: 0,
		}},
	})
	edsCluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: edsClusterName,
		ServiceName: edsClusterName,
	})

	logicalDNSCluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		Type:        e2e.ClusterTypeLogicalDNS,
		ClusterName: logicalDNSClusterName,
		// The DNS values are not used because we fake DNS later on, but they
		// are required to be present for the resource to be valid.
		DNSHostName: "server.example.com",
		DNSPort:     443,
	})
	cluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: clusterName,
		Type:        e2e.ClusterTypeAggregate,
		// TODO: when "A75: xDS Aggregate Cluster Behavior Fixes" is merged, the
		// policy will have to be set on the child clusters.
		Policy:     e2e.LoadBalancingPolicyRingHash,
		ChildNames: []string{edsClusterName, logicalDNSClusterName},
	})
	route := channelIDHashRoute("new_route", virtualHostName, clusterName)
	listener := e2e.DefaultClientListener(virtualHostName, route.Name)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	err := xdsServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{endpoints},
		Clusters:  []*v3clusterpb.Cluster{cluster, edsCluster, logicalDNSCluster},
		Routes:    []*v3routepb.RouteConfiguration{route},
		Listeners: []*v3listenerpb.Listener{listener},
	})
	if err != nil {
		t.Fatalf("Failed to update xDS resources: %v", err)
	}

	dnsR := replaceDNSResolver(t)
	dnsR.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: backends[0].Address}}})

	dialer := testutils.NewBlockingDialer()
	cp := grpc.ConnectParams{
		// Increase backoff time, so that subconns stay in TRANSIENT_FAILURE
		// for long enough to trigger potential problems.
		Backoff: backoff.Config{
			BaseDelay: defaultTestTimeout,
		},
		MinConnectTimeout: 0,
	}
	opts := []grpc.DialOption{
		grpc.WithResolvers(xdsResolver),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer.DialContext),
		grpc.WithConnectParams(cp)}
	conn, err := grpc.NewClient("xds:///test.server", opts...)
	if err != nil {
		t.Fatalf("Failed to create client: %s", err)
	}
	defer conn.Close()
	client := testgrpc.NewTestServiceClient(conn)

	errCh := make(chan error, 2)
	go func() {
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			errCh <- fmt.Errorf("first rpc UnaryCall() failed: %v", err)
			return
		}
		errCh <- nil
	}()

	testutils.AwaitState(ctx, t, conn, connectivity.Connecting)

	go func() {
		// Start a second RPC at this point, which should be queued as well.
		// This will fail if the priority policy fails to update the picker to
		// point to the LOGICAL_DNS child; if it leaves it pointing to the EDS
		// priority 1, then the RPC will fail, because all subchannels are in
		// transient failure.
		//
		// Note that sending only the first RPC does not catch this case,
		// because if the priority policy fails to update the picker, then the
		// pick for the first RPC will not be retried.
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			errCh <- fmt.Errorf("second UnaryCall() failed: %v", err)
			return
		}
		errCh <- nil
	}()

	// Allow the connection attempts to complete.
	dialer.Resume()

	// RPCs should complete successfully.
	for range []int{0, 1} {
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("Expected 2 rpc to succeed, but failed: %v", err)
			}
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for RPCs to complete")
		}
	}
}

// Tests that ring hash policy that hashes using channel id ensures all RPCs to
// go 1 particular backend.
func (s) TestRingHash_ChannelIdHashing(t *testing.T) {
	backends, stop := startTestServiceBackends(t, 4)
	defer stop()

	xdsServer, nodeID, _, xdsResolver, stop := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer stop()
	const clusterName = "cluster"
	endpoints := e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
		ClusterName: clusterName,
		Localities: []e2e.LocalityOptions{{
			Backends: backendOptions(t, backends),
			Weight:   1,
		}},
	})
	cluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: clusterName,
		ServiceName: clusterName,
		Policy:      e2e.LoadBalancingPolicyRingHash,
	})
	route := channelIDHashRoute("new_route", virtualHostName, clusterName)
	listener := e2e.DefaultClientListener(virtualHostName, route.Name)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	err := xdsServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{endpoints},
		Clusters:  []*v3clusterpb.Cluster{cluster},
		Routes:    []*v3routepb.RouteConfiguration{route},
		Listeners: []*v3listenerpb.Listener{listener},
	})
	if err != nil {
		t.Fatalf("Failed to update xDS resources: %v", err)
	}

	conn, err := grpc.NewClient("xds:///test.server", grpc.WithResolvers(xdsResolver), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create client: %s", err)
	}
	defer conn.Close()
	client := testgrpc.NewTestServiceClient(conn)

	received := checkRPCSendOK(t, ctx, client, 100)
	if len(received) != 1 {
		t.Errorf("Got RPCs routed to %v backends, want %v", len(received), 1)
	}
	var count int
	for _, count = range received {
	}
	if count != 100 {
		t.Errorf("Got %v RPCs routed to a backend, want %v", count, 100)
	}
}

// headerHashRoute creates a RouteConfiguration with a hash policy that uses the
// provided header.
func headerHashRoute(routeName, virtualHostName, clusterName, header string) *v3routepb.RouteConfiguration {
	route := e2e.DefaultRouteConfig(routeName, virtualHostName, clusterName)
	hashPolicy := v3routepb.RouteAction_HashPolicy{
		PolicySpecifier: &v3routepb.RouteAction_HashPolicy_Header_{
			Header: &v3routepb.RouteAction_HashPolicy_Header{
				HeaderName: header,
			},
		},
	}
	action := route.VirtualHosts[0].Routes[0].Action.(*v3routepb.Route_Route)
	action.Route.HashPolicy = []*v3routepb.RouteAction_HashPolicy{&hashPolicy}
	return route
}

// Tests that ring hash policy that hashes using a header value can spread RPCs
// across all the backends.
func (s) TestRingHash_HeaderHashing(t *testing.T) {
	backends, stop := startTestServiceBackends(t, 4)
	defer stop()

	// We must set the host name socket address in EDS, as the ring hash policy
	// uses it to construct the ring.
	host, _, err := net.SplitHostPort(backends[0].Address)
	if err != nil {
		t.Fatalf("Failed to split host and port from stubserver: %v", err)
	}

	xdsServer, nodeID, _, xdsResolver, stop := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer stop()
	const clusterName = "cluster"
	endpoints := e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
		ClusterName: clusterName,
		Host:        host,
		Localities: []e2e.LocalityOptions{{
			Backends: backendOptions(t, backends),
			Weight:   1,
		}},
	})
	cluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: clusterName,
		ServiceName: clusterName,
		Policy:      e2e.LoadBalancingPolicyRingHash,
	})
	route := headerHashRoute("new_route", virtualHostName, clusterName, "address_hash")
	listener := e2e.DefaultClientListener(virtualHostName, route.Name)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	err = xdsServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{endpoints},
		Clusters:  []*v3clusterpb.Cluster{cluster},
		Routes:    []*v3routepb.RouteConfiguration{route},
		Listeners: []*v3listenerpb.Listener{listener},
	})
	if err != nil {
		t.Fatalf("Failed to update xDS resources: %v", err)
	}

	conn, err := grpc.NewClient("xds:///test.server", grpc.WithResolvers(xdsResolver), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create client: %s", err)
	}
	defer conn.Close()
	client := testgrpc.NewTestServiceClient(conn)

	// Note each type of RPC contains a header value that will always be hashed
	// to a specific backend as the header value matches the value used to
	// create the entry in the ring.
	for _, backend := range backends {
		ctx := metadata.NewOutgoingContext(ctx, metadata.Pairs("address_hash", backend.Address+"_0"))
		reqPerBackend := checkRPCSendOK(t, ctx, client, 1)
		if reqPerBackend[backend.Address] != 1 {
			t.Errorf("Got RPC routed to backend %v, want %v", reqPerBackend, backend.Address)
		}
	}
}

// Tests that ring hash policy that hashes using a header value and regex
// rewrite to aggregate RPCs to 1 backend.
func (s) TestRingHash_HeaderHashingWithRegexRewrite(t *testing.T) {
	backends, stop := startTestServiceBackends(t, 4)
	defer stop()

	// We must set the host name socket address in EDS, as the ring hash policy
	// uses it to construct the ring.
	host, _, err := net.SplitHostPort(backends[0].Address)
	if err != nil {
		t.Fatalf("Failed to split host and port from stubserver: %v", err)
	}

	xdsServer, nodeID, _, xdsResolver, stop := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer stop()
	clusterName := "cluster"
	endpoints := e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
		ClusterName: clusterName,
		Host:        host,
		Localities: []e2e.LocalityOptions{{
			Backends: backendOptions(t, backends),
			Weight:   1,
		}},
	})
	cluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: clusterName,
		ServiceName: clusterName,
		Policy:      e2e.LoadBalancingPolicyRingHash,
	})
	route := headerHashRoute("new_route", virtualHostName, clusterName, "address_hash")
	action := route.VirtualHosts[0].Routes[0].Action.(*v3routepb.Route_Route)
	action.Route.HashPolicy[0].GetHeader().RegexRewrite = &v3matcherpb.RegexMatchAndSubstitute{
		Pattern: &v3matcherpb.RegexMatcher{
			EngineType: &v3matcherpb.RegexMatcher_GoogleRe2{},
			Regex:      "[0-9]+",
		},
		Substitution: "foo",
	}
	listener := e2e.DefaultClientListener(virtualHostName, route.Name)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	err = xdsServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{endpoints},
		Clusters:  []*v3clusterpb.Cluster{cluster},
		Routes:    []*v3routepb.RouteConfiguration{route},
		Listeners: []*v3listenerpb.Listener{listener},
	})
	if err != nil {
		t.Fatalf("Failed to update xDS resources: %v", err)
	}

	conn, err := grpc.NewClient("xds:///test.server", grpc.WithResolvers(xdsResolver), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create client: %s", err)
	}
	defer conn.Close()
	client := testgrpc.NewTestServiceClient(conn)

	// Note each type of RPC contains a header value that would always be hashed
	// to a specific backend as the header value matches the value used to
	// create the entry in the ring. However, the regex rewrites all numbers to
	// "foo", and header values only differ by numbers, so they all end up
	// hashing to the same value.
	gotPerBackend := make(map[string]int)
	for _, backend := range backends {
		ctx := metadata.NewOutgoingContext(ctx, metadata.Pairs("address_hash", backend.Address+"_0"))
		res := checkRPCSendOK(t, ctx, client, 100)
		for addr, count := range res {
			gotPerBackend[addr] += count
		}
	}
	if want := 1; len(gotPerBackend) != want {
		t.Errorf("Got RPCs routed to %v backends, want %v", len(gotPerBackend), want)
	}
	var got int
	for _, got = range gotPerBackend {
	}
	if want := 400; got != want {
		t.Errorf("Got %v RPCs routed to a backend, want %v", got, want)
	}
}

// computeIdealNumberOfRPCs computes the ideal number of RPCs to send so that
// we can observe an event happening with probability p, and the result will
// have value p with the given error tolerance.
//
// See https://github.com/grpc/grpc/blob/4f6e13bdda9e8c26d6027af97db4b368ca2b3069/test/cpp/end2end/xds/xds_end2end_test_lib.h#L941
// for an explanation of the formula.
func computeIdealNumberOfRPCs(t *testing.T, p, errorTolerance float64) int {
	if p < 0 || p > 1 {
		t.Fatal("p must be in (0, 1)")
	}
	numRPCs := math.Ceil(p * (1 - p) * 5. * 5. / errorTolerance / errorTolerance)
	return int(numRPCs + 1000.) // add 1k as a buffer to avoid flakyness.
}

// setRingHashLBPolicyWithHighMinRingSize sets the ring hash policy with a high
// minimum ring size to ensure that the ring is large enough to distribute
// requests more uniformly across endpoints when a random hash is used.
func setRingHashLBPolicyWithHighMinRingSize(t *testing.T, cluster *v3clusterpb.Cluster) {
	minRingSize := uint64(100000)
	oldVal := envconfig.RingHashCap
	envconfig.RingHashCap = minRingSize
	t.Cleanup(func() {
		envconfig.RingHashCap = oldVal
	})
	// Increasing min ring size for random distribution.
	config := testutils.MarshalAny(t, &v3ringhashpb.RingHash{
		HashFunction:    v3ringhashpb.RingHash_XX_HASH,
		MinimumRingSize: &wrapperspb.UInt64Value{Value: minRingSize},
	})
	cluster.LoadBalancingPolicy = &v3clusterpb.LoadBalancingPolicy{
		Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{{
			TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
				Name:        "envoy.load_balancing_policies.ring_hash",
				TypedConfig: config,
			},
		}},
	}
}

// Tests that ring hash policy that hashes using a random value.
func (s) TestRingHash_NoHashPolicy(t *testing.T) {
	backends, stop := startTestServiceBackends(t, 2)
	defer stop()
	numRPCs := computeIdealNumberOfRPCs(t, .5, errorTolerance)

	xdsServer, nodeID, _, xdsResolver, stop := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer stop()
	const clusterName = "cluster"
	endpoints := e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
		ClusterName: clusterName,
		Localities: []e2e.LocalityOptions{{
			Backends: backendOptions(t, backends),
			Weight:   1,
		}},
	})
	cluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: clusterName,
		ServiceName: clusterName,
	})
	setRingHashLBPolicyWithHighMinRingSize(t, cluster)
	route := e2e.DefaultRouteConfig("new_route", virtualHostName, clusterName)
	listener := e2e.DefaultClientListener(virtualHostName, route.Name)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	err := xdsServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{endpoints},
		Clusters:  []*v3clusterpb.Cluster{cluster},
		Routes:    []*v3routepb.RouteConfiguration{route},
		Listeners: []*v3listenerpb.Listener{listener},
	})
	if err != nil {
		t.Fatalf("Failed to update xDS resources: %v", err)
	}

	conn, err := grpc.NewClient("xds:///test.server", grpc.WithResolvers(xdsResolver), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create client: %s", err)
	}
	defer conn.Close()
	client := testgrpc.NewTestServiceClient(conn)

	// Send a large number of RPCs and check that they are distributed randomly.
	gotPerBackend := checkRPCSendOK(t, ctx, client, numRPCs)
	for _, backend := range backends {
		got := float64(gotPerBackend[backend.Address]) / float64(numRPCs)
		want := .5
		if !cmp.Equal(got, want, cmpopts.EquateApprox(0, errorTolerance)) {
			t.Errorf("Fraction of RPCs to backend %s: got %v, want %v (margin: +-%v)", backends[2].Address, got, want, errorTolerance)
		}
	}
}

// Tests that we observe endpoint weights.
func (s) TestRingHash_EndpointWeights(t *testing.T) {
	backends, stop := startTestServiceBackends(t, 3)
	defer stop()
	xdsServer, nodeID, _, xdsResolver, stop := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer stop()
	const clusterName = "cluster"
	backendOpts := []e2e.BackendOptions{
		{Port: testutils.ParsePort(t, backends[0].Address)},
		{Port: testutils.ParsePort(t, backends[1].Address)},
		{Port: testutils.ParsePort(t, backends[2].Address), Weight: 2},
	}

	endpoints := e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
		ClusterName: clusterName,
		Localities: []e2e.LocalityOptions{{
			Backends: backendOpts,
			Weight:   1,
		}},
	})
	endpoints.Endpoints[0].LbEndpoints[0].LoadBalancingWeight = wrapperspb.UInt32(uint32(1))
	endpoints.Endpoints[0].LbEndpoints[1].LoadBalancingWeight = wrapperspb.UInt32(uint32(1))
	endpoints.Endpoints[0].LbEndpoints[2].LoadBalancingWeight = wrapperspb.UInt32(uint32(2))
	cluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: clusterName,
		ServiceName: clusterName,
	})
	// Increasing min ring size for random distribution.
	setRingHashLBPolicyWithHighMinRingSize(t, cluster)
	route := e2e.DefaultRouteConfig("new_route", virtualHostName, clusterName)
	listener := e2e.DefaultClientListener(virtualHostName, route.Name)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	err := xdsServer.Update(ctx, e2e.UpdateOptions{
		NodeID:    nodeID,
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{endpoints},
		Clusters:  []*v3clusterpb.Cluster{cluster},
		Routes:    []*v3routepb.RouteConfiguration{route},
		Listeners: []*v3listenerpb.Listener{listener},
	})
	if err != nil {
		t.Fatalf("Failed to update xDS resources: %v", err)
	}

	conn, err := grpc.NewClient("xds:///test.server", grpc.WithResolvers(xdsResolver), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create client: %s", err)
	}
	defer conn.Close()
	client := testgrpc.NewTestServiceClient(conn)

	// Send a large number of RPCs and check that they are distributed randomly.
	numRPCs := computeIdealNumberOfRPCs(t, .25, errorTolerance)
	gotPerBackend := checkRPCSendOK(t, ctx, client, numRPCs)

	got := float64(gotPerBackend[backends[0].Address]) / float64(numRPCs)
	want := .25
	if !cmp.Equal(got, want, cmpopts.EquateApprox(0, errorTolerance)) {
		t.Errorf("Fraction of RPCs to backend %s: got %v, want %v (margin: +-%v)", backends[0].Address, got, want, errorTolerance)
	}
	got = float64(gotPerBackend[backends[1].Address]) / float64(numRPCs)
	if !cmp.Equal(got, want, cmpopts.EquateApprox(0, errorTolerance)) {
		t.Errorf("Fraction of RPCs to backend %s: got %v, want %v (margin: +-%v)", backends[1].Address, got, want, errorTolerance)
	}
	got = float64(gotPerBackend[backends[2].Address]) / float64(numRPCs)
	want = .50
	if !cmp.Equal(got, want, cmpopts.EquateApprox(0, errorTolerance)) {
		t.Errorf("Fraction of RPCs to backend %s: got %v, want %v (margin: +-%v)", backends[2].Address, got, want, errorTolerance)
	}
}
