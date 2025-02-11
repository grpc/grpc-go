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
	"google.golang.org/grpc/xds/bootstrap"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

const testDialerCredsBuilderName = "test_dialer_creds"

// testDialerCredsBuilder implements the `Credentials` interface defined in
// package `xds/bootstrap` and encapsulates an insecure credential with a
// custom Dialer that specifies how to dial the xDS server.
type testDialerCredsBuilder struct {
	dialerCalled chan struct{}
}

func (t *testDialerCredsBuilder) Build(config json.RawMessage) (credentials.Bundle, func(), error) {
	cfg := &struct {
		MgmtServerAddress string `json:"mgmt_server_address"`
	}{}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return nil, func() {}, fmt.Errorf("failed to unmarshal config: %v", err)
	}
	return &testDialerCredsBundle{insecure.NewBundle(), t.dialerCalled, cfg.MgmtServerAddress}, func() {}, nil
}

func (t *testDialerCredsBuilder) Name() string {
	return testDialerCredsBuilderName
}

// testDialerCredsBundle implements the `Bundle` interface defined in package
// `credentials` and encapsulates an insecure credential with a custom Dialer
// that specifies how to dial the xDS server.
type testDialerCredsBundle struct {
	credentials.Bundle
	dialerCalled      chan struct{}
	mgmtServerAddress string
}

// Dialer specifies how to dial the xDS management server.
func (t *testDialerCredsBundle) Dialer(context.Context, string) (net.Conn, error) {
	close(t.dialerCalled)
	// Create a pass-through connection (no-op) to the xDS management server.
	return net.Dial("tcp", t.mgmtServerAddress)
}

func (s) TestClientCustomDialerFromCredentialsBundle(t *testing.T) {
	// Create and register the credentials bundle builder.
	credsBuilder := &testDialerCredsBuilder{dialerCalled: make(chan struct{})}
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
		}]`, mgmtServer.Address, testDialerCredsBuilderName, mgmtServer.Address)),
		Node: []byte(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap configuration: %v", err)
	}

	// Create an xDS resolver with the above bootstrap configuration.
	if internal.NewXDSResolverWithConfigForTesting == nil {
		t.Fatalf("internal.NewXDSResolverWithConfigForTesting is nil")
	}
	resolverBuilder, err := internal.NewXDSResolverWithConfigForTesting.(func([]byte) (resolver.Builder, error))(bc)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
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
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	// Verify that the custom dialer was called.
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout when waiting for custom dialer to be called")
	case <-credsBuilder.dialerCalled:
	}
}
