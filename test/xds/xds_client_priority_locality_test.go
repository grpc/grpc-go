/*
 *
 * Copyright 2024 gRPC authors.
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
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	rrutil "google.golang.org/grpc/internal/testutils/roundrobin"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/resolver"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

// backendAddressesAndPorts extracts the address and port of each of the
// StubServers passed in and returns them. Fails the test if any of the
// StubServers passed have an invalid address.
func backendAddressesAndPorts(t *testing.T, servers []*stubserver.StubServer) ([]resolver.Address, []uint32) {
	addrs := make([]resolver.Address, len(servers))
	ports := make([]uint32, len(servers))
	for i := 0; i < len(servers); i++ {
		addrs[i] = resolver.Address{Addr: servers[i].Address}
		ports[i] = testutils.ParsePort(t, servers[i].Address)
	}
	return addrs, ports
}

// Tests scenarios involving localities moving between priorities.
//   - The test starts off with a cluster that contains two priorities, one
//     locality in each, and one endpoint in each. Verifies that traffic reaches
//     the endpoint in the higher priority.
//   - The test then moves the locality in the lower priority over to the higher
//     priority. At that point, we would have a cluster with a single priority,
//     but two localities, and one endpoint in each. Verifies that traffic is
//     split between the endpoints.
//   - The test then deletes the locality that was originally in the higher
//     priority.Verifies that all traffic is now reaching the only remaining
//     endpoint.
func (s) TestClientSideXDS_LocalityChangesPriority(t *testing.T) {
	// Spin up a management server and two test service backends.
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)
	backend0 := stubserver.StartTestService(t, nil)
	defer backend0.Stop()
	backend1 := stubserver.StartTestService(t, nil)
	defer backend1.Stop()
	addrs, ports := backendAddressesAndPorts(t, []*stubserver.StubServer{backend0, backend1})

	// Configure resources on the management server. We use default client side
	// resources for listener, route configuration and cluster. For the
	// endpoints resource though, we create one with two priorities, and one
	// locality each, and one endpoint each.
	const serviceName = "my-service-client-side-xds"
	const routeConfigName = "route-" + serviceName
	const clusterName = "cluster-" + serviceName
	const endpointsName = "endpoints-" + serviceName
	locality1 := e2e.LocalityID{Region: "my-region-1", Zone: "my-zone-1", SubZone: "my-subzone-1"}
	locality2 := e2e.LocalityID{Region: "my-region-2", Zone: "my-zone-2", SubZone: "my-subzone-2"}
	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, serviceName, clusterName)},
		Clusters:  []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, endpointsName, e2e.SecurityLevelNone)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
			ClusterName: endpointsName,
			Host:        "localhost",
			Localities: []e2e.LocalityOptions{
				{
					Name:     "my-locality-1",
					Weight:   1000000,
					Priority: 0,
					Backends: []e2e.BackendOptions{{Ports: []uint32{ports[0]}}},
					Locality: locality1,
				},
				{
					Name:     "my-locality-2",
					Weight:   1000000,
					Priority: 1,
					Backends: []e2e.BackendOptions{{Ports: []uint32{ports[1]}}},
					Locality: locality2,
				},
			},
		})},
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	// // Ensure that RPCs get routed to the backend in the higher priority.
	client := testgrpc.NewTestServiceClient(cc)
	if err := rrutil.CheckRoundRobinRPCs(ctx, client, addrs[:1]); err != nil {
		t.Fatal(err)
	}

	// Update the endpoints resource to contain a single priority with two
	// localities, and one endpoint each. The locality weights are equal at this
	// point, and we expect RPCs to be round-robined across the two localities.
	resources.Endpoints = []*v3endpointpb.ClusterLoadAssignment{e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
		ClusterName: endpointsName,
		Host:        "localhost",
		Localities: []e2e.LocalityOptions{
			{
				Name:     "my-locality-1",
				Weight:   500000,
				Priority: 0,
				Backends: []e2e.BackendOptions{{Ports: []uint32{testutils.ParsePort(t, backend0.Address)}}},
				Locality: locality1,
			},
			{
				Name:     "my-locality-2",
				Weight:   500000,
				Priority: 0,
				Backends: []e2e.BackendOptions{{Ports: []uint32{testutils.ParsePort(t, backend1.Address)}}},
				Locality: locality2,
			},
		},
	})}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if err := rrutil.CheckRoundRobinRPCs(ctx, client, addrs); err != nil {
		t.Fatal(err)
	}

	// Update the locality weights ever so slightly. We still expect RPCs to be
	// round-robined across the two localities.
	resources.Endpoints = []*v3endpointpb.ClusterLoadAssignment{e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
		ClusterName: endpointsName,
		Host:        "localhost",
		Localities: []e2e.LocalityOptions{
			{
				Name:     "my-locality-1",
				Weight:   499884,
				Priority: 0,
				Backends: []e2e.BackendOptions{{Ports: []uint32{testutils.ParsePort(t, backend0.Address)}}},
				Locality: locality1,
			},
			{
				Name:     "my-locality-2",
				Weight:   500115,
				Priority: 0,
				Backends: []e2e.BackendOptions{{Ports: []uint32{testutils.ParsePort(t, backend1.Address)}}},
				Locality: locality2,
			},
		},
	})}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if err := rrutil.CheckRoundRobinRPCs(ctx, client, addrs); err != nil {
		t.Fatal(err)
	}

	// Update the endpoints resource to contain a single priority with one
	// locality. The locality which was originally in the higher priority is now
	// dropped.
	resources.Endpoints = []*v3endpointpb.ClusterLoadAssignment{e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
		ClusterName: endpointsName,
		Host:        "localhost",
		Localities: []e2e.LocalityOptions{
			{
				Name:     "my-locality-2",
				Weight:   1000000,
				Priority: 0,
				Backends: []e2e.BackendOptions{{Ports: []uint32{testutils.ParsePort(t, backend1.Address)}}},
				Locality: locality2,
			},
		},
	})}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if err := rrutil.CheckRoundRobinRPCs(ctx, client, addrs[1:]); err != nil {
		t.Fatal(err)
	}
}
