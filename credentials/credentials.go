/*
 *
 * Copyright 2014, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

// Package credentials implements various credentials supported by gRPC library.
package credentials

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	metadataServer     = "metadata"
	serviceAccountPath = "/computeMetadata/v1/instance/service-accounts/default/token"
)

var (
	// alpnProtoStr are the specified application level protocols for gRPC.
	alpnProtoStr = []string{"h2-14", "h2-15", "h2-16"}
)

// Credentials defines the common interface all supported credentials must
// implement.
type Credentials interface {
	// GetRequestMetadata gets the current request metadata, refreshing
	// tokens if required. This should be called by the transport layer on
	// each request, and the data should be populated in headers or other
	// context. The operation may do things like refresh tokens.
	GetRequestMetadata() (map[string]string, error)
}

// TransportAuthenticator defines the common interface all supported transport
// authentication protocols (e.g., TLS, SSL) must implement.
type TransportAuthenticator interface {
	// Dial connects to the given network address and does the authentication
	// handshake specified by the corresponding authentication protocol.
	Dial(add string) (net.Conn, error)
	// NewListener creates a listener which accepts connections with requested
	// authentication handshake.
	NewListener(lis net.Listener) net.Listener
	Credentials
}

// tlsCreds is the credentials required for authenticating a connection.
type tlsCreds struct {
	// serverName is used to verify the hostname on the returned
	// certificates. It is also included in the client's handshake
	// to support virtual hosting. This is optional. If it is not
	// set gRPC internals will use the dialing address instead.
	serverName string
	// rootCAs defines the set of root certificate authorities
	// that clients use when verifying server certificates.
	// If rootCAs is nil, tls uses the host's root CA set.
	rootCAs *x509.CertPool
	// certificates contains one or more certificate chains
	// to present to the other side of the connection.
	// Server configurations must include at least one certificate.
	certificates []tls.Certificate
}

// GetRequestMetadata returns nil, nil since TLS credentials does not have
// metadata.
func (c *tlsCreds) GetRequestMetadata() (map[string]string, error) {
	return nil, nil
}

// Dial connects to addr and performs TLS handshake.
func (c *tlsCreds) Dial(addr string) (_ net.Conn, err error) {
	name := c.serverName
	if name == "" {
		name, _, err = net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse server address %v", err)
		}
	}
	return tls.Dial("tcp", addr, &tls.Config{
		RootCAs:    c.rootCAs,
		NextProtos: alpnProtoStr,
		ServerName: name,
	})
}

// NewListener creates a net.Listener with a TLS configuration constructed
// from the information in tlsCreds.
func (c *tlsCreds) NewListener(lis net.Listener) net.Listener {
	return tls.NewListener(lis, &tls.Config{
		Certificates: c.certificates,
		NextProtos:   alpnProtoStr,
	})
}

// NewClientTLSFromCert constructs a TLS from the input certificate for client.
func NewClientTLSFromCert(cp *x509.CertPool, serverName string) TransportAuthenticator {
	return &tlsCreds{
		serverName: serverName,
		rootCAs:    cp,
	}
}

// NewClientTLSFromFile constructs a TLS from the input certificate file for client.
func NewClientTLSFromFile(certFile, serverName string) (TransportAuthenticator, error) {
	b, err := ioutil.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM(b) {
		return nil, fmt.Errorf("failed to append certificates")
	}
	return &tlsCreds{
		serverName: serverName,
		rootCAs:    cp,
	}, nil
}

// NewServerTLSFromCert constructs a TLS from the input certificate for server.
func NewServerTLSFromCert(cert *tls.Certificate) TransportAuthenticator {
	return &tlsCreds{
		certificates: []tls.Certificate{*cert},
	}
}

// NewServerTLSFromFile constructs a TLS from the input certificate file and key
// file for server.
func NewServerTLSFromFile(certFile, keyFile string) (TransportAuthenticator, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return &tlsCreds{
		certificates: []tls.Certificate{cert},
	}, nil
}

type tokenData struct {
	accessToken string
	expiresIn   float64
	tokeType    string
}

type token struct {
	accessToken string
	expiry      time.Time
}

// expired returns true if there is no access token or the
// access token is expired.
func (t token) expired() bool {
	if t.accessToken == "" {
		return true
	}
	if t.expiry.IsZero() {
		return false
	}
	return t.expiry.Before(time.Now())
}

// computeEngine uses the Application Default Credentials as provided to Google Compute Engine instances.
type computeEngine struct {
	mu sync.Mutex
	t  token
}

// GetRequestMetadata returns a refreshed access token.
func (c *computeEngine) GetRequestMetadata() (map[string]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.t.expired() {
		if err := c.refresh(); err != nil {
			return nil, err
		}
	}
	return map[string]string{
		"authorization": "Bearer " + c.t.accessToken,
	}, nil
}

func (c *computeEngine) refresh() error {
	// https://developers.google.com/compute/docs/metadata
	// v1 requires "Metadata-Flavor: Google" header.
	tokenURL := &url.URL{
		Scheme: "http",
		Host:   metadataServer,
		Path:   serviceAccountPath,
	}
	req, err := http.NewRequest("GET", tokenURL.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Add("Metadata-Flavor", "Google")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var td tokenData
	err = json.NewDecoder(resp.Body).Decode(&td)
	if err != nil {
		return err
	}
	// No need to check td.tokenType.
	c.t = token{
		accessToken: td.accessToken,
		expiry:      time.Now().Add(time.Duration(td.expiresIn) * time.Second),
	}
	return nil
}

// NewComputeEngine constructs a credentials for GCE.
func NewComputeEngine() (Credentials, error) {
	creds := &computeEngine{}
	// TODO(zhaoq): This is not optimal if refresh() is persistently failed.
	if err := creds.refresh(); err != nil {
		return nil, err
	}
	return creds, nil
}
