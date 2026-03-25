/*
 *
 * Copyright 2026 gRPC authors.
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

package xds

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"google.golang.org/grpc"
	_ "google.golang.org/grpc/balancer/randomsubsetting"
	_ "google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/stubserver"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"

	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
)

// TestRandomSubsetting tests the random_subsetting load balancing policy.
// It starts 4 backends, configures random_subsetting with subsetSize=2,
// and verifies that RPCs are distributed to at most 2 unique backends.
func (s) TestRandomSubsetting(t *testing.T) {
	const numBackends = 4
	const subsetSize = 2

	backendsMu := sync.Mutex{}
	backendsHit := make(map[string]bool)

	var backends []*stubserver.StubServer
	var addrs []resolver.Address

	// Start backends.
	for i := 0; i < numBackends; i++ {
		// Create a local variable to capture in closure.
		backend := &stubserver.StubServer{}
		backend.UnaryCallF = func(ctx context.Context, _ *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			backendsMu.Lock()
			backendsHit[backend.Address] = true
			backendsMu.Unlock()
			return &testpb.SimpleResponse{}, nil
		}
		if err := backend.StartServer(); err != nil {
			t.Fatalf("Failed to start backend %d: %v", i, err)
		}
		defer backend.Stop()
		backends = append(backends, backend)
		addrs = append(addrs, resolver.Address{Addr: backend.Address})
		t.Logf("Started backend %d at %q", i, backend.Address)
	}

	// Configure random_subsetting.
	lbCfgJSON := fmt.Sprintf(`{
  		"loadBalancingConfig": [
    		{
      			"random_subsetting_experimental": {
					"subsetSize": %d,
					"childPolicy": [
						{
							"round_robin": {}
						}
					]
      			}
    		}
  		]
	}`, subsetSize)

	sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(lbCfgJSON)
	if sc.Err != nil {
		t.Fatalf("ParseServiceConfig failed: %v", sc.Err)
	}

	mr := manual.NewBuilderWithScheme("subsetting-e2e")
	defer mr.Close()
	mr.InitialState(resolver.State{
		Addresses:     addrs,
		ServiceConfig: sc,
	})

	cc, err := grpc.NewClient(mr.Scheme()+":///", grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testServiceClient := testgrpc.NewTestServiceClient(cc)

	// Make 20 Unary RPCs.
	for i := 0; i < 20; i++ {
		if _, err := testServiceClient.UnaryCall(ctx, &testpb.SimpleRequest{}); err != nil {
			t.Fatalf("UnaryCall() failed: %v", err)
		}
	}

	backendsMu.Lock()
	hitCount := len(backendsHit)
	backendsMu.Unlock()

	if hitCount > subsetSize {
		t.Fatalf("Expected at most %d backends to be hit, but got %d. Backends hit: %v", subsetSize, hitCount, backendsHit)
	}
	if hitCount == 0 {
		t.Fatalf("Expected backends to be hit, but got none")
	}

	t.Logf("Success: Hit %d unique backends (out of %d configured with subsetSize=%d)", hitCount, numBackends, subsetSize)
}
