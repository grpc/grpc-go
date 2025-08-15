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

package xdsclient_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	internalbootstrap "google.golang.org/grpc/internal/xds/bootstrap"
	xci "google.golang.org/grpc/internal/xds/xdsclient/internal"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/bootstrap"
)

// nopDialOption is a no-op grpc.DialOption with a name.
type nopDialOption struct {
	grpc.EmptyDialOption
	name string
}

// testCredsBundle implements `credentials.Bundle` and `extraDialOptions`.
type testCredsBundle struct {
	credentials.Bundle
	testDialOptNames []string
}

func (t *testCredsBundle) DialOptions() []grpc.DialOption {
	var opts []grpc.DialOption
	for _, name := range t.testDialOptNames {
		opts = append(opts, &nopDialOption{name: name})
	}
	return opts
}

type testCredsBuilder struct {
	testDialOptNames []string
}

func (t *testCredsBuilder) Build(json.RawMessage) (credentials.Bundle, func(), error) {
	return &testCredsBundle{
		Bundle:           insecure.NewBundle(),
		testDialOptNames: t.testDialOptNames,
	}, func() {}, nil
}

func (t *testCredsBuilder) Name() string {
	return "test_dialer_creds"
}

func (s) TestClientCustomDialOptsFromCredentialsBundle(t *testing.T) {
	// Create and register the credentials bundle builder.
	credsBuilder := &testCredsBuilder{
		testDialOptNames: []string{"opt1", "opt2", "opt3"},
	}
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
		}]`, mgmtServer.Address, credsBuilder.Name(), mgmtServer.Address)),
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

	// Intercept a grpc.NewClient call from the xds client to validate DialOptions.
	xci.GRPCNewClient = func(target string, opts ...grpc.DialOption) (conn *grpc.ClientConn, err error) {
		got := map[string]int{}
		for _, opt := range opts {
			if mo, ok := opt.(*nopDialOption); ok {
				got[mo.name]++
			}
		}
		want := map[string]int{}
		for _, name := range credsBuilder.testDialOptNames {
			want[name]++
		}
		if !cmp.Equal(got, want) {
			t.Errorf("grpc.NewClient() was called with unexpected DialOptions: got %v, want %v", got, want)
		}
		return grpc.NewClient(target, opts...)
	}
	defer func() { xci.GRPCNewClient = grpc.NewClient }()

	// Create a ClientConn and make a successful RPC. The insecure transport
	// credentials passed into the gRPC.NewClient is the credentials for the
	// data plane communication with the test backend.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolverBuilder))
	if err != nil {
		t.Fatalf("Failed to dial local test server: %v", err)
	}

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
	cc.Close()
}
