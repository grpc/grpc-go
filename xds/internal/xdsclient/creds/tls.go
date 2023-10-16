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

// Package creds implements gRFC A65: mTLS Credentials in xDS Bootstrap File.
package creds

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	_ "google.golang.org/grpc/credentials/tls/certprovider/pemfile" // for file_watcher provider
)

type tlsBundle struct {
	jd                   json.RawMessage
	transportCredentials credentials.TransportCredentials
}

// NewTLS returns a credentials.Bundle which delegates certificate loading to
// a file_watcher provider for mTLS transport security. See gRFC A65.
func NewTLS(jd json.RawMessage) (credentials.Bundle, error) {
	cfg := &struct {
		CertificateFile   string `json:"certificate_file"`
		CACertificateFile string `json:"ca_certificate_file"`
		PrivateKeyFile    string `json:"private_key_file"`
	}{}

	tlsConfig := tls.Config{}
	if err := json.Unmarshal(jd, cfg); err != nil {
		return nil, err
	}

	// We cannot simply always use a file_watcher provider because it behaves
	// slightly differently from the xDS TLS config. Quoting A65:
	//
	// > The only difference between the file-watcher certificate provider
	// > config and this one is that in the file-watcher certificate provider,
	// > at least one of the "certificate_file" or "ca_certificate_file" fields
	// > must be specified, whereas in this configuration, it is acceptable to
	// > specify neither one.
	//
	// We only use a file_watcher provider if either one of them or both are
	// specified.
	if cfg.CACertificateFile != "" || cfg.CertificateFile != "" || cfg.PrivateKeyFile != "" {
		// file_watcher currently ignores BuildOptions, but we set them for good
		// measure.
		opts := certprovider.BuildOptions{}
		if cfg.CACertificateFile != "" {
			opts.WantRoot = true
		}
		if cfg.CertificateFile != "" {
			opts.WantIdentity = true
		}
		provider, err := certprovider.GetProvider("file_watcher", jd, opts)
		if err != nil {
			// GetProvider fails if jd is invalid, e.g. if only one of private
			// key and certificate is specified.
			return nil, fmt.Errorf("failed to get TLS provider: %w", err)
		}
		if cfg.CertificateFile != "" {
			tlsConfig.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
				// Client cert reloading for mTLS.
				km, err := provider.KeyMaterial(context.Background())
				if err != nil {
					return nil, err
				}
				if len(km.Certs) != 1 {
					return nil, fmt.Errorf("there should be exactly exactly one certificate")
				}
				return &km.Certs[0], nil
			}
			if cfg.CACertificateFile == "" {
				// no need for a callback to load the CA. Use the normal mTLS
				// transport credentials.
				return &tlsBundle{
					jd:                   jd,
					transportCredentials: credentials.NewTLS(&tlsConfig),
				}, nil
			}
		}
		return &tlsBundle{
			jd: jd,
			transportCredentials: &caReloadingClientTLSCreds{
				baseConfig: &tlsConfig,
				provider:   provider,
			},
		}, nil
	}

	// None of certificate_file and ca_certificate_file are set.
	// Use the system-wide root certs.
	return &tlsBundle{
		jd:                   jd,
		transportCredentials: credentials.NewTLS(&tlsConfig),
	}, nil
}

func (t *tlsBundle) TransportCredentials() credentials.TransportCredentials {
	return t.transportCredentials
}

func (t *tlsBundle) PerRPCCredentials() credentials.PerRPCCredentials {
	// No per-RPC credentials in A65.
	return nil
}

func (t *tlsBundle) NewWithMode(_ string) (credentials.Bundle, error) {
	return NewTLS(t.jd)
}

// caReloadingClientTLSCreds is a client mTLS credentials.TransportCredentials
// that attempts to reload the server root certificate from its provider on
// every client handshake. This is needed because Go's tls.Config does not
// support reloading the root CAs.
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
	if !km.Roots.Equal(c.baseConfig.RootCAs) {
		// provider returned a different root CA. Update it.
		c.baseConfig.RootCAs = km.Roots
	}
	c.mu.Unlock()
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
	panic("server handshake for xds tls credentials, which are client only")
}
