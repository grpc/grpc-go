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

package xds_test

import (
	"context"
	"fmt"
	"testing"

	v3 "github.com/cncf/xds/go/xds/type/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3roundrobinpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/round_robin/v3"
	v3wrrlocalitypb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/wrr_locality/v3"
	"github.com/golang/protobuf/proto"
	structpb "github.com/golang/protobuf/ptypes/struct"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/roundrobin"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/resolver"
)

// wrrLocality is a helper that takes a proto message and returns a
// WrrLocalityProto with the proto message marshaled into a proto.Any as a
// child.
func wrrLocality(m proto.Message) *v3wrrlocalitypb.WrrLocality {
	return &v3wrrlocalitypb.WrrLocality{
		EndpointPickingPolicy: &v3clusterpb.LoadBalancingPolicy{
			Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
				{
					TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
						Name:        "what is this used for?",
						TypedConfig: testutils.MarshalAny(m),
					},
				},
			},
		},
	}
}

// clusterWithLBConfiguration returns a cluster resource with the proto message
// passed Marshaled to an any and specified through the load_balancing_policy
// field.
func clusterWithLBConfiguration(clusterName, edsServiceName string, secLevel e2e.SecurityLevel, m proto.Message) *v3clusterpb.Cluster {
	cluster := e2e.DefaultCluster(clusterName, edsServiceName, secLevel)
	cluster.LoadBalancingPolicy = &v3clusterpb.LoadBalancingPolicy{
		Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
			{
				TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
					Name:        "noop name",
					TypedConfig: testutils.MarshalAny(m),
				},
			},
		},
	}
	return cluster
}

// clientResourcesNewFieldSpecifiedAndPortsInMultipleLocalities returns default
// xDS resources with two localities, of weights 1 and 2 respectively. It must
// be passed 5 ports, and the first two ports will be put in the first locality,
// and the last three will be put in the second locality. It also configures the
// proto message passed in as the Locality + Endpoint picking policy in CDS.
func clientResourcesNewFieldSpecifiedAndPortsInMultipleLocalities2(params e2e.ResourceParams, ports []uint32, m proto.Message) e2e.UpdateOptions {
	routeConfigName := "route-" + params.DialTarget
	clusterName := "cluster-" + params.DialTarget
	endpointsName := "endpoints-" + params.DialTarget
	return e2e.UpdateOptions{
		NodeID:    params.NodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(params.DialTarget, routeConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, params.DialTarget, clusterName)},
		Clusters:  []*v3clusterpb.Cluster{clusterWithLBConfiguration(clusterName, endpointsName, params.SecLevel, m)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.EndpointResourceWithOptionsMultipleLocalities(e2e.EndpointOptions{
			ClusterName: endpointsName,
			Host:        params.Host,
			PortsInLocalities: [][]uint32{
				{ports[0], ports[1]},
				{ports[2], ports[3], ports[4]},
			},
			LocalityWeights: []uint32{
				1,
				2,
			},
		})},
	}
}

// TestWRRLocality tests RPC distribution across a scenario with 5 backends,
// with 2 backends in a locality with weight 1, and 3 backends in a second
// locality with weight 2. Through xDS, the test configures a
// wrr_locality_balancer with either a round robin or custom (specifying pick
// first) child load balancing policy, and asserts the correct distribution
// based on the locality weights and the endpoint picking policy specified.
func (s) TestWrrLocality(t *testing.T) {
	oldCustomLBSupport := envconfig.XDSCustomLBPolicy
	envconfig.XDSCustomLBPolicy = true
	defer func() {
		envconfig.XDSCustomLBPolicy = oldCustomLBSupport
	}()

	managementServer, nodeID, _, r, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup()
	backend1 := stubserver.StartTestService(t, nil)
	port1 := testutils.ParsePort(t, backend1.Address)
	defer backend1.Stop()
	backend2 := stubserver.StartTestService(t, nil)
	port2 := testutils.ParsePort(t, backend2.Address)
	defer backend2.Stop()
	backend3 := stubserver.StartTestService(t, nil)
	port3 := testutils.ParsePort(t, backend3.Address)
	defer backend3.Stop()
	backend4 := stubserver.StartTestService(t, nil)
	port4 := testutils.ParsePort(t, backend4.Address)
	defer backend4.Stop()
	backend5 := stubserver.StartTestService(t, nil)
	port5 := testutils.ParsePort(t, backend5.Address)
	defer backend5.Stop()
	const serviceName = "my-service-client-side-xds"
	tests := []struct {
		name string
		// Configuration will be specified through load_balancing_policy field.
		wrrLocalityConfiguration *v3wrrlocalitypb.WrrLocality
		addressDistributionWant  []resolver.Address
	}{
		{
			name:                     "rr_child",
			wrrLocalityConfiguration: wrrLocality(&v3roundrobinpb.RoundRobin{}),
			// Each addresses expected probability is locality weight of
			// locality / total locality weights multiplied by 1 / number of
			// endpoints in each locality (due to round robin across endpoints
			// in a locality). Thus, address 1 and address 2 have 1/3 * 1/2
			// probability, and addresses 3 4 5 have 2/3 * 1/3 probability of
			// being routed to.
			addressDistributionWant: []resolver.Address{
				{Addr: backend1.Address},
				{Addr: backend1.Address},
				{Addr: backend1.Address},
				{Addr: backend1.Address},
				{Addr: backend1.Address},
				{Addr: backend1.Address},
				{Addr: backend2.Address},
				{Addr: backend2.Address},
				{Addr: backend2.Address},
				{Addr: backend2.Address},
				{Addr: backend2.Address},
				{Addr: backend2.Address},
				{Addr: backend3.Address},
				{Addr: backend3.Address},
				{Addr: backend3.Address},
				{Addr: backend3.Address},
				{Addr: backend3.Address},
				{Addr: backend3.Address},
				{Addr: backend3.Address},
				{Addr: backend3.Address},
				{Addr: backend4.Address},
				{Addr: backend4.Address},
				{Addr: backend4.Address},
				{Addr: backend4.Address},
				{Addr: backend4.Address},
				{Addr: backend4.Address},
				{Addr: backend4.Address},
				{Addr: backend4.Address},
				{Addr: backend5.Address},
				{Addr: backend5.Address},
				{Addr: backend5.Address},
				{Addr: backend5.Address},
				{Addr: backend5.Address},
				{Addr: backend5.Address},
				{Addr: backend5.Address},
				{Addr: backend5.Address},
			},
		},
		// This configures custom lb as the child of wrr_locality, which points
		// to our pick_first implementation. Thus, the expected distribution of
		// addresses is locality weight of locality / total locality weights as
		// the probability of picking the first backend within the locality
		// (e.g. Address 1 for locality 1, and Address 3 for locality 2).
		{
			name: "custom_lb_child_pick_first",
			wrrLocalityConfiguration: wrrLocality(&v3.TypedStruct{
				TypeUrl: "type.googleapis.com/pick_first",
				Value:   &structpb.Struct{},
			}),
			addressDistributionWant: []resolver.Address{
				{Addr: backend1.Address},
				{Addr: backend3.Address},
				{Addr: backend3.Address},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resources := clientResourcesNewFieldSpecifiedAndPortsInMultipleLocalities2(e2e.ResourceParams{
				DialTarget: serviceName,
				NodeID:     nodeID,
				Host:       "localhost",
				SecLevel:   e2e.SecurityLevelNone,
			}, []uint32{port1, port2, port3, port4, port5}, test.wrrLocalityConfiguration)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if err := managementServer.Update(ctx, resources); err != nil {
				t.Fatal(err)
			}

			cc, err := grpc.Dial(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
			if err != nil {
				t.Fatalf("Failed to dial local test server: %v", err)
			}
			defer cc.Close()

			client := testgrpc.NewTestServiceClient(cc)
			if err := roundrobin.CheckWeightedRoundRobinRPCs(ctx, client, test.addressDistributionWant); err != nil {
				t.Fatalf("Error in expeected round robin: %v", err)
			}
		})
	}
}
