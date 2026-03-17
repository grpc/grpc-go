/*
 *
 * Copyright 2020 gRPC authors.
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
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/credentials/tls/certprovider/pemfile"
	pb "google.golang.org/grpc/examples/helloworld/helloworld"
	"google.golang.org/grpc/security/advancedtls/internal/testutils"
	"google.golang.org/grpc/security/advancedtls/testdata"
)

const (
	// Default timeout for normal connections.
	defaultTestTimeout = 5 * time.Second
	// Intervals that set to monitor the credential updates.
	credRefreshingInterval = 200 * time.Millisecond
	// Time we wait for the credential updates to be picked up.
	sleepInterval = 400 * time.Millisecond
)

// stageInfo contains a stage number indicating the current phase of each
// integration test, and a mutex.
// Based on the stage number of current test, we will use different
// certificates and custom verification functions to check if our tests behave
// as expected.
type stageInfo struct {
	mutex sync.Mutex
	stage int
}

func (s *stageInfo) increase() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.stage = s.stage + 1
}

func (s *stageInfo) read() int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.stage
}

func (s *stageInfo) reset() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.stage = 0
}

type greeterServer struct {
	pb.UnimplementedGreeterServer
}

// sayHello is a simple implementation of the pb.GreeterServer SayHello method.
func (greeterServer) SayHello(_ context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	return &pb.HelloReply{Message: "Hello " + in.Name}, nil
}

// TODO(ZhenLian): remove shouldFail to the function signature to provider
// tests.
func callAndVerify(ctx context.Context, msg string, client pb.GreeterClient, shouldFail bool) error {
	_, err := client.SayHello(ctx, &pb.HelloRequest{Name: msg})
	if want, got := shouldFail == true, err != nil; got != want {
		return fmt.Errorf("want and got mismatch,  want shouldFail=%v, got fail=%v, rpc error: %v", want, got, err)
	}
	return nil
}

// TODO(ZhenLian): remove shouldFail and add ...DialOption to the function
// signature to provider cleaner tests.
func callAndVerifyWithClientConn(ctx context.Context, address string, msg string, creds credentials.TransportCredentials, shouldFail bool) (*grpc.ClientConn, pb.GreeterClient, error) {
	// Disable service config lookups as it results in a DNS lookup which can
	// flake in CI.
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(creds), grpc.WithDisableServiceConfig())
	if err != nil {
		return nil, nil, fmt.Errorf("client failed to connect to %s. Error: %v", address, err)
	}
	greetClient := pb.NewGreeterClient(conn)
	err = callAndVerify(ctx, msg, greetClient, shouldFail)
	if err != nil {
		return nil, nil, err
	}
	return conn, greetClient, nil
}

// The advanced TLS features are tested in different stages.
// At stage 0, we establish a good connection between client and server.
// At stage 1, we change one factor(it could be we change the server's
// certificate, or custom verification function, etc), and test if the
// following connections would be dropped.
// At stage 2, we re-establish the connection by changing the counterpart of
// the factor we modified in stage 1.
// (could be change the client's trust certificate, or change custom
// verification function, etc)
func (s) TestEnd2End(t *testing.T) {
	cs := &testutils.CertStore{}
	if err := cs.LoadCerts(); err != nil {
		t.Fatalf("cs.LoadCerts() failed, err: %v", err)
	}
	stage := &stageInfo{}
	for _, test := range []struct {
		desc                   string
		clientCert             []tls.Certificate
		clientGetCert          func(*tls.CertificateRequestInfo) (*tls.Certificate, error)
		clientRoot             *x509.CertPool
		clientGetRoot          func(params *ConnectionInfo) (*RootCertificates, error)
		clientVerifyFunc       PostHandshakeVerificationFunc
		clientVerificationType VerificationType
		serverCert             []tls.Certificate
		serverGetCert          func(*tls.ClientHelloInfo) ([]*tls.Certificate, error)
		serverRoot             *x509.CertPool
		serverGetRoot          func(params *ConnectionInfo) (*RootCertificates, error)
		serverVerifyFunc       PostHandshakeVerificationFunc
		serverVerificationType VerificationType
	}{
		// Test Scenarios:
		// At initialization(stage = 0), client will be initialized with cert
		// ClientCert1 and ClientTrust1, server with ServerCert1 and ServerTrust1.
		// The mutual authentication works at the beginning, since ClientCert1 is
		// trusted by ServerTrust1, and ServerCert1 by ClientTrust1.
		// At stage 1, client changes ClientCert1 to ClientCert2. Since ClientCert2
		// is not trusted by ServerTrust1, following rpc calls are expected to
		// fail, while the previous rpc calls are still good because those are
		// already authenticated.
		// At stage 2, the server changes ServerTrust1 to ServerTrust2, and we
		// should see it again accepts the connection, since ClientCert2 is trusted
		// by ServerTrust2.
		{
			desc: "test the reloading feature for client identity callback and server trust callback",
			clientGetCert: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
				switch stage.read() {
				case 0:
					return &cs.ClientCert1, nil
				default:
					return &cs.ClientCert2, nil
				}
			},
			clientRoot: cs.ClientTrust1,
			clientVerifyFunc: func(*HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
				return &PostHandshakeVerificationResults{}, nil
			},
			clientVerificationType: CertVerification,
			serverCert:             []tls.Certificate{cs.ServerCert1},
			serverGetRoot: func(*ConnectionInfo) (*RootCertificates, error) {
				switch stage.read() {
				case 0, 1:
					return &RootCertificates{TrustCerts: cs.ServerTrust1}, nil
				default:
					return &RootCertificates{TrustCerts: cs.ServerTrust2}, nil
				}
			},
			serverVerifyFunc: func(*HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
				return &PostHandshakeVerificationResults{}, nil
			},
			serverVerificationType: CertVerification,
		},
		// Test Scenarios:
		// At initialization(stage = 0), client will be initialized with cert
		// ClientCert1 and ClientTrust1, server with ServerCert1 and ServerTrust1.
		// The mutual authentication works at the beginning, since ClientCert1 is
		// trusted by ServerTrust1, and ServerCert1 by ClientTrust1.
		// At stage 1, server changes ServerCert1 to ServerCert2. Since ServerCert2
		// is not trusted by ClientTrust1, following rpc calls are expected to
		// fail, while the previous rpc calls are still good because those are
		// already authenticated.
		// At stage 2, the client changes ClientTrust1 to ClientTrust2, and we
		// should see it again accepts the connection, since ServerCert2 is trusted
		// by ClientTrust2.
		{
			desc:       "test the reloading feature for server identity callback and client trust callback",
			clientCert: []tls.Certificate{cs.ClientCert1},
			clientGetRoot: func(*ConnectionInfo) (*RootCertificates, error) {
				switch stage.read() {
				case 0, 1:
					return &RootCertificates{TrustCerts: cs.ClientTrust1}, nil
				default:
					return &RootCertificates{TrustCerts: cs.ClientTrust2}, nil
				}
			},
			clientVerifyFunc: func(*HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
				return &PostHandshakeVerificationResults{}, nil
			},
			clientVerificationType: CertVerification,
			serverGetCert: func(*tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				switch stage.read() {
				case 0:
					return []*tls.Certificate{&cs.ServerCert1}, nil
				default:
					return []*tls.Certificate{&cs.ServerCert2}, nil
				}
			},
			serverRoot: cs.ServerTrust1,
			serverVerifyFunc: func(*HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
				return &PostHandshakeVerificationResults{}, nil
			},
			serverVerificationType: CertVerification,
		},
		// Test Scenarios:
		// At initialization(stage = 0), client will be initialized with cert
		// ClientCert1 and ClientTrust1, server with ServerCert1 and ServerTrust1.
		// The mutual authentication works at the beginning, since ClientCert1
		// trusted by ServerTrust1, ServerCert1 by ClientTrust1, and also the
		// custom verification check allows the CommonName on ServerCert1.
		// At stage 1, server changes ServerCert1 to ServerCert2, and client
		// changes ClientTrust1 to ClientTrust2. Although ServerCert2 is trusted by
		// ClientTrust2, our authorization check only accepts ServerCert1, and
		// hence the following calls should fail. Previous connections should
		// not be affected.
		// At stage 2, the client changes authorization check to only accept
		// ServerCert2. Now we should see the connection becomes normal again.
		{
			desc:       "test client custom verification",
			clientCert: []tls.Certificate{cs.ClientCert1},
			clientGetRoot: func(*ConnectionInfo) (*RootCertificates, error) {
				switch stage.read() {
				case 0:
					return &RootCertificates{TrustCerts: cs.ClientTrust1}, nil
				default:
					return &RootCertificates{TrustCerts: cs.ClientTrust2}, nil
				}
			},
			clientVerifyFunc: func(params *HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
				if len(params.RawCerts) == 0 {
					return nil, fmt.Errorf("no peer certs")
				}
				cert, err := x509.ParseCertificate(params.RawCerts[0])
				if err != nil || cert == nil {
					return nil, fmt.Errorf("failed to parse certificate: %v", err)
				}
				authzCheck := false
				switch stage.read() {
				case 0, 1:
					// foo.bar.com is the common name on ServerCert1
					if cert.Subject.CommonName == "foo.bar.com" {
						authzCheck = true
					}
				default:
					// foo.bar.server2.com is the common name on ServerCert2
					if cert.Subject.CommonName == "foo.bar.server2.com" {
						authzCheck = true
					}
				}
				if authzCheck {
					return &PostHandshakeVerificationResults{}, nil
				}
				return nil, fmt.Errorf("custom authz check fails")
			},
			clientVerificationType: CertVerification,
			serverGetCert: func(*tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				switch stage.read() {
				case 0:
					return []*tls.Certificate{&cs.ServerCert1}, nil
				default:
					return []*tls.Certificate{&cs.ServerCert2}, nil
				}
			},
			serverRoot: cs.ServerTrust1,
			serverVerifyFunc: func(*HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
				return &PostHandshakeVerificationResults{}, nil
			},
			serverVerificationType: CertVerification,
		},
		// Test Scenarios:
		// At initialization(stage = 0), client will be initialized with cert
		// ClientCert1 and ClientTrust1, server with ServerCert1 and ServerTrust1.
		// The mutual authentication works at the beginning, since ClientCert1
		// trusted by ServerTrust1, ServerCert1 by ClientTrust1, and also the
		// custom verification check on server side allows all connections.
		// At stage 1, server disallows the connections by setting custom
		// verification check. The following calls should fail. Previous
		// connections should not be affected.
		// At stage 2, server allows all the connections again and the
		// authentications should go back to normal.
		{
			desc:       "TestServerCustomVerification",
			clientCert: []tls.Certificate{cs.ClientCert1},
			clientRoot: cs.ClientTrust1,
			clientVerifyFunc: func(*HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
				return &PostHandshakeVerificationResults{}, nil
			},
			clientVerificationType: CertVerification,
			serverCert:             []tls.Certificate{cs.ServerCert1},
			serverRoot:             cs.ServerTrust1,
			serverVerifyFunc: func(*HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
				switch stage.read() {
				case 0, 2:
					return &PostHandshakeVerificationResults{}, nil
				case 1:
					return nil, fmt.Errorf("custom authz check fails")
				default:
					return nil, fmt.Errorf("custom authz check fails")
				}
			},
			serverVerificationType: CertVerification,
		},
	} {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			// Start a server using ServerOptions in another goroutine.
			serverOptions := &Options{
				IdentityOptions: IdentityCertificateOptions{
					Certificates:                     test.serverCert,
					GetIdentityCertificatesForServer: test.serverGetCert,
				},
				RootOptions: RootCertificateOptions{
					RootCertificates:    test.serverRoot,
					GetRootCertificates: test.serverGetRoot,
				},
				RequireClientCert:          true,
				AdditionalPeerVerification: test.serverVerifyFunc,
				VerificationType:           test.serverVerificationType,
			}
			serverTLSCreds, err := NewServerCreds(serverOptions)
			if err != nil {
				t.Fatalf("failed to create server creds: %v", err)
			}
			s := grpc.NewServer(grpc.Creds(serverTLSCreds))
			defer s.Stop()
			lis, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("failed to listen: %v", err)
			}
			defer lis.Close()
			addr := fmt.Sprintf("localhost:%v", lis.Addr().(*net.TCPAddr).Port)
			pb.RegisterGreeterServer(s, greeterServer{})
			go s.Serve(lis)
			clientOptions := &Options{
				IdentityOptions: IdentityCertificateOptions{
					Certificates:                     test.clientCert,
					GetIdentityCertificatesForClient: test.clientGetCert,
				},
				AdditionalPeerVerification: test.clientVerifyFunc,
				RootOptions: RootCertificateOptions{
					RootCertificates:    test.clientRoot,
					GetRootCertificates: test.clientGetRoot,
				},
				VerificationType: test.clientVerificationType,
			}
			clientTLSCreds, err := NewClientCreds(clientOptions)
			if err != nil {
				t.Fatalf("clientTLSCreds failed to create: %v", err)
			}
			// ------------------------Scenario 1------------------------------------
			// stage = 0, initial connection should succeed
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			conn, greetClient, err := callAndVerifyWithClientConn(ctx, addr, "rpc call 1", clientTLSCreds, false)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			// ----------------------------------------------------------------------
			stage.increase()
			// ------------------------Scenario 2------------------------------------
			// stage = 1, previous connection should still succeed
			err = callAndVerify(ctx, "rpc call 2", greetClient, false)
			if err != nil {
				t.Fatal(err)
			}
			// ------------------------Scenario 3------------------------------------
			// stage = 1, new connection should fail
			ctx2, cancel2 := context.WithTimeout(context.Background(), defaultTestTimeout)
			conn2, _, err := callAndVerifyWithClientConn(ctx2, addr, "rpc call 3", clientTLSCreds, true)
			if err != nil {
				t.Fatal(err)
			}
			defer conn2.Close()
			// Immediately cancel the context so the dialing won't drag the entire timeout still it stops.
			cancel2()
			// ----------------------------------------------------------------------
			stage.increase()
			// ------------------------Scenario 4------------------------------------
			// stage = 2,  new connection should succeed
			conn3, _, err := callAndVerifyWithClientConn(ctx, addr, "rpc call 4", clientTLSCreds, false)
			if err != nil {
				t.Fatal(err)
			}
			defer conn3.Close()
			// ----------------------------------------------------------------------
			stage.reset()
		})
	}
}

type tmpCredsFiles struct {
	clientCertTmp  *os.File
	clientKeyTmp   *os.File
	clientTrustTmp *os.File
	serverCertTmp  *os.File
	serverKeyTmp   *os.File
	serverTrustTmp *os.File
}

// Create temp files that are used to hold credentials.
func createTmpFiles() (*tmpCredsFiles, error) {
	tmpFiles := &tmpCredsFiles{}
	var err error
	tmpFiles.clientCertTmp, err = os.CreateTemp(os.TempDir(), "pre-")
	if err != nil {
		return nil, err
	}
	tmpFiles.clientKeyTmp, err = os.CreateTemp(os.TempDir(), "pre-")
	if err != nil {
		return nil, err
	}
	tmpFiles.clientTrustTmp, err = os.CreateTemp(os.TempDir(), "pre-")
	if err != nil {
		return nil, err
	}
	tmpFiles.serverCertTmp, err = os.CreateTemp(os.TempDir(), "pre-")
	if err != nil {
		return nil, err
	}
	tmpFiles.serverKeyTmp, err = os.CreateTemp(os.TempDir(), "pre-")
	if err != nil {
		return nil, err
	}
	tmpFiles.serverTrustTmp, err = os.CreateTemp(os.TempDir(), "pre-")
	if err != nil {
		return nil, err
	}
	return tmpFiles, nil
}

// Copy the credential contents to the temporary files.
func (tmpFiles *tmpCredsFiles) copyCredsToTmpFiles() error {
	if err := copyFileContents(testdata.Path("client_cert_1.pem"), tmpFiles.clientCertTmp.Name()); err != nil {
		return err
	}
	if err := copyFileContents(testdata.Path("client_key_1.pem"), tmpFiles.clientKeyTmp.Name()); err != nil {
		return err
	}
	if err := copyFileContents(testdata.Path("client_trust_cert_1.pem"), tmpFiles.clientTrustTmp.Name()); err != nil {
		return err
	}
	if err := copyFileContents(testdata.Path("server_cert_1.pem"), tmpFiles.serverCertTmp.Name()); err != nil {
		return err
	}
	if err := copyFileContents(testdata.Path("server_key_1.pem"), tmpFiles.serverKeyTmp.Name()); err != nil {
		return err
	}
	if err := copyFileContents(testdata.Path("server_trust_cert_1.pem"), tmpFiles.serverTrustTmp.Name()); err != nil {
		return err
	}
	return nil
}

func (tmpFiles *tmpCredsFiles) removeFiles() {
	os.Remove(tmpFiles.clientCertTmp.Name())
	os.Remove(tmpFiles.clientKeyTmp.Name())
	os.Remove(tmpFiles.clientTrustTmp.Name())
	os.Remove(tmpFiles.serverCertTmp.Name())
	os.Remove(tmpFiles.serverKeyTmp.Name())
	os.Remove(tmpFiles.serverTrustTmp.Name())
}

func copyFileContents(sourceFile, destinationFile string) error {
	input, err := os.ReadFile(sourceFile)
	if err != nil {
		return err
	}
	err = os.WriteFile(destinationFile, input, 0644)
	if err != nil {
		return err
	}
	return nil
}

// Create PEMFileProvider(s) watching the content changes of temporary
// files.
func createProviders(tmpFiles *tmpCredsFiles) (certprovider.Provider, certprovider.Provider, certprovider.Provider, certprovider.Provider, error) {
	clientIdentityOptions := pemfile.Options{
		CertFile:        tmpFiles.clientCertTmp.Name(),
		KeyFile:         tmpFiles.clientKeyTmp.Name(),
		RefreshDuration: credRefreshingInterval,
	}
	clientIdentityProvider, err := pemfile.NewProvider(clientIdentityOptions)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	clientRootOptions := pemfile.Options{
		RootFile:        tmpFiles.clientTrustTmp.Name(),
		RefreshDuration: credRefreshingInterval,
	}
	clientRootProvider, err := pemfile.NewProvider(clientRootOptions)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	serverIdentityOptions := pemfile.Options{
		CertFile:        tmpFiles.serverCertTmp.Name(),
		KeyFile:         tmpFiles.serverKeyTmp.Name(),
		RefreshDuration: credRefreshingInterval,
	}
	serverIdentityProvider, err := pemfile.NewProvider(serverIdentityOptions)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	serverRootOptions := pemfile.Options{
		RootFile:        tmpFiles.serverTrustTmp.Name(),
		RefreshDuration: credRefreshingInterval,
	}
	serverRootProvider, err := pemfile.NewProvider(serverRootOptions)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return clientIdentityProvider, clientRootProvider, serverIdentityProvider, serverRootProvider, nil
}

// In order to test advanced TLS provider features, we used temporary files to
// hold credential data, and copy the contents under testdata/ to these tmp
// files.
// Initially, we establish a good connection with providers watching contents
// from tmp files.
// Next, we change the identity certs that IdentityProvider is watching. Since
// the identity key is not changed, the IdentityProvider should ignore the
// update, and the connection should still be good.
// Then the identity key is changed. This time IdentityProvider should pick
// up the update, and the connection should fail, due to the trust certs on the
// other side is not changed.
// Finally, the trust certs that other-side's RootProvider is watching get
// changed. The connection should go back to normal again.
func (s) TestPEMFileProviderEnd2End(t *testing.T) {
	tmpFiles, err := createTmpFiles()
	if err != nil {
		t.Fatalf("createTmpFiles() failed, error: %v", err)
	}
	defer tmpFiles.removeFiles()
	for _, test := range []struct {
		desc                string
		certUpdateFunc      func()
		keyUpdateFunc       func()
		trustCertUpdateFunc func()
	}{
		{
			desc: "test the reloading feature for clientIdentityProvider and serverTrustProvider",
			certUpdateFunc: func() {
				err = copyFileContents(testdata.Path("client_cert_2.pem"), tmpFiles.clientCertTmp.Name())
				if err != nil {
					t.Fatalf("copyFileContents(%s, %s) failed: %v", testdata.Path("client_cert_2.pem"), tmpFiles.clientCertTmp.Name(), err)
				}
			},
			keyUpdateFunc: func() {
				err = copyFileContents(testdata.Path("client_key_2.pem"), tmpFiles.clientKeyTmp.Name())
				if err != nil {
					t.Fatalf("copyFileContents(%s, %s) failed: %v", testdata.Path("client_key_2.pem"), tmpFiles.clientKeyTmp.Name(), err)
				}
			},
			trustCertUpdateFunc: func() {
				err = copyFileContents(testdata.Path("server_trust_cert_2.pem"), tmpFiles.serverTrustTmp.Name())
				if err != nil {
					t.Fatalf("copyFileContents(%s, %s) failed: %v", testdata.Path("server_trust_cert_2.pem"), tmpFiles.serverTrustTmp.Name(), err)
				}
			},
		},
		{
			desc: "test the reloading feature for serverIdentityProvider and clientTrustProvider",
			certUpdateFunc: func() {
				err = copyFileContents(testdata.Path("server_cert_2.pem"), tmpFiles.serverCertTmp.Name())
				if err != nil {
					t.Fatalf("copyFileContents(%s, %s) failed: %v", testdata.Path("server_cert_2.pem"), tmpFiles.serverCertTmp.Name(), err)
				}
			},
			keyUpdateFunc: func() {
				err = copyFileContents(testdata.Path("server_key_2.pem"), tmpFiles.serverKeyTmp.Name())
				if err != nil {
					t.Fatalf("copyFileContents(%s, %s) failed: %v", testdata.Path("server_key_2.pem"), tmpFiles.serverKeyTmp.Name(), err)
				}
			},
			trustCertUpdateFunc: func() {
				err = copyFileContents(testdata.Path("client_trust_cert_2.pem"), tmpFiles.clientTrustTmp.Name())
				if err != nil {
					t.Fatalf("copyFileContents(%s, %s) failed: %v", testdata.Path("client_trust_cert_2.pem"), tmpFiles.clientTrustTmp.Name(), err)
				}
			},
		},
	} {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			if err := tmpFiles.copyCredsToTmpFiles(); err != nil {
				t.Fatalf("tmpFiles.copyCredsToTmpFiles() failed, error: %v", err)
			}
			clientIdentityProvider, clientRootProvider, serverIdentityProvider, serverRootProvider, err := createProviders(tmpFiles)
			if err != nil {
				t.Fatalf("createProviders(%v) failed, error: %v", tmpFiles, err)
			}
			defer clientIdentityProvider.Close()
			defer clientRootProvider.Close()
			defer serverIdentityProvider.Close()
			defer serverRootProvider.Close()
			// Start a server and create a client using advancedtls API with Provider.
			serverOptions := &Options{
				IdentityOptions: IdentityCertificateOptions{
					IdentityProvider: serverIdentityProvider,
				},
				RootOptions: RootCertificateOptions{
					RootProvider: serverRootProvider,
				},
				RequireClientCert: true,
				AdditionalPeerVerification: func(*HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
					return &PostHandshakeVerificationResults{}, nil
				},
				VerificationType: CertVerification,
			}
			serverTLSCreds, err := NewServerCreds(serverOptions)
			if err != nil {
				t.Fatalf("failed to create server creds: %v", err)
			}
			s := grpc.NewServer(grpc.Creds(serverTLSCreds))
			defer s.Stop()
			lis, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("failed to listen: %v", err)
			}
			defer lis.Close()
			addr := fmt.Sprintf("localhost:%v", lis.Addr().(*net.TCPAddr).Port)
			pb.RegisterGreeterServer(s, greeterServer{})
			go s.Serve(lis)
			clientOptions := &Options{
				IdentityOptions: IdentityCertificateOptions{
					IdentityProvider: clientIdentityProvider,
				},
				AdditionalPeerVerification: func(*HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error) {
					return &PostHandshakeVerificationResults{}, nil
				},
				RootOptions: RootCertificateOptions{
					RootProvider: clientRootProvider,
				},
				VerificationType: CertVerification,
			}
			clientTLSCreds, err := NewClientCreds(clientOptions)
			if err != nil {
				t.Fatalf("clientTLSCreds failed to create, error: %v", err)
			}

			// At initialization, the connection should be good.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			conn, greetClient, err := callAndVerifyWithClientConn(ctx, addr, "rpc call 1", clientTLSCreds, false)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			// Make the identity cert change, and wait 1 second for the provider to
			// pick up the change.
			test.certUpdateFunc()
			time.Sleep(sleepInterval)
			// The already-established connection should not be affected.
			err = callAndVerify(ctx, "rpc call 2", greetClient, false)
			if err != nil {
				t.Fatal(err)
			}
			// New connections should still be good, because the Provider didn't pick
			// up the changes due to key-cert mismatch.
			conn2, _, err := callAndVerifyWithClientConn(ctx, addr, "rpc call 3", clientTLSCreds, false)
			if err != nil {
				t.Fatal(err)
			}
			defer conn2.Close()
			// Make the identity key change, and wait 1 second for the provider to
			// pick up the change.
			test.keyUpdateFunc()
			time.Sleep(sleepInterval)
			// New connections should fail now, because the Provider picked the
			// change, and *_cert_2.pem is not trusted by *_trust_cert_1.pem on the
			// other side.
			ctx2, cancel2 := context.WithTimeout(context.Background(), defaultTestTimeout)
			conn3, _, err := callAndVerifyWithClientConn(ctx2, addr, "rpc call 4", clientTLSCreds, true)
			if err != nil {
				t.Fatal(err)
			}
			defer conn3.Close()
			// Immediately cancel the context so the dialing won't drag the entire timeout still it stops.
			cancel2()
			// Make the trust cert change on the other side, and wait 1 second for
			// the provider to pick up the change.
			test.trustCertUpdateFunc()
			time.Sleep(sleepInterval)
			// New connections should be good, because the other side is using
			// *_trust_cert_2.pem now.
			conn4, _, err := callAndVerifyWithClientConn(ctx, addr, "rpc call 5", clientTLSCreds, false)
			if err != nil {
				t.Fatal(err)
			}
			defer conn4.Close()
		})
	}
}

func (s) TestDefaultHostNameCheck(t *testing.T) {
	cs := &testutils.CertStore{}
	if err := cs.LoadCerts(); err != nil {
		t.Fatalf("cs.LoadCerts() failed, err: %v", err)
	}
	for _, test := range []struct {
		desc                   string
		clientRoot             *x509.CertPool
		clientVerificationType VerificationType
		serverCert             []tls.Certificate
		serverVerificationType VerificationType
		expectError            bool
	}{
		// Client side sets vType to CertAndHostVerification, and will do
		// default hostname check. Server uses a cert without "localhost" or
		// "127.0.0.1" as common name or SAN names, and will hence fail.
		{
			desc:                   "Bad default hostname check",
			clientRoot:             cs.ClientTrust1,
			clientVerificationType: CertAndHostVerification,
			serverCert:             []tls.Certificate{cs.ServerCert1},
			serverVerificationType: CertAndHostVerification,
			expectError:            true,
		},
		// Client side sets vType to CertAndHostVerification, and will do
		// default hostname check. Server uses a certificate with "localhost" as
		// common name, and will hence pass the default hostname check.
		{
			desc:                   "Good default hostname check",
			clientRoot:             cs.ClientTrust1,
			clientVerificationType: CertAndHostVerification,
			serverCert:             []tls.Certificate{cs.ServerPeerLocalhost1},
			serverVerificationType: CertAndHostVerification,
			expectError:            false,
		},
	} {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			// Start a server using ServerOptions in another goroutine.
			serverOptions := &Options{
				IdentityOptions: IdentityCertificateOptions{
					Certificates: test.serverCert,
				},
				RequireClientCert: false,
				VerificationType:  test.serverVerificationType,
			}
			serverTLSCreds, err := NewServerCreds(serverOptions)
			if err != nil {
				t.Fatalf("failed to create server creds: %v", err)
			}
			s := grpc.NewServer(grpc.Creds(serverTLSCreds))
			defer s.Stop()
			lis, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("failed to listen: %v", err)
			}
			defer lis.Close()
			addr := fmt.Sprintf("localhost:%v", lis.Addr().(*net.TCPAddr).Port)
			pb.RegisterGreeterServer(s, greeterServer{})
			go s.Serve(lis)
			clientOptions := &Options{
				RootOptions: RootCertificateOptions{
					RootCertificates: test.clientRoot,
				},
				VerificationType: test.clientVerificationType,
			}
			clientTLSCreds, err := NewClientCreds(clientOptions)
			if err != nil {
				t.Fatalf("clientTLSCreds failed to create: %v", err)
			}
			shouldFail := false
			if test.expectError {
				shouldFail = true
			}
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			conn, _, err := callAndVerifyWithClientConn(ctx, addr, "rpc call 1", clientTLSCreds, shouldFail)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
		})
	}
}

func (s) TestTLSVersions(t *testing.T) {
	cs := &testutils.CertStore{}
	if err := cs.LoadCerts(); err != nil {
		t.Fatalf("cs.LoadCerts() failed, err: %v", err)
	}
	for _, test := range []struct {
		desc             string
		expectError      bool
		clientMinVersion uint16
		clientMaxVersion uint16
		serverMinVersion uint16
		serverMaxVersion uint16
	}{
		// Client side sets TLS version that is higher than required from the server side.
		{
			desc:             "Client TLS version higher than server",
			clientMinVersion: tls.VersionTLS13,
			clientMaxVersion: tls.VersionTLS13,
			serverMinVersion: tls.VersionTLS12,
			serverMaxVersion: tls.VersionTLS12,
			expectError:      true,
		},
		// Server side sets TLS version that is higher than required from the client side.
		{
			desc:             "Server TLS version higher than client",
			clientMinVersion: tls.VersionTLS12,
			clientMaxVersion: tls.VersionTLS12,
			serverMinVersion: tls.VersionTLS13,
			serverMaxVersion: tls.VersionTLS13,
			expectError:      true,
		},
		// Client and server set proper TLS versions.
		{
			desc:             "Good TLS version settings",
			clientMinVersion: tls.VersionTLS12,
			clientMaxVersion: tls.VersionTLS13,
			serverMinVersion: tls.VersionTLS12,
			serverMaxVersion: tls.VersionTLS13,
			expectError:      false,
		},
		{
			desc:             "Client 1.2 - 1.3 and server 1.2",
			clientMinVersion: tls.VersionTLS12,
			clientMaxVersion: tls.VersionTLS13,
			serverMinVersion: tls.VersionTLS12,
			serverMaxVersion: tls.VersionTLS12,
			expectError:      false,
		},
		{
			desc:             "Client 1.2 - 1.3 and server 1.1 - 1.2",
			clientMinVersion: tls.VersionTLS12,
			clientMaxVersion: tls.VersionTLS13,
			serverMinVersion: tls.VersionTLS11,
			serverMaxVersion: tls.VersionTLS12,
			expectError:      false,
		},
		{
			desc:             "Client 1.2 - 1.3 and server 1.3",
			clientMinVersion: tls.VersionTLS12,
			clientMaxVersion: tls.VersionTLS13,
			serverMinVersion: tls.VersionTLS13,
			serverMaxVersion: tls.VersionTLS13,
			expectError:      false,
		},
		{
			desc:             "Client 1.2 - 1.2 and server 1.2 - 1.3",
			clientMinVersion: tls.VersionTLS12,
			clientMaxVersion: tls.VersionTLS12,
			serverMinVersion: tls.VersionTLS12,
			serverMaxVersion: tls.VersionTLS13,
			expectError:      false,
		},
		{
			desc:             "Client 1.1 - 1.2 and server 1.2 - 1.3",
			clientMinVersion: tls.VersionTLS11,
			clientMaxVersion: tls.VersionTLS12,
			serverMinVersion: tls.VersionTLS12,
			serverMaxVersion: tls.VersionTLS13,
			expectError:      false,
		},
		{
			desc:             "Client 1.3 and server 1.2 - 1.3",
			clientMinVersion: tls.VersionTLS13,
			clientMaxVersion: tls.VersionTLS13,
			serverMinVersion: tls.VersionTLS12,
			serverMaxVersion: tls.VersionTLS13,
			expectError:      false,
		},
	} {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			// Start a server using ServerOptions in another goroutine.
			serverOptions := &Options{
				IdentityOptions: IdentityCertificateOptions{
					Certificates: []tls.Certificate{cs.ServerPeerLocalhost1},
				},
				RequireClientCert: false,
				VerificationType:  CertAndHostVerification,
				MinTLSVersion:     test.serverMinVersion,
				MaxTLSVersion:     test.serverMaxVersion,
			}
			serverTLSCreds, err := NewServerCreds(serverOptions)
			if err != nil {
				t.Fatalf("failed to create server creds: %v", err)
			}
			s := grpc.NewServer(grpc.Creds(serverTLSCreds))
			defer s.Stop()
			lis, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("failed to listen: %v", err)
			}
			defer lis.Close()
			addr := fmt.Sprintf("localhost:%v", lis.Addr().(*net.TCPAddr).Port)
			pb.RegisterGreeterServer(s, greeterServer{})
			go s.Serve(lis)
			clientOptions := &Options{
				RootOptions: RootCertificateOptions{
					RootCertificates: cs.ClientTrust1,
				},
				VerificationType: CertAndHostVerification,
				MinTLSVersion:    test.clientMinVersion,
				MaxTLSVersion:    test.clientMaxVersion,
			}
			clientTLSCreds, err := NewClientCreds(clientOptions)
			if err != nil {
				t.Fatalf("clientTLSCreds failed to create: %v", err)
			}
			shouldFail := false
			if test.expectError {
				shouldFail = true
			}
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			conn, _, err := callAndVerifyWithClientConn(ctx, addr, "rpc call 1", clientTLSCreds, shouldFail)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
		})
	}
}
