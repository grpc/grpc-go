/*
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

// Package testutils contains helper functions for advancedtls.
package testutils

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"

	"google.golang.org/grpc/security/advancedtls/testdata"
)

// CertStore contains all the certificates used in the integration tests.
type CertStore struct {
	// ClientCert1 is the certificate sent by client to prove its identity.
	// It is trusted by ServerTrust1.
	ClientCert1 tls.Certificate
	// ClientCert2 is the certificate sent by client to prove its identity.
	// It is trusted by ServerTrust2.
	ClientCert2 tls.Certificate
	// ServerCert1 is the certificate sent by server to prove its identity.
	// It is trusted by ClientTrust1.
	ServerCert1 tls.Certificate
	// ServerCert2 is the certificate sent by server to prove its identity.
	// It is trusted by ClientTrust2.
	ServerCert2 tls.Certificate
	// ServerPeer3 is the certificate sent by server to prove its identity.
	ServerPeer3 tls.Certificate
	// ServerPeerLocalhost1 is the certificate sent by server to prove its
	// identity. It has "localhost" as its common name, and is trusted by
	// ClientTrust1.
	ServerPeerLocalhost1 tls.Certificate
	// ClientTrust1 is the root certificate used on the client side.
	ClientTrust1 *x509.CertPool
	// ClientTrust2 is the root certificate used on the client side.
	ClientTrust2 *x509.CertPool
	// ServerTrust1 is the root certificate used on the server side.
	ServerTrust1 *x509.CertPool
	// ServerTrust2 is the root certificate used on the server side.
	ServerTrust2 *x509.CertPool
}

func readTrustCert(fileName string) (*x509.CertPool, error) {
	trustData, err := ioutil.ReadFile(fileName)
	if err != nil {
		return nil, err
	}
	trustPool := x509.NewCertPool()
	if !trustPool.AppendCertsFromPEM(trustData) {
		return nil, fmt.Errorf("error loading trust certificates")
	}
	return trustPool, nil
}

// LoadCerts function is used to load test certificates at the beginning of
// each integration test.
func (cs *CertStore) LoadCerts() error {
	var err error
	if cs.ClientCert1, err = tls.LoadX509KeyPair(testdata.Path("client_cert_1.pem"), testdata.Path("client_key_1.pem")); err != nil {
		return err
	}
	if cs.ClientCert2, err = tls.LoadX509KeyPair(testdata.Path("client_cert_2.pem"), testdata.Path("client_key_2.pem")); err != nil {
		return err
	}
	if cs.ServerCert1, err = tls.LoadX509KeyPair(testdata.Path("server_cert_1.pem"), testdata.Path("server_key_1.pem")); err != nil {
		return err
	}
	if cs.ServerCert2, err = tls.LoadX509KeyPair(testdata.Path("server_cert_2.pem"), testdata.Path("server_key_2.pem")); err != nil {
		return err
	}
	if cs.ServerPeer3, err = tls.LoadX509KeyPair(testdata.Path("server_cert_3.pem"), testdata.Path("server_key_3.pem")); err != nil {
		return err
	}
	if cs.ServerPeerLocalhost1, err = tls.LoadX509KeyPair(testdata.Path("server_cert_localhost_1.pem"), testdata.Path("server_key_localhost_1.pem")); err != nil {
		return err
	}
	if cs.ClientTrust1, err = readTrustCert(testdata.Path("client_trust_cert_1.pem")); err != nil {
		return err
	}
	if cs.ClientTrust2, err = readTrustCert(testdata.Path("client_trust_cert_2.pem")); err != nil {
		return err
	}
	if cs.ServerTrust1, err = readTrustCert(testdata.Path("server_trust_cert_1.pem")); err != nil {
		return err
	}
	if cs.ServerTrust2, err = readTrustCert(testdata.Path("server_trust_cert_2.pem")); err != nil {
		return err
	}
	return nil
}
