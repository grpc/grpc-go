/*
 *
 * Copyright 2022 gRPC authors.
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
 */

package transport_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	internalbootstrap "google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/xds/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/transport"
	"google.golang.org/grpc/xds/internal/xdsclient/transport/internal"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const defaultTestTimeout = 10 * time.Second

var noopRecvHandler = func(_ transport.ResourceUpdate, onDone func()) error {
	onDone()
	return nil
}

func (s) TestNewWithGRPCDial(t *testing.T) {
	// Override the dialer with a custom one.
	customDialerCalled := false
	customDialer := func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		customDialerCalled = true
		return grpc.NewClient(target, opts...)
	}
	oldDial := internal.GRPCNewClient
	internal.GRPCNewClient = customDialer
	defer func() { internal.GRPCNewClient = oldDial }()

	serverCfg, err := internalbootstrap.ServerConfigForTesting(internalbootstrap.ServerConfigTestingOptions{URI: "server-address"})
	if err != nil {
		t.Fatalf("Failed to create server config for testing: %v", err)
	}
	// Create a new transport and ensure that the custom dialer was called.
	opts := transport.Options{
		ServerCfg: serverCfg,
		NodeProto: &v3corepb.Node{},
		OnRecvHandler: func(update transport.ResourceUpdate, onDone func()) error {
			onDone()
			return nil
		},
		OnErrorHandler: func(error) {},
		OnSendHandler:  func(*transport.ResourceSendInfo) {},
	}
	c, err := transport.New(opts)
	if err != nil {
		t.Fatalf("transport.New(%v) failed: %v", opts, err)
	}
	defer c.Close()

	if !customDialerCalled {
		t.Fatalf("transport.New(%+v) custom dialer called = false, want true", opts)
	}
	customDialerCalled = false

	// Reset the dialer, create a new transport and ensure that our custom
	// dialer is no longer called.
	internal.GRPCNewClient = grpc.NewClient
	c, err = transport.New(opts)
	defer func() {
		if c != nil {
			c.Close()
		}
	}()
	if err != nil {
		t.Fatalf("transport.New(%v) failed: %v", opts, err)
	}

	if customDialerCalled {
		t.Fatalf("transport.New(%+v) custom dialer called = true, want false", opts)
	}
}

const testDialerCredsBuilderName = "test_dialer_creds"

// testDialerCredsBuilder implements the `Credentials` interface defined in
// package `xds/bootstrap` and encapsulates an insecure credential with a
// custom Dialer that specifies how to dial the xDS server.
type testDialerCredsBuilder struct {
	// Closed with the custom Dialer is invoked.
	// Needs to be passed in by the test.
	dialCalled chan struct{}
}

func (t *testDialerCredsBuilder) Build(json.RawMessage) (credentials.Bundle, func(), error) {
	return &testDialerCredsBundle{
		Bundle:     insecure.NewBundle(),
		dialCalled: t.dialCalled,
	}, func() {}, nil
}

func (t *testDialerCredsBuilder) Name() string {
	return testDialerCredsBuilderName
}

// testDialerCredsBundle implements the `Bundle` interface defined in package
// `credentials` and encapsulates an insecure credential with a custom Dialer
// that specifies how to dial the xDS server.
type testDialerCredsBundle struct {
	credentials.Bundle
	dialCalled chan struct{}
}

func (t *testDialerCredsBundle) Dialer(_ context.Context, address string) (net.Conn, error) {
	close(t.dialCalled)
	return net.Dial("tcp", address)
}

func (s) TestNewWithDialerFromCredentialsBundle(t *testing.T) {
	// Override grpc.NewClient with a custom one.
	doptsLen := 0
	customGRPCNewClient := func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		doptsLen = len(opts)
		return grpc.NewClient(target, opts...)
	}
	oldGRPCNewClient := internal.GRPCNewClient
	internal.GRPCNewClient = customGRPCNewClient
	defer func() { internal.GRPCNewClient = oldGRPCNewClient }()

	dialCalled := make(chan struct{})
	bootstrap.RegisterCredentials(&testDialerCredsBuilder{dialCalled: dialCalled})
	serverCfg, err := internalbootstrap.ServerConfigForTesting(internalbootstrap.ServerConfigTestingOptions{
		URI:          "trafficdirector.googleapis.com:443",
		ChannelCreds: []internalbootstrap.ChannelCreds{{Type: testDialerCredsBuilderName}},
	})
	if err != nil {
		t.Fatalf("Failed to create server config for testing: %v", err)
	}

	// Create a new transport.
	opts := transport.Options{
		ServerCfg: serverCfg,
		NodeProto: &v3corepb.Node{},
		OnRecvHandler: func(update transport.ResourceUpdate, onDone func()) error {
			onDone()
			return nil
		},
		OnErrorHandler: func(error) {},
		OnSendHandler:  func(*transport.ResourceSendInfo) {},
	}
	c, err := transport.New(opts)
	defer func() {
		if c != nil {
			c.Close()
		}
	}()
	if err != nil {
		t.Fatalf("transport.New(%v) failed: %v", opts, err)
	}
	select {
	case <-dialCalled:
	case <-time.After(defaultTestTimeout):
		t.Fatal("Timeout when waiting for Dialer() to be invoked")
	}
	// Verify there are three dial options passed to the custom grpc.NewClient.
	// The first is opts.ServerCfg.CredsDialOption(), the second is
	// grpc.WithKeepaliveParams(), and the third is opts.ServerCfg.DialerOption()
	// from the credentials bundle.
	if doptsLen != 3 {
		t.Fatalf("transport.New(%v) custom grpc.NewClient called with %d dial options, want 3", opts, doptsLen)
	}
}
