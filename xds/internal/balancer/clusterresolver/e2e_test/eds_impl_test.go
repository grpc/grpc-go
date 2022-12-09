/*
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
 */

package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancergroup"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	rrutil "google.golang.org/grpc/internal/testutils/roundrobin"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds/internal/balancer/priority"
	"google.golang.org/grpc/xds/internal/xdsclient"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	testgrpc "google.golang.org/grpc/test/grpc_testing"
	testpb "google.golang.org/grpc/test/grpc_testing"

	_ "google.golang.org/grpc/xds/internal/balancer/clusterresolver"        // Register the "cluster_resolver_experimental" LB policy.
	_ "google.golang.org/grpc/xds/internal/xdsclient/controller/version/v3" // Register the v3 xDS API client.
)

const (
	clusterName    = "cluster-my-service-client-side-xds"
	edsServiceName = "endpoints-my-service-client-side-xds"
	localityName1  = "my-locality-1"
	localityName2  = "my-locality-2"
	localityName3  = "my-locality-3"

	defaultTestTimeout = 5 * time.Second
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// backendAddressesAndPorts extracts the address and port of each of the
// StubServers passed in and returns them. Fails the test if any of the
// StubServers passed have an invalid address.
func backendAddressesAndPorts(t *testing.T, servers []*stubserver.StubServer) ([]resolver.Address, []uint32) {
	addrs := make([]resolver.Address, len(servers))
	ports := make([]uint32, len(servers))
	for i := 0; i < len(servers); i++ {
		addrs[i] = resolver.Address{Addr: servers[i].Address}
		ports[i] = extractPortFromAddress(t, servers[i].Address)
	}
	return addrs, ports
}

func extractPortFromAddress(t *testing.T, address string) uint32 {
	_, p, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("invalid server address %q: %v", address, err)
	}
	port, err := strconv.ParseUint(p, 10, 32)
	if err != nil {
		t.Fatalf("invalid server address %q: %v", address, err)
	}
	return uint32(port)
}

func startTestServiceBackends(t *testing.T, numBackends int) ([]*stubserver.StubServer, func()) {
	servers := make([]*stubserver.StubServer, numBackends)
	for i := 0; i < numBackends; i++ {
		servers[i] = &stubserver.StubServer{
			EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
		}
		servers[i].StartServer()
	}

	return servers, func() {
		for _, server := range servers {
			server.Stop()
		}
	}
}

// endpointResource returns an EDS resource for the given cluster name and
// localities. Backends within a locality are all assumed to be on the same
// machine (localhost).
func endpointResource(clusterName string, localities []localityInfo) *v3endpointpb.ClusterLoadAssignment {
	var localityEndpoints []*v3endpointpb.LocalityLbEndpoints
	for _, locality := range localities {
		var endpoints []*v3endpointpb.LbEndpoint
		for i, port := range locality.ports {
			endpoint := &v3endpointpb.LbEndpoint{
				HostIdentifier: &v3endpointpb.LbEndpoint_Endpoint{
					Endpoint: &v3endpointpb.Endpoint{
						Address: &v3corepb.Address{Address: &v3corepb.Address_SocketAddress{
							SocketAddress: &v3corepb.SocketAddress{
								Protocol:      v3corepb.SocketAddress_TCP,
								Address:       "localhost",
								PortSpecifier: &v3corepb.SocketAddress_PortValue{PortValue: port}},
						},
						},
					},
				},
			}
			if i < len(locality.healthStatus) {
				endpoint.HealthStatus = locality.healthStatus[i]
			}
			endpoints = append(endpoints, endpoint)
		}
		localityEndpoints = append(localityEndpoints, &v3endpointpb.LocalityLbEndpoints{
			Locality:            &v3corepb.Locality{SubZone: locality.name},
			LbEndpoints:         endpoints,
			LoadBalancingWeight: &wrapperspb.UInt32Value{Value: locality.weight},
		})
	}
	return &v3endpointpb.ClusterLoadAssignment{
		ClusterName: clusterName,
		Endpoints:   localityEndpoints,
	}
}

type localityInfo struct {
	name         string
	weight       uint32
	ports        []uint32
	healthStatus []v3corepb.HealthStatus
}

// clientEndpointsResource returns an EDS resource for the specified nodeID,
// service name and localities.
func clientEndpointsResource(nodeID, edsServiceName string, localities []localityInfo) e2e.UpdateOptions {
	return e2e.UpdateOptions{
		NodeID:         nodeID,
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{endpointResource(edsServiceName, localities)},
		SkipValidation: true,
	}
}

// TestEDS_OneLocality tests the cluster_resolver LB policy using an EDS
// resource with one locality. The following scenarios are tested:
//  1. Single backend. Test verifies that RPCs reach this backend.
//  2. Add a backend. Test verifies that RPCs are roundrobined across the two
//     backends.
//  3. Remove one backend. Test verifies that all RPCs reach the other backend.
//  4. Replace the backend. Test verifies that all RPCs reach the new backend.
func (s) TestEDS_OneLocality(t *testing.T) {
	// Spin up a management server to receive xDS resources from.
	managementServer, nodeID, bootstrapContents, _, cleanup1 := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup1()

	// Start backend servers which provide an implementation of the TestService.
	servers, cleanup2 := startTestServiceBackends(t, 3)
	defer cleanup2()
	addrs, ports := backendAddressesAndPorts(t, servers)

	// Create xDS resources for consumption by the test. We start off with a
	// single backend in a single EDS locality.
	resources := clientEndpointsResource(nodeID, edsServiceName, []localityInfo{{name: localityName1, weight: 1, ports: ports[:1]}})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create an xDS client for use by the cluster_resolver LB policy.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Create a manual resolver and push a service config specifying the use of
	// the cluster_resolver LB policy with a single discovery mechanism.
	r := manual.NewBuilderWithScheme("whatever")
	jsonSC := fmt.Sprintf(`{
			"loadBalancingConfig":[{
				"cluster_resolver_experimental":{
					"discoveryMechanisms": [{
						"cluster": "%s",
						"type": "EDS",
						"edsServiceName": "%s"
					}],
					"xdsLbPolicy":[{"round_robin":{}}]
				}
			}]
		}`, clusterName, edsServiceName)
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	r.InitialState(xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, client))

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.Dial(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	// Ensure RPCs are being roundrobined across the single backend.
	testClient := testpb.NewTestServiceClient(cc)
	if err := rrutil.CheckRoundRobinRPCs(ctx, testClient, addrs[:1]); err != nil {
		t.Fatal(err)
	}

	// Add a backend to the same locality, and ensure RPCs are sent in a
	// roundrobin fashion across the two backends.
	resources = clientEndpointsResource(nodeID, edsServiceName, []localityInfo{{name: localityName1, weight: 1, ports: ports[:2]}})
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if err := rrutil.CheckRoundRobinRPCs(ctx, testClient, addrs[:2]); err != nil {
		t.Fatal(err)
	}

	// Remove the first backend, and ensure all RPCs are sent to the second
	// backend.
	resources = clientEndpointsResource(nodeID, edsServiceName, []localityInfo{{name: localityName1, weight: 1, ports: ports[1:2]}})
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if err := rrutil.CheckRoundRobinRPCs(ctx, testClient, addrs[1:2]); err != nil {
		t.Fatal(err)
	}

	// Replace the backend, and ensure all RPCs are sent to the new backend.
	resources = clientEndpointsResource(nodeID, edsServiceName, []localityInfo{{name: localityName1, weight: 1, ports: ports[2:3]}})
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if err := rrutil.CheckRoundRobinRPCs(ctx, testClient, addrs[2:3]); err != nil {
		t.Fatal(err)
	}
}

// TestEDS_MultipleLocalities tests the cluster_resolver LB policy using an EDS
// resource with multiple localities. The following scenarios are tested:
//  1. Two localities, each with a single backend. Test verifies that RPCs are
//     weighted roundrobined across these two backends.
//  2. Add another locality, with a single backend. Test verifies that RPCs are
//     weighted roundrobined across all the backends.
//  3. Remove one locality. Test verifies that RPCs are weighted roundrobined
//     across backends from the remaining localities.
//  4. Add a backend to one locality. Test verifies that RPCs are weighted
//     roundrobined across localities.
//  5. Change the weight of one of the localities. Test verifies that RPCs are
//     weighted roundrobined across the localities.
//
// In our LB policy tree, one of the descendents of the "cluster_resolver" LB
// policy is the "weighted_target" LB policy which performs weighted roundrobin
// across localities (and this has a randomness component associated with it).
// Therefore, the moment we have backends from more than one locality, RPCs are
// weighted roundrobined across them.
func (s) TestEDS_MultipleLocalities(t *testing.T) {
	// Spin up a management server to receive xDS resources from.
	managementServer, nodeID, bootstrapContents, _, cleanup1 := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup1()

	// Start backend servers which provide an implementation of the TestService.
	servers, cleanup2 := startTestServiceBackends(t, 4)
	defer cleanup2()
	addrs, ports := backendAddressesAndPorts(t, servers)

	// Create xDS resources for consumption by the test. We start off with two
	// localities, and single backend in each of them.
	resources := clientEndpointsResource(nodeID, edsServiceName, []localityInfo{
		{name: localityName1, weight: 1, ports: ports[:1]},
		{name: localityName2, weight: 1, ports: ports[1:2]},
	})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create an xDS client for use by the cluster_resolver LB policy.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Create a manual resolver and push service config specifying the use of
	// the cluster_resolver LB policy with a single discovery mechanism.
	r := manual.NewBuilderWithScheme("whatever")
	jsonSC := fmt.Sprintf(`{
			"loadBalancingConfig":[{
				"cluster_resolver_experimental":{
					"discoveryMechanisms": [{
						"cluster": "%s",
						"type": "EDS",
						"edsServiceName": "%s"
					}],
					"xdsLbPolicy":[{"round_robin":{}}]
				}
			}]
		}`, clusterName, edsServiceName)
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	r.InitialState(xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, client))

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.Dial(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	// Ensure RPCs are being weighted roundrobined across the two backends.
	testClient := testpb.NewTestServiceClient(cc)
	if err := rrutil.CheckWeightedRoundRobinRPCs(ctx, testClient, addrs[0:2]); err != nil {
		t.Fatal(err)
	}

	// Add another locality with a single backend, and ensure RPCs are being
	// weighted roundrobined across the three backends.
	resources = clientEndpointsResource(nodeID, edsServiceName, []localityInfo{
		{name: localityName1, weight: 1, ports: ports[:1]},
		{name: localityName2, weight: 1, ports: ports[1:2]},
		{name: localityName3, weight: 1, ports: ports[2:3]},
	})
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if err := rrutil.CheckWeightedRoundRobinRPCs(ctx, testClient, addrs[0:3]); err != nil {
		t.Fatal(err)
	}

	// Remove the first locality, and ensure RPCs are being weighted
	// roundrobined across the remaining two backends.
	resources = clientEndpointsResource(nodeID, edsServiceName, []localityInfo{
		{name: localityName2, weight: 1, ports: ports[1:2]},
		{name: localityName3, weight: 1, ports: ports[2:3]},
	})
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if err := rrutil.CheckWeightedRoundRobinRPCs(ctx, testClient, addrs[1:3]); err != nil {
		t.Fatal(err)
	}

	// Add a backend to one locality, and ensure weighted roundrobin. Since RPCs
	// are roundrobined across localities, locality2's backend will receive
	// twice the traffic.
	resources = clientEndpointsResource(nodeID, edsServiceName, []localityInfo{
		{name: localityName2, weight: 1, ports: ports[1:2]},
		{name: localityName3, weight: 1, ports: ports[2:4]},
	})
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	wantAddrs := []resolver.Address{addrs[1], addrs[1], addrs[2], addrs[3]}
	if err := rrutil.CheckWeightedRoundRobinRPCs(ctx, testClient, wantAddrs); err != nil {
		t.Fatal(err)
	}

	// Change the weight of locality2 and ensure weighted roundrobin.  Since
	// locality2 has twice the weight of locality3, it will be picked twice as
	// frequently as locality3 for RPCs. And since locality2 has a single
	// backend and locality3 has two backends, the backend in locality2 will
	// receive four times the traffic of each of locality3's backends.
	resources = clientEndpointsResource(nodeID, edsServiceName, []localityInfo{
		{name: localityName2, weight: 2, ports: ports[1:2]},
		{name: localityName3, weight: 1, ports: ports[2:4]},
	})
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	wantAddrs = []resolver.Address{addrs[1], addrs[1], addrs[1], addrs[1], addrs[2], addrs[3]}
	if err := rrutil.CheckWeightedRoundRobinRPCs(ctx, testClient, wantAddrs); err != nil {
		t.Fatal(err)
	}
}

// TestEDS_EndpointsHealth tests the cluster_resolver LB policy using an EDS
// resource which specifies endpoint health information and verifies that
// traffic is routed only to backends deemed capable of receiving traffic.
func (s) TestEDS_EndpointsHealth(t *testing.T) {
	// Spin up a management server to receive xDS resources from.
	managementServer, nodeID, bootstrapContents, _, cleanup1 := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup1()

	// Start backend servers which provide an implementation of the TestService.
	servers, cleanup2 := startTestServiceBackends(t, 12)
	defer cleanup2()
	addrs, ports := backendAddressesAndPorts(t, servers)

	// Create xDS resources for consumption by the test.  Two localities with
	// six backends each, with two of the six backends being healthy. Both
	// UNKNOWN and HEALTHY are considered by gRPC for load balancing.
	resources := clientEndpointsResource(nodeID, edsServiceName, []localityInfo{
		{name: localityName1, weight: 1, ports: ports[:6], healthStatus: []v3corepb.HealthStatus{
			v3corepb.HealthStatus_UNKNOWN,
			v3corepb.HealthStatus_HEALTHY,
			v3corepb.HealthStatus_UNHEALTHY,
			v3corepb.HealthStatus_DRAINING,
			v3corepb.HealthStatus_TIMEOUT,
			v3corepb.HealthStatus_DEGRADED,
		}},
		{name: localityName2, weight: 1, ports: ports[6:12], healthStatus: []v3corepb.HealthStatus{
			v3corepb.HealthStatus_UNKNOWN,
			v3corepb.HealthStatus_HEALTHY,
			v3corepb.HealthStatus_UNHEALTHY,
			v3corepb.HealthStatus_DRAINING,
			v3corepb.HealthStatus_TIMEOUT,
			v3corepb.HealthStatus_DEGRADED,
		}},
	})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create an xDS client for use by the cluster_resolver LB policy.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Create a manual resolver and push service config specifying the use of
	// the cluster_resolver LB policy with a single discovery mechanism.
	r := manual.NewBuilderWithScheme("whatever")
	jsonSC := fmt.Sprintf(`{
			"loadBalancingConfig":[{
				"cluster_resolver_experimental":{
					"discoveryMechanisms": [{
						"cluster": "%s",
						"type": "EDS",
						"edsServiceName": "%s"
					}],
					"xdsLbPolicy":[{"round_robin":{}}]
				}
			}]
		}`, clusterName, edsServiceName)
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	r.InitialState(xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, client))

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.Dial(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	// Ensure RPCs are being weighted roundrobined across healthy backends from
	// both localities.
	testClient := testpb.NewTestServiceClient(cc)
	if err := rrutil.CheckWeightedRoundRobinRPCs(ctx, testClient, append(addrs[0:2], addrs[6:8]...)); err != nil {
		t.Fatal(err)
	}
}

// TestEDS_EmptyUpdate tests the cluster_resolver LB policy using an EDS
// resource with no localities and verifies that RPCs fail with "all priorities
// removed" error.
func (s) TestEDS_EmptyUpdate(t *testing.T) {
	// Spin up a management server to receive xDS resources from.
	managementServer, nodeID, bootstrapContents, _, cleanup1 := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup1()

	// Start backend servers which provide an implementation of the TestService.
	servers, cleanup2 := startTestServiceBackends(t, 4)
	defer cleanup2()
	addrs, ports := backendAddressesAndPorts(t, servers)

	oldCacheTimeout := balancergroup.DefaultSubBalancerCloseTimeout
	balancergroup.DefaultSubBalancerCloseTimeout = 100 * time.Microsecond
	defer func() { balancergroup.DefaultSubBalancerCloseTimeout = oldCacheTimeout }()

	// Create xDS resources for consumption by the test. The first update is an
	// empty update. This should put the channel in TRANSIENT_FAILURE.
	resources := clientEndpointsResource(nodeID, edsServiceName, nil)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create an xDS client for use by the cluster_resolver LB policy.
	client, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer client.Close()

	// Create a manual resolver and push service config specifying the use of
	// the cluster_resolver LB policy with a single discovery mechanism.
	r := manual.NewBuilderWithScheme("whatever")
	jsonSC := fmt.Sprintf(`{
			"loadBalancingConfig":[{
				"cluster_resolver_experimental":{
					"discoveryMechanisms": [{
						"cluster": "%s",
						"type": "EDS",
						"edsServiceName": "%s"
					}],
					"xdsLbPolicy":[{"round_robin":{}}]
				}
			}]
		}`, clusterName, edsServiceName)
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	r.InitialState(xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, client))

	// Create a ClientConn and ensure that RPCs fail with "all priorities
	// removed" error. This is the expected error when the cluster_resolver LB
	// policy receives an EDS update with no localities.
	cc, err := grpc.Dial(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()
	testClient := testpb.NewTestServiceClient(cc)
	if err := waitForAllPrioritiesRemovedError(ctx, t, testClient); err != nil {
		t.Fatal(err)
	}

	// Add a locality with one backend and ensure RPCs are successful.
	resources = clientEndpointsResource(nodeID, edsServiceName, []localityInfo{{name: localityName1, weight: 1, ports: ports[:1]}})
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if err := rrutil.CheckRoundRobinRPCs(ctx, testClient, addrs[:1]); err != nil {
		t.Fatal(err)
	}

	// Push another empty update and ensure that RPCs fail with "all priorities
	// removed" error again.
	resources = clientEndpointsResource(nodeID, edsServiceName, nil)
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	if err := waitForAllPrioritiesRemovedError(ctx, t, testClient); err != nil {
		t.Fatal(err)
	}
}

// waitForAllPrioritiesRemovedError repeatedly makes RPCs using the
// TestServiceClient until they fail with an error which indicates that all
// priorities have been removed. A non-nil error is returned if the context
// expires before RPCs fail with the expected error.
func waitForAllPrioritiesRemovedError(ctx context.Context, t *testing.T, client testgrpc.TestServiceClient) error {
	for ; ctx.Err() == nil; <-time.After(time.Millisecond) {
		_, err := client.EmptyCall(ctx, &testpb.Empty{})
		if err == nil {
			t.Log("EmptyCall() succeeded after EDS update with no localities")
			continue
		}
		if code := status.Code(err); code != codes.Unavailable {
			t.Logf("EmptyCall() returned code: %v, want: %v", code, codes.Unavailable)
			continue
		}
		if !strings.Contains(err.Error(), priority.ErrAllPrioritiesRemoved.Error()) {
			t.Logf("EmptyCall() = %v, want %v", err, priority.ErrAllPrioritiesRemoved)
			continue
		}
		return nil
	}
	return errors.New("timeout when waiting for RPCs to fail with UNAVAILABLE status and priority.ErrAllPrioritiesRemoved error")
}
