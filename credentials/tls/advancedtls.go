/*
 *
 * Copyright 2018 gRPC authors.
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

// Package tls is an utility library containing functions to construct tls
// config that can perform credential reloading and custom server authorization.

package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"time"
)

type ClientOptions struct {
  // Certificates or GetClientCertificate indicates the certificates sent from clients to servers
  // to prove clients' identities. If requiring mutual authentication on server side, only one of
  // the two field must be set; otherwise these two fields are ignored.
	Certificates []tls.Certificate
	// If requiring mutual authentication and Certificates is nil, clients will invoke this
	// function every time a new connection is established and clients need to present certificates
	// to the servers, This is known as peer certificate reloading.
	GetClientCertificate func(*tls.CertificateRequestInfo) (*tls.Certificate, error)
	// A custom server authorization checking after certificate signature check. If nil, we will
	// only perform certificate signature check without hostname check.
	VerifyPeerCertificate func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error
	// RootCACerts or GetRootCAs indicates the certificates trusted by the client side.
	// Only one of the two field must be set.
	RootCACerts *x509.CertPool
	// If RootCACerts is nil, clients will invoke this function every time a new connection is
	// established and clients need to check certificates sent from the servers against root CAs.
	// This is known as trust certificate reloading.
	GetRootCAs func(rawCerts [][]byte) (*x509.CertPool, error)
	// serverNameOverride is for testing only. If set to a non empty string,
	// it will override the virtual host name of authority (e.g. :authority header field) in requests.
	ServerNameOverride string
}

type ServerOptions struct {
	// Certificates or GetClientCertificate indicates the certificates sent from servers to clients
	// to prove servers' identities. Only one of the two field must be set.
	Certificates []tls.Certificate
	// If Certificates is nil, servers will invoke this function every time a new connection
	// is established and servers need to present certificates to the clients, This is known as peer
	// certificate reloading.
	GetCertificate func(*tls.ClientHelloInfo) (*tls.Certificate, error)
	// Note on server side we don't usually perform custom client authorization check.
	VerifyPeerCertificate func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error
	// RootCACerts or GetRootCAs indicates the certificates trusted by the server side. If requiring
	// mutual authentication on server side, only one of the two field must be set; otherwise these
	// two fields are ignored.
	RootCACerts *x509.CertPool
	// If requiring mutual authentication and RootCACerts is nil, servers will invoke this function
	// every time a new connection is established and servers need to check certificates sent from
	// the clients against root CAs. This is known as trust certificate reloading.
	GetRootCAs func(rawCerts [][]byte) (*x509.CertPool, error)
	// If servers want clients to send certificates to prove its identities.
	MutualAuth bool
}


func (o *ClientOptions) Config() (*tls.Config, error) {
	if o.RootCACerts == nil && o.GetRootCAs == nil {
		return nil, fmt.Errorf("either RootCACerts or GetRootCAs must be specified")
	}
	config := &tls.Config{
		InsecureSkipVerify: true,
		ServerName: o.ServerNameOverride,
	}
	if o.Certificates != nil {
		config.Certificates = o.Certificates
	} else {
		config.GetClientCertificate = o.GetClientCertificate
	}

	// create a function which will reload the root cert and invoke users' VerifyPeerCertificate
	verifyFunc := func (rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		// verify peers' certificates against root CAs and get verifiedChains
		certs := make([]*x509.Certificate, len(rawCerts))
		for i, asn1Data := range rawCerts {
			cert, err := x509.ParseCertificate(asn1Data)
			if err != nil {
				return err
			}
			certs[i] = cert
		}
		rootCAs := o.RootCACerts
		// reload root CA certs if specified
		if rootCAs == nil {
			root, err := o.GetRootCAs(rawCerts)
			if err != nil {
				return err
			}
			rootCAs = root
		}
		opts := x509.VerifyOptions{
			Roots:         rootCAs,
			CurrentTime:   time.Now(),
			Intermediates: x509.NewCertPool(),
		}
		for _, cert := range certs[1:] {
			opts.Intermediates.AddCert(cert)
		}
		verifiedChains, err := certs[0].Verify(opts)
		if err != nil {
			return err
		}
		if o.VerifyPeerCertificate != nil {
			return o.VerifyPeerCertificate(rawCerts, verifiedChains)
		}
		return nil
	}
	config.VerifyPeerCertificate = verifyFunc
	return config, nil
}

func (o *ServerOptions) Config() (*tls.Config, error) {
	if o.Certificates == nil && o.GetCertificate == nil {
		return nil, fmt.Errorf("either Certificates or GetCertificate must be specified")
	}
	config := &tls.Config{}
	if o.Certificates != nil {
		config.Certificates = o.Certificates
	} else {
		config.GetCertificate = o.GetCertificate
	}
	if !o.MutualAuth {
		config.ClientAuth = tls.NoClientCert
		return config, nil
	}

	config.ClientAuth = tls.RequireAnyClientCert
	if o.RootCACerts == nil && o.GetRootCAs == nil {
		return nil, fmt.Errorf("server need trust certs if using mutual TLS")
	}
	// we don't usually do name checking on server side. Provide this function just in case we need
	// it.
	verifyFunc := func (rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		// verify peers' certificates against RootCAs and get verifiedChains
		certs := make([]*x509.Certificate, len(rawCerts))
		for i, asn1Data := range rawCerts {
			cert, _ := x509.ParseCertificate(asn1Data)
			certs[i] = cert
		}
		rootCAs := o.RootCACerts
		// reload root CA certs if specified
		if rootCAs == nil {
			root, err := o.GetRootCAs(rawCerts)
			if err != nil {
				return err
			}
			rootCAs = root
		}
		opts := x509.VerifyOptions{
			Roots:         rootCAs,
			CurrentTime:   time.Now(),
			Intermediates: x509.NewCertPool(),
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		}
		for _, cert := range certs[1:] {
			opts.Intermediates.AddCert(cert)
		}
		verifiedChains, err := certs[0].Verify(opts)
		if err != nil {
			return err
		}
		if o.VerifyPeerCertificate != nil {
			return o.VerifyPeerCertificate(rawCerts, verifiedChains)
		}
		return nil
	}
	config.VerifyPeerCertificate = verifyFunc
	return config, nil
}

