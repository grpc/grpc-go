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
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/testdata"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

func TestValidTlsBuilder(t *testing.T) {
	tests := []string{
		`{}`,
		`{"ca_certificate_file": "foo"}`,
		`{"certificate_file":"bar","private_key_file":"baz"}`,
		`{"ca_certificate_file":"foo","certificate_file":"bar","private_key_file":"baz"}`,
		`{"refresh_interval": "1s"}`,
		`{"refresh_interval": "1s","ca_certificate_file": "foo"}`,
		`{"refresh_interval": "1s","certificate_file":"bar","private_key_file":"baz"}`,
		`{"refresh_interval": "1s","ca_certificate_file":"foo","certificate_file":"bar","private_key_file":"baz"}`,
	}

	for _, jd := range tests {
		t.Run(jd, func(t *testing.T) {
			msg := json.RawMessage(jd)
			if _, err := NewBundle(msg); err != nil {
				t.Errorf("expected no error but got: %s", err)
			}
		})
	}
}

func TestInvalidTlsBuilder(t *testing.T) {
	tests := []struct {
		jd, err string
	}{
		{`{"ca_certificate_file": 1}`, "json: cannot unmarshal number into Go struct field .ca_certificate_file of type string"},
		{`{"certificate_file":"bar"}`, "pemfile: private key file and identity cert file should be both specified or not specified"},
	}

	for _, test := range tests {
		t.Run(test.jd, func(t *testing.T) {
			msg := json.RawMessage(test.jd)
			if _, err := NewBundle(msg); err.Error() != test.err {
				t.Errorf("expected: %s, got: %s", test.err, err)
			}
		})
	}
}

type testServer struct {
	testgrpc.UnimplementedTestServiceServer
}

func (t testServer) EmptyCall(_ context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
	return &testpb.Empty{}, nil
}

func TestCaReloading(t *testing.T) {
	srvAddr, stopSrv := startServer(t, "localhost:0", tls.NoClientCert)

	serverCa, err := os.ReadFile(testdata.Path("x509/server_ca_cert.pem"))
	if err != nil {
		t.Fatalf("failed to read test CA cert: %s", err)
	}

	// Write CA certs to a temporary file so that we can modify it later.
	caPath := t.TempDir() + "/ca.pem"
	err = os.WriteFile(caPath, serverCa, 0644)
	if err != nil {
		t.Fatalf("failed to write test CA cert: %v", err)
	}
	cfg := fmt.Sprintf(`{
		"ca_certificate_file": "%s",
		"refresh_interval": ".01s"
	}`, caPath)
	tlsBundle, err := NewBundle([]byte(cfg))
	if err != nil {
		t.Fatalf("failed to create TLS bundle: %v", err)
	}

	conn, err := grpc.Dial(
		srvAddr.String(),
		grpc.WithCredentialsBundle(tlsBundle),
		grpc.WithAuthority("x.test.example.com"),
	)
	if err != nil {
		t.Fatalf("error dialing: %v", err)
	}
	defer conn.Close()
	client := testgrpc.NewTestServiceClient(conn)
	_, err = client.EmptyCall(context.Background(), &testpb.Empty{})
	if err != nil {
		t.Errorf("error calling EmptyCall: %v", err)
	}
	// close the server and create a new one to force client to do a new
	// handshake.
	stopSrv()

	invalidCa, err := os.ReadFile(testdata.Path("ca.pem"))
	if err != nil {
		t.Fatalf("failed to read test CA cert: %v", err)
	}
	// unload root cert
	err = os.WriteFile(caPath, invalidCa, 0644)
	if err != nil {
		t.Fatalf("failed to write test CA cert: %v", err)
	}

	// Leave time for the file_watcher provider to reload the CA.
	time.Sleep(100 * time.Millisecond)

	_, stopFunc := startServer(t, srvAddr.String(), tls.NoClientCert)
	defer stopFunc()

	// Client handshake should fail because the server cert is signed by an
	// unknown CA.
	_, err = client.EmptyCall(context.Background(), &testpb.Empty{})
	if st, ok := status.FromError(err); !ok || st.Code() != codes.Unavailable {
		t.Errorf("expected unavailable error, got %v", err)
	} else if !strings.Contains(st.Message(), "certificate signed by unknown authority") {
		t.Errorf("expected error to contain 'certificate signed by unknown authority', got %v", st.Message())
	}
}

func TestMTLS(t *testing.T) {
	srvAddr, stopFunc := startServer(t, "localhost:0", tls.RequireAndVerifyClientCert)
	defer stopFunc()

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
		t.Fatalf("failed to create TLS bundle: %v", err)
	}
	dialOpts := []grpc.DialOption{
		grpc.WithCredentialsBundle(tlsBundle),
		grpc.WithAuthority("x.test.example.com"),
	}

	t.Run("ValidClientCert", func(t *testing.T) {
		conn, err := grpc.Dial(srvAddr.String(), dialOpts...)
		if err != nil {
			t.Fatalf("error dialing: %v", err)
		}
		client := testgrpc.NewTestServiceClient(conn)

		_, err = client.EmptyCall(context.Background(), &testpb.Empty{})
		if err != nil {
			t.Errorf("error calling EmptyCall: %v", err)
		}
		conn.Close()
	})

	t.Run("Provider failing", func(t *testing.T) {
		// Check that if the provider returns an errors, we fail the handshake.
		// It's not easy to trigger this condition, so we rely on closing the
		// provider.
		creds, ok := tlsBundle.TransportCredentials().(*reloadingCreds)

		// Force the provider to be initialized. The test is flaky otherwise,
		// since close may be a noop.
		_, _ = creds.provider.KeyMaterial(context.Background())

		if !ok {
			t.Fatalf("expected reloadingCreds, got %T", tlsBundle.TransportCredentials())
		}

		creds.provider.Close()

		conn, err := grpc.Dial(srvAddr.String(), dialOpts...)
		if err != nil {
			t.Fatalf("error dialing: %v", err)
		}
		client := testgrpc.NewTestServiceClient(conn)
		_, err = client.EmptyCall(context.Background(), &testpb.Empty{})
		if st, ok := status.FromError(err); !ok || st.Code() != codes.Unavailable {
			t.Errorf("expected unavailable error, got %v", err)
		} else if !strings.Contains(st.Message(), "provider instance is closed") {
			t.Errorf("expected error to contain 'provider instance is closed', got %v", st.Message())
		}
		conn.Close()
	})
}

type stopFunc func()

func startServer(t *testing.T, addr string, clientAuth tls.ClientAuthType) (net.Addr, stopFunc) {
	// Create a TLS server with a valid cert that requires a client cert.
	serverCert, err := tls.LoadX509KeyPair(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		t.Fatalf("failed to load server cert: %v", err)
	}
	pemClientCA, err := os.ReadFile(testdata.Path("x509/client_ca_cert.pem"))
	if err != nil {
		t.Fatalf("failed to read test client CA cert: %v", err)
	}
	clientCA := x509.NewCertPool()
	if !clientCA.AppendCertsFromPEM(pemClientCA) {
		t.Fatal("failed to add client CA's certificate")
	}
	serverTLSCfg := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   clientAuth,
		ClientCAs:    clientCA,
	}
	if err != nil {
		t.Fatalf("failed to generate server credentials: %v", err)
	}
	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLSCfg)))
	testgrpc.RegisterTestServiceServer(s, &testServer{})
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("error listening: %v", err)
	}
	go s.Serve(lis)
	return lis.Addr(), func() { s.Stop() }
}
