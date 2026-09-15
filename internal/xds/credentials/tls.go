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

package credentials

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"time"

	tlscredspb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/tls/v3"
	"github.com/spiffe/go-spiffe/v2/bundle/spiffebundle"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal/credentials/spiffe"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

)

const tlsCredsTypeURL = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.tls.v3.TlsCredentials"

func init() {
	RegisterChannelCredsBuilder(tlsCredsTypeURL, buildTLSCredentials)
}

// buildTLSCredentials builds TLS channel credentials from a TlsCredentials
// plugin config, whose root and identity certificates are sourced from
// certificate provider instances configured in the bootstrap config. Unknown
// instance names and provider build failures are errors, resulting in the
// resource being NACKed, the same way CommonTlsContext instances are handled
// (gRFC A29). The returned cleanup closes the certificate providers.
func buildTLSCredentials(config *anypb.Any, resolver CertProviderConfigResolver) (credentials.Bundle, func(), error) {
	var tlsCfg tlscredspb.TlsCredentials
	if err := anypb.UnmarshalTo(config, &tlsCfg, proto.UnmarshalOptions{}); err != nil {
		return nil, nil, fmt.Errorf("credentials: failed to unmarshal TlsCredentials: %v", err)
	}
	if resolver == nil {
		return nil, nil, fmt.Errorf("credentials: no bootstrap configuration available to resolve certificate provider instances")
	}

	rootInstanceName := tlsCfg.GetRootCertificateProvider().GetInstanceName()
	if rootInstanceName == "" {
		return nil, nil, fmt.Errorf("credentials: tls credentials must specify root_certificate_provider with an instance_name")
	}
	rootCfg, err := certProviderConfig(resolver, rootInstanceName, "root")
	if err != nil {
		return nil, nil, err
	}
	rootProvider, err := rootCfg.Build(certprovider.BuildOptions{
		CertName: tlsCfg.GetRootCertificateProvider().GetCertificateName(),
		WantRoot: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("credentials: failed to build root certificate provider: %v", err)
	}

	b := &tlsBundle{rootProvider: rootProvider}
	// The identity certificate provider is optional, and is configured only
	// for mTLS. When the field is set, it must name a provider instance.
	if identity := tlsCfg.GetIdentityCertificateProvider(); identity != nil {
		identityInstanceName := identity.GetInstanceName()
		if identityInstanceName == "" {
			rootProvider.Close()
			return nil, nil, fmt.Errorf("credentials: tls credentials identity_certificate_provider must specify an instance_name")
		}
		identityCfg, err := certProviderConfig(resolver, identityInstanceName, "identity")
		if err != nil {
			rootProvider.Close()
			return nil, nil, err
		}
		identityProvider, err := identityCfg.Build(certprovider.BuildOptions{
			CertName:     identity.GetCertificateName(),
			WantIdentity: true,
		})
		if err != nil {
			rootProvider.Close()
			return nil, nil, fmt.Errorf("credentials: failed to build identity certificate provider: %v", err)
		}
		b.identityProvider = identityProvider
	}
	return b, b.close, nil
}

// certProviderConfig looks up the certificate provider instance with the
// given name via the resolver. kind names the certificate the provider is
// used for (root or identity), for error messages.
func certProviderConfig(resolver CertProviderConfigResolver, instanceName, kind string) (*certprovider.BuildableConfig, error) {
	cfg, ok := resolver.CertProviderConfigs()[instanceName]
	if !ok {
		return nil, fmt.Errorf("credentials: tls credentials %s certificate provider: instance name %q missing in bootstrap configuration", kind, instanceName)
	}
	return cfg, nil
}

// tlsBundle is a credentials.Bundle providing client-side TLS transport
// credentials whose server root CA certificates, and optionally client
// identity certificates, come from certificate provider instances. The key
// material is fetched from the providers on every handshake, so certificate
// reloads are picked up. It carries no per-RPC credentials.
type tlsBundle struct {
	rootProvider     certprovider.Provider
	identityProvider certprovider.Provider // nil when no identity certificate is configured
}

func (b *tlsBundle) TransportCredentials() credentials.TransportCredentials {
	return b
}

func (b *tlsBundle) PerRPCCredentials() credentials.PerRPCCredentials {
	return nil
}

func (b *tlsBundle) NewWithMode(string) (credentials.Bundle, error) {
	return nil, fmt.Errorf("credentials: xDS TLS channel credentials only support one mode")
}

// close closes the certificate providers. Subsequent handshakes fail.
func (b *tlsBundle) close() {
	b.rootProvider.Close()
	if b.identityProvider != nil {
		b.identityProvider.Close()
	}
}

func (b *tlsBundle) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	rootKM, err := b.rootProvider.KeyMaterial(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("credentials: failed to get root certificates: %v", err)
	}

	cfg := &tls.Config{}
	if rootKM.SPIFFEBundleMap != nil {
		// The SPIFFE trust bundle map is authoritative for peer verification.
		cfg.InsecureSkipVerify = true //nolint:gosec // verification is performed by VerifyPeerCertificate below.
		cfg.VerifyPeerCertificate = buildSPIFFEVerifyFunc(rootKM.SPIFFEBundleMap)
	} else {
		if rootKM.Roots == nil {
			return nil, nil, errors.New("credentials: root certificate provider returned no root certificates")
		}
		cfg.RootCAs = rootKM.Roots
	}
	if b.identityProvider != nil {
		identityKM, err := b.identityProvider.KeyMaterial(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("credentials: failed to get identity certificates: %v", err)
		}
		cfg.Certificates = identityKM.Certs
	}
	return credentials.NewTLS(cfg).ClientHandshake(ctx, authority, rawConn)
}


// buildSPIFFEVerifyFunc returns a certificate verifier for the supplied SPIFFE
// trust bundle map. The verifier mirrors the SPIFFE trust-map validation used by
// the bootstrap TLS credentials, while allowing certificate providers to reload
// the map on each handshake.
func buildSPIFFEVerifyFunc(spiffeBundleMap map[string]*spiffebundle.Bundle) func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		rawCertList := make([]*x509.Certificate, len(rawCerts))
		for i, asn1Data := range rawCerts {
			cert, err := x509.ParseCertificate(asn1Data)
			if err != nil {
				return fmt.Errorf("spiffe: verify function could not parse input certificate: %v", err)
			}
			rawCertList[i] = cert
		}
		if len(rawCertList) == 0 {
			return fmt.Errorf("spiffe: verify function has no valid input certificates")
		}

		leafCert := rawCertList[0]
		roots, err := spiffe.GetRootsFromSPIFFEBundleMap(spiffeBundleMap, leafCert)
		if err != nil {
			return err
		}
		opts := x509.VerifyOptions{
			Roots:         roots,
			CurrentTime:   time.Now(),
			Intermediates: x509.NewCertPool(),
		}
		for _, cert := range rawCertList[1:] {
			opts.Intermediates.AddCert(cert)
		}
		if _, err = leafCert.Verify(opts); err != nil {
			return fmt.Errorf("spiffe: x509 certificate Verify failed: %v", err)
		}
		return nil
	}
}

func (b *tlsBundle) ServerHandshake(net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return nil, nil, errors.New("credentials: server handshake is not supported by xDS TLS channel credentials")
}

func (b *tlsBundle) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "tls"}
}

func (b *tlsBundle) Clone() credentials.TransportCredentials {
	return &tlsBundle{rootProvider: b.rootProvider, identityProvider: b.identityProvider}
}

func (b *tlsBundle) OverrideServerName(string) error {
	return errors.New("credentials: overriding server name is not supported by xDS TLS channel credentials")
}
