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

package ringhash_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	_ "google.golang.org/grpc/xds"
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

type testService struct {
	testgrpc.TestServiceServer
}

func (*testService) EmptyCall(context.Context, *testpb.Empty) (*testpb.Empty, error) {
	return &testpb.Empty{}, nil
}

// TestRingHash_ReconnectToMoveOutOfTransientFailure tests the case where the
// ring contains a single subConn, and verifies that when the server goes down,
// the LB policy on the client automatically reconnects until the subChannel
// moves out of TRANSIENT_FAILURE.
// XXX do we need to keep this one?
func (s) TestRingHash_ReconnectToMoveOutOfTransientFailure(t *testing.T) {
	// Create a restartable listener to simulate server being down.
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)

	// Start a server backend exposing the test service.
	server := grpc.NewServer()
	defer server.Stop()
	testgrpc.RegisterTestServiceServer(server, &testService{})
	go func() {
		if err := server.Serve(lis); err != nil {
			t.Errorf("Serve() failed: %v", err)
		}
	}()

	// Create a clientConn with a manual resolver (which is used to push the
	// address of the test backend), and a default service config pointing to
	// the use of the ring_hash_experimental LB policy.
	const ringHashServiceConfig = `{"loadBalancingConfig": [{"ring_hash_experimental":{}}]}`
	r := manual.NewBuilderWithScheme("whatever")
	dopts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
		grpc.WithDefaultServiceConfig(ringHashServiceConfig),
	}
	cc, err := grpc.NewClient(r.Scheme()+":///test.server", dopts...)
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	// Push the address of the test backend through the manual resolver.
	r.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: lis.Addr().String()}}})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// Stopping the server listener will close the transport on the client,
	// which will lead to the channel eventually moving to IDLE. The ring_hash
	// LB policy is not expected to reconnect by itself at this point.
	lis.Stop()

	testutils.AwaitState(ctx, t, cc, connectivity.Idle)

	// Make an RPC to get the ring_hash LB policy to reconnect and thereby move
	// to TRANSIENT_FAILURE upon connection failure.
	client.EmptyCall(ctx, &testpb.Empty{})

	testutils.AwaitState(ctx, t, cc, connectivity.TransientFailure)

	// An RPC at this point is expected to fail.
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}); err == nil {
		t.Fatal("EmptyCall RPC succeeded when the channel is in TRANSIENT_FAILURE")
	}

	// Restart the server listener. The ring_hash LB polcy is expected to
	// attempt to reconnect on its own and come out of TRANSIENT_FAILURE, even
	// without an RPC attempt.
	lis.Restart()
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		if cc.GetState() == connectivity.Ready {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("Timeout waiting for channel to reach READY after server restart: %v", err)
	}

	// An RPC at this point is expected to fail.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}

// XXX this is copy pasted from
// https://github.com/grpc/grpc-go/blob/9199290ff8fee7c37283586929af39ba23dbab4b/xds/internal/balancer/clusterresolver/e2e_test/eds_impl_test.go#L93
// Perhaps we should move it to a common package?
func startTestServiceBackends(t *testing.T, numBackends int) ([]*stubserver.StubServer, func()) {
	t.Helper()
	var servers []*stubserver.StubServer
	for i := 0; i < numBackends; i++ {
		servers = append(servers, stubserver.StartTestService(t, &stubserver.StubServer{
			UnaryCallF: func(ctx context.Context, _ *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
				return &testpb.SimpleResponse{
					Hostname: strconv.Itoa(i),
				}, nil
			},
		}))
	}

	return servers, func() {
		for _, server := range servers {
			server.Stop()
		}
	}
}

// makeNonExistingBackends starts numBackends backends and stops them
// immediately to simulate a server that is not reachable.
func makeNonExistingBackends(t *testing.T, numBackends int) []*stubserver.StubServer {
	var servers []*stubserver.StubServer
	for i := 0; i < numBackends; i++ {
		servers = append(servers, stubserver.StartTestService(t, nil))
		servers[i].Stop()
	}
	return servers
}

// backendOptions returns a slice of e2e.BackendOptions for the given stub
// servers.
func backendOptions(t *testing.T, servers []*stubserver.StubServer) []e2e.BackendOptions {
	t.Helper()
	var backendOpts []e2e.BackendOptions
	for _, server := range servers {
		backendOpts = append(backendOpts, e2e.BackendOptions{
			Port: testutils.ParsePort(t, server.Address),
		})
	}
	return backendOpts
}

// TestRingHash_AggregateClusterFallBackFromRingHashAtStartup tests that when
// an aggregate cluster is configured with ring hash policy, and the first
// cluster is in transient failure, all RPCs are sent to the second cluster
// using the ring hash policy.
func (s) TestRingHash_AggregateClusterFallBackFromRingHashAtStartup(t *testing.T) {
	// origin: https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L97

	xdsServer, nodeID, _, xdsResolver, stop := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{
		AllowResourceSubset: true,
	})
	defer stop()

	nonExistantServers := makeNonExistingBackends(t, 2)
	servers, stop := startTestServiceBackends(t, 2)
	defer stop()

	newCluster1Name := "new_cluster_1"
	newEdsService1Name := "new_eds_service_1"
	newCluster2Name := "new_cluster_2"
	newEdsService2Name := "new_eds_service_2"

	ep1 := e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
		ClusterName: newEdsService1Name,
		Localities: []e2e.LocalityOptions{{
			Name:     "locality0",
			Weight:   1,
			Backends: backendOptions(t, nonExistantServers),
		}},
	})
	ep2 := e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
		ClusterName: newEdsService2Name,
		Localities: []e2e.LocalityOptions{{
			Name:     "locality0",
			Weight:   1,
			Backends: backendOptions(t, servers),
		}},
	})
	cluster1 := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: newCluster1Name,
		ServiceName: newEdsService1Name,
		Policy:      e2e.LoadBalancingPolicyRingHash,
	})
	cluster2 := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		ClusterName: newCluster2Name,
		ServiceName: newEdsService2Name,
		Policy:      e2e.LoadBalancingPolicyRingHash,
	})
	t.Log(cluster1, cluster2, ep1, ep2) // XXX
	cluster := e2e.ClusterResourceWithOptions(e2e.ClusterOptions{
		Type:        e2e.ClusterTypeAggregate,
		ClusterName: "envoy.clusters.aggregate",
		ChildNames:  []string{newCluster1Name, newCluster2Name},
	})
	route := e2e.DefaultRouteConfig("new_route", "test.server", newCluster1Name)
	hashPolicy := v3routepb.RouteAction_HashPolicy{
		PolicySpecifier: &v3routepb.RouteAction_HashPolicy_FilterState_{
			FilterState: &v3routepb.RouteAction_HashPolicy_FilterState{
				Key: "io.grpc.channel_id",
			},
		},
	}
	action := route.VirtualHosts[0].Routes[0].Action.(*v3routepb.Route_Route)
	action.Route.HashPolicy = []*v3routepb.RouteAction_HashPolicy{&hashPolicy}
	action.Route.ClusterSpecifier = &v3routepb.RouteAction_Cluster{Cluster: "envoy.clusters.aggregate"}
	listener := e2e.DefaultClientListener("test.server", route.Name)

	err := xdsServer.Update(context.Background(), e2e.UpdateOptions{
		NodeID:    nodeID,
		Endpoints: []*endpointv3.ClusterLoadAssignment{ep1, ep2},
		Clusters:  []*v3clusterpb.Cluster{cluster, cluster1, cluster2},
		Routes:    []*v3routepb.RouteConfiguration{route},
		Listeners: []*v3listenerpb.Listener{listener},
	})
	if err != nil {
		t.Fatalf("failed to update server: %v", err)
	}

	conn, err := grpc.NewClient("xds:///test.server", grpc.WithResolvers(xdsResolver), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to create client: %s", err)
	}
	conn.Connect() // force triggering resource fetch. This isn't great, see comment below.
	defer conn.Close()
	client := testgrpc.NewTestServiceClient(conn)

	// Here we use the hostname field in the response proto to verify which
	// backend we routed to. In c-core it uses the number of requests received
	// from the backends directly, but we don't have an easy way to add mutable
	// variables to stubserver.Stubserver. This is supported from their e2e
	// test framework, so perhaps we should also support this?
	//
	var foundHostname string
	for i := 0; i < 100; i++ {
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			resp, err := client.UnaryCall(ctx, &testpb.SimpleRequest{})
			if err != nil {
				t.Fatalf("rpc UnaryCall() failed: %v", err)
			}
			// Verify that all requests were routed to a single server (ring hash behavior).
			if resp.Hostname == "" {
				t.Fatalf("response missing hostname")
			}
			if foundHostname == "" {
				foundHostname = resp.Hostname
			} else {
				if foundHostname != resp.Hostname {
					t.Error("request were not routed to a single server")
				}
			}
		}()
	}
	if foundHostname == "" {
		t.Error("request expected to contain the same hostnames")
	}
}

// TestRingHash_AggregateClusterFallBackFromRingHashToLogicalDnsAtStartup tests
// that... XXX
func (s) TestRingHash_AggregateClusterFallBackFromRingHashToLogicalDnsAtStartup(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L157
}

// TestRingHash_AggregateClusterFallBackFromRingHashToLogicalDnsAtStartupNoFailedRpcs
// tests that... XXX
func (s) TestRingHash_AggregateClusterFallBackFromRingHashToLogicalDnsAtStartupNoFailedRpcs(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L225
}

// TestRingHash_ChannelIdHashing tests that ring hash policy that hashes using
// channel id ensures all RPCs to go 1 particular backend.
func (s) TestRingHash_ChannelIdHashing(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L327
}

// TestRingHash_HeaderHashing tests that ring hash policy that hashes using a
// header value can spread RPCs across all the backends.
func (s) TestRingHash_HeaderHashing(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L355
}

// TestRingHash_HeaderHashingWithRegexRewrite tests that ring hash policy that
// hashes using a header value and regex rewrite to aggregate RPCs to 1 backend.
func (s) TestRingHash_HeaderHashingWithRegexRewrite(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L406
}

// TestRingHash_NoHashPolicy tests that ring hash policy that hashes using a
// random value.
func (s) TestRingHash_NoHashPolicy(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L458
}

// TestRingHash_EndpointWeights tests that we observe endpoint weights.
func (s) TestRingHash_EndpointWeights(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L488
}

// TestRingHash_ContinuesPastTerminalPolicyThatDoesNotProduceResult tests that
// ring hash policy evaluation will continue past the terminal policy if no
// results are produced yet.
func (s) TestRingHash_ContinuesPastTerminalPolicyThatDoesNotProduceResult(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L533
}

// TestRingHash_HashOnHeaderThatIsNotPresent tests that a random hash is used
// when header hashing specified a header field that the RPC did not have.
func (s) TestRingHash_HashOnHeaderThatIsNotPresent(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L560
}

// TestRingHash_UnsupportedHashPolicyDefaultToRandomHashing tests that a random
// hash is used when only unsupported hash policies are configured.
func (s) TestRingHash_UnsupportedHashPolicyDefaultToRandomHashing(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L601
}

// TestRingHash_RandomHashingDistributionAccordingToEndpointWeight tests that
// ring hash policy that hashes using a random value can spread RPCs across all
// the backends according to locality weight.
func (s) TestRingHash_RandomHashingDistributionAccordingToEndpointWeight(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L644
}

// TestRingHash_RandomHashingDistributionAccordingToLocalityAndEndpointWeight
// tests that ring hash policy that hashes using a random value can spread RPCs
// across all the backends according to locality weight.
func (s) TestRingHash_RandomHashingDistributionAccordingToLocalityAndEndpointWeight(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L683
}

// TestRingHash_FixedHashingTerminalPolicy tests that ring hash policy that
// hashes using a fixed string ensures all RPCs to go 1 particular backend; and
// that subsequent hashing policies are ignored due to the setting of terminal.
func (s) TestRingHash_FixedHashingTerminalPolicy(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L724
}

// TestRingHash_IdleToReady tests that the channel will go from idle to ready
// via connecting; (tho it is not possible to catch the connecting state before
// moving to ready).
func (s) TestRingHash_IdleToReady(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L762
}

// Test that the channel will transition to READY once it starts
// connecting even if there are no RPCs being sent to the picker.
func (s) TestRingHash_ContinuesConnectingWithoutPicks(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L782
}

// TestRingHash_ContinuesConnectingWithoutPicksOneSubchannelAtATime tests that
// when we trigger internal connection attempts without picks, we do so for only
// one subchannel at a time.
func (s) TestRingHash_ContinuesConnectingWithoutPicksOneSubchannelAtATime(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L825
}

// TestRingHash_TransientFailureCheckNextOne tests that when the first pick is
// down leading to a transient failure, we will move on to the next ring hash
// entry.
func (s) TestRingHash_TransientFailureCheckNextOne(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L909
}

// TestRingHash_TransientFailureCheckNextOneWithMultipleSubchannels tests that
// when a backend goes down, we will move on to the next subchannel (with a
// lower priority). When the backend comes back up, traffic will move back.
func (s) TestRingHash_SwitchToLowerPriorityAndThenBack(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L939
}

// TestRingHash_ReattemptWhenAllEndpointsUnreachable tests when all backends are
// down, we will keep reattempting.
func (s) TestRingHash_ReattemptWhenAllEndpointsUnreachable(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L976
}

// TestRingHash_TransientFailureSkipToAvailableReady tests that when all
// backends are down and then up, we may pick a TF backend and we will then jump
// to ready backend.
func (s) TestRingHash_TransientFailureSkipToAvailableReady(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L1008
}

// TestRingHash_ReattemptWhenGoingFromTransientFailureToIdle tests for a bug
// seen in the wild where ring_hash started with no endpoints and reported
// TRANSIENT_FAILURE, then got an update with endpoints and reported IDLE, but
// the picker update was squelched, so it failed to ever get reconnected.
func (s) TestRingHash_ReattemptWhenGoingFromTransientFailureToIdle(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L1090
}

// UnsupportedHashPolicyUntilChannelIdHashing tests that unsupported hash policy
// types are all ignored before a supported policy.
func (s) TestRingHash_UnsupportedHashPolicyUntilChannelIdHashing(t *testing.T) {
	// https://github.com/grpc/grpc/blob/083bbee4805c14ce62e6c9535fe936f68b854c4f/test/cpp/end2end/xds/xds_ring_hash_end2end_test.cc#L1125
}
