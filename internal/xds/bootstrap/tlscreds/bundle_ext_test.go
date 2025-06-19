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

package tlscreds_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/bootstrap/tlscreds"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/testdata"
)

const defaultTestTimeout = 5 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type Closable interface {
	Close()
}

func (s) TestValidTlsBuilder(t *testing.T) {
	caCert := testdata.Path("x509/server_ca_cert.pem")
	clientCert := testdata.Path("x509/client1_cert.pem")
	clientKey := testdata.Path("x509/client1_key.pem")
	clientSpiffeBundle := testdata.Path("spiffe_end2end/client_spiffebundle.json")
	tests := []struct {
		name string
		jd   string
	}{
		{
			name: "Absent configuration",
			jd:   `null`,
		},
		{
			name: "Empty configuration",
			jd:   `{}`,
		},
		{
			name: "Only CA certificate chain",
			jd:   fmt.Sprintf(`{"ca_certificate_file": "%s"}`, caCert),
		},
		{
			name: "Only private key and certificate chain",
			jd:   fmt.Sprintf(`{"certificate_file":"%s","private_key_file":"%s"}`, clientCert, clientKey),
		},
		{
			name: "CA chain, private key and certificate chain",
			jd:   fmt.Sprintf(`{"ca_certificate_file":"%s","certificate_file":"%s","private_key_file":"%s"}`, caCert, clientCert, clientKey),
		},
		{
			name: "Only refresh interval", jd: `{"refresh_interval": "1s"}`,
		},
		{
			name: "Refresh interval and CA certificate chain",
			jd:   fmt.Sprintf(`{"refresh_interval": "1s","ca_certificate_file": "%s"}`, caCert),
		},
		{
			name: "Refresh interval, private key and certificate chain",
			jd:   fmt.Sprintf(`{"refresh_interval": "1s","certificate_file":"%s","private_key_file":"%s"}`, clientCert, clientKey),
		},
		{
			name: "Refresh interval, CA chain, private key and certificate chain",
			jd:   fmt.Sprintf(`{"refresh_interval": "1s","ca_certificate_file":"%s","certificate_file":"%s","private_key_file":"%s"}`, caCert, clientCert, clientKey),
		},
		{
			name: "Refresh interval, CA chain, private key, certificate chain, spiffe bundle",
			jd:   fmt.Sprintf(`{"refresh_interval": "1s","ca_certificate_file":"%s","certificate_file":"%s","private_key_file":"%s","spiffe_trust_bundle_map_file":"%s"}`, caCert, clientCert, clientKey, clientSpiffeBundle),
		},
		{
			name: "Unknown field",
			jd:   `{"unknown_field": "foo"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			msg := json.RawMessage(test.jd)
			_, stop, err := tlscreds.NewBundle(msg)
			if err != nil {
				t.Fatalf("NewBundle(%s) returned error %s when expected to succeed", test.jd, err)
			}
			stop()
		})
	}
}

func (s) TestInvalidTlsBuilder(t *testing.T) {
	tests := []struct {
		name, jd, wantErrPrefix string
	}{
		{
			name:          "Wrong type in json",
			jd:            `{"ca_certificate_file": 1}`,
			wantErrPrefix: "failed to unmarshal config:"},
		{
			name:          "Missing private key",
			jd:            fmt.Sprintf(`{"certificate_file":"%s"}`, testdata.Path("x509/server_cert.pem")),
			wantErrPrefix: "pemfile: private key file and identity cert file should be both specified or not specified",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			msg := json.RawMessage(test.jd)
			_, stop, err := tlscreds.NewBundle(msg)
			if err == nil || !strings.HasPrefix(err.Error(), test.wantErrPrefix) {
				if stop != nil {
					stop()
				}
				t.Fatalf("NewBundle(%s): got error %s, want an error with prefix %s", msg, err, test.wantErrPrefix)
			}
		})
	}
}

func (s) TestCaReloading(t *testing.T) {
	serverCa, err := os.ReadFile(testdata.Path("x509/server_ca_cert.pem"))
	if err != nil {
		t.Fatalf("Failed to read test CA cert: %s", err)
	}

	// Write CA certs to a temporary file so that we can modify it later.
	caPath := t.TempDir() + "/ca.pem"
	if err = os.WriteFile(caPath, serverCa, 0644); err != nil {
		t.Fatalf("Failed to write test CA cert: %v", err)
	}
	cfg := fmt.Sprintf(`{
		"ca_certificate_file": "%s",
		"refresh_interval": ".01s"
	}`, caPath)
	tlsBundle, stop, err := tlscreds.NewBundle([]byte(cfg))
	if err != nil {
		t.Fatalf("Failed to create TLS bundle: %v", err)
	}
	defer stop()

	serverCredentials := grpc.Creds(testutils.CreateServerTLSCredentials(t, tls.NoClientCert))
	server := stubserver.StartTestService(t, nil, serverCredentials)

	conn, err := grpc.NewClient(
		server.Address,
		grpc.WithCredentialsBundle(tlsBundle),
		grpc.WithAuthority("x.test.example.com"),
	)
	if err != nil {
		t.Fatalf("Error dialing: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(conn)
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Errorf("Error calling EmptyCall: %v", err)
	}
	// close the server and create a new one to force client to do a new
	// handshake.
	server.Stop()

	invalidCa, err := os.ReadFile(testdata.Path("ca.pem"))
	if err != nil {
		t.Fatalf("Failed to read test CA cert: %v", err)
	}
	// unload root cert
	err = os.WriteFile(caPath, invalidCa, 0644)
	if err != nil {
		t.Fatalf("Failed to write test CA cert: %v", err)
	}
	for ; ctx.Err() == nil; <-time.After(10 * time.Millisecond) {
		ss := stubserver.StubServer{
			Address:    server.Address,
			EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
		}
		server = stubserver.StartTestService(t, &ss, serverCredentials)

		// Client handshake should eventually fail because the client CA was
		// reloaded, and thus the server cert is signed by an unknown CA.
		t.Log(server)
		_, err = client.EmptyCall(ctx, &testpb.Empty{})
		const wantErr = "certificate signed by unknown authority"
		if status.Code(err) == codes.Unavailable && strings.Contains(err.Error(), wantErr) {
			// Certs have reloaded.
			server.Stop()
			break
		}
		t.Logf("EmptyCall() got err: %s, want code: %s, want err: %s", err, codes.Unavailable, wantErr)
		server.Stop()
	}
	if ctx.Err() != nil {
		t.Errorf("Timed out waiting for CA certs reloading")
	}
}

// Test_SPIFFE_Reloading sets up a client and server. The client is configured
// to use a SPIFFE bundle map, and the server is configured to use TLS creds
// compatible with this bundle. A handshake is performed and connection is
// expected to be successful. Then we change the client's SPIFFE Bundle Map file
// on disk to one that should fail with the server's credentials. This change
// should be picked up by the client via our file reloading. Another handshake
// is performed and checked for failure, ensuring that gRPC is correctly using
// the changed-on-disk bundle map.
func (s) Test_SPIFFE_Reloading(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSSPIFFEEnabled, true)
	clientSPIFFEBundle, err := os.ReadFile(testdata.Path("spiffe_end2end/client_spiffebundle.json"))
	if err != nil {
		t.Fatalf("Failed to read test SPIFFE bundle: %v", err)
	}

	// Write CA certs to a temporary file so that we can modify it later.
	spiffePath := t.TempDir() + "/client_spiffe.json"
	if err = os.WriteFile(spiffePath, clientSPIFFEBundle, 0644); err != nil {
		t.Fatalf("Failed to write test SPIFFE Bundle %v: %v", clientSPIFFEBundle, err)
	}
	cfg := fmt.Sprintf(`{
		"spiffe_trust_bundle_map_file": "%s",
		"refresh_interval": ".01s"
	}`, spiffePath)
	tlsBundle, stop, err := tlscreds.NewBundle([]byte(cfg))
	if err != nil {
		t.Fatalf("Failed to create TLS bundle: %v", err)
	}
	defer stop()

	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)
	defer lis.Close()
	ss := stubserver.StubServer{
		Listener:   lis,
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
	}

	serverCredentials := grpc.Creds(testutils.CreateServerTLSCredentialsCompatibleWithSPIFFE(t, tls.NoClientCert))
	server := stubserver.StartTestService(t, &ss, serverCredentials)

	defer server.Stop()

	conn, err := grpc.NewClient(
		server.Address,
		grpc.WithCredentialsBundle(tlsBundle),
		grpc.WithAuthority("x.test.example.com"),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", server.Address, err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	client := testgrpc.NewTestServiceClient(conn)
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Errorf("Error calling EmptyCall: %v", err)
	}

	// Setup the wrong bundle to be reloaded
	wrongBundle, err := os.ReadFile(testdata.Path("spiffe_end2end/server_spiffebundle.json"))
	if err != nil {
		t.Fatalf("Failed to read test spiffe bundle %v: %v", "spiffe_end2end/server_spiffebundle.json", err)
	}
	// Write the bundle that will fail to the tmp file path to be reloaded
	err = os.WriteFile(spiffePath, wrongBundle, 0644)
	if err != nil {
		t.Fatalf("Failed to write test spiffe bundle %v: %v", "spiffe_end2end/server_spiffebundle.json", err)
	}

	for ; ctx.Err() == nil; <-time.After(10 * time.Millisecond) {
		// Stop and restart the listener to force new handshakes
		lis.Stop()
		lis.Restart()
		// Client handshake should eventually fail because the client CA was
		// reloaded, and thus the server cert is signed by an unknown CA.
		t.Log(server)
		_, err = client.EmptyCall(ctx, &testpb.Empty{})
		const wantErr = "no bundle found for peer certificates trust domain"
		if status.Code(err) == codes.Unavailable && strings.Contains(err.Error(), wantErr) {
			// Certs have reloaded.
			server.Stop()
			break
		}
		t.Logf("EmptyCall() got err: %s, want code: %s, want err: %s", err, codes.Unavailable, wantErr)
	}
	if ctx.Err() != nil {
		t.Errorf("Timed out waiting for CA certs reloading")
	}
}

func (s) TestMTLS(t *testing.T) {
	s := stubserver.StartTestService(t, nil, grpc.Creds(testutils.CreateServerTLSCredentials(t, tls.RequireAndVerifyClientCert)))
	defer s.Stop()

	cfg := fmt.Sprintf(`{
		"ca_certificate_file": "%s",
		"certificate_file": "%s",
		"private_key_file": "%s"
	}`,
		testdata.Path("x509/server_ca_cert.pem"),
		testdata.Path("x509/client1_cert.pem"),
		testdata.Path("x509/client1_key.pem"))
	tlsBundle, stop, err := tlscreds.NewBundle([]byte(cfg))
	if err != nil {
		t.Fatalf("Failed to create TLS bundle: %v", err)
	}
	defer stop()
	conn, err := grpc.NewClient(s.Address, grpc.WithCredentialsBundle(tlsBundle), grpc.WithAuthority("x.test.example.com"))
	if err != nil {
		t.Fatalf("Error dialing: %v", err)
	}
	defer conn.Close()
	client := testgrpc.NewTestServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Errorf("EmptyCall(): got error %v when expected to succeed", err)
	}
}

// Test_MTLS_SPIFFE configures a client and server. The server has a certificate
// chain that is compatible with the client's configured SPIFFE bundle map. An
// MTLS connection is attempted between the two and checked for success.
func (s) Test_MTLS_SPIFFE(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSSPIFFEEnabled, true)
	tests := []struct {
		name         string
		serverOption grpc.ServerOption
	}{
		{
			name:         "MTLS SPIFFE",
			serverOption: grpc.Creds(testutils.CreateServerTLSCredentialsCompatibleWithSPIFFE(t, tls.RequireAndVerifyClientCert)),
		},
		{
			name:         "MTLS SPIFFE Chain",
			serverOption: grpc.Creds(testutils.CreateServerTLSCredentialsCompatibleWithSPIFFEChain(t, tls.RequireAndVerifyClientCert)),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := stubserver.StartTestService(t, nil, tc.serverOption)
			defer s.Stop()

			cfg := fmt.Sprintf(`{
	"certificate_file": "%s",
	"private_key_file": "%s",
	"spiffe_trust_bundle_map_file": "%s"
}`,
				testdata.Path("spiffe_end2end/client_spiffe.pem"),
				testdata.Path("spiffe_end2end/client.key"),
				testdata.Path("spiffe_end2end/client_spiffebundle.json"))
			tlsBundle, stop, err := tlscreds.NewBundle([]byte(cfg))
			if err != nil {
				t.Fatalf("Failed to create TLS bundle: %v", err)
			}
			defer stop()
			conn, err := grpc.NewClient(s.Address, grpc.WithCredentialsBundle(tlsBundle), grpc.WithAuthority("x.test.example.com"))
			if err != nil {
				t.Fatalf("Error dialing: %v", err)
			}
			defer conn.Close()
			client := testgrpc.NewTestServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if _, err = client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
				t.Errorf("EmptyCall(): got error %v when expected to succeed", err)
			}
		})
	}
}

// Test_MTLS_SPIFFE_FlagDisabled configures a client and server. The server has
// a certificate chain that is compatible with the client's configured SPIFFE
// bundle map. However, the XDS flag that enabled SPIFFE usage is disabled. An
// MTLS connection is attempted between the two and checked for failure.
func (s) Test_MTLS_SPIFFE_FlagDisabled(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSSPIFFEEnabled, false)
	serverOption := grpc.Creds(testutils.CreateServerTLSCredentialsCompatibleWithSPIFFE(t, tls.RequireAndVerifyClientCert))
	s := stubserver.StartTestService(t, nil, serverOption)
	defer s.Stop()

	cfg := fmt.Sprintf(`{
"certificate_file": "%s",
"private_key_file": "%s",
"spiffe_trust_bundle_map_file": "%s"
}`,
		testdata.Path("spiffe_end2end/client_spiffe.pem"),
		testdata.Path("spiffe_end2end/client.key"),
		testdata.Path("spiffe_end2end/client_spiffebundle.json"))
	tlsBundle, stop, err := tlscreds.NewBundle([]byte(cfg))
	if err != nil {
		t.Fatalf("Failed to create TLS bundle: %v", err)
	}
	defer stop()
	conn, err := grpc.NewClient(s.Address, grpc.WithCredentialsBundle(tlsBundle), grpc.WithAuthority("x.test.example.com"))
	if err != nil {
		t.Fatalf("Error dialing: %v", err)
	}
	defer conn.Close()
	client := testgrpc.NewTestServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}); err == nil {
		t.Errorf("EmptyCall(): got success want failure")
	}
}

func (s) Test_MTLS_SPIFFE_Failure(t *testing.T) {
	testutils.SetEnvConfig(t, &envconfig.XDSSPIFFEEnabled, true)
	tests := []struct {
		name             string
		certFile         string
		keyFile          string
		spiffeBundleFile string
		serverOption     grpc.ServerOption
		wantErrContains  string
		wantErrCode      codes.Code
	}{
		{
			name:             "No matching trust domain in bundle",
			certFile:         "spiffe_end2end/client_spiffe.pem",
			keyFile:          "spiffe_end2end/client.key",
			spiffeBundleFile: "spiffe_end2end/server_spiffebundle.json",
			serverOption:     grpc.Creds(testutils.CreateServerTLSCredentialsCompatibleWithSPIFFE(t, tls.RequireAndVerifyClientCert)),
			wantErrContains:  "spiffe: no bundle found for peer certificates",
			wantErrCode:      codes.Unavailable,
		},
		{
			name:             "Server cert has no valid SPIFFE URIs",
			certFile:         "spiffe_end2end/client_spiffe.pem",
			keyFile:          "spiffe_end2end/client.key",
			spiffeBundleFile: "spiffe_end2end/client_spiffebundle.json",
			serverOption:     grpc.Creds(testutils.CreateServerTLSCredentials(t, tls.RequireAndVerifyClientCert)),
			wantErrContains:  "spiffe: could not get spiffe ID from peer leaf cert",
			wantErrCode:      codes.Unavailable,
		},
		{
			name:             "Server cert has valid spiffe ID but doesn't chain to the root CA",
			certFile:         "spiffe_end2end/client_spiffe.pem",
			keyFile:          "spiffe_end2end/client.key",
			spiffeBundleFile: "spiffe_end2end/client_spiffebundle.json",
			serverOption:     grpc.Creds(testutils.CreateServerTLSCredentialsValidSPIFFEButWrongCA(t, tls.RequireAndVerifyClientCert)),
			wantErrContains:  "spiffe: x509 certificate Verify failed: x509: certificate signed by unknown authority",
			wantErrCode:      codes.Unavailable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := stubserver.StartTestService(t, nil, tc.serverOption)
			defer s.Stop()
			cfg := fmt.Sprintf(`{
"certificate_file": "%s",
"private_key_file": "%s",
"spiffe_trust_bundle_map_file": "%s"
}`,
				testdata.Path(tc.certFile),
				testdata.Path(tc.keyFile),
				testdata.Path(tc.spiffeBundleFile))
			tlsBundle, stop, err := tlscreds.NewBundle([]byte(cfg))
			if err != nil {
				t.Fatalf("Failed to create TLS bundle: %v", err)
			}
			defer stop()
			conn, err := grpc.NewClient(s.Address, grpc.WithCredentialsBundle(tlsBundle), grpc.WithAuthority("x.test.example.com"))
			if err != nil {
				t.Fatalf("grpc.NewClient(%q) failed: %v", s.Address, err)
			}
			defer conn.Close()
			client := testgrpc.NewTestServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if _, err = client.EmptyCall(ctx, &testpb.Empty{}); err == nil {
				t.Errorf("EmptyCall(): got success. want failure")
			}
			if status.Code(err) != tc.wantErrCode {
				t.Errorf("EmptyCall(): failed with wrong error. got code %v. want code: %v", status.Code(err), tc.wantErrCode)
			}
			if !strings.Contains(err.Error(), tc.wantErrContains) {
				t.Errorf("EmptyCall(): failed with wrong error. got %v. want contains: %v", err, tc.wantErrContains)
			}
		})
	}
}
