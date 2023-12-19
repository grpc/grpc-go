/*
 *
 * Copyright 2023 gRPC authors.
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

package tlscreds

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/testdata"
)

const defaultTestTimeout = 5 * time.Second

func TestFailingProvider(t *testing.T) {
	s := stubserver.StartTestService(t, nil, grpc.Creds(e2e.CreateServerTLSCredentials(t, tls.RequireAndVerifyClientCert)))
	defer s.Stop()

	cfg := fmt.Sprintf(`{
		"ca_certificate_file": "%s",
		"certificate_file": "%s",
		"private_key_file": "%s"
	}`,
		testdata.Path("x509/server_ca_cert.pem"),
		testdata.Path("x509/client1_cert.pem"),
		testdata.Path("x509/client1_key.pem"))
	tlsBundle, err := NewBundle([]byte(cfg))
	if err != nil {
		t.Fatalf("Failed to create TLS bundle: %v", err)
	}

	dialOpts := []grpc.DialOption{
		grpc.WithCredentialsBundle(tlsBundle),
		grpc.WithAuthority("x.test.example.com"),
	}

	// Check that if the provider returns an errors, we fail the handshake.
	// It's not easy to trigger this condition, so we rely on closing the
	// provider.
	creds, ok := tlsBundle.TransportCredentials().(*reloadingCreds)
	if !ok {
		t.Fatalf("Got %T, expected reloadingCreds", tlsBundle.TransportCredentials())
	}

	// Force the provider to be initialized. The test is flaky otherwise,
	// since close may be a noop.
	_, _ = creds.provider.KeyMaterial(context.Background())

	creds.provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	for ; ctx.Err() == nil; <-time.After(10 * time.Millisecond) {
		conn, err := grpc.Dial(s.Address, dialOpts...)
		if err != nil {
			t.Fatalf("Error dialing: %v", err)
		}
		client := testgrpc.NewTestServiceClient(conn)
		_, err = client.EmptyCall(context.Background(), &testpb.Empty{})
		const wantErr = "provider instance is closed"
		if err != nil && strings.Contains(err.Error(), wantErr) {
			break
		}
		t.Logf("EmptyCall() got err: %s, want err to contain: %s", err, "provider instance is closed")
		conn.Close()
	}
	if ctx.Err() != nil {
		t.Errorf("Timed out waiting for provider closed to trigger an RPC error")
	}
}
