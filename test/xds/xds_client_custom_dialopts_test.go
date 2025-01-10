/*
 *
 * Copyright 2024 gRPC authors.
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
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	internalbootstrap "google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/xds/bootstrap"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

const testCredsBuilderName = "test_dialer_creds"

// testCredsBuilder implements the `Credentials` interface defined in
// package `xds/bootstrap` and encapsulates an insecure credential with a
// custom Dialer that specifies how to dial the xDS server.
type testCredsBuilder struct {
	dialerCalled     atomic.Bool
	tagRPCCalled     atomic.Bool
	handleRPCCalled  atomic.Bool
	tagConnCalled    atomic.Bool
	handleConnCalled atomic.Bool
}

func (t *testCredsBuilder) Build(config json.RawMessage) (credentials.Bundle, func(), error) {
	cfg := &struct {
		MgmtServerAddress string `json:"mgmt_server_address"`
	}{}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return nil, func() {}, fmt.Errorf("failed to unmarshal config: %v", err)
	}
	return &testCredsBundle{insecure.NewBundle(), &t.dialerCalled, cfg.MgmtServerAddress, &noopStatsHandler{
		tagRPCCalled:     &t.tagRPCCalled,
		handleRPCCalled:  &t.handleRPCCalled,
		tagConnCalled:    &t.tagConnCalled,
		handleConnCalled: &t.handleConnCalled,
	}}, func() {}, nil
}

func (t *testCredsBuilder) Name() string {
	return testCredsBuilderName
}

// noopStatsHandler implements `stats.Handler`. It's a no-op mock handler.
type noopStatsHandler struct {
	tagRPCCalled     *atomic.Bool
	handleRPCCalled  *atomic.Bool
	tagConnCalled    *atomic.Bool
	handleConnCalled *atomic.Bool
}

func (h *noopStatsHandler) TagRPC(ctx context.Context, i *stats.RPCTagInfo) context.Context {
	h.tagRPCCalled.Store(true)
	return ctx
}

func (h *noopStatsHandler) HandleRPC(ctx context.Context, s stats.RPCStats) {
	h.handleRPCCalled.Store(true)
}

func (h *noopStatsHandler) TagConn(ctx context.Context, i *stats.ConnTagInfo) context.Context {
	h.tagConnCalled.Store(true)
	return ctx
}

func (h *noopStatsHandler) HandleConn(ctx context.Context, s stats.ConnStats) {
	h.handleConnCalled.Store(true)
}

// testCredsBundle implements `credentials.Bundle` and `bootstrap.extraDialOptions`.
// It encapsulates an insecure credential and returns dial options with a mock dialer and a mock
// stats handler.
type testCredsBundle struct {
	credentials.Bundle
	dialerCalled      *atomic.Bool
	mgmtServerAddress string
	noopStatsHandler  *noopStatsHandler
}

func (t *testCredsBundle) DialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		// Custom dialer that creates a pass-through connection (no-op) to the xDS management server.
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			t.dialerCalled.Store(true)
			return net.Dial("tcp", t.mgmtServerAddress)
		}),
		// Custom no-op RPC stats handler.
		grpc.WithStatsHandler(t.noopStatsHandler),
	}
}

func (s) TestClientCustomDialOptsFromCredentialsBundle(t *testing.T) {
	// Create and register the credentials bundle builder.
	credsBuilder := &testCredsBuilder{}
	bootstrap.RegisterCredentials(credsBuilder)

	// Start an xDS management server.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{})

	// Create bootstrap configuration pointing to the above management server.
	nodeID := uuid.New().String()
	bc, err := internalbootstrap.NewContentsForTesting(internalbootstrap.ConfigOptionsForTesting{
		Servers: []byte(fmt.Sprintf(`[{
			"server_uri": %q,
			"channel_creds": [{
				"type": %q,
				"config": {"mgmt_server_address": %q}
			}]
		}]`, mgmtServer.Address, testCredsBuilderName, mgmtServer.Address)),
		Node: []byte(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap configuration: %v", err)
	}

	// Create an xDS resolver with the above bootstrap configuration.
	var resolverBuilder resolver.Builder
	if newResolver := internal.NewXDSResolverWithConfigForTesting; newResolver != nil {
		resolverBuilder, err = newResolver.(func([]byte) (resolver.Builder, error))(bc)
		if err != nil {
			t.Fatalf("Failed to create xDS resolver for testing: %v", err)
		}
	}

	// Spin up a test backend.
	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	// Configure client side xDS resources on the management server.
	const serviceName = "my-service-client-side-xds"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC. The insecure transport credentials passed into
	// the gRPC.NewClient is the credentials for the data plane communication with the test backend.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Close the connection to ensure stats handler calls are made.
	cc.Close()

	// Verify that the mock dialer was called.
	if !credsBuilder.dialerCalled.Load() {
		t.Errorf("credsBuilder.dialerCalled was not called")
	}

	// Verify that the mock stats handler methods were called.
	// We expect all to be called at least once.
	if !credsBuilder.tagRPCCalled.Load() {
		t.Errorf("credsBuilder.tagRPCCalled was not called")
	}
	if !credsBuilder.handleRPCCalled.Load() {
		t.Errorf("credsBuilder.handleRPCCalled was not called")
	}
	if !credsBuilder.tagConnCalled.Load() {
		t.Errorf("credsBuilder.tagConnCalled was not called")
	}
	if !credsBuilder.handleConnCalled.Load() {
		t.Errorf("credsBuilder.handleConnCalled was not called")
	}
}
