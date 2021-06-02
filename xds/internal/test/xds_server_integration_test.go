// +build go1.12
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
	"fmt"
	"net"
	"strconv"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds"
	"google.golang.org/grpc/xds/internal/testutils/e2e"

	xdscreds "google.golang.org/grpc/credentials/xds"
	testpb "google.golang.org/grpc/test/grpc_testing"
	xdstestutils "google.golang.org/grpc/xds/internal/testutils"
)

const (
	// Names of files inside tempdir, for certprovider plugin to watch.
	certFile = "cert.pem"
	keyFile  = "key.pem"
	rootFile = "ca.pem"
)

// setupGRPCServer performs the following:
// - spin up an xDS-enabled gRPC server, configure it with xdsCredentials and
//   register the test service on it
// - create a local TCP listener and start serving on it
//
// Returns the following:
// - local listener on which the xDS-enabled gRPC server is serving on
// - cleanup function to be invoked by the tests when done
func setupGRPCServer(t *testing.T) (net.Listener, func()) {
	t.Helper()

	// Configure xDS credentials to be used on the server-side.
	creds, err := xdscreds.NewServerCredentials(xdscreds.ServerOptions{
		FallbackCreds: insecure.NewCredentials(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Initialize an xDS-enabled gRPC server and register the stubServer on it.
	server := xds.NewGRPCServer(grpc.Creds(creds), xds.BootstrapContentsForTesting(bootstrapContents))
	testpb.RegisterTestServiceServer(server, &testService{})

	// Create a local listener and pass it to Serve().
	lis, err := xdstestutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}

	go func() {
		if err := server.Serve(lis); err != nil {
			t.Errorf("Serve() failed: %v", err)
		}
	}()

	return lis, func() {
		server.Stop()
	}
}

func hostPortFromListener(lis net.Listener) (string, uint32, error) {
	host, p, err := net.SplitHostPort(lis.Addr().String())
	if err != nil {
		return "", 0, fmt.Errorf("net.SplitHostPort(%s) failed: %v", lis.Addr().String(), err)
	}
	port, err := strconv.ParseInt(p, 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("strconv.ParseInt(%s, 10, 32) failed: %v", p, err)
	}
	return host, uint32(port), nil
}

// TestServerSideXDS_Fallback is an e2e test which verifies xDS credentials
// fallback functionality.
//
// The following sequence of events happen as part of this test:
// - An xDS-enabled gRPC server is created and xDS credentials are configured.
// - xDS is enabled on the client by the use of the xds:/// scheme, and xDS
//   credentials are configured.
// - Control plane is configured to not send any security configuration to both
//   the client and the server. This results in both of them using the
//   configured fallback credentials (which is insecure creds in this case).
func (s) TestServerSideXDS_Fallback(t *testing.T) {
	lis, cleanup := setupGRPCServer(t)
	defer cleanup()

	// Grab the host and port of the server and create client side xDS resources
	// corresponding to it. This contains default resources with no security
	// configuration in the Cluster resources.
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("failed to retrieve host and port of server: %v", err)
	}
	const serviceName = "my-service-fallback"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     xdsClientNodeID,
		Host:       host,
		Port:       port,
		SecLevel:   e2e.SecurityLevelNone,
	})

	// Create an inbound xDS listener resource for the server side that does not
	// contain any security configuration. This should force the server-side
	// xdsCredentials to use fallback.
	inboundLis := e2e.DefaultServerListener(host, port, e2e.SecurityLevelNone)
	resources.Listeners = append(resources.Listeners, inboundLis)

	// Setup the management server with client and server-side resources.
	if err := managementServer.Update(resources); err != nil {
		t.Fatal(err)
	}

	// Create client-side xDS credentials with an insecure fallback.
	creds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{
		FallbackCreds: insecure.NewCredentials(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn with the xds scheme and make a successful RPC.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc, err := grpc.DialContext(ctx, fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(creds), grpc.WithResolvers(xdsResolverBuilder))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testpb.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Errorf("rpc EmptyCall() failed: %v", err)
	}
}

// TestServerSideXDS_FileWatcherCerts is an e2e test which verifies xDS
// credentials with file watcher certificate provider.
//
// The following sequence of events happen as part of this test:
// - An xDS-enabled gRPC server is created and xDS credentials are configured.
// - xDS is enabled on the client by the use of the xds:/// scheme, and xDS
//   credentials are configured.
// - Control plane is configured to send security configuration to both the
//   client and the server, pointing to the file watcher certificate provider.
//   We verify both TLS and mTLS scenarios.
func (s) TestServerSideXDS_FileWatcherCerts(t *testing.T) {
	tests := []struct {
		name     string
		secLevel e2e.SecurityLevel
	}{
		{
			name:     "tls",
			secLevel: e2e.SecurityLevelTLS,
		},
		{
			name:     "mtls",
			secLevel: e2e.SecurityLevelMTLS,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lis, cleanup := setupGRPCServer(t)
			defer cleanup()

			// Grab the host and port of the server and create client side xDS
			// resources corresponding to it.
			host, port, err := hostPortFromListener(lis)
			if err != nil {
				t.Fatalf("failed to retrieve host and port of server: %v", err)
			}

			// Create xDS resources to be consumed on the client side. This
			// includes the listener, route configuration, cluster (with
			// security configuration) and endpoint resources.
			serviceName := "my-service-file-watcher-certs-" + test.name
			resources := e2e.DefaultClientResources(e2e.ResourceParams{
				DialTarget: serviceName,
				NodeID:     xdsClientNodeID,
				Host:       host,
				Port:       port,
				SecLevel:   test.secLevel,
			})

			// Create an inbound xDS listener resource for the server side that
			// contains security configuration pointing to the file watcher
			// plugin.
			inboundLis := e2e.DefaultServerListener(host, port, test.secLevel)
			resources.Listeners = append(resources.Listeners, inboundLis)

			// Setup the management server with client and server resources.
			if err := managementServer.Update(resources); err != nil {
				t.Fatal(err)
			}

			// Create client-side xDS credentials with an insecure fallback.
			creds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{
				FallbackCreds: insecure.NewCredentials(),
			})
			if err != nil {
				t.Fatal(err)
			}

			// Create a ClientConn with the xds scheme and make an RPC.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			cc, err := grpc.DialContext(ctx, fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(creds), grpc.WithResolvers(xdsResolverBuilder))
			if err != nil {
				t.Fatalf("failed to dial local test server: %v", err)
			}
			defer cc.Close()

			client := testpb.NewTestServiceClient(cc)
			if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
				t.Fatalf("rpc EmptyCall() failed: %v", err)
			}
		})
	}
}

// TestServerSideXDS_SecurityConfigChange is an e2e test where xDS is enabled on
// the server-side and xdsCredentials are configured for security. The control
// plane initially does not any security configuration. This forces the
// xdsCredentials to use fallback creds, which is this case is insecure creds.
// We verify that a client connecting with TLS creds is not able to successfully
// make an RPC. The control plane then sends a listener resource with security
// configuration pointing to the use of the file_watcher plugin and we verify
// that the same client is now able to successfully make an RPC.
func (s) TestServerSideXDS_SecurityConfigChange(t *testing.T) {
	lis, cleanup := setupGRPCServer(t)
	defer cleanup()

	// Grab the host and port of the server and create client side xDS resources
	// corresponding to it. This contains default resources with no security
	// configuration in the Cluster resource. This should force the xDS
	// credentials on the client to use its fallback.
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("failed to retrieve host and port of server: %v", err)
	}
	const serviceName = "my-service-security-config-change"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     xdsClientNodeID,
		Host:       host,
		Port:       port,
		SecLevel:   e2e.SecurityLevelNone,
	})

	// Create an inbound xDS listener resource for the server side that does not
	// contain any security configuration. This should force the xDS credentials
	// on server to use its fallback.
	inboundLis := e2e.DefaultServerListener(host, port, e2e.SecurityLevelNone)
	resources.Listeners = append(resources.Listeners, inboundLis)

	// Setup the management server with client and server-side resources.
	if err := managementServer.Update(resources); err != nil {
		t.Fatal(err)
	}

	// Create client-side xDS credentials with an insecure fallback.
	xdsCreds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{
		FallbackCreds: insecure.NewCredentials(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn with the xds scheme and make a successful RPC.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	xdsCC, err := grpc.DialContext(ctx, fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(xdsCreds), grpc.WithResolvers(xdsResolverBuilder))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer xdsCC.Close()

	client := testpb.NewTestServiceClient(xdsCC)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// Create a ClientConn with TLS creds. This should fail since the server is
	// using fallback credentials which in this case in insecure creds.
	tlsCreds := createClientTLSCredentials(t)
	tlsCC, err := grpc.DialContext(ctx, lis.Addr().String(), grpc.WithTransportCredentials(tlsCreds))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer tlsCC.Close()

	// We don't set 'waitForReady` here since we want this call to failfast.
	client = testpb.NewTestServiceClient(tlsCC)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.Unavailable {
		t.Fatal("rpc EmptyCall() succeeded when expected to fail")
	}

	// Switch server and client side resources with ones that contain required
	// security configuration for mTLS with a file watcher certificate provider.
	resources = e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     xdsClientNodeID,
		Host:       host,
		Port:       port,
		SecLevel:   e2e.SecurityLevelMTLS,
	})
	inboundLis = e2e.DefaultServerListener(host, port, e2e.SecurityLevelMTLS)
	resources.Listeners = append(resources.Listeners, inboundLis)
	if err := managementServer.Update(resources); err != nil {
		t.Fatal(err)
	}

	// Make another RPC with `waitForReady` set and expect this to succeed.
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}
