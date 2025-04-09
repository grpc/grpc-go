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
	"io"
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/local"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/xds/internal/clients"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	v3discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
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

// testServer implements the AggregatedDiscoveryServiceServer interface to test
// the gRPC transport implementation.
type testServer struct {
	v3discoverygrpc.UnimplementedAggregatedDiscoveryServiceServer

	address     string                               // address of the server
	requestChan chan *v3discoverypb.DiscoveryRequest // channel to send the received requests on for verification
	response    *v3discoverypb.DiscoveryResponse     // response to send back to the client from handler
}

// setupTestServer set up the gRPC server for AggregatedDiscoveryService. It
// creates an instance of testServer that returns the provided response from
// the StreamAggregatedResources() handler and registers it with a gRPC server.
func setupTestServer(t *testing.T, response *v3discoverypb.DiscoveryResponse) *testServer {
	t.Helper()

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen on localhost:0: %v", err)
	}
	ts := &testServer{
		requestChan: make(chan *v3discoverypb.DiscoveryRequest),
		address:     lis.Addr().String(),
		response:    response,
	}

	s := grpc.NewServer()

	v3discoverygrpc.RegisterAggregatedDiscoveryServiceServer(s, ts)
	go s.Serve(lis)
	t.Cleanup(s.Stop)

	return ts
}

// StreamAggregatedResources handles bidirectional streaming of
// DiscoveryRequest and DiscoveryResponse. It waits for a message from the
// client on the stream, and then sends a discovery response message back to
// the client. It also put the received message in requestChan for client to
// verify if the correct request was received. It continues until the client
// closes the stream.
func (s *testServer) StreamAggregatedResources(stream v3discoverygrpc.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	ctx := stream.Context()

	for {
		// Receive a DiscoveryRequest from the client
		req, err := stream.Recv()
		if err == io.EOF {
			return nil // Stream closed by client
		}
		if err != nil {
			return err // Handle other errors
		}

		// Push received request for client to verify the correct request was
		// received.
		select {
		case s.requestChan <- req:
		case <-ctx.Done():
			return ctx.Err()
		}

		// Send the response back to the client
		if err := stream.Send(s.response); err != nil {
			return err
		}
	}
}

type testCredentials struct {
	credentials.Bundle
	transportCredentials credentials.TransportCredentials
	perRPCCredentials    credentials.PerRPCCredentials
}

func (tc *testCredentials) TransportCredentials() credentials.TransportCredentials {
	return tc.transportCredentials
}
func (tc *testCredentials) PerRPCCredentials() credentials.PerRPCCredentials {
	return tc.perRPCCredentials
}

// TestBuild_Success verifies that the Builder successfully creates a new
// Transport in both cases when provided clients.ServerIdentifer is same
// one of the existing transport or a new one.
func (s) TestBuild_Success(t *testing.T) {
	credentials := map[string]credentials.Bundle{
		"local":    &testCredentials{transportCredentials: local.NewCredentials()},
		"insecure": insecure.NewBundle(),
	}
	b := NewBuilder(credentials)

	serverID1 := clients.ServerIdentifier{
		ServerURI:  "server-address",
		Extensions: ServerIdentifierExtension{Credentials: "local"},
	}
	tr1, err := b.Build(serverID1)
	if err != nil {
		t.Fatalf("Build(serverID1) call failed: %v", err)
	}
	defer tr1.Close()

	serverID2 := clients.ServerIdentifier{
		ServerURI:  "server-address",
		Extensions: ServerIdentifierExtension{Credentials: "local"},
	}
	tr2, err := b.Build(serverID2)
	if err != nil {
		t.Fatalf("Build(serverID2) call failed: %v", err)
	}
	defer tr2.Close()

	serverID3 := clients.ServerIdentifier{
		ServerURI:  "server-address",
		Extensions: ServerIdentifierExtension{Credentials: "insecure"},
	}
	tr3, err := b.Build(serverID3)
	if err != nil {
		t.Fatalf("Build(serverID3) call failed: %v", err)
	}
	defer tr3.Close()
}

// TestBuild_Failure verifies that the Builder returns error when incorrect
// ServerIdentifier is provided.
//
// It covers the following scenarios:
// - ServerURI is empty.
// - Extensions is nil.
// - Extensions is not ServerIdentifierExtension.
// - Credentials are nil.
func (s) TestBuild_Failure(t *testing.T) {
	tests := []struct {
		name     string
		serverID clients.ServerIdentifier
	}{
		{
			name: "ServerURI is empty",
			serverID: clients.ServerIdentifier{
				ServerURI:  "",
				Extensions: ServerIdentifierExtension{Credentials: "local"},
			},
		},
		{
			name:     "Extensions is nil",
			serverID: clients.ServerIdentifier{ServerURI: "server-address"},
		},
		{
			name: "Extensions is not a ServerIdentifierExtension",
			serverID: clients.ServerIdentifier{
				ServerURI:  "server-address",
				Extensions: 1,
			},
		},
		{
			name: "ServerIdentifierExtension Credentials is nil",
			serverID: clients.ServerIdentifier{
				ServerURI:  "server-address",
				Extensions: ServerIdentifierExtension{},
			},
		},
		{
			name: "ServerIdentifierExtension is added as pointer",
			serverID: clients.ServerIdentifier{
				ServerURI:  "server-address",
				Extensions: &ServerIdentifierExtension{Credentials: "local"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			credentials := map[string]credentials.Bundle{
				"local": &testCredentials{transportCredentials: local.NewCredentials()},
			}
			b := NewBuilder(credentials)
			tr, err := b.Build(test.serverID)
			if err == nil {
				t.Fatalf("Build() succeeded, want error")
			}
			if tr != nil {
				t.Fatalf("Got non-nil transport from Build(), want nil")
			}
		})
	}
}

// TestNewStream_Success verifies that NewStream() successfully creates a new
// client stream for the server when provided a valid server URI.
func (s) TestNewStream_Success(t *testing.T) {
	ts := setupTestServer(t, &v3discoverypb.DiscoveryResponse{VersionInfo: "1"})

	serverCfg := clients.ServerIdentifier{
		ServerURI:  ts.address,
		Extensions: ServerIdentifierExtension{Credentials: "local"},
	}
	credentials := map[string]credentials.Bundle{
		"local": &testCredentials{transportCredentials: local.NewCredentials()},
	}
	builder := NewBuilder(credentials)
	transport, err := builder.Build(serverCfg)
	if err != nil {
		t.Fatalf("Failed to build transport: %v", err)
	}
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err = transport.NewStream(ctx, "/envoy.service.discovery.v3.AggregatedDiscoveryService/StreamAggregatedResources"); err != nil {
		t.Fatalf("transport.NewStream() failed: %v", err)
	}
}

// TestNewStream_Error verifies that NewStream() returns an error
// when attempting to create a stream with an invalid server URI.
func (s) TestNewStream_Error(t *testing.T) {
	serverCfg := clients.ServerIdentifier{
		ServerURI:  "invalid-server-uri",
		Extensions: ServerIdentifierExtension{Credentials: "local"},
	}
	credentials := map[string]credentials.Bundle{
		"local": &testCredentials{transportCredentials: local.NewCredentials()},
	}
	builder := NewBuilder(credentials)
	transport, err := builder.Build(serverCfg)
	if err != nil {
		t.Fatalf("Failed to build transport: %v", err)
	}
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err = transport.NewStream(ctx, "/envoy.service.discovery.v3.AggregatedDiscoveryService/StreamAggregatedResources"); err == nil {
		t.Fatal("transport.NewStream() succeeded, want failure")
	}
}

// TestStream_SendAndRecv verifies that Send() and Recv() successfully send
// and receive messages on the stream to and from the gRPC server.
//
// It starts a gRPC test server using setupTestServer(). The test then sends a
// testDiscoverRequest on the stream and verifies that the received discovery
// request on the server is same as sent. It then wait to receive a
// testDiscoverResponse from the server and verifies that the received
// discovery response is same as sent from the server.
func (s) TestStream_SendAndRecv(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	ts := setupTestServer(t, &v3discoverypb.DiscoveryResponse{VersionInfo: "1"})

	// Build a grpc-based transport to the above server.
	serverCfg := clients.ServerIdentifier{
		ServerURI:  ts.address,
		Extensions: ServerIdentifierExtension{Credentials: "local"},
	}
	credentials := map[string]credentials.Bundle{
		"local": &testCredentials{transportCredentials: local.NewCredentials()},
	}
	builder := NewBuilder(credentials)
	transport, err := builder.Build(serverCfg)
	if err != nil {
		t.Fatalf("Failed to build transport: %v", err)
	}
	defer transport.Close()

	// Create a new stream to the server.
	stream, err := transport.NewStream(ctx, "/envoy.service.discovery.v3.AggregatedDiscoveryService/StreamAggregatedResources")
	if err != nil {
		t.Fatalf("Failed to create stream: %v", err)
	}

	// Send a discovery request message on the stream.
	testDiscoverRequest := &v3discoverypb.DiscoveryRequest{VersionInfo: "1"}
	msg, err := proto.Marshal(testDiscoverRequest)
	if err != nil {
		t.Fatalf("Failed to marshal DiscoveryRequest: %v", err)
	}
	if err := stream.Send(msg); err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	// Verify that the DiscoveryRequest received on the server was same as
	// sent.
	select {
	case gotReq := <-ts.requestChan:
		if diff := cmp.Diff(testDiscoverRequest, gotReq, protocmp.Transform()); diff != "" {
			t.Fatalf("Unexpected diff in request received on server (-want +got):\n%s", diff)
		}
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for request to reach server")
	}

	// Wait until response message is received from the server.
	res, err := stream.Recv()
	if err != nil {
		t.Fatalf("Failed to receive message: %v", err)
	}

	// Verify that the DiscoveryResponse received was same as sent from the
	// server.
	var gotRes v3discoverypb.DiscoveryResponse
	if err := proto.Unmarshal(res, &gotRes); err != nil {
		t.Fatalf("Failed to unmarshal response from server to DiscoveryResponse: %v", err)
	}
	if diff := cmp.Diff(ts.response, &gotRes, protocmp.Transform()); diff != "" {
		t.Fatalf("proto.Unmarshal(res, &gotRes) returned unexpected diff (-want +got):\n%s", diff)
	}
}
