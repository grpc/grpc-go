/*
 *
 * Copyright 2018 gRPC authors.
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

package test

// TODO: move all creds releated tests to this file.

import (
	"fmt"
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/leakcheck"
	testpb "google.golang.org/grpc/test/grpc_testing"
	"google.golang.org/grpc/testdata"
)

const (
	bundlePerRPCOnly = "perRPCOnly"
	bundleTLSOnly    = "tlsOnly"
)

type testCredsBundle struct {
	t    *testing.T
	mode string
}

func (c *testCredsBundle) TransportCredentials() (credentials.TransportCredentials, error) {
	if c.mode == bundlePerRPCOnly {
		return nil, nil
	}

	creds, err := credentials.NewClientTLSFromFile(testdata.Path("ca.pem"), "x.test.youtube.com")
	if err != nil {
		return nil, fmt.Errorf("Failed to load credentials: %v", err)
	}
	return creds, nil
}

func (c *testCredsBundle) PerRPCCredentials() (credentials.PerRPCCredentials, error) {
	if c.mode == bundleTLSOnly {
		return nil, nil
	}
	return testPerRPCCredentials{}, nil
}

func (c *testCredsBundle) SwitchMode(mode string) credentials.Bundle {
	return &testCredsBundle{mode: mode}
}

func TestCredsBundleBoth(t *testing.T) {
	defer leakcheck.Check(t)
	te := newTest(t, env{name: "creds-bundle", network: "tcp", balancer: "v1", security: "empty"})
	te.tapHandle = authHandle
	te.customDialOptions = []grpc.DialOption{
		grpc.WithCreds(&testCredsBundle{t: t}),
	}
	creds, err := credentials.NewServerTLSFromFile(testdata.Path("server1.pem"), testdata.Path("server1.key"))
	if err != nil {
		t.Fatalf("Failed to generate credentials %v", err)
	}
	te.customServerOptions = []grpc.ServerOption{
		grpc.Creds(creds),
	}
	te.startServer(&testServer{})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
		t.Fatalf("Test failed. Reason: %v", err)
	}
}

func TestCredsBundleTransportCredentials(t *testing.T) {
	defer leakcheck.Check(t)
	te := newTest(t, env{name: "creds-bundle", network: "tcp", balancer: "v1", security: "empty"})
	te.customDialOptions = []grpc.DialOption{
		grpc.WithCreds(&testCredsBundle{t: t, mode: bundleTLSOnly}),
	}
	creds, err := credentials.NewServerTLSFromFile(testdata.Path("server1.pem"), testdata.Path("server1.key"))
	if err != nil {
		t.Fatalf("Failed to generate credentials %v", err)
	}
	te.customServerOptions = []grpc.ServerOption{
		grpc.Creds(creds),
	}
	te.startServer(&testServer{})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
		t.Fatalf("Test failed. Reason: %v", err)
	}
}

func TestCredsBundlePerRPCCredentials(t *testing.T) {
	defer leakcheck.Check(t)
	te := newTest(t, env{name: "creds-bundle", network: "tcp", balancer: "v1", security: "empty"})
	te.tapHandle = authHandle
	te.customDialOptions = []grpc.DialOption{
		grpc.WithCreds(&testCredsBundle{t: t, mode: bundlePerRPCOnly}),
	}
	te.startServer(&testServer{})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
		t.Fatalf("Test failed. Reason: %v", err)
	}
}
