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

package xds_test

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/rls"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/protobuf/types/known/durationpb"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	rlspb "google.golang.org/grpc/internal/proto/grpc_lookup_v1"
	testgrpc "google.golang.org/grpc/test/grpc_testing"
	testpb "google.golang.org/grpc/test/grpc_testing"

	_ "google.golang.org/grpc/balancer/rls" // Register the RLS Load Balancing policy.
)

// defaultClientResourcesWithRLSCSP returns a set of resources (LDS, RDS, CDS, EDS) for a
// client to connect to a server with a RLS Load Balancer as a child of Cluster Manager.
func defaultClientResourcesWithRLSCSP(lb e2e.LoadBalancingPolicy, params e2e.ResourceParams, rlsProto *rlspb.RouteLookupConfig) e2e.UpdateOptions {
	routeConfigName := "route-" + params.DialTarget
	clusterName := "cluster-" + params.DialTarget
	endpointsName := "endpoints-" + params.DialTarget
	return e2e.UpdateOptions{
		NodeID:    params.NodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(params.DialTarget, routeConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{defaultRouteConfigWithRLSCSP(routeConfigName, params.DialTarget, rlsProto)},
		Clusters: []*v3clusterpb.Cluster{e2e.ClusterResourceWithOptions(&e2e.ClusterOptions{
			ClusterName:   clusterName,
			ServiceName:   endpointsName,
			Policy:        lb,
			SecurityLevel: params.SecLevel,
		})},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(endpointsName, params.Host, []uint32{params.Port})},
	}
}

// defaultRouteConfigWithRLSCSP returns a basic xds RouteConfig resource with an
// RLS Cluster Specifier Plugin configured as the route.
func defaultRouteConfigWithRLSCSP(routeName, ldsTarget string, rlsProto *rlspb.RouteLookupConfig) *v3routepb.RouteConfiguration {
	return &v3routepb.RouteConfiguration{
		Name: routeName,
		ClusterSpecifierPlugins: []*v3routepb.ClusterSpecifierPlugin{
			{
				Extension: &v3corepb.TypedExtensionConfig{
					Name: "rls-csp",
					TypedConfig: testutils.MarshalAny(&rlspb.RouteLookupClusterSpecifier{
						RouteLookupConfig: rlsProto,
					}),
				},
			},
		},
		VirtualHosts: []*v3routepb.VirtualHost{{
			Domains: []string{ldsTarget},
			Routes: []*v3routepb.Route{{
				Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
				Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
					ClusterSpecifier: &v3routepb.RouteAction_ClusterSpecifierPlugin{ClusterSpecifierPlugin: "rls-csp"},
				}},
			}},
		}},
	}
}

// TestRLSinxDS tests an xDS configured system with an RLS Balancer present.
//
// This test sets up the RLS Balancer using the RLS Cluster Specifier Plugin,
// spins up a test service and has a fake RLS Server correctly respond with a
// target corresponding to this test service. This test asserts an RPC proceeds
// as normal with the RLS Balancer as part of system.
func (s) TestRLSinxDS(t *testing.T) {
	tests := []struct {
		name     string
		lbPolicy e2e.LoadBalancingPolicy
	}{
		{
			name:     "roundrobin",
			lbPolicy: e2e.LoadBalancingPolicyRoundRobin,
		},
		{
			name:     "ringhash",
			lbPolicy: e2e.LoadBalancingPolicyRingHash,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testRLSinxDS(t, test.lbPolicy)
		})
	}
}

func testRLSinxDS(t *testing.T, lbPolicy e2e.LoadBalancingPolicy) {
	oldRLS := envconfig.XDSRLS
	envconfig.XDSRLS = true
	internal.RegisterRLSClusterSpecifierPluginForTesting()
	defer func() {
		envconfig.XDSRLS = oldRLS
		internal.UnregisterRLSClusterSpecifierPluginForTesting()
	}()

	// Set up all components and configuration necessary - management server,
	// xDS resolver, fake RLS Server, and xDS configuration which specifies an
	// RLS Balancer that communicates to this set up fake RLS Server.
	managementServer, nodeID, _, resolver, cleanup1 := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup1()
	port, cleanup2 := startTestService(t, nil)
	defer cleanup2()

	lis := testutils.NewListenerWrapper(t, nil)
	rlsServer, rlsRequestCh := rls.SetupFakeRLSServer(t, lis)
	rlsProto := &rlspb.RouteLookupConfig{
		GrpcKeybuilders:      []*rlspb.GrpcKeyBuilder{{Names: []*rlspb.GrpcKeyBuilder_Name{{Service: "grpc.testing.TestService"}}}},
		LookupService:        rlsServer.Address,
		LookupServiceTimeout: durationpb.New(defaultTestTimeout),
		CacheSizeBytes:       1024,
	}

	const serviceName = "my-service-client-side-xds"
	resources := defaultClientResourcesWithRLSCSP(lbPolicy, e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       port,
		SecLevel:   e2e.SecurityLevelNone,
	}, rlsProto)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Configure the fake RLS Server to set the RLS Balancers child CDS
	// Cluster's name as the target for the RPC to use.
	rlsServer.SetResponseCallback(func(_ context.Context, req *rlspb.RouteLookupRequest) *rls.RouteLookupResponse {
		return &rls.RouteLookupResponse{Resp: &rlspb.RouteLookupResponse{Targets: []string{"cluster-" + serviceName}}}
	})

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.Dial(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	// Successfully sending the RPC will require the RLS Load Balancer to
	// communicate with the fake RLS Server for information about the target.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// These RLS Verifications makes sure the RLS Load Balancer is actually part
	// of the xDS Configured system that correctly sends out RPC.

	// Verify connection is established to RLS Server.
	if _, err = lis.NewConnCh.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for RLS LB policy to create control channel")
	}

	// Verify an rls request is sent out to fake RLS Server.
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout when waiting for an RLS request to be sent out")
	case <-rlsRequestCh:
	}
}
