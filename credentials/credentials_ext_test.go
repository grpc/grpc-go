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
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/local"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/testdata"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

func authorityChecker(ctx context.Context, wantAuthority string) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.InvalidArgument, "failed to parse metadata")
	}
	auths, ok := md[":authority"]
	if !ok {
		return status.Error(codes.InvalidArgument, "no authority header")
	}
	if len(auths) != 1 {
		return status.Errorf(codes.InvalidArgument, "expected exactly one authority header, got %v", auths)
	}
	if auths[0] != wantAuthority {
		return status.Errorf(codes.InvalidArgument, "invalid authority header %q, want %q", auths[0], wantAuthority)
	}
	return nil
}

func loadTLSCreds(t *testing.T) (grpc.ServerOption, grpc.DialOption) {
	t.Helper()
	cert, err := tls.LoadX509KeyPair(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		t.Fatalf("Failed to load key pair: %v", err)
		return nil, nil
	}
	serverCreds := grpc.Creds(credentials.NewServerTLSFromCert(&cert))

	clientCreds, err := credentials.NewClientTLSFromFile(testdata.Path("x509/server_ca_cert.pem"), "x.test.example.com")
	if err != nil {
		t.Fatalf("Failed to create client credentials: %v", err)
	}
	return serverCreds, grpc.WithTransportCredentials(clientCreds)
}

// Tests the scenario where the `grpc.CallAuthority` call option is used with
// different transport credentials. The test verifies that the specified
// authority is correctly propagated to the serve when a correct authority is
// used.
func TestCorrectAuthorityWithCreds(t *testing.T) {
	const authority = "auth.test.example.com"
	const authorityWithPort = "auth.test.example.com:8010"

	tests := []struct {
		name         string
		creds        func(t *testing.T) (grpc.ServerOption, grpc.DialOption)
		expectedAuth string
	}{
		{
			name: "Insecure",
			creds: func(*testing.T) (grpc.ServerOption, grpc.DialOption) {
				c := insecure.NewCredentials()
				return grpc.Creds(c), grpc.WithTransportCredentials(c)
			},
			expectedAuth: authority,
		},
		{
			name: "Local",
			creds: func(*testing.T) (grpc.ServerOption, grpc.DialOption) {
				c := local.NewCredentials()
				return grpc.Creds(c), grpc.WithTransportCredentials(c)
			},
			expectedAuth: authority,
		},
		{
			name: "TLS",
			creds: func(t *testing.T) (grpc.ServerOption, grpc.DialOption) {
				return loadTLSCreds(t)
			},
			expectedAuth: authority,
		},
		{
			name: "TLSAuthorityWithPort",
			creds: func(t *testing.T) (grpc.ServerOption, grpc.DialOption) {
				return loadTLSCreds(t)
			},
			expectedAuth: authorityWithPort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss := &stubserver.StubServer{
				EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
					if err := authorityChecker(ctx, tt.expectedAuth); err != nil {
						return nil, err
					}
					return &testpb.Empty{}, nil
				},
			}
			serverOpt, dialOpt := tt.creds(t)
			if err := ss.StartServer(serverOpt); err != nil {
				t.Fatalf("Error starting endpoint server: %v", err)
			}
			defer ss.Stop()

			cc, err := grpc.NewClient(ss.Address, dialOpt)
			if err != nil {
				t.Fatalf("grpc.NewClient(%q) = %v", ss.Address, err)
			}
			defer cc.Close()

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if _, err = testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{}, grpc.CallAuthority(tt.expectedAuth)); err != nil {
				t.Fatalf("EmptyCall() rpc failed: %v", err)
			}
		})
	}
}

// Tests the `grpc.CallAuthority` option with TLS credentials. This test verifies
// that the RPC fails with `UNAVAILABLE` status code and doesn't reach the server
// when an incorrect authority is used.
func (s) TestIncorrectAuthorityWithTLS(t *testing.T) {
	cert, err := tls.LoadX509KeyPair(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		t.Fatalf("Failed to load key pair: %s", err)
	}
	creds, err := credentials.NewClientTLSFromFile(testdata.Path("x509/server_ca_cert.pem"), "x.test.example.com")
	if err != nil {
		t.Fatalf("Failed to create credentials %v", err)
	}

	tests := []struct {
		name      string
		authority string
	}{
		{
			name:      "IncorrectAuthority",
			authority: "auth.example.com",
		},
		{
			name:      "IncorrectAuthorityWithPort",
			authority: "auth.example.com:8443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			serverCalled := make(chan struct{})
			ss := &stubserver.StubServer{
				EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
					close(serverCalled)
					return nil, nil
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
			if _, err = testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{}, grpc.CallAuthority(tt.authority)); status.Code(err) != codes.Unavailable {
				t.Fatalf("EmptyCall() returned status %v, want %v", status.Code(err), codes.Unavailable)
			}
			select {
			case <-serverCalled:
				t.Fatalf("Server handler should not have been called")
			case <-time.After(defaultTestShortTimeout):
			}
		})
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
type testAuthInfoWithValidator struct {
	validAuthority string
}

// AuthType returns the authentication type.
func (testAuthInfoWithValidator) AuthType() string {
	return "test"
}

// ValidateAuthority implements credentials.AuthorityValidator.
func (v testAuthInfoWithValidator) ValidateAuthority(authority string) error {
	if authority == v.validAuthority {
		return nil
	}
	return fmt.Errorf("invalid authority %q, want %q", authority, v.validAuthority)
}

// testCreds is a test TransportCredentials that can optionally support
// authority validation.
type testCreds struct {
	authority string
}

// ClientHandshake performs the client-side handshake.
func (c *testCreds) ClientHandshake(_ context.Context, _ string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if c.authority != "" {
		return rawConn, testAuthInfoWithValidator{validAuthority: c.authority}, nil
	}
	return rawConn, testAuthInfoNoValidator{}, nil
}

// ServerHandshake performs the server-side handshake.
func (c *testCreds) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if c.authority != "" {
		return rawConn, testAuthInfoWithValidator{validAuthority: c.authority}, nil
	}
	return rawConn, testAuthInfoNoValidator{}, nil
}

// Clone creates a copy of testCreds.
func (c *testCreds) Clone() credentials.TransportCredentials {
	return &testCreds{authority: c.authority}
}

// Info provides protocol information.
func (c *testCreds) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{}
}

// OverrideServerName overrides the server name used for verification.
func (c *testCreds) OverrideServerName(string) error {
	return nil
}

// TestAuthorityValidationFailureWithCustomCreds tests the `grpc.CallAuthority`
// call option using custom credentials. It covers two failure scenarios:
// - The credentials implement AuthorityValidator but authority used to override
// is not valid.
// - The credentials do not implement AuthorityValidator, but an authority
// override is specified.
// In both cases, the RPC is expected to fail with an `UNAVAILABLE` status code.
func (s) TestAuthorityValidationFailureWithCustomCreds(t *testing.T) {
	tests := []struct {
		name      string
		creds     credentials.TransportCredentials
		authority string
	}{
		{
			name:      "IncorrectAuthorityWithFakeCreds",
			authority: "auth.example.com",
			creds:     &testCreds{authority: "auth.test.example.com"},
		},
		{
			name:      "FakeCredsWithNoAuthValidator",
			creds:     &testCreds{},
			authority: "auth.test.example.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverCalled := make(chan struct{})
			ss := stubserver.StubServer{
				EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
					close(serverCalled)
					return nil, nil
				},
			}
			if err := ss.StartServer(); err != nil {
				t.Fatalf("Failed to start stub server: %v", err)
			}
			defer ss.Stop()

			cc, err := grpc.NewClient(ss.Address, grpc.WithTransportCredentials(tt.creds))
			if err != nil {
				t.Fatalf("grpc.NewClient(%q) = %v", ss.Address, err)
			}
			defer cc.Close()

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if _, err = testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{}, grpc.CallAuthority(tt.authority)); status.Code(err) != codes.Unavailable {
				t.Fatalf("EmptyCall() returned status %v, want %v", status.Code(err), codes.Unavailable)
			}
			select {
			case <-serverCalled:
				t.Fatalf("Server should not have been called")
			case <-time.After(defaultTestShortTimeout):
			}
		})
	}

}

// TestCorrectAuthorityWithCustomCreds tests the `grpc.CallAuthority` call
// option using custom credentials. It verifies that the provided authority is
// correctly propagated to the server when a correct authority is used.
func (s) TestCorrectAuthorityWithCustomCreds(t *testing.T) {
	const authority = "auth.test.example.com"
	creds := &testCreds{authority: "auth.test.example.com"}
	ss := stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			if err := authorityChecker(ctx, authority); err != nil {
				return nil, err
			}
			return &testpb.Empty{}, nil
		},
	}
	if err := ss.StartServer(); err != nil {
		t.Fatalf("Failed to start stub server: %v", err)
	}
	defer ss.Stop()

	cc, err := grpc.NewClient(ss.Address, grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) = %v", ss.Address, err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err = testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{}, grpc.CallAuthority(authority)); status.Code(err) != codes.OK {
		t.Fatalf("EmptyCall() returned status %v, want %v", status.Code(err), codes.OK)
	}
}

// TestAuthorityOverrideWithChainedCerts tests that the authority being used to
// overwrite per-RPC authority is validated against the leaf certificate only
// and not against the intermediate certificates.
func (s) TestAuthorityOverrideWithChainedCerts(t *testing.T) {
	rootCert, certChain, leafKey, err := generateThreeLevelCertChain()
	if err != nil {
		t.Fatalf("Failed to generate cert chain: %v", err)
	}

	// Construct server credentials from leaf and intermediate certificates.
	serverCert := tls.Certificate{
		Certificate: [][]byte{certChain[0].Raw, certChain[1].Raw},
		PrivateKey:  leafKey,
	}
	serverCreds := credentials.NewServerTLSFromCert(&serverCert)

	// Create client credentials trusting the Root CA.
	certPool := x509.NewCertPool()
	certPool.AddCert(rootCert)

	clientCreds := credentials.NewTLS(&tls.Config{
		RootCAs:    certPool,
		ServerName: "test1.example.leaf.com",
	})

	tests := []struct {
		name      string
		authority string
		wantCode  codes.Code
		wantErr   string
	}{
		{
			name:      "AuthorityMatchesIntermediateNotLeaf",
			authority: "intermediate.example.com",
			wantCode:  codes.Unavailable,
			wantErr:   "failed to validate authority",
		},
		{
			name:      "AuthorityMatchesLeaf",
			authority: "test2.example.leaf.com",
			wantCode:  codes.OK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			// Setup and start the stub server.
			ss := &stubserver.StubServer{
				EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
					if err := authorityChecker(ctx, tt.authority); err != nil {
						return nil, err
					}
					return &testpb.Empty{}, nil
				},
			}
			if err := ss.StartServer(grpc.Creds(serverCreds)); err != nil {
				t.Fatalf("failed to start server: %v", err)
			}
			defer ss.Stop()

			cc, err := grpc.NewClient(ss.Address, grpc.WithTransportCredentials(clientCreds))
			if err != nil {
				t.Fatalf("grpc.NewClient(%q) = %v", ss.Address, err)
			}
			defer cc.Close()

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			_, err = testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{}, grpc.CallAuthority(tt.authority))
			if got := status.Code(err); got != tt.wantCode {
				t.Fatalf("EmptyCall() with authority %q: got code %v, want %v; err: %v", tt.authority, got, tt.wantCode, err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("EmptyCall() with authority %q: expected error to contain %q, got %v", tt.authority, tt.wantErr, err)
			}
		})
	}
}

// generateThreeLevelCertChain generates a three-level certificate chain:
// Root -> Intermediate -> Leaf. It returns the root certificate, a slice
// containing the leaf and intermediate certificates (in that order), the
// private key for the leaf certificate, and error if any.
func generateThreeLevelCertChain() (root *x509.Certificate, chain []*x509.Certificate, leafKey *rsa.PrivateKey, err error) {
	now := time.Now()
	// Function to create the certificate based on the template provided, the
	// parent certificate and parent key. If the parent is nil, it creates a
	// self-signed certificate. It returns the created certificate and its
	// private key.
	createCert := func(template, parent *x509.Certificate, parentKey any) (*x509.Certificate, *rsa.PrivateKey, error) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, nil, err
		}

		signerKey := parentKey
		if parent == nil { // Self-signed (Root)
			parent = template
			signerKey = key
		}

		der, err := x509.CreateCertificate(rand.Reader, template, parent, key.Public(), signerKey)
		if err != nil {
			return nil, nil, err
		}
		cert, err := x509.ParseCertificate(der)
		return cert, key, err
	}

	rootTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "root.example.com"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	rootCert, rootKey, err := createCert(rootTmpl, nil, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	interTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "inter.example.com"},
		DNSNames:              []string{"intermediate.example.com"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	interCert, interKey, err := createCert(interTmpl, rootCert, rootKey)
	if err != nil {
		return nil, nil, nil, err
	}

	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "*.example.leaf.com"},
		DNSNames:     []string{"*.example.leaf.com"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafCert, leafPriv, err := createCert(leafTmpl, interCert, interKey)
	if err != nil {
		return nil, nil, nil, err
	}

	return rootCert, []*x509.Certificate{leafCert, interCert}, leafPriv, nil
}
