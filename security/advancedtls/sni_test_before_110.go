// +build !go1.10

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

package advancedtls

import (
	"crypto/tls"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/security/advancedtls/testdata"
)

// TestGetCertificatesSNI tests SNI logic for go1.9.
func TestGetCertificatesSNI(t *testing.T) {
	// Load server certificates for setting the serverGetCert callback function.
	serverCert1, err := tls.LoadX509KeyPair(testdata.Path("server_cert_1.pem"), testdata.Path("server_key_1.pem"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(server_cert_1.pem, server_key_1.pem) failed: %v", err)
	}
	serverCert2, err := tls.LoadX509KeyPair(testdata.Path("server_cert_2.pem"), testdata.Path("server_key_2.pem"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(server_cert_2.pem, server_key_2.pem) failed: %v", err)
	}
	serverCert3, err := tls.LoadX509KeyPair(testdata.Path("server_cert_3.pem"), testdata.Path("server_key_3.pem"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(server_cert_3.pem, server_key_3.pem) failed: %v", err)
	}

	tests := []struct {
		desc          string
		serverGetCert func(*tls.ClientHelloInfo) ([]*tls.Certificate, error)
		serverName    string
		wantCert      tls.Certificate
	}{
		{
			desc: "Select serverCert1",
			serverGetCert: func(info *tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				return []*tls.Certificate{&serverCert1, &serverCert2, &serverCert3}, nil
			},
			// "foo.bar.com" is the common name on server certificate server_cert_1.pem.
			serverName: "foo.bar.com",
			wantCert:   serverCert1,
		},
		{
			desc: "Select serverCert2",
			serverGetCert: func(info *tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				return []*tls.Certificate{&serverCert1, &serverCert2, &serverCert3}, nil
			},
			// "foo.bar.server2.com" is the common name on server certificate server_cert_2.pem.
			serverName: "foo.bar.server2.com",
			wantCert:   serverCert1,
		},
		{
			desc: "Select serverCert3",
			serverGetCert: func(info *tls.ClientHelloInfo) ([]*tls.Certificate, error) {
				return []*tls.Certificate{&serverCert1, &serverCert2, &serverCert3}, nil
			},
			// "google.com" is one of the DNS names on server certificate server_cert_3.pem.
			serverName: "google.com",
			wantCert:   serverCert1,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			serverOptions := &ServerOptions{
				GetCertificates: test.serverGetCert,
			}
			serverConfig, err := serverOptions.config()
			if err != nil {
				t.Fatalf("serverOptions.config() failed: %v", err)
			}
			pointFormatUncompressed := uint8(0)
			clientHello := &tls.ClientHelloInfo{
				CipherSuites:      []uint16{tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA},
				ServerName:        test.serverName,
				SupportedCurves:   []tls.CurveID{tls.CurveP256},
				SupportedPoints:   []uint8{pointFormatUncompressed},
				SupportedVersions: []uint16{tls.VersionTLS10},
			}
			gotCertificate, err := serverConfig.GetCertificate(clientHello)
			if err != nil {
				t.Fatalf("serverConfig.GetCertificate(clientHello) failed: %v", err)
			}
			if !cmp.Equal(gotCertificate, test.wantCert, cmp.AllowUnexported(tls.Certificate{}, tls.Certificate{})) {
				t.Errorf("GetCertificates() = %v, want %v", gotCertificate, test.wantCert)
			}
		})
	}
}
