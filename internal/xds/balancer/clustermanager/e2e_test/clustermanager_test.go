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

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/pickfirst"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/hierarchy"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/balancer/clustermanager"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	v3xdsxdstypepb "github.com/cncf/xds/go/xds/type/v3"
	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3pickfirstpb "github.com/envoyproxy/go-control-plane/envoy/extensions/load_balancing_policies/pick_first/v3"
	"github.com/google/uuid"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	_ "google.golang.org/grpc/xds" // Register the xDS name resolver and related LB policies.
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

func makeEmptyCallRPCAndVerifyPeer(ctx context.Context, client testgrpc.TestServiceClient, wantPeer string) error {
	peer := &peer.Peer{}
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peer)); err != nil {
		return fmt.Errorf("EmptyCall() failed: %v", err)
	}
	if gotPeer := peer.Addr.String(); gotPeer != wantPeer {
		return fmt.Errorf("EmptyCall() routed to %q, want to be routed to: %q", gotPeer, wantPeer)
	}
	return nil
}

func makeUnaryCallRPCAndVerifyPeer(ctx context.Context, client testgrpc.TestServiceClient, wantPeer string) error {
	peer := &peer.Peer{}
	if _, err := client.UnaryCall(ctx, &testpb.SimpleRequest{}, grpc.Peer(peer)); err != nil {
		return fmt.Errorf("UnaryCall() failed: %v", err)
	}
	if gotPeer := peer.Addr.String(); gotPeer != wantPeer {
		return fmt.Errorf("EmptyCall() routed to %q, want to be routed to: %q", gotPeer, wantPeer)
	}
	return nil
}

func (s) TestConfigUpdate_ChildPolicyChange(t *testing.T) {
	// Spin up an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})

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

	// Configure client side xDS resources on the management server.
	const (
		serviceName     = "my-service-client-side-xds"
		routeConfigName = "route-" + serviceName
		clusterName1    = "cluster1-" + serviceName
		clusterName2    = "cluster2-" + serviceName
		endpointsName1  = "endpoints1-" + serviceName
		endpointsName2  = "endpoints2-" + serviceName
		endpointsName3  = "endpoints3-" + serviceName
	)
	// A single Listener resource pointing to the following Route
	// configuration:
	//   - "/grpc.testing.TestService/EmptyCall" --> cluster1
	//   - "/grpc.testing.TestService/UnaryCall" --> cluster2
	listeners := []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)}
	routes := []*v3routepb.RouteConfiguration{{
		Name: routeConfigName,
		VirtualHosts: []*v3routepb.VirtualHost{{
			Domains: []string{serviceName},
			Routes: []*v3routepb.Route{
				{
					Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/EmptyCall"}},
					Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
						ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName1},
					}},
				},
				{
					Match: &v3routepb.RouteMatch{PathSpecifier: &v3routepb.RouteMatch_Prefix{Prefix: "/grpc.testing.TestService/UnaryCall"}},
					Action: &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
						ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: clusterName2},
					}},
				},
			},
		}},
	}}
	// Two cluster resources corresponding to the ones mentioned in the above
	// route configuration resource. These are configured with round_robin as
	// their endpoint picking policy.
	clusters := []*v3clusterpb.Cluster{
		e2e.DefaultCluster(clusterName1, endpointsName1, e2e.SecurityLevelNone),
		e2e.DefaultCluster(clusterName2, endpointsName2, e2e.SecurityLevelNone),
	}
	// Spin up two test backends, one for each cluster below.
	server1 := stubserver.StartTestService(t, nil)
	defer server1.Stop()
	server2 := stubserver.StartTestService(t, nil)
	defer server2.Stop()
	// Two endpoints resources, each with one backend from above.
	endpoints := []*v3endpointpb.ClusterLoadAssignment{
		e2e.DefaultEndpoint(endpointsName1, "localhost", []uint32{testutils.ParsePort(t, server1.Address)}),
		e2e.DefaultEndpoint(endpointsName2, "localhost", []uint32{testutils.ParsePort(t, server2.Address)}),
	}
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      listeners,
		Routes:         routes,
		Clusters:       clusters,
		Endpoints:      endpoints,
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	// Make an EmptyCall RPC and verify that it is routed to cluster1.
	client := testgrpc.NewTestServiceClient(cc)
	if err := makeEmptyCallRPCAndVerifyPeer(ctx, client, server1.Address); err != nil {
		t.Fatal(err)
	}

	// Make a UnaryCall RPC and verify that it is routed to cluster2.
	if err := makeUnaryCallRPCAndVerifyPeer(ctx, client, server2.Address); err != nil {
		t.Fatal(err)
	}

	// Create a wrapped pickfirst LB policy. When the endpoint picking policy on
	// the cluster resource is changed to pickfirst, this will allow us to
	// verify that load balancing configuration is pushed to it.
	pfBuilder := balancer.Get(pickfirst.Name)
	internal.BalancerUnregister(pfBuilder.Name())

	lbCfgCh := make(chan serviceconfig.LoadBalancingConfig, 1)
	stub.Register(pfBuilder.Name(), stub.BalancerFuncs{
		ParseConfig: func(lbCfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return pfBuilder.(balancer.ConfigParser).ParseConfig(lbCfg)
		},
		Init: func(bd *stub.BalancerData) {
			bd.ChildBalancer = pfBuilder.Build(bd.ClientConn, bd.BuildOptions)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			select {
			case lbCfgCh <- ccs.BalancerConfig:
			default:
			}
			return bd.ChildBalancer.UpdateClientConnState(ccs)
		},
		Close: func(bd *stub.BalancerData) {
			bd.ChildBalancer.Close()
		},
	})

	// Send a config update that changes the child policy configuration for one
	// of the clusters to pickfirst. The endpoints resource is also changed here
	// to ensure that we can verify that the new child policy
	cluster2 := &v3clusterpb.Cluster{
		Name:                 clusterName2,
		ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
		EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
			EdsConfig: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
					Ads: &v3corepb.AggregatedConfigSource{},
				},
			},
			ServiceName: endpointsName3,
		},
		LoadBalancingPolicy: &v3clusterpb.LoadBalancingPolicy{
			Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
				{
					TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
						TypedConfig: testutils.MarshalAny(t, &v3pickfirstpb.PickFirst{
							ShuffleAddressList: true,
						}),
					},
				},
			},
		},
	}
	server3 := stubserver.StartTestService(t, nil)
	defer server3.Stop()
	endpoints3 := e2e.DefaultEndpoint(endpointsName3, "localhost", []uint32{testutils.ParsePort(t, server3.Address)})
	resources.Clusters = append(resources.Clusters, cluster2)
	resources.Endpoints = append(resources.Endpoints, endpoints3)
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ctx.Done():
		t.Fatalf("Timeout when waiting for configuration to be pushed to the new pickfirst child policy")
	case <-lbCfgCh:
	}

	// Ensure RPCs are still succeeding.

	// Make an EmptyCall RPC and verify that it is routed to cluster1.
	if err := makeEmptyCallRPCAndVerifyPeer(ctx, client, server1.Address); err != nil {
		t.Fatal(err)
	}

	// Make a UnaryCall RPC and verify that it is routed to cluster2, and the
	// new endpoints resource.
	for ; ctx.Err() != nil; <-time.After(defaultTestShortTimeout) {
		if err := makeUnaryCallRPCAndVerifyPeer(ctx, client, server3.Address); err == nil {
			break
		}
		t.Log(err)
	}
	if ctx.Err() != nil {
		t.Fatal("Timeout when waiting for RPCs to cluster2 to be routed to the new endpoints resource")
	}

	// Send a config update that changes the child policy configuration for one
	// of the clusters to an unsupported LB policy. This should result in
	// failure of RPCs to that cluster.
	cluster2 = &v3clusterpb.Cluster{
		Name:                 clusterName2,
		ClusterDiscoveryType: &v3clusterpb.Cluster_Type{Type: v3clusterpb.Cluster_EDS},
		EdsClusterConfig: &v3clusterpb.Cluster_EdsClusterConfig{
			EdsConfig: &v3corepb.ConfigSource{
				ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{
					Ads: &v3corepb.AggregatedConfigSource{},
				},
			},
			ServiceName: endpointsName3,
		},
		LoadBalancingPolicy: &v3clusterpb.LoadBalancingPolicy{
			Policies: []*v3clusterpb.LoadBalancingPolicy_Policy{
				{
					TypedExtensionConfig: &v3corepb.TypedExtensionConfig{
						// The type not registered in gRPC Policy registry.
						TypedConfig: testutils.MarshalAny(t, &v3xdsxdstypepb.TypedStruct{
							TypeUrl: "type.googleapis.com/myorg.ThisTypeDoesNotExist",
							Value:   &structpb.Struct{},
						}),
					},
				},
			},
		},
	}
	resources.Clusters[1] = cluster2
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// At this point, RPCs to cluster1 should continue to succeed, while RPCs to
	// cluster2 should start to fail.

	// Make an EmptyCall RPC and verify that it is routed to cluster1.
	if err := makeEmptyCallRPCAndVerifyPeer(ctx, client, server1.Address); err != nil {
		t.Fatal(err)
	}

	// Make a UnaryCall RPC and verify that it starts to fail.
	for ; ctx.Err() != nil; <-time.After(defaultTestShortTimeout) {
		_, err := client.UnaryCall(ctx, &testpb.SimpleRequest{})
		got := status.Code(err)
		if got == codes.Unavailable {
			break
		}
		t.Logf("UnaryCall() returned code: %v, want %v", got, codes.Unavailable)
	}
	if ctx.Err() != nil {
		t.Fatal("Timeout when waiting for RPCs to cluster2 to start failing")
	}

	// Channel should still be READY.
	if got, want := cc.GetState(), connectivity.Ready; got != want {
		t.Fatalf("grpc.ClientConn in state %v, want %v", got, want)
	}
}

// fixedClusterConfigSelector is a config selector that routes all RPCs to a
// single cluster of the xds_cluster_manager LB policy.
type fixedClusterConfigSelector struct {
	cluster string
}

func (cs *fixedClusterConfigSelector) SelectConfig(info iresolver.RPCInfo) (*iresolver.RPCConfig, error) {
	return &iresolver.RPCConfig{Context: clustermanager.SetPickedCluster(info.Context, cs.cluster)}, nil
}

// Tests the scenario where a resolver update adds a new cluster to the
// xds_cluster_manager LB policy config and simultaneously provides a config
// selector that routes RPCs to the new cluster. Per gRFC A31, the channel must
// update the LB policy before applying the new config selector, so that it is
// not possible for an RPC to be routed to a cluster that the current picker is
// not aware of.
//
// The test blocks the LB policy in the middle of processing the update (inside
// the new cluster's child policy) and verifies that RPCs made during this time
// continue to use the old config selector, and hence succeed.
func (s) TestConfigUpdate_NewClusterNotUsedBeforePickerUpdate(t *testing.T) {
	backend1 := stubserver.StartTestService(t, nil)
	defer backend1.Stop()
	backend2 := stubserver.StartTestService(t, nil)
	defer backend2.Stop()

	// Register a child policy that wraps pick_first and blocks in
	// UpdateClientConnState until the test unblocks it.
	const blockingPolicyName = "blocking_pick_first_new_cluster_not_used_before_picker_update"
	pfBuilder := balancer.Get(pickfirst.Name)
	updateReceived := make(chan struct{}, 1)
	unblockCh := make(chan struct{})
	var unblockOnce sync.Once
	unblock := func() { unblockOnce.Do(func() { close(unblockCh) }) }
	stub.Register(blockingPolicyName, stub.BalancerFuncs{
		ParseConfig: func(lbCfg json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
			return pfBuilder.(balancer.ConfigParser).ParseConfig(lbCfg)
		},
		Init: func(bd *stub.BalancerData) {
			bd.ChildBalancer = pfBuilder.Build(bd.ClientConn, bd.BuildOptions)
		},
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			select {
			case updateReceived <- struct{}{}:
			default:
			}
			<-unblockCh
			return bd.ChildBalancer.UpdateClientConnState(ccs)
		},
		ExitIdle: func(bd *stub.BalancerData) {
			bd.ChildBalancer.ExitIdle()
		},
		Close: func(bd *stub.BalancerData) {
			bd.ChildBalancer.Close()
		},
	})

	const (
		cluster1 = "cluster1"
		cluster2 = "cluster2"
	)
	endpoints := []resolver.Endpoint{
		hierarchy.SetInEndpoint(resolver.Endpoint{Addresses: []resolver.Address{{Addr: backend1.Address}}}, []string{cluster1}),
		hierarchy.SetInEndpoint(resolver.Endpoint{Addresses: []resolver.Address{{Addr: backend2.Address}}}, []string{cluster2}),
	}
	parseSC := func(sc string) *serviceconfig.ParseResult {
		t.Helper()
		pr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(sc)
		if pr.Err != nil {
			t.Fatalf("Failed to parse service config %q: %v", sc, pr.Err)
		}
		return pr
	}

	// Initial state: only cluster1 exists and all RPCs are routed to it.
	sc1 := parseSC(fmt.Sprintf(`{
  "loadBalancingConfig": [{
    "xds_cluster_manager_experimental": {
      "children": {
        %q: {"childPolicy": [{"pick_first": {}}]}
      }
    }
  }]
}`, cluster1))
	r := manual.NewBuilderWithScheme("cluster-manager-e2e")
	r.InitialState(iresolver.SetConfigSelector(resolver.State{
		Endpoints:     endpoints,
		ServiceConfig: sc1,
	}, &fixedClusterConfigSelector{cluster: cluster1}))

	cc, err := grpc.NewClient(r.Scheme()+":///test.server", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer cc.Close()
	// Ensure the LB policy is unblocked before the channel is closed, else
	// closing the channel would block forever.
	defer unblock()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client := testgrpc.NewTestServiceClient(cc)
	if err := makeEmptyCallRPCAndVerifyPeer(ctx, client, backend1.Address); err != nil {
		t.Fatal(err)
	}

	// Push an update that adds cluster2 and a config selector that routes all
	// RPCs to cluster2. The update blocks in the LB policy, so push it from a
	// separate goroutine.
	sc2 := parseSC(fmt.Sprintf(`{
  "loadBalancingConfig": [{
    "xds_cluster_manager_experimental": {
      "children": {
        %q: {"childPolicy": [{"pick_first": {}}]},
        %q: {"childPolicy": [{%q: {}}]}
      }
    }
  }]
}`, cluster1, cluster2, blockingPolicyName))
	updateDone := make(chan struct{})
	go func() {
		r.UpdateState(iresolver.SetConfigSelector(resolver.State{
			Endpoints:     endpoints,
			ServiceConfig: sc2,
		}, &fixedClusterConfigSelector{cluster: cluster2}))
		close(updateDone)
	}()

	select {
	case <-updateReceived:
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for the new child policy to receive a config update")
	}

	// The LB policy is now in the middle of processing the update, and has not
	// yet produced a picker that knows about cluster2. The new config selector
	// must not be in use yet, so RPCs should still be routed to cluster1.
	if err := makeEmptyCallRPCAndVerifyPeer(ctx, client, backend1.Address); err != nil {
		t.Fatalf("RPC made while the LB policy was processing the update failed: %v; the new config selector was likely applied before the LB policy was updated", err)
	}

	// Unblock the LB policy and wait for the update to complete. RPCs should
	// now be routed to cluster2.
	unblock()
	select {
	case <-updateDone:
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for the resolver update to complete")
	}
	if err := makeEmptyCallRPCAndVerifyPeer(ctx, client, backend2.Address); err != nil {
		t.Fatal(err)
	}
}
