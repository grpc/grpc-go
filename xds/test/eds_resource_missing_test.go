/*
 *
 * Copyright 2025 gRPC authors.
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
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/status"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	_ "google.golang.org/grpc/xds" // To register the xDS resolver and LB policies.
)

const (
	defaultTestWatchExpiryTimeout = 500 * time.Millisecond
	defaultTestTimeout            = 5 * time.Second
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// Test verifies the xDS-enabled gRPC channel's behavior when the management
// server fails to send an EDS resource referenced by a Cluster resource. The
// expected outcome is an RPC failure with a status code Unavailable and a
// message indicating the absence of available targets.
func (s) TestEDS_MissingResource(t *testing.T) {
	// Start an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	config, err := bootstrap.NewConfigFromContents(bc)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bc), err)
	}

	// Create an xDS client with a short resource expiry timer.
	pool := xdsclient.NewPool(config)
	xdsC, xdsClose, err := pool.NewClientForTesting(xdsclient.OptionsForTesting{
		Name:               t.Name(),
		WatchExpiryTimeout: defaultTestWatchExpiryTimeout,
	})
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer xdsClose()

	// Create an xDS resolver for the test that uses the above xDS client.
	resolverBuilder := internal.NewXDSResolverWithClientForTesting.(func(xdsclient.XDSClient) (resolver.Builder, error))
	xdsResolver, err := resolverBuilder(xdsC)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// Create resources on the management server. No EDS resource is configured.
	const serviceName = "my-service-client-side-xds"
	const routeConfigName = "route-" + serviceName
	const clusterName = "cluster-" + serviceName
	const endpointsName = "endpoints-" + serviceName
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		SkipValidation: true, // Cluster resource refers to an EDS resource that is not configured.
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)},
		Routes:         []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, serviceName, clusterName)},
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, endpointsName, e2e.SecurityLevelNone)},
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn with the xds:/// scheme.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("Failed to create a grpc channel: %v", err)
	}
	defer cc.Close()

	// Make an RPC and verify that it fails with the expected error.
	client := testgrpc.NewTestServiceClient(cc)
	_, err = client.EmptyCall(ctx, &testpb.Empty{})
	if err == nil {
		t.Fatal("EmptyCall() succeeded, want failure")
	}
	if gotCode, wantCode := status.Code(err), codes.Unavailable; gotCode != wantCode {
		t.Errorf("EmptyCall() failed with code = %v, want %s", gotCode, wantCode)
	}
	if gotMsg, wantMsg := err.Error(), "no targets to pick from"; !strings.Contains(gotMsg, wantMsg) {
		t.Errorf("EmptyCall() failed with message = %q, want to contain %q", gotMsg, wantMsg)
	}
}

// Test verifies the xDS-enabled gRPC channel's behavior when the management
// server sends an EDS resource with no endpoints. The expected outcome is an
// RPC failure with a status code Unavailable and a message indicating the
// absence of available targets.
func (s) TestEDS_NoEndpointsInResource(t *testing.T) {
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	// Create resources on the management server, with the EDS resource
	// containing no endpoints.
	const serviceName = "my-service-client-side-xds"
	const routeConfigName = "route-" + serviceName
	const clusterName = "cluster-" + serviceName
	const endpointsName = "endpoints-" + serviceName
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		SkipValidation: true, // Cluster resource refers to an EDS resource that is not configured.
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(serviceName, routeConfigName)},
		Routes:         []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(routeConfigName, serviceName, clusterName)},
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, endpointsName, e2e.SecurityLevelNone)},
		Endpoints: []*v3endpointpb.ClusterLoadAssignment{
			e2e.EndpointResourceWithOptions(e2e.EndpointOptions{
				ClusterName: "endpoints-" + serviceName,
				Host:        "localhost",
			}),
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn with the xds:/// scheme.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("Failed to create a grpc channel: %v", err)
	}
	defer cc.Close()

	// Make an RPC and verify that it fails with the expected error.
	client := testgrpc.NewTestServiceClient(cc)
	_, err = client.EmptyCall(ctx, &testpb.Empty{})
	if err == nil {
		t.Fatal("EmptyCall() succeeded, want failure")
	}
	if gotCode, wantCode := status.Code(err), codes.Unavailable; gotCode != wantCode {
		t.Errorf("EmptyCall() failed with code = %v, want %s", gotCode, wantCode)
	}
	if gotMsg, wantMsg := err.Error(), "no targets to pick from"; !strings.Contains(gotMsg, wantMsg) {
		t.Errorf("EmptyCall() failed with message = %q, want to contain %q", gotMsg, wantMsg)
	}
}
