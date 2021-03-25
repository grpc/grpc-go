// +build !386

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
	"net"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/xds"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/e2e"

	testpb "google.golang.org/grpc/test/grpc_testing"
)

// clientSetup performs a bunch of steps common to all xDS client tests here:
// - spin up an xDS management server on a local port
// - spin up a gRPC server and register the test service on it
// - create a local TCP listener and start serving on it
//
// Returns the following:
// - the management server: tests use this to configure resources
// - nodeID expected by the management server: this is set in the Node proto
//   sent by the xdsClient for queries.
// - the port the server is listening on
// - cleanup function to be invoked by the tests when done
func clientSetup(t *testing.T) (*e2e.ManagementServer, string, uint32, func()) {
	// Spin up a xDS management server on a local port.
	nodeID := uuid.New().String()
	fs, err := e2e.StartManagementServer()
	if err != nil {
		t.Fatal(err)
	}

	// Create a bootstrap file in a temporary directory.
	bootstrapCleanup, err := xds.SetupBootstrapFile(xds.BootstrapOptions{
		Version:   xds.TransportV3,
		NodeID:    nodeID,
		ServerURI: fs.Address,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Initialize a gRPC server and register the stubServer on it.
	server := grpc.NewServer()
	testpb.RegisterTestServiceServer(server, &testService{})

	// Create a local listener and pass it to Serve().
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}

	go func() {
		if err := server.Serve(lis); err != nil {
			t.Errorf("Serve() failed: %v", err)
		}
	}()

	return fs, nodeID, uint32(lis.Addr().(*net.TCPAddr).Port), func() {
		fs.Stop()
		bootstrapCleanup()
		server.Stop()
	}
}

func (s) TestClientSideXDS(t *testing.T) {
	fs, nodeID, port, cleanup := clientSetup(t)
	defer cleanup()

	resources := e2e.DefaultClientResources("myservice", nodeID, "localhost", port)
	if err := fs.Update(resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.Dial("xds:///myservice", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}
