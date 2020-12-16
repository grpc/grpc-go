// +build !386

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

// Package xds_test contains e2e tests for xDS use.
package xds_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"strconv"
	"testing"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	xdscreds "google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/status"
	testpb "google.golang.org/grpc/test/grpc_testing"
	"google.golang.org/grpc/testdata"
	"google.golang.org/grpc/xds"
	"google.golang.org/grpc/xds/internal/env"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/e2e"
	"google.golang.org/grpc/xds/internal/version"
)

const (
	// Names of files inside tempdir, for certprovider plugin to watch.
	certFile = "cert.pem"
	keyFile  = "key.pem"
	rootFile = "ca.pem"
)

func createTmpFile(t *testing.T, src, dst string) {
	t.Helper()

	data, err := ioutil.ReadFile(src)
	if err != nil {
		t.Fatalf("ioutil.ReadFile(%q) failed: %v", src, err)
	}
	if err := ioutil.WriteFile(dst, data, os.ModePerm); err != nil {
		t.Fatalf("ioutil.WriteFile(%q) failed: %v", dst, err)
	}
	t.Logf("Wrote file at: %s", dst)
	t.Logf("%s", string(data))
}

// createTempDirWithFiles creates a temporary directory under the system default
// tempDir with the given dirSuffix. It also reads from certSrc, keySrc and
// rootSrc files are creates appropriate files under the newly create tempDir.
// Returns the name of the created tempDir.
func createTmpDirWithFiles(t *testing.T, dirSuffix, certSrc, keySrc, rootSrc string) string {
	t.Helper()

	// Create a temp directory. Passing an empty string for the first argument
	// uses the system temp directory.
	dir, err := ioutil.TempDir("", dirSuffix)
	if err != nil {
		t.Fatalf("ioutil.TempDir() failed: %v", err)
	}
	t.Logf("Using tmpdir: %s", dir)

	createTmpFile(t, testdata.Path(certSrc), path.Join(dir, certFile))
	createTmpFile(t, testdata.Path(keySrc), path.Join(dir, keyFile))
	createTmpFile(t, testdata.Path(rootSrc), path.Join(dir, rootFile))
	return dir
}

// createClientTLSCredentials creates client-side TLS transport credentials.
func createClientTLSCredentials(t *testing.T) credentials.TransportCredentials {
	cert, err := tls.LoadX509KeyPair(testdata.Path("x509/client1_cert.pem"), testdata.Path("x509/client1_key.pem"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(x509/client1_cert.pem, x509/client1_key.pem) failed: %v", err)
	}
	b, err := ioutil.ReadFile(testdata.Path("x509/server_ca_cert.pem"))
	if err != nil {
		t.Fatalf("ioutil.ReadFile(x509/server_ca_cert.pem) failed: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(b) {
		t.Fatal("failed to append certificates")
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      roots,
		ServerName:   "x.test.example.com",
	})
}

// commonSetup performs a bunch of steps common to all xDS server tests here:
// - turn on V3 support by setting the environment variable
// - spin up an xDS management server on a local port
// - set up certificates for consumption by the file_watcher plugin
// - spin up an xDS-enabled gRPC server, configure it with xdsCredentials and
//   register the test service on it
// - create a local TCP listener and start serving on it
//
// Returns the following:
// - the management server: tests use this to configure resources
// - nodeID expected by the management server: this is set in the Node proto
//   sent by the xdsClient used on the xDS-enabled gRPC server
// - local listener on which the xDS-enabled gRPC server is serving on
// - cleanup function to be invoked by the tests when done
func commonSetup(t *testing.T) (*e2e.ManagementServer, string, net.Listener, func()) {
	t.Helper()

	// Turn on xDS V3 support.
	origV3Support := env.V3Support
	env.V3Support = true

	// Spin up a xDS management server on a local port.
	nodeID := uuid.New().String()
	fs, err := e2e.StartManagementServer()
	if err != nil {
		t.Fatal(err)
	}

	// Create certificate and key files in a temporary directory and generate
	// certificate provider configuration for a file_watcher plugin.
	tmpdir := createTmpDirWithFiles(t, "testServerSideXDS*", "x509/server1_cert.pem", "x509/server1_key.pem", "x509/client_ca_cert.pem")
	cpc := e2e.DefaultFileWatcherConfig(path.Join(tmpdir, certFile), path.Join(tmpdir, keyFile), path.Join(tmpdir, rootFile))

	// Create a bootstrap file in a temporary directory.
	bootstrapCleanup, err := e2e.SetupBootstrapFile(e2e.BootstrapOptions{
		Version:              e2e.TransportV3,
		NodeID:               nodeID,
		ServerURI:            fs.Address,
		CertificateProviders: cpc,
		ServerResourceNameID: "grpc/server",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Configure xDS credentials to be used on the server-side.
	creds, err := xdscreds.NewServerCredentials(xdscreds.ServerOptions{
		FallbackCreds: insecure.NewCredentials(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Initialize an xDS-enabled gRPC server and register the stubServer on it.
	server := xds.NewGRPCServer(grpc.Creds(creds))
	testpb.RegisterTestServiceServer(server, &testService{})

	// Create a local listener and pass it to Serve().
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}

	go func() {
		if err := server.Serve(lis); err != nil {
			t.Errorf("Serve() failed: %v", err)
		}
	}()

	return fs, nodeID, lis, func() {
		env.V3Support = origV3Support
		fs.Stop()
		bootstrapCleanup()
		server.Stop()
	}
}

func hostPortFromListener(t *testing.T, lis net.Listener) (string, uint32) {
	t.Helper()

	host, p, err := net.SplitHostPort(lis.Addr().String())
	if err != nil {
		t.Fatalf("net.SplitHostPort(%s) failed: %v", lis.Addr().String(), err)
	}
	port, err := strconv.ParseInt(p, 10, 32)
	if err != nil {
		t.Fatalf("strconv.ParseInt(%s, 10, 32) failed: %v", p, err)
	}
	return host, uint32(port)

}

// listenerResourceWithoutSecurityConfig returns a listener resource with no
// security configuration, and name and address fields matching the passed in
// net.Listener.
func listenerResourceWithoutSecurityConfig(t *testing.T, lis net.Listener) *v3listenerpb.Listener {
	host, port := hostPortFromListener(t, lis)
	return &v3listenerpb.Listener{
		// This needs to match the name we are querying for.
		Name: fmt.Sprintf("grpc/server?udpa.resource.listening_address=%s", lis.Addr().String()),
		Address: &v3corepb.Address{
			Address: &v3corepb.Address_SocketAddress{
				SocketAddress: &v3corepb.SocketAddress{
					Address: host,
					PortSpecifier: &v3corepb.SocketAddress_PortValue{
						PortValue: port,
					},
				},
			},
		},
		FilterChains: []*v3listenerpb.FilterChain{
			{
				Name: "filter-chain-1",
			},
		},
	}
}

// listenerResourceWithSecurityConfig returns a listener resource with security
// configuration pointing to the use of the file_watcher certificate provider
// plugin, and name and address fields matching the passed in net.Listener.
func listenerResourceWithSecurityConfig(t *testing.T, lis net.Listener) *v3listenerpb.Listener {
	host, port := hostPortFromListener(t, lis)
	return &v3listenerpb.Listener{
		// This needs to match the name we are querying for.
		Name: fmt.Sprintf("grpc/server?udpa.resource.listening_address=%s", lis.Addr().String()),
		Address: &v3corepb.Address{
			Address: &v3corepb.Address_SocketAddress{
				SocketAddress: &v3corepb.SocketAddress{
					Address: host,
					PortSpecifier: &v3corepb.SocketAddress_PortValue{
						PortValue: port,
					}}}},
		FilterChains: []*v3listenerpb.FilterChain{
			{
				Name: "filter-chain-1",
				TransportSocket: &v3corepb.TransportSocket{
					Name: "envoy.transport_sockets.tls",
					ConfigType: &v3corepb.TransportSocket_TypedConfig{
						TypedConfig: &anypb.Any{
							TypeUrl: version.V3DownstreamTLSContextURL,
							Value: func() []byte {
								tls := &v3tlspb.DownstreamTlsContext{
									RequireClientCertificate: &wrapperspb.BoolValue{Value: true},
									CommonTlsContext: &v3tlspb.CommonTlsContext{
										TlsCertificateCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
											InstanceName: "google_cloud_private_spiffe",
										},
										ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance{
											ValidationContextCertificateProviderInstance: &v3tlspb.CommonTlsContext_CertificateProviderInstance{
												InstanceName: "google_cloud_private_spiffe",
											}}}}
								mtls, _ := proto.Marshal(tls)
								return mtls
							}(),
						}}}}},
	}
}

// TestServerSideXDS_Fallback is an e2e test where xDS is enabled on the
// server-side and xdsCredentials are configured for security. The control plane
// does not provide any security configuration and therefore the xdsCredentials
// uses fallback credentials, which in this case is insecure creds.
func (s) TestServerSideXDS_Fallback(t *testing.T) {
	fs, nodeID, lis, cleanup := commonSetup(t)
	defer cleanup()

	// Setup the fake management server to respond with a Listener resource that
	// does not contain any security configuration. This should force the
	// server-side xdsCredentials to use fallback.
	listener := listenerResourceWithoutSecurityConfig(t, lis)
	if err := fs.Update(e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener},
	}); err != nil {
		t.Error(err)
	}

	// Create a ClientConn and make a successful RPC.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc, err := grpc.DialContext(ctx, lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testpb.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}

// TestServerSideXDS_FileWatcherCerts is an e2e test where xDS is enabled on the
// server-side and xdsCredentials are configured for security. The control plane
// sends security configuration pointing to the use of the file_watcher plugin,
// and we verify that a client connecting with TLS creds is able to successfully
// make an RPC.
func (s) TestServerSideXDS_FileWatcherCerts(t *testing.T) {
	fs, nodeID, lis, cleanup := commonSetup(t)
	defer cleanup()

	// Setup the fake management server to respond with a Listener resource with
	// security configuration pointing to the file watcher plugin and requiring
	// mTLS.
	listener := listenerResourceWithSecurityConfig(t, lis)
	if err := fs.Update(e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener},
	}); err != nil {
		t.Error(err)
	}

	// Create a ClientConn with TLS creds and make a successful RPC.
	clientCreds := createClientTLSCredentials(t)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc, err := grpc.DialContext(ctx, lis.Addr().String(), grpc.WithTransportCredentials(clientCreds))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testpb.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}

// TestServerSideXDS_SecurityConfigChange is an e2e test where xDS is enabled on
// the server-side and xdsCredentials are configured for security. The control
// plane initially does not any security configuration. This forces the
// xdsCredentials to use fallback creds, which is this case is insecure creds.
// We verify that a client connecting with TLS creds is not able to successfully
// make an RPC. The control plan then sends a listener resource with security
// configuration pointing to the use of the file_watcher plugin and we verify
// that the same client is now able to successfully make an RPC.
func (s) TestServerSideXDS_SecurityConfigChange(t *testing.T) {
	fs, nodeID, lis, cleanup := commonSetup(t)
	defer cleanup()

	// Setup the fake management server to respond with a Listener resource that
	// does not contain any security configuration. This should force the
	// server-side xdsCredentials to use fallback.
	listener := listenerResourceWithoutSecurityConfig(t, lis)
	if err := fs.Update(e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener},
	}); err != nil {
		t.Error(err)
	}

	// Create a ClientConn with TLS creds. This should fail since the server is
	// using fallback credentials which in this case in insecure creds.
	clientCreds := createClientTLSCredentials(t)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc, err := grpc.DialContext(ctx, lis.Addr().String(), grpc.WithTransportCredentials(clientCreds))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	// We don't set 'waitForReady` here since we want this call to failfast.
	client := testpb.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); status.Convert(err).Code() != codes.Unavailable {
		t.Fatal("rpc EmptyCall() succeeded when expected to fail")
	}

	// Setup the fake management server to respond with a Listener resource with
	// security configuration pointing to the file watcher plugin and requiring
	// mTLS.
	listener = listenerResourceWithSecurityConfig(t, lis)
	if err := fs.Update(e2e.UpdateOptions{
		NodeID:    nodeID,
		Listeners: []*v3listenerpb.Listener{listener},
	}); err != nil {
		t.Error(err)
	}

	// Make another RPC with `waitForReady` set and expect this to succeed.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}
