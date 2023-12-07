/*
 *
 * Copyright 2023 gRPC authors.
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

// Package tlscreds implements mTLS Credentials in xDS Bootstrap File.
// See [gRFC A65](github.com/grpc/proposal/blob/master/A65-xds-mtls-creds-in-bootstrap.md).
package tlscreds

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/credentials/tls/certprovider/pemfile"
)

// bundle is an implementation of credentials.Bundle which implements mTLS
// Credentials in xDS Bootstrap File.
// See [gRFC A65](github.com/grpc/proposal/blob/master/A65-xds-mtls-creds-in-bootstrap.md).
type bundle struct {
	jd                   json.RawMessage
	transportCredentials credentials.TransportCredentials
}

// NewBundle returns a credentials.Bundle which implements mTLS Credentials in xDS
// Bootstrap File. It delegates certificate loading to a file_watcher provider
// if either client certificates or server root CA is specified.
// See [gRFC A65](github.com/grpc/proposal/blob/master/A65-xds-mtls-creds-in-bootstrap.md).
func NewBundle(jd json.RawMessage) (credentials.Bundle, error) {
	cfg := &struct {
		CertificateFile   string `json:"certificate_file"`
		CACertificateFile string `json:"ca_certificate_file"`
		PrivateKeyFile    string `json:"private_key_file"`
	}{}

	if err := json.Unmarshal(jd, cfg); err != nil {
		return nil, err
	}

	if cfg.CACertificateFile == "" && cfg.CertificateFile == "" && cfg.PrivateKeyFile == "" {
		// We do not always use a file_watcher provider because it behaves
		// slightly differently from the xDS TLS config provider. Quoting A65:
		//
		// > The only difference between the file-watcher certificate provider
		// > config and this one is that in the file-watcher certificate provider,
		// > at least one of the "certificate_file" or "ca_certificate_file" fields
		// > must be specified, whereas in this configuration, it is acceptable to
		// > specify neither one.
		//
		// Here, none of certificate_file and ca_certificate_file are set.
		// Use the system-wide root certs. No need to use the file_watcher
		// provider.
		return &bundle{
			jd:                   jd,
			transportCredentials: credentials.NewTLS(&tls.Config{}),
		}, nil
	}

	tlsConfig := tls.Config{}
	// The pemfile plugin (file_watcher) currently ignores BuildOptions.
	provider, err := certprovider.GetProvider(pemfile.PluginName, jd, certprovider.BuildOptions{})
	if err != nil {
		// GetProvider fails if jd is invalid, e.g. if only one of private
		// key and certificate is specified.
		return nil, err
	}
	if cfg.CertificateFile != "" {
		tlsConfig.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			// Client cert reloading for mTLS.
			km, err := provider.KeyMaterial(context.Background())
			if err != nil {
				return nil, err
			}
			if len(km.Certs) != 1 {
				// xDS bootstrap has a single private key file, so there
				// must be exactly one certificate or certificate chain
				// matching this private key.
				return nil, fmt.Errorf("certificate_file must contains exactly one certificate or certificate chain")
			}
			return &km.Certs[0], nil
		}
		if cfg.CACertificateFile == "" {
			// No need for a callback to load the CA. Use the normal mTLS
			// transport credentials.
			return &bundle{
				jd:                   jd,
				transportCredentials: credentials.NewTLS(&tlsConfig),
			}, nil
		}
	}
	return &bundle{
		jd: jd,
		transportCredentials: &caReloadingClientTLSCreds{
			baseConfig: &tlsConfig,
			provider:   provider,
		},
	}, nil
}

func (t *bundle) TransportCredentials() credentials.TransportCredentials {
	return t.transportCredentials
}

func (t *bundle) PerRPCCredentials() credentials.PerRPCCredentials {
	// mTLS provides transport credentials only. There are no per-RPC
	// credentials.
	return nil
}

func (t *bundle) NewWithMode(string) (credentials.Bundle, error) {
	// This bundle has a single mode which only uses TLS transport credentials,
	// so there is no legitimate case where callers would call NewWithMode.
	return nil, fmt.Errorf("xDS TLS credentials only support one mode")
}

// caReloadingClientTLSCreds is credentials.TransportCredentials for client
// side mTLS that attempts to reload the server root certificate from its
// provider on every client handshake. This is needed because Go's tls.Config
// does not support reloading the root CAs.
type caReloadingClientTLSCreds struct {
	mu         sync.Mutex
	provider   certprovider.Provider
	baseConfig *tls.Config
}

func (c *caReloadingClientTLSCreds) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	km, err := c.provider.KeyMaterial(ctx)
	if err != nil {
		return nil, nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !km.Roots.Equal(c.baseConfig.RootCAs) {
		// Provider returned a different root CA. Update it.
		c.baseConfig.RootCAs = km.Roots
	}
	return credentials.NewTLS(c.baseConfig).ClientHandshake(ctx, authority, rawConn)
}

func (c *caReloadingClientTLSCreds) Info() credentials.ProtocolInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return credentials.NewTLS(c.baseConfig).Info()
}

func (c *caReloadingClientTLSCreds) Clone() credentials.TransportCredentials {
	c.mu.Lock()
	defer c.mu.Unlock()
	return &caReloadingClientTLSCreds{
		provider:   c.provider,
		baseConfig: c.baseConfig.Clone(),
	}
}

func (c *caReloadingClientTLSCreds) OverrideServerName(_ string) error {
	panic("cannot override server name for private xds tls credentials")
}

func (c *caReloadingClientTLSCreds) ServerHandshake(_ net.Conn) (net.Conn, credentials.AuthInfo, error) {
	panic("cannot perform handshake for server. xDS TLS credentials are client only.")
}
