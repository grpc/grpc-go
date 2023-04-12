/*
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
 */

package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	_ "google.golang.org/grpc/xds/internal/balancer/cdsbalancer" // Register the "cds_experimental" LB policy.
)

// TestErrorFromParentLB_ConnectionError tests the case where the parent of the
// clusterresolver LB policy sends its a connection error. The parent policy,
// CDS LB policy, sends a connection error when the ADS stream to the management
// server breaks. The test verifies that there is no perceivable effect because
// of this connection error, and that RPCs continue to work (because the LB
// policies are expected to use previously received xDS resources).
func (s) TestErrorFromParentLB_ConnectionError(t *testing.T) {
	// Create a listener to be used by the management server. The test will
	// close this listener to simulate ADS stream breakage.
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}

	// Start an xDS management server with the above restartable listener, and
	// push a channel when the stream is closed.
	streamClosedCh := make(chan struct{}, 1)
	managementServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{
		Listener: lis,
		OnStreamClosed: func(int64, *v3corepb.Node) {
			select {
			case streamClosedCh <- struct{}{}:
			default:
			}
		},
	})
	defer cleanup()

	// Start a test backend and extract its host and port.
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	// Configure cluster and endpoints resources in the management server.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, edsServiceName, e2e.SecurityLevelNone)},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(edsServiceName, "localhost", []uint32{testutils.ParsePort(t, server.Address)})},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create an xDS xdsClient for use by the cluster_resolver LB policy.
	xdsClient, close, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer close()

	// Create a manual resolver and push a service config specifying the use of
	// the cds LB policy as the top-level LB policy, and a corresponding config
	// with a single cluster.
	r := manual.NewBuilderWithScheme("whatever")
	jsonSC := fmt.Sprintf(`{
			"loadBalancingConfig":[{
				"cds_experimental":{
					"cluster": "%s"
				}
			}]
		}`, clusterName)
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	r.InitialState(xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, xdsClient))

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.Dial(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// Close the listener and ensure that the ADS stream breaks.
	lis.Close()
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for ADS stream to close")
	default:
	}

	// Ensure that RPCs continue to succeed for the next one second.
	for end := time.Now().Add(time.Second); time.Now().Before(end); <-time.After(defaultTestShortTimeout) {
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			t.Fatalf("rpc EmptyCall() failed: %v", err)
		}
	}
}

// TestErrorFromParentLB_ResourceNotFound tests the case where the parent of the
// clusterresolver LB policy sends it a resource-not-found error. The parent
// policy, CDS LB policy, sends a resource-not-found error when the cluster
// resource associated with these LB policies is removed by the management
// server. The test verifies that the associated EDS is canceled and RPCs fail.
// It also ensures that when the Cluster resource is added back, the EDS
// resource is re-requested and RPCs being to succeed.
func (s) TestErrorFromParentLB_ResourceNotFound(t *testing.T) {
	// Start an xDS management server that uses a couple of channels to
	// notify the test about the following events:
	// - an EDS requested with the expected resource name is requested
	// - EDS resource is unrequested, i.e, an EDS request with no resource name
	//   is received, which indicates that we are not longer interested in that
	//   resource.
	edsResourceRequestedCh := make(chan struct{}, 1)
	edsResourceCanceledCh := make(chan struct{}, 1)
	managementServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() == version.V3EndpointsURL {
				switch len(req.GetResourceNames()) {
				case 0:
					select {
					case edsResourceCanceledCh <- struct{}{}:
					default:
					}
				case 1:
					if req.GetResourceNames()[0] == edsServiceName {
						select {
						case edsResourceRequestedCh <- struct{}{}:
						default:
						}
					}
				default:
					t.Errorf("Unexpected number of resources, %d, in an EDS request", len(req.GetResourceNames()))
				}
			}
			return nil
		},
	})
	defer cleanup()

	// Start a test backend and extract its host and port.
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	// Configure cluster and endpoints resources in the management server.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, edsServiceName, e2e.SecurityLevelNone)},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(edsServiceName, "localhost", []uint32{testutils.ParsePort(t, server.Address)})},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create an xDS xdsClient for use by the cluster_resolver LB policy.
	xdsClient, close, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer close()

	// Create a manual resolver and push a service config specifying the use of
	// the cds LB policy as the top-level LB policy, and a corresponding config
	// with a single cluster.
	r := manual.NewBuilderWithScheme("whatever")
	jsonSC := fmt.Sprintf(`{
			"loadBalancingConfig":[{
				"cds_experimental":{
					"cluster": "%s"
				}
			}]
		}`, clusterName)
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	r.InitialState(xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, xdsClient))

	// Create a ClientConn that kick starts the xDS workflow.
	cc, err := grpc.Dial(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	// Wait for the EDS resource to be requested.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for EDS resource to be requested")
	case <-edsResourceRequestedCh:
	}

	// Ensure that a successful RPC can be made.
	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// Delete the cluster resource from the mangement server.
	resources.Clusters = nil
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for the EDS resource to be not requested anymore.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for EDS resource to not requested")
	case <-edsResourceCanceledCh:
	}

	// Ensure that RPCs start to fail with expected error.
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
		defer sCancel()
		_, err := client.EmptyCall(sCtx, &testpb.Empty{})
		if status.Code(err) == codes.Unavailable && strings.Contains(err.Error(), "all priorities are removed") {
			break
		}
		if err != nil {
			t.Logf("EmptyCall RPC failed: %v", err)
		}
	}
	if ctx.Err() != nil {
		t.Fatalf("RPCs did not fail after removal of Cluster resource")
	}

	// Ensure that the ClientConn is in TransientFailure.
	if state := cc.GetState(); state != connectivity.TransientFailure {
		t.Fatalf("Unexpected connectivity state for ClientConn, got: %s, want %s", state, connectivity.TransientFailure)
	}

	// Configure cluster and endpoints resources in the management server.
	resources = e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, edsServiceName, e2e.SecurityLevelNone)},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(edsServiceName, "localhost", []uint32{testutils.ParsePort(t, server.Address)})},
		SkipValidation: true,
	}
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Wait for the EDS resource to be requested again.
	select {
	case <-ctx.Done():
		t.Fatal("Timeout when waiting for EDS resource to be requested")
	case <-edsResourceRequestedCh:
	}

	// Ensure that a successful RPC can be made.
	for ; ctx.Err() == nil; <-time.After(defaultTestShortTimeout) {
		sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
		defer sCancel()
		if _, err := client.EmptyCall(sCtx, &testpb.Empty{}); err != nil {
			t.Logf("EmptyCall RPC failed: %v", err)
			continue
		}
		break
	}
	if ctx.Err() != nil {
		t.Fatalf("RPCs did not fail after removal of Cluster resource")
	}
}

// TestEDSResourceRemoved tests the case where the EDS resource requested by the
// clusterresolver LB policy is removed from the management server.  The test
// verifies that the EDS watch is not canceled and that RPCs continue to succeed
// with the previously received configuration.
func (s) TestEDSResourceRemoved(t *testing.T) {
	// Start an xDS management server that uses a couple of channels to
	// notify the test about the following events:
	// - an EDS requested with the expected resource name is requested
	// - EDS resource is unrequested, i.e, an EDS request with no resource name
	//   is received, which indicates that we are not longer interested in that
	//   resource.
	edsResourceRequestedCh := make(chan struct{}, 1)
	edsResourceCanceledCh := make(chan struct{}, 1)
	managementServer, nodeID, bootstrapContents, _, cleanup := e2e.SetupManagementServer(t, e2e.ManagementServerOptions{
		OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
			if req.GetTypeUrl() == version.V3EndpointsURL {
				switch len(req.GetResourceNames()) {
				case 0:
					select {
					case edsResourceCanceledCh <- struct{}{}:
					default:
					}
				case 1:
					if req.GetResourceNames()[0] == edsServiceName {
						select {
						case edsResourceRequestedCh <- struct{}{}:
						default:
						}
					}
				default:
					t.Errorf("Unexpected number of resources, %d, in an EDS request", len(req.GetResourceNames()))
				}
			}
			return nil
		},
	})
	defer cleanup()

	// Start a test backend and extract its host and port.
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	// Configure cluster and endpoints resources in the management server.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Clusters:       []*v3clusterpb.Cluster{e2e.DefaultCluster(clusterName, edsServiceName, e2e.SecurityLevelNone)},
		Endpoints:      []*v3endpointpb.ClusterLoadAssignment{e2e.DefaultEndpoint(edsServiceName, "localhost", []uint32{testutils.ParsePort(t, server.Address)})},
		SkipValidation: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create an xDS xdsClient for use by the cluster_resolver LB policy.
	xdsClient, close, err := xdsclient.NewWithBootstrapContentsForTesting(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer close()

	// Create a manual resolver and push a service config specifying the use of
	// the cds LB policy as the top-level LB policy, and a corresponding config
	// with a single cluster.
	r := manual.NewBuilderWithScheme("whatever")
	jsonSC := fmt.Sprintf(`{
			"loadBalancingConfig":[{
				"cds_experimental":{
					"cluster": "%s"
				}
			}]
		}`, clusterName)
	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	r.InitialState(xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, xdsClient))

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.Dial(r.Scheme()+":///test.service", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(r))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// Delete the endpoints resource from the mangement server.
	resources.Endpoints = nil
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Ensure that RPCs continue to succeed for the next one second, and that the EDS watch is not canceled.
	for end := time.Now().Add(time.Second); time.Now().Before(end); <-time.After(defaultTestShortTimeout) {
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			t.Fatalf("rpc EmptyCall() failed: %v", err)
		}
		select {
		case <-edsResourceCanceledCh:
			t.Fatal("EDS watch canceled when not expected to be canceled")
		default:
		}
	}
}
