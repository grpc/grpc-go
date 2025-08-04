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

// Package advancedtls provides gRPC transport credentials that allow easy
// configuration of advanced TLS features. The APIs here give the user more
// customizable control to fit their security landscape, thus the "advanced"
// moniker. This package provides both interfaces and generally useful
// implementations of those interfaces, for example periodic credential
// reloading, support for certificate revocation lists, and customizable
// certificate verification behaviors. If the provided implementations do not
// fit a given use case, a custom implementation of the interface can be
// injected.
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

// CertificateChains represents a slice of certificate chains, each consisting
// of a sequence of certificates. Each chain represents a path from a leaf
// certificate up to a root certificate in the certificate hierarchy.
type CertificateChains [][]*x509.Certificate

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
	VerifiedChains CertificateChains
	// The leaf certificate sent from peer, if choosing to verify the peer
	// certificate(s) and that verification passed. This field would be nil if
	// either user chose not to verify or the verification failed.
	Leaf *x509.Certificate
}

// PostHandshakeVerificationResults contains the information about results of
// PostHandshakeVerificationFunc.
// PostHandshakeVerificationResults is an empty struct for now. It may be extended in the
// future to include more information.
type PostHandshakeVerificationResults struct{}

// PostHandshakeVerificationFunc is the function defined by users to perform
// custom verification checks after chain building and regular handshake
// verification has been completed.
// PostHandshakeVerificationFunc should return (nil, error) if the authorization
// should fail, with the error containing information on why it failed.
type PostHandshakeVerificationFunc func(params *HandshakeVerificationInfo) (*PostHandshakeVerificationResults, error)

// ConnectionInfo contains the parameters available to users when
// implementing GetRootCertificates.
type ConnectionInfo struct {
	// RawConn is the raw net.Conn representing a connection.
	RawConn net.Conn
	// RawCerts is the byte representation of the presented peer cert chain.
	RawCerts [][]byte
}

// RootCertificates is the result of GetRootCertificates.
// If users want to reload the root trust certificate, it is required to return
// the proper TrustCerts in GetRootCAs.
type RootCertificates struct {
	// TrustCerts is the pool of trusted certificates.
	TrustCerts *x509.CertPool
}

// RootCertificateOptions contains options to obtain root trust certificates
// for both the client and the server.
// At most one field should be set. If none of them are set, we use the system
// default trust certificates. Setting more than one field will result in
// undefined behavior.
type RootCertificateOptions struct {
	// If RootCertificates is set, it will be used every time when verifying
	// the peer certificates, without performing root certificate reloading.
	RootCertificates *x509.CertPool
	// If GetRootCertificates is set, it will be invoked to obtain root certs for
	// every new connection.
	GetRootCertificates func(params *ConnectionInfo) (*RootCertificates, error)
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
// At most one field should be set. Setting more than one field will result in undefined behavior.
type IdentityCertificateOptions struct {
	// If Certificates is set, it will be used every time when needed to present
	// identity certificates, without performing identity certificate reloading.
	Certificates []tls.Certificate
	// If GetIdentityCertificatesForClient is set, it will be invoked to obtain
	// identity certs for every new connection.
	// This field is only relevant when set on the client side.
	GetIdentityCertificatesForClient func(*tls.CertificateRequestInfo) (*tls.Certificate, error)
	// If GetIdentityCertificatesForServer is set, it will be invoked to obtain
	// identity certs for every new connection.
	// This field is only relevant when set on the server side.
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

// Options contains the fields a user can configure when setting up TLS clients
// and servers
type Options struct {
	// IdentityOptions is OPTIONAL on client side. This field only needs to be
	// set if mutual authentication is required on server side.
	// IdentityOptions is REQUIRED on server side.
	IdentityOptions IdentityCertificateOptions
	// AdditionalPeerVerification is a custom verification check after certificate signature
	// check.
	// If this is set, we will perform this customized check after doing the
	// normal check(s) indicated by setting VerificationType.
	AdditionalPeerVerification PostHandshakeVerificationFunc
	// RootOptions is OPTIONAL on server side. This field only needs to be set if
	// mutual authentication is required(RequireClientCert is true).
	RootOptions RootCertificateOptions
	// If the server requires the client to send certificates. This value is only
	// relevant when configuring options for the server. Is not used for
	// client-side configuration.
	RequireClientCert bool
	// VerificationType defines what type of peer verification is done. See
	// the `VerificationType` enum for the different options.
	// Default: CertAndHostVerification
	VerificationType VerificationType
	// RevocationOptions is the configurations for certificate revocation checks.
	// It could be nil if such checks are not needed.
	RevocationOptions *RevocationOptions
	// MinTLSVersion contains the minimum TLS version that is acceptable.
	// The value should be set using tls.VersionTLSxx from https://pkg.go.dev/crypto/tls
	// By default, TLS 1.2 is currently used as the minimum when acting as a
	// client, and TLS 1.0 when acting as a server. TLS 1.0 is the minimum
	// supported by this package, both as a client and as a server.  This
	// default may be changed over time affecting backwards compatibility.
	MinTLSVersion uint16
	// MaxTLSVersion contains the maximum TLS version that is acceptable.
	// The value should be set using tls.VersionTLSxx from https://pkg.go.dev/crypto/tls
	// By default, the maximum version supported by this package is used,
	// which is currently TLS 1.3.  This default may be changed over time
	// affecting backwards compatibility.
	MaxTLSVersion uint16
	// CipherSuites is an unordered list of supported TLS 1.0–1.2
	// ciphersuites. TLS 1.3 ciphersuites are not configurable. If nil, a
	// safe default list is used.
	CipherSuites []uint16
	// serverNameOverride is for testing only and only relevant on the client
	// side. If set to a non-empty string, it will override the virtual host
	// name of authority (e.g. :authority header field) in requests and the
	// target hostname used during server cert verification.
	serverNameOverride string
}

func (o *Options) clientConfig() (*tls.Config, error) {
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
	if o.MinTLSVersion > o.MaxTLSVersion {
		return nil, fmt.Errorf("the minimum TLS version is larger than the maximum TLS version")
	}
	// If the MinTLSVersion isn't set, default to 1.2
	if o.MinTLSVersion == 0 {
		o.MinTLSVersion = tls.VersionTLS12
	}
	// If the MaxTLSVersion isn't set, default to 1.3
	if o.MaxTLSVersion == 0 {
		o.MaxTLSVersion = tls.VersionTLS13
	}
	config := &tls.Config{
		ServerName: o.serverNameOverride,
		// We have to set InsecureSkipVerify to true to skip the default checks and
		// use the verification function we built from buildVerifyFunc.
		InsecureSkipVerify: true,
		MinVersion:         o.MinTLSVersion,
		MaxVersion:         o.MaxTLSVersion,
		CipherSuites:       o.CipherSuites,
	}
	// Propagate root-certificate-related fields in tls.Config.
	switch {
	case o.RootOptions.RootCertificates != nil:
		config.RootCAs = o.RootOptions.RootCertificates
	case o.RootOptions.GetRootCertificates != nil:
		// In cases when users provide GetRootCertificates callback, since this
		// callback is not contained in tls.Config, we have nothing to set here.
		// We will invoke the callback in ClientHandshake.
	case o.RootOptions.RootProvider != nil:
		o.RootOptions.GetRootCertificates = func(*ConnectionInfo) (*RootCertificates, error) {
			km, err := o.RootOptions.RootProvider.KeyMaterial(context.Background())
			if err != nil {
				return nil, err
			}
			return &RootCertificates{TrustCerts: km.Roots}, nil
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

func (o *Options) serverConfig() (*tls.Config, error) {
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
	if o.MinTLSVersion > o.MaxTLSVersion {
		return nil, fmt.Errorf("the minimum TLS version is larger than the maximum TLS version")
	}
	clientAuth := tls.NoClientCert
	if o.RequireClientCert {
		// We have to set clientAuth to RequireAnyClientCert to force underlying
		// TLS package to use the verification function we built from
		// buildVerifyFunc.
		clientAuth = tls.RequireAnyClientCert
	}
	// If the MinTLSVersion isn't set, default to 1.2
	if o.MinTLSVersion == 0 {
		o.MinTLSVersion = tls.VersionTLS12
	}
	// If the MaxTLSVersion isn't set, default to 1.3
	if o.MaxTLSVersion == 0 {
		o.MaxTLSVersion = tls.VersionTLS13
	}
	config := &tls.Config{
		ClientAuth:   clientAuth,
		MinVersion:   o.MinTLSVersion,
		MaxVersion:   o.MaxTLSVersion,
		CipherSuites: o.CipherSuites,
	}
	// Propagate root-certificate-related fields in tls.Config.
	switch {
	case o.RootOptions.RootCertificates != nil:
		config.ClientCAs = o.RootOptions.RootCertificates
	case o.RootOptions.GetRootCertificates != nil:
		// In cases when users provide GetRootCertificates callback, since this
		// callback is not contained in tls.Config, we have nothing to set here.
		// We will invoke the callback in ServerHandshake.
	case o.RootOptions.RootProvider != nil:
		o.RootOptions.GetRootCertificates = func(*ConnectionInfo) (*RootCertificates, error) {
			km, err := o.RootOptions.RootProvider.KeyMaterial(context.Background())
			if err != nil {
				return nil, err
			}
			return &RootCertificates{TrustCerts: km.Roots}, nil
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
	config              *tls.Config
	verifyFunc          PostHandshakeVerificationFunc
	getRootCertificates func(params *ConnectionInfo) (*RootCertificates, error)
	isClient            bool
	revocationOptions   *RevocationOptions
	verificationType    VerificationType
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
	// We return the full authority name to users without stripping the trailing
	// port.
	cfg.ServerName = authority

	peerVerifiedChains := CertificateChains{}
	cfg.VerifyPeerCertificate = buildVerifyFunc(c, cfg.ServerName, rawConn, &peerVerifiedChains)
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
	info.State.VerifiedChains = peerVerifiedChains
	return credinternal.WrapSyscallConn(rawConn, conn), info, nil
}

func (c *advancedTLSCreds) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	cfg := credinternal.CloneTLSConfig(c.config)
	peerVerifiedChains := CertificateChains{}
	cfg.VerifyPeerCertificate = buildVerifyFunc(c, "", rawConn, &peerVerifiedChains)
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
	info.State.VerifiedChains = peerVerifiedChains
	return credinternal.WrapSyscallConn(rawConn, conn), info, nil
}

func (c *advancedTLSCreds) Clone() credentials.TransportCredentials {
	return &advancedTLSCreds{
		config:              credinternal.CloneTLSConfig(c.config),
		verifyFunc:          c.verifyFunc,
		getRootCertificates: c.getRootCertificates,
		isClient:            c.isClient,
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
//
// peerVerifiedChains(output param): verified chain of certs from leaf to the
// trust cert that the peer trusts.
//  1. For server it is, client certs + Root ca that the server trusts
//  2. For client it is, server certs + Root ca that the client trusts
func buildVerifyFunc(c *advancedTLSCreds,
	serverName string,
	rawConn net.Conn,
	peerVerifiedChains *CertificateChains) func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
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
			if rootCAs == nil && c.getRootCertificates != nil {
				results, err := c.getRootCertificates(&ConnectionInfo{
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
		if c.revocationOptions != nil {
			verifiedChains := chains
			if verifiedChains == nil {
				verifiedChains = CertificateChains{rawCertList}
			}
			if err := checkChainRevocation(verifiedChains, *c.revocationOptions); err != nil {
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
			if err != nil {
				return err
			}
		}
		*peerVerifiedChains = chains
		return nil
	}
}

// NewClientCreds uses ClientOptions to construct a TransportCredentials based
// on TLS.
func NewClientCreds(o *Options) (credentials.TransportCredentials, error) {
	conf, err := o.clientConfig()
	if err != nil {
		return nil, err
	}
	tc := &advancedTLSCreds{
		config:              conf,
		isClient:            true,
		getRootCertificates: o.RootOptions.GetRootCertificates,
		verifyFunc:          o.AdditionalPeerVerification,
		revocationOptions:   o.RevocationOptions,
		verificationType:    o.VerificationType,
	}
	tc.config.NextProtos = credinternal.AppendH2ToNextProtos(tc.config.NextProtos)
	return tc, nil
}

// NewServerCreds uses ServerOptions to construct a TransportCredentials based
// on TLS.
func NewServerCreds(o *Options) (credentials.TransportCredentials, error) {
	conf, err := o.serverConfig()
	if err != nil {
		return nil, err
	}
	tc := &advancedTLSCreds{
		config:              conf,
		isClient:            false,
		getRootCertificates: o.RootOptions.GetRootCertificates,
		verifyFunc:          o.AdditionalPeerVerification,
		revocationOptions:   o.RevocationOptions,
		verificationType:    o.VerificationType,
	}
	tc.config.NextProtos = credinternal.AppendH2ToNextProtos(tc.config.NextProtos)
	return tc, nil
}
