/*
 *
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
 *
 */

package xds_test

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/testutils/xds/e2e"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	testgrpc "google.golang.org/grpc/test/grpc_testing"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

// hashRouteConfig returns a RouteConfig resource with hash policy set to
// header "session_id".
func hashRouteConfig(routeName, ldsTarget, clusterName string) *v3routepb.RouteConfiguration {
	return &v3routepb.RouteConfiguration{
		Name: routeName,
		VirtualHosts: []*v3routepb.VirtualHost{{
			Domains: []string{ldsTarget},
			Routes: []*v3routepb.Route{{
				Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/"}},
				Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
					ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
					HashPolicy: []*v3routepb.RouteAction_HashPolicy{{
						PolicySpecifier: &v3routepb.RouteAction_HashPolicy_Header_{
							Header: &v3routepb.RouteAction_HashPolicy_Header{
								HeaderName: "session_id",
							},
						},
						Terminal: true,
					}},
				}},
			}},
		}},
	}
}

// ringhashCluster returns a Cluster resource that picks ringhash as the lb
// policy.
func ringhashCluster(clusterName, edsServiceName string) *v3clusterpb.Cluster {
	return &v3clusterpb.Cluster{
		Name:                 clusterName,
		ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
		EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
			EdsConfig: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
					Ads: &v3corepb.AggregatedConfigSource{},
				},
			},
			ServiceName: edsServiceName,
		},
		LbPolicy: v3clusterpb.Cluster_RING_HASH,
	}
}

// TestClientSideAffinitySanityCheck tests that the affinity config can be
// propagated to pick the ring_hash policy. It doesn't test the affinity
// behavior in ring_hash policy.
func (s) TestClientSideAffinitySanityCheck(t *testing.T) {
	defer func() func() {
		old := envconfig.XDSRingHash
		envconfig.XDSRingHash = true
		return func() { envconfig.XDSRingHash = old }
	}()()

	managementServer, nodeID, _, resolver, cleanup1 := e2e.SetupManagementServer(t, nil)
	defer cleanup1()

	port, cleanup2 := startTestService(t, nil)
	defer cleanup2()

	const serviceName = "my-service-client-side-xds"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       port,
		SecLevel:   e2e.SecurityLevelNone,
	})
	// Replace RDS and CDS resources with ringhash config, but keep the resource
	// names.
	resources.Routes = []*v3routepb.RouteConfiguration{hashRouteConfig(
		resources.Routes[0].Name,
		resources.Listeners[0].Name,
		resources.Clusters[0].Name,
	)}
	resources.Clusters = []*v3clusterpb.Cluster{ringhashCluster(
		resources.Clusters[0].Name,
		resources.Clusters[0].EdsClusterConfig.ServiceName,
	)}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.Dial(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}
