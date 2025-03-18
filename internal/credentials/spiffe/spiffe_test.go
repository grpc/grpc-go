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

	"github.com/spiffe/go-spiffe/v2/bundle/spiffebundle"
	"google.golang.org/grpc/testdata"
)

const wantURI = "spiffe://foo.bar.com/client/workload/1"

// loadSPIFFEBundleMap loads a SPIFFE Bundle Map from a file. See the SPIFFE
// Bundle Map spec for more detail -
// https://github.com/spiffe/spiffe/blob/main/standards/SPIFFE_Trust_Domain_and_Bundle.md#4-spiffe-bundle-format
// If duplicate keys are encountered in the JSON parsing, Go's default unmarshal
// behavior occurs which causes the last processed entry to be the entry in the
// parsed map.
func loadSPIFFEBundleMap(filePath string) (map[string]*spiffebundle.Bundle, error) {
	bundleMapRaw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	return BundleMapFromBytes(bundleMapRaw)
}

func TestKnownSPIFFEBundle(t *testing.T) {
	spiffeBundleFile := testdata.Path("spiffe/spiffebundle.json")
	bundles, err := loadSPIFFEBundleMap(spiffeBundleFile)
	if err != nil {
		t.Fatalf("loadSPIFFEBundleMap(%v) Error during parsing: %v", spiffeBundleFile, err)
	}
	wantBundleSize := 2
	if len(bundles) != wantBundleSize {
		t.Fatalf("loadSPIFFEBundleMap(%v) did not parse correct bundle length. got %v want %v", spiffeBundleFile, len(bundles), wantBundleSize)
	}
	if bundles["example.com"] == nil {
		t.Fatalf("loadSPIFFEBundleMap(%v) got no bundle for example.com", spiffeBundleFile)
	}
	if bundles["test.example.com"] == nil {
		t.Fatalf("loadSPIFFEBundleMap(%v) got no bundle for test.example.com", spiffeBundleFile)
	}

	expectedExampleComCert := loadX509Cert(t, testdata.Path("spiffe/spiffe_cert.pem"))
	expectedTestExampleComCert := loadX509Cert(t, testdata.Path("spiffe/server1_spiffe.pem"))
	if !bundles["example.com"].X509Authorities()[0].Equal(expectedExampleComCert) {
		t.Fatalf("loadSPIFFEBundleMap(%v) parsed wrong cert for example.com.", spiffeBundleFile)
	}
	if !bundles["test.example.com"].X509Authorities()[0].Equal(expectedTestExampleComCert) {
		t.Fatalf("loadSPIFFEBundleMap(%v) parsed wrong cert for test.example.com", spiffeBundleFile)
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
		testdata.Path("NOT_A_REAL_FILE"),
		testdata.Path("spiffe/spiffebundle_invalid_trustdomain.json"),
		testdata.Path("spiffe/spiffebundle_empty_string_key.json"),
		testdata.Path("spiffe/spiffebundle_empty_keys.json"),
	}
	for _, path := range filePaths {
		t.Run(path, func(t *testing.T) {
			if _, err := loadSPIFFEBundleMap(path); err == nil {
				t.Fatalf("loadSPIFFEBundleMap(%v) did not fail but should have.", path)
			}
		})
	}
}

func TestSPIFFEBundleMapX509Failures(t *testing.T) {
	// SPIFFE Bundles only support a use of x509-svid and jwt-svid. If a
	// use other than this is specified, the parser does not fail, it
	// just doesn't add an x509 authority or jwt authority to the bundle
	filePath := testdata.Path("spiffe/spiffebundle_wrong_use.json")
	bundle, err := loadSPIFFEBundleMap(filePath)
	if err != nil {
		t.Fatalf("loadSPIFFEBundleMap(%v) failed with error: %v", filePath, err)
	}
	if len(bundle["example.com"].X509Authorities()) != 0 {
		t.Fatalf("loadSPIFFEBundleMap(%v) did not have empty bundle but should have.", filePath)
	}
}

func TestGetRootsFromSPIFFEBundleMapSuccess(t *testing.T) {
	bundleMapFile := testdata.Path("spiffe/spiffebundle_match_client_spiffe.json")
	bundle, err := loadSPIFFEBundleMap(bundleMapFile)
	if err != nil {
		t.Fatalf("loadSPIFFEBundleMap(%v) failed with error: %v", bundleMapFile, err)
	}

	cert := loadX509Cert(t, testdata.Path("spiffe/client_spiffe.pem"))
	rootCerts, err := GetRootsFromSPIFFEBundleMap(bundle, cert)
	if err != nil {
		t.Fatalf("GetRootsFromSPIFFEBundleMap() failed with err %v", err)
	}
	wantRoot := loadX509Cert(t, testdata.Path("spiffe/spiffe_cert.pem"))
	wantPool := x509.NewCertPool()
	wantPool.AddCert(wantRoot)
	if !rootCerts.Equal(wantPool) {
		t.Fatalf("GetRootsFromSPIFFEBundleMap() got %v want %v", rootCerts, wantPool)
	}
}

func TestGetRootsFromSPIFFEBundleMapFailures(t *testing.T) {
	tests := []struct {
		name          string
		bundleMapFile string
		leafCertFile  string
		wantErr       string
	}{
		{
			name:          "no bundle for peer cert spiffeID",
			bundleMapFile: testdata.Path("spiffe/spiffebundle.json"),
			leafCertFile:  testdata.Path("spiffe/client_spiffe.pem"),
			wantErr:       "no bundle found for peer certificates",
		},
		{
			name:          "cert has invalid SPIFFE id",
			bundleMapFile: testdata.Path("spiffe/spiffebundle.json"),
			leafCertFile:  testdata.Path("ca.pem"),
			wantErr:       "could not get spiffe ID from peer leaf cert",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bundle, err := loadSPIFFEBundleMap(tc.bundleMapFile)
			if err != nil {
				t.Fatalf("loadSPIFFEBundleMap(%v) failed with error: %v", tc.bundleMapFile, err)
			}
			cert := loadX509Cert(t, tc.leafCertFile)
			_, err = GetRootsFromSPIFFEBundleMap(bundle, cert)
			if err == nil {
				t.Fatalf("GetRootsFromSPIFFEBundleMap() want err got none")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("GetRootsFromSPIFFEBundleMap() want err to contain %v. got %v", tc.wantErr, err)
			}
		})
	}
}

func TestGetRootsFromSPIFFEBundleMapNilCert(t *testing.T) {
	wantErr := "input cert is nil"
	bundleMapFile := testdata.Path("spiffe/spiffebundle.json")
	bundle, err := loadSPIFFEBundleMap(bundleMapFile)
	if err != nil {
		t.Fatalf("loadSPIFFEBundleMap(%v) failed with error: %v", bundleMapFile, err)
	}
	_, err = GetRootsFromSPIFFEBundleMap(bundle, nil)
	if err == nil {
		t.Fatalf("GetRootsFromSPIFFEBundleMap() want err got none")
	}
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("GetRootsFromSPIFFEBundleMap() want err to contain %v. got %v", wantErr, err)
	}
}

func TestGetRootsFromSPIFFEBundleMapMultipleURIs(t *testing.T) {
	wantErr := "input cert has more than 1 URI"
	bundleMapFile := testdata.Path("spiffe/spiffebundle.json")
	leafCertFile := testdata.Path("spiffe/client_spiffe.pem")
	bundle, err := loadSPIFFEBundleMap(bundleMapFile)
	if err != nil {
		t.Fatalf("loadSPIFFEBundleMap(%v) failed with error: %v", bundleMapFile, err)
	}
	cert := loadX509Cert(t, leafCertFile)
	// Add a duplicate URI of the first
	cert.URIs = append(cert.URIs, cert.URIs[0])
	_, err = GetRootsFromSPIFFEBundleMap(bundle, cert)
	if err == nil {
		t.Fatalf("GetRootsFromSPIFFEBundleMap() want err got none")
	}
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("GetRootsFromSPIFFEBundleMap() want err to contain %v. got %v", wantErr, err)
	}
}

func TestIDFromCert(t *testing.T) {
	data, err := os.ReadFile(testdata.Path("x509/spiffe_cert.pem"))
	if err != nil {
		t.Fatalf("os.ReadFile(%s) failed: %v", "x509/spiffe_cert.pem", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatalf("Failed to parse the certificate: byte block is nil")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("x509.ParseCertificate(%b) failed: %v", block.Bytes, err)
	}
	uri, err := IDFromCert(cert)
	if err != nil {
		t.Fatalf("IDFromCert() failed with err: %v", err)
	}
	if uri != nil && uri.String() != wantURI {
		t.Fatalf(" ID not expected, got %s, want %s", uri.String(), wantURI)
	}
}

func TestIDFromCertFileFailures(t *testing.T) {
	tests := []struct {
		name     string
		dataPath string
	}{
		{
			name:     "certificate with multiple URIs",
			dataPath: "x509/multiple_uri_cert.pem",
		},
		{
			name:     "certificate without SPIFFE ID",
			dataPath: "x509/client1_cert.pem",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(testdata.Path(tt.dataPath))
			if err != nil {
				t.Fatalf("os.ReadFile(%s) failed: %v", testdata.Path(tt.dataPath), err)
			}
			block, _ := pem.Decode(data)
			if block == nil {
				t.Fatalf("Failed to parse the certificate: byte block is nil")
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				t.Fatalf("x509.ParseCertificate(%b) failed: %v", block.Bytes, err)
			}
			_, err = IDFromCert(cert)
			if err == nil {
				t.Fatalf("IDFromCert() succeeded but want error")
			}
		})
	}
}

func TestIDFromCertNoURIs(t *testing.T) {
	data, err := os.ReadFile(testdata.Path("x509/spiffe_cert.pem"))
	if err != nil {
		t.Fatalf("os.ReadFile(%s) failed: %v", testdata.Path("x509/spiffe_cert.pem"), err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatalf("Failed to parse the certificate: byte block is nil")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("x509.ParseCertificate(%b) failed: %v", block.Bytes, err)
	}
	// This cert has valid URIs. Set to nil for this test
	cert.URIs = nil
	_, err = IDFromCert(cert)
	if err == nil {
		t.Fatalf("IDFromCert() succeeded but want error")
	}
}

func TestIDFromCertNonSPIFFEURI(t *testing.T) {
	data, err := os.ReadFile(testdata.Path("x509/spiffe_cert.pem"))
	if err != nil {
		t.Fatalf("os.ReadFile(%s) failed: %v", testdata.Path("x509/spiffe_cert.pem"), err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatalf("Failed to parse the certificate: byte block is nil")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("x509.ParseCertificate(%b) failed: %v", block.Bytes, err)
	}
	// This cert has valid URIs. Set to nil for this test
	cert.URIs = []*url.URL{{Path: "non-spiffe.bad"}}
	_, err = IDFromCert(cert)
	if err == nil {
		t.Fatal("IDFromCert() succeeded but want error")
	}
}

func TestIDFromCertNil(t *testing.T) {
	_, err := IDFromCert(nil)
	if err == nil {
		t.Fatalf("IDFromCert() succeeded but want error")
	}
}
