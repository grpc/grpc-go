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

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/bootstrap"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/status"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
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
	// Start a management server as the default authority.
	serverDefaultAuth, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		t.Fatalf("Failed to spin up the xDS management server: %v", err)
	}
	t.Cleanup(serverDefaultAuth.Stop)

	// Start another management server as the other authority.
	const nonDefaultAuth = "non-default-auth"
	serverAnotherAuth, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		t.Fatalf("Failed to spin up the xDS management server: %v", err)
	}
	t.Cleanup(serverAnotherAuth.Stop)

	// Create a bootstrap file in a temporary directory.
	nodeID := uuid.New().String()
	bootstrapContents, err := bootstrap.Contents(bootstrap.Options{
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
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

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
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(edsName, "localhost", []uint32{testutils.ParsePort(t, server.Address)})},
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

// TestFederation_UnknownAuthorityInDialTarget tests the case where a ClientConn
// is created with a dial target containing an authority which is not specified
// in the bootstrap configuration. The test verifies that RPCs on the ClientConn
// fail with an appropriate error.
func (s) TestFederation_UnknownAuthorityInDialTarget(t *testing.T) {
	// Setting up the management server is not *really* required for this test
	// case. All we need is a bootstrap configuration which does not contain the
	// authority mentioned in the dial target. But setting up the management
	// server and actually making an RPC ensures that the xDS client is
	// configured properly, and when we dial with an unknown authority in the
	// next step, we can be sure that the error we receive is legitimate.
	managementServer, nodeID, _, resolver, cleanup1 := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{})
	defer cleanup1()

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
	target := fmt.Sprintf("xds:///%s", serviceName)
	cc, err := grpc.Dial(target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("Dialing target %q: %v", target, err)
	}
	defer cc.Close()
	t.Log("Created ClientConn to test service")

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() RPC: %v", err)
	}
	t.Log("Successfully performed an EmptyCall RPC")

	target = fmt.Sprintf("xds://unknown-authority/%s", serviceName)
	t.Logf("Dialing target %q with unknown authority which is expected to fail", target)
	wantErr := fmt.Sprintf("authority \"unknown-authority\" specified in dial target %q is not found in the bootstrap file", target)
	_, err = grpc.Dial(target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("grpc.Dial(%q) returned %v, want: %s", target, err, wantErr)
	}
}

// TestFederation_UnknownAuthorityInReceivedResponse tests the case where the
// LDS resource associated with the dial target contains an RDS resource name
// with an authority which is not specified in the bootstrap configuration. The
// test verifies that RPCs fail with an appropriate error.
func (s) TestFederation_UnknownAuthorityInReceivedResponse(t *testing.T) {
	mgmtServer, err := e2e.StartManagementServer(e2e.ManagementServerOptions{})
	if err != nil {
		t.Fatalf("Failed to spin up the xDS management server: %v", err)
	}
	defer mgmtServer.Stop()

	nodeID := uuid.New().String()
	bootstrapContents, err := bootstrap.Contents(bootstrap.Options{
		NodeID:                             nodeID,
		ServerURI:                          mgmtServer.Address,
		ServerListenerResourceNameTemplate: e2e.ServerListenerResourceNameTemplate,
	})
	if err != nil {
		t.Fatal(err)
	}

	resolverBuilder := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))
	resolver, err := resolverBuilder(bootstrapContents)
	if err != nil {
		t.Fatalf("Creating xDS resolver for testing: %v", err)
	}

	// LDS is old style name.
	// RDS is new style, with an unknown authority.
	const serviceName = "my-service-client-side-xds"
	const unknownAuthority = "unknown-authority"
	ldsName := serviceName
	rdsName := fmt.Sprintf("xdstp://%s/envoy.config.route.v3.RouteConfiguration/%s", unknownAuthority, "route-"+serviceName)

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

	target := fmt.Sprintf("xds:///%s", serviceName)
	cc, err := grpc.Dial(target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("Dialing target %q: %v", target, err)
	}
	defer cc.Close()
	t.Log("Created ClientConn to test service")

	client := testgrpc.NewTestServiceClient(cc)
	_, err = client.EmptyCall(ctx, &testpb.Empty{})
	if err == nil {
		t.Fatal("EmptyCall RPC succeeded for target with unknown authority when expected to fail")
	}
	if got, want := status.Code(err), codes.Unavailable; got != want {
		t.Fatalf("EmptyCall RPC returned status code: %v, want %v", got, want)
	}
	if wantErr := `failed to find authority "unknown-authority"`; !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("EmptyCall RPC returned error: %v, want %v", err, wantErr)
	}
}
