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
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond // For events expected to *not* happen.
)

func (s) TestClientSideXDS(t *testing.T) {
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	const serviceName = "my-service-client-side-xds"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
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

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}

// TestClient_ConcurrentRPC ensures thread safety for xDS clients executing
// concurrent RPCs, particularly verifying that the regex matchers do not cause
// data races.
func (s) TestClient_ConcurrentRPC(t *testing.T) {
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	const serviceName = "my-service-client-side-xds"
	const routeConfigName = "route-" + serviceName
	const clusterName = "cluster-" + serviceName

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)},
		Routes: []*v3routepb.RouteConfiguration{{
			Name: routeConfigName,
			VirtualHosts: []*v3routepb.VirtualHost{{
				Domains: []string{serviceName},
				Routes: []*v3routepb.Route{{
					Match: &v3routepb.RouteMatch{
						PathSpecifier: &v3routepb.RouteMatch_SafeRegex{
							SafeRegex: &v3matcherpb.RegexMatcher{Regex: "/.*"},
						},
					},
					Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
						ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName},
					}},
				}},
			}},
		}},
		Clusters:  []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, clusterName, e2e.SecurityLevelNone)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(clusterName, "localhost", []uint32{testutils.ParsePort(t, server.Address)})},
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}
	defer cc.Close()

	const numRPCs = 20
	wg := sync.WaitGroup{}
	for i := 0; i < numRPCs; i++ {
		wg.Go(func() {
			client := testgrpc.NewTestServiceClient(cc)
			if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
				t.Errorf("EmptyCall() failed: %v", err)
			}
		})
	}
	wg.Wait()
}

// Test verifies that client-side xDS routing strictly evaluates
// OutgoingContext metadata and ignores IncomingContext metadata. This
// prevents incoming headers on a server handler context from leaking
// into and contaminating outgoing client routing decisions.
func (s) TestClientSideXDS_RouteMatching_IgnoreIncomingMetadata(t *testing.T) {
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	serverA := stubserver.StartTestService(t, nil)
	defer serverA.Stop()
	serverB := stubserver.StartTestService(t, nil)
	defer serverB.Stop()

	const (
		serviceName     = "my-service-client"
		routeConfigName = "route-" + serviceName
		clusterA        = "cluster-A-" + serviceName
		clusterB        = "cluster-B-" + serviceName
		headerName      = "x-route-target"
		headerValue     = "cluster-a"
	)

	resources := e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)},
		Routes: []*v3routepb.RouteConfiguration{{
			Name: routeConfigName,
			VirtualHosts: []*v3routepb.VirtualHost{{
				Domains: []string{serviceName},
				Routes: []*v3routepb.Route{
					{
						Match: &v3routepb.RouteMatch{
							PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"},
							Headers: []*v3routepb.HeaderMatcher{{
								Name: headerName,
								HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{
									ExactMatch: headerValue,
								},
							}},
						},
						Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterA},
						}},
					},
					{
						Match: &v3routepb.RouteMatch{
							PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"},
						},
						Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
							ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterB},
						}},
					},
				},
			}},
		}},
		Clusters: []*v3clusterpb.Cluster{
			e2e.DefaultCluster(clusterA, clusterA, e2e.SecurityLevelNone),
			e2e.DefaultCluster(clusterB, clusterB, e2e.SecurityLevelNone),
		},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{
			e2e.DefaultEndpoint(clusterA, "localhost", []uint32{testutils.ParsePort(t, serverA.Address)}),
			e2e.DefaultEndpoint(clusterB, "localhost", []uint32{testutils.ParsePort(t, serverB.Address)}),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)

	// Simulate an intermediary server that receives an incoming RPC containing
	// the header "x-route-target: cluster-a" in its IncomingContext, and then
	// initiates an outgoing client RPC using that same context without setting
	// any OutgoingContext metadata. Client-side routing  must strictly evaluate
	// OutgoingContext (which is empty) and select Route 2.
	incomingCtx := metadata.NewIncomingContext(ctx, metadata.Pairs(headerName, headerValue))
	var p peer.Peer
	if _, err := client.EmptyCall(incomingCtx, &testpb.Empty{}, grpc.Peer(&p)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	if p.Addr.String() != serverB.Address {
		t.Fatalf("Unexpected client routing to peer %s, want fallback Route 2 (clusterB at %s)", p.Addr.String(), serverB.Address)
	}
}
