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

// Package bootstrap provides functionality to generate bootstrap configuration.
package bootstrap

import (
	"encoding/json"
	"fmt"
	"os"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/envconfig"
)

var logger = grpclog.Component("internal/xds")

// Options wraps the parameters used to generate bootstrap configuration.
type Options struct {
	// NodeID is the node identifier of the gRPC client/server node in the
	// proxyless service mesh.
	NodeID string
	// ServerURI is the address of the management server.
	ServerURI string
	// IgnoreResourceDeletion, if true, results in a bootstrap config with the
	// `server_features` list containing `ignore_resource_deletion`. This results
	// in gRPC ignoring resource deletions from the management server, as per A53.
	IgnoreResourceDeletion bool
	// ClientDefaultListenerResourceNameTemplate is the default listener
	// resource name template to be used on the gRPC client.
	ClientDefaultListenerResourceNameTemplate string
	// ServerListenerResourceNameTemplate is the listener resource name template
	// to be used on the gRPC server.
	ServerListenerResourceNameTemplate string
	// CertificateProviders is the certificate providers configuration.
	CertificateProviders map[string]json.RawMessage
	// Authorities is a list of non-default authorities.
	//
	// In the config, an authority contains {ServerURI, xds-version, creds,
	// features, etc}. Note that this fields only has ServerURI (it's a
	// map[authority-name]ServerURI). The other fields (version, creds,
	// features) are assumed to be the same as the default authority (they can
	// be added later if needed).
	//
	// If the env var corresponding to federation (envconfig.XDSFederation) is
	// set, an entry with empty string as the key and empty server config as
	// value will be added. This will be used by new style resource names with
	// an empty authority.
	Authorities map[string]string
}

// CreateFile creates a temporary file with bootstrap contents, based on the
// passed in options, and updates the bootstrap environment variable to point to
// this file.
//
// Returns a cleanup function which will be non-nil if the setup process was
// completed successfully. It is the responsibility of the caller to invoke the
// cleanup function at the end of the test.
func CreateFile(opts Options) (func(), error) {
	bootstrapContents, err := Contents(opts)
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", "test_xds_bootstrap_*")
	if err != nil {
		return nil, fmt.Errorf("failed to created bootstrap file: %v", err)
	}

	if err := os.WriteFile(f.Name(), bootstrapContents, 0644); err != nil {
		return nil, fmt.Errorf("failed to created bootstrap file: %v", err)
	}
	logger.Infof("Created bootstrap file at %q with contents: %s\n", f.Name(), bootstrapContents)

	origBootstrapFileName := envconfig.XDSBootstrapFileName
	envconfig.XDSBootstrapFileName = f.Name()
	return func() {
		os.Remove(f.Name())
		envconfig.XDSBootstrapFileName = origBootstrapFileName
	}, nil
}

// Contents returns the contents to go into a bootstrap file, environment, or
// configuration passed to xds.NewXDSResolverWithConfigForTesting.
func Contents(opts Options) ([]byte, error) {
	cfg := &bootstrapConfig{
		XdsServers: []server{
			{
				ServerURI:    opts.ServerURI,
				ChannelCreds: []creds{{Type: "insecure"}},
			},
		},
		Node: node{
			ID: opts.NodeID,
		},
		CertificateProviders:                      opts.CertificateProviders,
		ClientDefaultListenerResourceNameTemplate: opts.ClientDefaultListenerResourceNameTemplate,
		ServerListenerResourceNameTemplate:        opts.ServerListenerResourceNameTemplate,
	}
	cfg.XdsServers[0].ServerFeatures = append(cfg.XdsServers[0].ServerFeatures, "xds_v3")
	if opts.IgnoreResourceDeletion {
		cfg.XdsServers[0].ServerFeatures = append(cfg.XdsServers[0].ServerFeatures, "ignore_resource_deletion")
	}

	// This will end up using the top-level server list for new style
	// resources with empty authority.
	auths := map[string]authority{"": {}}
	for n, auURI := range opts.Authorities {
		auths[n] = authority{XdsServers: []server{{
			ServerURI:      auURI,
			ChannelCreds:   []creds{{Type: "insecure"}},
			ServerFeatures: cfg.XdsServers[0].ServerFeatures,
		}}}
	}
	cfg.Authorities = auths

	bootstrapContents, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to created bootstrap file: %v", err)
	}
	return bootstrapContents, nil
}

type bootstrapConfig struct {
	XdsServers                                []server                   `json:"xds_servers,omitempty"`
	Node                                      node                       `json:"node,omitempty"`
	CertificateProviders                      map[string]json.RawMessage `json:"certificate_providers,omitempty"`
	ClientDefaultListenerResourceNameTemplate string                     `json:"client_default_listener_resource_name_template,omitempty"`
	ServerListenerResourceNameTemplate        string                     `json:"server_listener_resource_name_template,omitempty"`
	Authorities                               map[string]authority       `json:"authorities,omitempty"`
}

type authority struct {
	XdsServers []server `json:"xds_servers,omitempty"`
}

type server struct {
	ServerURI      string   `json:"server_uri,omitempty"`
	ChannelCreds   []creds  `json:"channel_creds,omitempty"`
	ServerFeatures []string `json:"server_features,omitempty"`
}

type creds struct {
	Type   string `json:"type,omitempty"`
	Config any    `json:"config,omitempty"`
}

type node struct {
	ID string `json:"id,omitempty"`
}
