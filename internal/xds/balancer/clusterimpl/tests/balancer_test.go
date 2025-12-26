/*
 *
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
 *
 */

package clusterimpl_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/pickfirst"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/fakeserver"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3xdsxdstypepb "github.com/cncf/xds/go/xds/type/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3pickfirstpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/pick_first/v3"
	v3lrspb "github.com/envoyproxy/go-control-plane/envoy/service/load_stats/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/protobuf/types/known/structpb"

	_ "google.golang.org/grpc/xds"
)

const (
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 100 * time.Millisecond
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestConfigUpdateWithSameLoadReportingServerConfig tests the scenario where
// the clusterimpl LB policy receives a config update with no change in the load
// reporting server configuration. The test verifies that the existing load
// reporting stream is not terminated and that a new load reporting stream is not
// created.
func (s) TestConfigUpdateWithSameLoadReportingServerConfig(t *testing.T) {
	// Create an xDS management server that serves ADS and LRS requests.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)

	// Create an xDS resolver with the above bootstrap configuration.
	if internal.NewXDSResolverWithConfigForTesting == nil {
		t.Fatalf("internal.NewXDSResolverWithConfigForTesting is nil")
	}
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a server backend exposing the test service.
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	// Configure the xDS management server with default resources. Override the
	// default cluster to include an LRS server config pointing to self.
	const serviceName = "my-test-xds-service"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	resources.Clusters[0].LrsServer = &v3corepb.ConfigSource{
		ConfigSourceSpecifier: &v3corepb.ConfigSource_Self{
			Self: &v3corepb.SelfConfigSource{},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// Ensure that an LRS stream is created.
	if _, err := mgmtServer.LRSServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Failure when waiting for an LRS stream to be opened: %v", err)
	}

	// Configure a new resource on the management server with drop config that
	// drops all RPCs, but with no change in the load reporting server config.
	resources.Endpoints = []*v3endpointpb.ClusterLoadAssignment{
		e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
			ClusterName: "endpoints-" + serviceName,
			Host:        "localhost",
			Localities: []e2e.LocalityOptions{
				{
					Backends: []e2e.BackendOptions{{Ports: []uint32{testutils.ParsePort(t, server.Address)}}},
					Weight:   1,
				},
			},
			DropPercents: map[string]int{"test-drop-everything": 100},
		}),
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Repeatedly send RPCs until we sees that they are getting dropped, or the
	// test context deadline expires. The former indicates that new config with
	// drops has been applied.
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		_, err := client.EmptyCall(ctx, &testpb.Empty{})
		if err != nil && status.Code(err) == codes.Unavailable && strings.Contains(err.Error(), "RPC is dropped") {
			break
		}
	}
	if ctx.Err() != nil {
		t.Fatalf("Timeout when waiting for RPCs to be dropped after config update")
	}

	// Ensure that the old LRS stream is not closed.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := mgmtServer.LRSServer.LRSStreamCloseChan.Receive(sCtx); err == nil {
		t.Fatal("LRS stream closed when expected not to")
	}

	// Also ensure that a new LRS stream is not created.
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := mgmtServer.LRSServer.LRSStreamOpenChan.Receive(sCtx); err == nil {
		t.Fatal("New LRS stream created when expected not to")
	}
}

// Tests whether load is reported correctly when using pickfirst with endpoints
// in multiple localities.
func (s) TestLoadReportingPickFirstMultiLocality(t *testing.T) {
	// Create an xDS management server that serves ADS and LRS requests.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)

	// Create an xDS resolver with the above bootstrap configuration.
	if internal.NewXDSResolverWithConfigForTesting == nil {
		t.Fatalf("internal.NewXDSResolverWithConfigForTesting is nil")
	}
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start two server backends exposing the test service.
	server1 := stubserver.StartTestService(t, nil)
	defer server1.Stop()

	server2 := stubserver.StartTestService(t, nil)
	defer server2.Stop()

	// Configure the xDS management server.
	const serviceName = "my-test-xds-service"
	routeConfigName := "route-" + serviceName
	clusterName := "cluster-" + serviceName
	endpointsName := "endpoints-" + serviceName
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, serviceName, clusterName)},
		Clusters: []*v3clusterpb.Cluster{
			{
				Name:                 clusterName,
				ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
				EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
					EdsConfig: &v3corepb.ConfigSource{
						ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
							Ads: &v3corepb.AggregatedConfigSource{},
						},
					},
					ServiceName: endpointsName,
				},
				// Specify a custom load balancing policy to use pickfirst.
				LoadBalancingPolicy: &v3clusterpb.LoadBalancingPolicy{
					Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
						{
							TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
								TypedConfig: testutils.MarshalAny(t, &v3pickfirstpb.PickFirst{}),
							},
						},
					},
				},
				// Include a fake LRS server config pointing to self.
				LrsServer: &v3corepb.ConfigSource{
					ConfigSourceSpecifier: &v3corepb.ConfigSource_Self{
						Self: &v3corepb.SelfConfigSource{},
					},
				},
			},
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
			ClusterName: endpointsName,
			Host:        "localhost",
			Localities: []e2e.LocalityOptions{
				{
					Backends: []e2e.BackendOptions{
						{Ports: []uint32{testutils.ParsePort(t, server1.Address)}},
					},
					Weight: 1,
				},
				{
					Backends: []e2e.BackendOptions{
						{Ports: []uint32{testutils.ParsePort(t, server2.Address)}},
					},
					Weight: 2,
				},
			},
		})},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	var peer peer.Peer
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// Verify that the request was sent to server 1.
	if got, want := peer.Addr.String(), server1.Address; got != want {
		t.Errorf("peer.Addr = %q, want = %q", got, want)
	}

	// Ensure that an LRS stream is created.
	if _, err = mgmtServer.LRSServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Failure when waiting for an LRS stream to be opened: %v", err)
	}

	// Handle the initial LRS request from the xDS client.
	if _, err = mgmtServer.LRSServer.LRSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Failure waiting for initial LRS request: %v", err)
	}

	resp := fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: durationpb.New(10 * time.Millisecond),
		},
	}
	mgmtServer.LRSServer.LRSResponseChan <- &resp

	// Wait for load to be reported for locality of server 1.
	if err := waitForSuccessfulLoadReport(ctx, mgmtServer.LRSServer, "region-1"); err != nil {
		t.Fatalf("Server 1 did not receive load due to error: %v", err)
	}

	// Stop server 1 and send one more rpc. Now the request should go to server 2.
	server1.Stop()

	// Wait for the balancer to pick up the server state change.
	testutils.AwaitState(ctx, t, cc, connectivity.Idle)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// Verify that the request was sent to server 2.
	if got, want := peer.Addr.String(), server2.Address; got != want {
		t.Errorf("peer.Addr = %q, want = %q", got, want)
	}

	// Wait for load to be reported for locality of server 2.
	if err := waitForSuccessfulLoadReport(ctx, mgmtServer.LRSServer, "region-2"); err != nil {
		t.Fatalf("Server 2 did not receive load due to error: %v", err)
	}
}

// waitForSuccessfulLoadReport waits for a successful request to be reported for
// the specified locality region.
func waitForSuccessfulLoadReport(ctx context.Context, lrsServer *fakeserver.Server, region string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case req := <-lrsServer.LRSRequestChan.C:
			loadStats := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest)
			for _, load := range loadStats.ClusterStats {
				for _, locality := range load.UpstreamLocalityStats {
					if locality.TotalSuccessfulRequests > 0 && locality.Locality.Region == region {
						return nil
					}
				}
			}
		}
	}
}

// Tests that circuit breaking limits RPCs E2E.
func (s) TestCircuitBreaking(t *testing.T) {
	// Create an xDS management server that serves ADS and LRS requests.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)

	// Create an xDS resolver with the above bootstrap configuration.
	if internal.NewXDSResolverWithConfigForTesting == nil {
		t.Fatalf("internal.NewXDSResolverWithConfigForTesting is nil")
	}
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a server backend exposing the test service.
	f := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				if _, err := stream.Recv(); err != nil {
					return err
				}
			}
		},
	}
	server := stubserver.StartTestService(t, f)
	defer server.Stop()

	// Configure xDS resources on the management server with a circuit breaking
	// policy that limits the maximum number of concurrent requests to 3.
	const serviceName = "my-test-xds-service"
	const maxRequests = 3
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
	})
	resources.Clusters[0].CircuitBreakers = &v3clusterpb.CircuitBreakers{
		Thresholds: []*v3clusterpb.CircuitBreakers_Thresholds{
			{
				Priority:    v3corepb.RoutingPriority_DEFAULT,
				MaxRequests: wrapperspb.UInt32(maxRequests),
			},
		},
	}
	resources.Clusters[0].LrsServer = &v3corepb.ConfigSource{
		ConfigSourceSpecifier: &v3corepb.ConfigSource_Self{
			Self: &v3corepb.SelfConfigSource{},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// Start maxRequests streams.
	for range maxRequests {
		if _, err := client.FullDuplexCall(ctx); err != nil {
			t.Fatalf("rpc FullDuplexCall() failed: %v", err)
		}
	}

	// Since we are at the max, new streams should fail.  It's possible some are
	// allowed due to inherent raciness in the tracking, however.
	const droppedRPCCount = 100
	for i := 0; i < droppedRPCCount; i++ {
		if ctx.Err() != nil {
			t.Fatalf("Context error: %v", ctx.Err())
		}
		if _, err := client.FullDuplexCall(ctx); status.Code(err) != codes.Unavailable {
			t.Fatalf("client.FullDuplexCall() returned status %q, want %q", status.Code(err), codes.Unavailable)
		}
	}

	if _, err = mgmtServer.LRSServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Failure when waiting for an LRS stream to be opened: %v", err)
	}

	if _, err = mgmtServer.LRSServer.LRSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Failure waiting for initial LRS request: %v", err)
	}

	mgmtServer.LRSServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: durationpb.New(10 * time.Millisecond),
		},
	}

	select {
	case req := <-mgmtServer.LRSServer.LRSRequestChan.C:
		clusterStats := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest).ClusterStats
		if l := len(clusterStats); l != 1 {
			t.Fatalf("Received load for %d clusters, want 1", l)
		}
		clusterStats[0].LoadReportInterval = nil
		wantLoad := &v3endpointpb.ClusterStats{
			ClusterName:        "cluster-my-test-xds-service",
			ClusterServiceName: "endpoints-my-test-xds-service",
			UpstreamLocalityStats: []*v3endpointpb.UpstreamLocalityStats{
				{
					Locality:                &v3corepb.Locality{Region: "region-1", Zone: "zone-1", SubZone: "subzone-1"},
					TotalIssuedRequests:     maxRequests,
					TotalRequestsInProgress: maxRequests,
				},
			},
			TotalDroppedRequests: droppedRPCCount,
		}
		if diff := cmp.Diff(wantLoad, clusterStats[0], protocmp.Transform()); diff != "" {
			t.Errorf("Failed with unexpected diff (-want +got):\n%s", diff)
		}
	case <-ctx.Done():
		t.Fatalf("Timeout while waiting for load report on LRS stream: %v", ctx.Err())
	}
}

// ComputeIdealNumRpcs calculates the ideal number of RPCs to send to test
// probabilistic behaviors.
//
// It's based on the idea of a binomial distribution approximated by a normal
// distribution. The function aims to find a number of RPCs such that
// the observed probability is within a certain error_tolerance of the expected
// probability (p).
func computeIdealNumRpcs(p, errorTolerance float64) uint64 {
	return uint64(math.Ceil(p * (1 - p) * 5.00 * 5.00 / errorTolerance / errorTolerance))
}

func verifyDropRateByCategory(ctx context.Context, client testgrpc.TestServiceClient, rpcCount int, lrsServer *fakeserver.Server, wantCluster, wantDropCategory string, wantDropRate, errorTolerance float64) error {
	for i := 0; i < rpcCount; i++ {
		if ctx.Err() != nil {
			return fmt.Errorf("context error: %v", ctx.Err())
		}
		stream, err := client.FullDuplexCall(ctx)
		switch {
		case err == nil:
			// Close stream to avoid exceeding limits.
			stream.CloseSend()
		case status.Code(err) == codes.Unavailable && strings.Contains(err.Error(), "RPC is dropped"):
			continue
		default:
			return fmt.Errorf("client.FullDuplexCall(_) failed with unexpected error = %v", err)
		}
	}

	for {
		if ctx.Err() != nil {
			return fmt.Errorf("timeout when waiting for load stats")
		}
		req, err := lrsServer.LRSRequestChan.Receive(ctx)
		if err != nil {
			continue
		}
		loadStats := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest).ClusterStats
		if len(loadStats) != 1 || loadStats[0].ClusterName != wantCluster {
			continue
		}
		if loadStats[0].ClusterName != wantCluster {
			return fmt.Errorf("Received stats for unexpected cluster got: %q, want: %q", loadStats[0].ClusterName, wantCluster)
		}

		for _, cs := range loadStats {
			found := false
			for _, dr := range cs.DroppedRequests {
				if dr.Category == wantDropCategory {
					found = true
					gotRPCDropRate := float64(dr.DroppedCount) / float64(rpcCount)
					if (math.Trunc(math.Abs(gotRPCDropRate-wantDropRate)*100) / 100) > errorTolerance {
						return fmt.Errorf("Drop rate goes out of errortolerance got: %v, want: %v, totalDroppedRequest: %v, totalIssuesRequest: %v", (math.Trunc(math.Abs(gotRPCDropRate-wantDropRate)*100) / 100), errorTolerance, cs.TotalDroppedRequests, cs.UpstreamLocalityStats[0].TotalIssuedRequests)
					}
				}
			}
			if !found {
				continue
			}
		}
		break
	}
	return nil
}

// TestDropByCategory verifies that the balancer correctly drops the picks, and
// that the drops are reported.
func (s) TestDropByCategory(t *testing.T) {
	// Create an xDS management server that serves ADS and LRS requests.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)

	// Create an xDS resolver with the above bootstrap configuration.
	if internal.NewXDSResolverWithConfigForTesting == nil {
		t.Fatalf("internal.NewXDSResolverWithConfigForTesting is nil")
	}
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a server backend exposing the test service.
	f := &stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				if _, err := stream.Recv(); err != nil {
					return err
				}
			}
		},
	}
	server := stubserver.StartTestService(t, f)
	defer server.Stop()

	// Configure xDS resources on the management server with drops
	// configuration that drops one RPC for every 50 RPCs made.
	const (
		dropReason      = "test-dropping-category"
		dropNumerator   = 1
		dropDenominator = 50
		serviceName     = "my-test-xds-service"
		errorTolerance  = 0.05
	)
	wantRPCDropRate := float64(dropNumerator) / float64(dropDenominator)
	rpcCount := computeIdealNumRpcs(wantRPCDropRate, errorTolerance)
	t.Logf("About to send %d RPCs to test drop rate of %v", rpcCount, wantRPCDropRate)

	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
	})
	resources.Clusters[0].LrsServer = &v3corepb.ConfigSource{
		ConfigSourceSpecifier: &v3corepb.ConfigSource_Self{
			Self: &v3corepb.SelfConfigSource{},
		},
	}
	resources.Endpoints = []*v3endpointpb.ClusterLoadAssignment{
		e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
			ClusterName: "endpoints-" + serviceName,
			Host:        "localhost",
			Localities: []e2e.LocalityOptions{
				{
					Backends: []e2e.BackendOptions{{Ports: []uint32{testutils.ParsePort(t, server.Address)}}},
					Weight:   1,
				},
			},
			DropPercents: map[string]int{
				dropReason: dropNumerator * 100 / dropDenominator,
			},
		})}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	// Ensure the gRPC channel is READY before issuing RPCs to get accurate
	// drop count.
	cc.Connect()
	client := testgrpc.NewTestServiceClient(cc)
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	if _, err = mgmtServer.LRSServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Failure when waiting for an LRS stream to be opened: %v", err)
	}

	resp := fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			Clusters:              []string{resources.Clusters[0].Name},
			LoadReportingInterval: durationpb.New(50 * time.Millisecond),
		},
	}
	mgmtServer.LRSServer.LRSResponseChan <- &resp

	if err := verifyDropRateByCategory(ctx, client, int(rpcCount), mgmtServer.LRSServer, resources.Clusters[0].Name, dropReason, wantRPCDropRate, errorTolerance); err != nil {
		t.Fatal(err)
	}

	// Update the drop configuration to drop 1 out of every 40 RPCs,
	// and verify that the observed drop rate matches this new config.
	const (
		dropReason2      = "test-dropping-category-2"
		dropNumerator2   = 1
		dropDenominator2 = 40
	)

	resources.Endpoints = []*v3endpointpb.ClusterLoadAssignment{
		e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
			ClusterName: "endpoints-" + serviceName,
			Host:        "localhost",
			Localities: []e2e.LocalityOptions{
				{
					Backends: []e2e.BackendOptions{{Ports: []uint32{testutils.ParsePort(t, server.Address)}}},
					Weight:   1,
				},
			},
			DropPercents: map[string]int{
				dropReason2: dropNumerator2 * 100 / dropDenominator2, // drops one RPC for every 40 RPCs made
			},
		}),
	}

	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	wantRPCDropRate = float64(dropNumerator2) / float64(dropDenominator2)
	rpcCount = computeIdealNumRpcs(wantRPCDropRate, errorTolerance)
	t.Logf("About to send %d RPCs to test drop rate of %v", rpcCount, wantRPCDropRate)

	if err := verifyDropRateByCategory(ctx, client, int(rpcCount), mgmtServer.LRSServer, resources.Clusters[0].Name, dropReason2, wantRPCDropRate, errorTolerance); err != nil {
		t.Fatal(err)
	}
}

// Tests that circuit breaking limits RPCs in LOGICAL_DNS clusters E2E.
func (s) TestCircuitBreakingLogicalDNS(t *testing.T) {
	// Create an xDS management server that serves ADS requests.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)

	// Create an xDS resolver with the above bootstrap configuration.
	if internal.NewXDSResolverWithConfigForTesting == nil {
		t.Fatalf("internal.NewXDSResolverWithConfigForTesting is nil")
	}
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a server backend exposing the test service.
	f := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				if _, err := stream.Recv(); err != nil {
					return err
				}
			}
		},
	}
	server := stubserver.StartTestService(t, f)
	defer server.Stop()
	host, port := hostAndPortFromAddress(t, server.Address)

	// Configure the xDS management server with default resources. Override the
	// default cluster to include a circuit breaking config.
	const serviceName = "my-test-xds-service"
	const maxRequests = 3
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
	})
	resources.Clusters = []*v3clusterpb.Cluster{
		e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
			ClusterName: "cluster-" + serviceName,
			Type:        e2e.ClusterTypeLogicalDNS,
			DNSHostName: host,
			DNSPort:     port,
		}),
	}
	resources.Clusters[0].CircuitBreakers = &v3clusterpb.CircuitBreakers{
		Thresholds: []*v3clusterpb.CircuitBreakers_Thresholds{
			{
				Priority:    v3corepb.RoutingPriority_DEFAULT,
				MaxRequests: wrapperspb.UInt32(maxRequests),
			},
		},
	}
	resources.Endpoints = nil

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// Start maxRequests streams.
	for range maxRequests {
		if _, err := client.FullDuplexCall(ctx); err != nil {
			t.Fatalf("rpc FullDuplexCall() failed: %v", err)
		}
	}

	// Since we are at the max, new streams should fail. It's possible some are
	// allowed due to inherent raciness in the tracking, however.
	for i := 0; i < 100; i++ {
		stream, err := client.FullDuplexCall(ctx)
		if status.Code(err) == codes.Unavailable {
			return
		}
		if err == nil {
			// Terminate the stream (the server immediately exits upon a client
			// CloseSend) to ensure we never go over the limit.
			stream.CloseSend()
			stream.Recv()
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("RPCs unexpectedly allowed beyond circuit breaking maximum")
}

func hostAndPortFromAddress(t *testing.T, addr string) (string, uint32) {
	t.Helper()

	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("Invalid serving address: %v", addr)
	}
	port, err := strconv.ParseUint(p, 10, 32)
	if err != nil {
		t.Fatalf("Invalid serving port %q: %v", p, err)
	}
	return host, uint32(port)
}

// Tests that LRS works correctly in a LOGICAL_DNS cluster.
func (s) TestLRSLogicalDNS(t *testing.T) {
	// Create an xDS management server that serves ADS and LRS requests.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)

	// Create an xDS resolver with the above bootstrap configuration.
	if internal.NewXDSResolverWithConfigForTesting == nil {
		t.Fatalf("internal.NewXDSResolverWithConfigForTesting is nil")
	}
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a server backend exposing the test service.
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()
	host, port := hostAndPortFromAddress(t, server.Address)

	// Configure the xDS management server with default resources. Override the
	// default cluster to include an LRS server config pointing to self.
	const serviceName = "my-test-xds-service"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	resources.Clusters = []*v3clusterpb.Cluster{
		e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
			ClusterName: "cluster-" + serviceName,
			Type:        e2e.ClusterTypeLogicalDNS,
			DNSHostName: host,
			DNSPort:     port,
		}),
	}
	resources.Clusters[0].LrsServer = &v3corepb.ConfigSource{
		ConfigSourceSpecifier: &v3corepb.ConfigSource_Self{
			Self: &v3corepb.SelfConfigSource{},
		},
	}
	resources.Endpoints = nil
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// Ensure that an LRS stream is created.
	if _, err := mgmtServer.LRSServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Failure when waiting for an LRS stream to be opened: %v", err)
	}

	// Handle the initial LRS request from the xDS client.
	if _, err = mgmtServer.LRSServer.LRSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Failure waiting for initial LRS request: %v", err)
	}

	resp := fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: durationpb.New(10 * time.Millisecond),
		},
	}
	mgmtServer.LRSServer.LRSResponseChan <- &resp

	// Wait for load to be reported for locality of server 1.
	if err := waitForSuccessfulLoadReport(ctx, mgmtServer.LRSServer, ""); err != nil {
		t.Fatalf("Server did not receive load due to error: %v", err)
	}
}

// TestReResolution verifies that when a SubConn turns transient failure,
// re-resolution is triggered.
func (s) TestReResolutionAfterTransientFailure(t *testing.T) {
	// Create an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer mgmtServer.Stop()

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)

	// Create an xDS resolver with the above bootstrap configuration.
	if internal.NewXDSResolverWithConfigForTesting == nil {
		t.Fatalf("internal.NewXDSResolverWithConfigForTesting is nil")
	}
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Create a restartable listener to simulate server being down.
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)
	server := stubserver.StartTestService(t, &stubserver.StubServer{
		Listener:   lis,
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
	})
	defer server.Stop()
	host, port := hostAndPortFromAddress(t, server.Address)

	const (
		listenerName = "test-listener"
		routeName    = "test-route"
		clusterName  = "test-aggregate-cluster"
		dnsCluster   = "logical-dns-cluster"
	)

	// Configure xDS resources.
	ldnsCluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		Type:        e2e.ClusterTypeLogicalDNS,
		ClusterName: dnsCluster,
		DNSHostName: host,
		DNSPort:     port,
	})
	cluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: clusterName,
		Type:        e2e.ClusterTypeAggregate,
		ChildNames:  []string{dnsCluster},
	})
	updateOpts := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(listenerName, routeName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeName, listenerName, clusterName)},
		Clusters:  []*v3clusterpb.Cluster{cluster, ldnsCluster},
		Endpoints: nil,
	}

	// Replace DNS resolver with a wrapped resolver to capture ResolveNow calls.
	resolveNowCh := make(chan struct{}, 3)
	dnsR := manual.NewBuilderWithScheme("dns")
	dnsResolverBuilder := resolver.Get("dns")
	resolver.Register(dnsR)
	defer resolver.Register(dnsResolverBuilder)
	dnsR.ResolveNowCallback = func(resolver.ResolveNowOptions) {
		resolveNowCh <- struct{}{}
	}
	dnsR.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: fmt.Sprintf("%s:%d", host, port)}}})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, updateOpts); err != nil {
		t.Fatalf("Failed to update xDS resources: %v", err)
	}

	conn, err := grpc.NewClient(fmt.Sprintf("xds:///%s", listenerName), grpc.WithResolvers(resolverBuilder), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	client := testgrpc.NewTestServiceClient(conn)

	// Verify initial RPC routes correctly to backend.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("client.EmptyCall() failed: %v", err)
	}

	// Stopping the server listener will close the transport on the client,
	// which will lead to the channel eventually moving to IDLE.
	lis.Stop()
	testutils.AwaitState(ctx, t, conn, connectivity.Idle)

	// An RPC at this point is expected to fail with TRANSIENT_FAILURE.
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("EmptyCall RPC succeeded when the channel is in TRANSIENT_FAILURE, got %v want %v", err, codes.Unavailable)
	}

	// Expect resolver's ResolveNow to be called due to TF state.
	select {
	case <-resolveNowCh:
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for ResolveNow call after TransientFailure")
	}

	// Restart the listener and expected to reconnect on its own and come out
	// of TRANSIENT_FAILURE, even without an RPC attempt.
	lis.Restart()
	testutils.AwaitState(ctx, t, conn, connectivity.Ready)

	// An RPC at this point is expected to succeed.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("client.EmptyCall() failed: %v", err)
	}
}

// TestUpdateLRSServerToNil verifies that updating the cluster's LRS server
// config from 'Self' to nil correctly closes the LRS stream and ensures no
// more LRS reports are sent.
func (s) TestUpdateLRSServerToNil(t *testing.T) {
	// Create an xDS management server that serves ADS and LRS requests.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})
	defer mgmtServer.Stop()

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)

	// Create an xDS resolver with the above bootstrap configuration.
	if internal.NewXDSResolverWithConfigForTesting == nil {
		t.Fatalf("internal.NewXDSResolverWithConfigForTesting is nil")
	}
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a server backend exposing the test service.
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	// Configure the xDS management server with default resources.
	const serviceName = "my-test-xds-service"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	resources.Clusters[0].LrsServer = &v3corepb.ConfigSource{
		ConfigSourceSpecifier: &v3corepb.ConfigSource_Self{
			Self: &v3corepb.SelfConfigSource{},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("client.EmptyCall() failed: %v", err)
	}

	// Ensure that an LRS stream is created.
	if _, err := mgmtServer.LRSServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Error waiting for initial LRS stream open: %v", err)
	}
	if _, err := mgmtServer.LRSServer.LRSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Error waiting for initial LRS report: %v", err)
	}

	// Update LRS Server to nil
	resources.Clusters[0].LrsServer = nil
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Ensure that the old LRS stream is closed.
	if _, err := mgmtServer.LRSServer.LRSStreamCloseChan.Receive(ctx); err != nil {
		t.Fatalf("Error waiting for initial LRS stream close : %v", err)
	}

	// Also ensure that a new LRS stream is not created.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := mgmtServer.LRSServer.LRSRequestChan.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("Expected no LRS reports after disable, got %v want %v", err, context.DeadlineExceeded)
	}
}

// Test verifies that child policy was updated on receipt of
// configuration update.
func (s) TestChildPolicyChangeOnConfigUpdate(t *testing.T) {
	const customLBPolicy = "test_custom_lb_policy"

	// Create an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer mgmtServer.Stop()

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)

	// Create an xDS resolver with the above bootstrap configuration.
	if internal.NewXDSResolverWithConfigForTesting == nil {
		t.Fatalf("internal.NewXDSResolverWithConfigForTesting is nil")
	}
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a server backend exposing the test service.
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	const serviceName = "test-child-policy"

	// Configure the xDS management server with default resources. Cluster
	// corresponding to this resource will be configured with "round_robin"
	// as the endpoint picking policy
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update xDS resources: %v", err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithResolvers(resolverBuilder), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("client.EmptyCall() failed: %v", err)
	}

	// Register stub customLBPolicy LB policy so that we can catch config changes.
	pfBuilder := balancer.Get(pickfirst.Name)
	lbCfgCh := make(chan serviceconfig.LoadBalancingConfig, 1)
	var updatedChildPolicy atomic.Value

	stub.Register(customLBPolicy, stub.BalancerFuncs{
		ParseConfig: func(lbCfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return pfBuilder.(balancer.ConfigParser).ParseConfig(lbCfg)
		},
		Init: func(bd *stub.BalancerData) {
			bd.ChildBalancer = pfBuilder.Build(bd.ClientConn, bd.BuildOptions)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			// name := customLBPolicy
			updatedChildPolicy.Store(customLBPolicy)
			select {
			case lbCfgCh <- ccs.BalancerConfig:
			case <-ctx.Done():
				t.Error("Timed out while waiting for BalancerConfig, context deadline exceeded")
			}
			return bd.ChildBalancer.UpdateClientConnState(ccs)
		},
		Close: func(bd *stub.BalancerData) {
			bd.ChildBalancer.Close()
		},
	})

	defer internal.BalancerUnregister(customLBPolicy)

	// Update the cluster to use customLBPolicy as the endpoint picking policy.
	resources.Clusters[0].LoadBalancingPolicy = &v3clusterpb.LoadBalancingPolicy{
		Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{{
			TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
				TypedConfig: testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
					TypeUrl: "type.googleapis.com/" + customLBPolicy,
					Value:   &structpb.Struct{},
				}),
			},
		}},
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for child policy config")
	case <-lbCfgCh:
	}

	if p, ok := updatedChildPolicy.Load().(string); !ok || p != customLBPolicy {
		t.Fatalf("Unexpected child policy after config update, got %q (ok: %v), want %q", p, ok, customLBPolicy)
	}

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for successful RPC after policy update.")
		case <-ticker.C:
			if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err == nil {
				return
			}
		}
	}
}

// Test verifies that config update fails if child policy config
// failed to parse.
func (s) TestFailedToParseChildPolicyConfig(t *testing.T) {
	// Create an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})
	defer mgmtServer.Stop()

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)

	// Create an xDS resolver with the above bootstrap configuration.
	if internal.NewXDSResolverWithConfigForTesting == nil {
		t.Fatalf("internal.NewXDSResolverWithConfigForTesting is nil")
	}
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start a server backend exposing the test service.
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	const serviceName = "test-child-policy"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
	})
	resources.Clusters[0].LoadBalancingPolicy = &v3clusterpb.LoadBalancingPolicy{
		Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{{
			TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
				TypedConfig: testutils.MarshalAny(t, &v3pickfirstpb.PickFirst{}),
			},
		}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update xDS resources: %v", err)
	}

	// Register stub pickfirst LB policy so that we can catch parsing errors.
	pfBuilder := balancer.Get(pickfirst.Name)
	internal.BalancerUnregister(pfBuilder.Name())
	const parseConfigError = "failed to parse config"
	stub.Register(pfBuilder.Name(), stub.BalancerFuncs{
		ParseConfig: func(_ json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return nil, errors.New(parseConfigError)
		},
	})
	defer balancer.Register(pfBuilder)

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithResolvers(resolverBuilder), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err == nil || !strings.Contains(err.Error(), parseConfigError) {
		t.Fatal("EmptyCall RPC succeeded when expected to fail")
	}
}
