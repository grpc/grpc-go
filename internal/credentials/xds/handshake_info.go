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

// Package xds contains non-user facing functionality of the xds credentials.
package xds

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/resolver"
)

func init() {
	internal.GetXDSHandshakeInfoForTesting = GetHandshakeInfo
}

// handshakeAttrKey is the type used as the key to store HandshakeInfo in
// the Attributes field of resolver.Address.
type handshakeAttrKey struct{}

// SetHandshakeInfo returns a copy of addr in which the Attributes field is
// updated with hInfo.
func SetHandshakeInfo(addr resolver.Address, hInfo *HandshakeInfo) resolver.Address {
	addr.Attributes = addr.Attributes.WithValues(handshakeAttrKey{}, hInfo)
	return addr
}

// GetHandshakeInfo returns a pointer to the HandshakeInfo stored in attr.
func GetHandshakeInfo(attr *attributes.Attributes) *HandshakeInfo {
	v := attr.Value(handshakeAttrKey{})
	hi, _ := v.(*HandshakeInfo)
	return hi
}

// HandshakeInfo wraps all the security configuration required by client and
// server handshake methods in xds credentials. The xDS implementation will be
// responsible for populating these fields.
//
// Safe for concurrent access.
type HandshakeInfo struct {
	mu                sync.Mutex
	rootProvider      certprovider.Provider
	identityProvider  certprovider.Provider
	acceptedSANs      map[string]bool // Only on the client side.
	requireClientCert bool            // Only on server side.
}

// SetRootCertProvider updates the root certificate provider.
func (hi *HandshakeInfo) SetRootCertProvider(root certprovider.Provider) {
	hi.mu.Lock()
	hi.rootProvider = root
	hi.mu.Unlock()
}

// SetIdentityCertProvider updates the identity certificate provider.
func (hi *HandshakeInfo) SetIdentityCertProvider(identity certprovider.Provider) {
	hi.mu.Lock()
	hi.identityProvider = identity
	hi.mu.Unlock()
}

// SetAcceptedSANs updates the list of accepted SANs.
func (hi *HandshakeInfo) SetAcceptedSANs(sans []string) {
	hi.mu.Lock()
	hi.acceptedSANs = make(map[string]bool, len(sans))
	for _, san := range sans {
		hi.acceptedSANs[san] = true
	}
	hi.mu.Unlock()
}

// SetRequireClientCert updates whether a client cert is required during the
// ServerHandshake(). A value of true indicates that we are performing mTLS.
func (hi *HandshakeInfo) SetRequireClientCert(require bool) {
	hi.mu.Lock()
	hi.requireClientCert = require
	hi.mu.Unlock()
}

// UseFallbackCreds returns true when fallback credentials are to be used based
// on the contents of the HandshakeInfo.
func (hi *HandshakeInfo) UseFallbackCreds() bool {
	if hi == nil {
		return true
	}

	hi.mu.Lock()
	defer hi.mu.Unlock()
	return hi.identityProvider == nil && hi.rootProvider == nil
}

// ClientSideTLSConfig constructs a tls.Config to be used in a client-side
// handshake based on the contents of the HandshakeInfo.
func (hi *HandshakeInfo) ClientSideTLSConfig(ctx context.Context) (*tls.Config, error) {
	hi.mu.Lock()
	// On the client side, rootProvider is mandatory. IdentityProvider is
	// optional based on whether the client is doing TLS or mTLS.
	if hi.rootProvider == nil {
		return nil, errors.New("xds: CertificateProvider to fetch trusted roots is missing, cannot perform TLS handshake. Please check configuration on the management server")
	}
	// Since the call to KeyMaterial() can block, we read the providers under
	// the lock but call the actual function after releasing the lock.
	rootProv, idProv := hi.rootProvider, hi.identityProvider
	hi.mu.Unlock()

	// InsecureSkipVerify needs to be set to true because we need to perform
	// custom verification to check the SAN on the received certificate.
	// Currently the Go stdlib does complete verification of the cert (which
	// includes hostname verification) or none. We are forced to go with the
	// latter and perform the normal cert validation ourselves.
	cfg := &tls.Config{InsecureSkipVerify: true}

	km, err := rootProv.KeyMaterial(ctx)
	if err != nil {
		return nil, fmt.Errorf("xds: fetching trusted roots from CertificateProvider failed: %v", err)
	}
	cfg.RootCAs = km.Roots

	if idProv != nil {
		km, err := idProv.KeyMaterial(ctx)
		if err != nil {
			return nil, fmt.Errorf("xds: fetching identity certificates from CertificateProvider failed: %v", err)
		}
		cfg.Certificates = km.Certs
	}
	return cfg, nil
}

// ServerSideTLSConfig constructs a tls.Config to be used in a server-side
// handshake based on the contents of the HandshakeInfo.
func (hi *HandshakeInfo) ServerSideTLSConfig(ctx context.Context) (*tls.Config, error) {
	cfg := &tls.Config{ClientAuth: tls.NoClientCert}
	hi.mu.Lock()
	// On the server side, identityProvider is mandatory. RootProvider is
	// optional based on whether the server is doing TLS or mTLS.
	if hi.identityProvider == nil {
		return nil, errors.New("xds: CertificateProvider to fetch identity certificate is missing, cannot perform TLS handshake. Please check configuration on the management server")
	}
	// Since the call to KeyMaterial() can block, we read the providers under
	// the lock but call the actual function after releasing the lock.
	rootProv, idProv := hi.rootProvider, hi.identityProvider
	if hi.requireClientCert {
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	hi.mu.Unlock()

	// identityProvider is mandatory on the server side.
	km, err := idProv.KeyMaterial(ctx)
	if err != nil {
		return nil, fmt.Errorf("xds: fetching identity certificates from CertificateProvider failed: %v", err)
	}
	cfg.Certificates = km.Certs

	if rootProv != nil {
		km, err := rootProv.KeyMaterial(ctx)
		if err != nil {
			return nil, fmt.Errorf("xds: fetching trusted roots from CertificateProvider failed: %v", err)
		}
		cfg.ClientCAs = km.Roots
	}
	return cfg, nil
}

// MatchingSANExists returns true if the SAN contained in the passed in
// certificate is present in the list of accepted SANs in the HandshakeInfo.
//
// If the list of accepted SANs in the HandshakeInfo is empty, this function
// returns true for all input certificates.
func (hi *HandshakeInfo) MatchingSANExists(cert *x509.Certificate) bool {
	if len(hi.acceptedSANs) == 0 {
		return true
	}

	var sans []string
	// SANs can be specified in any of these four fields on the parsed cert.
	sans = append(sans, cert.DNSNames...)
	sans = append(sans, cert.EmailAddresses...)
	for _, ip := range cert.IPAddresses {
		sans = append(sans, ip.String())
	}
	for _, uri := range cert.URIs {
		sans = append(sans, uri.String())
	}

	hi.mu.Lock()
	defer hi.mu.Unlock()
	for _, san := range sans {
		if hi.acceptedSANs[san] {
			return true
		}
	}
	return false
}

// NewHandshakeInfo returns a new instance of HandshakeInfo with the given root
// and identity certificate providers.
func NewHandshakeInfo(root, identity certprovider.Provider, sans ...string) *HandshakeInfo {
	acceptedSANs := make(map[string]bool, len(sans))
	for _, san := range sans {
		acceptedSANs[san] = true
	}
	return &HandshakeInfo{
		rootProvider:     root,
		identityProvider: identity,
		acceptedSANs:     acceptedSANs,
	}
}
