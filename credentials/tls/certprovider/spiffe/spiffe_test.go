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

package spiffe

import (
	"crypto/x509"
	"encoding/pem"
	"io"
	"os"
	"testing"

	"github.com/spiffe/go-spiffe/v2/bundle/spiffebundle"
	"google.golang.org/grpc/testdata"
)

var SPIFFETrustBundle map[string]*spiffebundle.Bundle

func TestKnownSPIFFEBundle(t *testing.T) {
	bundles, err := LoadSPIFFEBundleMap(testdata.Path("spiffe/spiffebundle.json"))
	if err != nil {
		t.Fatalf("Error during parsing: %v", err)
	}
	if len(bundles) != 2 {
		t.Fatal("did not parse correct bundle length")
	}
	if bundles["example.com"] == nil {
		t.Fatal("expected bundle for example.com")
	}
	if bundles["test.example.com"] == nil {
		t.Fatal("expected bundle for test.example.com")
	}

	expectedExampleComCert := loadX509Cert(testdata.Path("spiffe/spiffe_cert.pem"))
	expectedTestExampleComCert := loadX509Cert(testdata.Path("spiffe/server1_spiffe.pem"))
	if !bundles["example.com"].X509Authorities()[0].Equal(expectedExampleComCert) {
		t.Fatalf("expected cert for example.com bundle not correct.")
	}
	if !bundles["test.example.com"].X509Authorities()[0].Equal(expectedTestExampleComCert) {
		t.Fatalf("expected cert for test.example.com bundle not correct.")
	}

}

func loadX509Cert(t *testing.T, filePath string) *x509.Certificate {
	t.Helper()
	certFile, _ := os.Open(filePath)
	certRaw, _ := io.ReadAll(certFile)
	block, _ := pem.Decode([]byte(certRaw))
	if block == nil {
		panic("failed to parse certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		panic("failed to parse certificate: " + err.Error())
	}
	return cert
}

func TestLoadSPIFFEBundleMapFailures(t *testing.T) {
	testCases := []struct {
		filePath   string
		wantError  bool
		wantNoX509 bool
	}{
		{
			filePath:  testdata.Path("spiffe/spiffebundle_corrupted_cert.json"),
			wantError: true,
		},
		{
			filePath:  testdata.Path("spiffe/spiffebundle_malformed.json"),
			wantError: true,
		},
		{
			filePath:  testdata.Path("spiffe/spiffebundle_wrong_kid.json"),
			wantError: true,
		},
		{
			filePath:  testdata.Path("spiffe/spiffebundle_wrong_kty.json"),
			wantError: true,
		},
		{
			filePath:  testdata.Path("spiffe/spiffebundle_wrong_multi_certs.json"),
			wantError: true,
		},
		{
			filePath:  testdata.Path("spiffe/spiffebundle_wrong_root.json"),
			wantError: true,
		},
		{
			filePath:  testdata.Path("spiffe/spiffebundle_wrong_seq_type.json"),
			wantError: true,
		},
		{
			filePath:  testdata.Path("NOT_A_REAL_FILE"),
			wantError: true,
		},
		{
			filePath:  testdata.Path("spiffe/spiffebundle_invalid_trustdomain.json"),
			wantError: true,
		},
		{
			// SPIFFE Bundles only support a use of x509-svid and jwt-svid. If a
			// use other than this is specified, the parser does not fail, it
			// just doesn't add an x509 authority or jwt authority to the bundle
			filePath:   testdata.Path("spiffe/spiffebundle_wrong_use.json"),
			wantNoX509: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.filePath, func(t *testing.T) {
			bundle, err := LoadSPIFFEBundleMap(tc.filePath)
			if tc.wantError && err == nil {
				t.Fatalf("LoadSPIFFEBundleMap(%v) did not fail but should have.", tc.filePath)
			}
			if tc.wantNoX509 && len(bundle["example.com"].X509Authorities()) != 0 {
				t.Fatalf("LoadSPIFFEBundleMap(%v) did not have empty bundle but should have.", tc.filePath)
			}
		})
	}
}
