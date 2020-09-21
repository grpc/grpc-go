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
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"net"
	"reflect"
	"syscall"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal/grpctest"
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

func (f fakeProvider) KeyMaterial(ctx context.Context) (*certprovider.KeyMaterial, error) {
	if f.wantError {
		return nil, fmt.Errorf("bad fakeProvider")
	}
	cs := &certStore{}
	err := cs.loadCerts()
	if err != nil {
		return nil, fmt.Errorf("failed to load certs: %v", err)
	}
	if f.pt == provTypeRoot && f.isClient {
		return &certprovider.KeyMaterial{Roots: cs.clientTrust1}, nil
	}
	if f.pt == provTypeRoot && !f.isClient {
		return &certprovider.KeyMaterial{Roots: cs.serverTrust1}, nil
	}
	if f.pt == provTypeIdentity && f.isClient {
		if f.wantMultiCert {
			return &certprovider.KeyMaterial{Certs: []tls.Certificate{cs.clientPeer1, cs.clientPeer2}}, nil
		}
		return &certprovider.KeyMaterial{Certs: []tls.Certificate{cs.clientPeer1}}, nil
	}
	if f.wantMultiCert {
		return &certprovider.KeyMaterial{Certs: []tls.Certificate{cs.serverPeer1, cs.serverPeer2}}, nil
	}
	return &certprovider.KeyMaterial{Certs: []tls.Certificate{cs.serverPeer1}}, nil
}

func (f fakeProvider) Close() {}

func (s) TestClientOptionsConfigErrorCases(t *testing.T) {
	tests := []struct {
		desc            string
		clientVType     VerificationType
		IdentityOptions IdentityCertificateOptions
		RootOptions     RootCertificateOptions
	}{
		{
			desc:        "Skip default verification and provide no root credentials",
			clientVType: SkipVerification,
		},
		{
			desc:        "More than one fields in RootCertificateOptions is specified",
			clientVType: CertVerification,
			RootOptions: RootCertificateOptions{
				RootCACerts:  x509.NewCertPool(),
				RootProvider: fakeProvider{},
			},
		},
		{
			desc:        "More than one fields in IdentityCertificateOptions is specified",
			clientVType: CertVerification,
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
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			clientOptions := &ClientOptions{
				VType:           test.clientVType,
				IdentityOptions: test.IdentityOptions,
				RootOptions:     test.RootOptions,
			}
			_, err := clientOptions.config()
			if err == nil {
				t.Fatalf("ClientOptions{%v}.config() returns no err, wantErr != nil", clientOptions)
			}
		})
	}
}

func (s) TestClientOptionsConfigSuccessCases(t *testing.T) {
	tests := []struct {
		desc            string
		clientVType     VerificationType
		IdentityOptions IdentityCertificateOptions
		RootOptions     RootCertificateOptions
	}{
		{
			desc:        "Use system default if no fields in RootCertificateOptions is specified",
			clientVType: CertVerification,
		},
		{
			desc:        "Good case with mutual TLS",
			clientVType: CertVerification,
			RootOptions: RootCertificateOptions{
				RootProvider: fakeProvider{},
			},
			IdentityOptions: IdentityCertificateOptions{
				IdentityProvider: fakeProvider{pt: provTypeIdentity},
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			clientOptions := &ClientOptions{
				VType:           test.clientVType,
				IdentityOptions: test.IdentityOptions,
				RootOptions:     test.RootOptions,
			}
			clientConfig, err := clientOptions.config()
			if err != nil {
				t.Fatalf("ClientOptions{%v}.config() = %v, wantErr == nil", clientOptions, err)
			}
			// Verify that the system-provided certificates would be used
			// when no verification method was set in clientOptions.
			if clientOptions.RootOptions.RootCACerts == nil &&
				clientOptions.RootOptions.GetRootCertificates == nil && clientOptions.RootOptions.RootProvider == nil {
				if clientConfig.RootCAs == nil {
					t.Fatalf("Failed to assign system-provided certificates on the client side.")
				}
			}
		})
	}
}

func (s) TestServerOptionsConfigErrorCases(t *testing.T) {
	tests := []struct {
		desc              string
		requireClientCert bool
		serverVType       VerificationType
		IdentityOptions   IdentityCertificateOptions
		RootOptions       RootCertificateOptions
	}{
		{
			desc:              "Skip default verification and provide no root credentials",
			requireClientCert: true,
			serverVType:       SkipVerification,
		},
		{
			desc:              "More than one fields in RootCertificateOptions is specified",
			requireClientCert: true,
			serverVType:       CertVerification,
			RootOptions: RootCertificateOptions{
				RootCACerts: x509.NewCertPool(),
				GetRootCertificates: func(*GetRootCAsParams) (*GetRootCAsResults, error) {
					return nil, nil
				},
			},
		},
		{
			desc:        "More than one fields in IdentityCertificateOptions is specified",
			serverVType: CertVerification,
			IdentityOptions: IdentityCertificateOptions{
				Certificates:     []tls.Certificate{},
				IdentityProvider: fakeProvider{pt: provTypeIdentity},
			},
		},
		{
			desc:        "no field in IdentityCertificateOptions is specified",
			serverVType: CertVerification,
		},
		{
			desc: "Specify GetIdentityCertificatesForClient",
			IdentityOptions: IdentityCertificateOptions{
				GetIdentityCertificatesForClient: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
					return nil, nil
				},
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			serverOptions := &ServerOptions{
				VType:             test.serverVType,
				RequireClientCert: test.requireClientCert,
				IdentityOptions:   test.IdentityOptions,
				RootOptions:       test.RootOptions,
			}
			_, err := serverOptions.config()
			if err == nil {
				t.Fatalf("ServerOptions{%v}.config() returns no err, wantErr != nil", serverOptions)
			}
		})
	}
}

func (s) TestServerOptionsConfigSuccessCases(t *testing.T) {
	tests := []struct {
		desc              string
		requireClientCert bool
		serverVType       VerificationType
		IdentityOptions   IdentityCertificateOptions
		RootOptions       RootCertificateOptions
	}{
		{
			desc:              "Use system default if no fields in RootCertificateOptions is specified",
			requireClientCert: true,
			serverVType:       CertVerification,
			IdentityOptions: IdentityCertificateOptions{
				Certificates: []tls.Certificate{},
			},
		},
		{
			desc:              "Good case with mutual TLS",
			requireClientCert: true,
			serverVType:       CertVerification,
			RootOptions: RootCertificateOptions{
				RootProvider: fakeProvider{},
			},
			IdentityOptions: IdentityCertificateOptions{
				GetIdentityCertificatesForServer: func(*tls.ClientHelloInfo) ([]*tls.Certificate, error) {
					return nil, nil
				},
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			serverOptions := &ServerOptions{
				VType:             test.serverVType,
				RequireClientCert: test.requireClientCert,
				IdentityOptions:   test.IdentityOptions,
				RootOptions:       test.RootOptions,
			}
			serverConfig, err := serverOptions.config()
			if err != nil {
				t.Fatalf("ServerOptions{%v}.config() = %v, wantErr == nil", serverOptions, err)
			}
			// Verify that the system-provided certificates would be used
			// when no verification method was set in serverOptions.
			if serverOptions.RootOptions.RootCACerts == nil &&
				serverOptions.RootOptions.GetRootCertificates == nil && serverOptions.RootOptions.RootProvider == nil {
				if serverConfig.ClientCAs == nil {
					t.Fatalf("Failed to assign system-provided certificates on the server side.")
				}
			}
		})
	}
}

func (s) TestClientServerHandshake(t *testing.T) {
	cs := &certStore{}
	err := cs.loadCerts()
	if err != nil {
		t.Fatalf("Failed to load certs: %v", err)
	}
	getRootCAsForClient := func(params *GetRootCAsParams) (*GetRootCAsResults, error) {
		return &GetRootCAsResults{TrustCerts: cs.clientTrust1}, nil
	}
	clientVerifyFuncGood := func(params *VerificationFuncParams) (*VerificationResults, error) {
		if params.ServerName == "" {
			return nil, errors.New("client side server name should have a value")
		}
		// "foo.bar.com" is the common name on server certificate server_cert_1.pem.
		if len(params.VerifiedChains) > 0 && (params.Leaf == nil || params.Leaf.Subject.CommonName != "foo.bar.com") {
			return nil, errors.New("client side params parsing error")
		}

		return &VerificationResults{}, nil
	}
	verifyFuncBad := func(params *VerificationFuncParams) (*VerificationResults, error) {
		return nil, fmt.Errorf("custom verification function failed")
	}
	getRootCAsForServer := func(params *GetRootCAsParams) (*GetRootCAsResults, error) {
		return &GetRootCAsResults{TrustCerts: cs.serverTrust1}, nil
	}
	serverVerifyFunc := func(params *VerificationFuncParams) (*VerificationResults, error) {
		if params.ServerName != "" {
			return nil, errors.New("server side server name should not have a value")
		}
		// "foo.bar.hoo.com" is the common name on client certificate client_cert_1.pem.
		if len(params.VerifiedChains) > 0 && (params.Leaf == nil || params.Leaf.Subject.CommonName != "foo.bar.hoo.com") {
			return nil, errors.New("server side params parsing error")
		}

		return &VerificationResults{}, nil
	}
	getRootCAsForServerBad := func(params *GetRootCAsParams) (*GetRootCAsResults, error) {
		return nil, fmt.Errorf("bad root certificate reloading")
	}
	for _, test := range []struct {
		desc                       string
		clientCert                 []tls.Certificate
		clientGetCert              func(*tls.CertificateRequestInfo) (*tls.Certificate, error)
		clientRoot                 *x509.CertPool
		clientGetRoot              func(params *GetRootCAsParams) (*GetRootCAsResults, error)
		clientVerifyFunc           CustomVerificationFunc
		clientVType                VerificationType
		clientRootProvider         certprovider.Provider
		clientIdentityProvider     certprovider.Provider
		clientExpectHandshakeError bool
		serverMutualTLS            bool
		serverCert                 []tls.Certificate
		serverGetCert              func(*tls.ClientHelloInfo) ([]*tls.Certificate, error)
		serverRoot                 *x509.CertPool
		serverGetRoot              func(params *GetRootCAsParams) (*GetRootCAsResults, error)
		serverVerifyFunc           CustomVerificationFunc
		serverVType                VerificationType
		serverRootProvider         certprovider.Provider
		serverIdentityProvider     certprovider.Provider
		serverExpectError          bool
	}{
		// Client: nil setting except verifyFuncGood
		// Server: only set serverCert with mutual TLS off
		// Expected Behavior: success
		// Reason: we will use verifyFuncGood to verify the server,
		// if either clientCert or clientGetCert is not set
		{
			desc:             "Client has no trust cert with verifyFuncGood; server sends peer cert",
			clientVerifyFunc: clientVerifyFuncGood,
			clientVType:      SkipVerification,
			serverCert:       []tls.Certificate{cs.serverPeer1},
			serverVType:      CertAndHostVerification,
		},
		// Client: only set clientRoot
		// Server: only set serverCert with mutual TLS off
		// Expected Behavior: server side failure and client handshake failure
		// Reason: client side sets vType to CertAndHostVerification, and will do
		// default hostname check. All the default hostname checks will fail in
		// this test suites.
		{
			desc:                       "Client has root cert; server sends peer cert",
			clientRoot:                 cs.clientTrust1,
			clientVType:                CertAndHostVerification,
			clientExpectHandshakeError: true,
			serverCert:                 []tls.Certificate{cs.serverPeer1},
			serverVType:                CertAndHostVerification,
			serverExpectError:          true,
		},
		// Client: only set clientGetRoot
		// Server: only set serverCert with mutual TLS off
		// Expected Behavior: server side failure and client handshake failure
		// Reason: client side sets vType to CertAndHostVerification, and will do
		// default hostname check. All the default hostname checks will fail in
		// this test suites.
		{
			desc:                       "Client sets reload root function; server sends peer cert",
			clientGetRoot:              getRootCAsForClient,
			clientVType:                CertAndHostVerification,
			clientExpectHandshakeError: true,
			serverCert:                 []tls.Certificate{cs.serverPeer1},
			serverVType:                CertAndHostVerification,
			serverExpectError:          true,
		},
		// Client: set clientGetRoot and clientVerifyFunc
		// Server: only set serverCert with mutual TLS off
		// Expected Behavior: success
		{
			desc:             "Client sets reload root function with verifyFuncGood; server sends peer cert",
			clientGetRoot:    getRootCAsForClient,
			clientVerifyFunc: clientVerifyFuncGood,
			clientVType:      CertVerification,
			serverCert:       []tls.Certificate{cs.serverPeer1},
			serverVType:      CertAndHostVerification,
		},
		// Client: set clientGetRoot and bad clientVerifyFunc function
		// Server: only set serverCert with mutual TLS off
		// Expected Behavior: server side failure and client handshake failure
		// Reason: custom verification function is bad
		{
			desc:                       "Client sets reload root function with verifyFuncBad; server sends peer cert",
			clientGetRoot:              getRootCAsForClient,
			clientVerifyFunc:           verifyFuncBad,
			clientVType:                CertVerification,
			clientExpectHandshakeError: true,
			serverCert:                 []tls.Certificate{cs.serverPeer1},
			serverVType:                CertVerification,
			serverExpectError:          true,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientCert
		// Server: set serverRoot and serverCert with mutual TLS on
		// Expected Behavior: success
		{
			desc:             "Client sets peer cert, reload root function with verifyFuncGood; server sets peer cert and root cert; mutualTLS",
			clientCert:       []tls.Certificate{cs.clientPeer1},
			clientGetRoot:    getRootCAsForClient,
			clientVerifyFunc: clientVerifyFuncGood,
			clientVType:      CertVerification,
			serverMutualTLS:  true,
			serverCert:       []tls.Certificate{cs.serverPeer1},
			serverRoot:       cs.serverTrust1,
			serverVType:      CertVerification,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientCert
		// Server: set serverGetRoot and serverCert with mutual TLS on
		// Expected Behavior: success
		{
			desc:             "Client sets peer cert, reload root function with verifyFuncGood; Server sets peer cert, reload root function; mutualTLS",
			clientCert:       []tls.Certificate{cs.clientPeer1},
			clientGetRoot:    getRootCAsForClient,
			clientVerifyFunc: clientVerifyFuncGood,
			clientVType:      CertVerification,
			serverMutualTLS:  true,
			serverCert:       []tls.Certificate{cs.serverPeer1},
			serverGetRoot:    getRootCAsForServer,
			serverVType:      CertVerification,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientCert
		// Server: set serverGetRoot returning error and serverCert with mutual
		// TLS on
		// Expected Behavior: server side failure
		// Reason: server side reloading returns failure
		{
			desc:              "Client sets peer cert, reload root function with verifyFuncGood; Server sets peer cert, bad reload root function; mutualTLS",
			clientCert:        []tls.Certificate{cs.clientPeer1},
			clientGetRoot:     getRootCAsForClient,
			clientVerifyFunc:  clientVerifyFuncGood,
			clientVType:       CertVerification,
			serverMutualTLS:   true,
			serverCert:        []tls.Certificate{cs.serverPeer1},
			serverGetRoot:     getRootCAsForServerBad,
			serverVType:       CertVerification,
			serverExpectError: true,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientGetCert
		// Server: set serverGetRoot and serverGetCert with mutual TLS on
		// Expected Behavior: success
		{
			desc: "Client sets reload peer/root function with verifyFuncGood; Server sets reload peer/root function with verifyFuncGood; mutualTLS",
			clientGetCert: func(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return &cs.clientPeer1, nil
			},
			clientGetRoot:    getRootCAsForClient,
			clientVerifyFunc: clientVerifyFuncGood,
			clientVType:      CertVerification,
			serverMutualTLS:  true,
			serverGetCert: func(info *tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				return []*tls.Certificate{&cs.serverPeer1}, nil
			},
			serverGetRoot:    getRootCAsForServer,
			serverVerifyFunc: serverVerifyFunc,
			serverVType:      CertVerification,
		},
		// Client: set everything but with the wrong peer cert not trusted by
		// server
		// Server: set serverGetRoot and serverGetCert with mutual TLS on
		// Expected Behavior: server side returns failure because of
		// certificate mismatch
		{
			desc: "Client sends wrong peer cert; Server sets reload peer/root function with verifyFuncGood; mutualTLS",
			clientGetCert: func(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return &cs.serverPeer1, nil
			},
			clientGetRoot:    getRootCAsForClient,
			clientVerifyFunc: clientVerifyFuncGood,
			clientVType:      CertVerification,
			serverMutualTLS:  true,
			serverGetCert: func(info *tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				return []*tls.Certificate{&cs.serverPeer1}, nil
			},
			serverGetRoot:     getRootCAsForServer,
			serverVerifyFunc:  serverVerifyFunc,
			serverVType:       CertVerification,
			serverExpectError: true,
		},
		// Client: set everything but with the wrong trust cert not trusting server
		// Server: set serverGetRoot and serverGetCert with mutual TLS on
		// Expected Behavior: server side and client side return failure due to
		// certificate mismatch and handshake failure
		{
			desc: "Client has wrong trust cert; Server sets reload peer/root function with verifyFuncGood; mutualTLS",
			clientGetCert: func(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return &cs.clientPeer1, nil
			},
			clientGetRoot:              getRootCAsForServer,
			clientVerifyFunc:           clientVerifyFuncGood,
			clientVType:                CertVerification,
			clientExpectHandshakeError: true,
			serverMutualTLS:            true,
			serverGetCert: func(info *tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				return []*tls.Certificate{&cs.serverPeer1}, nil
			},
			serverGetRoot:     getRootCAsForServer,
			serverVerifyFunc:  serverVerifyFunc,
			serverVType:       CertVerification,
			serverExpectError: true,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientCert
		// Server: set everything but with the wrong peer cert not trusted by
		// client
		// Expected Behavior: server side and client side return failure due to
		// certificate mismatch and handshake failure
		{
			desc: "Client sets reload peer/root function with verifyFuncGood; Server sends wrong peer cert; mutualTLS",
			clientGetCert: func(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return &cs.clientPeer1, nil
			},
			clientGetRoot:    getRootCAsForClient,
			clientVerifyFunc: clientVerifyFuncGood,
			clientVType:      CertVerification,
			serverMutualTLS:  true,
			serverGetCert: func(info *tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				return []*tls.Certificate{&cs.clientPeer1}, nil
			},
			serverGetRoot:     getRootCAsForServer,
			serverVerifyFunc:  serverVerifyFunc,
			serverVType:       CertVerification,
			serverExpectError: true,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientCert
		// Server: set everything but with the wrong trust cert not trusting client
		// Expected Behavior: server side and client side return failure due to
		// certificate mismatch and handshake failure
		{
			desc: "Client sets reload peer/root function with verifyFuncGood; Server has wrong trust cert; mutualTLS",
			clientGetCert: func(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return &cs.clientPeer1, nil
			},
			clientGetRoot:              getRootCAsForClient,
			clientVerifyFunc:           clientVerifyFuncGood,
			clientVType:                CertVerification,
			clientExpectHandshakeError: true,
			serverMutualTLS:            true,
			serverGetCert: func(info *tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				return []*tls.Certificate{&cs.serverPeer1}, nil
			},
			serverGetRoot:     getRootCAsForClient,
			serverVerifyFunc:  serverVerifyFunc,
			serverVType:       CertVerification,
			serverExpectError: true,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientCert
		// Server: set serverGetRoot and serverCert, but with bad verifyFunc
		// Expected Behavior: server side and client side return failure due to
		// server custom check fails
		{
			desc:                       "Client sets peer cert, reload root function with verifyFuncGood; Server sets bad custom check; mutualTLS",
			clientCert:                 []tls.Certificate{cs.clientPeer1},
			clientGetRoot:              getRootCAsForClient,
			clientVerifyFunc:           clientVerifyFuncGood,
			clientVType:                CertVerification,
			clientExpectHandshakeError: true,
			serverMutualTLS:            true,
			serverCert:                 []tls.Certificate{cs.serverPeer1},
			serverGetRoot:              getRootCAsForServer,
			serverVerifyFunc:           verifyFuncBad,
			serverVType:                CertVerification,
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
			clientVType:            CertVerification,
			serverMutualTLS:        true,
			serverIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: false},
			serverRootProvider:     fakeProvider{isClient: false},
			serverVType:            CertVerification,
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
			clientVType:            CertVerification,
			serverMutualTLS:        true,
			serverIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: false},
			serverRootProvider:     fakeProvider{isClient: false},
			serverVType:            CertVerification,
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
			clientVType:            CertVerification,
			serverMutualTLS:        true,
			serverIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: false},
			serverRootProvider:     fakeProvider{isClient: false, wantError: true},
			serverVType:            CertVerification,
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
			clientVType:            CertVerification,
			serverMutualTLS:        true,
			serverIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: false},
			serverRootProvider:     fakeProvider{isClient: false},
			serverVType:            CertVerification,
		},
		// Client: set clientIdentityProvider and clientRootProvider
		// Server: set serverIdentityProvider getting multiple cert chains and serverRootProvider with mutual TLS on
		// Expected Behavior: success, because server side has SNI
		{
			desc:                   "Client sets root and identity provider; Server sets multiple certs in serverIdentityProvider; mutualTLS",
			clientIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: true},
			clientRootProvider:     fakeProvider{isClient: true},
			clientVerifyFunc:       clientVerifyFuncGood,
			clientVType:            CertVerification,
			serverMutualTLS:        true,
			serverIdentityProvider: fakeProvider{pt: provTypeIdentity, isClient: false, wantMultiCert: true},
			serverRootProvider:     fakeProvider{isClient: false},
			serverVType:            CertVerification,
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
			serverOptions := &ServerOptions{
				IdentityOptions: IdentityCertificateOptions{
					Certificates:                     test.serverCert,
					GetIdentityCertificatesForServer: test.serverGetCert,
					IdentityProvider:                 test.serverIdentityProvider,
				},
				RootOptions: RootCertificateOptions{
					RootCACerts:         test.serverRoot,
					GetRootCertificates: test.serverGetRoot,
					RootProvider:        test.serverRootProvider,
				},
				RequireClientCert: test.serverMutualTLS,
				VerifyPeer:        test.serverVerifyFunc,
				VType:             test.serverVType,
			}
			go func(done chan credentials.AuthInfo, lis net.Listener, serverOptions *ServerOptions) {
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
			clientOptions := &ClientOptions{
				IdentityOptions: IdentityCertificateOptions{
					Certificates:                     test.clientCert,
					GetIdentityCertificatesForClient: test.clientGetCert,
					IdentityProvider:                 test.clientIdentityProvider,
				},
				VerifyPeer: test.clientVerifyFunc,
				RootOptions: RootCertificateOptions{
					RootCACerts:         test.clientRoot,
					GetRootCertificates: test.clientGetRoot,
					RootProvider:        test.clientRootProvider,
				},
				VType: test.clientVType,
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
		})
	}
}

func readTrustCert(fileName string) (*x509.CertPool, error) {
	trustData, err := ioutil.ReadFile(fileName)
	if err != nil {
		return nil, err
	}
	trustBlock, _ := pem.Decode(trustData)
	if trustBlock == nil {
		return nil, err
	}
	trustCert, err := x509.ParseCertificate(trustBlock.Bytes)
	if err != nil {
		return nil, err
	}
	trustPool := x509.NewCertPool()
	trustPool.AddCert(trustCert)
	return trustPool, nil
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
	clientTrustPool, err := readTrustCert(testdata.Path("client_trust_cert_1.pem"))
	if err != nil {
		t.Fatalf("Client is unable to load trust certs. Error: %v", err)
	}
	clientOptions := &ClientOptions{
		RootOptions: RootCertificateOptions{
			RootCACerts: clientTrustPool,
		},
		ServerNameOverride: expectedServerName,
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

func (s) TestTLSClone(t *testing.T) {
	expectedServerName := "server.name"
	clientTrustPool, err := readTrustCert(testdata.Path("client_trust_cert_1.pem"))
	if err != nil {
		t.Fatalf("Client is unable to load trust certs. Error: %v", err)
	}
	clientOptions := &ClientOptions{
		RootOptions: RootCertificateOptions{
			RootCACerts: clientTrustPool,
		},
		ServerNameOverride: expectedServerName,
	}
	c, err := NewClientCreds(clientOptions)
	if err != nil {
		t.Fatalf("Failed to create new client: %v", err)
	}
	cc := c.Clone()
	if cc.Info().ServerName != expectedServerName {
		t.Fatalf("cc.Info().ServerName = %v, want %v", cc.Info().ServerName, expectedServerName)
	}
	cc.OverrideServerName("")
	if c.Info().ServerName != expectedServerName {
		t.Fatalf("Change in clone should not affect the original, "+
			"c.Info().ServerName = %v, want %v", c.Info().ServerName, expectedServerName)
	}

}

func (s) TestAppendH2ToNextProtos(t *testing.T) {
	tests := []struct {
		name string
		ps   []string
		want []string
	}{
		{
			name: "empty",
			ps:   nil,
			want: []string{"h2"},
		},
		{
			name: "only h2",
			ps:   []string{"h2"},
			want: []string{"h2"},
		},
		{
			name: "with h2",
			ps:   []string{"alpn", "h2"},
			want: []string{"alpn", "h2"},
		},
		{
			name: "no h2",
			ps:   []string{"alpn"},
			want: []string{"alpn", "h2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := appendH2ToNextProtos(tt.ps); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("appendH2ToNextProtos() = %v, want %v", got, tt.want)
			}
		})
	}
}

type nonSyscallConn struct {
	net.Conn
}

func (s) TestWrapSyscallConn(t *testing.T) {
	sc := &syscallConn{}
	nsc := &nonSyscallConn{}

	wrapConn := WrapSyscallConn(sc, nsc)
	if _, ok := wrapConn.(syscall.Conn); !ok {
		t.Errorf("returned conn (type %T) doesn't implement syscall.Conn, want implement",
			wrapConn)
	}
}

func (s) TestGetCertificatesSNI(t *testing.T) {
	// Load server certificates for setting the serverGetCert callback function.
	serverCert1, err := tls.LoadX509KeyPair(testdata.Path("server_cert_1.pem"), testdata.Path("server_key_1.pem"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(server_cert_1.pem, server_key_1.pem) failed: %v", err)
	}
	serverCert2, err := tls.LoadX509KeyPair(testdata.Path("server_cert_2.pem"), testdata.Path("server_key_2.pem"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(server_cert_2.pem, server_key_2.pem) failed: %v", err)
	}
	serverCert3, err := tls.LoadX509KeyPair(testdata.Path("server_cert_3.pem"), testdata.Path("server_key_3.pem"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(server_cert_3.pem, server_key_3.pem) failed: %v", err)
	}

	tests := []struct {
		desc       string
		serverName string
		wantCert   tls.Certificate
	}{
		{
			desc: "Select serverCert1",
			// "foo.bar.com" is the common name on server certificate server_cert_1.pem.
			serverName: "foo.bar.com",
			wantCert:   serverCert1,
		},
		{
			desc: "Select serverCert2",
			// "foo.bar.server2.com" is the common name on server certificate server_cert_2.pem.
			serverName: "foo.bar.server2.com",
			wantCert:   serverCert2,
		},
		{
			desc: "Select serverCert3",
			// "google.com" is one of the DNS names on server certificate server_cert_3.pem.
			serverName: "google.com",
			wantCert:   serverCert3,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			serverOptions := &ServerOptions{
				IdentityOptions: IdentityCertificateOptions{
					GetIdentityCertificatesForServer: func(info *tls.ClientHelloInfo) ([]*tls.Certificate, error) {
						return []*tls.Certificate{&serverCert1, &serverCert2, &serverCert3}, nil
					},
				},
			}
			serverConfig, err := serverOptions.config()
			if err != nil {
				t.Fatalf("serverOptions.config() failed: %v", err)
			}
			pointFormatUncompressed := uint8(0)
			clientHello := &tls.ClientHelloInfo{
				CipherSuites:      []uint16{tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA},
				ServerName:        test.serverName,
				SupportedCurves:   []tls.CurveID{tls.CurveP256},
				SupportedPoints:   []uint8{pointFormatUncompressed},
				SupportedVersions: []uint16{tls.VersionTLS10},
			}
			gotCertificate, err := serverConfig.GetCertificate(clientHello)
			if err != nil {
				t.Fatalf("serverConfig.GetCertificate(clientHello) failed: %v", err)
			}
			if !cmp.Equal(*gotCertificate, test.wantCert, cmp.AllowUnexported(big.Int{})) {
				t.Errorf("GetCertificates() = %v, want %v", *gotCertificate, test.wantCert)
			}
		})
	}
}
