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

package jwt_test

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/jwt"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/testdata"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

const defaultTestTimeout = 5 * time.Second

// TestJWTCallCredentials_InsecureTransport_AsCallOption verifies that when JWT
// call credentials are passed as a per-RPC call option over an insecure
// transport, the RPC fails with a meaningful error.
func TestJWTCallCredentials_InsecureTransport_AsCallOption(t *testing.T) {
	token := createTestJWT(t, time.Now().Add(time.Hour))
	tokenFile := writeTempTokenFile(t, token)

	jwtCreds, err := jwt.NewTokenFileCallCredentials(tokenFile)
	if err != nil {
		t.Fatalf("NewTokenFileCallCredentials(%q) failed: %v", tokenFile, err)
	}

	ss := &stubserver.StubServer{}
	if err := ss.StartServer(grpc.Creds(insecure.NewCredentials())); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer ss.Stop()

	cc, err := grpc.NewClient(ss.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", ss.Address, err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	_, err = client.EmptyCall(ctx, &testpb.Empty{}, grpc.PerRPCCredentials(jwtCreds))

	if err == nil || !strings.Contains(err.Error(), "cannot send secure credentials on an insecure connection") {
		t.Fatalf("EmptyCall() error = %v; want error containing %q", err, "cannot send secure credentials on an insecure connection")
	}
}

// TestJWTCallCredentials_InsecureTransport_AsDialOption verifies that when JWT
// call credentials are passed as a dial option over an insecure transport, the
// client creation fails with a meaningful error.
func TestJWTCallCredentials_InsecureTransport_AsDialOption(t *testing.T) {
	token := createTestJWT(t, time.Now().Add(time.Hour))
	tokenFile := writeTempTokenFile(t, token)

	jwtCreds, err := jwt.NewTokenFileCallCredentials(tokenFile)
	if err != nil {
		t.Fatalf("NewTokenFileCallCredentials(%q) failed: %v", tokenFile, err)
	}

	ss := &stubserver.StubServer{}
	if err := ss.StartServer(grpc.Creds(insecure.NewCredentials())); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer ss.Stop()

	_, err = grpc.NewClient(ss.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(jwtCreds),
	)

	if err == nil || !strings.Contains(err.Error(), "the credentials require transport level security") {
		t.Fatalf("grpc.NewClient() error = %v; want error containing %q", err, "the credentials require transport level security")
	}
}

// TestJWTCallCredentials_SecureTransport_AsDialOption verifies that JWT call
// credentials work correctly when passed as a dial option over a secure TLS
// transport.
func TestJWTCallCredentials_SecureTransport_AsDialOption(t *testing.T) {
	token := createTestJWT(t, time.Now().Add(time.Hour))
	tokenFile := writeTempTokenFile(t, token)

	jwtCreds, err := jwt.NewTokenFileCallCredentials(tokenFile)
	if err != nil {
		t.Fatalf("NewTokenFileCallCredentials(%q) failed: %v", tokenFile, err)
	}

	wantAuth := "Bearer " + token
	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return nil, fmt.Errorf("no metadata received")
			}
			authHeaders := md.Get("authorization")
			if len(authHeaders) != 1 || authHeaders[0] != wantAuth {
				return nil, fmt.Errorf("authorization header mismatch: got %v, want %q", authHeaders, wantAuth)
			}
			return &testpb.Empty{}, nil
		},
	}

	serverCert, err := tls.LoadX509KeyPair(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		t.Fatalf("Failed to load server cert: %v", err)
	}
	if err := ss.StartServer(grpc.Creds(credentials.NewServerTLSFromCert(&serverCert))); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer ss.Stop()

	clientCreds, err := credentials.NewClientTLSFromFile(testdata.Path("x509/server_ca_cert.pem"), "x.test.example.com")
	if err != nil {
		t.Fatalf("Failed to create client TLS credentials: %v", err)
	}

	cc, err := grpc.NewClient(ss.Address,
		grpc.WithTransportCredentials(clientCreds),
		grpc.WithPerRPCCredentials(jwtCreds),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", ss.Address, err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

// TestJWTCallCredentials_SecureTransport_AsCallOption verifies that JWT call
// credentials work correctly when passed as a per-RPC call option over a secure
// TLS transport.
func TestJWTCallCredentials_SecureTransport_AsCallOption(t *testing.T) {
	token := createTestJWT(t, time.Now().Add(time.Hour))
	tokenFile := writeTempTokenFile(t, token)

	jwtCreds, err := jwt.NewTokenFileCallCredentials(tokenFile)
	if err != nil {
		t.Fatalf("NewTokenFileCallCredentials(%q) failed: %v", tokenFile, err)
	}

	wantAuth := "Bearer " + token
	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return nil, fmt.Errorf("no metadata received")
			}
			authHeaders := md.Get("authorization")
			if len(authHeaders) != 1 || authHeaders[0] != wantAuth {
				return nil, fmt.Errorf("authorization header mismatch: got %v, want %q", authHeaders, wantAuth)
			}
			return &testpb.Empty{}, nil
		},
	}

	serverCert, err := tls.LoadX509KeyPair(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		t.Fatalf("Failed to load server cert: %v", err)
	}
	if err := ss.StartServer(grpc.Creds(credentials.NewServerTLSFromCert(&serverCert))); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer ss.Stop()

	clientCreds, err := credentials.NewClientTLSFromFile(testdata.Path("x509/server_ca_cert.pem"), "x.test.example.com")
	if err != nil {
		t.Fatalf("Failed to create client TLS credentials: %v", err)
	}

	cc, err := grpc.NewClient(ss.Address, grpc.WithTransportCredentials(clientCreds))
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", ss.Address, err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.PerRPCCredentials(jwtCreds)); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

// createTestJWT creates a test JWT token with the specified expiration.
func createTestJWT(t *testing.T, expiration time.Time) string {
	t.Helper()

	claims := map[string]any{}
	if !expiration.IsZero() {
		claims["exp"] = expiration.Unix()
	}

	header := map[string]any{
		"typ": "JWT",
		"alg": "HS256",
	}
	headerBytes, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("Failed to marshal header: %v", err)
	}

	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("Failed to marshal claims: %v", err)
	}

	headerB64 := base64.URLEncoding.EncodeToString(headerBytes)
	claimsB64 := base64.URLEncoding.EncodeToString(claimsBytes)

	// Remove padding for URL-safe base64.
	headerB64 = strings.TrimRight(headerB64, "=")
	claimsB64 = strings.TrimRight(claimsB64, "=")

	// For testing, we use a fake signature.
	signature := base64.URLEncoding.EncodeToString([]byte("fake_signature"))
	signature = strings.TrimRight(signature, "=")

	return fmt.Sprintf("%s.%s.%s", headerB64, claimsB64, signature)
}

// writeTempTokenFile writes the token to a temporary file and returns the path.
func writeTempTokenFile(t *testing.T, token string) string {
	t.Helper()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "token")
	if err := os.WriteFile(filePath, []byte(token), 0600); err != nil {
		t.Fatalf("Failed to write temp token file: %v", err)
	}
	return filePath
}
