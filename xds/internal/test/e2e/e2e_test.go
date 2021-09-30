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
 */

package e2e

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	channelzpb "google.golang.org/grpc/channelz/grpc_channelz_v1"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/xds/internal/testutils/e2e"
)

func TestMain(m *testing.M) {
	flag.Parse()
	if _, err := os.Stat(*clientPath); os.IsNotExist(err) {
		return
	}
	if _, err := os.Stat(*serverPath); os.IsNotExist(err) {
		return
	}
	os.Exit(m.Run())
}

func TestPingPong(t *testing.T) {
	const testName = "pingpong"

	cp, err := newControlPlane(testName)
	if err != nil {
		t.Fatalf("failed to start control-plane: %v", err)
	}
	defer cp.stop()

	var clientLog bytes.Buffer
	c, err := newClient(testName, &clientLog)
	if err != nil {
		t.Fatalf("failed to start client: %v", err)
	}
	defer c.stop()
	// defer func() { t.Logf("----- client logs -----\n%v", clientLog.String()) }()

	var serverLog bytes.Buffer
	servers, err := newServers(testName, &serverLog, 1)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		for _, s := range servers {
			s.stop()
		}
	}()
	// defer func() { t.Logf("----- server logs -----\n%v", serverLog.String()) }()

	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: testName,
		NodeID:     cp.nodeID,
		Host:       "localhost",
		Port:       serverPort,
		SecLevel:   e2e.SecurityLevelNone,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cp.server.Update(ctx, resources); err != nil {
		t.Fatalf("failed to update control plane resources: %v", err)
	}

	st, err := c.clientStats(ctx)
	if err != nil {
		t.Fatalf("failed to get client stats: %v", err)
	}
	if st.NumFailures != 0 {
		t.Fatalf("Got %v failures: %+v", st.NumFailures, st)
	}
}

// TestAffinity covers the affinity tests with ringhash policy.
// - client is configured to use ringhash, with 3 backends
// - all RPCs will hash a specific metadata header
// - verify that
//   - all RPCs with the same metadata value are sent to the same backend
//   - only one backend is Ready
// - send more RPCs with different metadata values until a new backend is picked, and verify that
//   - only two backends are in Ready
func TestAffinity(t *testing.T) {
	const (
		testName     = "affinity"
		backendCount = 3
		testMDKey    = "xds_md"
		testMDValue  = "unary_yranu"
	)

	cp, err := newControlPlane(testName)
	if err != nil {
		t.Fatalf("failed to start control-plane: %v", err)
	}
	defer cp.stop()

	var clientLog bytes.Buffer
	c, err := newClient(testName, &clientLog, "--rpc=EmptyCall", fmt.Sprintf("--metadata=EmptyCall:%s:%s", testMDKey, testMDValue))
	if err != nil {
		t.Fatalf("failed to start client: %v", err)
	}
	defer c.stop()
	// defer func() { t.Logf("----- client logs -----\n%v", clientLog.String()) }()

	var serverLog bytes.Buffer
	servers, err := newServers(testName, &serverLog, backendCount)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		for _, s := range servers {
			s.stop()
		}
	}()
	// defer func() { t.Logf("----- server logs -----\n%v", serverLog.String()) }()

	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: testName,
		NodeID:     cp.nodeID,
		Host:       "localhost",
		Port:       serverPort,
		SecLevel:   e2e.SecurityLevelNone,
	})

	// Update EDS to multiple backends.
	var ports []uint32
	for _, s := range servers {
		ports = append(ports, uint32(s.port))
	}
	edsMsg := resources.Endpoints[0]
	resources.Endpoints[0] = e2e.DefaultEndpoint(
		edsMsg.ClusterName,
		"localhost",
		ports,
	)

	// Update CDS lbpolicy to ringhash.
	cdsMsg := resources.Clusters[0]
	cdsMsg.LbPolicy = v3clusterpb.Cluster_RING_HASH

	// Update RDS to hash the header.
	rdsMsg := resources.Routes[0]
	rdsMsg.VirtualHosts[0].Routes[0].Action = &v3routepb.Route_Route{Route: &v3routepb.RouteAction{
		ClusterSpecifier: &v3routepb.RouteAction_Cluster{Cluster: cdsMsg.Name},
		HashPolicy: []*v3routepb.RouteAction_HashPolicy{{
			PolicySpecifier: &v3routepb.RouteAction_HashPolicy_Header_{
				Header: &v3routepb.RouteAction_HashPolicy_Header{
					HeaderName: testMDKey,
				},
			},
		}},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cp.server.Update(ctx, resources); err != nil {
		t.Fatalf("failed to update control plane resources: %v", err)
	}

	// We can skip CSDS check because there's no long delay as in TD.

	st, err := c.clientStats(ctx)
	if err != nil {
		t.Fatalf("failed to get client stats: %v", err)
	}
	if st.NumFailures != 0 {
		t.Fatalf("Got %v failures: %+v", st.NumFailures, st)
	}
	if len(st.RpcsByPeer) != 1 {
		t.Fatalf("more than 1 backends got traffic: %v, want 1", st.RpcsByPeer)
	}

	// Call channelz to verify that only one subchannel is in state Ready.
	scs, err := c.channelzSubChannels(ctx)
	if err != nil {
		t.Fatalf("failed to fetch channelz: %v", err)
	}
	verifySubConnStates(t, scs, map[channelzpb.ChannelConnectivityState_State]int{
		channelzpb.ChannelConnectivityState_READY: 1,
		channelzpb.ChannelConnectivityState_IDLE:  2,
	})

	// Send Unary call with different metadata value with integers starting from
	// 0. Stop when a second peer is picked.
	var (
		diffPeerPicked bool
		mdValue        int
	)
	for !diffPeerPicked {
		if err := c.configRPCs(ctx, &testpb.ClientConfigureRequest{
			Types: []testpb.ClientConfigureRequest_RpcType{
				testpb.ClientConfigureRequest_EMPTY_CALL,
				testpb.ClientConfigureRequest_UNARY_CALL,
			},
			Metadata: []*testpb.ClientConfigureRequest_Metadata{
				{Type: testpb.ClientConfigureRequest_EMPTY_CALL, Key: testMDKey, Value: testMDValue},
				{Type: testpb.ClientConfigureRequest_UNARY_CALL, Key: testMDKey, Value: strconv.Itoa(mdValue)},
			},
		}); err != nil {
			t.Fatalf("failed to configure RPC: %v", err)
		}

		st, err := c.clientStats(ctx)
		if err != nil {
			t.Fatalf("failed to get client stats: %v", err)
		}
		if st.NumFailures != 0 {
			t.Fatalf("Got %v failures: %+v", st.NumFailures, st)
		}
		if len(st.RpcsByPeer) == 2 {
			break
		}

		mdValue++
	}

	// Call channelz to verify that only one subchannel is in state Ready.
	scs2, err := c.channelzSubChannels(ctx)
	if err != nil {
		t.Fatalf("failed to fetch channelz: %v", err)
	}
	verifySubConnStates(t, scs2, map[channelzpb.ChannelConnectivityState_State]int{
		channelzpb.ChannelConnectivityState_READY: 2,
		channelzpb.ChannelConnectivityState_IDLE:  1,
	})
}
