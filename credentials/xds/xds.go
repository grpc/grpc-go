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
// Experimental
//
// Notice: All APIs in this package are EXPERIMENTAL and may be removed in a
// later release.
package xds

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal"
	credinternal "google.golang.org/grpc/internal/credentials"
	"google.golang.org/grpc/resolver"
)

func init() {
	internal.GetXDSHandshakeInfoForTesting = getHandshakeInfo
}

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

// handshakeAttrKey is the type used as the key to store HandshakeInfo in
// the Attributes field of resolver.Address.
type handshakeAttrKey struct{}

// SetHandshakeInfo returns a copy of addr in which the Attributes field is
// updated with hInfo.
func SetHandshakeInfo(addr resolver.Address, hInfo *HandshakeInfo) resolver.Address {
	addr.Attributes = addr.Attributes.WithValues(handshakeAttrKey{}, hInfo)
	return addr
}

// getHandshakeInfo returns a pointer to the HandshakeInfo stored in attr.
func getHandshakeInfo(attr *attributes.Attributes) *HandshakeInfo {
	v := attr.Value(handshakeAttrKey{})
	hi, _ := v.(*HandshakeInfo)
	return hi
}

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

func (hi *HandshakeInfo) validate(isClient bool) error {
	hi.mu.Lock()
	defer hi.mu.Unlock()

	// On the client side, rootProvider is mandatory. IdentityProvider is
	// optional based on whether the client is doing TLS or mTLS.
	if isClient && hi.rootProvider == nil {
		return errors.New("xds: CertificateProvider to fetch trusted roots is missing, cannot perform TLS handshake. Please check configuration on the management server")
	}

	// On the server side, identityProvider is mandatory. RootProvider is
	// optional based on whether the server is doing TLS or mTLS.
	if !isClient && hi.identityProvider == nil {
		return errors.New("xds: CertificateProvider to fetch identity certificate is missing, cannot perform TLS handshake. Please check configuration on the management server")
	}

	return nil
}

func (hi *HandshakeInfo) makeTLSConfig(ctx context.Context) (*tls.Config, error) {
	hi.mu.Lock()
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
	if rootProv != nil {
		km, err := rootProv.KeyMaterial(ctx)
		if err != nil {
			return nil, fmt.Errorf("xds: fetching trusted roots from CertificateProvider failed: %v", err)
		}
		cfg.RootCAs = km.Roots
	}
	if idProv != nil {
		km, err := idProv.KeyMaterial(ctx)
		if err != nil {
			return nil, fmt.Errorf("xds: fetching identity certificates from CertificateProvider failed: %v", err)
		}
		cfg.Certificates = km.Certs
	}
	return cfg, nil
}

func (hi *HandshakeInfo) matchingSANExists(cert *x509.Certificate) bool {
	if len(hi.acceptedSANs) == 0 {
		// An empty list of acceptedSANs means "accept everything".
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

	// The CDS balancer constructs a new HandshakeInfo using a call to
	// NewHandshakeInfo(), and then adds it to the attributes field of the
	// resolver.Address when handling calls to NewSubConn(). The transport layer
	// takes care of shipping these attributes in the context to this handshake
	// function. We first read the credentials.ClientHandshakeInfo type from the
	// context, which contains the attributes added by the CDS balancer. We then
	// read the HandshakeInfo from the attributes to get to the actual data that
	// we need here for the handshake.
	chi := credentials.ClientHandshakeInfoFromContext(ctx)
	// If there are no attributes in the received context or the attributes does
	// not contain a HandshakeInfo, it could either mean that the user did not
	// specify an `xds` scheme in their dial target or that the xDS server did
	// not provide any security configuration. In both of these cases, we use
	// the fallback credentials specified by the user.
	if chi.Attributes == nil {
		return c.fallback.ClientHandshake(ctx, authority, rawConn)
	}
	hi := getHandshakeInfo(chi.Attributes)
	if hi.UseFallbackCreds() {
		return c.fallback.ClientHandshake(ctx, authority, rawConn)
	}
	if err := hi.validate(c.isClient); err != nil {
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
	cfg, err := hi.makeTLSConfig(ctx)
	if err != nil {
		return nil, nil, err
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
			Roots:         cfg.RootCAs,
			Intermediates: intermediates,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		if _, err := certs[0].Verify(opts); err != nil {
			return err
		}
		// The SANs sent by the MeshCA are encoded as SPIFFE IDs. We need to
		// only look at the SANs on the leaf cert.
		if !hi.matchingSANExists(certs[0]) {
			return fmt.Errorf("SANs received in leaf certificate %+v does not match any of the accepted SANs", certs[0])
		}
		return nil
	}

	// Perform the TLS handshake with the tls.Config that we have. We run the
	// actual Handshake() function in a goroutine because we need to respect the
	// deadline specified on the passed in context, and we need a way to cancel
	// the handshake if the context is cancelled.
	conn := tls.Client(rawConn, cfg)
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

// UsesXDS returns true if c uses xDS to fetch security configuration
// used at handshake time, and false otherwise.
func (c *credsImpl) UsesXDS() bool {
	return true
}
