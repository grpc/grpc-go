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
	serverCa, err := os.ReadFile(testdata.Path("x509/server_ca_cert.pem"))
	if err != nil {
		t.Fatalf("failed to read test CA cert: %s", err)
	}

	// Write CA to a temporary file so that we can modify it later.
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

	// TLS server with a valid cert
	serverCreds, err := credentials.NewServerTLSFromFile(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		t.Fatalf("failed to generate server credentials: %v", err)
	}
	s := grpc.NewServer(grpc.Creds(serverCreds))
	testgrpc.RegisterTestServiceServer(s, &testServer{})
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("error listening: %v", err)
	}
	go s.Serve(lis)

	conn, err := grpc.Dial(
		lis.Addr().String(),
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
	// close the server so that we force a new handshake later on.
	s.Stop()

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

	s = grpc.NewServer(grpc.Creds(serverCreds))
	defer s.Stop()
	testgrpc.RegisterTestServiceServer(s, &testServer{})
	lis, err = net.Listen("tcp", lis.Addr().String())
	if err != nil {
		t.Fatalf("error listening: %v", err)
	}
	go s.Serve(lis)

	// Client handshake should fail because the server cert is signed by an
	// unknown CA.
	_, err = client.EmptyCall(context.Background(), &testpb.Empty{})
	if st, ok := status.FromError(err); !ok || st.Code() != codes.Unavailable {
		t.Errorf("expected unavailable error, got %v", err)
		if strings.Contains(st.Message(), "x509: certificate signed by unknown authority") {
			t.Errorf("expected error to contain 'x509: certificate signed by unknown authority', got %v", st.Message())
		}
	}
}

func TestMTLS(t *testing.T) {
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
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCA,
	}
	if err != nil {
		t.Fatalf("failed to generate server credentials: %v", err)
	}
	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLSCfg)))
	testgrpc.RegisterTestServiceServer(s, &testServer{})
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("error listening: %v", err)
	}
	go s.Serve(lis)
	defer s.Stop()

	conn, err := grpc.Dial(
		lis.Addr().String(),
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
}
