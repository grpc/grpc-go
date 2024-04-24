/*
 *
 * Copyright 2019 gRPC authors.
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

// Package advancedtls is a utility library containing functions to construct
// credentials.TransportCredentials that can perform credential reloading and
// custom verification check.
package advancedtls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"reflect"
	"time"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/tls/certprovider"
	credinternal "google.golang.org/grpc/internal/credentials"
)

// HandshakeVerificationInfo contains information about a handshake needed for
// verification for use when implementing the `PostHandshakeVerificationFunc`
// The fields in this struct are read-only.
type HandshakeVerificationInfo struct {
	// The target server name that the client connects to when establishing the
	// connection. This field is only meaningful for client side. On server side,
	// this field would be an empty string.
	ServerName string
	// The raw certificates sent from peer.
	RawCerts [][]byte
	// The verification chain obtained by checking peer RawCerts against the
	// trust certificate bundle(s), if applicable.
	VerifiedChains [][]*x509.Certificate
	// The leaf certificate sent from peer, if choosing to verify the peer
	// certificate(s) and that verification passed. This field would be nil if
	// either user chose not to verify or the verification failed.
	Leaf *x509.Certificate
}

// VerificationFuncParams contains parameters available to users when
// implementing CustomVerificationFunc.
// The fields in this struct are read-only.
//
// Deprecated: use HandshakeVerificationInfo instead.
type VerificationFuncParams = HandshakeVerificationInfo

// PostHandshakeVerificationResults contains the information about results of
// PostHandshakeVerificationFunc.
// PostHandshakeVerificationResults is an empty struct for now. It may be extended in the
// future to include more information.
type PostHandshakeVerificationResults struct{}

// VerificationResults contains the information about results of
// CustomVerificationFunc.
//
// Deprecated: use PostHandshakeVerificationResults instead.
type VerificationResults = PostHandshakeVerificationResults

// PostHandshakeVerificationFunc is the function defined by users to perform
// custom verification checks after chain building and regular handshake
// verification has been completed.
// PostHandshakeVerificationFunc should return (nil, error) if the authorization
// should fail, with the error containing information on why it failed.
type PostHandshakeVerificationFunc func(params *HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error)

// CustomVerificationFunc is the function defined by users to perform custom
// verification check.
// CustomVerificationFunc returns nil if the authorization fails; otherwise
// returns an empty struct.
//
// Deprecated: use PostHandshakeVerificationFunc instead.
type CustomVerificationFunc = PostHandshakeVerificationFunc

// GetRootCAsParams contains the parameters available to users when
// implementing GetRootCAs.
type GetRootCAsParams struct {
	RawConn  net.Conn
	RawCerts [][]byte
}

// GetRootCAsResults contains the results of GetRootCAs.
// If users want to reload the root trust certificate, it is required to return
// the proper TrustCerts in GetRootCAs.
type GetRootCAsResults struct {
	TrustCerts *x509.CertPool
}

// RootCertificateOptions contains options to obtain root trust certificates
// for both the client and the server.
// At most one option could be set. If none of them are set, we
// use the system default trust certificates.
type RootCertificateOptions struct {
	// If RootCACerts is set, it will be used every time when verifying
	// the peer certificates, without performing root certificate reloading.
	RootCACerts *x509.CertPool
	// If GetRootCertificates is set, it will be invoked to obtain root certs for
	// every new connection.
	GetRootCertificates func(params *GetRootCAsParams) (*GetRootCAsResults, error)
	// If RootProvider is set, we will use the root certs from the Provider's
	// KeyMaterial() call in the new connections. The Provider must have initial
	// credentials if specified. Otherwise, KeyMaterial() will block forever.
	RootProvider certprovider.Provider
}

// nonNilFieldCount returns the number of set fields in RootCertificateOptions.
func (o RootCertificateOptions) nonNilFieldCount() int {
	cnt := 0
	rv := reflect.ValueOf(o)
	for i := 0; i < rv.NumField(); i++ {
		if !rv.Field(i).IsNil() {
			cnt++
		}
	}
	return cnt
}

// IdentityCertificateOptions contains options to obtain identity certificates
// for both the client and the server.
// At most one option could be set.
type IdentityCertificateOptions struct {
	// If Certificates is set, it will be used every time when needed to present
	//identity certificates, without performing identity certificate reloading.
	Certificates []tls.Certificate
	// If GetIdentityCertificatesForClient is set, it will be invoked to obtain
	// identity certs for every new connection.
	// This field MUST be set on client side.
	GetIdentityCertificatesForClient func(*tls.CertificateRequestInfo) (*tls.Certificate, error)
	// If GetIdentityCertificatesForServer is set, it will be invoked to obtain
	// identity certs for every new connection.
	// This field MUST be set on server side.
	GetIdentityCertificatesForServer func(*tls.ClientHelloInfo) ([]*tls.Certificate, error)
	// If IdentityProvider is set, we will use the identity certs from the
	// Provider's KeyMaterial() call in the new connections. The Provider must
	// have initial credentials if specified. Otherwise, KeyMaterial() will block
	// forever.
	IdentityProvider certprovider.Provider
}

// nonNilFieldCount returns the number of set fields in IdentityCertificateOptions.
func (o IdentityCertificateOptions) nonNilFieldCount() int {
	cnt := 0
	rv := reflect.ValueOf(o)
	for i := 0; i < rv.NumField(); i++ {
		if !rv.Field(i).IsNil() {
			cnt++
		}
	}
	return cnt
}

// VerificationType is the enum type that represents different levels of
// verification users could set, both on client side and on server side.
type VerificationType int

const (
	// CertAndHostVerification indicates doing both certificate signature check
	// and hostname check.
	CertAndHostVerification VerificationType = iota
	// CertVerification indicates doing certificate signature check only. Setting
	// this field without proper custom verification check would leave the
	// application susceptible to the MITM attack.
	CertVerification
	// SkipVerification indicates skipping both certificate signature check and
	// hostname check. If setting this field, proper custom verification needs to
	// be implemented in order to complete the authentication. Setting this field
	// with a nil custom verification would raise an error.
	SkipVerification
)

// ClientOptions contains the fields needed to be filled by the client.
type ClientOptions struct {
	// IdentityOptions is OPTIONAL on client side. This field only needs to be
	// set if mutual authentication is required on server side.
	IdentityOptions IdentityCertificateOptions
	// AdditionalPeerVerification is a custom verification check after certificate signature
	// check.
	// If this is set, we will perform this customized check after doing the
	// normal check(s) indicated by setting VerificationType.
	AdditionalPeerVerification PostHandshakeVerificationFunc
	// VerifyPeer is a custom verification check after certificate signature
	// check.
	// If this is set, we will perform this customized check after doing the
	// normal check(s) indicated by setting VerificationType.
	//
	// Deprecated: use AdditionalPeerVerification instead.
	VerifyPeer PostHandshakeVerificationFunc
	// RootOptions is OPTIONAL on client side. If not set, we will try to use the
	// default trust certificates in users' OS system.
	RootOptions RootCertificateOptions
	// VerificationType defines what type of server verification is done. See
	// the `VerificationType` enum for the different options.
	// Default: CertAndHostVerification
	VerificationType VerificationType
	// VType is the verification type on the client side.
	//
	// Deprecated: use VerificationType instead.
	VType VerificationType
	// RevocationConfig is the configurations for certificate revocation checks.
	// It could be nil if such checks are not needed.
	RevocationConfig *RevocationConfig
	// MinVersion contains the minimum TLS version that is acceptable.
	// By default, TLS 1.2 is currently used as the minimum when acting as a
	// client, and TLS 1.0 when acting as a server. TLS 1.0 is the minimum
	// supported by this package, both as a client and as a server.
	MinVersion uint16
	// MaxVersion contains the maximum TLS version that is acceptable.
	// By default, the maximum version supported by this package is used,
	// which is currently TLS 1.3.
	MaxVersion uint16
	// serverNameOverride is for testing only. If set to a non-empty string, it
	// will override the virtual host name of authority (e.g. :authority header
	// field) in requests and the target hostname used during server cert
	// verification.
	serverNameOverride string
}

// ServerOptions contains the fields needed to be filled by the server.
type ServerOptions struct {
	// IdentityOptions is REQUIRED on server side.
	IdentityOptions IdentityCertificateOptions
	// AdditionalPeerVerification is a custom verification check after certificate signature
	// check.
	// If this is set, we will perform this customized check after doing the
	// normal check(s) indicated by setting VerificationType.
	AdditionalPeerVerification PostHandshakeVerificationFunc
	// VerifyPeer is a custom verification check after certificate signature
	// check.
	// If this is set, we will perform this customized check after doing the
	// normal check(s) indicated by setting VerificationType.
	//
	// Deprecated: use AdditionalPeerVerification instead.
	VerifyPeer PostHandshakeVerificationFunc
	// RootOptions is OPTIONAL on server side. This field only needs to be set if
	// mutual authentication is required(RequireClientCert is true).
	RootOptions RootCertificateOptions
	// If the server want the client to send certificates.
	RequireClientCert bool
	// VerificationType defines what type of client verification is done. See
	// the `VerificationType` enum for the different options.
	// Default: CertAndHostVerification
	VerificationType VerificationType
	// VType is the verification type on the server side.
	//
	// Deprecated: use VerificationType instead.
	VType VerificationType
	// RevocationConfig is the configurations for certificate revocation checks.
	// It could be nil if such checks are not needed.
	RevocationConfig *RevocationConfig
	// MinVersion contains the minimum TLS version that is acceptable.
	// By default, TLS 1.2 is currently used as the minimum when acting as a
	// client, and TLS 1.0 when acting as a server. TLS 1.0 is the minimum
	// supported by this package, both as a client and as a server.
	MinVersion uint16
	// MaxVersion contains the maximum TLS version that is acceptable.
	// By default, the maximum version supported by this package is used,
	// which is currently TLS 1.3.
	MaxVersion uint16
}

func (o *ClientOptions) config() (*tls.Config, error) {
	// TODO(gtcooke94) Remove this block when o.VerifyPeer is remoed.
	// VerifyPeer is deprecated, but do this to aid the transitory migration time.
	if o.AdditionalPeerVerification == nil {
		o.AdditionalPeerVerification = o.VerifyPeer
	}
	// TODO(gtcooke94). VType is deprecated, eventually remove this block. This
	// will ensure that users still explicitly setting `VType` will get the
	// setting to the right place.
	if o.VType != CertAndHostVerification {
		o.VerificationType = o.VType
	}
	if o.VerificationType == SkipVerification && o.AdditionalPeerVerification == nil {
		return nil, fmt.Errorf("client needs to provide custom verification mechanism if choose to skip default verification")
	}
	// Make sure users didn't specify more than one fields in
	// RootCertificateOptions and IdentityCertificateOptions.
	if num := o.RootOptions.nonNilFieldCount(); num > 1 {
		return nil, fmt.Errorf("at most one field in RootCertificateOptions could be specified")
	}
	if num := o.IdentityOptions.nonNilFieldCount(); num > 1 {
		return nil, fmt.Errorf("at most one field in IdentityCertificateOptions could be specified")
	}
	if o.IdentityOptions.GetIdentityCertificatesForServer != nil {
		return nil, fmt.Errorf("GetIdentityCertificatesForServer cannot be specified on the client side")
	}
	if o.MinVersion > o.MaxVersion {
		return nil, fmt.Errorf("the minimum TLS version is larger than the maximum TLS version")
	}
	config := &tls.Config{
		ServerName: o.serverNameOverride,
		// We have to set InsecureSkipVerify to true to skip the default checks and
		// use the verification function we built from buildVerifyFunc.
		InsecureSkipVerify: true,
		MinVersion:         o.MinVersion,
		MaxVersion:         o.MaxVersion,
	}
	// Propagate root-certificate-related fields in tls.Config.
	switch {
	case o.RootOptions.RootCACerts != nil:
		config.RootCAs = o.RootOptions.RootCACerts
	case o.RootOptions.GetRootCertificates != nil:
		// In cases when users provide GetRootCertificates callback, since this
		// callback is not contained in tls.Config, we have nothing to set here.
		// We will invoke the callback in ClientHandshake.
	case o.RootOptions.RootProvider != nil:
		o.RootOptions.GetRootCertificates = func(*GetRootCAsParams) (*GetRootCAsResults, error) {
			km, err := o.RootOptions.RootProvider.KeyMaterial(context.Background())
			if err != nil {
				return nil, err
			}
			return &GetRootCAsResults{TrustCerts: km.Roots}, nil
		}
	default:
		// No root certificate options specified by user. Use the certificates
		// stored in system default path as the last resort.
		if o.VerificationType != SkipVerification {
			systemRootCAs, err := x509.SystemCertPool()
			if err != nil {
				return nil, err
			}
			config.RootCAs = systemRootCAs
		}
	}
	// Propagate identity-certificate-related fields in tls.Config.
	switch {
	case o.IdentityOptions.Certificates != nil:
		config.Certificates = o.IdentityOptions.Certificates
	case o.IdentityOptions.GetIdentityCertificatesForClient != nil:
		config.GetClientCertificate = o.IdentityOptions.GetIdentityCertificatesForClient
	case o.IdentityOptions.IdentityProvider != nil:
		config.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			km, err := o.IdentityOptions.IdentityProvider.KeyMaterial(context.Background())
			if err != nil {
				return nil, err
			}
			if len(km.Certs) != 1 {
				return nil, fmt.Errorf("there should always be only one identity cert chain on the client side in IdentityProvider")
			}
			return &km.Certs[0], nil
		}
	default:
		// It's fine for users to not specify identity certificate options here.
	}
	return config, nil
}

func (o *ServerOptions) config() (*tls.Config, error) {
	// TODO(gtcooke94) Remove this block when o.VerifyPeer is remoed.
	// VerifyPeer is deprecated, but do this to aid the transitory migration time.
	if o.AdditionalPeerVerification == nil {
		o.AdditionalPeerVerification = o.VerifyPeer
	}
	// TODO(gtcooke94). VType is deprecated, eventually remove this block. This
	// will ensure that users still explicitly setting `VType` will get the
	// setting to the right place.
	if o.VType != CertAndHostVerification {
		o.VerificationType = o.VType
	}
	if o.RequireClientCert && o.VerificationType == SkipVerification && o.AdditionalPeerVerification == nil {
		return nil, fmt.Errorf("server needs to provide custom verification mechanism if choose to skip default verification, but require client certificate(s)")
	}
	// Make sure users didn't specify more than one fields in
	// RootCertificateOptions and IdentityCertificateOptions.
	if num := o.RootOptions.nonNilFieldCount(); num > 1 {
		return nil, fmt.Errorf("at most one field in RootCertificateOptions could be specified")
	}
	if num := o.IdentityOptions.nonNilFieldCount(); num > 1 {
		return nil, fmt.Errorf("at most one field in IdentityCertificateOptions could be specified")
	}
	if o.IdentityOptions.GetIdentityCertificatesForClient != nil {
		return nil, fmt.Errorf("GetIdentityCertificatesForClient cannot be specified on the server side")
	}
	if o.MinVersion > o.MaxVersion {
		return nil, fmt.Errorf("the minimum TLS version is larger than the maximum TLS version")
	}
	clientAuth := tls.NoClientCert
	if o.RequireClientCert {
		// We have to set clientAuth to RequireAnyClientCert to force underlying
		// TLS package to use the verification function we built from
		// buildVerifyFunc.
		clientAuth = tls.RequireAnyClientCert
	}
	config := &tls.Config{
		ClientAuth: clientAuth,
		MinVersion: o.MinVersion,
		MaxVersion: o.MaxVersion,
	}
	// Propagate root-certificate-related fields in tls.Config.
	switch {
	case o.RootOptions.RootCACerts != nil:
		config.ClientCAs = o.RootOptions.RootCACerts
	case o.RootOptions.GetRootCertificates != nil:
		// In cases when users provide GetRootCertificates callback, since this
		// callback is not contained in tls.Config, we have nothing to set here.
		// We will invoke the callback in ServerHandshake.
	case o.RootOptions.RootProvider != nil:
		o.RootOptions.GetRootCertificates = func(*GetRootCAsParams) (*GetRootCAsResults, error) {
			km, err := o.RootOptions.RootProvider.KeyMaterial(context.Background())
			if err != nil {
				return nil, err
			}
			return &GetRootCAsResults{TrustCerts: km.Roots}, nil
		}
	default:
		// No root certificate options specified by user. Use the certificates
		// stored in system default path as the last resort.
		if o.VerificationType != SkipVerification && o.RequireClientCert {
			systemRootCAs, err := x509.SystemCertPool()
			if err != nil {
				return nil, err
			}
			config.ClientCAs = systemRootCAs
		}
	}
	// Propagate identity-certificate-related fields in tls.Config.
	switch {
	case o.IdentityOptions.Certificates != nil:
		config.Certificates = o.IdentityOptions.Certificates
	case o.IdentityOptions.GetIdentityCertificatesForServer != nil:
		config.GetCertificate = func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return buildGetCertificates(clientHello, o)
		}
	case o.IdentityOptions.IdentityProvider != nil:
		o.IdentityOptions.GetIdentityCertificatesForServer = func(*tls.ClientHelloInfo) ([]*tls.Certificate, error) {
			km, err := o.IdentityOptions.IdentityProvider.KeyMaterial(context.Background())
			if err != nil {
				return nil, err
			}
			var certChains []*tls.Certificate
			for i := 0; i < len(km.Certs); i++ {
				certChains = append(certChains, &km.Certs[i])
			}
			return certChains, nil
		}
		config.GetCertificate = func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return buildGetCertificates(clientHello, o)
		}
	default:
		return nil, fmt.Errorf("needs to specify at least one field in IdentityCertificateOptions")
	}
	return config, nil
}

// advancedTLSCreds is the credentials required for authenticating a connection
// using TLS.
type advancedTLSCreds struct {
	config           *tls.Config
	verifyFunc       PostHandshakeVerificationFunc
	getRootCAs       func(params *GetRootCAsParams) (*GetRootCAsResults, error)
	isClient         bool
	verificationType VerificationType
	revocationConfig *RevocationConfig
}

func (c advancedTLSCreds) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{
		SecurityProtocol: "tls",
		SecurityVersion:  "1.2",
		ServerName:       c.config.ServerName,
	}
}

func (c *advancedTLSCreds) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	// Use local cfg to avoid clobbering ServerName if using multiple endpoints.
	cfg := credinternal.CloneTLSConfig(c.config)
	// We return the full authority name to users if ServerName is empty without
	// stripping the trailing port.
	if cfg.ServerName == "" {
		cfg.ServerName = authority
	}
	cfg.VerifyPeerCertificate = buildVerifyFunc(c, cfg.ServerName, rawConn)
	conn := tls.Client(rawConn, cfg)
	errChannel := make(chan error, 1)
	go func() {
		errChannel <- conn.Handshake()
		close(errChannel)
	}()
	select {
	case err := <-errChannel:
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
	}
	info.SPIFFEID = credinternal.SPIFFEIDFromState(conn.ConnectionState())
	return credinternal.WrapSyscallConn(rawConn, conn), info, nil
}

func (c *advancedTLSCreds) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	cfg := credinternal.CloneTLSConfig(c.config)
	cfg.VerifyPeerCertificate = buildVerifyFunc(c, "", rawConn)
	conn := tls.Server(rawConn, cfg)
	if err := conn.Handshake(); err != nil {
		conn.Close()
		return nil, nil, err
	}
	info := credentials.TLSInfo{
		State: conn.ConnectionState(),
		CommonAuthInfo: credentials.CommonAuthInfo{
			SecurityLevel: credentials.PrivacyAndIntegrity,
		},
	}
	info.SPIFFEID = credinternal.SPIFFEIDFromState(conn.ConnectionState())
	return credinternal.WrapSyscallConn(rawConn, conn), info, nil
}

func (c *advancedTLSCreds) Clone() credentials.TransportCredentials {
	return &advancedTLSCreds{
		config:     credinternal.CloneTLSConfig(c.config),
		verifyFunc: c.verifyFunc,
		getRootCAs: c.getRootCAs,
		isClient:   c.isClient,
	}
}

func (c *advancedTLSCreds) OverrideServerName(serverNameOverride string) error {
	c.config.ServerName = serverNameOverride
	return nil
}

// The function buildVerifyFunc is used when users want root cert reloading,
// and possibly custom verification check.
// We have to build our own verification function here because current
// tls module:
//  1. does not have a good support on root cert reloading.
//  2. will ignore basic certificate check when setting InsecureSkipVerify
//     to true.
func buildVerifyFunc(c *advancedTLSCreds,
	serverName string,
	rawConn net.Conn) func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		chains := verifiedChains
		var leafCert *x509.Certificate
		rawCertList := make([]*x509.Certificate, len(rawCerts))
		for i, asn1Data := range rawCerts {
			cert, err := x509.ParseCertificate(asn1Data)
			if err != nil {
				return err
			}
			rawCertList[i] = cert
		}
		if c.verificationType == CertAndHostVerification || c.verificationType == CertVerification {
			// perform possible trust credential reloading and certificate check
			rootCAs := c.config.RootCAs
			if !c.isClient {
				rootCAs = c.config.ClientCAs
			}
			// Reload root CA certs.
			if rootCAs == nil && c.getRootCAs != nil {
				results, err := c.getRootCAs(&GetRootCAsParams{
					RawConn:  rawConn,
					RawCerts: rawCerts,
				})
				if err != nil {
					return err
				}
				rootCAs = results.TrustCerts
			}
			// Verify peers' certificates against RootCAs and get verifiedChains.
			keyUsages := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
			if !c.isClient {
				keyUsages = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
			}
			opts := x509.VerifyOptions{
				Roots:         rootCAs,
				CurrentTime:   time.Now(),
				Intermediates: x509.NewCertPool(),
				KeyUsages:     keyUsages,
			}
			for _, cert := range rawCertList[1:] {
				opts.Intermediates.AddCert(cert)
			}
			// Perform default hostname check if specified.
			if c.isClient && c.verificationType == CertAndHostVerification && serverName != "" {
				parsedName, _, err := net.SplitHostPort(serverName)
				if err != nil {
					// If the serverName had no host port or if the serverName cannot be
					// parsed, use it as-is.
					parsedName = serverName
				}
				opts.DNSName = parsedName
			}
			var err error
			chains, err = rawCertList[0].Verify(opts)
			if err != nil {
				return err
			}
			leafCert = rawCertList[0]
		}
		// Perform certificate revocation check if specified.
		if c.revocationConfig != nil {
			verifiedChains := chains
			if verifiedChains == nil {
				verifiedChains = [][]*x509.Certificate{rawCertList}
			}
			if err := checkChainRevocation(verifiedChains, *c.revocationConfig); err != nil {
				return err
			}
		}
		// Perform custom verification check if specified.
		if c.verifyFunc != nil {
			_, err := c.verifyFunc(&HandshakeVerificationInfo{
				ServerName:     serverName,
				RawCerts:       rawCerts,
				VerifiedChains: chains,
				Leaf:           leafCert,
			})
			return err
		}
		return nil
	}
}

// NewClientCreds uses ClientOptions to construct a TransportCredentials based
// on TLS.
func NewClientCreds(o *ClientOptions) (credentials.TransportCredentials, error) {
	conf, err := o.config()
	if err != nil {
		return nil, err
	}
	tc := &advancedTLSCreds{
		config:           conf,
		isClient:         true,
		getRootCAs:       o.RootOptions.GetRootCertificates,
		verifyFunc:       o.AdditionalPeerVerification,
		verificationType: o.VerificationType,
		revocationConfig: o.RevocationConfig,
	}
	tc.config.NextProtos = credinternal.AppendH2ToNextProtos(tc.config.NextProtos)
	return tc, nil
}

// NewServerCreds uses ServerOptions to construct a TransportCredentials based
// on TLS.
func NewServerCreds(o *ServerOptions) (credentials.TransportCredentials, error) {
	conf, err := o.config()
	if err != nil {
		return nil, err
	}
	tc := &advancedTLSCreds{
		config:           conf,
		isClient:         false,
		getRootCAs:       o.RootOptions.GetRootCertificates,
		verifyFunc:       o.AdditionalPeerVerification,
		verificationType: o.VerificationType,
		revocationConfig: o.RevocationConfig,
	}
	tc.config.NextProtos = credinternal.AppendH2ToNextProtos(tc.config.NextProtos)
	return tc, nil
}
