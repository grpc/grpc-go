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

package tls

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"reflect"
	"testing"
)

func TestClientConfig(t *testing.T) {
	cert := make([]tls.Certificate, 0)
	getClientCert := func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
		return nil, nil
	}

	for _, test := range []struct {
		desc                     string
		cert                     []tls.Certificate
		getClientCert            func(*tls.CertificateRequestInfo) (*tls.Certificate, error)
		serverNameOverride       string
		root                     *x509.CertPool
		getRoot                  func(rawCerts [][]byte) (*x509.CertPool, error)
		expectError              bool
		expectCert               []tls.Certificate
		expectGetCert            func(*tls.CertificateRequestInfo) (*tls.Certificate, error)
		expectServerNameOverride string
	}{
		{
			"both_root_and_getRoot_are_nil",
			cert,
			getClientCert,
			"foo",
			nil,
			nil,
			true,
			nil,
			nil,
			"bar",
		},
		{
			"nil_cert",
			nil,
			getClientCert,
			"foo",
			&x509.CertPool{},
			nil,
			false,
			nil,
			getClientCert,
			"foo",
		},
		{
			"nil_getCert",
			cert,
			nil,
			"foo",
			&x509.CertPool{},
			nil,
			false,
			cert,
			nil,
			"foo",
		},
	} {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			options := &ClientOptions{
				Certificates:         test.cert,
				GetClientCertificate: test.getClientCert,
				RootCACerts:          test.root,
				GetRootCAs:           test.getRoot,
				ServerNameOverride:   test.serverNameOverride,
			}
			config, err := options.Config()
			if got, want := err != nil, test.expectError; got != want {
				t.Fatalf("%s: got error = %v, want error = %v.", test.desc, got, want)
			}
			if err != nil {
				return
			}
			if config.VerifyPeerCertificate == nil {
				t.Errorf("%s: VerifyPeerCertificate is nil.", test.desc)
			}
			if got, want := config.ServerName, test.expectServerNameOverride; got != want {
				t.Errorf("%s: got ServerNameOveride = %v, want ServerNameOveride = %v.", test.desc, got, want)
			}
			if got, want := config.InsecureSkipVerify, true; got != want {
				t.Errorf("%s: got InsecureSkipVerify = %v, want InsecureSkipVerify = %v.", test.desc, got, want)
			}
			if got, want := config.Certificates, test.cert; !reflect.DeepEqual(got, want) {
				t.Errorf("%s: got Certificates = %v, want Certificates = %v.", test.desc, got, want)
			}
		})
	}
}

func TestVerifyFunction(t *testing.T) {
	cert := make([]tls.Certificate, 0)
	rootCert := x509.NewCertPool()
	d, err := ioutil.ReadFile("testdata/ca_cert.pem")
	if err != nil {
		t.Fatalf("load root cert error: %v.", err)
	}
	ok := rootCert.AppendCertsFromPEM(d)
	if !ok {
		t.Fatalf("load root cert error")
	}
	getRootSuccess := func(rawCerts [][]byte) (*x509.CertPool, error) {
		return rootCert, nil
	}
	getRootFailure := func(rawCerts [][]byte) (*x509.CertPool, error) {
		return nil, errors.New("error")
	}
	verifyFunc := func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		if rawCerts == nil || len(rawCerts) == 0 {
			return errors.New("error")
		}
		certs := make([]*x509.Certificate, len(rawCerts))
		for i, asn1Data := range rawCerts {
			cert, _ := x509.ParseCertificate(asn1Data)
			certs[i] = cert
		}
		if certs[0].Subject.CommonName != "foo.bar.com" {
			return errors.New("common name error")
		}
		return nil
	}
	verifyFailure := func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		return errors.New("error")
	}

	for _, test := range []struct {
		desc        string
		root        *x509.CertPool
		getRoot     func(rawCerts [][]byte) (*x509.CertPool, error)
		verifyFunc  func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error
		expectError bool
	}{
		{
			"variable_root_certs",
			rootCert,
			nil,
			verifyFunc,
			false,
		},
		{
			"function_root_certs_success",
			nil,
			getRootSuccess,
			verifyFunc,
			false,
		},
		{
			"function_root_certs_failure",
			nil,
			getRootFailure,
			verifyFunc,
			true,
		},
		{
			"empty_verification",
			rootCert,
			nil,
			nil,
			false,
		},
		{
			"verification_failure",
			rootCert,
			nil,
			verifyFailure,
			true,
		},
	} {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			clientOptions := &ClientOptions{
				Certificates:          cert,
				RootCACerts:           test.root,
				GetRootCAs:            test.getRoot,
				VerifyPeerCertificate: test.verifyFunc,
			}
			clientConfig, err := clientOptions.Config()
			if err != nil {
				t.Fatalf("%s: clientConfig creation error: %v.", test.desc, err)
			}
			err = checkVerifyPeerCertificate(clientConfig, test.expectError)
			if err != nil {
				t.Fatalf("%s: clientConfig checkVerifyPeerCertificate failed: %v.", test.desc, err)
			}
		})
	}
}

func TestServerConfig(t *testing.T) {
	cert := make([]tls.Certificate, 0)
	getCert := func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return nil, nil
	}
	getRootCert := func(rawCerts [][]byte) (*x509.CertPool, error) {
		return nil, nil
	}
	rootCert := x509.NewCertPool()
	for _, test := range []struct {
		desc             string
		cert             []tls.Certificate
		getCert          func(*tls.ClientHelloInfo) (*tls.Certificate, error)
		mutualAuth       bool
		root             *x509.CertPool
		getRoot          func(rawCerts [][]byte) (*x509.CertPool, error)
		expectError      bool
		expectCert       []tls.Certificate
		expectGetCert    func(*tls.ClientHelloInfo) (*tls.Certificate, error)
		expectClientAuth tls.ClientAuthType
	}{
		{
			"both_cert_and_getCert_are_nil",
			nil,
			nil,
			true,
			nil,
			nil,
			true,
			nil,
			nil,
			tls.RequireAnyClientCert,
		},
		{
			"nil_getCert",
			cert,
			nil,
			true,
			rootCert,
			nil,
			false,
			cert,
			nil,
			tls.RequireAndVerifyClientCert,
		},
		{
			"nil_cert",
			nil,
			getCert,
			true,
			rootCert,
			nil,
			false,
			nil,
			getCert,
			tls.RequireAndVerifyClientCert,
		},
		{
			"nil_root_cert",
			cert,
			nil,
			true,
			nil,
			getRootCert,
			false,
			cert,
			nil,
			tls.RequireAnyClientCert,
		},
		{
			"no_mutual_tls",
			nil,
			getCert,
			false,
			rootCert,
			nil,
			false,
			nil,
			getCert,
			tls.NoClientCert,
		},
		{
			"mutual_tls_but_no_root",
			nil,
			getCert,
			true,
			nil,
			nil,
			true,
			nil,
			getCert,
			tls.NoClientCert,
		},
	} {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			options := &ServerOptions{
				Certificates:   test.cert,
				GetCertificate: test.getCert,
				RootCACerts:    test.root,
				GetRootCAs:     test.getRoot,
				MutualAuth:     test.mutualAuth,
			}
			config, err := options.Config()
			if got, want := err != nil, test.expectError; got != want {
				t.Fatalf("%s: got error = %v, want error = %v.", test.desc, got, want)
			}
			if err != nil {
				return
			}
			if got, want := config.ClientAuth, test.expectClientAuth; got != want {
				t.Errorf("%s: got ClientAuth = %v, want ClientAuth = %v.", test.desc, got, want)
			}
			if got, want := config.Certificates, test.cert; !reflect.DeepEqual(got, want) {
				t.Errorf("%s: got Certificates = %v, want Certificates = %v.", test.desc, got, want)
			}
		})
	}
}

func checkVerifyPeerCertificate(config *tls.Config, expectError bool) error {
	if config.VerifyPeerCertificate == nil {
		return fmt.Errorf("function VerifyPeerCertificate is nil")
	}
	data, err := ioutil.ReadFile("testdata/client_cert.pem")
	if err != nil {
		return fmt.Errorf("load peer cert error: %v", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return fmt.Errorf("failed to parse certificate PEM")
	}
	peerCert := [][]byte{block.Bytes}
	err = config.VerifyPeerCertificate(peerCert, nil)
	if got, want := err != nil, expectError; got != want {
		return fmt.Errorf("got error = %v, want error = %v", got, want)
	}
	return nil
}
