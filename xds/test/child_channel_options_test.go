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

package xds_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds"
	xdsbootstrap "google.golang.org/grpc/xds/bootstrap"
)

// TestChildChannelOptions_Client verifies that user-specified child dial options
// propagate correctly to client-side channels established with the control plane.
func (s) TestChildChannelOptions_Client(t *testing.T) {
	userAgentCh := make(chan string, 10)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamOpen: func(ctx context.Context, id int64, typeURL string) error {
			md, ok := metadata.FromIncomingContext(ctx)
			t.Logf("OnStreamOpen: md=%v, ok=%v", md, ok)
			if ok {
				if ua := md.Get("user-agent"); len(ua) > 0 {
					select {
					case userAgentCh <- ua[0]:
					default:
					}
				}
			}
			return nil
		},
	})

	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	config, err := bootstrap.NewConfigFromContents(bc)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %v", err)
	}

	pool := xdsclient.NewPool(config)

	resolverBuilder, err := internal.NewXDSResolverWithPoolForTesting.(func(*xdsclient.Pool) (resolver.Builder, error))(pool)
	if err != nil {
		t.Fatalf("Failed to create xds resolver builder: %v", err)
	}

	cc, err := grpc.NewClient(
		"xds:///my-service",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChildDialOptions(grpc.WithUserAgent("child-agent-client")),
		grpc.WithResolvers(resolverBuilder),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer cc.Close()

	// Trigger resolver build and xDS client connection to management server
	cc.Connect()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	var gotUA string
	select {
	case gotUA = <-userAgentCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for OnStreamOpen to be called on management server")
	}

	if !strings.Contains(gotUA, "child-agent-client") {
		t.Errorf("Received user-agent = %q, want it to contain %q", gotUA, "child-agent-client")
	}
}

// TestChildChannelOptions_Server verifies that user-specified child dial options
// propagate correctly to server-side channels established with the control plane.
func (s) TestChildChannelOptions_Server(t *testing.T) {
	userAgentCh := make(chan string, 10)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamOpen: func(ctx context.Context, id int64, typeURL string) error {
			md, ok := metadata.FromIncomingContext(ctx)
			t.Logf("OnStreamOpen: md=%v, ok=%v", md, ok)
			if ok {
				if ua := md.Get("user-agent"); len(ua) > 0 {
					select {
					case userAgentCh <- ua[0]:
					default:
					}
				}
			}
			return nil
		},
	})

	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	config, err := bootstrap.NewConfigFromContents(bc)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %v", err)
	}

	pool := xdsclient.NewPool(config)

	xs, err := xds.NewGRPCServer(
		grpc.ChildDialOptions(grpc.WithUserAgent("child-agent-server")),
		xds.ClientPoolForTesting(pool),
	)
	if err != nil {
		t.Fatalf("Failed to create xds server: %v", err)
	}
	defer xs.Stop()

	// Trigger connection to management server by calling Serve on a listener.
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer lis.Close()

	go xs.Serve(lis)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	var gotUA string
	select {
	case gotUA = <-userAgentCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for OnStreamOpen to be called on management server")
	}

	if !strings.Contains(gotUA, "child-agent-server") {
		t.Errorf("Received user-agent = %q, want it to contain %q", gotUA, "child-agent-server")
	}
}

type precedenceCredsBuilder struct{}

func (precedenceCredsBuilder) Build(config json.RawMessage) (credentials.Bundle, func(), error) {
	return precedenceBundle{Bundle: insecure.NewBundle()}, func() {}, nil
}

func (precedenceCredsBuilder) Name() string {
	return "precedence-creds"
}

type precedenceBundle struct {
	credentials.Bundle
}

func (precedenceBundle) DialOptions() []grpc.DialOption {
	return []grpc.DialOption{grpc.WithUserAgent("bootstrap-agent")}
}

// TestChildChannelOptions_Precedence verifies that options configured in the
// bootstrap configuration take precedence over child dial options.
func (s) TestChildChannelOptions_Precedence(t *testing.T) {
	xdsbootstrap.RegisterChannelCredentials(precedenceCredsBuilder{})

	userAgentCh := make(chan string, 10)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamOpen: func(ctx context.Context, id int64, typeURL string) error {
			md, ok := metadata.FromIncomingContext(ctx)
			t.Logf("OnStreamOpen: md=%v, ok=%v", md, ok)
			if ok {
				if ua := md.Get("user-agent"); len(ua) > 0 {
					select {
					case userAgentCh <- ua[0]:
					default:
					}
				}
			}
			return nil
		},
	})

	nodeID := uuid.New().String()
	serversJSON := fmt.Sprintf(`[{
		"server_uri": "passthrough:///%s",
		"channel_creds": [{"type": "precedence-creds"}]
	}]`, mgmtServer.Address)

	bc, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: []byte(serversJSON),
		Node:    []byte(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap configuration: %v", err)
	}

	config, err := bootstrap.NewConfigFromContents(bc)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %v", err)
	}

	pool := xdsclient.NewPool(config)

	resolverBuilder, err := internal.NewXDSResolverWithPoolForTesting.(func(*xdsclient.Pool) (resolver.Builder, error))(pool)
	if err != nil {
		t.Fatalf("Failed to create xds resolver builder: %v", err)
	}

	cc, err := grpc.NewClient(
		"xds:///my-service",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChildDialOptions(grpc.WithUserAgent("child-agent")),
		grpc.WithResolvers(resolverBuilder),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer cc.Close()

	// Trigger connection
	cc.Connect()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	var gotUA string
	select {
	case gotUA = <-userAgentCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for OnStreamOpen to be called on management server")
	}

	if !strings.Contains(gotUA, "bootstrap-agent") {
		t.Errorf("Received user-agent = %q, want it to contain %q", gotUA, "bootstrap-agent")
	}
	if strings.Contains(gotUA, "child-agent") {
		t.Errorf("Received user-agent = %q, want it NOT to contain %q", gotUA, "child-agent")
	}
}

// TestChildChannelOptions_SharedResourceIgnoresSubsequent verifies that when
// an xDS client is shared, subsequent dials with different options do not recreate
// or modify the connection, and are ignored.
func (s) TestChildChannelOptions_SharedResourceIgnoresSubsequent(t *testing.T) {
	userAgentCh := make(chan string, 10)
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		OnStreamOpen: func(ctx context.Context, id int64, typeURL string) error {
			md, ok := metadata.FromIncomingContext(ctx)
			t.Logf("OnStreamOpen: md=%v, ok=%v", md, ok)
			if ok {
				if ua := md.Get("user-agent"); len(ua) > 0 {
					select {
					case userAgentCh <- ua[0]:
					default:
					}
				}
			}
			return nil
		},
	})

	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	config, err := bootstrap.NewConfigFromContents(bc)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %v", err)
	}

	pool := xdsclient.NewPool(config)

	resolverBuilder, err := internal.NewXDSResolverWithPoolForTesting.(func(*xdsclient.Pool) (resolver.Builder, error))(pool)
	if err != nil {
		t.Fatalf("Failed to create xds resolver builder: %v", err)
	}

	cc1, err := grpc.NewClient(
		"xds:///service-1",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChildDialOptions(grpc.WithUserAgent("agent-1")),
		grpc.WithResolvers(resolverBuilder),
	)
	if err != nil {
		t.Fatalf("Failed to create client 1: %v", err)
	}
	defer cc1.Close()

	// Trigger connection for cc1
	cc1.Connect()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	var gotUA1 string
	select {
	case gotUA1 = <-userAgentCh:
	case <-ctx.Done():
		t.Fatal("Timeout waiting for first client connection")
	}

	if !strings.Contains(gotUA1, "agent-1") {
		t.Errorf("First connection user-agent = %q, want it to contain %q", gotUA1, "agent-1")
	}

	cc2, err := grpc.NewClient(
		"xds:///service-1", // Use the same target name to ensure connection sharing
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChildDialOptions(grpc.WithUserAgent("agent-2")),
		grpc.WithResolvers(resolverBuilder),
	)
	if err != nil {
		t.Fatalf("Failed to create client 2: %v", err)
	}
	defer cc2.Close()

	// Trigger connection for cc2
	cc2.Connect()

	select {
	case gotUA2 := <-userAgentCh:
		t.Fatalf("A new stream was opened to the management server with user-agent = %q; expected connection sharing", gotUA2)
	case <-time.After(500 * time.Millisecond):
		// Success: no new stream was opened, meaning cc2 reused the cached connection/client.
	}
}
