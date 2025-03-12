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

	"google.golang.org/grpc/testdata"
)

func TestKnownSPIFFEBundle(t *testing.T) {
	spiffeBundleFile := testdata.Path("spiffe/spiffebundle.json")
	bundles, err := LoadSPIFFEBundleMap(spiffeBundleFile)
	if err != nil {
		t.Fatalf("LoadSPIFFEBundleMap(%v) Error during parsing: %v", spiffeBundleFile, err)
	}
	wantBundleSize := 2
	if len(bundles) != wantBundleSize {
		t.Fatalf("LoadSPIFFEBundleMap(%v) did not parse correct bundle length. got %v want %v", spiffeBundleFile, len(bundles), wantBundleSize)
	}
	if bundles["example.com"] == nil {
		t.Fatalf("LoadSPIFFEBundleMap(%v) got no bundle for example.com", spiffeBundleFile)
	}
	if bundles["test.example.com"] == nil {
		t.Fatalf("LoadSPIFFEBundleMap(%v) got no bundle for test.example.com", spiffeBundleFile)
	}

	expectedExampleComCert := loadX509Cert(t, testdata.Path("spiffe/spiffe_cert.pem"))
	expectedTestExampleComCert := loadX509Cert(t, testdata.Path("spiffe/server1_spiffe.pem"))
	if !bundles["example.com"].X509Authorities()[0].Equal(expectedExampleComCert) {
		t.Fatalf("LoadSPIFFEBundleMap(%v) parsed wrong cert for example.com.", spiffeBundleFile)
	}
	if !bundles["test.example.com"].X509Authorities()[0].Equal(expectedTestExampleComCert) {
		t.Fatalf("LoadSPIFFEBundleMap(%v) parsed wrong cert for test.example.com", spiffeBundleFile)
	}

}

func loadX509Cert(t *testing.T, filePath string) *x509.Certificate {
	t.Helper()
	certFile, _ := os.Open(filePath)
	certRaw, _ := io.ReadAll(certFile)
	block, _ := pem.Decode([]byte(certRaw))
	if block == nil {
		t.Fatalf("pem.Decode(%v) = nil. Want a value.", certRaw)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("x509.ParseCertificate(%v) failed %v", block.Bytes, err.Error())
	}
	return cert
}

func TestLoadSPIFFEBundleMapFailures(t *testing.T) {
	filePaths := []string{
		testdata.Path("spiffe/spiffebundle_corrupted_cert.json"),
		testdata.Path("spiffe/spiffebundle_malformed.json"),
		testdata.Path("spiffe/spiffebundle_wrong_kid.json"),
		testdata.Path("spiffe/spiffebundle_wrong_kty.json"),
		testdata.Path("spiffe/spiffebundle_wrong_multi_certs.json"),
		testdata.Path("spiffe/spiffebundle_wrong_root.json"),
		testdata.Path("spiffe/spiffebundle_wrong_seq_type.json"),
		testdata.Path("NOT_A_REAL_FILE"),
		testdata.Path("spiffe/spiffebundle_invalid_trustdomain.json"),
	}
	for _, path := range filePaths {
		t.Run(path, func(t *testing.T) {
			if _, err := LoadSPIFFEBundleMap(path); err == nil {
				t.Fatalf("LoadSPIFFEBundleMap(%v) did not fail but should have.", path)
			}
		})
	}
}

func TestLoadSPIFFEBundleMapX509Failures(t *testing.T) {
	// SPIFFE Bundles only support a use of x509-svid and jwt-svid. If a
	// use other than this is specified, the parser does not fail, it
	// just doesn't add an x509 authority or jwt authority to the bundle
	filePath := testdata.Path("spiffe/spiffebundle_wrong_use.json")
	bundle, err := LoadSPIFFEBundleMap(filePath)
	if err != nil {
		t.Fatalf("LoadSPIFFEBundleMap(%v) failed with error: %v", filePath, err)
	}
	if len(bundle["example.com"].X509Authorities()) != 0 {
		t.Fatalf("LoadSPIFFEBundleMap(%v) did not have empty bundle but should have.", filePath)
	}
}
