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

package grpctransport

import (
	"context"
	"sync"
	"testing"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/local"
	"google.golang.org/grpc/xds/internal/clients"
	"google.golang.org/grpc/xds/internal/clients/internal/testutils"

	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// TestBuild_Single tests that multiple calls to Build() with the same
// clients.ServerIdentifier returns the same transport. Also verifies that
// only when all references to the newly created transport are released,
// the underlying transport is closed.
func (s) TestBuild_Single(t *testing.T) {
	ts := setupTestServer(t, &v3discoverypb.DiscoveryResponse{VersionInfo: "1"})

	serverID := clients.ServerIdentifier{
		ServerURI:  ts.address,
		Extensions: ServerIdentifierExtension{Credentials: "local"},
	}
	credentials := map[string]credentials.Bundle{
		"local": &testCredentials{transportCredentials: local.NewCredentials()},
	}

	// Override the gRPC transport creation hook to get notified.
	origGRPCTransportCreateHook := grpcTransportCreateHook
	grpcTransportCreateCh := testutils.NewChannelWithSize(1)
	grpcTransportCreateHook = func() {
		grpcTransportCreateCh.Replace(struct{}{})
	}
	defer func() { grpcTransportCreateHook = origGRPCTransportCreateHook }()

	// Override the gRPC transport close hook to get notified.
	origGRPCTransportCloseHook := grpcTransportCloseHook
	grpcTransportCloseCh := testutils.NewChannelWithSize(1)
	grpcTransportCloseHook = func() {
		grpcTransportCloseCh.Replace(struct{}{})
	}
	defer func() { grpcTransportCloseHook = origGRPCTransportCloseHook }()

	builder := NewBuilder(credentials)
	tr, err := builder.Build(serverID)
	if err != nil {
		t.Fatalf("Failed to build transport: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := grpcTransportCreateCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for gRPC transport to be created: %v", err)
	}

	// Calling Build() again should not create new gRPC transport.
	const count = 9
	transports := make([]clients.Transport, count)

	for i := 0; i < count; i++ {
		func() {
			transports[i], err = builder.Build(serverID)
			if err != nil {
				t.Fatalf("Failed to build transport: %v", err)
			}

			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if _, err := grpcTransportCreateCh.Receive(sCtx); err == nil {
				t.Fatalf("%d-th call to New() created a new gRPC transportå", i)
			}
		}()
	}

	// Call Close() multiple times on each of the transport received in the
	// above for loop. Close() calls are idempotent. The underlying gRPC
	// transport implementation will not be closed until we release the first
	// reference we acquired above, via the first call to Build().
	for i := 0; i < count; i++ {
		func() {
			transports[i].Close()
			transports[i].Close()

			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if _, err := grpcTransportCloseCh.Receive(sCtx); err == nil {
				t.Fatal("gRPC transport closed before all references are released")
			}
		}()
	}

	// Call the last Close(). The underlying gRPC transport should be closed.
	tr.Close()
	if _, err := grpcTransportCloseCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for gRPC transport to be closed: %v", err)
	}

	// Calling Build() again, after the previous transport was actually closed,
	// should create a new one.
	tr, err = builder.Build(serverID)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer tr.Close()
	if _, err := grpcTransportCreateCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for gRPC transport to be created: %v", err)
	}
}

// TestBuild_Multiple tests the scenario where there are multiple calls to
// Build() with different clients.ServerIdentifier. Verifies that reference
// counts are tracked correctly for each transport and that only when all
// references are released for a transport, it is closed.
func (s) TestBuild_Multiple(t *testing.T) {
	ts := setupTestServer(t, &v3discoverypb.DiscoveryResponse{VersionInfo: "1"})

	serverID1 := clients.ServerIdentifier{
		ServerURI:  ts.address,
		Extensions: ServerIdentifierExtension{Credentials: "local"},
	}
	serverID2 := clients.ServerIdentifier{
		ServerURI:  ts.address,
		Extensions: ServerIdentifierExtension{Credentials: "insecure"},
	}
	credentials := map[string]credentials.Bundle{
		"local":    &testCredentials{transportCredentials: local.NewCredentials()},
		"insecure": insecure.NewBundle(),
	}

	// Override the gRPC transport creation hook to get notified.
	origGRPCTransportCreateHook := grpcTransportCreateHook
	grpcTransportCreateCh := testutils.NewChannelWithSize(1)
	grpcTransportCreateHook = func() {
		grpcTransportCreateCh.Replace(struct{}{})
	}
	defer func() { grpcTransportCreateHook = origGRPCTransportCreateHook }()

	// Override the gRPC transport close hook to get notified.
	origGRPCTransportCloseHook := grpcTransportCloseHook
	grpcTransportCloseCh := testutils.NewChannelWithSize(1)
	grpcTransportCloseHook = func() {
		grpcTransportCloseCh.Replace(struct{}{})
	}
	defer func() { grpcTransportCloseHook = origGRPCTransportCloseHook }()

	// Create two gRPC transports.
	builder := NewBuilder(credentials)
	tr1, err := builder.Build(serverID1)
	if err != nil {
		t.Fatalf("Failed to build transport: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := grpcTransportCreateCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for gRPC transport to be created: %v", err)
	}

	tr2, err := builder.Build(serverID2)
	if err != nil {
		t.Fatalf("Failed to build transport: %v", err)
	}
	if _, err := grpcTransportCreateCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for gRPC transport to be created: %v", err)
	}

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
				t.Errorf("%d-th call to Build() failed with error: %v", i, err)
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
		}
	}()
	wg.Wait()
	if t.Failed() {
		t.FailNow()
	}

	// Ensure that none of the create hooks are called.
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := grpcTransportCreateCh.Receive(sCtx); err == nil {
		t.Fatalf("gRPC transport created when expected to reuse an existing one")
	}

	// The close on transport is idempotent and calling it multiple times
	// should not decrement the reference count multiple times.
	for i := 0; i < count; i++ {
		transports1[i].Close()
		transports1[i].Close()
	}
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := grpcTransportCloseCh.Receive(sCtx); err == nil {
		t.Fatal("gRPC transport closed before all references are released")
	}

	// Call the last Close(). The underlying gRPC transport should be closed.
	tr1.Close()
	if _, err := grpcTransportCloseCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for gRPC transport to be closed: %v", err)
	}

	// Ensure that the close hook is not called for the second transport.
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := grpcTransportCloseCh.Receive(sCtx); err == nil {
		t.Fatal("gRPC transport closed before all references are released")
	}

	// The close on transport is idempotent and calling it multiple times
	// should not decrement the reference count multiple times.
	for i := 0; i < count; i++ {
		transports2[i].Close()
		transports1[i].Close()
	}
	sCtx, sCancel = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	if _, err := grpcTransportCloseCh.Receive(sCtx); err == nil {
		t.Fatal("gRPC transport closed before all references are released")
	}

	// Call the last Close(). The underlying gRPC transport should be closed.
	tr2.Close()
	if _, err := grpcTransportCloseCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for gRPC transport to be closed: %v", err)
	}
}
