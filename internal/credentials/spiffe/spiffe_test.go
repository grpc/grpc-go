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
	"net/url"
	"os"
	"strings"
	"testing"

	"google.golang.org/grpc/testdata"
)

const wantURI = "spiffe://foo.bar.com/client/workload/1"

func loadFileBytes(t *testing.T, filePath string) []byte {
	bytes, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Error reading file: %v", err)
	}
	return bytes
}

func TestKnownSPIFFEBundle(t *testing.T) {
	spiffeBundleFile := testdata.Path("spiffe/spiffebundle.json")
	spiffeBundleBytes := loadFileBytes(t, spiffeBundleFile)
	bundles, err := BundleMapFromBytes(spiffeBundleBytes)
	if err != nil {
		t.Fatalf("BundleMapFromBytes(%v) Error during parsing: %v", spiffeBundleFile, err)
	}
	const wantBundleSize = 2
	if len(bundles) != wantBundleSize {
		t.Fatalf("BundleMapFromBytes(%v) did not parse correct bundle length. got %v want %v", spiffeBundleFile, len(bundles), wantBundleSize)
	}
	if bundles["example.com"] == nil {
		t.Fatalf("BundleMapFromBytes(%v) got no bundle for example.com", spiffeBundleFile)
	}
	if bundles["test.example.com"] == nil {
		t.Fatalf("BundleMapFromBytes(%v) got no bundle for test.example.com", spiffeBundleFile)
	}

	wantExampleComCert := loadX509Cert(t, testdata.Path("spiffe/spiffe_cert.pem"))
	wantTestExampleComCert := loadX509Cert(t, testdata.Path("spiffe/server1_spiffe.pem"))
	if !bundles["example.com"].X509Authorities()[0].Equal(wantExampleComCert) {
		t.Fatalf("BundleMapFromBytes(%v) parsed wrong cert for example.com.", spiffeBundleFile)
	}
	if !bundles["test.example.com"].X509Authorities()[0].Equal(wantTestExampleComCert) {
		t.Fatalf("BundleMapFromBytes(%v) parsed wrong cert for test.example.com", spiffeBundleFile)
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

func TestSPIFFEBundleMapFailures(t *testing.T) {
	filePaths := []string{
		testdata.Path("spiffe/spiffebundle_corrupted_cert.json"),
		testdata.Path("spiffe/spiffebundle_malformed.json"),
		testdata.Path("spiffe/spiffebundle_wrong_kid.json"),
		testdata.Path("spiffe/spiffebundle_wrong_kty.json"),
		testdata.Path("spiffe/spiffebundle_wrong_multi_certs.json"),
		testdata.Path("spiffe/spiffebundle_wrong_root.json"),
		testdata.Path("spiffe/spiffebundle_wrong_seq_type.json"),
		testdata.Path("spiffe/spiffebundle_invalid_trustdomain.json"),
		testdata.Path("spiffe/spiffebundle_empty_string_key.json"),
		testdata.Path("spiffe/spiffebundle_empty_keys.json"),
	}
	for _, path := range filePaths {
		t.Run(path, func(t *testing.T) {
			bundleBytes := loadFileBytes(t, path)
			if _, err := BundleMapFromBytes(bundleBytes); err == nil {
				t.Fatalf("BundleMapFromBytes(%v) did not fail but should have.", path)
			}
		})
	}
}

func TestSPIFFEBundleMapX509Failures(t *testing.T) {
	// SPIFFE Bundles only support a use of x509-svid and jwt-svid. If a
	// use other than this is specified, the parser does not fail, it
	// just doesn't add an x509 authority or jwt authority to the bundle
	filePath := testdata.Path("spiffe/spiffebundle_wrong_use.json")
	bundleBytes := loadFileBytes(t, filePath)
	bundle, err := BundleMapFromBytes(bundleBytes)
	if err != nil {
		t.Fatalf("BundleMapFromBytes(%v) failed with error: %v", filePath, err)
	}
	if len(bundle["example.com"].X509Authorities()) != 0 {
		t.Fatalf("BundleMapFromBytes(%v) did not have empty bundle but should have.", filePath)
	}
}

func TestGetRootsFromSPIFFEBundleMapSuccess(t *testing.T) {
	bundleMapFile := testdata.Path("spiffe/spiffebundle_match_client_spiffe.json")
	bundleBytes := loadFileBytes(t, bundleMapFile)
	bundle, err := BundleMapFromBytes(bundleBytes)
	if err != nil {
		t.Fatalf("BundleMapFromBytes(%v) failed with error: %v", bundleMapFile, err)
	}

	cert := loadX509Cert(t, testdata.Path("spiffe/client_spiffe.pem"))
	gotRoots, err := GetRootsFromSPIFFEBundleMap(bundle, cert)
	if err != nil {
		t.Fatalf("GetRootsFromSPIFFEBundleMap() failed with err %v", err)
	}
	wantRoot := loadX509Cert(t, testdata.Path("spiffe/spiffe_cert.pem"))
	wantRoots := x509.NewCertPool()
	wantRoots.AddCert(wantRoot)
	if !gotRoots.Equal(wantRoots) {
		t.Fatalf("GetRootsFromSPIFFEBundleMap() got %v want %v", gotRoots, wantRoots)
	}
}

func TestGetRootsFromSPIFFEBundleMapFailures(t *testing.T) {
	bundleMapFile := testdata.Path("spiffe/spiffebundle.json")
	bundleBytes := loadFileBytes(t, bundleMapFile)
	bundle, err := BundleMapFromBytes(bundleBytes)
	certWithTwoURIs := loadX509Cert(t, testdata.Path("spiffe/client_spiffe.pem"))
	certWithTwoURIs.URIs = append(certWithTwoURIs.URIs, certWithTwoURIs.URIs[0])
	certWithNoURIs := loadX509Cert(t, testdata.Path("spiffe/client_spiffe.pem"))
	certWithNoURIs.URIs = nil
	if err != nil {
		t.Fatalf("BundleMapFromBytes(%v) failed with error: %v", bundleMapFile, err)
	}
	tests := []struct {
		name          string
		bundleMapFile string
		leafCert      *x509.Certificate
		wantErr       string
	}{
		{
			name:     "no bundle for peer cert spiffeID",
			leafCert: loadX509Cert(t, testdata.Path("spiffe/client_spiffe.pem")),
			wantErr:  "no bundle found for peer certificates",
		},
		{
			name:     "cert has invalid SPIFFE id",
			leafCert: loadX509Cert(t, testdata.Path("ca.pem")),
			wantErr:  "could not get spiffe ID from peer leaf cert",
		},
		{
			name:     "nil cert",
			leafCert: nil,
			wantErr:  "input cert is nil",
		},
		{
			name:     "cert has multiple URIs",
			leafCert: certWithTwoURIs,
			wantErr:  "input cert has 2 URIs but should have 1",
		},
		{
			name:     "cert has no URIs",
			leafCert: certWithNoURIs,
			wantErr:  "input cert has 0 URIs but should have 1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err = GetRootsFromSPIFFEBundleMap(bundle, tc.leafCert)
			if err == nil {
				t.Fatalf("GetRootsFromSPIFFEBundleMap() got no error but want error containing %v.", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("GetRootsFromSPIFFEBundleMap() got error: %v. want error to contain %v", err, tc.wantErr)
			}
		})
	}
}

func TestIDFromCert(t *testing.T) {
	cert := loadX509Cert(t, testdata.Path("x509/spiffe_cert.pem"))
	uri, err := idFromCert(cert)
	if err != nil {
		t.Fatalf("idFromCert() failed with err: %v", err)
	}
	if uri != nil && uri.String() != wantURI {
		t.Fatalf("ID not expected, got %s, want %s", uri.String(), wantURI)
	}
}

func TestIDFromCertFileFailures(t *testing.T) {
	certWithNoURIs := loadX509Cert(t, testdata.Path("spiffe/client_spiffe.pem"))
	certWithNoURIs.URIs = nil
	certWithInvalidSPIFFEID := loadX509Cert(t, testdata.Path("spiffe/client_spiffe.pem"))
	certWithInvalidSPIFFEID.URIs = []*url.URL{{Path: "non-spiffe.bad"}}
	tests := []struct {
		name string
		cert *x509.Certificate
	}{
		{
			name: "certificate with multiple URIs",
			cert: loadX509Cert(t, testdata.Path("x509/multiple_uri_cert.pem")),
		},
		{
			name: "certificate with invalidSPIFFE ID",
			cert: certWithInvalidSPIFFEID,
		},
		{
			name: "nil cert",
			cert: nil,
		},
		{
			name: "cert with no URIs",
			cert: certWithNoURIs,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := idFromCert(tt.cert); err == nil {
				t.Fatalf("IDFromCert() succeeded but want error")
			}
		})
	}
}
