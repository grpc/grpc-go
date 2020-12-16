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

package e2e

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"google.golang.org/grpc/xds/internal/env"
)

// TransportAPI refers to the API version for xDS transport protocol.
type TransportAPI int

const (
	// TransportV2 refers to the v2 xDS transport protocol.
	TransportV2 TransportAPI = iota
	// TransportV3 refers to the v3 xDS transport protocol.
	TransportV3
)

// BootstrapOptions wraps the parameters passed to SetupBootstrapFile.
type BootstrapOptions struct {
	// Version is the xDS transport protocol version.
	Version TransportAPI
	// NodeID is the node identifier of the gRPC client/server node in the
	// proxyless service mesh.
	NodeID string
	// ServerURI is the address of the management server.
	ServerURI string
	// ServerResourceNameID is the Listener resource name to fetch.
	ServerResourceNameID string
	// CertificateProviders is the certificate providers configuration.
	CertificateProviders map[string]json.RawMessage
}

// SetupBootstrapFile creates a temporary file with bootstrap contents, based on
// the passed in options, and updates the bootstrap environment variable to
// point to this file.
//
// Returns a cleanup function which will be non-nil if the setup process was
// completed successfully. It is the responsibility of the caller to invoke the
// cleanup function at the end of the test.
func SetupBootstrapFile(opts BootstrapOptions) (func(), error) {
	f, err := ioutil.TempFile("", "test_xds_bootstrap_*")
	if err != nil {
		return nil, fmt.Errorf("failed to created bootstrap file: %v", err)
	}

	cfg := &bootstrapConfig{
		XdsServers: []server{
			{
				ServerURI: opts.ServerURI,
				ChannelCreds: []creds{
					{
						Type: "insecure",
					},
				},
			},
		},
		Node: node{
			ID: opts.NodeID,
		},
		CertificateProviders:     opts.CertificateProviders,
		GRPCServerResourceNameID: opts.ServerResourceNameID,
	}
	switch opts.Version {
	case TransportV2:
		// TODO: Add any v2 specific fields.
	case TransportV3:
		cfg.XdsServers[0].ServerFeatures = append(cfg.XdsServers[0].ServerFeatures, "xds_v3")
	default:
		return nil, fmt.Errorf("unsupported xDS transport protocol version: %v", opts.Version)
	}

	bootstrapContents, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to created bootstrap file: %v", err)
	}
	if err := ioutil.WriteFile(f.Name(), bootstrapContents, 0644); err != nil {
		return nil, fmt.Errorf("failed to created bootstrap file: %v", err)
	}
	logger.Infof("Created bootstrap file at %q with contents: %s\n", f.Name(), bootstrapContents)

	origBootstrapFileName := env.BootstrapFileName
	env.BootstrapFileName = f.Name()
	return func() {
		os.Remove(f.Name())
		env.BootstrapFileName = origBootstrapFileName
	}, nil
}

// DefaultFileWatcherConfig is a helper function to create a default certificate
// provider plugin configuration. The test is expected to have setup the files
// appropriately before this configuration is used to instantiate providers.
func DefaultFileWatcherConfig(certPath, keyPath, caPath string) map[string]json.RawMessage {
	cfg := fmt.Sprintf(`{
			"plugin_name": "file_watcher",
			"config": {
				"certificate_file": %q,
				"private_key_file": %q,
				"ca_certificate_file": %q,
				"refresh_interval": "600s"
			}
		}`, certPath, keyPath, caPath)
	return map[string]json.RawMessage{
		"google_cloud_private_spiffe": json.RawMessage(cfg),
	}
}

type bootstrapConfig struct {
	XdsServers               []server                   `json:"xds_servers,omitempty"`
	Node                     node                       `json:"node,omitempty"`
	CertificateProviders     map[string]json.RawMessage `json:"certificate_providers,omitempty"`
	GRPCServerResourceNameID string                     `json:"grpc_server_resource_name_id,omitempty"`
}

type server struct {
	ServerURI      string   `json:"server_uri,omitempty"`
	ChannelCreds   []creds  `json:"channel_creds,omitempty"`
	ServerFeatures []string `json:"server_features,omitempty"`
}

type creds struct {
	Type   string      `json:"type,omitempty"`
	Config interface{} `json:"config,omitempty"`
}

type node struct {
	ID string `json:"id,omitempty"`
}
