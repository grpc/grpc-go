/*
 *
 * Copyright 2026 gRPC authors.
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
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/xds/bootstrap"
	xdscreds "google.golang.org/grpc/internal/xds/credentials"
	"google.golang.org/grpc/testdata"
	"google.golang.org/protobuf/types/known/anypb"

	tlscredspb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/tls/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestTimeout = 10 * time.Second

	tlsCredsTypeURL = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.tls.v3.TlsCredentials"
)

// testBootstrapConfig returns a bootstrap config with two certificate
// provider instances: "root-instance" watching the CA certificate that signed
// the test server's certificate, and "identity-instance" watching a client
// certificate and key.
func testBootstrapConfig(t *testing.T) *bootstrap.Config {
	t.Helper()

	// Use forward slashes so the paths survive JSON encoding on Windows.
	rootCert := filepath.ToSlash(testdata.Path("x509/server_ca_cert.pem"))
	clientCert := filepath.ToSlash(testdata.Path("x509/client1_cert.pem"))
	clientKey := filepath.ToSlash(testdata.Path("x509/client1_key.pem"))

	contents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: json.RawMessage(`[{"server_uri": "passthrough:///unused", "channel_creds": [{"type": "insecure"}]}]`),
		Node:    json.RawMessage(`{"id": "test-node"}`),
		CertificateProviders: map[string]json.RawMessage{
			"root-instance": json.RawMessage(fmt.Sprintf(`{
				"plugin_name": "file_watcher",
				"config": {"ca_certificate_file": %q, "refresh_interval": "600s"}
			}`, rootCert)),
			"identity-instance": json.RawMessage(fmt.Sprintf(`{
				"plugin_name": "file_watcher",
				"config": {"certificate_file": %q, "private_key_file": %q, "refresh_interval": "600s"}
			}`, clientCert, clientKey)),
		},
	})
	if err != nil {
		t.Fatalf("NewContentsForTesting() failed: %v", err)
	}
	cfg, err := bootstrap.NewConfigFromContents(contents)
	if err != nil {
		t.Fatalf("NewConfigFromContents() failed: %v", err)
	}
	return cfg
}

// tlsCredsConfig returns a marshaled TlsCredentials plugin config referencing
// the given provider instance names. An empty identity omits the identity
// certificate provider.
func tlsCredsConfig(t *testing.T, root, identity string) *anypb.Any {
	t.Helper()
	cfg := &tlscredspb.TlsCredentials{}
	if root != "" {
		cfg.RootCertificateProvider = &v3tlspb.CommonTlsContext_CertificateProviderInstance{InstanceName: root}
	}
	if identity != "" {
		cfg.IdentityCertificateProvider = &v3tlspb.CommonTlsContext_CertificateProviderInstance{InstanceName: identity}
	}
	a, err := anypb.New(cfg)
	if err != nil {
		t.Fatalf("Failed to marshal TlsCredentials: %v", err)
	}
	return a
}

// Tests that building TLS channel credentials validates the certificate
// provider instance names against the bootstrap config.
func (s) TestTLSCredsBuild_Errors(t *testing.T) {
	bc := testBootstrapConfig(t)

	// The tlsCredsConfig helper cannot express an identity certificate
	// provider with an empty instance name, so build that config directly.
	emptyIdentityInstance, err := anypb.New(&tlscredspb.TlsCredentials{
		RootCertificateProvider:     &v3tlspb.CommonTlsContext_CertificateProviderInstance{InstanceName: "root-instance"},
		IdentityCertificateProvider: &v3tlspb.CommonTlsContext_CertificateProviderInstance{},
	})
	if err != nil {
		t.Fatalf("Failed to marshal TlsCredentials: %v", err)
	}

	tests := []struct {
		name     string
		config   *anypb.Any
		resolver xdscreds.CertProviderConfigResolver
		wantErr  string
	}{
		{
			name:     "unmarshal_failure",
			config:   &anypb.Any{TypeUrl: tlsCredsTypeURL, Value: []byte{0xff}},
			resolver: bc,
			wantErr:  "failed to unmarshal TlsCredentials",
		},
		{
			name:     "missing_root_certificate_provider",
			config:   tlsCredsConfig(t, "", "identity-instance"),
			resolver: bc,
			wantErr:  "must specify root_certificate_provider",
		},
		{
			name:     "empty_identity_instance_name",
			config:   emptyIdentityInstance,
			resolver: bc,
			wantErr:  "identity_certificate_provider must specify an instance_name",
		},
		{
			name:     "unknown_root_instance",
			config:   tlsCredsConfig(t, "unknown-instance", ""),
			resolver: bc,
			wantErr:  `"unknown-instance" missing in bootstrap`,
		},
		{
			name:     "unknown_identity_instance",
			config:   tlsCredsConfig(t, "root-instance", "unknown-instance"),
			resolver: bc,
			wantErr:  `"unknown-instance" missing in bootstrap`,
		},
		{
			name:     "nil_resolver",
			config:   tlsCredsConfig(t, "root-instance", ""),
			resolver: nil,
			wantErr:  "no bootstrap configuration",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := xdscreds.GetChannelCredsBuilder(tlsCredsTypeURL)(tt.config, tt.resolver)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Build returned error %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

// startTestTLSServer starts a TLS server that performs one handshake per
// accepted connection. If mTLS is true, the server requires and verifies a
// client certificate.
func startTestTLSServer(t *testing.T, mTLS bool) net.Listener {
	t.Helper()

	serverCert, err := tls.LoadX509KeyPair(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		t.Fatalf("Failed to load server certificate: %v", err)
	}
	// gRPC's TLS credentials enforce ALPN, so the server must advertise h2.
	cfg := &tls.Config{Certificates: []tls.Certificate{serverCert}, NextProtos: []string{"h2"}}
	if mTLS {
		pem, err := os.ReadFile(testdata.Path("x509/client_ca_cert.pem"))
		if err != nil {
			t.Fatalf("Failed to read client CA certificate: %v", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			t.Fatal("Failed to parse client CA certificate")
		}
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
		cfg.ClientCAs = pool
	}

	lis, err := tls.Listen("tcp", "localhost:0", cfg)
	if err != nil {
		t.Fatalf("Failed to start test TLS server: %v", err)
	}
	t.Cleanup(func() { lis.Close() })
	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			go func() {
				conn.(*tls.Conn).Handshake()
				conn.Close()
			}()
		}
	}()
	return lis
}

// clientHandshake dials the test server and performs a client-side TLS
// handshake with credentials built from the given plugin config.
func clientHandshake(t *testing.T, config *anypb.Any, bc *bootstrap.Config) error {
	t.Helper()

	bundle, cleanup, err := xdscreds.GetChannelCredsBuilder(tlsCredsTypeURL)(config, bc)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	defer cleanup()

	mTLS := false
	var cfg tlscredspb.TlsCredentials
	if err := config.UnmarshalTo(&cfg); err == nil && cfg.GetIdentityCertificateProvider() != nil {
		mTLS = true
	}
	lis := startTestTLSServer(t, mTLS)

	rawConn, err := net.Dial("tcp", lis.Addr().String())
	if err != nil {
		t.Fatalf("Failed to dial test server: %v", err)
	}
	defer rawConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// The test server certificate is issued for *.test.example.com.
	_, _, err = bundle.TransportCredentials().ClientHandshake(ctx, "x.test.example.com", rawConn)
	return err
}

// Tests that TLS channel credentials backed by certificate provider instances
// complete a TLS handshake, with and without an identity certificate.
func (s) TestTLSCredsHandshake(t *testing.T) {
	bc := testBootstrapConfig(t)

	if err := clientHandshake(t, tlsCredsConfig(t, "root-instance", ""), bc); err != nil {
		t.Fatalf("ClientHandshake() with root-only TLS credentials failed: %v", err)
	}
	if err := clientHandshake(t, tlsCredsConfig(t, "root-instance", "identity-instance"), bc); err != nil {
		t.Fatalf("ClientHandshake() with mTLS credentials failed: %v", err)
	}
}

// Tests that closed TLS channel credentials fail handshakes, and that closing
// credentials that never performed a handshake is safe.
func (s) TestTLSCredsClose(t *testing.T) {
	bc := testBootstrapConfig(t)

	// Closing credentials whose providers were never instantiated must be a
	// no-op.
	bundle, cleanup, err := xdscreds.GetChannelCredsBuilder(tlsCredsTypeURL)(tlsCredsConfig(t, "root-instance", ""), bc)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	cleanup()

	// A handshake after close must fail.
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, _, err := bundle.TransportCredentials().ClientHandshake(ctx, "x.test.example.com", client); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("ClientHandshake() after close returned error %v, want error containing %q", err, "closed")
	}
}
