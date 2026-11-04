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

package credentials_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/testdata"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

func loadCertPool(t *testing.T, pemFiles ...string) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	for _, f := range pemFiles {
		b, err := os.ReadFile(testdata.Path(f))
		if err != nil {
			t.Fatalf("os.ReadFile(%q) failed: %v", f, err)
		}
		if !pool.AppendCertsFromPEM(b) {
			t.Fatalf("AppendCertsFromPEM(%q) failed", f)
		}
	}
	return pool
}

func loadCertChain(t *testing.T, pemFile string) []*x509.Certificate {
	t.Helper()
	b, err := os.ReadFile(testdata.Path(pemFile))
	if err != nil {
		t.Fatalf("os.ReadFile(%q) failed: %v", pemFile, err)
	}
	var certs []*x509.Certificate
	for len(b) > 0 {
		var block *pem.Block
		block, b = pem.Decode(b)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("x509.ParseCertificate(%q) failed: %v", pemFile, err)
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		t.Fatalf("No certificates found in %q", pemFile)
	}
	return certs
}

// TestTLS_MLDSA_ServerAuth tests server-authenticated TLS 1.3 handshakes and
// unary RPCs using ML-DSA-44, ML-DSA-65, ML-DSA-87, and a 2-certificate
// ML-DSA-87 chain.
func (s) TestTLS_MLDSA_ServerAuth(t *testing.T) {
	tests := []struct {
		name           string
		serverCertFile string
		serverKeyFile  string
		caCertFile     string
	}{
		{
			name:           "mldsa44",
			serverCertFile: "x509/mldsa/server_mldsa44.pem",
			serverKeyFile:  "x509/mldsa/server_mldsa44.key",
			caCertFile:     "x509/mldsa/ca_mldsa44.pem",
		},
		{
			name:           "mldsa65",
			serverCertFile: "x509/mldsa/server_mldsa65.pem",
			serverKeyFile:  "x509/mldsa/server_mldsa65.key",
			caCertFile:     "x509/mldsa/ca_mldsa65.pem",
		},
		{
			name:           "mldsa87",
			serverCertFile: "x509/mldsa/server_mldsa87.pem",
			serverKeyFile:  "x509/mldsa/server_mldsa87.key",
			caCertFile:     "x509/mldsa/ca_mldsa87.pem",
		},
		{
			name:           "mldsa87_chain",
			serverCertFile: "x509/mldsa/server_chain_mldsa87.pem",
			serverKeyFile:  "x509/mldsa/server_chain_mldsa87.key",
			caCertFile:     "x509/mldsa/ca_mldsa87.pem",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			serverCreds, err := credentials.NewServerTLSFromFile(testdata.Path(tc.serverCertFile), testdata.Path(tc.serverKeyFile))
			if err != nil {
				t.Fatalf("NewServerTLSFromFile(%q, %q) failed: %v", tc.serverCertFile, tc.serverKeyFile, err)
			}
			clientCreds, err := credentials.NewClientTLSFromFile(testdata.Path(tc.caCertFile), serverName)
			if err != nil {
				t.Fatalf("NewClientTLSFromFile(%q, %q) failed: %v", tc.caCertFile, serverName, err)
			}

			ss := stubserver.StubServer{
				EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
					return &testpb.Empty{}, nil
				},
			}
			if err := ss.Start([]grpc.ServerOption{grpc.Creds(serverCreds)}, grpc.WithTransportCredentials(clientCreds)); err != nil {
				t.Fatalf("Error starting stub server: %v", err)
			}
			defer ss.Stop()

			var serverPeer peer.Peer
			if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&serverPeer)); err != nil {
				t.Fatalf("EmptyCall err = %v; want <nil>", err)
			}
			tlsInfo, ok := serverPeer.AuthInfo.(credentials.TLSInfo)
			if !ok {
				t.Fatalf("serverPeer.AuthInfo = %T, want credentials.TLSInfo", serverPeer.AuthInfo)
			}
			if tlsInfo.State.Version != tls.VersionTLS13 {
				t.Errorf("Negotiated TLS version = 0x%x, want 0x%x (TLS 1.3)", tlsInfo.State.Version, tls.VersionTLS13)
			}
			wantServerCerts := loadCertChain(t, tc.serverCertFile)
			if diff := cmp.Diff(wantServerCerts, tlsInfo.State.PeerCertificates); diff != "" {
				t.Errorf("Server PeerCertificates diff (-want +got):\n%s", diff)
			}
		})
	}
}

// TestTLS_MLDSA_MutualTLS tests mutual TLS (mTLS) 1.3 handshakes and unary RPCs
// using homogeneous ML-DSA-44, ML-DSA-65, ML-DSA-87, 2-certificate ML-DSA-87
// chains, mixed ML-DSA variants (ML-DSA-87 server + ML-DSA-44 client), and a
// combined classical RSA + ML-DSA-65 root store.
func (s) TestTLS_MLDSA_MutualTLS(t *testing.T) {
	tests := []struct {
		name            string
		serverCertFile  string
		serverKeyFile   string
		serverRootFiles []string
		clientCertFile  string
		clientKeyFile   string
		clientRootFiles []string
	}{
		{
			name:            "mldsa44",
			serverCertFile:  "x509/mldsa/server_mldsa44.pem",
			serverKeyFile:   "x509/mldsa/server_mldsa44.key",
			serverRootFiles: []string{"x509/mldsa/ca_mldsa44.pem"},
			clientCertFile:  "x509/mldsa/client_mldsa44.pem",
			clientKeyFile:   "x509/mldsa/client_mldsa44.key",
			clientRootFiles: []string{"x509/mldsa/ca_mldsa44.pem"},
		},
		{
			name:            "mldsa65",
			serverCertFile:  "x509/mldsa/server_mldsa65.pem",
			serverKeyFile:   "x509/mldsa/server_mldsa65.key",
			serverRootFiles: []string{"x509/mldsa/ca_mldsa65.pem"},
			clientCertFile:  "x509/mldsa/client_mldsa65.pem",
			clientKeyFile:   "x509/mldsa/client_mldsa65.key",
			clientRootFiles: []string{"x509/mldsa/ca_mldsa65.pem"},
		},
		{
			name:            "mldsa87",
			serverCertFile:  "x509/mldsa/server_mldsa87.pem",
			serverKeyFile:   "x509/mldsa/server_mldsa87.key",
			serverRootFiles: []string{"x509/mldsa/ca_mldsa87.pem"},
			clientCertFile:  "x509/mldsa/client_mldsa87.pem",
			clientKeyFile:   "x509/mldsa/client_mldsa87.key",
			clientRootFiles: []string{"x509/mldsa/ca_mldsa87.pem"},
		},
		{
			name:            "mldsa87_chain",
			serverCertFile:  "x509/mldsa/server_chain_mldsa87.pem",
			serverKeyFile:   "x509/mldsa/server_chain_mldsa87.key",
			serverRootFiles: []string{"x509/mldsa/ca_mldsa87.pem"},
			clientCertFile:  "x509/mldsa/client_chain_mldsa87.pem",
			clientKeyFile:   "x509/mldsa/client_chain_mldsa87.key",
			clientRootFiles: []string{"x509/mldsa/ca_mldsa87.pem"},
		},
		{
			name:            "mixed_mldsa_server87_client44",
			serverCertFile:  "x509/mldsa/server_mldsa87.pem",
			serverKeyFile:   "x509/mldsa/server_mldsa87.key",
			serverRootFiles: []string{"x509/mldsa/ca_mldsa87.pem", "x509/mldsa/ca_mldsa44.pem"},
			clientCertFile:  "x509/mldsa/client_mldsa44.pem",
			clientKeyFile:   "x509/mldsa/client_mldsa44.key",
			clientRootFiles: []string{"x509/mldsa/ca_mldsa87.pem", "x509/mldsa/ca_mldsa44.pem"},
		},
		{
			name:            "mixed_rsa_server_mldsa65_client",
			serverCertFile:  "x509/server1_cert.pem",
			serverKeyFile:   "x509/server1_key.pem",
			serverRootFiles: []string{"x509/server_ca_cert.pem", "x509/mldsa/ca_mldsa65.pem"},
			clientCertFile:  "x509/mldsa/client_mldsa65.pem",
			clientKeyFile:   "x509/mldsa/client_mldsa65.key",
			clientRootFiles: []string{"x509/server_ca_cert.pem", "x509/mldsa/ca_mldsa65.pem"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			srvCert, err := tls.LoadX509KeyPair(testdata.Path(tc.serverCertFile), testdata.Path(tc.serverKeyFile))
			if err != nil {
				t.Fatalf("LoadX509KeyPair(%q, %q) failed: %v", tc.serverCertFile, tc.serverKeyFile, err)
			}
			cliCert, err := tls.LoadX509KeyPair(testdata.Path(tc.clientCertFile), testdata.Path(tc.clientKeyFile))
			if err != nil {
				t.Fatalf("LoadX509KeyPair(%q, %q) failed: %v", tc.clientCertFile, tc.clientKeyFile, err)
			}

			wantServerCerts := loadCertChain(t, tc.serverCertFile)
			wantClientCerts := loadCertChain(t, tc.clientCertFile)

			serverCreds := credentials.NewTLS(&tls.Config{
				Certificates: []tls.Certificate{srvCert},
				ClientAuth:   tls.RequireAndVerifyClientCert,
				ClientCAs:    loadCertPool(t, tc.serverRootFiles...),
			})
			clientCreds := credentials.NewTLS(&tls.Config{
				ServerName:   serverName,
				Certificates: []tls.Certificate{cliCert},
				RootCAs:      loadCertPool(t, tc.clientRootFiles...),
			})

			ss := stubserver.StubServer{
				EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
					p, ok := peer.FromContext(ctx)
					if !ok {
						return nil, status.Error(codes.Internal, "missing peer in server context")
					}
					tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
					if !ok {
						return nil, status.Errorf(codes.Internal, "unexpected AuthInfo type %T", p.AuthInfo)
					}
					if diff := cmp.Diff(wantClientCerts, tlsInfo.State.PeerCertificates); diff != "" {
						return nil, status.Errorf(codes.Internal, "client PeerCertificates diff (-want +got):\n%s", diff)
					}
					return &testpb.Empty{}, nil
				},
			}
			if err := ss.Start([]grpc.ServerOption{grpc.Creds(serverCreds)}, grpc.WithTransportCredentials(clientCreds)); err != nil {
				t.Fatalf("Error starting stub server: %v", err)
			}
			defer ss.Stop()

			var serverPeer peer.Peer
			if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&serverPeer)); err != nil {
				t.Fatalf("EmptyCall err = %v; want <nil>", err)
			}
			tlsInfo, ok := serverPeer.AuthInfo.(credentials.TLSInfo)
			if !ok {
				t.Fatalf("serverPeer.AuthInfo = %T, want credentials.TLSInfo", serverPeer.AuthInfo)
			}
			if diff := cmp.Diff(wantServerCerts, tlsInfo.State.PeerCertificates); diff != "" {
				t.Errorf("Server PeerCertificates diff (-want +got):\n%s", diff)
			}
		})
	}
}

// TestTLS_MLDSA_ChainSessionResumption tests TLS 1.3 session resumption with
// 2-certificate ML-DSA-87 server and client chains.
func (s) TestTLS_MLDSA_ChainSessionResumption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	srvCert, err := tls.LoadX509KeyPair(testdata.Path("x509/mldsa/server_chain_mldsa87.pem"), testdata.Path("x509/mldsa/server_chain_mldsa87.key"))
	if err != nil {
		t.Fatalf("LoadX509KeyPair(server_chain_mldsa87) failed: %v", err)
	}
	cliCert, err := tls.LoadX509KeyPair(testdata.Path("x509/mldsa/client_chain_mldsa87.pem"), testdata.Path("x509/mldsa/client_chain_mldsa87.key"))
	if err != nil {
		t.Fatalf("LoadX509KeyPair(client_chain_mldsa87) failed: %v", err)
	}
	rootPool := loadCertPool(t, "x509/mldsa/ca_mldsa87.pem")

	serverCreds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{srvCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    rootPool,
	})
	ss := stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	if err := ss.StartServer(grpc.Creds(serverCreds)); err != nil {
		t.Fatalf("Error starting server: %v", err)
	}
	defer ss.Stop()

	sessionCache := tls.NewLRUClientSessionCache(16)
	clientCreds := credentials.NewTLS(&tls.Config{
		ServerName:         serverName,
		Certificates:       []tls.Certificate{cliCert},
		RootCAs:            rootPool,
		ClientSessionCache: sessionCache,
	})

	for i, wantResume := range []bool{false, true} {
		cc, err := grpc.NewClient(ss.Address, grpc.WithTransportCredentials(clientCreds))
		if err != nil {
			t.Fatalf("Iteration %d: grpc.NewClient error: %v", i, err)
		}
		client := testgrpc.NewTestServiceClient(cc)
		var serverPeer peer.Peer
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&serverPeer)); err != nil {
			cc.Close()
			t.Fatalf("Iteration %d: EmptyCall err = %v; want <nil>", i, err)
		}
		tlsInfo, ok := serverPeer.AuthInfo.(credentials.TLSInfo)
		if !ok {
			cc.Close()
			t.Fatalf("Iteration %d: serverPeer.AuthInfo = %T, want credentials.TLSInfo", i, serverPeer.AuthInfo)
		}
		if tlsInfo.State.DidResume != wantResume {
			t.Errorf("Iteration %d: DidResume = %v, want %v", i, tlsInfo.State.DidResume, wantResume)
		}
		cc.Close()
	}
}

// TestTLS_MLDSA_RequiresTLS13 verifies that ML-DSA certificates cannot be used
// when the TLS version is capped at TLS 1.2.
func (s) TestTLS_MLDSA_RequiresTLS13(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	srvCert, err := tls.LoadX509KeyPair(testdata.Path("x509/mldsa/server_mldsa65.pem"), testdata.Path("x509/mldsa/server_mldsa65.key"))
	if err != nil {
		t.Fatalf("LoadX509KeyPair(server_mldsa65) failed: %v", err)
	}
	rootPool := loadCertPool(t, "x509/mldsa/ca_mldsa65.pem")

	serverCreds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{srvCert},
	})
	clientCreds := credentials.NewTLS(&tls.Config{
		ServerName: serverName,
		RootCAs:    rootPool,
		MaxVersion: tls.VersionTLS12,
	})

	ss := stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	if err := ss.StartServer(grpc.Creds(serverCreds)); err != nil {
		t.Fatalf("Error starting server: %v", err)
	}
	defer ss.Stop()

	cc, err := grpc.NewClient(ss.Address, grpc.WithTransportCredentials(clientCreds))
	if err != nil {
		t.Fatalf("grpc.NewClient error: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	const wantStr = "authentication handshake failed"
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.Unavailable || !strings.Contains(err.Error(), wantStr) {
		t.Fatalf("EmptyCall err = %v; want code=%v, message contains %q", err, codes.Unavailable, wantStr)
	}
}
