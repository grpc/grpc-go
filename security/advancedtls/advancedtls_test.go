/*
 *
 * Copyright 2019 gRPC authors.
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
	"errors"
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/security/advancedtls/internal/testutils"
	"google.golang.org/grpc/security/advancedtls/testdata"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type provType int

const (
	provTypeRoot provType = iota
	provTypeIdentity
)

type fakeProvider struct {
	pt            provType
	isClient      bool
	wantMultiCert bool
	wantError     bool
}

func (f fakeProvider) KeyMaterial(context.Context) (*certprovider.KeyMaterial, error) {
	if f.wantError {
		return nil, fmt.Errorf("bad fakeProvider")
	}
	cs := &testutils.CertStore{}
	if err := cs.LoadCerts(); err != nil {
		return nil, fmt.Errorf("cs.LoadCerts() failed, err: %v", err)
	}
	if f.pt == provTypeRoot && f.isClient {
		return &certprovider.KeyMaterial{Roots: cs.ClientTrust1}, nil
	}
	if f.pt == provTypeRoot && !f.isClient {
		return &certprovider.KeyMaterial{Roots: cs.ServerTrust1}, nil
	}
	if f.pt == provTypeIdentity && f.isClient {
		if f.wantMultiCert {
			return &certprovider.KeyMaterial{Certs: []tls.Certificate{cs.ClientCert1, cs.ClientCert2}}, nil
		}
		return &certprovider.KeyMaterial{Certs: []tls.Certificate{cs.ClientCert1}}, nil
	}
	if f.wantMultiCert {
		return &certprovider.KeyMaterial{Certs: []tls.Certificate{cs.ServerCert1, cs.ServerCert2}}, nil
	}
	return &certprovider.KeyMaterial{Certs: []tls.Certificate{cs.ServerCert1}}, nil
}

func (f fakeProvider) Close() {}

func (s) TestClientOptionsConfigErrorCases(t *testing.T) {
	tests := []struct {
		desc                   string
		clientVerificationType VerificationType
		IdentityOptions        IdentityCertificateOptions
		RootOptions            RootCertificateOptions
		MinVersion             uint16
		MaxVersion             uint16
	}{
		{
			desc:                   "Skip default verification and provide no root credentials",
			clientVerificationType: SkipVerification,
		},
		{
			desc:                   "More than one fields in RootCertificateOptions is specified",
			clientVerificationType: CertVerification,
			RootOptions: RootCertificateOptions{
				RootCertificates: x509.NewCertPool(),
				RootProvider:     fakeProvider{},
			},
		},
		{
			desc:                   "More than one fields in IdentityCertificateOptions is specified",
			clientVerificationType: CertVerification,
			IdentityOptions: IdentityCertificateOptions{
				GetIdentityCertificatesForClient: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
					return nil, nil
				},
				IdentityProvider: fakeProvider{pt: provTypeIdentity},
			},
		},
		{
			desc: "Specify GetIdentityCertificatesForServer",
			IdentityOptions: IdentityCertificateOptions{
				GetIdentityCertificatesForServer: func(*tls.ClientHelloInfo) ([]*tls.Certificate, error) {
					return nil, nil
				},
			},
		},
		{
			desc:       "Invalid min/max TLS versions",
			MinVersion: tls.VersionTLS13,
			MaxVersion: tls.VersionTLS12,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			clientOptions := &Options{
				VerificationType: test.clientVerificationType,
				IdentityOptions:  test.IdentityOptions,
				RootOptions:      test.RootOptions,
				MinTLSVersion:    test.MinVersion,
				MaxTLSVersion:    test.MaxVersion,
			}
			_, err := clientOptions.clientConfig()
			if err == nil {
				t.Fatalf("ClientOptions{%v}.config() returns no err, wantErr != nil", clientOptions)
			}
		})
	}
}

func (s) TestClientOptionsConfigSuccessCases(t *testing.T) {
	tests := []struct {
		desc                   string
		clientVerificationType VerificationType
		IdentityOptions        IdentityCertificateOptions
		RootOptions            RootCertificateOptions
		MinVersion             uint16
		MaxVersion             uint16
		cipherSuites           []uint16
	}{
		{
			desc:                   "Use system default if no fields in RootCertificateOptions is specified",
			clientVerificationType: CertVerification,
		},
		{
			desc:                   "Good case with mutual TLS",
			clientVerificationType: CertVerification,
			RootOptions: RootCertificateOptions{
				RootProvider: fakeProvider{},
			},
			IdentityOptions: IdentityCertificateOptions{
				IdentityProvider: fakeProvider{pt: provTypeIdentity},
			},
			MinVersion: tls.VersionTLS12,
			MaxVersion: tls.VersionTLS13,
		},
		{
			desc: "Ciphersuite plumbing through client options",
			cipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			clientOptions := &Options{
				VerificationType: test.clientVerificationType,
				IdentityOptions:  test.IdentityOptions,
				RootOptions:      test.RootOptions,
				MinTLSVersion:    test.MinVersion,
				MaxTLSVersion:    test.MaxVersion,
				CipherSuites:     test.cipherSuites,
			}
			clientConfig, err := clientOptions.clientConfig()
			if err != nil {
				t.Fatalf("ClientOptions{%v}.config() = %v, wantErr == nil", clientOptions, err)
			}
			// Verify that the system-provided certificates would be used
			// when no verification method was set in clientOptions.
			if clientOptions.RootOptions.RootCertificates == nil &&
				clientOptions.RootOptions.GetRootCertificates == nil && clientOptions.RootOptions.RootProvider == nil {
				if clientConfig.RootCAs == nil {
					t.Fatalf("Failed to assign system-provided certificates on the client side.")
				}
			}
			if test.MinVersion != 0 {
				if clientConfig.MinVersion != test.MinVersion {
					t.Fatalf("Failed to assign min tls version.")
				}
			} else {
				if clientConfig.MinVersion != tls.VersionTLS12 {
					t.Fatalf("Default min tls version not set correctly")
				}
			}
			if test.MaxVersion != 0 {
				if clientConfig.MaxVersion != test.MaxVersion {
					t.Fatalf("Failed to assign max tls version.")
				}
			} else {
				if clientConfig.MaxVersion != tls.VersionTLS13 {
					t.Fatalf("Default max tls version not set correctly")
				}
			}
			if diff := cmp.Diff(clientConfig.CipherSuites, test.cipherSuites); diff != "" {
				t.Errorf("cipherSuites diff (-want +got):\n%s", diff)
			}
		})
	}
}

func (s) TestServerOptionsConfigErrorCases(t *testing.T) {
	tests := []struct {
		desc                   string
		requireClientCert      bool
		serverVerificationType VerificationType
		IdentityOptions        IdentityCertificateOptions
		RootOptions            RootCertificateOptions
		MinVersion             uint16
		MaxVersion             uint16
	}{
		{
			desc:                   "Skip default verification and provide no root credentials",
			requireClientCert:      true,
			serverVerificationType: SkipVerification,
		},
		{
			desc:                   "More than one fields in RootCertificateOptions is specified",
			requireClientCert:      true,
			serverVerificationType: CertVerification,
			RootOptions: RootCertificateOptions{
				RootCertificates: x509.NewCertPool(),
				GetRootCertificates: func(*ConnectionInfo) (*RootCertificates, error) {
					return nil, nil
				},
			},
		},
		{
			desc:                   "More than one fields in IdentityCertificateOptions is specified",
			serverVerificationType: CertVerification,
			IdentityOptions: IdentityCertificateOptions{
				Certificates:     []tls.Certificate{},
				IdentityProvider: fakeProvider{pt: provTypeIdentity},
			},
		},
		{
			desc:                   "no field in IdentityCertificateOptions is specified",
			serverVerificationType: CertVerification,
		},
		{
			desc: "Specify GetIdentityCertificatesForClient",
			IdentityOptions: IdentityCertificateOptions{
				GetIdentityCertificatesForClient: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
					return nil, nil
				},
			},
		},
		{
			desc:       "Invalid min/max TLS versions",
			MinVersion: tls.VersionTLS13,
			MaxVersion: tls.VersionTLS12,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			serverOptions := &Options{
				VerificationType:  test.serverVerificationType,
				RequireClientCert: test.requireClientCert,
				IdentityOptions:   test.IdentityOptions,
				RootOptions:       test.RootOptions,
				MinTLSVersion:     test.MinVersion,
				MaxTLSVersion:     test.MaxVersion,
			}
			_, err := serverOptions.serverConfig()
			if err == nil {
				t.Fatalf("ServerOptions{%v}.serverConfig() returns no err, wantErr != nil", serverOptions)
			}
		})
	}
}

func (s) TestServerOptionsConfigSuccessCases(t *testing.T) {
	tests := []struct {
		desc                   string
		requireClientCert      bool
		serverVerificationType VerificationType
		IdentityOptions        IdentityCertificateOptions
		RootOptions            RootCertificateOptions
		MinVersion             uint16
		MaxVersion             uint16
		cipherSuites           []uint16
	}{
		{
			desc:                   "Use system default if no fields in RootCertificateOptions is specified",
			requireClientCert:      true,
			serverVerificationType: CertVerification,
			IdentityOptions: IdentityCertificateOptions{
				Certificates: []tls.Certificate{},
			},
		},
		{
			desc:                   "Good case with mutual TLS",
			requireClientCert:      true,
			serverVerificationType: CertVerification,
			RootOptions: RootCertificateOptions{
				RootProvider: fakeProvider{},
			},
			IdentityOptions: IdentityCertificateOptions{
				GetIdentityCertificatesForServer: func(*tls.ClientHelloInfo) ([]*tls.Certificate, error) {
					return nil, nil
				},
			},
			MinVersion: tls.VersionTLS12,
			MaxVersion: tls.VersionTLS13,
		},
		{
			desc: "Ciphersuite plumbing through server options",
			IdentityOptions: IdentityCertificateOptions{
				Certificates: []tls.Certificate{},
			},
			RootOptions: RootCertificateOptions{
				RootCertificates: x509.NewCertPool(),
			},
			cipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			serverOptions := &Options{
				VerificationType:  test.serverVerificationType,
				RequireClientCert: test.requireClientCert,
				IdentityOptions:   test.IdentityOptions,
				RootOptions:       test.RootOptions,
				MinTLSVersion:     test.MinVersion,
				MaxTLSVersion:     test.MaxVersion,
				CipherSuites:      test.cipherSuites,
			}
			serverConfig, err := serverOptions.serverConfig()
			if err != nil {
				t.Fatalf("ServerOptions{%v}.config() = %v, wantErr == nil", serverOptions, err)
			}
			// Verify that the system-provided certificates would be used
			// when no verification method was set in serverOptions.
			if serverOptions.RootOptions.RootCertificates == nil &&
				serverOptions.RootOptions.GetRootCertificates == nil && serverOptions.RootOptions.RootProvider == nil {
				if serverConfig.ClientCAs == nil {
					t.Fatalf("Failed to assign system-provided certificates on the server side.")
				}
			}
			if diff := cmp.Diff(serverConfig.CipherSuites, test.cipherSuites); diff != "" {
				t.Errorf("cipherSuites diff (-want +got):\n%s", diff)
			}
		})
	}
}

func (s) TestClientServerHandshake(t *testing.T) {
	cs := &testutils.CertStore{}
	if err := cs.LoadCerts(); err != nil {
		t.Fatalf("cs.LoadCerts() failed, err: %v", err)
	}
	getRootCertificatesForClient := func(*ConnectionInfo) (*RootCertificates, error) {
		return &RootCertificates{TrustCerts: cs.ClientTrust1}, nil
	}

	clientVerifyFuncGood := func(params *HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
		if params.ServerName == "" {
			return nil, errors.New("client side server name should have a value")
		}
		// "foo.bar.com" is the common name on server certificate server_cert_1.pem.
		if len(params.VerifiedChains) > 0 && (params.Leaf == nil || params.Leaf.Subject.CommonName != "foo.bar.com") {
			return nil, errors.New("client side params parsing error")
		}

		return &PostHandshakeVerificationResults{}, nil
	}
	verifyFuncBad := func(*HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
		return nil, fmt.Errorf("custom verification function failed")
	}
	getRootCertificatesForServer := func(*ConnectionInfo) (*RootCertificates, error) {
		return &RootCertificates{TrustCerts: cs.ServerTrust1}, nil
	}
	serverVerifyFunc := func(params *HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
		if params.ServerName != "" {
			return nil, errors.New("server side server name should not have a value")
		}
		// "foo.bar.hoo.com" is the common name on client certificate client_cert_1.pem.
		if len(params.VerifiedChains) > 0 && (params.Leaf == nil || params.Leaf.Subject.CommonName != "foo.bar.hoo.com") {
			return nil, errors.New("server side params parsing error")
		}

		return &PostHandshakeVerificationResults{}, nil
	}
	getRootCertificatesForServerBad := func(*ConnectionInfo) (*RootCertificates, error) {
		return nil, fmt.Errorf("bad root certificate reloading")
	}

	getRootCertificatesForClientCRL := func(*ConnectionInfo) (*RootCertificates, error) {
		return &RootCertificates{TrustCerts: cs.ClientTrust3}, nil
	}

	getRootCertificatesForServerCRL := func(*ConnectionInfo) (*RootCertificates, error) {
		return &RootCertificates{TrustCerts: cs.ServerTrust3}, nil
	}

	makeStaticCRLRevocationOptions := func(crlPath string, denyUndetermined bool) *RevocationOptions {
		rawCRL, err := os.ReadFile(crlPath)
		if err != nil {
			t.Fatalf("readFile(%v) failed err = %v", crlPath, err)
		}
		cRLProvider := NewStaticCRLProvider([][]byte{rawCRL})
		return &RevocationOptions{
			DenyUndetermined: denyUndetermined,
			CRLProvider:      cRLProvider,
		}
	}

	for _, test := range []struct {
		desc                       string
		clientCert                 []tls.Certificate
		clientGetCert              func(*tls.CertificateRequestInfo) (*tls.Certificate, error)
		clientRoot                 *x509.CertPool
		clientGetRoot              func(params *ConnectionInfo) (*RootCertificates, error)
		clientVerifyFunc           PostHandshakeVerificationFunc
		clientVerificationType     VerificationType
		clientRootProvider         certprovider.Provider
		clientIdentityProvider     certprovider.Provider
		clientRevocationOptions    *RevocationOptions
		clientExpectHandshakeError bool
		serverMutualTLS            bool
		serverCert                 []tls.Certificate
		serverGetCert              func(*tls.ClientHelloInfo) ([]*tls.Certificate, error)
		serverRoot                 *x509.CertPool
		serverGetRoot              func(params *ConnectionInfo) (*RootCertificates, error)
		serverVerifyFunc           PostHandshakeVerificationFunc
		serverVerificationType     VerificationType
		serverRootProvider         certprovider.Provider
		serverIdentityProvider     certprovider.Provider
		serverRevocationOptions    *RevocationOptions
		serverExpectError          bool
	}{
		// Client: nil setting except verifyFuncGood
		// Server: only set serverCert with mutual TLS off
		// Expected Behavior: success
		// Reason: we will use verifyFuncGood to verify the server,
		// if either clientCert or clientGetCert is not set
		{
			desc:                   "Client has no trust cert with verifyFuncGood; server sends peer cert",
			clientVerifyFunc:       clientVerifyFuncGood,
			clientVerificationType: SkipVerification,
			serverCert:             []tls.Certificate{cs.ServerCert1},
			serverVerificationType: CertAndHostVerification,
		},
		// Client: set clientGetRoot and clientVerifyFunc
		// Server: only set serverCert with mutual TLS off
		// Expected Behavior: success
		{
			desc:                   "Client sets reload root function with verifyFuncGood; server sends peer cert",
			clientGetRoot:          getRootCertificatesForClient,
			clientVerifyFunc:       clientVerifyFuncGood,
			clientVerificationType: CertVerification,
			serverCert:             []tls.Certificate{cs.ServerCert1},
			serverVerificationType: CertAndHostVerification,
		},
		// Client: set clientGetRoot and bad clientVerifyFunc function
		// Server: only set serverCert with mutual TLS off
		// Expected Behavior: server side failure and client handshake failure
		// Reason: custom verification function is bad
		{
			desc:                       "Client sets reload root function with verifyFuncBad; server sends peer cert",
			clientGetRoot:              getRootCertificatesForClient,
			clientVerifyFunc:           verifyFuncBad,
			clientVerificationType:     CertVerification,
			clientExpectHandshakeError: true,
			serverCert:                 []tls.Certificate{cs.ServerCert1},
			serverVerificationType:     CertVerification,
			serverExpectError:          true,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientCert
		// Server: set serverRoot and serverCert with mutual TLS on
		// Expected Behavior: success
		{
			desc:                   "Client sets peer cert, reload root function with verifyFuncGood; server sets peer cert and root cert; mutualTLS",
			clientCert:             []tls.Certificate{cs.ClientCert1},
			clientGetRoot:          getRootCertificatesForClient,
			clientVerifyFunc:       clientVerifyFuncGood,
			clientVerificationType: CertVerification,
			serverMutualTLS:        true,
			serverCert:             []tls.Certificate{cs.ServerCert1},
			serverRoot:             cs.ServerTrust1,
			serverVerificationType: CertVerification,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientCert
		// Server: set serverGetRoot and serverCert with mutual TLS on
		// Expected Behavior: success
		{
			desc:                   "Client sets peer cert, reload root function with verifyFuncGood; Server sets peer cert, reload root function; mutualTLS",
			clientCert:             []tls.Certificate{cs.ClientCert1},
			clientGetRoot:          getRootCertificatesForClient,
			clientVerifyFunc:       clientVerifyFuncGood,
			clientVerificationType: CertVerification,
			serverMutualTLS:        true,
			serverCert:             []tls.Certificate{cs.ServerCert1},
			serverGetRoot:          getRootCertificatesForServer,
			serverVerificationType: CertVerification,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientCert
		// Server: set serverGetRoot returning error and serverCert with mutual
		// TLS on
		// Expected Behavior: server side failure
		// Reason: server side reloading returns failure
		{
			desc:                   "Client sets peer cert, reload root function with verifyFuncGood; Server sets peer cert, bad reload root function; mutualTLS",
			clientCert:             []tls.Certificate{cs.ClientCert1},
			clientGetRoot:          getRootCertificatesForClient,
			clientVerifyFunc:       clientVerifyFuncGood,
			clientVerificationType: CertVerification,
			serverMutualTLS:        true,
			serverCert:             []tls.Certificate{cs.ServerCert1},
			serverGetRoot:          getRootCertificatesForServerBad,
			serverVerificationType: CertVerification,
			serverExpectError:      true,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientGetCert
		// Server: set serverGetRoot and serverGetCert with mutual TLS on
		// Expected Behavior: success
		{
			desc: "Client sets reload peer/root function with verifyFuncGood; Server sets reload peer/root function with verifyFuncGood; mutualTLS",
			clientGetCert: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return &cs.ClientCert1, nil
			},
			clientGetRoot:          getRootCertificatesForClient,
			clientVerifyFunc:       clientVerifyFuncGood,
			clientVerificationType: CertVerification,
			serverMutualTLS:        true,
			serverGetCert: func(*tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				return []*tls.Certificate{&cs.ServerCert1}, nil
			},
			serverGetRoot:          getRootCertificatesForServer,
			serverVerifyFunc:       serverVerifyFunc,
			serverVerificationType: CertVerification,
		},
		// Client: set everything but with the wrong peer cert not trusted by
		// server
		// Server: set serverGetRoot and serverGetCert with mutual TLS on
		// Expected Behavior: server side returns failure because of
		// certificate mismatch
		{
			desc: "Client sends wrong peer cert; Server sets reload peer/root function with verifyFuncGood; mutualTLS",
			clientGetCert: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return &cs.ServerCert1, nil
			},
			clientGetRoot:          getRootCertificatesForClient,
			clientVerifyFunc:       clientVerifyFuncGood,
			clientVerificationType: CertVerification,
			serverMutualTLS:        true,
			serverGetCert: func(*tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				return []*tls.Certificate{&cs.ServerCert1}, nil
			},
			serverGetRoot:          getRootCertificatesForServer,
			serverVerifyFunc:       serverVerifyFunc,
			serverVerificationType: CertVerification,
			serverExpectError:      true,
		},
		// Client: set everything but with the wrong trust cert not trusting server
		// Server: set serverGetRoot and serverGetCert with mutual TLS on
		// Expected Behavior: server side and client side return failure due to
		// certificate mismatch and handshake failure
		{
			desc: "Client has wrong trust cert; Server sets reload peer/root function with verifyFuncGood; mutualTLS",
			clientGetCert: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return &cs.ClientCert1, nil
			},
			clientGetRoot:              getRootCertificatesForServer,
			clientVerifyFunc:           clientVerifyFuncGood,
			clientVerificationType:     CertVerification,
			clientExpectHandshakeError: true,
			serverMutualTLS:            true,
			serverGetCert: func(*tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				return []*tls.Certificate{&cs.ServerCert1}, nil
			},
			serverGetRoot:          getRootCertificatesForServer,
			serverVerifyFunc:       serverVerifyFunc,
			serverVerificationType: CertVerification,
			serverExpectError:      true,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientCert
		// Server: set everything but with the wrong peer cert not trusted by
		// client
		// Expected Behavior: server side and client side return failure due to
		// certificate mismatch and handshake failure
		{
			desc: "Client sets reload peer/root function with verifyFuncGood; Server sends wrong peer cert; mutualTLS",
			clientGetCert: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return &cs.ClientCert1, nil
			},
			clientGetRoot:          getRootCertificatesForClient,
			clientVerifyFunc:       clientVerifyFuncGood,
			clientVerificationType: CertVerification,
			serverMutualTLS:        true,
			serverGetCert: func(*tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				return []*tls.Certificate{&cs.ClientCert1}, nil
			},
			serverGetRoot:          getRootCertificatesForServer,
			serverVerifyFunc:       serverVerifyFunc,
			serverVerificationType: CertVerification,
			serverExpectError:      true,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientCert
		// Server: set everything but with the wrong trust cert not trusting client
		// Expected Behavior: server side and client side return failure due to
		// certificate mismatch and handshake failure
		{
			desc: "Client sets reload peer/root function with verifyFuncGood; Server has wrong trust cert; mutualTLS",
			clientGetCert: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return &cs.ClientCert1, nil
			},
			clientGetRoot:              getRootCertificatesForClient,
			clientVerifyFunc:           clientVerifyFuncGood,
			clientVerificationType:     CertVerification,
			clientExpectHandshakeError: true,
			serverMutualTLS:            true,
			serverGetCert: func(*tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				return []*tls.Certificate{&cs.ServerCert1}, nil
			},
			serverGetRoot:          getRootCertificatesForClient,
			serverVerifyFunc:       serverVerifyFunc,
			serverVerificationType: CertVerification,
			serverExpectError:      true,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientCert
		// Server: set serverGetRoot and serverCert, but with bad verifyFunc
		// Expected Behavior: server side and client side return failure due to
		// server custom check fails
		{
			desc:                       "Client sets peer cert, reload root function with verifyFuncGood; Server sets bad custom check; mutualTLS",
			clientCert:                 []tls.Certificate{cs.ClientCert1},
			clientGetRoot:              getRootCertificatesForClient,
			clientVerifyFunc:           clientVerifyFuncGood,
			clientVerificationType:     CertVerification,
			clientExpectHandshakeError: true,
			serverMutualTLS:            true,
			serverCert:                 []tls.Certificate{cs.ServerCert1},
			serverGetRoot:              getRootCertificatesForServer,
			serverVerifyFunc:           verifyFuncBad,
			serverVerificationType:     CertVerification,
			serverExpectError:          true,
		},
		// Client: set a clientIdentityProvider which will get multiple cert chains
		// Server: set serverIdentityProvider and serverRootProvider with mutual TLS on
		// Expected Behavior: server side failure due to multiple cert chains in
		// clientIdentityProvider
		{
			desc:                   "Client sets multiple certs in clientIdentityProvider; Server sets root and identity provider; mutualTLS",
			clientIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: true, wantMultiCert: true},
			clientRootProvider:     fakeProvider{isClient: true},
			clientVerifyFunc:       clientVerifyFuncGood,
			clientVerificationType: CertVerification,
			serverMutualTLS:        true,
			serverIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: false},
			serverRootProvider:     fakeProvider{isClient: false},
			serverVerificationType: CertVerification,
			serverExpectError:      true,
		},
		// Client: set a bad clientIdentityProvider
		// Server: set serverIdentityProvider and serverRootProvider with mutual TLS on
		// Expected Behavior: server side failure due to bad clientIdentityProvider
		{
			desc:                   "Client sets bad clientIdentityProvider; Server sets root and identity provider; mutualTLS",
			clientIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: true, wantError: true},
			clientRootProvider:     fakeProvider{isClient: true},
			clientVerifyFunc:       clientVerifyFuncGood,
			clientVerificationType: CertVerification,
			serverMutualTLS:        true,
			serverIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: false},
			serverRootProvider:     fakeProvider{isClient: false},
			serverVerificationType: CertVerification,
			serverExpectError:      true,
		},
		// Client: set clientIdentityProvider and clientRootProvider
		// Server: set bad serverRootProvider with mutual TLS on
		// Expected Behavior: server side failure due to bad serverRootProvider
		{
			desc:                   "Client sets root and identity provider; Server sets bad root provider; mutualTLS",
			clientIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: true},
			clientRootProvider:     fakeProvider{isClient: true},
			clientVerifyFunc:       clientVerifyFuncGood,
			clientVerificationType: CertVerification,
			serverMutualTLS:        true,
			serverIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: false},
			serverRootProvider:     fakeProvider{isClient: false, wantError: true},
			serverVerificationType: CertVerification,
			serverExpectError:      true,
		},
		// Client: set clientIdentityProvider and clientRootProvider
		// Server: set serverIdentityProvider and serverRootProvider with mutual TLS on
		// Expected Behavior: success
		{
			desc:                   "Client sets root and identity provider; Server sets root and identity provider; mutualTLS",
			clientIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: true},
			clientRootProvider:     fakeProvider{isClient: true},
			clientVerifyFunc:       clientVerifyFuncGood,
			clientVerificationType: CertVerification,
			serverMutualTLS:        true,
			serverIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: false},
			serverRootProvider:     fakeProvider{isClient: false},
			serverVerificationType: CertVerification,
		},
		// Client: set clientIdentityProvider and clientRootProvider
		// Server: set serverIdentityProvider getting multiple cert chains and serverRootProvider with mutual TLS on
		// Expected Behavior: success, because server side has SNI
		{
			desc:                   "Client sets root and identity provider; Server sets multiple certs in serverIdentityProvider; mutualTLS",
			clientIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: true},
			clientRootProvider:     fakeProvider{isClient: true},
			clientVerifyFunc:       clientVerifyFuncGood,
			clientVerificationType: CertVerification,
			serverMutualTLS:        true,
			serverIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: false, wantMultiCert: true},
			serverRootProvider:     fakeProvider{isClient: false},
			serverVerificationType: CertVerification,
		},
		// Client: set valid credentials with the revocation config
		// Server: set valid credentials with the revocation config
		// Expected Behavior: success, because none of the certificate chains sent in the connection are revoked
		{
			desc:                    "Client sets peer cert, reload root function with verifyFuncGood; Server sets peer cert, reload root function; Client uses CRL; mutualTLS",
			clientCert:              []tls.Certificate{cs.ClientCertForCRL},
			clientGetRoot:           getRootCertificatesForClientCRL,
			clientVerifyFunc:        clientVerifyFuncGood,
			clientVerificationType:  CertVerification,
			clientRevocationOptions: makeStaticCRLRevocationOptions(testdata.Path("crl/provider_crl_empty.pem"), false),
			serverMutualTLS:         true,
			serverCert:              []tls.Certificate{cs.ServerCertForCRL},
			serverGetRoot:           getRootCertificatesForServerCRL,
			serverVerificationType:  CertVerification,
		},
		// Client: set valid credentials with the revocation config
		// Server: set revoked credentials with the revocation config
		// Expected Behavior: fail, server creds are revoked
		{
			desc:                    "Client sets peer cert, reload root function with verifyFuncGood; Server sets revoked cert; Client uses CRL; mutualTLS",
			clientCert:              []tls.Certificate{cs.ClientCertForCRL},
			clientGetRoot:           getRootCertificatesForClientCRL,
			clientVerifyFunc:        clientVerifyFuncGood,
			clientVerificationType:  CertVerification,
			clientRevocationOptions: makeStaticCRLRevocationOptions(testdata.Path("crl/provider_crl_server_revoked.pem"), true),
			serverMutualTLS:         true,
			serverCert:              []tls.Certificate{cs.ServerCertForCRL},
			serverGetRoot:           getRootCertificatesForServerCRL,
			serverVerificationType:  CertVerification,
			serverExpectError:       true,
		},
		// Client: set valid credentials with the revocation config
		// Server: set valid credentials with the revocation config
		// Expected Behavior: fail, because CRL is issued by the malicious CA. It
		// can't be properly processed, and we don't allow RevocationUndetermined.
		{
			desc:                    "Client sets peer cert, reload root function with verifyFuncGood; Server sets peer cert, reload root function; Client uses CRL; mutualTLS",
			clientCert:              []tls.Certificate{cs.ClientCertForCRL},
			clientGetRoot:           getRootCertificatesForClientCRL,
			clientVerifyFunc:        clientVerifyFuncGood,
			clientVerificationType:  CertVerification,
			clientRevocationOptions: makeStaticCRLRevocationOptions(testdata.Path("crl/provider_malicious_crl_empty.pem"), true),
			serverMutualTLS:         true,
			serverCert:              []tls.Certificate{cs.ServerCertForCRL},
			serverGetRoot:           getRootCertificatesForServerCRL,
			serverVerificationType:  CertVerification,
			serverExpectError:       true,
		},
	} {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			done := make(chan credentials.AuthInfo, 1)
			lis, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("Failed to listen: %v", err)
			}
			// Start a server using ServerOptions in another goroutine.
			serverOptions := &Options{
				IdentityOptions: IdentityCertificateOptions{
					Certificates:                     test.serverCert,
					GetIdentityCertificatesForServer: test.serverGetCert,
					IdentityProvider:                 test.serverIdentityProvider,
				},
				RootOptions: RootCertificateOptions{
					RootCertificates:    test.serverRoot,
					GetRootCertificates: test.serverGetRoot,
					RootProvider:        test.serverRootProvider,
				},
				RequireClientCert:          test.serverMutualTLS,
				AdditionalPeerVerification: test.serverVerifyFunc,
				VerificationType:           test.serverVerificationType,
				RevocationOptions:          test.serverRevocationOptions,
			}
			go func(done chan credentials.AuthInfo, lis net.Listener, serverOptions *Options) {
				serverRawConn, err := lis.Accept()
				if err != nil {
					close(done)
					return
				}
				serverTLS, err := NewServerCreds(serverOptions)
				if err != nil {
					serverRawConn.Close()
					close(done)
					return
				}
				_, serverAuthInfo, err := serverTLS.ServerHandshake(serverRawConn)
				if err != nil {
					serverRawConn.Close()
					close(done)
					return
				}
				done <- serverAuthInfo
			}(done, lis, serverOptions)
			defer lis.Close()
			// Start a client using ClientOptions and connects to the server.
			lisAddr := lis.Addr().String()
			conn, err := net.Dial("tcp", lisAddr)
			if err != nil {
				t.Fatalf("Client failed to connect to %s. Error: %v", lisAddr, err)
			}
			defer conn.Close()
			clientOptions := &Options{
				IdentityOptions: IdentityCertificateOptions{
					Certificates:                     test.clientCert,
					GetIdentityCertificatesForClient: test.clientGetCert,
					IdentityProvider:                 test.clientIdentityProvider,
				},
				AdditionalPeerVerification: test.clientVerifyFunc,
				RootOptions: RootCertificateOptions{
					RootCertificates:    test.clientRoot,
					GetRootCertificates: test.clientGetRoot,
					RootProvider:        test.clientRootProvider,
				},
				VerificationType:  test.clientVerificationType,
				RevocationOptions: test.clientRevocationOptions,
			}
			clientTLS, err := NewClientCreds(clientOptions)
			if err != nil {
				t.Fatalf("NewClientCreds failed: %v", err)
			}
			_, clientAuthInfo, handshakeErr := clientTLS.ClientHandshake(context.Background(),
				lisAddr, conn)
			// wait until server sends serverAuthInfo or fails.
			serverAuthInfo, ok := <-done
			if !ok && test.serverExpectError {
				return
			}
			if ok && test.serverExpectError || !ok && !test.serverExpectError {
				t.Fatalf("Server side error mismatch, got %v, want %v", !ok, test.serverExpectError)
			}
			if handshakeErr != nil && test.clientExpectHandshakeError {
				return
			}
			if handshakeErr != nil && !test.clientExpectHandshakeError ||
				handshakeErr == nil && test.clientExpectHandshakeError {
				t.Fatalf("Expect error: %v, but err is %v",
					test.clientExpectHandshakeError, handshakeErr)
			}
			if !compare(clientAuthInfo, serverAuthInfo) {
				t.Fatalf("c.ClientHandshake(_, %v, _) = %v, want %v.", lisAddr,
					clientAuthInfo, serverAuthInfo)
			}
			serverVerifiedChains := serverAuthInfo.(credentials.TLSInfo).State.VerifiedChains
			if test.serverMutualTLS && !test.serverExpectError {
				if len(serverVerifiedChains) == 0 {
					t.Fatalf("server verified chains is empty")
				}
				var clientCert *tls.Certificate
				if len(test.clientCert) > 0 {
					clientCert = &test.clientCert[0]
				} else if test.clientGetCert != nil {
					cert, _ := test.clientGetCert(&tls.CertificateRequestInfo{})
					clientCert = cert
				} else if test.clientIdentityProvider != nil {
					km, _ := test.clientIdentityProvider.KeyMaterial(context.TODO())
					clientCert = &km.Certs[0]
				}
				if !bytes.Equal((*serverVerifiedChains[0][0]).Raw, clientCert.Certificate[0]) {
					t.Fatal("server verifiedChains leaf cert doesn't match client cert")
				}

				var serverRoot *x509.CertPool
				if test.serverRoot != nil {
					serverRoot = test.serverRoot
				} else if test.serverGetRoot != nil {
					result, _ := test.serverGetRoot(&ConnectionInfo{})
					serverRoot = result.TrustCerts
				} else if test.serverRootProvider != nil {
					km, _ := test.serverRootProvider.KeyMaterial(context.TODO())
					serverRoot = km.Roots
				}
				serverVerifiedChainsCp := x509.NewCertPool()
				serverVerifiedChainsCp.AddCert(serverVerifiedChains[0][len(serverVerifiedChains[0])-1])
				if !serverVerifiedChainsCp.Equal(serverRoot) {
					t.Fatalf("server verified chain hierarchy doesn't match")
				}
			}
			clientVerifiedChains := clientAuthInfo.(credentials.TLSInfo).State.VerifiedChains
			if test.serverMutualTLS && !test.clientExpectHandshakeError {
				if len(clientVerifiedChains) == 0 {
					t.Fatalf("client verified chains is empty")
				}
				var serverCert *tls.Certificate
				if len(test.serverCert) > 0 {
					serverCert = &test.serverCert[0]
				} else if test.serverGetCert != nil {
					cert, _ := test.serverGetCert(&tls.ClientHelloInfo{})
					serverCert = cert[0]
				} else if test.serverIdentityProvider != nil {
					km, _ := test.serverIdentityProvider.KeyMaterial(context.TODO())
					serverCert = &km.Certs[0]
				}
				if !bytes.Equal((*clientVerifiedChains[0][0]).Raw, serverCert.Certificate[0]) {
					t.Fatal("client verifiedChains leaf cert doesn't match server cert")
				}

				var clientRoot *x509.CertPool
				if test.clientRoot != nil {
					clientRoot = test.clientRoot
				} else if test.clientGetRoot != nil {
					result, _ := test.clientGetRoot(&ConnectionInfo{})
					clientRoot = result.TrustCerts
				} else if test.clientRootProvider != nil {
					km, _ := test.clientRootProvider.KeyMaterial(context.TODO())
					clientRoot = km.Roots
				}
				clientVerifiedChainsCp := x509.NewCertPool()
				clientVerifiedChainsCp.AddCert(clientVerifiedChains[0][len(clientVerifiedChains[0])-1])
				if !clientVerifiedChainsCp.Equal(clientRoot) {
					t.Fatalf("client verified chain hierarchy doesn't match")
				}
			}
		})
	}
}

func compare(a1, a2 credentials.AuthInfo) bool {
	if a1.AuthType() != a2.AuthType() {
		return false
	}
	switch a1.AuthType() {
	case "tls":
		state1 := a1.(credentials.TLSInfo).State
		state2 := a2.(credentials.TLSInfo).State
		if state1.Version == state2.Version &&
			state1.HandshakeComplete == state2.HandshakeComplete &&
			state1.CipherSuite == state2.CipherSuite &&
			state1.NegotiatedProtocol == state2.NegotiatedProtocol {
			return true
		}
		return false
	default:
		return false
	}
}

func (s) TestAdvancedTLSOverrideServerName(t *testing.T) {
	expectedServerName := "server.name"
	cs := &testutils.CertStore{}
	if err := cs.LoadCerts(); err != nil {
		t.Fatalf("cs.LoadCerts() failed, err: %v", err)
	}
	clientOptions := &Options{
		RootOptions: RootCertificateOptions{
			RootCertificates: cs.ClientTrust1,
		},
		serverNameOverride: expectedServerName,
	}
	c, err := NewClientCreds(clientOptions)
	if err != nil {
		t.Fatalf("Client is unable to create credentials. Error: %v", err)
	}
	c.OverrideServerName(expectedServerName)
	if c.Info().ServerName != expectedServerName {
		t.Fatalf("c.Info().ServerName = %v, want %v", c.Info().ServerName, expectedServerName)
	}
}

func (s) TestGetCertificatesSNI(t *testing.T) {
	cs := &testutils.CertStore{}
	if err := cs.LoadCerts(); err != nil {
		t.Fatalf("cs.LoadCerts() failed, err: %v", err)
	}
	tests := []struct {
		desc       string
		serverName string
		// Use Common Name on the certificate to differentiate if we choose the right cert. The common name on all of the three certs are different.
		wantCommonName string
	}{
		{
			desc: "Select ServerCert1",
			// "foo.bar.com" is the common name on server certificate server_cert_1.pem.
			serverName:     "foo.bar.com",
			wantCommonName: "foo.bar.com",
		},
		{
			desc: "Select serverCert3",
			// "foo.bar.server3.com" is the common name on server certificate server_cert_3.pem.
			// "google.com" is one of the DNS names on server certificate server_cert_3.pem.
			serverName:     "google.com",
			wantCommonName: "foo.bar.server3.com",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			serverOptions := &Options{
				IdentityOptions: IdentityCertificateOptions{
					GetIdentityCertificatesForServer: func(*tls.ClientHelloInfo) ([]*tls.Certificate, error) {
						return []*tls.Certificate{&cs.ServerCert1, &cs.ServerCert2, &cs.ServerPeer3}, nil
					},
				},
			}
			serverConfig, err := serverOptions.serverConfig()
			if err != nil {
				t.Fatalf("serverOptions.serverConfig() failed: %v", err)
			}
			pointFormatUncompressed := uint8(0)
			clientHello := &tls.ClientHelloInfo{
				CipherSuites:      []uint16{tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA},
				ServerName:        test.serverName,
				SupportedCurves:   []tls.CurveID{tls.CurveP256},
				SupportedPoints:   []uint8{pointFormatUncompressed},
				SupportedVersions: []uint16{tls.VersionTLS12},
			}
			gotCertificate, err := serverConfig.GetCertificate(clientHello)
			if err != nil {
				t.Fatalf("serverConfig.GetCertificate(clientHello) failed: %v", err)
			}
			if gotCertificate == nil || len(gotCertificate.Certificate) == 0 {
				t.Fatalf("Got nil or empty Certificate after calling serverConfig.GetCertificate.")
			}
			parsedCert, err := x509.ParseCertificate(gotCertificate.Certificate[0])
			if err != nil {
				t.Fatalf("x509.ParseCertificate(%v) failed: %v", gotCertificate.Certificate[0], err)
			}
			if parsedCert == nil {
				t.Fatalf("Got nil Certificate after calling x509.ParseCertificate.")
			}
			if parsedCert.Subject.CommonName != test.wantCommonName {
				t.Errorf("Common name mismatch, got %v, want %v", parsedCert.Subject.CommonName, test.wantCommonName)
			}
		})
	}
}
