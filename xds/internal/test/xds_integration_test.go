//go:build !386
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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	"github.com/google/uuid"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/testdata"
	"google.golang.org/grpc/xds"
	"google.golang.org/grpc/xds/internal/testutils/e2e"

	xdsinternal "google.golang.org/grpc/internal/xds"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

const (
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 100 * time.Millisecond
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type testService struct {
	testpb.TestServiceServer
}

func (*testService) EmptyCall(context.Context, *testpb.Empty) (*testpb.Empty, error) {
	return &testpb.Empty{}, nil
}

func (*testService) UnaryCall(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	return &testpb.SimpleResponse{}, nil
}

func createTmpFile(src, dst string) error {
	data, err := ioutil.ReadFile(src)
	if err != nil {
		return fmt.Errorf("ioutil.ReadFile(%q) failed: %v", src, err)
	}
	if err := ioutil.WriteFile(dst, data, os.ModePerm); err != nil {
		return fmt.Errorf("ioutil.WriteFile(%q) failed: %v", dst, err)
	}
	return nil
}

// createTempDirWithFiles creates a temporary directory under the system default
// tempDir with the given dirSuffix. It also reads from certSrc, keySrc and
// rootSrc files are creates appropriate files under the newly create tempDir.
// Returns the name of the created tempDir.
func createTmpDirWithFiles(dirSuffix, certSrc, keySrc, rootSrc string) (string, error) {
	// Create a temp directory. Passing an empty string for the first argument
	// uses the system temp directory.
	dir, err := ioutil.TempDir("", dirSuffix)
	if err != nil {
		return "", fmt.Errorf("ioutil.TempDir() failed: %v", err)
	}

	if err := createTmpFile(testdata.Path(certSrc), path.Join(dir, certFile)); err != nil {
		return "", err
	}
	if err := createTmpFile(testdata.Path(keySrc), path.Join(dir, keyFile)); err != nil {
		return "", err
	}
	if err := createTmpFile(testdata.Path(rootSrc), path.Join(dir, rootFile)); err != nil {
		return "", err
	}
	return dir, nil
}

// createClientTLSCredentials creates client-side TLS transport credentials.
func createClientTLSCredentials(t *testing.T) credentials.TransportCredentials {
	t.Helper()

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

// setupManagement server performs the following:
// - spin up an xDS management server on a local port
// - set up certificates for consumption by the file_watcher plugin
// - creates a bootstrap file in a temporary location
// - creates an xDS resolver using the above bootstrap contents
//
// Returns the following:
// - management server
// - nodeID to be used by the client when connecting to the management server
// - bootstrap contents to be used by the client
// - xDS resolver builder to be used by the client
// - a cleanup function to be invoked at the end of the test
func setupManagementServer(t *testing.T) (*e2e.ManagementServer, string, []byte, resolver.Builder, func()) {
	t.Helper()

	// Spin up an xDS management server on a local port.
	server, err := e2e.StartManagementServer()
	if err != nil {
		t.Fatalf("Failed to spin up the xDS management server: %v", err)
	}
	defer func() {
		if err != nil {
			server.Stop()
		}
	}()

	// Create a directory to hold certs and key files used on the server side.
	serverDir, err := createTmpDirWithFiles("testServerSideXDS*", "x509/server1_cert.pem", "x509/server1_key.pem", "x509/client_ca_cert.pem")
	if err != nil {
		server.Stop()
		t.Fatal(err)
	}

	// Create a directory to hold certs and key files used on the client side.
	clientDir, err := createTmpDirWithFiles("testClientSideXDS*", "x509/client1_cert.pem", "x509/client1_key.pem", "x509/server_ca_cert.pem")
	if err != nil {
		server.Stop()
		t.Fatal(err)
	}

	// Create certificate providers section of the bootstrap config with entries
	// for both the client and server sides.
	cpc := map[string]json.RawMessage{
		e2e.ServerSideCertProviderInstance: e2e.DefaultFileWatcherConfig(path.Join(serverDir, certFile), path.Join(serverDir, keyFile), path.Join(serverDir, rootFile)),
		e2e.ClientSideCertProviderInstance: e2e.DefaultFileWatcherConfig(path.Join(clientDir, certFile), path.Join(clientDir, keyFile), path.Join(clientDir, rootFile)),
	}

	// Create a bootstrap file in a temporary directory.
	nodeID := uuid.New().String()
	bootstrapContents, err := xdsinternal.BootstrapContents(xdsinternal.BootstrapOptions{
		Version:                            xdsinternal.TransportV3,
		NodeID:                             nodeID,
		ServerURI:                          server.Address,
		CertificateProviders:               cpc,
		ServerListenerResourceNameTemplate: e2e.ServerListenerResourceNameTemplate,
	})
	if err != nil {
		server.Stop()
		t.Fatalf("Failed to create bootstrap file: %v", err)
	}
	resolver, err := xds.NewXDSResolverWithConfigForTesting(bootstrapContents)
	if err != nil {
		server.Stop()
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	return server, nodeID, bootstrapContents, resolver, func() { server.Stop() }
}
