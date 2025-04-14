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
	"sync"
	"testing"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/local"
	"google.golang.org/grpc/xds/internal/clients"

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

	// Calling Build() first time should create new gRPC transport.
	builder := NewBuilder(credentials)
	tr, err := builder.Build(serverID)
	if err != nil {
		t.Fatalf("Failed to build transport: %v", err)
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
			if (tr.(*transportRef)).grpcTransport != (transports[i].(*transportRef)).grpcTransport {
				t.Fatalf("Wanted underlying gRPC transport to be reused, but got different transport: %v", (transports[i].(*transportRef)).grpcTransport)
			}
		}()
	}

	// Call Close() multiple times on each of the transport received in the
	// above for loop. Close() calls are idempotent. The underlying gRPC
	// transport is removed after the Close() call but calling close second
	// time should not panic.
	for i := 0; i < count; i++ {
		func() {
			transports[i].Close()
			transports[i].Close()
		}()
	}

	// Call the last Close(). The underlying gRPC transport should be closed
	// because calls in the above for loop have released all references.
	//
	// If not closed, it will be caught by the goroutine leak check.
	tr.Close()

	// Calling Build() again, after the previous transport was actually closed,
	// should create a new one.
	tr2, err := builder.Build(serverID)
	if err != nil {
		t.Fatalf("Failed to create xDS client: %v", err)
	}
	defer tr2.Close()
	if (tr.(*transportRef)).grpcTransport == (tr2.(*transportRef)).grpcTransport {
		t.Fatalf("Wanted new underlying gRPC transport, but got same transport: %v", (tr.(*transportRef)).grpcTransport)
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

	// Create two gRPC transports.
	builder := NewBuilder(credentials)
	tr1, err := builder.Build(serverID1)
	if err != nil {
		t.Fatalf("Failed to build transport: %v", err)
	}
	tr2, err := builder.Build(serverID2)
	if err != nil {
		t.Fatalf("Failed to build transport: %v", err)
	}
	if tr1.(*transportRef).grpcTransport == tr2.(*transportRef).grpcTransport {
		t.Fatalf("Wanted different underlying gRPC transports, but got same transport: %v", tr1.(*transportRef).grpcTransport)
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
			if tr1.(*transportRef).grpcTransport != transports1[i].(*transportRef).grpcTransport {
				t.Errorf("Wanted underlying gRPC transport to be reused, but got different transport: %v", transports1[i].(*transportRef).grpcTransport)
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
			if tr2.(*transportRef).grpcTransport != transports2[i].(*transportRef).grpcTransport {
				t.Errorf("Wanted underlying gRPC transport to be reused, but got different transport: %v", transports2[i].(*transportRef).grpcTransport)
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
	// time should not panic.
	for i := 0; i < count; i++ {
		transports1[i].Close()
		transports1[i].Close()
	}

	// Call the last Close(). The underlying gRPC transport should be closed
	// because calls in the above for loop have released all references.
	//
	// If not closed, it will be caught by the goroutine leak check.
	tr1.Close()

	// Call Close() multiple times on each of the transport received in the
	// above for loop. Close() calls are idempotent. The underlying gRPC
	// transport is removed after the Close() call but calling close second
	// time should not panic.
	for i := 0; i < count; i++ {
		transports2[i].Close()
		transports2[i].Close()
	}

	// Call the last Close(). The underlying gRPC transport should be closed
	// because calls in the above for loop have released all references.
	//
	// If not closed, it will be caught by the goroutine leak check.
	tr2.Close()
}
