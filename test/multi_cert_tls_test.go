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
		desc               string
		serverConfig       *tls.Config
		wantNegotiatedAlgo x509.PublicKeyAlgorithm
	}{
		{
			desc: "Server configured with [RSA, ECDSA] certificates in TLS 1.3",
			serverConfig: &tls.Config{
				Certificates: []tls.Certificate{rsaCert, ecdsaCert},
				MinVersion:   tls.VersionTLS13,
				MaxVersion:   tls.VersionTLS13,
			},
			wantNegotiatedAlgo: x509.RSA,
		},
		{
			desc: "Server configured with reversed [ECDSA, RSA] certificates in TLS 1.3",
			serverConfig: &tls.Config{
				Certificates: []tls.Certificate{ecdsaCert, rsaCert},
				MinVersion:   tls.VersionTLS13,
				MaxVersion:   tls.VersionTLS13,
			},
			wantNegotiatedAlgo: x509.ECDSA,
		},
		{
			desc: "Server configured with GetCertificate callback preferring ECDSA in TLS 1.3",
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
			wantNegotiatedAlgo: x509.ECDSA,
		},
		{
			desc: "Server configured with GetCertificate callback preferring RSA in TLS 1.3",
			serverConfig: &tls.Config{
				MinVersion: tls.VersionTLS13,
				MaxVersion: tls.VersionTLS13,
				GetCertificate: func(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
					for _, c := range []*tls.Certificate{&rsaCert, &ecdsaCert} {
						if err := chi.SupportsCertificate(c); err == nil {
							return c, nil
						}
					}
					return &ecdsaCert, nil
				},
			},
			wantNegotiatedAlgo: x509.RSA,
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
			if negotiatedAlgo != tc.wantNegotiatedAlgo {
				t.Errorf("negotiated certificate algorithm = %v, want %v", negotiatedAlgo, tc.wantNegotiatedAlgo)
			}
		})
	}
}

// TestServerMultipleCerts_TLS12_Negotiation tests end-to-end gRPC communication
// in TLS 1.2 where the server has both RSA and ECDSA certificate chains, and clients
// negotiate between them using CipherSuites and SignatureSchemes via SupportsCertificate.
func (s) TestServerMultipleCerts_TLS12_Negotiation(t *testing.T) {
	rsaCert := loadTestCert(t, "x509/server1_cert.pem", "x509/server1_key.pem")
	ecdsaCert := loadTestCert(t, "x509/server_ecdsa_cert.pem", "x509/server_ecdsa_key.pem")
	caPool := loadCertPool(t, "x509/server_ca_cert.pem")

	testCases := []struct {
		desc               string
		serverConfig       *tls.Config
		clientCipherSuites []uint16
		wantNegotiatedAlgo x509.PublicKeyAlgorithm
	}{
		{
			desc: "Server configured with [RSA, ECDSA], RSA client in TLS 1.2",
			serverConfig: &tls.Config{
				Certificates: []tls.Certificate{rsaCert, ecdsaCert},
				MinVersion:   tls.VersionTLS12,
				MaxVersion:   tls.VersionTLS12,
			},
			clientCipherSuites: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
			wantNegotiatedAlgo: x509.RSA,
		},
		{
			desc: "Server configured with [RSA, ECDSA], ECDSA client in TLS 1.2",
			serverConfig: &tls.Config{
				Certificates: []tls.Certificate{rsaCert, ecdsaCert},
				MinVersion:   tls.VersionTLS12,
				MaxVersion:   tls.VersionTLS12,
			},
			clientCipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
			wantNegotiatedAlgo: x509.ECDSA,
		},
		{
			desc: "Server configured with reversed [ECDSA, RSA], RSA client in TLS 1.2",
			serverConfig: &tls.Config{
				Certificates: []tls.Certificate{ecdsaCert, rsaCert},
				MinVersion:   tls.VersionTLS12,
				MaxVersion:   tls.VersionTLS12,
			},
			clientCipherSuites: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
			wantNegotiatedAlgo: x509.RSA,
		},
		{
			desc: "Server configured with reversed [ECDSA, RSA], ECDSA client in TLS 1.2",
			serverConfig: &tls.Config{
				Certificates: []tls.Certificate{ecdsaCert, rsaCert},
				MinVersion:   tls.VersionTLS12,
				MaxVersion:   tls.VersionTLS12,
			},
			clientCipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
			wantNegotiatedAlgo: x509.ECDSA,
		},
		{
			desc: "Server configured with [RSA, ECDSA], client supporting both [RSA, ECDSA] in TLS 1.2",
			serverConfig: &tls.Config{
				Certificates: []tls.Certificate{rsaCert, ecdsaCert},
				MinVersion:   tls.VersionTLS12,
				MaxVersion:   tls.VersionTLS12,
			},
			clientCipherSuites: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
			wantNegotiatedAlgo: x509.RSA,
		},
		{
			desc: "Server configured with reversed [ECDSA, RSA], client supporting both [ECDSA, RSA] in TLS 1.2",
			serverConfig: &tls.Config{
				Certificates: []tls.Certificate{ecdsaCert, rsaCert},
				MinVersion:   tls.VersionTLS12,
				MaxVersion:   tls.VersionTLS12,
			},
			clientCipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
			wantNegotiatedAlgo: x509.ECDSA,
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
				RootCAs:      caPool,
				ServerName:   "x.test.example.com",
				CipherSuites: tc.clientCipherSuites,
				MinVersion:   tls.VersionTLS12,
				MaxVersion:   tls.VersionTLS12,
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
				t.Fatalf("client EmptyCall failed: %v", err)
			}

			tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
			if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
				t.Fatalf("Failed to retrieve TLSInfo or peer certificates: %v", p.AuthInfo)
			}
			negotiatedAlgo := tlsInfo.State.PeerCertificates[0].PublicKeyAlgorithm
			if negotiatedAlgo != tc.wantNegotiatedAlgo {
				t.Errorf("negotiated certificate algorithm = %v, want %v", negotiatedAlgo, tc.wantNegotiatedAlgo)
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
	unaryInterceptor := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if p, ok := peer.FromContext(ctx); ok {
			if tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo); ok && len(tlsInfo.State.PeerCertificates) > 0 {
				lastClientCertAlgo = tlsInfo.State.PeerCertificates[0].PublicKeyAlgorithm
				lastTLSVersion = tlsInfo.State.Version
			}
		}
		return handler(ctx, req)
	}

	testCases := []struct {
		desc               string
		serverCerts        []tls.Certificate
		clientCert         tls.Certificate
		wantClientAlgo     x509.PublicKeyAlgorithm
		wantNegotiatedAlgo x509.PublicKeyAlgorithm
	}{
		{
			desc:               "Server configured with [RSA, ECDSA], RSA client in TLS 1.3 mTLS",
			serverCerts:        []tls.Certificate{rsaServerCert, ecdsaServerCert},
			clientCert:         rsaClientCert,
			wantClientAlgo:     x509.RSA,
			wantNegotiatedAlgo: x509.RSA,
		},
		{
			desc:               "Server configured with [RSA, ECDSA], ECDSA client in TLS 1.3 mTLS",
			serverCerts:        []tls.Certificate{rsaServerCert, ecdsaServerCert},
			clientCert:         ecdsaClientCert,
			wantClientAlgo:     x509.ECDSA,
			wantNegotiatedAlgo: x509.RSA,
		},
		{
			desc:               "Server configured with reversed [ECDSA, RSA], RSA client in TLS 1.3 mTLS",
			serverCerts:        []tls.Certificate{ecdsaServerCert, rsaServerCert},
			clientCert:         rsaClientCert,
			wantClientAlgo:     x509.RSA,
			wantNegotiatedAlgo: x509.ECDSA,
		},
		{
			desc:               "Server configured with reversed [ECDSA, RSA], ECDSA client in TLS 1.3 mTLS",
			serverCerts:        []tls.Certificate{ecdsaServerCert, rsaServerCert},
			clientCert:         ecdsaClientCert,
			wantClientAlgo:     x509.ECDSA,
			wantNegotiatedAlgo: x509.ECDSA,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			serverCreds := credentials.NewTLS(&tls.Config{
				Certificates: tc.serverCerts,
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

			clientCreds := credentials.NewTLS(&tls.Config{
				Certificates: []tls.Certificate{tc.clientCert},
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
				t.Fatalf("client EmptyCall failed in TLS 1.3 mTLS: %v", err)
			}

			tlsInfo := p.AuthInfo.(credentials.TLSInfo)
			if tlsInfo.State.Version != tls.VersionTLS13 {
				t.Errorf("negotiated TLS version = %x, want %x (TLS 1.3)", tlsInfo.State.Version, tls.VersionTLS13)
			}
			if lastTLSVersion != tls.VersionTLS13 {
				t.Errorf("server observed TLS version = %x, want %x (TLS 1.3)", lastTLSVersion, tls.VersionTLS13)
			}
			if lastClientCertAlgo != tc.wantClientAlgo {
				t.Errorf("client certificate algorithm verified by server = %v, want %v", lastClientCertAlgo, tc.wantClientAlgo)
			}
			if serverAlgo := tlsInfo.State.PeerCertificates[0].PublicKeyAlgorithm; serverAlgo != tc.wantNegotiatedAlgo {
				t.Errorf("server certificate algorithm = %v, want %v", serverAlgo, tc.wantNegotiatedAlgo)
			}
		})
	}
}

// TestServerMultipleCerts_TLS12_MutualTLS tests mTLS end-to-end in TLS 1.2
// where the server has dual RSA and ECDSA certificates and requires client certs.
func (s) TestServerMultipleCerts_TLS12_MutualTLS(t *testing.T) {
	rsaServerCert := loadTestCert(t, "x509/server1_cert.pem", "x509/server1_key.pem")
	ecdsaServerCert := loadTestCert(t, "x509/server_ecdsa_cert.pem", "x509/server_ecdsa_key.pem")
	serverCAPool := loadCertPool(t, "x509/server_ca_cert.pem")
	clientCAPool := loadCertPool(t, "x509/client_ca_cert.pem")

	rsaClientCert := loadTestCert(t, "x509/client1_cert.pem", "x509/client1_key.pem")
	ecdsaClientCert := loadTestCert(t, "x509/client_ecdsa_cert.pem", "x509/client_ecdsa_key.pem")

	var lastClientCertAlgo x509.PublicKeyAlgorithm
	unaryInterceptor := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if p, ok := peer.FromContext(ctx); ok {
			if tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo); ok && len(tlsInfo.State.PeerCertificates) > 0 {
				lastClientCertAlgo = tlsInfo.State.PeerCertificates[0].PublicKeyAlgorithm
			}
		}
		return handler(ctx, req)
	}

	testCases := []struct {
		desc               string
		serverCerts        []tls.Certificate
		clientCert         tls.Certificate
		clientCipherSuites []uint16
		wantClientAlgo     x509.PublicKeyAlgorithm
		wantNegotiatedAlgo x509.PublicKeyAlgorithm
	}{
		{
			desc:               "Server [RSA, ECDSA], RSA client in TLS 1.2 mTLS",
			serverCerts:        []tls.Certificate{rsaServerCert, ecdsaServerCert},
			clientCert:         rsaClientCert,
			clientCipherSuites: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
			wantClientAlgo:     x509.RSA,
			wantNegotiatedAlgo: x509.RSA,
		},
		{
			desc:               "Server [RSA, ECDSA], ECDSA client in TLS 1.2 mTLS",
			serverCerts:        []tls.Certificate{rsaServerCert, ecdsaServerCert},
			clientCert:         ecdsaClientCert,
			clientCipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
			wantClientAlgo:     x509.ECDSA,
			wantNegotiatedAlgo: x509.ECDSA,
		},
		{
			desc:               "Server reversed [ECDSA, RSA], RSA client in TLS 1.2 mTLS",
			serverCerts:        []tls.Certificate{ecdsaServerCert, rsaServerCert},
			clientCert:         rsaClientCert,
			clientCipherSuites: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
			wantClientAlgo:     x509.RSA,
			wantNegotiatedAlgo: x509.RSA,
		},
		{
			desc:               "Server reversed [ECDSA, RSA], ECDSA client in TLS 1.2 mTLS",
			serverCerts:        []tls.Certificate{ecdsaServerCert, rsaServerCert},
			clientCert:         ecdsaClientCert,
			clientCipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
			wantClientAlgo:     x509.ECDSA,
			wantNegotiatedAlgo: x509.ECDSA,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			serverCreds := credentials.NewTLS(&tls.Config{
				Certificates: tc.serverCerts,
				ClientCAs:    clientCAPool,
				ClientAuth:   tls.RequireAndVerifyClientCert,
				MinVersion:   tls.VersionTLS12,
				MaxVersion:   tls.VersionTLS12,
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

			clientCreds := credentials.NewTLS(&tls.Config{
				Certificates: []tls.Certificate{tc.clientCert},
				RootCAs:      serverCAPool,
				ServerName:   "x.test.example.com",
				CipherSuites: tc.clientCipherSuites,
				MinVersion:   tls.VersionTLS12,
				MaxVersion:   tls.VersionTLS12,
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
				t.Fatalf("client EmptyCall failed in TLS 1.2 mTLS: %v", err)
			}

			tlsInfo := p.AuthInfo.(credentials.TLSInfo)
			if serverAlgo := tlsInfo.State.PeerCertificates[0].PublicKeyAlgorithm; serverAlgo != tc.wantNegotiatedAlgo {
				t.Errorf("server certificate algorithm = %v, want %v", serverAlgo, tc.wantNegotiatedAlgo)
			}
			if lastClientCertAlgo != tc.wantClientAlgo {
				t.Errorf("client certificate algorithm verified by server = %v, want %v", lastClientCertAlgo, tc.wantClientAlgo)
			}
		})
	}
}

// TestServerMultipleCerts_IncompatibleAlgorithm tests that when the server possesses
// only RSA certificates, a client offering only ECDSA cipher suites fails to handshake in TLS 1.2.
func (s) TestServerMultipleCerts_IncompatibleAlgorithm(t *testing.T) {
	rsaCert := loadTestCert(t, "x509/server1_cert.pem", "x509/server1_key.pem")
	ecdsaCert := loadTestCert(t, "x509/server_ecdsa_cert.pem", "x509/server_ecdsa_key.pem")
	caPool := loadCertPool(t, "x509/server_ca_cert.pem")

	testCases := []struct {
		desc               string
		serverCerts        []tls.Certificate
		clientCipherSuites []uint16
	}{
		{
			desc:               "Server RSA-only, client ECDSA-only in TLS 1.2 fails",
			serverCerts:        []tls.Certificate{rsaCert},
			clientCipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
		},
		{
			desc:               "Server ECDSA-only, client RSA-only in TLS 1.2 fails",
			serverCerts:        []tls.Certificate{ecdsaCert},
			clientCipherSuites: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			serverCreds := credentials.NewTLS(&tls.Config{
				Certificates: tc.serverCerts,
				MinVersion:   tls.VersionTLS12,
				MaxVersion:   tls.VersionTLS12,
			})
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
				RootCAs:      caPool,
				ServerName:   "x.test.example.com",
				CipherSuites: tc.clientCipherSuites,
				MinVersion:   tls.VersionTLS12,
				MaxVersion:   tls.VersionTLS12,
			})
			conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(clientCreds), grpc.WithAuthority("x.test.example.com"), grpc.WithDisableServiceConfig())
			if err != nil {
				t.Fatalf("grpc.NewClient failed: %v", err)
			}
			defer conn.Close()

			client := testgrpc.NewTestServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err == nil {
				t.Fatalf("EmptyCall succeeded unexpectedly when client offered only incompatible cipher suites")
			}
		})
	}
}
