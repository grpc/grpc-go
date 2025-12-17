/*
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
 */

package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"

	testpb "google.golang.org/grpc/interop/grpc_testing"

	_ "google.golang.org/grpc/internal/xds/balancer/clusterresolver"
)

// TestLogicalDNS_MultipleEndpoints tests the cluster_resolver LB policy
// using a LOGICAL_DNS discovery mechanism.
//
// The test verifies that multiple addresses returned by the DNS resolver are
// grouped into a single endpoint (as per gRFC A61). Because the round_robin
// LB policy (configured via xdsLbPolicy) sees only one endpoint, it should
// not rotate traffic between the addresses. Instead, the single endpoint
// (which contains all addresses) is picked, and connects to the first address.
func (s) TestLogicalDNS_MultipleEndpoints(t *testing.T) {
	// Spin up a management server to receive xDS resources from.
	managementServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bootstrapContents := e2e.DefaultBootstrapContents(t, nodeID, managementServer.Address)

	// Start backend servers which provide an implementation of the TestService.
	server1 := stubserver.StartTestService(t, nil)
	defer server1.Stop()
	server2 := stubserver.StartTestService(t, nil)
	defer server2.Stop()

	// Override the DNS resolver with a manual resolver that returns the
	// addresses of the above server backends.
	const dnsScheme = "dns"
	dnsR := manual.NewBuilderWithScheme(dnsScheme)
	originalDNS := resolver.Get("dns")
	resolver.Register(dnsR)
	t.Cleanup(func() { resolver.Register(originalDNS) })

	// Capture the ClientConn created by the cluster_resolver so we can push updates.
	// We use a channel to synchronize access and avoid race conditions.
	dnsCCCh := make(chan resolver.ClientConn, 1)
	dnsR.BuildCallback = func(_ resolver.Target, cc resolver.ClientConn, _ resolver.BuildOptions) {
		select {
		case dnsCCCh <- cc:
		default:
		}
	}

	// Create an xDS client for use by the cluster_resolver LB policy.
	config, err := bootstrap.NewConfigFromContents(bootstrapContents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap: %v", err)
	}
	pool := xdsclient.NewPool(config)
	xdsC, closeClient, err := pool.NewClientForTesting(xdsclient.OptionsForTesting{
		Name: t.Name(),
	})
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer closeClient()

	// Create a manual resolver and push service config specifying the use of
	// the cluster_resolver LB policy with LOGICAL_DNS discovery mechanism.
	r := manual.NewBuilderWithScheme("whatever")
	jsonSC := fmt.Sprintf(`{
            "loadBalancingConfig":[{
                "cluster_resolver_experimental":{
                    "discoveryMechanisms": [{
                        "cluster": "test-cluster",
                        "type": "LOGICAL_DNS",
                        "dnsHostname": "%s:///target-name",
                        "outlierDetection": {}
                    }],
                    "xdsLbPolicy":[{"round_robin":{}}]
                }
            }]
        }`, dnsScheme)

	scpr := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
	r.InitialState(xdsclient.SetClient(resolver.State{ServiceConfig: scpr}, xdsC))

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.NewClient(r.Scheme()+":///test.service",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
	)
	cc.Connect()
	if err != nil {
		t.Fatalf("failed to create new client for local test server: %v", err)
	}
	defer cc.Close()

	testClient := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var dnsClientConn resolver.ClientConn
	select {
	case dnsClientConn = <-dnsCCCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for cluster_resolver to build the DNS resolver")
	}

	// For LOGICAL_DNS, this updates the SINGLE endpoint to have 2 IPs.
	dnsClientConn.UpdateState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: server1.Address},
			{Addr: server2.Address},
		},
	})

	// Ensure the RPC is routed to the first backend.
	var peer peer.Peer
	if _, err := testClient.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer)); err != nil {
		t.Fatalf("RPC failed: %v", err)
	}

	if got, want := peer.Addr.String(), server1.Address; got != want {
		t.Errorf("peer.Addr = %q, want = %q", got, want)
	}
}
