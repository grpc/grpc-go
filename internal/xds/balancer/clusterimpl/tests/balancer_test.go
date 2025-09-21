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
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/fakeserver"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3pickfirstpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/pick_first/v3"
	v3lrspb "github.com/envoyproxy/go-control-plane/envoy/service/load_stats/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"

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
	var resolverBuilder resolver.Builder
	var err error
	if newResolver := internal.NewXDSResolverWithConfigForTesting; newResolver != nil {
		resolverBuilder, err = newResolver.(func([]byte) (resolver.Builder, error))(bc)
		if err != nil {
			t.Fatalf("Failed to create xDS resolver for testing: %v", err)
		}
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
	var resolverBuilder resolver.Builder
	var err error
	if newResolver := internal.NewXDSResolverWithConfigForTesting; newResolver != nil {
		resolverBuilder, err = newResolver.(func([]byte) (resolver.Builder, error))(bc)
		if err != nil {
			t.Fatalf("Failed to create xDS resolver for testing: %v", err)
		}
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
	// Create an xDS management server that serves ADS requests.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)

	// Create an xDS resolver with the above bootstrap configuration.
	var resolverBuilder resolver.Builder
	var err error
	if newResolver := internal.NewXDSResolverWithConfigForTesting; newResolver != nil {
		resolverBuilder, err = newResolver.(func([]byte) (resolver.Builder, error))(bc)
		if err != nil {
			t.Fatalf("Failed to create xDS resolver for testing: %v", err)
		}
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

	// Configure the xDS management server with default resources.
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

// Tests that circuit breaking limits RPCs in LOGICAL_DNS clusters E2E.
func (s) TestCircuitBreakingLogicalDNS(t *testing.T) {
	// Create an xDS management server that serves ADS requests.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	testutils.CreateBootstrapFileForTesting(t, bc)

	// Create an xDS resolver with the above bootstrap configuration.
	var resolverBuilder resolver.Builder
	var err error
	if newResolver := internal.NewXDSResolverWithConfigForTesting; newResolver != nil {
		resolverBuilder, err = newResolver.(func([]byte) (resolver.Builder, error))(bc)
		if err != nil {
			t.Fatalf("Failed to create xDS resolver for testing: %v", err)
		}
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

	// Since we are at the max, new streams should fail.  It's possible some are
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
	var resolverBuilder resolver.Builder
	var err error
	if newResolver := internal.NewXDSResolverWithConfigForTesting; newResolver != nil {
		resolverBuilder, err = newResolver.(func([]byte) (resolver.Builder, error))(bc)
		if err != nil {
			t.Fatalf("Failed to create xDS resolver for testing: %v", err)
		}
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
		clusterName       = "test-xds-service"
		logicalDNSCluster = "logical_dns_cluster"
		virtualHostName   = "test-route"
	)

	// Configure xDS resources.
	ldnsCluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		Type:        e2e.ClusterTypeLogicalDNS,
		ClusterName: logicalDNSCluster,
		DNSHostName: host,
		DNSPort:     port,
	})
	cluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: clusterName,
		Type:        e2e.ClusterTypeAggregate,
		ChildNames:  []string{logicalDNSCluster},
	})
	route := e2e.DefaultRouteConfig("test-route", virtualHostName, clusterName)
	listener := e2e.DefaultClientListener(virtualHostName, route.Name)
	updateOpts := e2e.UpdateOptions{
		NodeID:         nodeID,
		Endpoints:      nil,
		Clusters:       []*v3clusterpb.Cluster{cluster, ldnsCluster},
		Routes:         []*v3routepb.RouteConfiguration{route},
		Listeners:      []*v3listenerpb.Listener{listener},
		SkipValidation: true,
	}

	// Replace DNS resolver with a wrapped resolver to capture ResolveNow calls.
	resolveNowCh := make(chan struct{}, 1)
	dnsR := manual.NewBuilderWithScheme("dns")
	dnsResolverBuilder := resolver.Get("dns")
	resolver.Register(dnsR)
	t.Cleanup(func() { resolver.Register(dnsResolverBuilder) })
	dnsR.ResolveNowCallback = func(resolver.ResolveNowOptions) {
		close(resolveNowCh)
	}
	dnsR.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: fmt.Sprintf("%s:%d", host, port)}}})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, updateOpts); err != nil {
		t.Fatalf("Failed to update xDS resources: %v", err)
	}

	conn, err := grpc.NewClient("xds:///test-route", grpc.WithResolvers(resolverBuilder), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	conn.Connect()
	client := testgrpc.NewTestServiceClient(conn)

	// Verify initial RPC routes correctly to backend.
	testutils.AwaitState(ctx, t, conn, connectivity.Ready)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("client.EmptyCall() failed: %v", err)
	}

	// Stopping the server listener will close the transport on the client,
	// which will lead to the channel eventually moving to IDLE.
	lis.Stop()
	testutils.AwaitState(ctx, t, conn, connectivity.Idle)

	// Make an RPC to reconnect and thereby move to TRANSIENT_FAILURE upon
	// connection failure.
	client.EmptyCall(ctx, &testpb.Empty{})
	testutils.AwaitState(ctx, t, conn, connectivity.TransientFailure)

	// An RPC at this point is expected to fail.
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}); err == nil {
		t.Fatal("EmptyCall RPC succeeded when the channel is in TRANSIENT_FAILURE")
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

	// Ensure that the old LRS stream is not closed.
	if _, err := mgmtServer.LRSServer.LRSStreamCloseChan.Receive(ctx); err != nil {
		t.Fatalf("Error waiting for initial LRS stream close : %v", err)
	}

	// Also ensure that a new LRS stream is not created.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := mgmtServer.LRSServer.LRSRequestChan.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatalf("Expected no LRS reports after disable, but got: %v", err)
	}
}
