//go:build go1.27

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

package advancedtls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
	"google.golang.org/grpc/security/advancedtls/testdata"
	grpctestdata "google.golang.org/grpc/testdata"
)

func loadMLDSACertPool(t *testing.T, pemPaths ...string) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	for _, p := range pemPaths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("os.ReadFile(%q) failed: %v", p, err)
		}
		if !pool.AppendCertsFromPEM(b) {
			t.Fatalf("AppendCertsFromPEM(%q) failed", p)
		}
	}
	return pool
}

// TestMLDSAEnd2End tests server-authenticated TLS and mutual TLS (mTLS)
// handshakes and RPCs with advancedtls using ML-DSA-44, ML-DSA-65, ML-DSA-87,
// 2-certificate ML-DSA-87 chains, mixed ML-DSA variants, and combined classical
// RSA + ML-DSA root stores.
func (s) TestMLDSAEnd2End(t *testing.T) {
	tests := []struct {
		desc              string
		serverCertFile    string
		serverKeyFile     string
		serverRootFiles   []string
		requireClientCert bool
		clientCertFile    string
		clientKeyFile     string
		clientRootFiles   []string
	}{
		{
			desc:            "mldsa44_server_tls",
			serverCertFile:  grpctestdata.Path("x509/mldsa/server_mldsa44.pem"),
			serverKeyFile:   grpctestdata.Path("x509/mldsa/server_mldsa44.key"),
			clientRootFiles: []string{grpctestdata.Path("x509/mldsa/ca_mldsa44.pem")},
		},
		{
			desc:            "mldsa65_server_tls",
			serverCertFile:  grpctestdata.Path("x509/mldsa/server_mldsa65.pem"),
			serverKeyFile:   grpctestdata.Path("x509/mldsa/server_mldsa65.key"),
			clientRootFiles: []string{grpctestdata.Path("x509/mldsa/ca_mldsa65.pem")},
		},
		{
			desc:            "mldsa87_server_tls",
			serverCertFile:  grpctestdata.Path("x509/mldsa/server_mldsa87.pem"),
			serverKeyFile:   grpctestdata.Path("x509/mldsa/server_mldsa87.key"),
			clientRootFiles: []string{grpctestdata.Path("x509/mldsa/ca_mldsa87.pem")},
		},
		{
			desc:            "mldsa87_chain_server_tls",
			serverCertFile:  grpctestdata.Path("x509/mldsa/server_chain_mldsa87.pem"),
			serverKeyFile:   grpctestdata.Path("x509/mldsa/server_chain_mldsa87.key"),
			clientRootFiles: []string{grpctestdata.Path("x509/mldsa/ca_mldsa87.pem")},
		},
		{
			desc:              "mldsa44_mtls",
			serverCertFile:    grpctestdata.Path("x509/mldsa/server_mldsa44.pem"),
			serverKeyFile:     grpctestdata.Path("x509/mldsa/server_mldsa44.key"),
			serverRootFiles:   []string{grpctestdata.Path("x509/mldsa/ca_mldsa44.pem")},
			requireClientCert: true,
			clientCertFile:    grpctestdata.Path("x509/mldsa/client_mldsa44.pem"),
			clientKeyFile:     grpctestdata.Path("x509/mldsa/client_mldsa44.key"),
			clientRootFiles:   []string{grpctestdata.Path("x509/mldsa/ca_mldsa44.pem")},
		},
		{
			desc:              "mldsa65_mtls",
			serverCertFile:    grpctestdata.Path("x509/mldsa/server_mldsa65.pem"),
			serverKeyFile:     grpctestdata.Path("x509/mldsa/server_mldsa65.key"),
			serverRootFiles:   []string{grpctestdata.Path("x509/mldsa/ca_mldsa65.pem")},
			requireClientCert: true,
			clientCertFile:    grpctestdata.Path("x509/mldsa/client_mldsa65.pem"),
			clientKeyFile:     grpctestdata.Path("x509/mldsa/client_mldsa65.key"),
			clientRootFiles:   []string{grpctestdata.Path("x509/mldsa/ca_mldsa65.pem")},
		},
		{
			desc:              "mldsa87_mtls",
			serverCertFile:    grpctestdata.Path("x509/mldsa/server_mldsa87.pem"),
			serverKeyFile:     grpctestdata.Path("x509/mldsa/server_mldsa87.key"),
			serverRootFiles:   []string{grpctestdata.Path("x509/mldsa/ca_mldsa87.pem")},
			requireClientCert: true,
			clientCertFile:    grpctestdata.Path("x509/mldsa/client_mldsa87.pem"),
			clientKeyFile:     grpctestdata.Path("x509/mldsa/client_mldsa87.key"),
			clientRootFiles:   []string{grpctestdata.Path("x509/mldsa/ca_mldsa87.pem")},
		},
		{
			desc:              "mldsa87_chain_mtls",
			serverCertFile:    grpctestdata.Path("x509/mldsa/server_chain_mldsa87.pem"),
			serverKeyFile:     grpctestdata.Path("x509/mldsa/server_chain_mldsa87.key"),
			serverRootFiles:   []string{grpctestdata.Path("x509/mldsa/ca_mldsa87.pem")},
			requireClientCert: true,
			clientCertFile:    grpctestdata.Path("x509/mldsa/client_chain_mldsa87.pem"),
			clientKeyFile:     grpctestdata.Path("x509/mldsa/client_chain_mldsa87.key"),
			clientRootFiles:   []string{grpctestdata.Path("x509/mldsa/ca_mldsa87.pem")},
		},
		{
			desc:              "mixed_mldsa_certs_mtls",
			serverCertFile:    grpctestdata.Path("x509/mldsa/server_mldsa87.pem"),
			serverKeyFile:     grpctestdata.Path("x509/mldsa/server_mldsa87.key"),
			serverRootFiles:   []string{grpctestdata.Path("x509/mldsa/ca_mldsa44.pem")},
			requireClientCert: true,
			clientCertFile:    grpctestdata.Path("x509/mldsa/client_mldsa44.pem"),
			clientKeyFile:     grpctestdata.Path("x509/mldsa/client_mldsa44.key"),
			clientRootFiles:   []string{grpctestdata.Path("x509/mldsa/ca_mldsa87.pem")},
		},
		{
			desc:              "mixed_rsa_server_and_mldsa65_client_mtls",
			serverCertFile:    testdata.Path("server_cert_localhost_1.pem"),
			serverKeyFile:     testdata.Path("server_key_localhost_1.pem"),
			serverRootFiles:   []string{testdata.Path("client_trust_cert_1.pem"), grpctestdata.Path("x509/mldsa/ca_mldsa65.pem")},
			requireClientCert: true,
			clientCertFile:    grpctestdata.Path("x509/mldsa/client_mldsa65.pem"),
			clientKeyFile:     grpctestdata.Path("x509/mldsa/client_mldsa65.key"),
			clientRootFiles:   []string{testdata.Path("client_trust_cert_1.pem"), grpctestdata.Path("x509/mldsa/ca_mldsa65.pem")},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			srvCert, err := tls.LoadX509KeyPair(test.serverCertFile, test.serverKeyFile)
			if err != nil {
				t.Fatalf("LoadX509KeyPair(%q, %q) failed: %v", test.serverCertFile, test.serverKeyFile, err)
			}

			var cliCert tls.Certificate
			if test.clientCertFile != "" {
				cliCert, err = tls.LoadX509KeyPair(test.clientCertFile, test.clientKeyFile)
				if err != nil {
					t.Fatalf("LoadX509KeyPair(%q, %q) failed: %v", test.clientCertFile, test.clientKeyFile, err)
				}
			}

			serverOptions := &Options{
				IdentityOptions: IdentityCertificateOptions{
					Certificates: []tls.Certificate{srvCert},
				},
				RequireClientCert: test.requireClientCert,
				VerificationType:  CertAndHostVerification,
			}
			if test.requireClientCert {
				serverOptions.RootOptions = RootCertificateOptions{
					RootCertificates: loadMLDSACertPool(t, test.serverRootFiles...),
				}
				serverOptions.AdditionalPeerVerification = func(info *HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
					if !info.Leaf.Equal(cliCert.Leaf) {
						return nil, fmt.Errorf("client leaf cert = %v, want %v", info.Leaf, cliCert.Leaf)
					}
					return &PostHandshakeVerificationResults{}, nil
				}
			}

			serverTLSCreds, err := NewServerCreds(serverOptions)
			if err != nil {
				t.Fatalf("NewServerCreds() failed: %v", err)
			}
			s := grpc.NewServer(grpc.Creds(serverTLSCreds))
			defer s.Stop()
			lis, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("Failed to listen: %v", err)
			}
			defer lis.Close()
			addr := fmt.Sprintf("localhost:%v", lis.Addr().(*net.TCPAddr).Port)
			pb.RegisterGreeterServer(s, greeterServer{})
			go s.Serve(lis)

			clientOptions := &Options{
				RootOptions: RootCertificateOptions{
					RootCertificates: loadMLDSACertPool(t, test.clientRootFiles...),
				},
				VerificationType: CertAndHostVerification,
				AdditionalPeerVerification: func(info *HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
					if !info.Leaf.Equal(srvCert.Leaf) {
						return nil, fmt.Errorf("server leaf cert = %v, want %v", info.Leaf, srvCert.Leaf)
					}
					return &PostHandshakeVerificationResults{}, nil
				},
			}
			if test.clientCertFile != "" {
				clientOptions.IdentityOptions = IdentityCertificateOptions{
					Certificates: []tls.Certificate{cliCert},
				}
			}

			clientTLSCreds, err := NewClientCreds(clientOptions)
			if err != nil {
				t.Fatalf("NewClientCreds() failed: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			conn, _, err := callAndVerifyWithClientConn(ctx, addr, "mldsa rpc", clientTLSCreds, false)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
		})
	}
}

// TestMLDSAPEMFileProviderEnd2End tests advancedtls with pemfile.NewProvider
// using ML-DSA credentials and verifying credential rotation from ML-DSA-44 to
// ML-DSA-87.
func (s) TestMLDSAPEMFileProviderEnd2End(t *testing.T) {
	tmpFiles, err := createTmpFiles()
	if err != nil {
		t.Fatalf("createTmpFiles() failed: %v", err)
	}
	defer tmpFiles.removeFiles()

	if err := copyFileContents(grpctestdata.Path("x509/mldsa/client_mldsa44.pem"), tmpFiles.clientCertTmp.Name()); err != nil {
		t.Fatal(err)
	}
	if err := copyFileContents(grpctestdata.Path("x509/mldsa/client_mldsa44.key"), tmpFiles.clientKeyTmp.Name()); err != nil {
		t.Fatal(err)
	}
	if err := copyFileContents(grpctestdata.Path("x509/mldsa/ca_mldsa44.pem"), tmpFiles.clientTrustTmp.Name()); err != nil {
		t.Fatal(err)
	}
	if err := copyFileContents(grpctestdata.Path("x509/mldsa/server_mldsa44.pem"), tmpFiles.serverCertTmp.Name()); err != nil {
		t.Fatal(err)
	}
	if err := copyFileContents(grpctestdata.Path("x509/mldsa/server_mldsa44.key"), tmpFiles.serverKeyTmp.Name()); err != nil {
		t.Fatal(err)
	}
	if err := copyFileContents(grpctestdata.Path("x509/mldsa/ca_mldsa44.pem"), tmpFiles.serverTrustTmp.Name()); err != nil {
		t.Fatal(err)
	}

	clientIdentityProvider, clientRootProvider, serverIdentityProvider, serverRootProvider, err := createProviders(tmpFiles)
	if err != nil {
		t.Fatalf("createProviders() failed: %v", err)
	}
	defer clientIdentityProvider.Close()
	defer clientRootProvider.Close()
	defer serverIdentityProvider.Close()
	defer serverRootProvider.Close()

	serverOptions := &Options{
		IdentityOptions: IdentityCertificateOptions{
			IdentityProvider: serverIdentityProvider,
		},
		RootOptions: RootCertificateOptions{
			RootProvider: serverRootProvider,
		},
		RequireClientCert: true,
		VerificationType:  CertAndHostVerification,
	}
	serverTLSCreds, err := NewServerCreds(serverOptions)
	if err != nil {
		t.Fatalf("NewServerCreds() failed: %v", err)
	}
	s := grpc.NewServer(grpc.Creds(serverTLSCreds))
	defer s.Stop()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer lis.Close()
	addr := fmt.Sprintf("localhost:%v", lis.Addr().(*net.TCPAddr).Port)
	pb.RegisterGreeterServer(s, greeterServer{})
	go s.Serve(lis)

	clientOptions := &Options{
		IdentityOptions: IdentityCertificateOptions{
			IdentityProvider: clientIdentityProvider,
		},
		RootOptions: RootCertificateOptions{
			RootProvider: clientRootProvider,
		},
		VerificationType: CertAndHostVerification,
	}
	clientTLSCreds, err := NewClientCreds(clientOptions)
	if err != nil {
		t.Fatalf("NewClientCreds() failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Initial connection with ML-DSA-44 credentials should succeed.
	conn, _, err := callAndVerifyWithClientConn(ctx, addr, "rpc 1", clientTLSCreds, false)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Rotate server identity to ML-DSA-87 chain while client still trusts only ML-DSA-44 CA.
	if err := copyFileContents(grpctestdata.Path("x509/mldsa/server_chain_mldsa87.pem"), tmpFiles.serverCertTmp.Name()); err != nil {
		t.Fatal(err)
	}
	if err := copyFileContents(grpctestdata.Path("x509/mldsa/server_chain_mldsa87.key"), tmpFiles.serverKeyTmp.Name()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(sleepInterval)

	// New connection should fail until client trust root is updated to ML-DSA-87 CA.
	conn, _, err = callAndVerifyWithClientConn(ctx, addr, "rpc 2", clientTLSCreds, true)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Update client trust root to ML-DSA-87 CA; connection should succeed again.
	if err := copyFileContents(grpctestdata.Path("x509/mldsa/ca_mldsa87.pem"), tmpFiles.clientTrustTmp.Name()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(sleepInterval)

	conn, _, err = callAndVerifyWithClientConn(ctx, addr, "rpc 3", clientTLSCreds, false)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
}
