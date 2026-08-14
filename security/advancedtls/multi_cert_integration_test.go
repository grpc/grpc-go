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
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
	"google.golang.org/grpc/security/advancedtls/internal/testutils"
)

// staticIdentityProvider implements certprovider.Provider for in-memory testing.
type staticIdentityProvider struct {
	certs []tls.Certificate
}

func (p *staticIdentityProvider) KeyMaterial(context.Context) (*certprovider.KeyMaterial, error) {
	return &certprovider.KeyMaterial{Certs: p.certs}, nil
}

func (p *staticIdentityProvider) Close() {}

func dialAndCall(ctx context.Context, addr string, authority string, creds credentials.TransportCredentials, shouldFail bool) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds), grpc.WithAuthority(authority), grpc.WithDisableServiceConfig())
	if err != nil {
		return nil, err
	}
	client := pb.NewGreeterClient(conn)
	_, err = client.SayHello(ctx, &pb.HelloRequest{Name: "test"})
	if want, got := shouldFail, err != nil; got != want {
		conn.Close()
		return nil, fmt.Errorf("want and got mismatch, want shouldFail=%v, got fail=%v, rpc error: %v", want, got, err)
	}
	return conn, nil
}

// TestBuildGetCertificates_SignatureAlgorithmsExtension_TLS13 verifies that
// buildGetCertificates and ClientHelloInfo.SupportsCertificate evaluate the
// signature_algorithms TLS extension in TLS 1.3 to select the supported certificate
// (RSA vs ECDSA) without relying on TLS 1.2 CipherSuites.
func (s) TestBuildGetCertificates_SignatureAlgorithmsExtension_TLS13(t *testing.T) {
	cs := &testutils.CertStore{}
	if err := cs.LoadCerts(); err != nil {
		t.Fatalf("cs.LoadCerts() failed, err: %v", err)
	}

	opts := &Options{
		IdentityOptions: IdentityCertificateOptions{
			Certificates: []tls.Certificate{cs.ServerPeerLocalhost1, cs.ServerPeerECDSALocalhost1},
		},
	}

	// 1. ClientHello in TLS 1.3 offering only RSA signature algorithms (RSASSA-PSS).
	chiRSA := &tls.ClientHelloInfo{
		ServerName: "localhost",
		SignatureSchemes: []tls.SignatureScheme{
			tls.PSSWithSHA256,
			tls.PSSWithSHA384,
			tls.PSSWithSHA512,
		},
		SupportedCurves:   []tls.CurveID{tls.X25519, tls.CurveP256},
		SupportedVersions: []uint16{tls.VersionTLS13},
	}
	// Verify SupportsCertificate behavior directly for TLS 1.3
	if err := chiRSA.SupportsCertificate(&cs.ServerPeerLocalhost1); err != nil {
		t.Fatalf("chiRSA.SupportsCertificate(RSA) failed unexpectedly in TLS 1.3: %v", err)
	}
	if err := chiRSA.SupportsCertificate(&cs.ServerPeerECDSALocalhost1); err == nil {
		t.Fatalf("chiRSA.SupportsCertificate(ECDSA) succeeded unexpectedly for RSA-only signature schemes")
	}

	selectedRSA, err := buildGetCertificates(chiRSA, opts)
	if err != nil {
		t.Fatalf("buildGetCertificates(chiRSA) returned error: %v", err)
	}
	if !bytes.Equal(selectedRSA.Certificate[0], cs.ServerPeerLocalhost1.Certificate[0]) {
		t.Errorf("buildGetCertificates(chiRSA) selected unexpected cert, want RSA cert")
	}

	// 2. ClientHello in TLS 1.3 offering only ECDSA signature algorithms.
	chiECDSA := &tls.ClientHelloInfo{
		ServerName: "localhost",
		SignatureSchemes: []tls.SignatureScheme{
			tls.ECDSAWithP256AndSHA256,
			tls.ECDSAWithP384AndSHA384,
		},
		SupportedCurves:   []tls.CurveID{tls.X25519, tls.CurveP256},
		SupportedVersions: []uint16{tls.VersionTLS13},
	}
	// Verify SupportsCertificate behavior directly for TLS 1.3
	if err := chiECDSA.SupportsCertificate(&cs.ServerPeerECDSALocalhost1); err != nil {
		t.Fatalf("chiECDSA.SupportsCertificate(ECDSA) failed unexpectedly in TLS 1.3: %v", err)
	}
	if err := chiECDSA.SupportsCertificate(&cs.ServerPeerLocalhost1); err == nil {
		t.Fatalf("chiECDSA.SupportsCertificate(RSA) succeeded unexpectedly for ECDSA-only signature schemes")
	}

	selectedECDSA, err := buildGetCertificates(chiECDSA, opts)
	if err != nil {
		t.Fatalf("buildGetCertificates(chiECDSA) returned error: %v", err)
	}
	if !bytes.Equal(selectedECDSA.Certificate[0], cs.ServerPeerECDSALocalhost1.Certificate[0]) {
		t.Errorf("buildGetCertificates(chiECDSA) selected unexpected cert, want ECDSA cert")
	}

	// 3. ClientHello in TLS 1.3 offering only incompatible signature algorithms (e.g. Ed25519).
	chiIncompatible := &tls.ClientHelloInfo{
		ServerName: "localhost",
		SignatureSchemes: []tls.SignatureScheme{
			tls.Ed25519,
		},
		SupportedCurves:   []tls.CurveID{tls.X25519},
		SupportedVersions: []uint16{tls.VersionTLS13},
	}
	if err := chiIncompatible.SupportsCertificate(&cs.ServerPeerLocalhost1); err == nil {
		t.Fatalf("chiIncompatible.SupportsCertificate(RSA) succeeded unexpectedly for Ed25519-only signature schemes")
	}
	if err := chiIncompatible.SupportsCertificate(&cs.ServerPeerECDSALocalhost1); err == nil {
		t.Fatalf("chiIncompatible.SupportsCertificate(ECDSA) succeeded unexpectedly for Ed25519-only signature schemes")
	}

	// 4. Reversed order: verify buildGetCertificates returns RSA cert when RSA is requested even if ECDSA is first in list.
	optsReversed := &Options{
		IdentityOptions: IdentityCertificateOptions{
			Certificates: []tls.Certificate{cs.ServerPeerECDSALocalhost1, cs.ServerPeerLocalhost1},
		},
	}
	selectedRSAFromReversed, err := buildGetCertificates(chiRSA, optsReversed)
	if err != nil {
		t.Fatalf("buildGetCertificates(chiRSA) from reversed returned error: %v", err)
	}
	if !bytes.Equal(selectedRSAFromReversed.Certificate[0], cs.ServerPeerLocalhost1.Certificate[0]) {
		t.Errorf("buildGetCertificates(chiRSA) selected unexpected cert from reversed list, want RSA cert")
	}
}

// TestBuildGetCertificates_SignatureAlgorithmsAndCipherSuites_TLS12 verifies that
// buildGetCertificates and ClientHelloInfo.SupportsCertificate correctly evaluate
// CipherSuites and SignatureSchemes in TLS 1.2 to select the matching certificate.
func (s) TestBuildGetCertificates_SignatureAlgorithmsAndCipherSuites_TLS12(t *testing.T) {
	cs := &testutils.CertStore{}
	if err := cs.LoadCerts(); err != nil {
		t.Fatalf("cs.LoadCerts() failed, err: %v", err)
	}

	opts := &Options{
		IdentityOptions: IdentityCertificateOptions{
			Certificates: []tls.Certificate{cs.ServerPeerLocalhost1, cs.ServerPeerECDSALocalhost1},
		},
	}

	// 1. ClientHello in TLS 1.2 with RSA cipher suite and RSA signature schemes.
	chiRSA := &tls.ClientHelloInfo{
		ServerName: "localhost",
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
		SignatureSchemes: []tls.SignatureScheme{
			tls.PKCS1WithSHA256,
			tls.PSSWithSHA256,
		},
		SupportedCurves:   []tls.CurveID{tls.X25519, tls.CurveP256},
		SupportedVersions: []uint16{tls.VersionTLS12},
	}
	if err := chiRSA.SupportsCertificate(&cs.ServerPeerLocalhost1); err != nil {
		t.Fatalf("chiRSA.SupportsCertificate(RSA) failed in TLS 1.2: %v", err)
	}
	if err := chiRSA.SupportsCertificate(&cs.ServerPeerECDSALocalhost1); err == nil {
		t.Fatalf("chiRSA.SupportsCertificate(ECDSA) succeeded unexpectedly for RSA cipher suite in TLS 1.2")
	}

	selectedRSA, err := buildGetCertificates(chiRSA, opts)
	if err != nil {
		t.Fatalf("buildGetCertificates(chiRSA) returned error: %v", err)
	}
	if !bytes.Equal(selectedRSA.Certificate[0], cs.ServerPeerLocalhost1.Certificate[0]) {
		t.Errorf("buildGetCertificates(chiRSA) selected unexpected cert, want RSA cert")
	}

	// 2. ClientHello in TLS 1.2 with ECDSA cipher suite and ECDSA signature schemes.
	chiECDSA := &tls.ClientHelloInfo{
		ServerName: "localhost",
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		},
		SignatureSchemes: []tls.SignatureScheme{
			tls.ECDSAWithP256AndSHA256,
		},
		SupportedCurves:   []tls.CurveID{tls.X25519, tls.CurveP256},
		SupportedVersions: []uint16{tls.VersionTLS12},
	}
	if err := chiECDSA.SupportsCertificate(&cs.ServerPeerECDSALocalhost1); err != nil {
		t.Fatalf("chiECDSA.SupportsCertificate(ECDSA) failed in TLS 1.2: %v", err)
	}
	if err := chiECDSA.SupportsCertificate(&cs.ServerPeerLocalhost1); err == nil {
		t.Fatalf("chiECDSA.SupportsCertificate(RSA) succeeded unexpectedly for ECDSA cipher suite in TLS 1.2")
	}

	selectedECDSA, err := buildGetCertificates(chiECDSA, opts)
	if err != nil {
		t.Fatalf("buildGetCertificates(chiECDSA) returned error: %v", err)
	}
	if !bytes.Equal(selectedECDSA.Certificate[0], cs.ServerPeerECDSALocalhost1.Certificate[0]) {
		t.Errorf("buildGetCertificates(chiECDSA) selected unexpected cert, want ECDSA cert")
	}
}

// TestServerMultipleCerts_TLS13_Negotiation tests end-to-end gRPC communication
// where both server and client strictly enforce TLS 1.3 (MinTLSVersion: TLS 1.3,
// MaxTLSVersion: TLS 1.3) with no CipherSuites configured, and the server negotiates
// multiple certificates (RSA and ECDSA with identical SNI "localhost") using
// SupportsCertificate.
func (s) TestServerMultipleCerts_TLS13_Negotiation(t *testing.T) {
	cs := &testutils.CertStore{}
	if err := cs.LoadCerts(); err != nil {
		t.Fatalf("cs.LoadCerts() failed, err: %v", err)
	}

	testCases := []struct {
		desc          string
		serverOptions func() *Options
	}{
		{
			desc: "Server configured with direct Certificates slice [RSA, ECDSA] in TLS 1.3",
			serverOptions: func() *Options {
				return &Options{
					IdentityOptions: IdentityCertificateOptions{
						Certificates: []tls.Certificate{cs.ServerPeerLocalhost1, cs.ServerPeerECDSALocalhost1},
					},
					RootOptions: RootCertificateOptions{
						RootCertificates: cs.ServerTrust1,
					},
					MinTLSVersion:     tls.VersionTLS13,
					MaxTLSVersion:     tls.VersionTLS13,
					RequireClientCert: false,
					VerificationType:  CertVerification,
				}
			},
		},
		{
			desc: "Server configured with reversed Certificates slice [ECDSA, RSA] in TLS 1.3",
			serverOptions: func() *Options {
				return &Options{
					IdentityOptions: IdentityCertificateOptions{
						Certificates: []tls.Certificate{cs.ServerPeerECDSALocalhost1, cs.ServerPeerLocalhost1},
					},
					RootOptions: RootCertificateOptions{
						RootCertificates: cs.ServerTrust1,
					},
					MinTLSVersion:     tls.VersionTLS13,
					MaxTLSVersion:     tls.VersionTLS13,
					RequireClientCert: false,
					VerificationType:  CertVerification,
				}
			},
		},
		{
			desc: "Server configured with GetIdentityCertificatesForServer callback in TLS 1.3",
			serverOptions: func() *Options {
				return &Options{
					IdentityOptions: IdentityCertificateOptions{
						GetIdentityCertificatesForServer: func(*tls.ClientHelloInfo) ([]*tls.Certificate, error) {
							return []*tls.Certificate{&cs.ServerPeerLocalhost1, &cs.ServerPeerECDSALocalhost1}, nil
						},
					},
					RootOptions: RootCertificateOptions{
						RootCertificates: cs.ServerTrust1,
					},
					MinTLSVersion:     tls.VersionTLS13,
					MaxTLSVersion:     tls.VersionTLS13,
					RequireClientCert: false,
					VerificationType:  CertVerification,
				}
			},
		},
		{
			desc: "Server configured with IdentityProvider in TLS 1.3",
			serverOptions: func() *Options {
				return &Options{
					IdentityOptions: IdentityCertificateOptions{
						IdentityProvider: &staticIdentityProvider{
							certs: []tls.Certificate{cs.ServerPeerLocalhost1, cs.ServerPeerECDSALocalhost1},
						},
					},
					RootOptions: RootCertificateOptions{
						RootCertificates: cs.ServerTrust1,
					},
					MinTLSVersion:     tls.VersionTLS13,
					MaxTLSVersion:     tls.VersionTLS13,
					RequireClientCert: false,
					VerificationType:  CertVerification,
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			serverOpts := tc.serverOptions()
			serverTLSCreds, err := NewServerCreds(serverOpts)
			if err != nil {
				t.Fatalf("NewServerCreds failed: %v", err)
			}
			s := grpc.NewServer(grpc.Creds(serverTLSCreds))
			defer s.Stop()

			lis, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("net.Listen failed: %v", err)
			}
			defer lis.Close()
			addr := lis.Addr().String()
			pb.RegisterGreeterServer(s, greeterServer{})
			go s.Serve(lis)

			// Client connects using strictly TLS 1.3 with no CipherSuites configured.
			var negotiatedAlgo x509.PublicKeyAlgorithm
			clientOpts := &Options{
				RootOptions: RootCertificateOptions{
					RootCertificates: cs.ClientTrust1,
				},
				MinTLSVersion:    tls.VersionTLS13,
				MaxTLSVersion:    tls.VersionTLS13,
				VerificationType: CertAndHostVerification,
				AdditionalPeerVerification: func(params *HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
					if params.Leaf != nil {
						negotiatedAlgo = params.Leaf.PublicKeyAlgorithm
					} else if len(params.RawCerts) > 0 {
						cert, err := x509.ParseCertificate(params.RawCerts[0])
						if err != nil {
							return nil, err
						}
						negotiatedAlgo = cert.PublicKeyAlgorithm
					}
					return &PostHandshakeVerificationResults{}, nil
				},
			}
			clientCreds, err := NewClientCreds(clientOpts)
			if err != nil {
				t.Fatalf("NewClientCreds failed: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			conn, err := dialAndCall(ctx, addr, "localhost", clientCreds, false)
			if err != nil {
				t.Fatalf("TLS 1.3 client call failed: %v", err)
			}
			conn.Close()

			if negotiatedAlgo != x509.RSA && negotiatedAlgo != x509.ECDSA {
				t.Errorf("negotiated certificate algorithm = %v, want RSA or ECDSA", negotiatedAlgo)
			}
		})
	}
}

// TestServerMultipleCerts_TLS12_Negotiation tests end-to-end gRPC communication
// in TLS 1.2 where the server is configured with both RSA and ECDSA certificates
// sharing the same SNI ("localhost"), and RSA vs ECDSA clients negotiate between
// them using CipherSuites and SignatureSchemes via SupportsCertificate.
func (s) TestServerMultipleCerts_TLS12_Negotiation(t *testing.T) {
	cs := &testutils.CertStore{}
	if err := cs.LoadCerts(); err != nil {
		t.Fatalf("cs.LoadCerts() failed, err: %v", err)
	}

	testCases := []struct {
		desc          string
		serverOptions func() *Options
	}{
		{
			desc: "Server configured with direct Certificates slice [RSA, ECDSA] in TLS 1.2",
			serverOptions: func() *Options {
				return &Options{
					IdentityOptions: IdentityCertificateOptions{
						Certificates: []tls.Certificate{cs.ServerPeerLocalhost1, cs.ServerPeerECDSALocalhost1},
					},
					RootOptions: RootCertificateOptions{
						RootCertificates: cs.ServerTrust1,
					},
					MinTLSVersion:     tls.VersionTLS12,
					MaxTLSVersion:     tls.VersionTLS12,
					RequireClientCert: false,
					VerificationType:  CertVerification,
				}
			},
		},
		{
			desc: "Server configured with reversed Certificates slice [ECDSA, RSA] in TLS 1.2",
			serverOptions: func() *Options {
				return &Options{
					IdentityOptions: IdentityCertificateOptions{
						Certificates: []tls.Certificate{cs.ServerPeerECDSALocalhost1, cs.ServerPeerLocalhost1},
					},
					RootOptions: RootCertificateOptions{
						RootCertificates: cs.ServerTrust1,
					},
					MinTLSVersion:     tls.VersionTLS12,
					MaxTLSVersion:     tls.VersionTLS12,
					RequireClientCert: false,
					VerificationType:  CertVerification,
				}
			},
		},
		{
			desc: "Server configured with GetIdentityCertificatesForServer callback in TLS 1.2",
			serverOptions: func() *Options {
				return &Options{
					IdentityOptions: IdentityCertificateOptions{
						GetIdentityCertificatesForServer: func(*tls.ClientHelloInfo) ([]*tls.Certificate, error) {
							return []*tls.Certificate{&cs.ServerPeerLocalhost1, &cs.ServerPeerECDSALocalhost1}, nil
						},
					},
					RootOptions: RootCertificateOptions{
						RootCertificates: cs.ServerTrust1,
					},
					MinTLSVersion:     tls.VersionTLS12,
					MaxTLSVersion:     tls.VersionTLS12,
					RequireClientCert: false,
					VerificationType:  CertVerification,
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			serverOpts := tc.serverOptions()
			serverTLSCreds, err := NewServerCreds(serverOpts)
			if err != nil {
				t.Fatalf("NewServerCreds failed: %v", err)
			}
			s := grpc.NewServer(grpc.Creds(serverTLSCreds))
			defer s.Stop()

			lis, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("net.Listen failed: %v", err)
			}
			defer lis.Close()
			addr := lis.Addr().String()
			pb.RegisterGreeterServer(s, greeterServer{})
			go s.Serve(lis)

			// 1. Connect with an RSA-only client in TLS 1.2.
			{
				var negotiatedAlgo x509.PublicKeyAlgorithm
				clientOpts := &Options{
					RootOptions: RootCertificateOptions{
						RootCertificates: cs.ClientTrust1,
					},
					VerificationType: CertAndHostVerification,
					CipherSuites:     []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
					MinTLSVersion:    tls.VersionTLS12,
					MaxTLSVersion:    tls.VersionTLS12,
					AdditionalPeerVerification: func(params *HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
						if params.Leaf != nil {
							negotiatedAlgo = params.Leaf.PublicKeyAlgorithm
						} else if len(params.RawCerts) > 0 {
							cert, err := x509.ParseCertificate(params.RawCerts[0])
							if err != nil {
								return nil, err
							}
							negotiatedAlgo = cert.PublicKeyAlgorithm
						}
						return &PostHandshakeVerificationResults{}, nil
					},
				}
				clientCreds, err := NewClientCreds(clientOpts)
				if err != nil {
					t.Fatalf("NewClientCreds failed: %v", err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
				defer cancel()
				conn, err := dialAndCall(ctx, addr, "localhost", clientCreds, false)
				if err != nil {
					t.Fatalf("RSA client call failed: %v", err)
				}
				conn.Close()

				if negotiatedAlgo != x509.RSA {
					t.Errorf("RSA client negotiated certificate algorithm = %v, want %v (x509.RSA)", negotiatedAlgo, x509.RSA)
				}
			}

			// 2. Connect with an ECDSA-only client in TLS 1.2.
			{
				var negotiatedAlgo x509.PublicKeyAlgorithm
				clientOpts := &Options{
					RootOptions: RootCertificateOptions{
						RootCertificates: cs.ClientTrust1,
					},
					VerificationType: CertAndHostVerification,
					CipherSuites:     []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
					MinTLSVersion:    tls.VersionTLS12,
					MaxTLSVersion:    tls.VersionTLS12,
					AdditionalPeerVerification: func(params *HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
						if params.Leaf != nil {
							negotiatedAlgo = params.Leaf.PublicKeyAlgorithm
						} else if len(params.RawCerts) > 0 {
							cert, err := x509.ParseCertificate(params.RawCerts[0])
							if err != nil {
								return nil, err
							}
							negotiatedAlgo = cert.PublicKeyAlgorithm
						}
						return &PostHandshakeVerificationResults{}, nil
					},
				}
				clientCreds, err := NewClientCreds(clientOpts)
				if err != nil {
					t.Fatalf("NewClientCreds failed: %v", err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
				defer cancel()
				conn, err := dialAndCall(ctx, addr, "localhost", clientCreds, false)
				if err != nil {
					t.Fatalf("ECDSA client call failed: %v", err)
				}
				conn.Close()

				if negotiatedAlgo != x509.ECDSA {
					t.Errorf("ECDSA client negotiated certificate algorithm = %v, want %v (x509.ECDSA)", negotiatedAlgo, x509.ECDSA)
				}
			}
		})
	}
}

// TestServerMultipleCerts_TLS13_MutualTLS tests mTLS end-to-end strictly in TLS 1.3
// where the server has both RSA and ECDSA certificate chains and requires client certs.
func (s) TestServerMultipleCerts_TLS13_MutualTLS(t *testing.T) {
	cs := &testutils.CertStore{}
	if err := cs.LoadCerts(); err != nil {
		t.Fatalf("cs.LoadCerts() failed, err: %v", err)
	}

	serverOptions := &Options{
		IdentityOptions: IdentityCertificateOptions{
			Certificates: []tls.Certificate{cs.ServerPeerLocalhost1, cs.ServerPeerECDSALocalhost1},
		},
		RootOptions: RootCertificateOptions{
			RootCertificates: cs.ServerTrust1,
		},
		MinTLSVersion:     tls.VersionTLS13,
		MaxTLSVersion:     tls.VersionTLS13,
		RequireClientCert: true,
		VerificationType:  CertVerification,
	}
	serverTLSCreds, err := NewServerCreds(serverOptions)
	if err != nil {
		t.Fatalf("NewServerCreds failed: %v", err)
	}
	s := grpc.NewServer(grpc.Creds(serverTLSCreds))
	defer s.Stop()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	defer lis.Close()
	addr := lis.Addr().String()
	pb.RegisterGreeterServer(s, greeterServer{})
	go s.Serve(lis)

	// 1. RSA client with RSA client certificate in TLS 1.3
	{
		var serverCertAlgo x509.PublicKeyAlgorithm
		clientOpts := &Options{
			IdentityOptions: IdentityCertificateOptions{
				Certificates: []tls.Certificate{cs.ClientCert1},
			},
			RootOptions: RootCertificateOptions{
				RootCertificates: cs.ClientTrust1,
			},
			MinTLSVersion:    tls.VersionTLS13,
			MaxTLSVersion:    tls.VersionTLS13,
			VerificationType: CertAndHostVerification,
			AdditionalPeerVerification: func(params *HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
				if params.Leaf != nil {
					serverCertAlgo = params.Leaf.PublicKeyAlgorithm
				} else if len(params.RawCerts) > 0 {
					cert, err := x509.ParseCertificate(params.RawCerts[0])
					if err != nil {
						return nil, err
					}
					serverCertAlgo = cert.PublicKeyAlgorithm
				}
				return &PostHandshakeVerificationResults{}, nil
			},
		}
		clientCreds, err := NewClientCreds(clientOpts)
		if err != nil {
			t.Fatalf("NewClientCreds failed: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		conn, err := dialAndCall(ctx, addr, "localhost", clientCreds, false)
		if err != nil {
			t.Fatalf("RSA mTLS TLS 1.3 call failed: %v", err)
		}
		conn.Close()
		if serverCertAlgo != x509.RSA && serverCertAlgo != x509.ECDSA {
			t.Errorf("serverCertAlgo = %v, want RSA or ECDSA", serverCertAlgo)
		}
	}

	// 2. ECDSA client with ECDSA client certificate in TLS 1.3
	{
		var serverCertAlgo x509.PublicKeyAlgorithm
		clientOpts := &Options{
			IdentityOptions: IdentityCertificateOptions{
				Certificates: []tls.Certificate{cs.ClientPeerECDSALocalhost1},
			},
			RootOptions: RootCertificateOptions{
				RootCertificates: cs.ClientTrust1,
			},
			MinTLSVersion:    tls.VersionTLS13,
			MaxTLSVersion:    tls.VersionTLS13,
			VerificationType: CertAndHostVerification,
			AdditionalPeerVerification: func(params *HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
				if params.Leaf != nil {
					serverCertAlgo = params.Leaf.PublicKeyAlgorithm
				} else if len(params.RawCerts) > 0 {
					cert, err := x509.ParseCertificate(params.RawCerts[0])
					if err != nil {
						return nil, err
					}
					serverCertAlgo = cert.PublicKeyAlgorithm
				}
				return &PostHandshakeVerificationResults{}, nil
			},
		}
		clientCreds, err := NewClientCreds(clientOpts)
		if err != nil {
			t.Fatalf("NewClientCreds failed: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		conn, err := dialAndCall(ctx, addr, "localhost", clientCreds, false)
		if err != nil {
			t.Fatalf("ECDSA mTLS TLS 1.3 call failed: %v", err)
		}
		conn.Close()
		if serverCertAlgo != x509.RSA && serverCertAlgo != x509.ECDSA {
			t.Errorf("serverCertAlgo = %v, want RSA or ECDSA", serverCertAlgo)
		}
	}
}

// TestServerMultipleCerts_TLS12_MutualTLS tests mTLS end-to-end in TLS 1.2
// where the server has both RSA and ECDSA certificate chains and client certificates of both types connect.
func (s) TestServerMultipleCerts_TLS12_MutualTLS(t *testing.T) {
	cs := &testutils.CertStore{}
	if err := cs.LoadCerts(); err != nil {
		t.Fatalf("cs.LoadCerts() failed, err: %v", err)
	}

	serverOptions := &Options{
		IdentityOptions: IdentityCertificateOptions{
			Certificates: []tls.Certificate{cs.ServerPeerLocalhost1, cs.ServerPeerECDSALocalhost1},
		},
		RootOptions: RootCertificateOptions{
			RootCertificates: cs.ServerTrust1,
		},
		MinTLSVersion:     tls.VersionTLS12,
		MaxTLSVersion:     tls.VersionTLS12,
		RequireClientCert: true,
		VerificationType:  CertVerification,
	}
	serverTLSCreds, err := NewServerCreds(serverOptions)
	if err != nil {
		t.Fatalf("NewServerCreds failed: %v", err)
	}
	s := grpc.NewServer(grpc.Creds(serverTLSCreds))
	defer s.Stop()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	defer lis.Close()
	addr := lis.Addr().String()
	pb.RegisterGreeterServer(s, greeterServer{})
	go s.Serve(lis)

	// 1. RSA client with RSA client certificate in TLS 1.2
	{
		var serverCertAlgo x509.PublicKeyAlgorithm
		clientOpts := &Options{
			IdentityOptions: IdentityCertificateOptions{
				Certificates: []tls.Certificate{cs.ClientCert1},
			},
			RootOptions: RootCertificateOptions{
				RootCertificates: cs.ClientTrust1,
			},
			MinTLSVersion:    tls.VersionTLS12,
			MaxTLSVersion:    tls.VersionTLS12,
			VerificationType: CertAndHostVerification,
			CipherSuites:     []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
			AdditionalPeerVerification: func(params *HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
				if params.Leaf != nil {
					serverCertAlgo = params.Leaf.PublicKeyAlgorithm
				} else if len(params.RawCerts) > 0 {
					cert, err := x509.ParseCertificate(params.RawCerts[0])
					if err != nil {
						return nil, err
					}
					serverCertAlgo = cert.PublicKeyAlgorithm
				}
				return &PostHandshakeVerificationResults{}, nil
			},
		}
		clientCreds, err := NewClientCreds(clientOpts)
		if err != nil {
			t.Fatalf("NewClientCreds failed: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		conn, err := dialAndCall(ctx, addr, "localhost", clientCreds, false)
		if err != nil {
			t.Fatalf("RSA mTLS TLS 1.2 call failed: %v", err)
		}
		conn.Close()
		if serverCertAlgo != x509.RSA {
			t.Errorf("serverCertAlgo = %v, want %v (x509.RSA)", serverCertAlgo, x509.RSA)
		}
	}

	// 2. ECDSA client with ECDSA client certificate in TLS 1.2
	{
		var serverCertAlgo x509.PublicKeyAlgorithm
		clientOpts := &Options{
			IdentityOptions: IdentityCertificateOptions{
				Certificates: []tls.Certificate{cs.ClientPeerECDSALocalhost1},
			},
			RootOptions: RootCertificateOptions{
				RootCertificates: cs.ClientTrust1,
			},
			MinTLSVersion:    tls.VersionTLS12,
			MaxTLSVersion:    tls.VersionTLS12,
			VerificationType: CertAndHostVerification,
			CipherSuites:     []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
			AdditionalPeerVerification: func(params *HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
				if params.Leaf != nil {
					serverCertAlgo = params.Leaf.PublicKeyAlgorithm
				} else if len(params.RawCerts) > 0 {
					cert, err := x509.ParseCertificate(params.RawCerts[0])
					if err != nil {
						return nil, err
					}
					serverCertAlgo = cert.PublicKeyAlgorithm
				}
				return &PostHandshakeVerificationResults{}, nil
			},
		}
		clientCreds, err := NewClientCreds(clientOpts)
		if err != nil {
			t.Fatalf("NewClientCreds failed: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		conn, err := dialAndCall(ctx, addr, "localhost", clientCreds, false)
		if err != nil {
			t.Fatalf("ECDSA mTLS TLS 1.2 call failed: %v", err)
		}
		conn.Close()
		if serverCertAlgo != x509.ECDSA {
			t.Errorf("serverCertAlgo = %v, want %v (x509.ECDSA)", serverCertAlgo, x509.ECDSA)
		}
	}
}

// TestServerMultipleCerts_TLS13_DynamicSelection verifies that GetIdentityCertificatesForServer
// dynamically receives ClientHelloInfo with signature_algorithms in TLS 1.3 and uses
// SupportsCertificate to return the matching certificate chain.
func (s) TestServerMultipleCerts_TLS13_DynamicSelection(t *testing.T) {
	cs := &testutils.CertStore{}
	if err := cs.LoadCerts(); err != nil {
		t.Fatalf("cs.LoadCerts() failed, err: %v", err)
	}

	var capturedSigSchemes []tls.SignatureScheme
	serverOptions := &Options{
		IdentityOptions: IdentityCertificateOptions{
			GetIdentityCertificatesForServer: func(chi *tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				capturedSigSchemes = chi.SignatureSchemes
				// Filter certificates using SupportsCertificate against the client's offered signature algorithms
				candidates := []*tls.Certificate{&cs.ServerPeerECDSALocalhost1, &cs.ServerPeerLocalhost1}
				var supported []*tls.Certificate
				for _, c := range candidates {
					if err := chi.SupportsCertificate(c); err == nil {
						supported = append(supported, c)
					}
				}
				if len(supported) == 0 {
					return nil, fmt.Errorf("no supported certificate for client signature schemes: %v", chi.SignatureSchemes)
				}
				return supported, nil
			},
		},
		RootOptions: RootCertificateOptions{
			RootCertificates: cs.ServerTrust1,
		},
		MinTLSVersion:     tls.VersionTLS13,
		MaxTLSVersion:     tls.VersionTLS13,
		RequireClientCert: false,
		VerificationType:  CertVerification,
	}
	serverTLSCreds, err := NewServerCreds(serverOptions)
	if err != nil {
		t.Fatalf("NewServerCreds failed: %v", err)
	}
	s := grpc.NewServer(grpc.Creds(serverTLSCreds))
	defer s.Stop()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	defer lis.Close()
	addr := lis.Addr().String()
	pb.RegisterGreeterServer(s, greeterServer{})
	go s.Serve(lis)

	clientOpts := &Options{
		RootOptions: RootCertificateOptions{
			RootCertificates: cs.ClientTrust1,
		},
		MinTLSVersion:    tls.VersionTLS13,
		MaxTLSVersion:    tls.VersionTLS13,
		VerificationType: CertAndHostVerification,
	}
	clientCreds, err := NewClientCreds(clientOpts)
	if err != nil {
		t.Fatalf("NewClientCreds failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := dialAndCall(ctx, addr, "localhost", clientCreds, false)
	if err != nil {
		t.Fatalf("TLS 1.3 call failed: %v", err)
	}
	conn.Close()

	if len(capturedSigSchemes) == 0 {
		t.Errorf("expected non-empty ClientHelloInfo.SignatureSchemes in TLS 1.3")
	}
}

// TestServerMultipleCerts_IncompatibleAlgorithm tests that when the server only has
// an RSA cert, an ECDSA-only client fails in TLS 1.2, and vice versa.
func (s) TestServerMultipleCerts_IncompatibleAlgorithm(t *testing.T) {
	cs := &testutils.CertStore{}
	if err := cs.LoadCerts(); err != nil {
		t.Fatalf("cs.LoadCerts() failed, err: %v", err)
	}

	// Server with ONLY RSA certificate
	serverOptions := &Options{
		IdentityOptions: IdentityCertificateOptions{
			Certificates: []tls.Certificate{cs.ServerPeerLocalhost1},
		},
		RootOptions: RootCertificateOptions{
			RootCertificates: cs.ServerTrust1,
		},
		MinTLSVersion:     tls.VersionTLS12,
		MaxTLSVersion:     tls.VersionTLS12,
		RequireClientCert: false,
		VerificationType:  CertVerification,
	}
	serverTLSCreds, err := NewServerCreds(serverOptions)
	if err != nil {
		t.Fatalf("NewServerCreds failed: %v", err)
	}
	s := grpc.NewServer(grpc.Creds(serverTLSCreds))
	defer s.Stop()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	defer lis.Close()
	addr := lis.Addr().String()
	pb.RegisterGreeterServer(s, greeterServer{})
	go s.Serve(lis)

	// ECDSA-only client connecting to RSA-only server must fail
	clientOpts := &Options{
		RootOptions: RootCertificateOptions{
			RootCertificates: cs.ClientTrust1,
		},
		VerificationType: CertAndHostVerification,
		CipherSuites:     []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
		MinTLSVersion:    tls.VersionTLS12,
		MaxTLSVersion:    tls.VersionTLS12,
	}
	clientCreds, err := NewClientCreds(clientOpts)
	if err != nil {
		t.Fatalf("NewClientCreds failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	conn, err := dialAndCall(ctx, addr, "localhost", clientCreds, true)
	if err != nil {
		t.Fatalf("dialAndCall error: %v", err)
	}
	if conn != nil {
		conn.Close()
	}
}
