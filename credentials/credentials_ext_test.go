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

package credentials_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/stubserver"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/testdata"
)

func authorityChecker(ctx context.Context, expectedAuthority string) (*testpb.Empty, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "failed to parse metadata")
	}
	auths, ok := md[":authority"]
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "no authority header")
	}
	if len(auths) != 1 {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("no authority header, auths = %v", auths))
	}
	if auths[0] != expectedAuthority {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid authority header %v, expected %v", auths[0], expectedAuthority))
	}
	return &testpb.Empty{}, nil
}

func checkUnavailableRPCError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("EmptyCall() should fail")
	}
	s, ok := status.FromError(err)
	if !ok {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Code() != codes.Unavailable {
		t.Fatalf("EmptyCall() = _, %v, want _, error code: %v", s.Code(), codes.Unavailable)
	}
}

// Tests the grpc.CallAuthority option with TLS credentials. This test verifies
// that the provided authority is correctly propagated to the server when using TLS.
// It covers both positive and negative cases:  correct authority and incorrect
// authority, expecting the RPC to fail with `UNAVAILABLE` status code error in
// the latter case.
func (s) TestAuthorityCallOptionsWithTLSCreds(t *testing.T) {
	tests := []struct {
		name           string
		expectedAuth   string
		expectRPCError bool
	}{
		{
			name:           "CorrectAuthorityWithTLS",
			expectedAuth:   "auth.test.example.com",
			expectRPCError: false,
		},
		{
			name:           "IncorrectAuthorityWithTLS",
			expectedAuth:   "auth.example.com",
			expectRPCError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cert, err := tls.LoadX509KeyPair(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
			if err != nil {
				log.Fatalf("failed to load key pair: %s", err)
			}
			ss := &stubserver.StubServer{
				EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
					return authorityChecker(ctx, tt.expectedAuth)
				},
			}
			if err := ss.StartServer(grpc.Creds(credentials.NewServerTLSFromCert(&cert))); err != nil {
				t.Fatalf("Error starting endpoint server: %v", err)
			}
			defer ss.Stop()
			creds, err := credentials.NewClientTLSFromFile(testdata.Path("x509/server_ca_cert.pem"), "x.test.example.com")
			if err != nil {
				t.Fatalf("Failed to create credentials %v", err)
			}

			cc, err := grpc.NewClient(ss.Address, grpc.WithTransportCredentials(creds))
			if err != nil {
				t.Fatalf("grpc.NewClient(%q) = %v", ss.Address, err)
			}
			defer cc.Close()

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			_, err = testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{}, grpc.CallAuthority(tt.expectedAuth))
			if tt.expectRPCError {
				checkUnavailableRPCError(t, err)
			} else if err != nil {
				t.Fatalf("EmptyCall() rpc failed: %v", err)
			}
		})
	}
}

func (s) TestTLSCredsWithNoAuthorityOverride(t *testing.T) {
	cert, err := tls.LoadX509KeyPair(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		log.Fatalf("failed to load key pair: %s", err)
	}
	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			return authorityChecker(ctx, "x.test.example.com")
		},
	}
	if err := ss.StartServer(grpc.Creds(credentials.NewServerTLSFromCert(&cert))); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()
	creds, err := credentials.NewClientTLSFromFile(testdata.Path("x509/server_ca_cert.pem"), "x.test.example.com")
	if err != nil {
		t.Fatalf("Failed to create credentials %v", err)
	}

	cc, err := grpc.NewClient(ss.Address, grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) = %v", ss.Address, err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	_, err = testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{})
	if err != nil {
		t.Fatalf("EmptyCall() rpc failed: %v", err)
	}
}

// Tests the scenario where grpc.CallAuthority option is used with insecure credentials.
// The test verifies that the CallAuthority option is correctly passed even when
// insecure credentials are used.
func (s) TestAuthorityCallOptionWithInsecureCreds(t *testing.T) {
	const expectedAuthority = "test.server.name"

	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			return authorityChecker(ctx, expectedAuthority)
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	cc, err := grpc.NewClient(ss.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) = %v", ss.Address, err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err = testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{}, grpc.CallAuthority(expectedAuthority)); err != nil {
		t.Fatalf("EmptyCall() rpc failed: %v", err)
	}
}

// FakeCredsNoAuthValidator is a test credential that does not implement AuthorityValidator.
type FakeCredsNoAuthValidator struct {
}

// ClientHandshake performs the client-side handshake.
func (c *FakeCredsNoAuthValidator) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return rawConn, TestAuthInfo{}, nil
}

// TestAuthInfo implements the AuthInfo interface.
type TestAuthInfo struct{}

// AuthType returns the authentication type.
func (TestAuthInfo) AuthType() string { return "test" }

// Clone creates a copy of FakeCredsNoAuthValidator.
func (c *FakeCredsNoAuthValidator) Clone() credentials.TransportCredentials {
	return c
}

// Info provides protocol information.
func (c *FakeCredsNoAuthValidator) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{}
}

// OverrideServerName overrides the server name used for verification.
func (c *FakeCredsNoAuthValidator) OverrideServerName(serverName string) error {
	return nil
}

// ServerHandshake performs the server-side handshake.
// Returns a test AuthInfo object to satisfy the interface requirements.
func (c *FakeCredsNoAuthValidator) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return rawConn, TestAuthInfo{}, nil
}

// TestCallOptionWithNoAuthorityValidator tests the CallAuthority call option
// with custom credentials that do not implement AuthorityValidator and verifies
// that it fails with `UNAVAILABLE` status code.
func (s) TestCallOptionWithNoAuthorityValidator(t *testing.T) {
	const expectedAuthority = "auth.test.example.com"

	// Initialize a stub server with a basic handler.
	ss := stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	if err := ss.StartServer(); err != nil {
		t.Fatalf("Failed to start stub server: %v", err)
	}
	defer ss.Stop()

	// Create a gRPC client connection with FakeCredsNoAuthValidator.
	clientConn, err := grpc.NewClient(ss.Address,
		grpc.WithTransportCredentials(&FakeCredsNoAuthValidator{}))
	if err != nil {
		t.Fatalf("Failed to create gRPC client connection: %v", err)
	}
	defer clientConn.Close()

	// Perform a test RPC with a specified call authority.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	_, err = testgrpc.NewTestServiceClient(clientConn).EmptyCall(ctx, &testpb.Empty{}, grpc.CallAuthority(expectedAuthority))

	// Verify that the RPC fails with an UNAVAILABLE error.
	checkUnavailableRPCError(t, err)
}

// FakeCredsWithAuthValidator is a test credential that does not implement AuthorityValidator.
type FakeCredsWithAuthValidator struct {
}

// ClientHandshake performs the client-side handshake.
func (c *FakeCredsWithAuthValidator) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return rawConn, FakeAuthInfo{}, nil
}

// TestAuthInfo implements the AuthInfo interface.
type FakeAuthInfo struct{}

// AuthType returns the authentication type.
func (FakeAuthInfo) AuthType() string { return "test" }

// AuthType returns the authentication type.
func (FakeAuthInfo) ValidateAuthority(authority string) error {
	if authority == "auth.test.example.com" {
		return nil
	} else {
		return fmt.Errorf("invalid authority")
	}
}

// Clone creates a copy of FakeCredsWithAuthValidator.
func (c *FakeCredsWithAuthValidator) Clone() credentials.TransportCredentials {
	return c
}

// Info provides protocol information.
func (c *FakeCredsWithAuthValidator) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{}
}

// OverrideServerName overrides the server name used for verification.
func (c *FakeCredsWithAuthValidator) OverrideServerName(serverName string) error {
	return nil
}

// ServerHandshake performs the server-side handshake.
// Returns a test AuthInfo object to satisfy the interface requirements.
func (c *FakeCredsWithAuthValidator) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return rawConn, FakeAuthInfo{}, nil
}

// TestCorrectAuthorityWithCustomCreds tests the CallAuthority call option
// with custom credentials that implement AuthorityValidator and verifies
// it with both correct and incorrect authority override.
func (s) TestCorrectAuthorityWithCustomCreds(t *testing.T) {
	tests := []struct {
		name           string
		expectedAuth   string
		expectRPCError bool
	}{
		{
			name:           "CorrectAuthorityWithFakeCreds",
			expectedAuth:   "auth.test.example.com",
			expectRPCError: false,
		},
		{
			name:           "IncorrectAuthorityWithFakeCreds",
			expectedAuth:   "auth.example.com",
			expectRPCError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss := stubserver.StubServer{
				EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
					return authorityChecker(ctx, tt.expectedAuth)
				},
			}
			if err := ss.StartServer(); err != nil {
				t.Fatalf("Failed to start stub server: %v", err)
			}
			defer ss.Stop()

			// Create a gRPC client connection with FakeCredsWithAuthValidator.
			clientConn, err := grpc.NewClient(ss.Address,
				grpc.WithTransportCredentials(&FakeCredsWithAuthValidator{}))
			if err != nil {
				t.Fatalf("Failed to create gRPC client connection: %v", err)
			}
			defer clientConn.Close()

			// Perform a test RPC with a specified call authority.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			_, err = testgrpc.NewTestServiceClient(clientConn).EmptyCall(ctx, &testpb.Empty{}, grpc.CallAuthority(tt.expectedAuth))
			if tt.expectRPCError {
				checkUnavailableRPCError(t, err)
			} else if err != nil {
				t.Fatalf("EmptyCall() rpc failed: %v", err)
			}
		})
	}
}
