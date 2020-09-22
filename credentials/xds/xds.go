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

// Package xds provides a transport credentials implementation where the
// security configuration is pushed by a management server using xDS APIs.
//
// All APIs in this package are EXPERIMENTAL.
package xds

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	credinternal "google.golang.org/grpc/internal/credentials"
)

// ClientOptions contains parameters to configure a new client-side xDS
// credentials implementation.
type ClientOptions struct {
	// FallbackCreds specifies the fallback credentials to be used when either
	// the `xds` scheme is not used in the user's dial target or when the xDS
	// server does not return any security configuration. Attempts to create
	// client credentials without a fallback credentials will fail.
	FallbackCreds credentials.TransportCredentials
}

// NewClientCredentials returns a new client-side transport credentials
// implementation which uses xDS APIs to fetch its security configuration.
func NewClientCredentials(opts ClientOptions) (credentials.TransportCredentials, error) {
	if opts.FallbackCreds == nil {
		return nil, errors.New("missing fallback credentials")
	}
	return &credsImpl{
		isClient: true,
		fallback: opts.FallbackCreds,
	}, nil
}

// credsImpl is an implementation of the credentials.TransportCredentials
// interface which uses xDS APIs to fetch its security configuration.
type credsImpl struct {
	isClient bool
	fallback credentials.TransportCredentials
}

// handshakeCtxKey is the context key used to store HandshakeInfo values.
type handshakeCtxKey struct{}

// HandshakeInfo wraps all the security configuration required by client and
// server handshake methods in credsImpl. The xDS implementation will be
// responsible for populating these fields.
//
// Safe for concurrent access.
type HandshakeInfo struct {
	mu               sync.Mutex
	rootProvider     certprovider.Provider
	identityProvider certprovider.Provider
	acceptedSANs     map[string]bool // Only on the client side.
}

// SetRootCertProvider updates the root certificate provider.
func (chi *HandshakeInfo) SetRootCertProvider(root certprovider.Provider) {
	chi.mu.Lock()
	chi.rootProvider = root
	chi.mu.Unlock()
}

// SetIdentityCertProvider updates the identity certificate provider.
func (chi *HandshakeInfo) SetIdentityCertProvider(identity certprovider.Provider) {
	chi.mu.Lock()
	chi.identityProvider = identity
	chi.mu.Unlock()
}

// SetAcceptedSANs updates the list of accepted SANs.
func (chi *HandshakeInfo) SetAcceptedSANs(sans []string) {
	chi.mu.Lock()
	chi.acceptedSANs = make(map[string]bool)
	for _, san := range sans {
		chi.acceptedSANs[san] = true
	}
	chi.mu.Unlock()
}

func (chi *HandshakeInfo) validate(isClient bool) error {
	chi.mu.Lock()
	defer chi.mu.Unlock()

	// On the client side, rootProvider is mandatory. IdentityProvider is
	// optional based on whether the client is doing TLS or mTLS.
	if isClient && chi.rootProvider == nil {
		return errors.New("root certificate provider is missing")
	}

	// On the server side, identityProvider is mandatory. RootProvider is
	// optional based on whether the server is doing TLS or mTLS.
	if !isClient && chi.identityProvider == nil {
		return errors.New("identity certificate provider is missing")
	}

	return nil
}

func (chi *HandshakeInfo) matchingSANExists(cert *x509.Certificate) bool {
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

	chi.mu.Lock()
	defer chi.mu.Unlock()
	for _, san := range sans {
		if chi.acceptedSANs[san] {
			return true
		}
	}
	return false
}

// NewHandshakeInfo returns a new instance of HandshakeInfo with the given root
// and identity certificate providers.
func NewHandshakeInfo(root, identity certprovider.Provider, sans ...string) *HandshakeInfo {
	acceptedSANs := make(map[string]bool)
	for _, san := range sans {
		acceptedSANs[san] = true
	}
	return &HandshakeInfo{
		rootProvider:     root,
		identityProvider: identity,
		acceptedSANs:     acceptedSANs,
	}
}

// NewContextWithHandshakeInfo returns a copy of the parent context with the
// provided HandshakeInfo stored as a value.
func NewContextWithHandshakeInfo(parent context.Context, info *HandshakeInfo) context.Context {
	return context.WithValue(parent, handshakeCtxKey{}, info)
}

// handshakeInfoFromCtx returns a pointer to the HandshakeInfo stored in ctx.
func handshakeInfoFromCtx(ctx context.Context) *HandshakeInfo {
	val, ok := ctx.Value(handshakeCtxKey{}).(*HandshakeInfo)
	if !ok {
		return nil
	}
	return val
}

// ClientHandshake performs the TLS handshake on the client-side.
//
// It looks for the presence of a HandshakeInfo value in the passed in context
// (added using a call to NewContextWithHandshakeInfo()), and retrieves identity
// and root certificates from there. It also retrieves a list of acceptable SANs
// and uses a custom verification function to validate the certificate presented
// by the peer. It uses fallback credentials if no HandshakeInfo is present in
// the passed in context.
func (c *credsImpl) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if !c.isClient {
		return nil, nil, errors.New("ClientHandshake() is not supported for server credentials")
	}

	chi := handshakeInfoFromCtx(ctx)
	if chi == nil {
		// A missing handshake info in the provided context could mean either
		// the user did not specify an `xds` scheme in their dial target or that
		// the xDS server did not provide any security configuration. In both of
		// these cases, we use the fallback credentials specified by the user.
		return c.fallback.ClientHandshake(ctx, authority, rawConn)
	}
	if err := chi.validate(c.isClient); err != nil {
		return nil, nil, err
	}

	// We build the tls.Config with the following values
	// 1. Root certificate as returned by the root provider.
	// 2. Identity certificate as returned by the identity provider. This may be
	//    empty on the client side, if the client is not doing mTLS.
	// 3. InsecureSkipVerify to true. Certificates used in Mesh environments
	//    usually contains the identity of the workload presenting the
	//    certificate as a SAN (instead of a hostname in the CommonName field).
	//    This means that normal certificate verification as done by the
	//    standard library will fail.
	// 4. Key usage to match whether client/server usage.
	// 5. A `VerifyPeerCertificate` function which performs normal peer
	// 	  cert verification using configured roots, and the custom SAN checks.
	var certs []tls.Certificate
	var roots *x509.CertPool
	err := func() error {
		// We use this anonymous function trick to be able to defer the unlock.
		chi.mu.Lock()
		defer chi.mu.Unlock()

		if chi.rootProvider != nil {
			km, err := chi.rootProvider.KeyMaterial(ctx)
			if err != nil {
				return fmt.Errorf("fetching root certificates failed: %v", err)
			}
			roots = km.Roots
		}
		if chi.identityProvider != nil {
			km, err := chi.identityProvider.KeyMaterial(ctx)
			if err != nil {
				return fmt.Errorf("fetching identity certificates failed: %v", err)
			}
			certs = km.Certs
		}
		return nil
	}()
	if err != nil {
		return nil, nil, err
	}

	cfg := &tls.Config{
		Certificates:       certs,
		InsecureSkipVerify: true,
		RootCAs:            roots,
	}
	cfg.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		// Parse all raw certificates presented by the peer.
		var certs []*x509.Certificate
		for _, rc := range rawCerts {
			cert, err := x509.ParseCertificate(rc)
			if err != nil {
				return err
			}
			certs = append(certs, cert)
		}

		// Build the intermediates list and verify that the leaf certificate
		// is signed by one of the root certificates.
		intermediates := x509.NewCertPool()
		for _, cert := range certs[1:] {
			intermediates.AddCert(cert)
		}
		opts := x509.VerifyOptions{
			Roots:         roots,
			Intermediates: intermediates,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		if _, err := certs[0].Verify(opts); err != nil {
			return err
		}
		// The SANs sent by the MeshCA are encoded as SPIFFE IDs. We need to
		// only look at the SANs on the leaf cert.
		if !chi.matchingSANExists(certs[0]) {
			return fmt.Errorf("SANs received in leaf certificate %+v does not match any of the accepted SANs", certs[0])
		}
		return nil
	}

	// Perform the TLS handshake with the tls.Config that we have. We run the
	// actual Handshake() function in a goroutine because we need to respect the
	// deadline specified on the passed in context, and we need a way to cancel
	// the handshake if the context is cancelled.
	var conn *tls.Conn
	if c.isClient {
		conn = tls.Client(rawConn, cfg)
	} else {
		conn = tls.Server(rawConn, cfg)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- conn.Handshake()
		close(errCh)
	}()
	select {
	case err := <-errCh:
		if err != nil {
			conn.Close()
			return nil, nil, err
		}
	case <-ctx.Done():
		conn.Close()
		return nil, nil, ctx.Err()
	}
	info := credentials.TLSInfo{
		State: conn.ConnectionState(),
		CommonAuthInfo: credentials.CommonAuthInfo{
			SecurityLevel: credentials.PrivacyAndIntegrity,
		},
		SPIFFEID: credinternal.SPIFFEIDFromState(conn.ConnectionState()),
	}
	return credinternal.WrapSyscallConn(rawConn, conn), info, nil
}

// ServerHandshake performs the TLS handshake on the server-side.
func (c *credsImpl) ServerHandshake(net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if c.isClient {
		return nil, nil, errors.New("ServerHandshake is not supported for client credentials")
	}
	// TODO(easwars): Implement along with server side xDS implementation.
	return nil, nil, errors.New("not implemented")
}

// Info provides the ProtocolInfo of this TransportCredentials.
func (c *credsImpl) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "tls"}
}

// Clone makes a copy of this TransportCredentials.
func (c *credsImpl) Clone() credentials.TransportCredentials {
	clone := *c
	return &clone
}

func (c *credsImpl) OverrideServerName(_ string) error {
	return errors.New("serverName for peer validation must be configured as a list of acceptable SANs")
}
