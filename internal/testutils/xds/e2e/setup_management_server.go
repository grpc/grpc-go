/*
 *
 * Copyright 2022 gRPC authors.
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

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/testdata"
)

// SetupManagementServer performs the following:
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
func SetupManagementServer(t *testing.T, opts ManagementServerOptions) (*ManagementServer, string, []byte, resolver.Builder, func()) {
	t.Helper()

	// Spin up an xDS management server on a local port.
	server, err := StartManagementServer(opts)
	if err != nil {
		t.Fatalf("Failed to spin up the xDS management server: %v", err)
	}
	defer func() {
		if err != nil {
			server.Stop()
		}
	}()

	nodeID := uuid.New().String()
	bootstrapContents, err := DefaultBootstrapContents(nodeID, fmt.Sprintf("passthrough:///%s", server.Address))
	if err != nil {
		server.Stop()
		t.Fatal(err)
	}
	var rb resolver.Builder
	if newResolver := internal.NewXDSResolverWithConfigForTesting; newResolver != nil {
		rb, err = newResolver.(func([]byte) (resolver.Builder, error))(bootstrapContents)
		if err != nil {
			server.Stop()
			t.Fatalf("Failed to create xDS resolver for testing: %v", err)
		}
	}

	return server, nodeID, bootstrapContents, rb, func() { server.Stop() }
}

// DefaultBootstrapContents creates a default bootstrap configuration with the
// given node ID and server URI. It also creates certificate provider
// configuration and sets the listener resource name template to be used on the
// server side.
func DefaultBootstrapContents(nodeID, serverURI string) ([]byte, error) {
	// Create a directory to hold certs and key files used on the server side.
	serverDir, err := createTmpDirWithFiles("testServerSideXDS*", "x509/server1_cert.pem", "x509/server1_key.pem", "x509/client_ca_cert.pem")
	if err != nil {
		return nil, fmt.Errorf("failed to create bootstrap configuration: %v", err)
	}

	// Create a directory to hold certs and key files used on the client side.
	clientDir, err := createTmpDirWithFiles("testClientSideXDS*", "x509/client1_cert.pem", "x509/client1_key.pem", "x509/server_ca_cert.pem")
	if err != nil {
		return nil, fmt.Errorf("failed to create bootstrap configuration: %v", err)
	}

	// Create certificate providers section of the bootstrap config with entries
	// for both the client and server sides.
	cpc := map[string]json.RawMessage{
		ServerSideCertProviderInstance: DefaultFileWatcherConfig(path.Join(serverDir, certFile), path.Join(serverDir, keyFile), path.Join(serverDir, rootFile)),
		ClientSideCertProviderInstance: DefaultFileWatcherConfig(path.Join(clientDir, certFile), path.Join(clientDir, keyFile), path.Join(clientDir, rootFile)),
	}

	// Create the bootstrap configuration.
	bs, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: []json.RawMessage{[]byte(fmt.Sprintf(`{
			"server_uri": %q,
			"channel_creds": [{"type": "insecure"}]
		}`, serverURI))},
		NodeID:                             nodeID,
		CertificateProviders:               cpc,
		ServerListenerResourceNameTemplate: ServerListenerResourceNameTemplate,
		Authorities: map[string]json.RawMessage{
			// Most tests that use new style xdstp resource names do not specify
			// an authority. These end up looking up an entry with the empty key
			// in the authorities map. Having an entry with an empty key and
			// empty configuration, results in these resources also using the
			// top-level configuration, which is what we want mostly for our
			// tests, unless explicitly specified by tests that use multiple
			// authorities etc.
			"": []byte(`{}`),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create bootstrap configuration: %v", err)
	}
	return bs, nil
}

const (
	// Names of files inside tempdir, for certprovider plugin to watch.
	certFile = "cert.pem"
	keyFile  = "key.pem"
	rootFile = "ca.pem"
)

func createTmpFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("os.ReadFile(%q) failed: %v", src, err)
	}
	if err := os.WriteFile(dst, data, os.ModePerm); err != nil {
		return fmt.Errorf("os.WriteFile(%q) failed: %v", dst, err)
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
	dir, err := os.MkdirTemp("", dirSuffix)
	if err != nil {
		return "", fmt.Errorf("os.MkdirTemp() failed: %v", err)
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
