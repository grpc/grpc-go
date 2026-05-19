/*
 * Copyright 2026 gRPC authors.
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
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	_ "google.golang.org/grpc/internal/xds/balancer/priority" // Register priority LB policy.
)

// TestLogicalDNS_MultipleEndpoints tests the priority LB policy using a
// LOGICAL_DNS discovery mechanism.
//
// The test verifies that multiple addresses returned by the DNS resolver are
// grouped into a single endpoint (as per gRFC A61). Because the round_robin LB
// policy sees only one endpoint, it should not rotate traffic between the
// addresses. Instead, the single endpoint is picked, and connects to the first
// address.
func (s) TestLogicalDNS_MultipleEndpoints(t *testing.T) {
	// Spin up a management server to receive xDS resources from.
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Start backend servers which provide an implementation of the TestService.
	server1 := stubserver.StartTestService(t, nil)
	defer server1.Stop()
	server2 := stubserver.StartTestService(t, nil)
	defer server2.Stop()

	// Register a manual resolver with the "dns" scheme to override DNS resolution.
	// This global override is safe because connection to the xDS management
	// server uses the passthrough scheme instead and therefore overriding
	// the DNS resolver does not affect it in any way.
	const dnsScheme = "dns"
	dnsR := manual.NewBuilderWithScheme(dnsScheme)
	originalDNS := resolver.Get("dns")
	resolver.Register(dnsR)
	t.Cleanup(func() { resolver.Register(originalDNS) })

	// For LOGICAL_DNS, this updates the SINGLE endpoint to have 2 IPs.
	dnsR.InitialState(resolver.State{
		Endpoints: []resolver.Endpoint{{
			Addresses: []resolver.Address{
				{Addr: server1.Address},
				{Addr: server2.Address},
			}}},
	})

	const (
		serviceName   = "test-xds-service"
		clusterName   = "cluster-test-xds-service"
		endpointsName = "endpoints-test-xds-service"
		rdsName       = "route-test-xds-service"
	)

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, rdsName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(rdsName, serviceName, clusterName)},
		Clusters: []*v3clusterpb.Cluster{e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
			ClusterName: clusterName,
			ServiceName: endpointsName,
			Type:        e2e.ClusterTypeLogicalDNS,
			DNSHostName: "dns",
			DNSPort:     uint32(8080),
			Policy:      e2e.LoadBalancingPolicyRoundRobin,
		})},
		Endpoints: nil,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatalf("Failed to update management server: %v", err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///"+serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("Failed to create new client for local test server: %v", err)
	}
	defer cc.Close()

	// Ensure the RPC is routed to the first backend.
	testClient := testgrpc.NewTestServiceClient(cc)
	for i := 0; i < 10; i++ {
		var peer peer.Peer
		if _, err := testClient.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer)); err != nil {
			t.Fatalf("RPC failed: %v", err)
		}

		if got, want := peer.Addr.String(), server1.Address; got != want {
			t.Errorf("peer.Addr = %q, want = %q", got, want)
		}
	}
}
