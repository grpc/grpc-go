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
// credentials.TransportCredentials that can perform credential reloading and custom
// server authorization.
package advancedtls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"syscall"
	"time"

	"google.golang.org/grpc/credentials"
)

// VerificationFuncParams contains the parameters available to users when implementing CustomVerificationFunc.
type VerificationFuncParams struct {
	ServerName     string
	RawCerts       [][]byte
	VerifiedChains [][]*x509.Certificate
}

// VerificationResults contains the information about results of CustomVerificationFunc.
// VerificationResults is an empty struct for now. It may be extended in the future to include more information.
type VerificationResults struct{}

// CustomVerificationFunc is the function defined by users to perform custom server authorization.
// CustomVerificationFunc returns nil if the authorization fails; otherwise returns an empty struct.
type CustomVerificationFunc func(params *VerificationFuncParams) (*VerificationResults, error)

// GetRootCAsParams contains the parameters available to users when implementing GetRootCAs.
type GetRootCAsParams struct {
	RawConn  net.Conn
	RawCerts [][]byte
}

// GetRootCAsResults contains the results of GetRootCAs.
// If users want to reload the root trust certificate, it is required to return the proper TrustCerts in GetRootCAs.
type GetRootCAsResults struct {
	TrustCerts *x509.CertPool
}

// RootCertificateOptions contains a field and a function for obtaining root trust certificates.
// It is used by both ClientOptions and ServerOptions. Note that RootCertificateOptions is required
// to be correctly set on client side; on server side, it is only required when mutual TLS is
// enabled(RequireClientCert in ServerOptions is true).
type RootCertificateOptions struct {
	// If field RootCACerts is set, field GetRootCAs will be ignored. RootCACerts will be used
	// every time when verifying the peer certificates, without performing root certificate reloading.
	RootCACerts *x509.CertPool
	// If GetRootCAs is set and RootCACerts is nil, GetRootCAs will be invoked every time
	// asked to check certificates sent from the server when a new connection is established.
	// This is known as root CA certificate reloading.
	GetRootCAs func(params *GetRootCAsParams) (*GetRootCAsResults, error)
}

// ClientOptions contains all the fields and functions needed to be filled by the client.
// General rules for certificate setting on client side:
// Certificates or GetClientCertificate indicates the certificates sent from the client to the
// server to prove client's identities. The rules for setting these two fields are:
// If requiring mutual authentication on server side:
//     Either Certificates or GetClientCertificate must be set; the other will be ignored
// Otherwise:
//     Nothing needed(the two fields will be ignored)
type ClientOptions struct {
	// If field Certificates is set, field GetClientCertificate will be ignored. The client will use
	// Certificates every time when asked for a certificate, without performing certificate reloading.
	Certificates []tls.Certificate
	// If GetClientCertificate is set and Certificates is nil, the client will invoke this
	// function every time asked to present certificates to the server when a new connection is
	// established. This is known as peer certificate reloading.
	GetClientCertificate func(*tls.CertificateRequestInfo) (*tls.Certificate, error)
	// VerifyPeer is a custom server authorization checking after certificate signature check.
	// If this is set, we will replace the hostname check with this customized authorization check.
	// If this is nil, we fall back to typical hostname check.
	VerifyPeer CustomVerificationFunc
	// ServerNameOverride is for testing only. If set to a non-empty string,
	// it will override the virtual host name of authority (e.g. :authority header field) in requests.
	ServerNameOverride string
	RootCertificateOptions
}

// ServerOptions contains all the fields and functions needed to be filled by the client.
// General rules for certificate setting on server side:
// Certificates or GetClientCertificate indicates the certificates sent from the server to
// the client to prove server's identities. The rules for setting these two fields are:
// Either Certificates or GetCertificate must be set; the other will be ignored
type ServerOptions struct {
	// If field Certificates is set, field GetClientCertificate will be ignored. The server will use
	// Certificates every time when asked for a certificate, without performing certificate reloading.
	Certificates []tls.Certificate
	// If GetClientCertificate is set and Certificates is nil, the server will invoke this
	// function every time asked to present certificates to the client when a new connection is
	// established. This is known as peer certificate reloading.
	GetCertificate func(*tls.ClientHelloInfo) (*tls.Certificate, error)
	RootCertificateOptions
	// If the server want the client to send certificates.
	RequireClientCert bool
}

func (o *ClientOptions) config() (*tls.Config, error) {
	if o.RootCACerts == nil && o.GetRootCAs == nil && o.VerifyPeer == nil {
		return nil, fmt.Errorf(
			"client needs to provide root CA certs, or a custom verification function")
	}
	// We have to set InsecureSkipVerify to true to skip the default checks and use the
	// verification function we built from buildVerifyFunc.
	config := &tls.Config{
		ServerName:           o.ServerNameOverride,
		Certificates:         o.Certificates,
		GetClientCertificate: o.GetClientCertificate,
		RootCAs:              o.RootCACerts,
		InsecureSkipVerify:   true,
	}
	return config, nil
}

func (o *ServerOptions) config() (*tls.Config, error) {
	if o.Certificates == nil && o.GetCertificate == nil {
		return nil, fmt.Errorf("either Certificates or GetCertificate must be specified")
	}
	if o.RequireClientCert && o.GetRootCAs == nil && o.RootCACerts == nil {
		return nil, fmt.Errorf("server needs to provide root CA certs if requiring client cert")
	}
	clientAuth := tls.NoClientCert
	if o.RequireClientCert {
		// We fall back to normal config settings if users don't need to reload root certificates.
		// If using RequireAndVerifyClientCert, the underlying stack would use the default
		// checking and ignore the verification function we built from buildVerifyFunc.
		// If using RequireAnyClientCert, the code would skip all the checks and use the
		// function from buildVerifyFunc.
		if o.RootCACerts != nil {
			clientAuth = tls.RequireAndVerifyClientCert
		} else {
			clientAuth = tls.RequireAnyClientCert
		}
	}
	config := &tls.Config{
		ClientAuth:     clientAuth,
		Certificates:   o.Certificates,
		GetCertificate: o.GetCertificate,
	}
	if o.RootCACerts != nil {
		config.ClientCAs = o.RootCACerts
	}
	return config, nil
}

// advancedTLSCreds is the credentials required for authenticating a connection using TLS.
type advancedTLSCreds struct {
	config     *tls.Config
	verifyFunc CustomVerificationFunc
	getRootCAs func(params *GetRootCAsParams) (*GetRootCAsResults, error)
	isClient   bool
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
	cfg := cloneTLSConfig(c.config)
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
	return WrapSyscallConn(rawConn, conn), info, nil
}

func (c *advancedTLSCreds) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	cfg := cloneTLSConfig(c.config)
	// We build server side verification function only when root cert reloading is needed.
	if c.getRootCAs != nil {
		cfg.VerifyPeerCertificate = buildVerifyFunc(c, "", rawConn)
	}
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
	return WrapSyscallConn(rawConn, conn), info, nil
}

func (c *advancedTLSCreds) Clone() credentials.TransportCredentials {
	return &advancedTLSCreds{
		config:     cloneTLSConfig(c.config),
		verifyFunc: c.verifyFunc,
		getRootCAs: c.getRootCAs,
		isClient:   c.isClient,
	}
}

func (c *advancedTLSCreds) OverrideServerName(serverNameOverride string) error {
	c.config.ServerName = serverNameOverride
	return nil
}

// The function buildVerifyFunc is used when users want root cert reloading, and possibly custom
// server authorization check.
// We have to build our own verification function here because current tls module:
// 1. does not have a good support on root cert reloading
// 2. will ignore basic certificate check when setting InsecureSkipVerify to true
func buildVerifyFunc(c *advancedTLSCreds,
	serverName string,
	rawConn net.Conn) func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		// If users didn't specify either rootCAs or getRootCAs on client side,
		// as we see some use cases such as https://github.com/grpc/grpc/pull/20530,
		// instead of failing, we just don't validate the server cert and let
		// application decide via VerifyPeer
		if c.isClient && c.config.RootCAs == nil && c.getRootCAs == nil {
			if c.verifyFunc != nil {
				_, err := c.verifyFunc(&VerificationFuncParams{
					ServerName:     serverName,
					RawCerts:       rawCerts,
					VerifiedChains: verifiedChains,
				})
				return err
			}
		}
		var rootCAs *x509.CertPool
		if c.isClient {
			rootCAs = c.config.RootCAs
		} else {
			rootCAs = c.config.ClientCAs
		}
		// reload root CA certs
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
		// verify peers' certificates against RootCAs and get verifiedChains
		certs := make([]*x509.Certificate, len(rawCerts))
		for i, asn1Data := range rawCerts {
			cert, err := x509.ParseCertificate(asn1Data)
			if err != nil {
				return err
			}
			certs[i] = cert
		}
		opts := x509.VerifyOptions{
			Roots:         rootCAs,
			CurrentTime:   time.Now(),
			Intermediates: x509.NewCertPool(),
		}
		if !c.isClient {
			opts.KeyUsages = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		} else {
			opts.KeyUsages = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		}
		for _, cert := range certs[1:] {
			opts.Intermediates.AddCert(cert)
		}
		// We use default hostname check if users don't specify verifyFunc function
		if c.isClient && c.verifyFunc == nil && serverName != "" {
			opts.DNSName = serverName
		}
		verifiedChains, err := certs[0].Verify(opts)
		if err != nil {
			return err
		}
		if c.isClient && c.verifyFunc != nil {
			if c.verifyFunc != nil {
				_, err := c.verifyFunc(&VerificationFuncParams{
					ServerName:     serverName,
					RawCerts:       rawCerts,
					VerifiedChains: verifiedChains,
				})
				return err
			}
		}
		return nil
	}
}

// NewClientCreds uses ClientOptions to construct a TransportCredentials based on TLS.
func NewClientCreds(o *ClientOptions) (credentials.TransportCredentials, error) {
	conf, err := o.config()
	if err != nil {
		return nil, err
	}
	tc := &advancedTLSCreds{
		config:     conf,
		isClient:   true,
		getRootCAs: o.GetRootCAs,
		verifyFunc: o.VerifyPeer,
	}
	tc.config.NextProtos = appendH2ToNextProtos(tc.config.NextProtos)
	return tc, nil
}

// NewServerCreds uses ServerOptions to construct a TransportCredentials based on TLS.
func NewServerCreds(o *ServerOptions) (credentials.TransportCredentials, error) {
	conf, err := o.config()
	if err != nil {
		return nil, err
	}
	tc := &advancedTLSCreds{
		config:     conf,
		isClient:   false,
		getRootCAs: o.GetRootCAs,
	}
	tc.config.NextProtos = appendH2ToNextProtos(tc.config.NextProtos)
	return tc, nil
}

// TODO(ZhenLian): The code below are duplicates with gRPC-Go under
//                 credentials/internal. Consider refactoring in the future.
const alpnProtoStrH2 = "h2"

func appendH2ToNextProtos(ps []string) []string {
	for _, p := range ps {
		if p == alpnProtoStrH2 {
			return ps
		}
	}
	ret := make([]string, 0, len(ps)+1)
	ret = append(ret, ps...)
	return append(ret, alpnProtoStrH2)
}

// We give syscall.Conn a new name here since syscall.Conn and net.Conn used
// below have the same names.
type sysConn = syscall.Conn

// syscallConn keeps reference of rawConn to support syscall.Conn for channelz.
// SyscallConn() (the method in interface syscall.Conn) is explicitly
// implemented on this type,
//
// Interface syscall.Conn is implemented by most net.Conn implementations (e.g.
// TCPConn, UnixConn), but is not part of net.Conn interface. So wrapper conns
// that embed net.Conn don't implement syscall.Conn. (Side note: tls.Conn
// doesn't embed net.Conn, so even if syscall.Conn is part of net.Conn, it won't
// help here).
type syscallConn struct {
	net.Conn
	// sysConn is a type alias of syscall.Conn. It's necessary because the name
	// `Conn` collides with `net.Conn`.
	sysConn
}

// WrapSyscallConn tries to wrap rawConn and newConn into a net.Conn that
// implements syscall.Conn. rawConn will be used to support syscall, and newConn
// will be used for read/write.
//
// This function returns newConn if rawConn doesn't implement syscall.Conn.
func WrapSyscallConn(rawConn, newConn net.Conn) net.Conn {
	sysConn, ok := rawConn.(syscall.Conn)
	if !ok {
		return newConn
	}
	return &syscallConn{
		Conn:    newConn,
		sysConn: sysConn,
	}
}

func cloneTLSConfig(cfg *tls.Config) *tls.Config {
	if cfg == nil {
		return &tls.Config{}
	}
	return cfg.Clone()
}
