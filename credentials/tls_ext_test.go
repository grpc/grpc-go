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

package credentials_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/testdata"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

const defaultTestTimeout = 10 * time.Second
const defaultTestShortTimeout = 10 * time.Millisecond

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

var serverCert tls.Certificate
var certPool *x509.CertPool
var serverName = "x.test.example.com"

func init() {
	var err error
	serverCert, err = tls.LoadX509KeyPair(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		panic(fmt.Sprintf("tls.LoadX509KeyPair(server1.pem, server1.key) failed: %v", err))
	}

	b, err := os.ReadFile(testdata.Path("x509/server_ca_cert.pem"))
	if err != nil {
		panic(fmt.Sprintf("Error reading CA cert file: %v", err))
	}
	certPool = x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(b) {
		panic("Error appending cert from PEM")
	}
}

// Tests that the MinVersion of tls.Config is set to 1.2 if it is not already
// set by the user.
func (s) TestTLS_MinVersion12(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testCases := []struct {
		name      string
		serverTLS func() *tls.Config
	}{
		{
			name: "base_case",
			serverTLS: func() *tls.Config {
				return &tls.Config{
					// MinVersion should be set to 1.2 by gRPC by default.
					Certificates: []tls.Certificate{serverCert},
				}
			},
		},
		{
			name: "fallback_to_base",
			serverTLS: func() *tls.Config {
				config := &tls.Config{
					// MinVersion should be set to 1.2 by gRPC by default.
					Certificates: []tls.Certificate{serverCert},
				}
				config.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
					return nil, nil
				}
				return config
			},
		},
		{
			name: "dynamic_using_get_config_for_client",
			serverTLS: func() *tls.Config {
				return &tls.Config{
					GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
						return &tls.Config{
							// MinVersion should be set to 1.2 by gRPC by default.
							Certificates: []tls.Certificate{serverCert},
						}, nil
					},
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create server creds without a minimum version.
			serverCreds := credentials.NewTLS(tc.serverTLS())
			ss := stubserver.StubServer{
				EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
					return &testpb.Empty{}, nil
				},
			}

			// Create client creds that supports V1.0-V1.1.
			clientCreds := credentials.NewTLS(&tls.Config{
				ServerName: serverName,
				RootCAs:    certPool,
				MinVersion: tls.VersionTLS10,
				MaxVersion: tls.VersionTLS11,
			})

			// Start server and client separately, because Start() blocks on a
			// successful connection, which we will not get.
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
			if _, err = client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.Unavailable || !strings.Contains(status.Convert(err).Message(), wantStr) {
				t.Fatalf("EmptyCall err = %v; want code=%v, message contains %q", err, codes.Unavailable, wantStr)
			}

		})
	}
}

// Tests that the MinVersion of tls.Config is not changed if it is set by the
// user.
func (s) TestTLS_MinVersionOverridable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	var allCipherSuites []uint16
	for _, cs := range tls.CipherSuites() {
		allCipherSuites = append(allCipherSuites, cs.ID)
	}
	testCases := []struct {
		name      string
		serverTLS func() *tls.Config
	}{
		{
			name: "base_case",
			serverTLS: func() *tls.Config {
				return &tls.Config{
					MinVersion:   tls.VersionTLS10,
					Certificates: []tls.Certificate{serverCert},
					CipherSuites: allCipherSuites,
				}
			},
		},
		{
			name: "fallback_to_base",
			serverTLS: func() *tls.Config {
				config := &tls.Config{
					MinVersion:   tls.VersionTLS10,
					Certificates: []tls.Certificate{serverCert},
					CipherSuites: allCipherSuites,
				}
				config.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
					return nil, nil
				}
				return config
			},
		},
		{
			name: "dynamic_using_get_config_for_client",
			serverTLS: func() *tls.Config {
				return &tls.Config{
					GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
						return &tls.Config{
							MinVersion:   tls.VersionTLS10,
							Certificates: []tls.Certificate{serverCert},
							CipherSuites: allCipherSuites,
						}, nil
					},
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create server creds that allow v1.0.
			serverCreds := credentials.NewTLS(tc.serverTLS())
			ss := stubserver.StubServer{
				EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
					return &testpb.Empty{}, nil
				},
			}

			// Create client creds that supports V1.0-V1.1.
			clientCreds := credentials.NewTLS(&tls.Config{
				ServerName:   serverName,
				RootCAs:      certPool,
				CipherSuites: allCipherSuites,
				MinVersion:   tls.VersionTLS10,
				MaxVersion:   tls.VersionTLS11,
			})

			if err := ss.Start([]grpc.ServerOption{grpc.Creds(serverCreds)}, grpc.WithTransportCredentials(clientCreds)); err != nil {
				t.Fatalf("Error starting stub server: %v", err)
			}
			defer ss.Stop()

			if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
				t.Fatalf("EmptyCall err = %v; want <nil>", err)
			}
		})
	}
}

// Tests that CipherSuites is set to exclude HTTP/2 forbidden suites by default.
func (s) TestTLS_CipherSuites(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testCases := []struct {
		name      string
		serverTLS func() *tls.Config
	}{
		{
			name: "base_case",
			serverTLS: func() *tls.Config {
				return &tls.Config{
					Certificates: []tls.Certificate{serverCert},
				}
			},
		},
		{
			name: "fallback_to_base",
			serverTLS: func() *tls.Config {
				config := &tls.Config{
					Certificates: []tls.Certificate{serverCert},
				}
				config.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
					return nil, nil
				}
				return config
			},
		},
		{
			name: "dynamic_using_get_config_for_client",
			serverTLS: func() *tls.Config {
				return &tls.Config{
					GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
						return &tls.Config{
							Certificates: []tls.Certificate{serverCert},
						}, nil
					},
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create server creds without cipher suites.
			serverCreds := credentials.NewTLS(tc.serverTLS())
			ss := stubserver.StubServer{
				EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
					return &testpb.Empty{}, nil
				},
			}

			// Create client creds that use a forbidden suite only.
			clientCreds := credentials.NewTLS(&tls.Config{
				ServerName:   serverName,
				RootCAs:      certPool,
				CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA},
				MaxVersion:   tls.VersionTLS12, // TLS1.3 cipher suites are not configurable, so limit to 1.2.
			})

			// Start server and client separately, because Start() blocks on a
			// successful connection, which we will not get.
			if err := ss.StartServer(grpc.Creds(serverCreds)); err != nil {
				t.Fatalf("Error starting server: %v", err)
			}
			defer ss.Stop()

			cc, err := grpc.NewClient("dns:"+ss.Address, grpc.WithTransportCredentials(clientCreds))
			if err != nil {
				t.Fatalf("grpc.NewClient error: %v", err)
			}
			defer cc.Close()

			client := testgrpc.NewTestServiceClient(cc)

			const wantStr = "authentication handshake failed"
			if _, err = client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.Unavailable || !strings.Contains(status.Convert(err).Message(), wantStr) {
				t.Fatalf("EmptyCall err = %v; want code=%v, message contains %q", err, codes.Unavailable, wantStr)
			}
		})
	}
}

// Tests that CipherSuites is not overridden when it is set.
func (s) TestTLS_CipherSuitesOverridable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testCases := []struct {
		name      string
		serverTLS func() *tls.Config
	}{
		{
			name: "base_case",
			serverTLS: func() *tls.Config {
				return &tls.Config{
					Certificates: []tls.Certificate{serverCert},
					CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA},
				}
			},
		},
		{
			name: "fallback_to_base",
			serverTLS: func() *tls.Config {
				config := &tls.Config{
					Certificates: []tls.Certificate{serverCert},
					CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA},
				}
				config.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
					return nil, nil
				}
				return config
			},
		},
		{
			name: "dynamic_using_get_config_for_client",
			serverTLS: func() *tls.Config {
				return &tls.Config{
					GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
						return &tls.Config{
							Certificates: []tls.Certificate{serverCert},
							CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA},
						}, nil
					},
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create server that allows only a forbidden cipher suite.
			serverCreds := credentials.NewTLS(tc.serverTLS())
			ss := stubserver.StubServer{
				EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
					return &testpb.Empty{}, nil
				},
			}

			// Create server that allows only a forbidden cipher suite.
			clientCreds := credentials.NewTLS(&tls.Config{
				ServerName:   serverName,
				RootCAs:      certPool,
				CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA},
				MaxVersion:   tls.VersionTLS12, // TLS1.3 cipher suites are not configurable, so limit to 1.2.
			})

			if err := ss.Start([]grpc.ServerOption{grpc.Creds(serverCreds)}, grpc.WithTransportCredentials(clientCreds)); err != nil {
				t.Fatalf("Error starting stub server: %v", err)
			}
			defer ss.Stop()

			if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
				t.Fatalf("EmptyCall err = %v; want <nil>", err)
			}
		})
	}
}

// TestTLS_ServerConfiguresALPNByDefault verifies that ALPN is configured
// correctly for a server that doesn't specify the NextProtos field and uses
// GetConfigForClient to provide the TLS config during the handshake.
func (s) TestTLS_ServerConfiguresALPNByDefault(t *testing.T) {
	initialVal := envconfig.EnforceALPNEnabled
	defer func() {
		envconfig.EnforceALPNEnabled = initialVal
	}()
	envconfig.EnforceALPNEnabled = true

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create a server that doesn't set the NextProtos field.
	serverCreds := credentials.NewTLS(&tls.Config{
		GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
			return &tls.Config{
				Certificates: []tls.Certificate{serverCert},
			}, nil
		},
	})

	ss := stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}

	clientCreds := credentials.NewTLS(&tls.Config{
		ServerName: serverName,
		RootCAs:    certPool,
	})

	if err := ss.Start([]grpc.ServerOption{grpc.Creds(serverCreds)}, grpc.WithTransportCredentials(clientCreds)); err != nil {
		t.Fatalf("Error starting stub server: %v", err)
	}
	defer ss.Stop()

	if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall err = %v; want <nil>", err)
	}
}

// TestTLS_DisabledALPNClient tests the behaviour of TransportCredentials when
// connecting to a server that doesn't support ALPN.
func (s) TestTLS_DisabledALPNClient(t *testing.T) {
	initialVal := envconfig.EnforceALPNEnabled
	defer func() {
		envconfig.EnforceALPNEnabled = initialVal
	}()

	tests := []struct {
		name         string
		alpnEnforced bool
		wantErr      bool
	}{
		{
			name:         "enforced",
			alpnEnforced: true,
			wantErr:      true,
		},
		{
			name: "not_enforced",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			envconfig.EnforceALPNEnabled = tc.alpnEnforced

			listener, err := tls.Listen("tcp", "localhost:0", &tls.Config{
				Certificates: []tls.Certificate{serverCert},
				NextProtos:   []string{}, // Empty list indicates ALPN is disabled.
			})
			if err != nil {
				t.Fatalf("Error starting TLS server: %v", err)
			}

			errCh := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					errCh <- fmt.Errorf("listener.Accept returned error: %v", err)
				} else {
					// The first write to the TLS listener initiates the TLS handshake.
					conn.Write([]byte("Hello, World!"))
					conn.Close()
				}
				close(errCh)
			}()

			serverAddr := listener.Addr().String()
			conn, err := net.Dial("tcp", serverAddr)
			if err != nil {
				t.Fatalf("net.Dial(%s) failed: %v", serverAddr, err)
			}
			defer conn.Close()

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			clientCfg := tls.Config{
				ServerName: serverName,
				RootCAs:    certPool,
				NextProtos: []string{"h2"},
			}
			_, _, err = credentials.NewTLS(&clientCfg).ClientHandshake(ctx, serverName, conn)

			if gotErr := (err != nil); gotErr != tc.wantErr {
				t.Errorf("ClientHandshake returned unexpected error: got=%v, want=%t", err, tc.wantErr)
			}

			select {
			case err := <-errCh:
				if err != nil {
					t.Fatalf("Unexpected error received from server: %v", err)
				}
			case <-ctx.Done():
				t.Fatalf("Timeout waiting for error from server")
			}
		})
	}
}

// TestTLS_DisabledALPNServer tests the behaviour of TransportCredentials when
// accepting a request from a client that doesn't support ALPN.
func (s) TestTLS_DisabledALPNServer(t *testing.T) {
	initialVal := envconfig.EnforceALPNEnabled
	defer func() {
		envconfig.EnforceALPNEnabled = initialVal
	}()

	tests := []struct {
		name         string
		alpnEnforced bool
		wantErr      bool
	}{
		{
			name:         "enforced",
			alpnEnforced: true,
			wantErr:      true,
		},
		{
			name: "not_enforced",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			envconfig.EnforceALPNEnabled = tc.alpnEnforced

			listener, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("Error starting server: %v", err)
			}

			errCh := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					errCh <- fmt.Errorf("listener.Accept returned error: %v", err)
					return
				}
				defer conn.Close()
				serverCfg := tls.Config{
					Certificates: []tls.Certificate{serverCert},
					NextProtos:   []string{"h2"},
				}
				_, _, err = credentials.NewTLS(&serverCfg).ServerHandshake(conn)
				if gotErr := (err != nil); gotErr != tc.wantErr {
					t.Errorf("ServerHandshake returned unexpected error: got=%v, want=%t", err, tc.wantErr)
				}
				close(errCh)
			}()

			serverAddr := listener.Addr().String()
			clientCfg := &tls.Config{
				Certificates: []tls.Certificate{serverCert},
				NextProtos:   []string{}, // Empty list indicates ALPN is disabled.
				RootCAs:      certPool,
				ServerName:   serverName,
			}
			conn, err := tls.Dial("tcp", serverAddr, clientCfg)
			if err != nil {
				t.Fatalf("tls.Dial(%s) failed: %v", serverAddr, err)
			}
			defer conn.Close()

			select {
			case <-time.After(defaultTestTimeout):
				t.Fatal("Timed out waiting for completion")
			case err := <-errCh:
				if err != nil {
					t.Fatalf("Unexpected server error: %v", err)
				}
			}
		})
	}
}

// skipIfMLDSAUnsupported skips t if the running Go standard library does not
// support ML-DSA X.509 certificates and PKCS#8 private keys (added in Go 1.27).
func skipIfMLDSAUnsupported(t *testing.T) {
	t.Helper()
	if _, err := tls.LoadX509KeyPair(testdata.Path("x509/mldsa/server_mldsa44.pem"), testdata.Path("x509/mldsa/server_mldsa44.key")); err != nil {
		t.Skipf("skipping ML-DSA test: crypto/tls does not support ML-DSA keys/certificates in this Go version: %v", err)
	}
}

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

// TestTLS_MLDSA_ServerAuth tests server-authenticated TLS 1.3 handshakes and
// unary RPCs using ML-DSA-44, ML-DSA-65, ML-DSA-87, and a 2-certificate
// ML-DSA-87 chain.
func (s) TestTLS_MLDSA_ServerAuth(t *testing.T) {
	skipIfMLDSAUnsupported(t)
	tests := []struct {
		name              string
		serverCertFile    string
		serverKeyFile     string
		caCertFile        string
		wantServerSigAlgo string
		wantChainLen      int
	}{
		{
			name:              "mldsa44",
			serverCertFile:    "x509/mldsa/server_mldsa44.pem",
			serverKeyFile:     "x509/mldsa/server_mldsa44.key",
			caCertFile:        "x509/mldsa/ca_mldsa44.pem",
			wantServerSigAlgo: "ML-DSA-44",
			wantChainLen:      1,
		},
		{
			name:              "mldsa65",
			serverCertFile:    "x509/mldsa/server_mldsa65.pem",
			serverKeyFile:     "x509/mldsa/server_mldsa65.key",
			caCertFile:        "x509/mldsa/ca_mldsa65.pem",
			wantServerSigAlgo: "ML-DSA-65",
			wantChainLen:      1,
		},
		{
			name:              "mldsa87",
			serverCertFile:    "x509/mldsa/server_mldsa87.pem",
			serverKeyFile:     "x509/mldsa/server_mldsa87.key",
			caCertFile:        "x509/mldsa/ca_mldsa87.pem",
			wantServerSigAlgo: "ML-DSA-87",
			wantChainLen:      1,
		},
		{
			name:              "mldsa87_chain",
			serverCertFile:    "x509/mldsa/server_chain_mldsa87.pem",
			serverKeyFile:     "x509/mldsa/server_chain_mldsa87.key",
			caCertFile:        "x509/mldsa/ca_mldsa87.pem",
			wantServerSigAlgo: "ML-DSA-87",
			wantChainLen:      2,
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
				t.Errorf("negotiated TLS version = 0x%x, want 0x%x (TLS 1.3)", tlsInfo.State.Version, tls.VersionTLS13)
			}
			if got := len(tlsInfo.State.PeerCertificates); got != tc.wantChainLen {
				t.Fatalf("len(PeerCertificates) = %d, want %d", got, tc.wantChainLen)
			}
			if got := tlsInfo.State.PeerCertificates[0].SignatureAlgorithm.String(); got != tc.wantServerSigAlgo {
				t.Errorf("server leaf SignatureAlgorithm = %q, want %q", got, tc.wantServerSigAlgo)
			}
		})
	}
}

// TestTLS_MLDSA_MutualTLS tests mutual TLS (mTLS) 1.3 handshakes and unary RPCs
// using homogeneous ML-DSA-44, ML-DSA-65, ML-DSA-87, 2-certificate ML-DSA-87
// chains, mixed ML-DSA variants (ML-DSA-87 server + ML-DSA-44 client), and a
// combined classical RSA + ML-DSA-65 root store.
func (s) TestTLS_MLDSA_MutualTLS(t *testing.T) {
	skipIfMLDSAUnsupported(t)
	tests := []struct {
		name              string
		serverCertFile    string
		serverKeyFile     string
		serverRootFiles   []string
		clientCertFile    string
		clientKeyFile     string
		clientRootFiles   []string
		wantServerSigAlgo string
		wantClientSigAlgo string
	}{
		{
			name:              "mldsa44",
			serverCertFile:    "x509/mldsa/server_mldsa44.pem",
			serverKeyFile:     "x509/mldsa/server_mldsa44.key",
			serverRootFiles:   []string{"x509/mldsa/ca_mldsa44.pem"},
			clientCertFile:    "x509/mldsa/client_mldsa44.pem",
			clientKeyFile:     "x509/mldsa/client_mldsa44.key",
			clientRootFiles:   []string{"x509/mldsa/ca_mldsa44.pem"},
			wantServerSigAlgo: "ML-DSA-44",
			wantClientSigAlgo: "ML-DSA-44",
		},
		{
			name:              "mldsa65",
			serverCertFile:    "x509/mldsa/server_mldsa65.pem",
			serverKeyFile:     "x509/mldsa/server_mldsa65.key",
			serverRootFiles:   []string{"x509/mldsa/ca_mldsa65.pem"},
			clientCertFile:    "x509/mldsa/client_mldsa65.pem",
			clientKeyFile:     "x509/mldsa/client_mldsa65.key",
			clientRootFiles:   []string{"x509/mldsa/ca_mldsa65.pem"},
			wantServerSigAlgo: "ML-DSA-65",
			wantClientSigAlgo: "ML-DSA-65",
		},
		{
			name:              "mldsa87",
			serverCertFile:    "x509/mldsa/server_mldsa87.pem",
			serverKeyFile:     "x509/mldsa/server_mldsa87.key",
			serverRootFiles:   []string{"x509/mldsa/ca_mldsa87.pem"},
			clientCertFile:    "x509/mldsa/client_mldsa87.pem",
			clientKeyFile:     "x509/mldsa/client_mldsa87.key",
			clientRootFiles:   []string{"x509/mldsa/ca_mldsa87.pem"},
			wantServerSigAlgo: "ML-DSA-87",
			wantClientSigAlgo: "ML-DSA-87",
		},
		{
			name:              "mldsa87_chain",
			serverCertFile:    "x509/mldsa/server_chain_mldsa87.pem",
			serverKeyFile:     "x509/mldsa/server_chain_mldsa87.key",
			serverRootFiles:   []string{"x509/mldsa/ca_mldsa87.pem"},
			clientCertFile:    "x509/mldsa/client_chain_mldsa87.pem",
			clientKeyFile:     "x509/mldsa/client_chain_mldsa87.key",
			clientRootFiles:   []string{"x509/mldsa/ca_mldsa87.pem"},
			wantServerSigAlgo: "ML-DSA-87",
			wantClientSigAlgo: "ML-DSA-87",
		},
		{
			name:              "mixed_mldsa_server87_client44",
			serverCertFile:    "x509/mldsa/server_mldsa87.pem",
			serverKeyFile:     "x509/mldsa/server_mldsa87.key",
			serverRootFiles:   []string{"x509/mldsa/ca_mldsa87.pem", "x509/mldsa/ca_mldsa44.pem"},
			clientCertFile:    "x509/mldsa/client_mldsa44.pem",
			clientKeyFile:     "x509/mldsa/client_mldsa44.key",
			clientRootFiles:   []string{"x509/mldsa/ca_mldsa87.pem", "x509/mldsa/ca_mldsa44.pem"},
			wantServerSigAlgo: "ML-DSA-87",
			wantClientSigAlgo: "ML-DSA-44",
		},
		{
			name:              "mixed_rsa_server_mldsa65_client",
			serverCertFile:    "x509/server1_cert.pem",
			serverKeyFile:     "x509/server1_key.pem",
			serverRootFiles:   []string{"x509/server_ca_cert.pem", "x509/mldsa/ca_mldsa65.pem"},
			clientCertFile:    "x509/mldsa/client_mldsa65.pem",
			clientKeyFile:     "x509/mldsa/client_mldsa65.key",
			clientRootFiles:   []string{"x509/server_ca_cert.pem", "x509/mldsa/ca_mldsa65.pem"},
			wantServerSigAlgo: "SHA256-RSA",
			wantClientSigAlgo: "ML-DSA-65",
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
					if len(tlsInfo.State.PeerCertificates) == 0 {
						return nil, status.Error(codes.Internal, "server received no client certificates")
					}
					if got := tlsInfo.State.PeerCertificates[0].SignatureAlgorithm.String(); got != tc.wantClientSigAlgo {
						return nil, status.Errorf(codes.Internal, "client leaf SignatureAlgorithm = %q, want %q", got, tc.wantClientSigAlgo)
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
			if got := tlsInfo.State.PeerCertificates[0].SignatureAlgorithm.String(); got != tc.wantServerSigAlgo {
				t.Errorf("server leaf SignatureAlgorithm = %q, want %q", got, tc.wantServerSigAlgo)
			}
		})
	}
}

// TestTLS_MLDSA_ChainSessionResumption tests TLS 1.3 session resumption with
// 2-certificate ML-DSA-87 server and client chains.
func (s) TestTLS_MLDSA_ChainSessionResumption(t *testing.T) {
	skipIfMLDSAUnsupported(t)
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
			t.Fatalf("iteration %d: grpc.NewClient error: %v", i, err)
		}
		client := testgrpc.NewTestServiceClient(cc)
		var serverPeer peer.Peer
		if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&serverPeer)); err != nil {
			cc.Close()
			t.Fatalf("iteration %d: EmptyCall err = %v; want <nil>", i, err)
		}
		tlsInfo, ok := serverPeer.AuthInfo.(credentials.TLSInfo)
		if !ok {
			cc.Close()
			t.Fatalf("iteration %d: serverPeer.AuthInfo = %T, want credentials.TLSInfo", i, serverPeer.AuthInfo)
		}
		if tlsInfo.State.DidResume != wantResume {
			t.Errorf("iteration %d: DidResume = %v, want %v", i, tlsInfo.State.DidResume, wantResume)
		}
		cc.Close()
	}
}

// TestTLS_MLDSA_RequiresTLS13 verifies that ML-DSA certificates cannot be used
// when the TLS version is capped at TLS 1.2.
func (s) TestTLS_MLDSA_RequiresTLS13(t *testing.T) {
	skipIfMLDSAUnsupported(t)
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
	if _, err = client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.Unavailable || !strings.Contains(status.Convert(err).Message(), wantStr) {
		t.Fatalf("EmptyCall err = %v; want code=%v, message contains %q", err, codes.Unavailable, wantStr)
	}
}
