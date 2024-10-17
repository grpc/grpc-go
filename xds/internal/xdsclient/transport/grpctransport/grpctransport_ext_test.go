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
 */

package grpctransport_test

import (
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/grpctest"
	internalbootstrap "google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/internal"
	"google.golang.org/grpc/xds/internal/xdsclient/transport"
	"google.golang.org/grpc/xds/internal/xdsclient/transport/grpctransport"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// Tests that the grpctransport.Builder creates a new grpc.ClientConn every time
// Build() is called.
func (s) TestBuild_CustomDialer(t *testing.T) {
	// Override the dialer with a custom one.
	customDialerCalled := false
	origDialer := internal.GRPCNewClient
	internal.GRPCNewClient = func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		customDialerCalled = true
		return grpc.NewClient(target, opts...)
	}
	defer func() { internal.GRPCNewClient = origDialer }()

	serverCfg, err := internalbootstrap.ServerConfigForTesting(internalbootstrap.ServerConfigTestingOptions{URI: "server-address"})
	if err != nil {
		t.Fatalf("Failed to create server config for testing: %v", err)
	}

	// Create a new transport and ensure that the custom dialer was called.
	opts := transport.BuildOptions{ServerConfig: serverCfg}
	builder := &grpctransport.Builder{}
	tr, err := builder.Build(opts)
	if err != nil {
		t.Fatalf("Builder.Build(%+v) failed: %v", opts, err)
	}
	defer tr.Close()

	if !customDialerCalled {
		t.Fatalf("Builder.Build(%+v): custom dialer called = false, want true", opts)
	}
	customDialerCalled = false

	// Create another transport and ensure that the custom dialer was called.
	tr, err = builder.Build(opts)
	if err != nil {
		t.Fatalf("Builder.Build(%+v) failed: %v", opts, err)
	}
	defer tr.Close()

	if !customDialerCalled {
		t.Fatalf("Builder.Build(%+v): custom dialer called = false, want true", opts)
	}
}

// Tests that the grpctransport.Builder fails to build a transport when the
// provided BuildOptions do not contain a ServerConfig.
func (s) TestBuild_EmptyServerConfig(t *testing.T) {
	builder := &grpctransport.Builder{}
	opts := transport.BuildOptions{}
	if tr, err := builder.Build(opts); err == nil {
		tr.Close()
		t.Fatalf("Builder.Build(%+v) succeeded when expected to fail", opts)
	}
}
