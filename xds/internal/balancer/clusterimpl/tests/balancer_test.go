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

package clusterimpl_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/status"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	_ "google.golang.org/grpc/xds"
)

const (
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 100 * time.Millisecond
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestConfigUpdateWithSameLoadReportingServerConfig tests the scenario where
// the clusterimpl LB policy receives a config update with no change in the load
// reporting server configuration. The test verifies that the existing load
// repoting stream is not terminated and that a new load reporting stream is not
// created.
func (s) TestConfigUpdateWithSameLoadReportingServerConfig(t *testing.T) {
	// Create an xDS management server that serves ADS and LRS requests.
	opts := e2e.ManagementServerOptions{SupportLoadReportingService: true}
	mgmtServer, nodeID, _, resolver, mgmtServerCleanup := e2e.SetupManagementServer(t, opts)
	defer mgmtServerCleanup()

	// Start a server backend exposing the test service.
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	// Configure the xDS management server with default resources. Override the
	// default cluster to include an LRS server config pointing to self.
	const serviceName = "my-test-xds-service"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	resources.Clusters[0].LrsServer = &v3corepb.ConfigSource{
		ConfigSourceSpecifier: &v3corepb.ConfigSource_Self{
			Self: &v3corepb.SelfConfigSource{},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.Dial(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// Ensure that an LRS stream is created.
	if _, err := mgmtServer.LRSServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Failure when waiting for an LRS stream to be opened: %v", err)
	}

	// Configure a new resource on the management server with drop config that
	// drops all RPCs, but with no change in the load reporting server config.
	resources.Endpoints = []*v3endpointpb.ClusterLoadAssignment{
		e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
			ClusterName: "endpoints-" + serviceName,
			Host:        "localhost",
			Localities: []e2e.LocalityOptions{
				{
					Backends: []e2e.BackendOptions{{Port: testutils.ParsePort(t, server.Address)}},
					Weight:   1,
				},
			},
			DropPercents: map[string]int{"test-drop-everything": 100},
		}),
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Repeatedly send RPCs until we sees that they are getting dropped, or the
	// test context deadline expires. The former indicates that new config with
	// drops has been applied.
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		_, err := client.EmptyCall(ctx, &testpb.Empty{})
		if err != nil && status.Code(err) == codes.Unavailable && strings.Contains(err.Error(), "RPC is dropped") {
			break
		}
	}
	if ctx.Err() != nil {
		t.Fatalf("Timeout when waiting for RPCs to be dropped after config update")
	}

	// Ensure that the old LRS stream is not closed.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := mgmtServer.LRSServer.LRSStreamCloseChan.Receive(sCtx); err == nil {
		t.Fatal("LRS stream closed when expected not to")
	}

	// Also ensure that a new LRS stream is not created.
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := mgmtServer.LRSServer.LRSStreamOpenChan.Receive(sCtx); err == nil {
		t.Fatal("New LRS stream created when expected not to")
	}
}
