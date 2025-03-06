/*
 *
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

package credentials

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/url"
	"os"
	"testing"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/testdata"
)

const wantURI = "spiffe://foo.bar.com/client/workload/1"

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestSPIFFEIDFromState(t *testing.T) {
	tests := []struct {
		name string
		urls []*url.URL
		// If we expect a SPIFFE ID to be returned.
		wantID bool
	}{
		{
			name:   "empty URIs",
			urls:   []*url.URL{},
			wantID: false,
		},
		{
			name: "good SPIFFE ID",
			urls: []*url.URL{
				{
					Scheme:  "spiffe",
					Host:    "foo.bar.com",
					Path:    "workload/wl1",
					RawPath: "workload/wl1",
				},
			},
			wantID: true,
		},
		{
			name: "invalid host",
			urls: []*url.URL{
				{
					Scheme:  "spiffe",
					Host:    "",
					Path:    "workload/wl1",
					RawPath: "workload/wl1",
				},
			},
			wantID: false,
		},
		{
			name: "invalid path",
			urls: []*url.URL{
				{
					Scheme:  "spiffe",
					Host:    "foo.bar.com",
					Path:    "",
					RawPath: "",
				},
			},
			wantID: false,
		},
		{
			name: "large path",
			urls: []*url.URL{
				{
					Scheme:  "spiffe",
					Host:    "foo.bar.com",
					Path:    string(make([]byte, 2050)),
					RawPath: string(make([]byte, 2050)),
				},
			},
			wantID: false,
		},
		{
			name: "large host",
			urls: []*url.URL{
				{
					Scheme:  "spiffe",
					Host:    string(make([]byte, 256)),
					Path:    "workload/wl1",
					RawPath: "workload/wl1",
				},
			},
			wantID: false,
		},
		{
			name: "multiple URI SANs",
			urls: []*url.URL{
				{
					Scheme:  "spiffe",
					Host:    "foo.bar.com",
					Path:    "workload/wl1",
					RawPath: "workload/wl1",
				},
				{
					Scheme:  "spiffe",
					Host:    "bar.baz.com",
					Path:    "workload/wl2",
					RawPath: "workload/wl2",
				},
				{
					Scheme:  "https",
					Host:    "foo.bar.com",
					Path:    "workload/wl1",
					RawPath: "workload/wl1",
				},
			},
			wantID: false,
		},
		{
			name: "multiple URI SANs without SPIFFE ID",
			urls: []*url.URL{
				{
					Scheme:  "https",
					Host:    "foo.bar.com",
					Path:    "workload/wl1",
					RawPath: "workload/wl1",
				},
				{
					Scheme:  "ssh",
					Host:    "foo.bar.com",
					Path:    "workload/wl1",
					RawPath: "workload/wl1",
				},
			},
			wantID: false,
		},
		{
			name: "multiple URI SANs with one SPIFFE ID",
			urls: []*url.URL{
				{
					Scheme:  "spiffe",
					Host:    "foo.bar.com",
					Path:    "workload/wl1",
					RawPath: "workload/wl1",
				},
				{
					Scheme:  "https",
					Host:    "foo.bar.com",
					Path:    "workload/wl1",
					RawPath: "workload/wl1",
				},
			},
			wantID: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{{URIs: tt.urls}}}
			id := SPIFFEIDFromState(state)
			if got, want := id != nil, tt.wantID; got != want {
				t.Errorf("want wantID = %v, but SPIFFE ID is %v", want, id)
			}
		})
	}
}

func (s) TestSPIFFEIDFromCert(t *testing.T) {
	tests := []struct {
		name     string
		dataPath string
		// If we expect a SPIFFE ID to be returned.
		wantID bool
	}{
		{
			name:     "good certificate with SPIFFE ID",
			dataPath: "x509/spiffe_cert.pem",
			wantID:   true,
		},
		{
			name:     "bad certificate with SPIFFE ID and another URI",
			dataPath: "x509/multiple_uri_cert.pem",
			wantID:   false,
		},
		{
			name:     "certificate without SPIFFE ID",
			dataPath: "x509/client1_cert.pem",
			wantID:   false,
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
			uri := SPIFFEIDFromCert(cert)
			if (uri != nil) != tt.wantID {
				t.Fatalf("wantID got and want mismatch, got %t, want %t", uri != nil, tt.wantID)
			}
			if uri != nil && uri.String() != wantURI {
				t.Fatalf("SPIFFE ID not expected, got %s, want %s", uri.String(), wantURI)
			}
		})
	}
}

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
