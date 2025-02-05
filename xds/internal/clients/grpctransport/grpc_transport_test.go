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
	"testing"
	"time"

	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/internal/clients"
	"google.golang.org/protobuf/proto"

	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

const (
	defaultTestWatchExpiryTimeout = 500 * time.Millisecond
	defaultTestTimeout            = 10 * time.Second
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
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
			name: "ServerURI_empty",
			serverCfg: clients.ServerConfig{
				ServerURI:  "",
				Extensions: ServerConfigExtension{Credentials: insecure.NewBundle()},
			},
			wantErr: true,
		},
		{
			name:      "Extensions_nil",
			serverCfg: clients.ServerConfig{ServerURI: "server-address"},
			wantErr:   true,
		},
		{
			name: "Extensions_not_ServerConfigExtension",
			serverCfg: clients.ServerConfig{
				ServerURI:  "server-address",
				Extensions: 1,
			},
			wantErr: true,
		},
		{
			name: "ServerConfigExtension_Credentials_nil",
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
// new client stream for the xDS management server.
func (s) TestNewStream(t *testing.T) {
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	tests := []struct {
		name      string
		serverURI string
		wantErr   bool
	}{
		{
			name:      "success",
			serverURI: mgmtServer.Address,
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
			_, err = transport.NewStream(ctx, "test-method")
			if (err != nil) != test.wantErr {
				t.Fatalf("transport.NewStream() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

// TestStream_Send verifies that grpcTransport.Stream.Send() successfully sends
// a message on the stream. It starts a management server to create a stream
// and sends a marshalled DiscoveryRequest proto on it.
func (s) TestStream_Send(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Start an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Build a grpc-based transport to the above xDS management server.
	serverCfg := clients.ServerConfig{
		ServerURI:  mgmtServer.Address,
		Extensions: ServerConfigExtension{Credentials: insecure.NewBundle()},
	}
	builder := Builder{}
	transport, err := builder.Build(serverCfg)
	if err != nil {
		t.Fatalf("failed to build transport: %v", err)
	}
	defer transport.Close()

	// Create a new stream to the xDS management server.
	stream, err := transport.NewStream(ctx, "test-method")
	if err != nil {
		t.Fatalf("failed to create stream: %v", err)
	}

	// Send a discovery request message on the stream.
	req, err := proto.Marshal(&v3discoverypb.DiscoveryRequest{})
	if err != nil {
		t.Fatalf("failed to marshal DiscoveryRequest: %v", err)
	}
	if err := stream.Send(req); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}
}
