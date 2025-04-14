/*
 *
 * Copyright 2021 gRPC authors.
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

package xds_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/e2e/setup"
	"google.golang.org/grpc/testdata"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond // For events expected to *not* happen.
)

func (s) TestClientSideXDS(t *testing.T) {
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	const serviceName = "my-service-client-side-xds"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}

// Helper function to create a real TLS client credentials which is used as
// fallback credentials from multiple tests.
func makeFallbackClientCreds(t *testing.T, useSPIFFECreds bool, isMTLS bool) credentials.TransportCredentials {
	var creds credentials.TransportCredentials
	var err error
	if useSPIFFECreds {
		if isMTLS {
			b, err := os.ReadFile(testdata.Path("spiffe_end2end/ca.pem"))
			if err != nil {
				t.Fatal(err)
			}
			cp := x509.NewCertPool()
			if !cp.AppendCertsFromPEM(b) {
				t.Fatalf("failed to append certificates")
			}
			cert, err := tls.LoadX509KeyPair(testdata.Path("spiffe_end2end/client_spiffe.pem"), testdata.Path("spiffe_end2end/client.key"))
			if err != nil {
				t.Fatal(err)
			}
			creds = credentials.NewTLS(&tls.Config{
				ServerName:   "x.test.example.com",
				RootCAs:      cp,
				Certificates: []tls.Certificate{cert},
			})
		} else {
			creds, err = credentials.NewClientTLSFromFile(testdata.Path("spiffe_end2end/ca.pem"), "x.test.example.com")
		}
	} else {
		if isMTLS {
			b, err := os.ReadFile(testdata.Path("x509/server_ca_cert.pem"))
			if err != nil {
				t.Fatal(err)
			}
			cp := x509.NewCertPool()
			if !cp.AppendCertsFromPEM(b) {
				t.Fatalf("failed to append certificates")
			}
			cert, err := tls.LoadX509KeyPair(testdata.Path("x509/client1_cert.pem"), testdata.Path("x509/client1_key.pem"))
			if err != nil {
				t.Fatal(err)
			}
			creds = credentials.NewTLS(&tls.Config{
				ServerName:   "x.test.example.com",
				RootCAs:      cp,
				Certificates: []tls.Certificate{cert},
			})
		} else {
			creds, err = credentials.NewClientTLSFromFile(testdata.Path("x509/server_ca_cert.pem"), "x.test.example.com")
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	return creds
}

func (s) TestClientSideXDSSPIFFE(t *testing.T) {
	managementServer, nodeID, _, xdsResolver := setup.ManagementServerAndResolver(t)

	serverCreds := testutils.CreateServerTLSCredentialsCompatibleWithSPIFFE(t, tls.RequireAndVerifyClientCert)
	server := stubserver.StartTestService(t, nil, grpc.Creds(serverCreds))
	defer server.Stop()

	const serviceName = "my-service-client-side-xds"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
		SecLevel:   e2e.SecurityLevelMTLS,
	})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}
	opts := xds.ClientOptions{FallbackCreds: makeFallbackClientCreds(t, true, true)}
	creds, err := xds.NewClientCredentials(opts)
	if err != nil {
		t.Fatalf("NewClientCredentials(%v) failed: %v", opts, err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(creds), grpc.WithResolvers(xdsResolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}
