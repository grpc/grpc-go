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
	"net"
	"reflect"
	"syscall"
	"testing"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/security/advancedtls/testdata"
)

func TestClientServerHandshake(t *testing.T) {
	// ------------------Load Client Trust Cert and Peer Cert-------------------
	clientTrustPool, err := readTrustCert(testdata.Path("client_trust_cert_1.pem"))
	if err != nil {
		t.Fatalf("Client is unable to load trust certs. Error: %v", err)
	}
	getRootCAsForClient := func(params *GetRootCAsParams) (*GetRootCAsResults, error) {
		return &GetRootCAsResults{TrustCerts: clientTrustPool}, nil
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
	clientPeerCert, err := tls.LoadX509KeyPair(testdata.Path("client_cert_1.pem"),
		testdata.Path("client_key_1.pem"))
	if err != nil {
		t.Fatalf("Client is unable to parse peer certificates. Error: %v", err)
	}
	// ------------------Load Server Trust Cert and Peer Cert-------------------
	serverTrustPool, err := readTrustCert(testdata.Path("server_trust_cert_1.pem"))
	if err != nil {
		t.Fatalf("Server is unable to load trust certs. Error: %v", err)
	}
	getRootCAsForServer := func(params *GetRootCAsParams) (*GetRootCAsResults, error) {
		return &GetRootCAsResults{TrustCerts: serverTrustPool}, nil
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
	serverPeerCert, err := tls.LoadX509KeyPair(testdata.Path("server_cert_1.pem"),
		testdata.Path("server_key_1.pem"))
	if err != nil {
		t.Fatalf("Server is unable to parse peer certificates. Error: %v", err)
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
		clientExpectCreateError    bool
		clientExpectHandshakeError bool
		serverMutualTLS            bool
		serverCert                 []tls.Certificate
		serverGetCert              func(*tls.ClientHelloInfo) (*tls.Certificate, error)
		serverRoot                 *x509.CertPool
		serverGetRoot              func(params *GetRootCAsParams) (*GetRootCAsResults, error)
		serverVerifyFunc           CustomVerificationFunc
		serverVType                VerificationType
		serverExpectError          bool
	}{
		// Client: nil setting
		// Server: only set serverCert with mutual TLS off
		// Expected Behavior: server side failure
		// Reason: if clientRoot, clientGetRoot and verifyFunc is not set, client
		// side doesn't provide any verification mechanism. We don't allow this
		// even setting vType to SkipVerification. Clients should at least provide
		// their own verification logic.
		{
			desc:                    "Client has no trust cert; server sends peer cert",
			clientVType:             SkipVerification,
			clientExpectCreateError: true,
			serverCert:              []tls.Certificate{serverPeerCert},
			serverVType:             CertAndHostVerification,
			serverExpectError:       true,
		},
		// Client: nil setting except verifyFuncGood
		// Server: only set serverCert with mutual TLS off
		// Expected Behavior: success
		// Reason: we will use verifyFuncGood to verify the server,
		// if either clientCert or clientGetCert is not set
		{
			desc:             "Client has no trust cert with verifyFuncGood; server sends peer cert",
			clientVerifyFunc: clientVerifyFuncGood,
			clientVType:      SkipVerification,
			serverCert:       []tls.Certificate{serverPeerCert},
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
			clientRoot:                 clientTrustPool,
			clientVType:                CertAndHostVerification,
			clientExpectHandshakeError: true,
			serverCert:                 []tls.Certificate{serverPeerCert},
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
			serverCert:                 []tls.Certificate{serverPeerCert},
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
			serverCert:       []tls.Certificate{serverPeerCert},
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
			serverCert:                 []tls.Certificate{serverPeerCert},
			serverVType:                CertVerification,
			serverExpectError:          true,
		},
		// Client: set clientGetRoot and clientVerifyFunc
		// Server: nil setting
		// Expected Behavior: server side failure
		// Reason: server side must either set serverCert or serverGetCert
		{
			desc:              "Client sets reload root function with verifyFuncGood; server sets nil",
			clientGetRoot:     getRootCAsForClient,
			clientVerifyFunc:  clientVerifyFuncGood,
			clientVType:       CertVerification,
			serverVType:       CertVerification,
			serverExpectError: true,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientCert
		// Server: set serverRoot and serverCert with mutual TLS on
		// Expected Behavior: success
		{
			desc:             "Client sets peer cert, reload root function with verifyFuncGood; server sets peer cert and root cert; mutualTLS",
			clientCert:       []tls.Certificate{clientPeerCert},
			clientGetRoot:    getRootCAsForClient,
			clientVerifyFunc: clientVerifyFuncGood,
			clientVType:      CertVerification,
			serverMutualTLS:  true,
			serverCert:       []tls.Certificate{serverPeerCert},
			serverRoot:       serverTrustPool,
			serverVType:      CertVerification,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientCert
		// Server: set serverCert, but not setting any of serverRoot, serverGetRoot
		// or serverVerifyFunc, with mutual TLS on
		// Expected Behavior: server side failure
		// Reason: server side needs to provide any verification mechanism when
		// mTLS in on, even setting vType to SkipVerification. Servers should at
		// least provide their own verification logic.
		{
			desc:                       "Client sets peer cert, reload root function with verifyFuncGood; server sets no verification; mutualTLS",
			clientCert:                 []tls.Certificate{clientPeerCert},
			clientGetRoot:              getRootCAsForClient,
			clientVerifyFunc:           clientVerifyFuncGood,
			clientVType:                CertVerification,
			clientExpectHandshakeError: true,
			serverMutualTLS:            true,
			serverCert:                 []tls.Certificate{serverPeerCert},
			serverVType:                SkipVerification,
			serverExpectError:          true,
		},
		// Client: set clientGetRoot, clientVerifyFunc and clientCert
		// Server: set serverGetRoot and serverCert with mutual TLS on
		// Expected Behavior: success
		{
			desc:             "Client sets peer cert, reload root function with verifyFuncGood; Server sets peer cert, reload root function; mutualTLS",
			clientCert:       []tls.Certificate{clientPeerCert},
			clientGetRoot:    getRootCAsForClient,
			clientVerifyFunc: clientVerifyFuncGood,
			clientVType:      CertVerification,
			serverMutualTLS:  true,
			serverCert:       []tls.Certificate{serverPeerCert},
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
			clientCert:        []tls.Certificate{clientPeerCert},
			clientGetRoot:     getRootCAsForClient,
			clientVerifyFunc:  clientVerifyFuncGood,
			clientVType:       CertVerification,
			serverMutualTLS:   true,
			serverCert:        []tls.Certificate{serverPeerCert},
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
				return &clientPeerCert, nil
			},
			clientGetRoot:    getRootCAsForClient,
			clientVerifyFunc: clientVerifyFuncGood,
			clientVType:      CertVerification,
			serverMutualTLS:  true,
			serverGetCert: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
				return &serverPeerCert, nil
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
				return &serverPeerCert, nil
			},
			clientGetRoot:    getRootCAsForClient,
			clientVerifyFunc: clientVerifyFuncGood,
			clientVType:      CertVerification,
			serverMutualTLS:  true,
			serverGetCert: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
				return &serverPeerCert, nil
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
				return &clientPeerCert, nil
			},
			clientGetRoot:              getRootCAsForServer,
			clientVerifyFunc:           clientVerifyFuncGood,
			clientVType:                CertVerification,
			clientExpectHandshakeError: true,
			serverMutualTLS:            true,
			serverGetCert: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
				return &serverPeerCert, nil
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
				return &clientPeerCert, nil
			},
			clientGetRoot:    getRootCAsForClient,
			clientVerifyFunc: clientVerifyFuncGood,
			clientVType:      CertVerification,
			serverMutualTLS:  true,
			serverGetCert: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
				return &clientPeerCert, nil
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
				return &clientPeerCert, nil
			},
			clientGetRoot:              getRootCAsForClient,
			clientVerifyFunc:           clientVerifyFuncGood,
			clientVType:                CertVerification,
			clientExpectHandshakeError: true,
			serverMutualTLS:            true,
			serverGetCert: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
				return &serverPeerCert, nil
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
			clientCert:                 []tls.Certificate{clientPeerCert},
			clientGetRoot:              getRootCAsForClient,
			clientVerifyFunc:           clientVerifyFuncGood,
			clientVType:                CertVerification,
			clientExpectHandshakeError: true,
			serverMutualTLS:            true,
			serverCert:                 []tls.Certificate{serverPeerCert},
			serverGetRoot:              getRootCAsForServer,
			serverVerifyFunc:           verifyFuncBad,
			serverVType:                CertVerification,
			serverExpectError:          true,
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
				Certificates:   test.serverCert,
				GetCertificate: test.serverGetCert,
				RootCertificateOptions: RootCertificateOptions{
					RootCACerts: test.serverRoot,
					GetRootCAs:  test.serverGetRoot,
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
				Certificates:         test.clientCert,
				GetClientCertificate: test.clientGetCert,
				VerifyPeer:           test.clientVerifyFunc,
				RootCertificateOptions: RootCertificateOptions{
					RootCACerts: test.clientRoot,
					GetRootCAs:  test.clientGetRoot,
				},
				VType: test.clientVType,
			}
			clientTLS, newClientErr := NewClientCreds(clientOptions)
			if newClientErr != nil && test.clientExpectCreateError {
				return
			}
			if newClientErr != nil && !test.clientExpectCreateError ||
				newClientErr == nil && test.clientExpectCreateError {
				t.Fatalf("Expect error: %v, but err is %v",
					test.clientExpectCreateError, newClientErr)
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

func TestAdvancedTLSOverrideServerName(t *testing.T) {
	expectedServerName := "server.name"
	clientTrustPool, err := readTrustCert(testdata.Path("client_trust_cert_1.pem"))
	if err != nil {
		t.Fatalf("Client is unable to load trust certs. Error: %v", err)
	}
	clientOptions := &ClientOptions{
		RootCertificateOptions: RootCertificateOptions{
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

func TestTLSClone(t *testing.T) {
	expectedServerName := "server.name"
	clientTrustPool, err := readTrustCert(testdata.Path("client_trust_cert_1.pem"))
	if err != nil {
		t.Fatalf("Client is unable to load trust certs. Error: %v", err)
	}
	clientOptions := &ClientOptions{
		RootCertificateOptions: RootCertificateOptions{
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

func TestAppendH2ToNextProtos(t *testing.T) {
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

func TestWrapSyscallConn(t *testing.T) {
	sc := &syscallConn{}
	nsc := &nonSyscallConn{}

	wrapConn := WrapSyscallConn(sc, nsc)
	if _, ok := wrapConn.(syscall.Conn); !ok {
		t.Errorf("returned conn (type %T) doesn't implement syscall.Conn, want implement",
			wrapConn)
	}
}
