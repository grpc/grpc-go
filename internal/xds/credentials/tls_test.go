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
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
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

// testBootstrapConfig returns a bootstrap config with these certificate
// provider instances:
//   - "root-instance": the CA certificate that issued x509/server1_cert.pem.
//   - "identity-instance": an x509 client certificate and key.
//   - "spiffe-root-instance": a SPIFFE bundle map with the CA that issued
//     spiffe_end2end/server_spiffe.pem, for trust domain example.com.
//   - "spiffe-identity-instance": a SPIFFE client certificate and key.
func testBootstrapConfig(t *testing.T) *bootstrap.Config {
	t.Helper()

	// The file_watcher plugin ignores SPIFFE bundle maps unless this is
	// set.
	testutils.SetEnvConfig(t, &envconfig.XDSSPIFFEEnabled, true)

	rootCert := testdata.Path("x509/server_ca_cert.pem")
	clientCert := testdata.Path("x509/client1_cert.pem")
	clientKey := testdata.Path("x509/client1_key.pem")
	spiffeBundleMap := testdata.Path("spiffe_end2end/client_spiffebundle.json")
	spiffeClientCert := testdata.Path("spiffe_end2end/client_spiffe.pem")
	spiffeClientKey := testdata.Path("spiffe_end2end/client.key")

	contents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: json.RawMessage(`[{"server_uri": "passthrough:///unused", "channel_creds": [{"type": "insecure"}]}]`),
		Node:    json.RawMessage(`{"id": "test-node"}`),
		CertificateProviders: map[string]json.RawMessage{
			"root-instance": json.RawMessage(fmt.Sprintf(`{
				"plugin_name": "file_watcher",
				"config": {"ca_certificate_file": %q}
			}`, rootCert)),
			"identity-instance": json.RawMessage(fmt.Sprintf(`{
				"plugin_name": "file_watcher",
				"config": {"certificate_file": %q, "private_key_file": %q}
			}`, clientCert, clientKey)),
			"spiffe-root-instance": json.RawMessage(fmt.Sprintf(`{
				"plugin_name": "file_watcher",
				"config": {"spiffe_trust_bundle_map_file": %q}
			}`, spiffeBundleMap)),
			"spiffe-identity-instance": json.RawMessage(fmt.Sprintf(`{
				"plugin_name": "file_watcher",
				"config": {"certificate_file": %q, "private_key_file": %q}
			}`, spiffeClientCert, spiffeClientKey)),
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

// startTestTLSServer starts a TLS server with the given certificate and
// returns its address. It accepts one connection and reports the result of the
// server-side handshake on the returned channel. If clientCAFile is set, the
// server requires a client certificate issued by that CA.
func startTestTLSServer(t *testing.T, certFile, keyFile, clientCAFile string) (string, <-chan error) {
	t.Helper()

	serverCert, err := tls.LoadX509KeyPair(testdata.Path(certFile), testdata.Path(keyFile))
	if err != nil {
		t.Fatalf("Failed to load server certificate: %v", err)
	}
	// gRPC's TLS credentials enforce ALPN, so the server must advertise h2.
	cfg := &tls.Config{Certificates: []tls.Certificate{serverCert}, NextProtos: []string{"h2"}}
	if clientCAFile != "" {
		pem, err := os.ReadFile(testdata.Path(clientCAFile))
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
	stop := make(chan struct{})
	done := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		lis.Close()
		<-done
	})
	handshakeErr := make(chan error, 1)
	go func() {
		defer close(done)
		conn, err := lis.Accept()
		if err != nil {
			handshakeErr <- err
			return
		}
		defer conn.Close()
		handshakeErr <- conn.(*tls.Conn).Handshake()
		<-stop
	}()
	return lis.Addr().String(), handshakeErr
}

// clientHandshake performs a client-side TLS handshake with the server at
// addr, using credentials built from the given plugin config. The connection
// stays open until the test ends, so that the server can finish its handshake.
func clientHandshake(ctx context.Context, t *testing.T, config *anypb.Any, bc *bootstrap.Config, addr, authority string) error {
	t.Helper()

	bundle, cleanup, err := xdscreds.GetChannelCredsBuilder(tlsCredsTypeURL)(config, bc)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	defer cleanup()

	rawConn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to dial test server: %v", err)
	}
	t.Cleanup(func() { rawConn.Close() })
	_, _, err = bundle.TransportCredentials().ClientHandshake(ctx, authority, rawConn)
	return err
}

// Tests that TLS channel credentials verify the server certificate using the
// CA certificates or the SPIFFE bundle map from the root certificate provider,
// and present the identity certificate, if configured.
func (s) TestTLSCredsHandshake(t *testing.T) {
	bc := testBootstrapConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	tests := []struct {
		name       string
		root       string // Root certificate provider instance.
		identity   string // Identity certificate provider instance.
		serverCert string
		serverKey  string
		// If set, the server requires a client certificate issued by
		// this CA.
		clientCA  string
		authority string
		wantErr   string // Empty if the handshake must succeed.
	}{
		{
			// The server certificate is valid for
			// *.test.example.com.
			name:       "ca_roots",
			root:       "root-instance",
			serverCert: "x509/server1_cert.pem",
			serverKey:  "x509/server1_key.pem",
			authority:  "x.test.example.com",
		},
		{
			name:       "ca_roots_mtls",
			root:       "root-instance",
			identity:   "identity-instance",
			serverCert: "x509/server1_cert.pem",
			serverKey:  "x509/server1_key.pem",
			clientCA:   "x509/client_ca_cert.pem",
			authority:  "x.test.example.com",
		},
		{
			name:       "ca_roots_hostname_mismatch",
			root:       "root-instance",
			serverCert: "x509/server1_cert.pem",
			serverKey:  "x509/server1_key.pem",
			authority:  "foo.test.google.fr",
			wantErr:    "certificate is valid for",
		},
		{
			name:       "no_root_certificates",
			root:       "identity-instance",
			serverCert: "x509/server1_cert.pem",
			serverKey:  "x509/server1_key.pem",
			authority:  "x.test.example.com",
			wantErr:    "neither root certificates nor a SPIFFE bundle map",
		},
		{
			// The server certificate is valid for *.test.google.fr
			// and 192.168.1.3.
			name:       "spiffe",
			root:       "spiffe-root-instance",
			serverCert: "spiffe_end2end/server_spiffe.pem",
			serverKey:  "spiffe_end2end/server.key",
			authority:  "foo.test.google.fr:443",
		},
		{
			name:       "spiffe_mtls",
			root:       "spiffe-root-instance",
			identity:   "spiffe-identity-instance",
			serverCert: "spiffe_end2end/server_spiffe.pem",
			serverKey:  "spiffe_end2end/server.key",
			clientCA:   "spiffe_end2end/ca.pem",
			authority:  "foo.test.google.fr:443",
		},
		{
			name:       "spiffe_authority_without_port",
			root:       "spiffe-root-instance",
			serverCert: "spiffe_end2end/server_spiffe.pem",
			serverKey:  "spiffe_end2end/server.key",
			authority:  "foo.test.google.fr",
		},
		{
			name:       "spiffe_hostname_mismatch",
			root:       "spiffe-root-instance",
			serverCert: "spiffe_end2end/server_spiffe.pem",
			serverKey:  "spiffe_end2end/server.key",
			authority:  "x.test.example.com:443",
			wantErr:    "certificate is valid for",
		},
		{
			name:       "spiffe_ip_authority",
			root:       "spiffe-root-instance",
			serverCert: "spiffe_end2end/server_spiffe.pem",
			serverKey:  "spiffe_end2end/server.key",
			authority:  "192.168.1.3:443",
		},
		{
			// IP addresses are not sent as SNI, but must still be
			// verified.
			name:       "spiffe_ip_authority_mismatch",
			root:       "spiffe-root-instance",
			serverCert: "spiffe_end2end/server_spiffe.pem",
			serverKey:  "spiffe_end2end/server.key",
			authority:  "127.0.0.1:443",
			wantErr:    "certificate is valid for",
		},
		{
			// An empty server name must not disable the hostname
			// check.
			name:       "spiffe_empty_authority",
			root:       "spiffe-root-instance",
			serverCert: "spiffe_end2end/server_spiffe.pem",
			serverKey:  "spiffe_end2end/server.key",
			authority:  "",
			wantErr:    "has no server name to verify",
		},
		{
			name:       "spiffe_authority_without_host",
			root:       "spiffe-root-instance",
			serverCert: "spiffe_end2end/server_spiffe.pem",
			serverKey:  "spiffe_end2end/server.key",
			authority:  ":443",
			wantErr:    "has no server name to verify",
		},
		{
			// The server certificate has a SPIFFE ID in trust
			// domain example.com, but is issued by a different CA.
			name:       "spiffe_untrusted_server_certificate",
			root:       "spiffe-root-instance",
			serverCert: "spiffe/server1_spiffe.pem",
			serverKey:  "server1.key",
			authority:  "foo.test.google.fr:443",
			wantErr:    "certificate signed by unknown authority",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, serverErrCh := startTestTLSServer(t, tt.serverCert, tt.serverKey, tt.clientCA)
			err := clientHandshake(ctx, t, tlsCredsConfig(t, tt.root, tt.identity), bc, addr, tt.authority)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ClientHandshake() returned error %v, want error containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ClientHandshake() failed: %v", err)
			}
			// With TLS 1.3, the client finishes its handshake
			// before the server verifies the client certificate.
			select {
			case err := <-serverErrCh:
				if err != nil {
					t.Fatalf("Server handshake failed: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("Timed out waiting for the server handshake")
			}
		})
	}
}

// Tests that closed TLS channel credentials fail handshakes.
func (s) TestTLSCredsClose(t *testing.T) {
	bc := testBootstrapConfig(t)

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
