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
	"path"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/testutils/xds/bootstrap"
	"google.golang.org/grpc/resolver"
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
		ServerSideCertProviderInstance: DefaultFileWatcherConfig(path.Join(serverDir, certFile), path.Join(serverDir, keyFile), path.Join(serverDir, rootFile)),
		ClientSideCertProviderInstance: DefaultFileWatcherConfig(path.Join(clientDir, certFile), path.Join(clientDir, keyFile), path.Join(clientDir, rootFile)),
	}

	// Create a bootstrap file in a temporary directory.
	nodeID := uuid.New().String()
	bootstrapContents, err := bootstrap.Contents(bootstrap.Options{
		Version:                            bootstrap.TransportV3,
		NodeID:                             nodeID,
		ServerURI:                          server.Address,
		CertificateProviders:               cpc,
		ServerListenerResourceNameTemplate: ServerListenerResourceNameTemplate,
	})
	if err != nil {
		server.Stop()
		t.Fatalf("Failed to create bootstrap file: %v", err)
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
