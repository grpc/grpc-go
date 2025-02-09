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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/xds/internal/clients"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
)

const (
	defaultTestTimeout = 10 * time.Second
)

var (
	testDiscoverRequest  = &v3discoverypb.DiscoveryRequest{VersionInfo: "1"}
	testDiscoverResponse = &v3discoverypb.DiscoveryResponse{VersionInfo: "1"}
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type testServer struct {
	lis         net.Listener // listener used by the test gRPC server
	requestChan chan []byte  // channel to send received requests on to verify
}

// setupTestServer starts a gRPC test server that uses the same byteCodec as
// grpcTransport. It registers a streaming handler for the "test.Service/Stream"
// method and returns a testServer struct that contains the listener and a
// channel for received requests from the client on the stream.
func setupTestServer(t *testing.T) *testServer {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen on localhost:0: %v", err)
	}
	ts := &testServer{
		requestChan: make(chan []byte, 100),
		lis:         lis,
	}

	s := grpc.NewServer(grpc.ForceServerCodec(&byteCodec{}))
	s.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Service",
		HandlerType: (*any)(nil),
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "Stream",
				Handler:       ts.streamHandler,
				ServerStreams: true,
				ClientStreams: true,
			},
		},
	}, struct{}{})
	go func() {
		if err := s.Serve(lis); err != nil {
			t.Logf("Server exited with error: %v", err)
		}
	}()
	t.Cleanup(s.Stop)

	return ts
}

// streamHandler is the handler for the "test.Service/Stream" method. It waits
// for a message from the client on the stream, and then sends a discovery
// response message back to the client. It also put the received message in
// requestChan for client to verify if the correct request was received. It
// continues until the client closes the stream.
func (s *testServer) streamHandler(_ any, stream grpc.ServerStream) error {
	for {
		var msg []byte
		err := stream.RecvMsg(&msg)
		if err == io.EOF {
			// read done.
			return nil
		}
		if err != nil {
			return err
		}
		s.requestChan <- msg

		// Send a discovery response message on the stream.
		res, err := proto.Marshal(testDiscoverResponse)
		if err != nil {
			return err
		}
		if err := stream.SendMsg(res); err != nil {
			return err
		}
	}
}

// TestBuild verifies that the grpctransport.Builder creates a new
// grpc.ClientConn every time Build() is called.
//
// It covers the following scenarios:
// - ServerURI is empty.
// - Extensions is nil.
// - Extensions is not ServerConfigExtension.
// - Credentials are nil.
// - Success cases.
func (s) TestBuild(t *testing.T) {
	tests := []struct {
		name      string
		serverCfg clients.ServerConfig
		wantErr   bool
	}{
		{
			name: "ServerURI_is_empty",
			serverCfg: clients.ServerConfig{
				ServerURI:  "",
				Extensions: ServerConfigExtension{Credentials: insecure.NewBundle()},
			},
			wantErr: true,
		},
		{
			name:      "Extensions is nil",
			serverCfg: clients.ServerConfig{ServerURI: "server-address"},
			wantErr:   true,
		},
		{
			name: "Extensions is not a ServerConfigExtension",
			serverCfg: clients.ServerConfig{
				ServerURI:  "server-address",
				Extensions: 1,
			},
			wantErr: true,
		},
		{
			name: "ServerConfigExtension Credentials is nil",
			serverCfg: clients.ServerConfig{
				ServerURI:  "server-address",
				Extensions: ServerConfigExtension{},
			},
			wantErr: true,
		},
		{
			name: "success",
			serverCfg: clients.ServerConfig{
				ServerURI:  "server-address",
				Extensions: ServerConfigExtension{Credentials: insecure.NewBundle()},
			},
			wantErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := &Builder{}
			tr, err := b.Build(test.serverCfg)
			if (err != nil) != test.wantErr {
				t.Fatalf("Build() error = %v, wantErr %v", err, test.wantErr)
			}
			if tr != nil {
				defer tr.Close()
			}
			if !test.wantErr && tr == nil {
				t.Fatalf("got non-nil transport from Build(), want nil")
			}
		})
	}
}

// TestNewStream verifies that grpcTransport.NewStream() successfully creates a
// new client stream for the server.
func (s) TestNewStream(t *testing.T) {
	ts := setupTestServer(t)

	tests := []struct {
		name      string
		serverURI string
		wantErr   bool
	}{
		{
			name:      "success",
			serverURI: ts.lis.Addr().String(),
			wantErr:   false,
		},
		{
			name:      "error",
			serverURI: "invalid-server-uri",
			wantErr:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serverCfg := clients.ServerConfig{
				ServerURI:  test.serverURI,
				Extensions: ServerConfigExtension{Credentials: insecure.NewBundle()},
			}
			builder := Builder{}
			transport, err := builder.Build(serverCfg)
			if err != nil {
				t.Fatalf("failed to build transport: %v", err)
			}
			defer transport.Close()

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			_, err = transport.NewStream(ctx, "/test.Service/Stream")
			if (err != nil) != test.wantErr {
				t.Fatalf("transport.NewStream() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

// TestStream_SendAndRecv verifies that grpcTransport.Stream.Send()
// and grpcTransport.Stream.Recv() successfully send and receive messages
// on the stream.
//
// It starts a gRPC test server using setupTestServer(). The test then sends a
// testDiscoverRequest on the stream and verifies that the received discovery
// on the server is same as sent. It then wait to receive a
// testDiscoverResponse from the server and verifies that the received
// discovery response is same as sent from the server.
func (s) TestStream_SendAndRecv(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	ts := setupTestServer(t)

	// Build a grpc-based transport to the above server.
	serverCfg := clients.ServerConfig{
		ServerURI:  ts.lis.Addr().String(),
		Extensions: ServerConfigExtension{Credentials: insecure.NewBundle()},
	}
	builder := Builder{}
	transport, err := builder.Build(serverCfg)
	if err != nil {
		t.Fatalf("failed to build transport: %v", err)
	}
	defer transport.Close()

	// Create a new stream to the server.
	stream, err := transport.NewStream(ctx, "/test.Service/Stream")
	if err != nil {
		t.Fatalf("failed to create stream: %v", err)
	}

	// Send a discovery request message on the stream.
	msg, err := proto.Marshal(testDiscoverRequest)
	if err != nil {
		t.Fatalf("failed to marshal DiscoveryRequest: %v", err)
	}
	if err := stream.Send(msg); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}
	// Verify that the DiscoveryRequest received on the server was same as
	// sent.
	select {
	case res, ok := <-ts.requestChan:
		if !ok {
			t.Fatalf("ts.requestChan is closed")
		}
		var gotReq v3discoverypb.DiscoveryRequest
		if err := proto.Unmarshal(res, &gotReq); err != nil {
			t.Fatalf("failed to unmarshal response from ts.requestChan to DiscoveryRequest: %v", err)
		}
		if !cmp.Equal(testDiscoverRequest, &gotReq, protocmp.Transform()) {
			t.Fatalf("<-ts.requestChan = %v, want %v", &gotReq, &testDiscoverRequest)
		}
	case <-ctx.Done():
		t.Fatalf("timeout waiting for request to reach server")
	}

	// Wait until response message is received from the server.
	res, err := stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive message: %v", err)
	}
	// Verify that the DiscoveryResponse received was same as sent from the
	// server.
	var gotRes v3discoverypb.DiscoveryResponse
	if err := proto.Unmarshal(res, &gotRes); err != nil {
		t.Fatalf("failed to unmarshal response from ts.requestChan to DiscoveryRequest: %v", err)
	}
	if !cmp.Equal(testDiscoverResponse, &gotRes, protocmp.Transform()) {
		t.Fatalf("proto.Unmarshal(res, &gotRes) = %v, want %v", &gotRes, testDiscoverResponse)
	}
}
