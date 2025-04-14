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
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/testdata"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

var cert tls.Certificate
var creds credentials.TransportCredentials

func init() {
	var err error
	cert, err = tls.LoadX509KeyPair(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		log.Fatalf("failed to load key pair: %s", err)
	}
	creds, err = credentials.NewClientTLSFromFile(testdata.Path("x509/server_ca_cert.pem"), "x.test.example.com")
	if err != nil {
		log.Fatalf("Failed to create credentials %v", err)
	}
}

func authorityChecker(ctx context.Context, wantAuthority string) (*testpb.Empty, error) {
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
	if auths[0] != wantAuthority {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid authority header %v, want %v", auths[0], wantAuthority))
	}
	return &testpb.Empty{}, nil
}

// Tests the grpc.CallAuthority option with TLS credentials. This test verifies
// that the provided authority is correctly propagated to the server when using
// TLS. It covers both positive and negative cases: correct authority and
// incorrect authority, expecting the RPC to fail with `UNAVAILABLE` status code
// error in the latter case.
func TestAuthorityCallOptionsWithTLSCreds(t *testing.T) {
	tests := []struct {
		name       string
		wantAuth   string
		wantStatus codes.Code
	}{
		{
			name:       "CorrectAuthority",
			wantAuth:   "auth.test.example.com",
			wantStatus: codes.OK,
		},
		{
			name:       "IncorrectAuthority",
			wantAuth:   "auth.example.com",
			wantStatus: codes.Unavailable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss := &stubserver.StubServer{
				EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
					return authorityChecker(ctx, tt.wantAuth)
				},
			}
			if err := ss.StartServer(grpc.Creds(credentials.NewServerTLSFromCert(&cert))); err != nil {
				t.Fatalf("Error starting endpoint server: %v", err)
			}
			defer ss.Stop()

			cc, err := grpc.NewClient(ss.Address, grpc.WithTransportCredentials(creds))
			if err != nil {
				t.Fatalf("grpc.NewClient(%q) = %v", ss.Address, err)
			}
			defer cc.Close()

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			if _, err = testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{}, grpc.CallAuthority(tt.wantAuth)); status.Code(err) != tt.wantStatus {
				t.Fatalf("EmptyCall() returned status %v, want %v", status.Code(err), tt.wantStatus)
			}
		})
	}
}

// Tests the scenario where the grpc.CallAuthority per-RPC option is used with
// insecure transport credentials. The test verifies that the specified
// authority is correctly propagated to the server, even without TLS.
func (s) TestAuthorityCallOptionWithInsecureCreds(t *testing.T) {
	const wantAuthority = "test.server.name"

	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			return authorityChecker(ctx, wantAuthority)
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
	if _, err = testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{}, grpc.CallAuthority(wantAuthority)); err != nil {
		t.Fatalf("EmptyCall() rpc failed: %v", err)
	}
}

// testAuthInfoNoValidator implements only credentials.AuthInfo and not
// credentials.AuthorityValidator.
type testAuthInfoNoValidator struct{}

// AuthType returns the authentication type.
func (testAuthInfoNoValidator) AuthType() string {
	return "test"
}

// testAuthInfoWithValidator implements both credentials.AuthInfo and
// credentials.AuthorityValidator.
type testAuthInfoWithValidator struct{}

// AuthType returns the authentication type.
func (testAuthInfoWithValidator) AuthType() string {
	return "test"
}

// ValidateAuthority implements credentials.AuthorityValidator.
func (testAuthInfoWithValidator) ValidateAuthority(authority string) error {
	if authority == "auth.test.example.com" {
		return nil
	}
	return fmt.Errorf("invalid authority")
}

// testCreds is a test TransportCredentials that can optionally support
// authority validation.
type testCreds struct {
	WithValidator bool
}

// ClientHandshake performs the client-side handshake.
func (c *testCreds) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if c.WithValidator {
		return rawConn, testAuthInfoWithValidator{}, nil
	}
	return rawConn, testAuthInfoNoValidator{}, nil
}

// ServerHandshake performs the server-side handshake.
func (c *testCreds) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if c.WithValidator {
		return rawConn, testAuthInfoWithValidator{}, nil
	}
	return rawConn, testAuthInfoNoValidator{}, nil
}

// Clone creates a copy of testCreds.
func (c *testCreds) Clone() credentials.TransportCredentials {
	return &testCreds{WithValidator: c.WithValidator}
}

// Info provides protocol information.
func (c *testCreds) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{}
}

// OverrideServerName overrides the server name used for verification.
func (c *testCreds) OverrideServerName(serverName string) error {
	return nil
}

// TestCorrectAuthorityWithCustomCreds tests the CallAuthority call option with
// custom credentials that implement AuthorityValidator and verifies it with
// both correct and incorrect authority override.
func (s) TestCorrectAuthorityWithCustomCreds(t *testing.T) {
	tests := []struct {
		name       string
		creds      credentials.TransportCredentials
		wantAuth   string
		wantStatus codes.Code
	}{
		{
			name:       "CorrectAuthorityWithFakeCreds",
			wantAuth:   "auth.test.example.com",
			creds:      &testCreds{WithValidator: true},
			wantStatus: codes.OK,
		},
		{
			name:       "IncorrectAuthorityWithFakeCreds",
			wantAuth:   "auth.example.com",
			creds:      &testCreds{WithValidator: true},
			wantStatus: codes.Unavailable,
		},
		{
			name:       "FakeCredsWithNoAuthValidator",
			creds:      &testCreds{WithValidator: false},
			wantAuth:   "auth.test.example.com",
			wantStatus: codes.Unavailable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss := stubserver.StubServer{
				EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
					return authorityChecker(ctx, tt.wantAuth)
				},
			}
			if err := ss.StartServer(); err != nil {
				t.Fatalf("Failed to start stub server: %v", err)
			}
			defer ss.Stop()

			// Create a gRPC client connection with FakeCredsWithAuthValidator.
			clientConn, err := grpc.NewClient(ss.Address,
				grpc.WithTransportCredentials(tt.creds))
			if err != nil {
				t.Fatalf("Failed to create gRPC client connection: %v", err)
			}
			defer clientConn.Close()

			// Perform a test RPC with a specified call authority.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if _, err = testgrpc.NewTestServiceClient(clientConn).EmptyCall(ctx, &testpb.Empty{}, grpc.CallAuthority(tt.wantAuth)); status.Code(err) != tt.wantStatus {
				t.Fatalf("EmptyCall() returned status %v, want %v", status.Code(err), tt.wantStatus)
			}
		})
	}
}
