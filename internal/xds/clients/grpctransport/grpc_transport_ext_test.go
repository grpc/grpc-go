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

package grpctransport_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/local"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils/e2e"
)

const (
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond // For events expected to *not* happen.
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type testCredentials struct {
	credentials.Bundle
	transportCredentials credentials.TransportCredentials
}

func (tc *testCredentials) TransportCredentials() credentials.TransportCredentials {
	return tc.transportCredentials
}
func (tc *testCredentials) PerRPCCredentials() credentials.PerRPCCredentials {
	return nil
}

// TestBuild_Single tests that multiple calls to Build() with the same
// clients.ServerIdentifier returns the same transport. Also verifies that
// only when all references to the newly created transport are released,
// the underlying transport is closed.
func (s) TestBuild_Single(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	lis := testutils.NewListenerWrapper(t, nil)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: lis})

	serverID := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "local"},
	}
	configs := map[string]grpctransport.Config{
		"local": {Credentials: &testCredentials{transportCredentials: local.NewCredentials()}},
	}

	// Calling Build() first time should create new gRPC transport.
	builder := grpctransport.NewBuilder(configs)
	tr, err := builder.Build(serverID)
	if err != nil {
		t.Fatalf("Failed to build transport: %v", err)
	}
	// Create a new stream to the server and verify that a new transport is
	// created.
	if _, err = tr.NewStream(ctx, "/envoy.service.discovery.v3.AggregatedDiscoveryService/StreamAggregatedResources"); err != nil {
		t.Fatalf("Failed to create stream: %v", err)
	}
	val, err := lis.NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timed out when waiting for a new transport to be created to the management server: %v", err)
	}
	conn := val.(*testutils.ConnWrapper)

	// Calling Build() again should not create new gRPC transport.
	const count = 9
	transports := make([]clients.Transport, count)
	for i := 0; i < count; i++ {
		func() {
			transports[i], err = builder.Build(serverID)
			if err != nil {
				t.Fatalf("Failed to build transport: %v", err)
			}
			// Create a new stream to the server and verify that no connection
			// is established to the management server at this point. A new
			// transport is created only when an existing connection for
			// serverID does not exist.
			if _, err = tr.NewStream(ctx, "/envoy.service.discovery.v3.AggregatedDiscoveryService/StreamAggregatedResources"); err != nil {
				t.Fatalf("Failed to create stream: %v", err)
			}
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if _, err := lis.NewConnCh.Receive(sCtx); err != context.DeadlineExceeded {
				t.Fatal("Unexpected new transport created to management server")
			}
		}()
	}

	// Call Close() multiple times on each of the transport received in the
	// above for loop. Close() calls are idempotent. The underlying gRPC
	// transport is removed after the Close() call but calling close second
	// time should not panic and underlying gRPC transport should not be
	// closed.
	for i := 0; i < count; i++ {
		func() {
			transports[i].Close()
			transports[i].Close()
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if _, err := conn.CloseCh.Receive(sCtx); err != context.DeadlineExceeded {
				t.Fatal("Unexpected transport closure to management server")
			}
		}()
	}

	// Call the last Close(). The underlying gRPC transport should be closed
	// because calls in the above for loop have released all references.
	tr.Close()
	if _, err := conn.CloseCh.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for connection to management server to be closed")
	}

	// Calling Build() again, after the previous transport was actually closed,
	// should create a new one.
	tr2, err := builder.Build(serverID)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer tr2.Close()
	// Create a new stream to the server and verify that a new transport is
	// created.
	if _, err = tr2.NewStream(ctx, "/envoy.service.discovery.v3.AggregatedDiscoveryService/StreamAggregatedResources"); err != nil {
		t.Fatalf("Failed to create stream: %v", err)
	}
	if _, err := lis.NewConnCh.Receive(ctx); err != nil {
		t.Fatalf("Timed out when waiting for a new transport to be created to the management server: %v", err)
	}
}

// TestBuild_Multiple tests the scenario where there are multiple calls to
// Build() with different clients.ServerIdentifier. Verifies that reference
// counts are tracked correctly for each transport and that only when all
// references are released for a transport, it is closed.
func (s) TestBuild_Multiple(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	lis := testutils.NewListenerWrapper(t, nil)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{Listener: lis})

	serverID1 := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "local"},
	}
	serverID2 := clients.ServerIdentifier{
		ServerURI:  mgmtServer.Address,
		Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"},
	}
	configs := map[string]grpctransport.Config{
		"local":    {Credentials: &testCredentials{transportCredentials: local.NewCredentials()}},
		"insecure": {Credentials: insecure.NewBundle()},
	}

	// Create two gRPC transports.
	builder := grpctransport.NewBuilder(configs)

	tr1, err := builder.Build(serverID1)
	if err != nil {
		t.Fatalf("Failed to build transport: %v", err)
	}
	// Create a new stream to the server and verify that a new transport is
	// created.
	if _, err = tr1.NewStream(ctx, "/envoy.service.discovery.v3.AggregatedDiscoveryService/StreamAggregatedResources"); err != nil {
		t.Fatalf("Failed to create stream: %v", err)
	}
	val, err := lis.NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timed out when waiting for a new transport to be created to the management server: %v", err)
	}
	conn1 := val.(*testutils.ConnWrapper)

	tr2, err := builder.Build(serverID2)
	if err != nil {
		t.Fatalf("Failed to build transport: %v", err)
	}
	// Create a new stream to the server and verify that a new transport is
	// created because credentials are different.
	if _, err = tr2.NewStream(ctx, "/envoy.service.discovery.v3.AggregatedDiscoveryService/StreamAggregatedResources"); err != nil {
		t.Fatalf("Failed to create stream: %v", err)
	}
	val, err = lis.NewConnCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timed out when waiting for a new transport to be created to the management server: %v", err)
	}
	conn2 := val.(*testutils.ConnWrapper)

	// Create N more references to each of the two transports.
	const count = 9
	transports1 := make([]clients.Transport, count)
	transports2 := make([]clients.Transport, count)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < count; i++ {
			var err error
			transports1[i], err = builder.Build(serverID1)
			if err != nil {
				t.Errorf("Failed to build transport: %v", err)
			}
			// Create a new stream to the server and verify that no connection
			// is established to the management server at this point. A new
			// transport is created only when an existing connection for
			// serverID does not exist.
			if _, err = transports1[i].NewStream(ctx, "/envoy.service.discovery.v3.AggregatedDiscoveryService/StreamAggregatedResources"); err != nil {
				t.Errorf("Failed to create stream: %v", err)
			}
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if _, err := lis.NewConnCh.Receive(sCtx); err != context.DeadlineExceeded {
				t.Error("Unexpected new transport created to management server")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < count; i++ {
			var err error
			transports2[i], err = builder.Build(serverID2)
			if err != nil {
				t.Errorf("%d-th call to Build() failed with error: %v", i, err)
			}
			// Create a new stream to the server and verify that no connection
			// is established to the management server at this point. A new
			// transport is created only when an existing connection for
			// serverID does not exist.
			if _, err = transports2[i].NewStream(ctx, "/envoy.service.discovery.v3.AggregatedDiscoveryService/StreamAggregatedResources"); err != nil {
				t.Errorf("Failed to create stream: %v", err)
			}
			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if _, err := lis.NewConnCh.Receive(sCtx); err != context.DeadlineExceeded {
				t.Error("Unexpected new transport created to management server")
			}
		}
	}()
	wg.Wait()
	if t.Failed() {
		t.FailNow()
	}

	// Call Close() multiple times on each of the transport received in the
	// above for loop. Close() calls are idempotent. The underlying gRPC
	// transport is removed after the Close() call but calling close second
	// time should not panic and underlying gRPC transport should not be
	// closed.
	for i := 0; i < count; i++ {
		transports1[i].Close()
		sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
		defer sCancel()
		if _, err := conn1.CloseCh.Receive(sCtx); err != context.DeadlineExceeded {
			t.Fatal("Unexpected transport closure to management server")
		}
		transports1[i].Close()
	}
	// Call the last Close(). The underlying gRPC transport should be closed
	// because calls in the above for loop have released all references.
	tr1.Close()
	if _, err := conn1.CloseCh.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for connection to management server to be closed")
	}

	// Call Close() multiple times on each of the transport received in the
	// above for loop. Close() calls are idempotent. The underlying gRPC
	// transport is removed after the Close() call but calling close second
	// time should not panic and underlying gRPC transport should not be
	// closed.
	for i := 0; i < count; i++ {
		transports2[i].Close()
		sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
		defer sCancel()
		if _, err := conn2.CloseCh.Receive(sCtx); err != context.DeadlineExceeded {
			t.Fatal("Unexpected transport closure to management server")
		}
		transports2[i].Close()
	}
	// Call the last Close(). The underlying gRPC transport should be closed
	// because calls in the above for loop have released all references.
	tr2.Close()
	if _, err := conn2.CloseCh.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for connection to management server to be closed")
	}
}
