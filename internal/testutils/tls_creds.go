/*
 *
 * Copyright 2024 gRPC authors.
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
 */

package testutils

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"testing"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/testdata"
)

// CreateClientTLSCredentials creates client-side TLS transport credentials
// using certificate and key files from testdata/x509 directory.
func CreateClientTLSCredentials(t *testing.T) credentials.TransportCredentials {
	t.Helper()

	cert, err := tls.LoadX509KeyPair(testdata.Path("x509/client1_cert.pem"), testdata.Path("x509/client1_key.pem"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(x509/client1_cert.pem, x509/client1_key.pem) failed: %v", err)
	}
	b, err := os.ReadFile(testdata.Path("x509/server_ca_cert.pem"))
	if err != nil {
		t.Fatalf("os.ReadFile(x509/server_ca_cert.pem) failed: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(b) {
		t.Fatal("Failed to append certificates")
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      roots,
		ServerName:   "x.test.example.com",
	})
}

// CreateServerTLSCredentials creates server-side TLS transport credentials
// using certificate and key files from testdata/x509 directory.
func CreateServerTLSCredentials(t *testing.T, clientAuth tls.ClientAuthType) credentials.TransportCredentials {
	t.Helper()

	cert, err := tls.LoadX509KeyPair(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(x509/server1_cert.pem, x509/server1_key.pem) failed: %v", err)
	}
	b, err := os.ReadFile(testdata.Path("x509/client_ca_cert.pem"))
	if err != nil {
		t.Fatalf("os.ReadFile(x509/client_ca_cert.pem) failed: %v", err)
	}
	ca := x509.NewCertPool()
	if !ca.AppendCertsFromPEM(b) {
		t.Fatal("Failed to append certificates")
	}
	return credentials.NewTLS(&tls.Config{
		ClientAuth:   clientAuth,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    ca,
	})
}

// CreateServerTLSCredentialsCompatibleWithSPIFFE creates server-side TLS
// transport credentials using certificate and key files from the
// testdata/spiffe_end2end directory. These credentials are compatible with the
// SPIFFE trust bundles used on the client side.
func CreateServerTLSCredentialsCompatibleWithSPIFFE(t *testing.T, clientAuth tls.ClientAuthType) credentials.TransportCredentials {
	t.Helper()

	cert, err := tls.LoadX509KeyPair(testdata.Path("spiffe_end2end/server_spiffe.pem"), testdata.Path("spiffe_end2end/server.key"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(spiffe_end2end/server_spiffe.pem, spiffe_end2end/server.key) failed: %v", err)
	}
	b, err := os.ReadFile(testdata.Path("spiffe_end2end/ca.pem"))
	if err != nil {
		t.Fatalf("os.ReadFile(spiffe_end2end/ca.pem) failed: %v", err)
	}
	ca := x509.NewCertPool()
	if !ca.AppendCertsFromPEM(b) {
		t.Fatal("Failed to append certificates")
	}
	return credentials.NewTLS(&tls.Config{
		ClientAuth:   clientAuth,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    ca,
	})
}

// CreateServerTLSCredentialsCompatibleWithSPIFFEChain creates server-side TLS
// transport credentials using a certificate chain and key files from the
// testdata/spiffe_end2end directory. These credentials are compatible with the
// SPIFFE trust bundles used on the client side.
func CreateServerTLSCredentialsCompatibleWithSPIFFEChain(t *testing.T, clientAuth tls.ClientAuthType) credentials.TransportCredentials {
	t.Helper()

	certs, err := tls.LoadX509KeyPair(testdata.Path("spiffe_end2end/leaf_and_intermediate_chain.pem"), testdata.Path("spiffe_end2end/leaf_signed_by_intermediate.key"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(spiffe_end2end/leaf_and_intermediate_chain.pem, spiffe_end2end/leaf_signed_by_intermediate.key) failed: %v", err)
	}
	b, err := os.ReadFile(testdata.Path("spiffe_end2end/ca.pem"))
	if err != nil {
		t.Fatalf("os.ReadFile(spiffe_end2end/ca.pem) failed: %v", err)
	}
	ca := x509.NewCertPool()
	if !ca.AppendCertsFromPEM(b) {
		t.Fatal("Failed to append certificates")
	}
	return credentials.NewTLS(&tls.Config{
		ClientAuth:   clientAuth,
		Certificates: []tls.Certificate{certs},
		ClientCAs:    ca,
	})
}

// CreateServerTLSCredentialsValidSPIFFEButWrongCA creates server-side TLS
// transport credentials using certificate and key files from the
// testdata/spiffe directory rather than the testdata/spiffe_end2end directory.
// These credentials have the expected trust domains and SPIFFE IDs that are
// compatible with testdata/spiffe_end2end client files, but they are signed by
// a different CA and will thus fail the connection.
func CreateServerTLSCredentialsValidSPIFFEButWrongCA(t *testing.T, clientAuth tls.ClientAuthType) credentials.TransportCredentials {
	t.Helper()

	cert, err := tls.LoadX509KeyPair(testdata.Path("spiffe/server1_spiffe.pem"), testdata.Path("server1.key"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(spiffe/server1_spiffe.pem, spiffe/server.key) failed: %v", err)
	}
	b, err := os.ReadFile(testdata.Path("spiffe_end2end/ca.pem"))
	if err != nil {
		t.Fatalf("os.ReadFile(spiffe_end2end/ca.pem) failed: %v", err)
	}
	ca := x509.NewCertPool()
	if !ca.AppendCertsFromPEM(b) {
		t.Fatal("Failed to append certificates")
	}
	return credentials.NewTLS(&tls.Config{
		ClientAuth:   clientAuth,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    ca,
	})
}
