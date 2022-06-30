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
	"strings"
	"testing"
	"time"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	xdsinternal "google.golang.org/grpc/internal/xds"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/status"
	testgrpc "google.golang.org/grpc/test/grpc_testing"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

// TestClientSideFederation tests that federation is supported.
//
// In this test, some xDS responses contain resource names in another authority
// (in the new resource name style):
// - LDS: old style, no authority (default authority)
// - RDS: new style, in a different authority
// - CDS: old style, no authority (default authority)
// - EDS: new style, in a different authority
func (s) TestClientSideFederation(t *testing.T) {
	oldXDSFederation := envconfig.XDSFederation
	envconfig.XDSFederation = true
	defer func() { envconfig.XDSFederation = oldXDSFederation }()

	// Start a management server as the default authority.
	serverDefaultAuth, err := e2e.StartManagementServer()
	if err != nil {
		t.Fatalf("Failed to spin up the xDS management server: %v", err)
	}
	t.Cleanup(serverDefaultAuth.Stop)

	// Start another management server as the other authority.
	const nonDefaultAuth = "non-default-auth"
	serverAnotherAuth, err := e2e.StartManagementServer()
	if err != nil {
		t.Fatalf("Failed to spin up the xDS management server: %v", err)
	}
	t.Cleanup(serverAnotherAuth.Stop)

	// Create a bootstrap file in a temporary directory.
	nodeID := uuid.New().String()
	bootstrapContents, err := xdsinternal.BootstrapContents(xdsinternal.BootstrapOptions{
		Version:                            xdsinternal.TransportV3,
		NodeID:                             nodeID,
		ServerURI:                          serverDefaultAuth.Address,
		ServerListenerResourceNameTemplate: e2e.ServerListenerResourceNameTemplate,
		// Specify the address of the non-default authority.
		Authorities: map[string]string{nonDefaultAuth: serverAnotherAuth.Address},
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap file: %v", err)
	}

	resolverBuilder := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))
	resolver, err := resolverBuilder(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}
	port, cleanup := startTestService(t, nil)
	defer cleanup()

	const serviceName = "my-service-client-side-xds"
	// LDS is old style name.
	ldsName := serviceName
	// RDS is new style, with the non default authority.
	rdsName := fmt.Sprintf("xdstp://%s/envoy.config.route.v3.RouteConfiguration/%s", nonDefaultAuth, "route-"+serviceName)
	// CDS is old style name.
	cdsName := "cluster-" + serviceName
	// EDS is new style, with the non default authority.
	edsName := fmt.Sprintf("xdstp://%s/envoy.config.route.v3.ClusterLoadAssignment/%s", nonDefaultAuth, "endpoints-"+serviceName)

	// Split resources, put LDS/CDS in the default authority, and put RDS/EDS in
	// the other authority.
	resourcesDefault := e2e.UpdateOptions{
		NodeID: nodeID,
		// This has only LDS and CDS.
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(ldsName, rdsName)},
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(cdsName, edsName, e2e.SecurityLevelNone)},
		SkipValidation: true,
	}
	resourcesAnother := e2e.UpdateOptions{
		NodeID: nodeID,
		// This has only RDS and EDS.
		Routes:         []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(rdsName, ldsName, cdsName)},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(edsName, "localhost", []uint32{port})},
		SkipValidation: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// This has only LDS and CDS.
	if err := serverDefaultAuth.Update(ctx, resourcesDefault); err != nil {
		t.Fatal(err)
	}
	// This has only RDS and EDS.
	if err := serverAnotherAuth.Update(ctx, resourcesAnother); err != nil {
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

func (s) TestFederation_UnknownAuthorityInDialTarget(t *testing.T) {
	oldXDSFederation := envconfig.XDSFederation
	envconfig.XDSFederation = true
	defer func() { envconfig.XDSFederation = oldXDSFederation }()

	managementServer, nodeID, _, resolver, cleanup1 := e2e.SetupManagementServer(t)
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

	client := testpb.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	_, err = grpc.Dial(fmt.Sprintf("xds://unknown-authority/%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err == nil || !strings.Contains(err.Error(), `authority "unknown-authority" is not found in the bootstrap file`) {
		t.Fatal("grpc.Dial() for target with unknown authority succeeded")
	}
}

func (s) TestFederation_UnknownAuthorityInReceivedResponse(t *testing.T) {
	oldXDSFederation := envconfig.XDSFederation
	envconfig.XDSFederation = true
	defer func() { envconfig.XDSFederation = oldXDSFederation }()

	// Start a management server as the default authority.
	mgmtServer, err := e2e.StartManagementServer()
	if err != nil {
		t.Fatalf("Failed to spin up the xDS management server: %v", err)
	}
	t.Cleanup(mgmtServer.Stop)

	// Create a bootstrap file in a temporary directory.
	nodeID := uuid.New().String()
	bootstrapContents, err := xdsinternal.BootstrapContents(xdsinternal.BootstrapOptions{
		Version:                            xdsinternal.TransportV3,
		NodeID:                             nodeID,
		ServerURI:                          mgmtServer.Address,
		ServerListenerResourceNameTemplate: e2e.ServerListenerResourceNameTemplate,
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap file: %v", err)
	}

	resolverBuilder := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))
	resolver, err := resolverBuilder(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	// LDS is old style name.
	// RDS is new style, with an unknown authority.
	const serviceName = "my-service-client-side-xds"
	const unknownAuthority = "unknown-authority"
	ldsName := serviceName
	rdsName := fmt.Sprintf("xdstp:///envoy.config.route.v3.RouteConfiguration/%s/%s", unknownAuthority, "route-"+serviceName)

	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultClientListener(ldsName, rdsName)},
		Routes:         []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(rdsName, ldsName, "cluster-"+serviceName)},
		SkipValidation: true, // This update has only LDS and RDS resources.
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

	// Since the RDS resource name returned by the management server contains an
	// unknown authority, the xDS resolver will not push a successful update on
	// the channel. The first RPC on the channel always waits for a good update
	// from the resolver before being able to proceed. So, we expect this RPC to
	// exceed its deadline, and as we don't want this test to take
	// `defaultTestTimeout` amount of time to succeed, we pass it a shorter
	// deadline.
	sCtx, sCancel := context.WithTimeout(ctx, 1*time.Second)
	defer sCancel()
	client := testpb.NewTestServiceClient(cc)
	_, err = client.EmptyCall(sCtx, &testpb.Empty{})
	if got := status.Code(err); got != codes.DeadlineExceeded {
		t.Fatalf("rpc EmptyCall() returned code: %v, want %v", got, codes.DeadlineExceeded)
	}
}
