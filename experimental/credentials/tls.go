/*
 *
 * Copyright 2025 gRPC authors.
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

// Package credentials provides experimental TLS credentials.
// The use of this package is strongly discouraged. These credentials exist
// solely to maintain compatibility for users interacting with clients that
// violate the HTTP/2 specification by not advertising support for "h2" in ALPN.
// This package is slated for removal in upcoming grpc-go releases. Users must
// not rely on this package directly. Instead, they should either vendor a
// specific version of gRPC or copy the relevant credentials into their own
// codebase if absolutely necessary.
package credentials

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"

	"golang.org/x/net/http2"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/experimental/credentials/internal"
)

// tlsCreds is the credentials required for authenticating a connection using TLS.
type tlsCreds struct {
	// TLS configuration
	config *tls.Config
}

func (c tlsCreds) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{
		SecurityProtocol: "tls",
		SecurityVersion:  "1.2",
		ServerName:       c.config.ServerName,
	}
}

func (c *tlsCreds) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (_ net.Conn, _ credentials.AuthInfo, err error) {
	// use local cfg to avoid clobbering ServerName if using multiple endpoints
	cfg := cloneTLSConfig(c.config)

	serverName, _, err := net.SplitHostPort(authority)
	if err != nil {
		// If the authority had no host port or if the authority cannot be parsed, use it as-is.
		serverName = authority
	}
	cfg.ServerName = serverName

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

	tlsInfo := credentials.TLSInfo{
		State: conn.ConnectionState(),
		CommonAuthInfo: credentials.CommonAuthInfo{
			SecurityLevel: credentials.PrivacyAndIntegrity,
		},
	}
	id := internal.SPIFFEIDFromState(conn.ConnectionState())
	if id != nil {
		tlsInfo.SPIFFEID = id
	}
	return internal.WrapSyscallConn(rawConn, conn), tlsInfo, nil
}

func (c *tlsCreds) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	conn := tls.Server(rawConn, c.config)
	if err := conn.Handshake(); err != nil {
		conn.Close()
		return nil, nil, err
	}
	cs := conn.ConnectionState()
	tlsInfo := credentials.TLSInfo{
		State: cs,
		CommonAuthInfo: credentials.CommonAuthInfo{
			SecurityLevel: credentials.PrivacyAndIntegrity,
		},
	}
	id := internal.SPIFFEIDFromState(conn.ConnectionState())
	if id != nil {
		tlsInfo.SPIFFEID = id
	}
	return internal.WrapSyscallConn(rawConn, conn), tlsInfo, nil
}

func (c *tlsCreds) Clone() credentials.TransportCredentials {
	return NewTLSWithALPNDisabled(c.config)
}

func (c *tlsCreds) OverrideServerName(serverNameOverride string) error {
	c.config.ServerName = serverNameOverride
	return nil
}

// The following cipher suites are forbidden for use with HTTP/2 by
// https://datatracker.ietf.org/doc/html/rfc7540#appendix-A
var tls12ForbiddenCipherSuites = map[uint16]struct{}{
	tls.TLS_RSA_WITH_AES_128_CBC_SHA:         {},
	tls.TLS_RSA_WITH_AES_256_CBC_SHA:         {},
	tls.TLS_RSA_WITH_AES_128_GCM_SHA256:      {},
	tls.TLS_RSA_WITH_AES_256_GCM_SHA384:      {},
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA: {},
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA: {},
	tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA:   {},
	tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA:   {},
}

// NewTLSWithALPNDisabled uses c to construct a TransportCredentials based on
// TLS. ALPN verification is disabled.
func NewTLSWithALPNDisabled(c *tls.Config) credentials.TransportCredentials {
	config := applyDefaults(c)
	if config.GetConfigForClient != nil {
		oldFn := config.GetConfigForClient
		config.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			cfgForClient, err := oldFn(hello)
			if err != nil || cfgForClient == nil {
				return cfgForClient, err
			}
			return applyDefaults(cfgForClient), nil
		}
	}
	return &tlsCreds{config: config}
}

func applyDefaults(c *tls.Config) *tls.Config {
	config := cloneTLSConfig(c)
	config.NextProtos = appendH2ToNextProtos(config.NextProtos)
	// If the user did not configure a MinVersion and did not configure a
	// MaxVersion < 1.2, use MinVersion=1.2, which is required by
	// https://datatracker.ietf.org/doc/html/rfc7540#section-9.2
	if config.MinVersion == 0 && (config.MaxVersion == 0 || config.MaxVersion >= tls.VersionTLS12) {
		config.MinVersion = tls.VersionTLS12
	}
	// If the user did not configure CipherSuites, use all "secure" cipher
	// suites reported by the TLS package, but remove some explicitly forbidden
	// by https://datatracker.ietf.org/doc/html/rfc7540#appendix-A
	if config.CipherSuites == nil {
		for _, cs := range tls.CipherSuites() {
			if _, ok := tls12ForbiddenCipherSuites[cs.ID]; !ok {
				config.CipherSuites = append(config.CipherSuites, cs.ID)
			}
		}
	}
	return config
}

// NewClientTLSFromCertWithALPNDisabled constructs TLS credentials from the
// provided root certificate authority certificate(s) to validate server
// connections. If certificates to establish the identity of the client need to
// be included in the credentials (eg: for mTLS), use NewTLS instead, where a
// complete tls.Config can be specified.
// serverNameOverride is for testing only. If set to a non empty string,
// it will override the virtual host name of authority (e.g. :authority header
// field) in requests. ALPN verification is disabled.
func NewClientTLSFromCertWithALPNDisabled(cp *x509.CertPool, serverNameOverride string) credentials.TransportCredentials {
	return NewTLSWithALPNDisabled(&tls.Config{ServerName: serverNameOverride, RootCAs: cp})
}

// NewClientTLSFromFileWithALPNDisabled constructs TLS credentials from the
// provided root certificate authority certificate file(s) to validate server
// connections. If certificates to establish the identity of the client need to
// be included in the credentials (eg: for mTLS), use NewTLS instead, where a
// complete tls.Config can be specified.
// serverNameOverride is for testing only. If set to a non empty string,
// it will override the virtual host name of authority (e.g. :authority header
// field) in requests. ALPN verification is disabled.
func NewClientTLSFromFileWithALPNDisabled(certFile, serverNameOverride string) (credentials.TransportCredentials, error) {
	b, err := os.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM(b) {
		return nil, fmt.Errorf("credentials: failed to append certificates")
	}
	return NewTLSWithALPNDisabled(&tls.Config{ServerName: serverNameOverride, RootCAs: cp}), nil
}

// NewServerTLSFromCertWithALPNDisabled constructs TLS credentials from the
// input certificate for server. ALPN verification is disabled.
func NewServerTLSFromCertWithALPNDisabled(cert *tls.Certificate) credentials.TransportCredentials {
	return NewTLSWithALPNDisabled(&tls.Config{Certificates: []tls.Certificate{*cert}})
}

// NewServerTLSFromFileWithALPNDisabled constructs TLS credentials from the
// input certificate file and key file for server. ALPN verification is disabled.
func NewServerTLSFromFileWithALPNDisabled(certFile, keyFile string) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return NewTLSWithALPNDisabled(&tls.Config{Certificates: []tls.Certificate{cert}}), nil
}

// cloneTLSConfig returns a shallow clone of the exported
// fields of cfg, ignoring the unexported sync.Once, which
// contains a mutex and must not be copied.
//
// If cfg is nil, a new zero tls.Config is returned.
func cloneTLSConfig(cfg *tls.Config) *tls.Config {
	if cfg == nil {
		return &tls.Config{}
	}

	return cfg.Clone()
}

// appendH2ToNextProtos appends h2 to next protos.
func appendH2ToNextProtos(ps []string) []string {
	for _, p := range ps {
		if p == http2.NextProtoTLS {
			return ps
		}
	}
	ret := make([]string, 0, len(ps)+1)
	ret = append(ret, ps...)
	return append(ret, http2.NextProtoTLS)
}
