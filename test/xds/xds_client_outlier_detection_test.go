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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	testgrpc "google.golang.org/grpc/test/grpc_testing"
	testpb "google.golang.org/grpc/test/grpc_testing"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestOutlierDetection_NoopConfig tests the scenario where the Outlier
// Detection feature is enabled on the gRPC client, but it receives no Outlier
// Detection configuration from the management server. This should result in a
// no-op Outlier Detection configuration being used to configure the Outlier
// Detection balancer. This test verifies that an RPC is able to proceed
// normally with this configuration.
func (s) TestOutlierDetection_NoopConfig(t *testing.T) {
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

// clientResourcesMultipleBackendsAndOD returns xDS resources which correspond
// to multiple upstreams, corresponding different backends listening on
// different localhost:port combinations. The resources also configure an
// Outlier Detection Balancer set up with Failure Percentage Algorithm, which
// ejects endpoints based on failure rate.
func clientResourcesMultipleBackendsAndOD(params e2e.ResourceParams, ports []uint32) e2e.UpdateOptions {
	routeConfigName := "route-" + params.DialTarget
	clusterName := "cluster-" + params.DialTarget
	endpointsName := "endpoints-" + params.DialTarget
	return e2e.UpdateOptions{
		NodeID:    params.NodeID,
		Listeners: []*v3listenerpb.Listener{e2e.DefaultClientListener(params.DialTarget, routeConfigName)},
		Routes:    []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, params.DialTarget, clusterName)},
		Clusters:  []*v3clusterpb.Cluster{clusterWithOutlierDetection(clusterName, endpointsName, params.SecLevel)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(endpointsName, params.Host, ports)},
	}
}

func clusterWithOutlierDetection(clusterName, edsServiceName string, secLevel e2e.SecurityLevel) *v3clusterpb.Cluster {
	cluster := e2e.DefaultCluster(clusterName, edsServiceName, secLevel)
	cluster.OutlierDetection = &v3clusterpb.OutlierDetection{
		Interval:                       &durationpb.Duration{Nanos: 50000000}, // .5 seconds
		BaseEjectionTime:               &durationpb.Duration{Seconds: 30},
		MaxEjectionTime:                &durationpb.Duration{Seconds: 300},
		MaxEjectionPercent:             &wrapperspb.UInt32Value{Value: 1},
		FailurePercentageThreshold:     &wrapperspb.UInt32Value{Value: 50},
		EnforcingFailurePercentage:     &wrapperspb.UInt32Value{Value: 100},
		FailurePercentageRequestVolume: &wrapperspb.UInt32Value{Value: 1},
		FailurePercentageMinimumHosts:  &wrapperspb.UInt32Value{Value: 1},
	}
	return cluster
}

// TestOutlierDetectionWithOutlier tests the Outlier Detection Balancer e2e. It
// spins up three backends, one which consistently errors, and configures the
// ClientConn using xDS to connect to all three of those backends. The Outlier
// Detection Balancer should eject the connection to the backend which
// constantly errors, and thus RPC's should mainly go to backend 1 and 2.
func (s) TestOutlierDetectionWithOutlier(t *testing.T) {
	managementServer, nodeID, _, resolver, cleanup := e2e.SetupManagementServer(t, nil)
	defer cleanup()

	// Counters for how many times backends got called.
	var count1, count2, count3 int

	// Working backend 1.
	port1, cleanup1 := startTestService(t, &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			count1++
			return &testpb.Empty{}, nil
		},
		Address: "localhost:0",
	})
	defer cleanup1()

	// Working backend 2.
	port2, cleanup2 := startTestService(t, &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			count2++
			return &testpb.Empty{}, nil
		},
		Address: "localhost:0",
	})
	defer cleanup2()

	// Backend 3 that will always return an error and eventually ejected.
	port3, cleanup3 := startTestService(t, &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			count3++
			return nil, errors.New("some error")
		},
		Address: "localhost:0",
	})
	defer cleanup3()

	const serviceName = "my-service-client-side-xds"
	resources := clientResourcesMultipleBackendsAndOD(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		SecLevel:   e2e.SecurityLevelNone,
	}, []uint32{port1, port2, port3})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	cc, err := grpc.Dial(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	for i := 0; i < 2000; i++ {
		// Can either error or not depending on the backend called.
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil && !strings.Contains(err.Error(), "some error") {
			t.Fatalf("rpc EmptyCall() failed: %v", err)
		}
		time.Sleep(time.Millisecond)
	}

	// Backend 1 should've gotten more than 1/3rd of the load as backend 3
	// should get ejected, leaving only 1 and 2.
	if count1 < 700 {
		t.Fatalf("backend 1 should've gotten more than 1/3rd of the load")
	}
	// Backend 2 should've gotten more than 1/3rd of the load as backend 3
	// should get ejected, leaving only 1 and 2.
	if count2 < 700 {
		t.Fatalf("backend 2 should've gotten more than 1/3rd of the load")
	}
	// Backend 3 should've gotten less than 1/3rd of the load since it gets
	// ejected.
	if count3 > 650 {
		t.Fatalf("backend 1 should've gotten more than 1/3rd of the load")
	}
}
