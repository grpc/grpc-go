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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/leakcheck"
	"google.golang.org/grpc/internal/xds/env"
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

var (
	// Globals corresponding to the single instance of the xDS management server
	// which is spawned for all the tests in this package.
	managementServer   *e2e.ManagementServer
	xdsClientNodeID    string
	bootstrapContents  []byte
	xdsResolverBuilder resolver.Builder
)

// TestMain sets up an xDS management server, runs all tests, and stops the
// management server.
func TestMain(m *testing.M) {
	// The management server is started and stopped from here, but the leakcheck
	// runs after every individual test. So, we need to skip the goroutine which
	// spawns the management server and is blocked on the call to `Serve()`.
	leakcheck.RegisterIgnoreGoroutine("e2e.StartManagementServer")

	cancel, err := setupManagementServer()
	if err != nil {
		log.Printf("setupManagementServer() failed: %v", err)
		os.Exit(1)
	}

	code := m.Run()
	cancel()
	os.Exit(code)
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
// - sets up the global variables which refer to this management server and the
//   nodeID to be used when talking to this management server.
//
// Returns a function to be invoked by the caller to stop the management
// server.
func setupManagementServer() (cleanup func(), err error) {
	// Turn on the env var protection for client-side security.
	origClientSideSecurityEnvVar := env.ClientSideSecuritySupport
	env.ClientSideSecuritySupport = true

	// Spin up an xDS management server on a local port.
	managementServer, err = e2e.StartManagementServer()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			managementServer.Stop()
		}
	}()

	// Create a directory to hold certs and key files used on the server side.
	serverDir, err := createTmpDirWithFiles("testServerSideXDS*", "x509/server1_cert.pem", "x509/server1_key.pem", "x509/client_ca_cert.pem")
	if err != nil {
		return nil, err
	}

	// Create a directory to hold certs and key files used on the client side.
	clientDir, err := createTmpDirWithFiles("testClientSideXDS*", "x509/client1_cert.pem", "x509/client1_key.pem", "x509/server_ca_cert.pem")
	if err != nil {
		return nil, err
	}

	// Create certificate providers section of the bootstrap config with entries
	// for both the client and server sides.
	cpc := map[string]json.RawMessage{
		e2e.ServerSideCertProviderInstance: e2e.DefaultFileWatcherConfig(path.Join(serverDir, certFile), path.Join(serverDir, keyFile), path.Join(serverDir, rootFile)),
		e2e.ClientSideCertProviderInstance: e2e.DefaultFileWatcherConfig(path.Join(clientDir, certFile), path.Join(clientDir, keyFile), path.Join(clientDir, rootFile)),
	}

	// Create a bootstrap file in a temporary directory.
	xdsClientNodeID = uuid.New().String()
	bootstrapContents, err = xdsinternal.BootstrapContents(xdsinternal.BootstrapOptions{
		Version:                            xdsinternal.TransportV3,
		NodeID:                             xdsClientNodeID,
		ServerURI:                          managementServer.Address,
		CertificateProviders:               cpc,
		ServerListenerResourceNameTemplate: e2e.ServerListenerResourceNameTemplate,
	})
	if err != nil {
		return nil, err
	}
	xdsResolverBuilder, err = xds.NewXDSResolverWithConfigForTesting(bootstrapContents)
	if err != nil {
		return nil, err
	}

	return func() {
		managementServer.Stop()
		env.ClientSideSecuritySupport = origClientSideSecurityEnvVar
	}, nil
}
