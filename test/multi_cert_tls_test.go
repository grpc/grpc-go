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

package test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/testdata"
)

// loadTestCert loads a tls.Certificate from the specified testdata paths.
func loadTestCert(t *testing.T, certFile, keyFile string) tls.Certificate {
	t.Helper()
	cert, err := tls.LoadX509KeyPair(testdata.Path(certFile), testdata.Path(keyFile))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(%q, %q) failed: %v", certFile, keyFile, err)
	}
	return cert
}

// loadCertPool loads a certificate pool from the specified testdata path.
func loadCertPool(t *testing.T, caFile string) *x509.CertPool {
	t.Helper()
	data, err := os.ReadFile(testdata.Path(caFile))
	if err != nil {
		t.Fatalf("os.ReadFile(%q) failed: %v", caFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		t.Fatalf("AppendCertsFromPEM failed for %q", caFile)
	}
	return pool
}

// TestServerMultipleCerts_TLS13_Negotiation tests end-to-end gRPC communication
// where both server and client strictly enforce TLS 1.3, the server possesses
// both RSA and ECDSA certificate chains with the same SNI (*.test.example.com),
// and certificate selection operates via SupportsCertificate and the signature_algorithms
// TLS extension without depending on TLS 1.2 CipherSuites.
func (s) TestServerMultipleCerts_TLS13_Negotiation(t *testing.T) {
	rsaCert := loadTestCert(t, "x509/server1_cert.pem", "x509/server1_key.pem")
	ecdsaCert := loadTestCert(t, "x509/server_ecdsa_cert.pem", "x509/server_ecdsa_key.pem")
	caPool := loadCertPool(t, "x509/server_ca_cert.pem")

	testCases := []struct {
		desc         string
		serverConfig *tls.Config
	}{
		{
			desc: "Server configured with [RSA, ECDSA] certificates in TLS 1.3",
			serverConfig: &tls.Config{
				Certificates: []tls.Certificate{rsaCert, ecdsaCert},
				MinVersion:   tls.VersionTLS13,
				MaxVersion:   tls.VersionTLS13,
			},
		},
		{
			desc: "Server configured with reversed [ECDSA, RSA] certificates in TLS 1.3",
			serverConfig: &tls.Config{
				Certificates: []tls.Certificate{ecdsaCert, rsaCert},
				MinVersion:   tls.VersionTLS13,
				MaxVersion:   tls.VersionTLS13,
			},
		},
		{
			desc: "Server configured with GetCertificate callback evaluating SupportsCertificate in TLS 1.3",
			serverConfig: &tls.Config{
				MinVersion: tls.VersionTLS13,
				MaxVersion: tls.VersionTLS13,
				GetCertificate: func(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
					for _, c := range []*tls.Certificate{&ecdsaCert, &rsaCert} {
						if err := chi.SupportsCertificate(c); err == nil {
							return c, nil
						}
					}
					return &rsaCert, nil
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			serverCreds := credentials.NewTLS(tc.serverConfig)
			s := grpc.NewServer(grpc.Creds(serverCreds))
			defer s.Stop()

			testgrpc.RegisterTestServiceServer(s, &testServer{})
			lis, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("net.Listen failed: %v", err)
			}
			defer lis.Close()
			go s.Serve(lis)

			addr := lis.Addr().String()

			clientCreds := credentials.NewTLS(&tls.Config{
				RootCAs:    caPool,
				ServerName: "x.test.example.com",
				MinVersion: tls.VersionTLS13,
				MaxVersion: tls.VersionTLS13,
			})
			conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(clientCreds), grpc.WithAuthority("x.test.example.com"), grpc.WithDisableServiceConfig())
			if err != nil {
				t.Fatalf("grpc.NewClient failed: %v", err)
			}
			defer conn.Close()

			client := testgrpc.NewTestServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			var p peer.Peer
			if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&p)); err != nil {
				t.Fatalf("Client EmptyCall failed in TLS 1.3: %v", err)
			}

			tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
			if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
				t.Fatalf("Failed to retrieve TLSInfo or peer certificates: %v", p.AuthInfo)
			}
			if tlsInfo.State.Version != tls.VersionTLS13 {
				t.Errorf("negotiated TLS version = %x, want %x (TLS 1.3)", tlsInfo.State.Version, tls.VersionTLS13)
			}
			negotiatedAlgo := tlsInfo.State.PeerCertificates[0].PublicKeyAlgorithm
			if negotiatedAlgo != x509.RSA && negotiatedAlgo != x509.ECDSA {
				t.Errorf("negotiated certificate algorithm = %v, want RSA or ECDSA", negotiatedAlgo)
			}
		})
	}
}

// TestServerMultipleCerts_TLS13_MutualTLS tests mTLS end-to-end strictly in TLS 1.3
// where the server has dual RSA and ECDSA certificates and requires client certificates.
func (s) TestServerMultipleCerts_TLS13_MutualTLS(t *testing.T) {
	rsaServerCert := loadTestCert(t, "x509/server1_cert.pem", "x509/server1_key.pem")
	ecdsaServerCert := loadTestCert(t, "x509/server_ecdsa_cert.pem", "x509/server_ecdsa_key.pem")
	serverCAPool := loadCertPool(t, "x509/server_ca_cert.pem")
	clientCAPool := loadCertPool(t, "x509/client_ca_cert.pem")

	rsaClientCert := loadTestCert(t, "x509/client1_cert.pem", "x509/client1_key.pem")
	ecdsaClientCert := loadTestCert(t, "x509/client_ecdsa_cert.pem", "x509/client_ecdsa_key.pem")

	var lastClientCertAlgo x509.PublicKeyAlgorithm
	var lastTLSVersion uint16
	unaryInterceptor := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if p, ok := peer.FromContext(ctx); ok {
			if tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo); ok && len(tlsInfo.State.PeerCertificates) > 0 {
				lastClientCertAlgo = tlsInfo.State.PeerCertificates[0].PublicKeyAlgorithm
				lastTLSVersion = tlsInfo.State.Version
			}
		}
		return handler(ctx, req)
	}

	serverCreds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{rsaServerCert, ecdsaServerCert},
		ClientCAs:    clientCAPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
		MaxVersion:   tls.VersionTLS13,
	})
	s := grpc.NewServer(grpc.Creds(serverCreds), grpc.UnaryInterceptor(unaryInterceptor))
	defer s.Stop()

	testgrpc.RegisterTestServiceServer(s, &testServer{})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	defer lis.Close()
	go s.Serve(lis)

	addr := lis.Addr().String()

	// 1. RSA client in TLS 1.3
	{
		clientCreds := credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{rsaClientCert},
			RootCAs:      serverCAPool,
			ServerName:   "x.test.example.com",
			MinVersion:   tls.VersionTLS13,
			MaxVersion:   tls.VersionTLS13,
		})
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(clientCreds), grpc.WithAuthority("x.test.example.com"), grpc.WithDisableServiceConfig())
		if err != nil {
			t.Fatalf("grpc.NewClient failed: %v", err)
		}
		defer conn.Close()

		client := testgrpc.NewTestServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()

		var p peer.Peer
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&p)); err != nil {
			t.Fatalf("RSA client EmptyCall failed in TLS 1.3: %v", err)
		}

		tlsInfo := p.AuthInfo.(credentials.TLSInfo)
		if tlsInfo.State.Version != tls.VersionTLS13 {
			t.Errorf("negotiated TLS version = %x, want %x (TLS 1.3)", tlsInfo.State.Version, tls.VersionTLS13)
		}
		if lastTLSVersion != tls.VersionTLS13 {
			t.Errorf("server observed TLS version = %x, want %x (TLS 1.3)", lastTLSVersion, tls.VersionTLS13)
		}
		if lastClientCertAlgo != x509.RSA {
			t.Errorf("client certificate algorithm verified by server = %v, want %v (x509.RSA)", lastClientCertAlgo, x509.RSA)
		}
	}

	// 2. ECDSA client in TLS 1.3
	{
		clientCreds := credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{ecdsaClientCert},
			RootCAs:      serverCAPool,
			ServerName:   "x.test.example.com",
			MinVersion:   tls.VersionTLS13,
			MaxVersion:   tls.VersionTLS13,
		})
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(clientCreds), grpc.WithAuthority("x.test.example.com"), grpc.WithDisableServiceConfig())
		if err != nil {
			t.Fatalf("grpc.NewClient failed: %v", err)
		}
		defer conn.Close()

		client := testgrpc.NewTestServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()

		var p peer.Peer
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&p)); err != nil {
			t.Fatalf("ECDSA client EmptyCall failed in TLS 1.3: %v", err)
		}

		tlsInfo := p.AuthInfo.(credentials.TLSInfo)
		if tlsInfo.State.Version != tls.VersionTLS13 {
			t.Errorf("negotiated TLS version = %x, want %x (TLS 1.3)", tlsInfo.State.Version, tls.VersionTLS13)
		}
		if lastTLSVersion != tls.VersionTLS13 {
			t.Errorf("server observed TLS version = %x, want %x (TLS 1.3)", lastTLSVersion, tls.VersionTLS13)
		}
		if lastClientCertAlgo != x509.ECDSA {
			t.Errorf("client certificate algorithm verified by server = %v, want %v (x509.ECDSA)", lastClientCertAlgo, x509.ECDSA)
		}
	}
}
