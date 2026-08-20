/*
 *
 * Copyright 2026 gRPC authors.
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

package credsregistry

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	tlscredspb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/tls/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
)

const tlsCredsTypeURL = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.tls.v3.TlsCredentials"

func init() {
	RegisterChannelCredsBuilder(tlsCredsTypeURL, tlsCredsBuilder{})
}

// tlsCredsBuilder builds TLS channel credentials from a TlsCredentials plugin
// config, whose root and identity certificates are sourced from certificate
// provider instances configured in the bootstrap config.
type tlsCredsBuilder struct{}

func (tlsCredsBuilder) Build(config *anypb.Any, bc *bootstrap.Config) (credentials.Bundle, func(), error) {
	var tlsCfg tlscredspb.TlsCredentials
	if err := anypb.UnmarshalTo(config, &tlsCfg, proto.UnmarshalOptions{}); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal TlsCredentials: %v", err)
	}

	// The certificate provider instance names are validated against the
	// bootstrap config here, at parse time, the same way CommonTlsContext
	// instances are (gRFC A29): an unknown instance name is a NACK. The
	// providers themselves are instantiated lazily, on the first handshake,
	// so that a parsed-but-never-dialed config does not start certificate
	// watchers.
	root := tlsCfg.GetRootCertificateProvider()
	if root.GetInstanceName() == "" {
		return nil, nil, fmt.Errorf("tls credentials must specify root_certificate_provider with an instance_name")
	}
	rootCfg, err := certProviderConfig(bc, root)
	if err != nil {
		return nil, nil, fmt.Errorf("tls credentials root certificate provider: %v", err)
	}
	creds := &providerTLSCreds{
		rootConfig:   rootCfg,
		rootCertName: root.GetCertificateName(),
	}
	if identity := tlsCfg.GetIdentityCertificateProvider(); identity != nil {
		if identity.GetInstanceName() == "" {
			return nil, nil, fmt.Errorf("tls credentials identity_certificate_provider must specify an instance_name")
		}
		identityCfg, err := certProviderConfig(bc, identity)
		if err != nil {
			return nil, nil, fmt.Errorf("tls credentials identity certificate provider: %v", err)
		}
		creds.identityConfig = identityCfg
		creds.identityCertName = identity.GetCertificateName()
	}
	return &tlsBundle{creds: creds}, creds.close, nil
}

// certProviderConfig looks up the certificate provider instance referenced by
// the given proto in the bootstrap config.
func certProviderConfig(bc *bootstrap.Config, instance *v3tlspb.CommonTlsContext_CertificateProviderInstance) (*certprovider.BuildableConfig, error) {
	if bc == nil {
		return nil, fmt.Errorf("no bootstrap configuration available to resolve certificate provider instances")
	}
	cfg, ok := bc.CertProviderConfigs()[instance.GetInstanceName()]
	if !ok {
		return nil, fmt.Errorf("certificate provider instance name %q missing in bootstrap configuration", instance.GetInstanceName())
	}
	return cfg, nil
}

// tlsBundle is a credentials.Bundle wrapping provider-backed TLS transport
// credentials. It carries no per-RPC credentials.
type tlsBundle struct {
	creds *providerTLSCreds
}

func (b *tlsBundle) TransportCredentials() credentials.TransportCredentials {
	return b.creds
}

func (b *tlsBundle) PerRPCCredentials() credentials.PerRPCCredentials {
	return nil
}

func (b *tlsBundle) NewWithMode(string) (credentials.Bundle, error) {
	return nil, fmt.Errorf("xDS TLS channel credentials only support one mode")
}

// providerTLSCreds is a client-side credentials.TransportCredentials that
// sources the server root CA certificates, and optionally the client identity
// certificates, from certificate provider instances. The key material is
// fetched from the providers on every handshake, so certificate reloads are
// picked up; the providers themselves are instantiated on the first
// handshake.
type providerTLSCreds struct {
	rootConfig       *certprovider.BuildableConfig
	rootCertName     string
	identityConfig   *certprovider.BuildableConfig // nil when no identity certificate is configured
	identityCertName string

	mu               sync.Mutex
	closed           bool
	rootProvider     certprovider.Provider
	identityProvider certprovider.Provider
}

// providers instantiates the certificate providers on first use and returns
// them. It fails if the credentials have already been closed.
func (c *providerTLSCreds) providers() (root, identity certprovider.Provider, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, nil, errors.New("xDS TLS channel credentials have been closed")
	}
	if c.rootProvider == nil {
		p, err := c.rootConfig.Build(certprovider.BuildOptions{CertName: c.rootCertName, WantRoot: true})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build root certificate provider: %v", err)
		}
		c.rootProvider = p
	}
	if c.identityConfig != nil && c.identityProvider == nil {
		p, err := c.identityConfig.Build(certprovider.BuildOptions{CertName: c.identityCertName, WantIdentity: true})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build identity certificate provider: %v", err)
		}
		c.identityProvider = p
	}
	return c.rootProvider, c.identityProvider, nil
}

// close releases the certificate providers, if they were instantiated.
// Subsequent handshakes fail.
func (c *providerTLSCreds) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.rootProvider != nil {
		c.rootProvider.Close()
		c.rootProvider = nil
	}
	if c.identityProvider != nil {
		c.identityProvider.Close()
		c.identityProvider = nil
	}
}

func (c *providerTLSCreds) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	root, identity, err := c.providers()
	if err != nil {
		return nil, nil, err
	}
	rootKM, err := root.KeyMaterial(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get root certificates: %v", err)
	}
	if rootKM.Roots == nil {
		return nil, nil, errors.New("root certificate provider returned no root certificates")
	}
	cfg := &tls.Config{RootCAs: rootKM.Roots}
	if identity != nil {
		identityKM, err := identity.KeyMaterial(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get identity certificates: %v", err)
		}
		cfg.Certificates = identityKM.Certs
	}
	return credentials.NewTLS(cfg).ClientHandshake(ctx, authority, rawConn)
}

func (c *providerTLSCreds) ServerHandshake(net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return nil, nil, errors.New("server handshake is not supported by xDS TLS channel credentials")
}

func (c *providerTLSCreds) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "tls"}
}

func (c *providerTLSCreds) Clone() credentials.TransportCredentials {
	return &providerTLSCreds{
		rootConfig:       c.rootConfig,
		rootCertName:     c.rootCertName,
		identityConfig:   c.identityConfig,
		identityCertName: c.identityCertName,
	}
}

func (c *providerTLSCreds) OverrideServerName(string) error {
	return errors.New("overriding server name is not supported by xDS TLS channel credentials")
}
