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
	"io/ioutil"
	"net/url"
	"testing"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/testdata"
)

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
		expectID bool
	}{
		{
			name:     "empty URIs",
			urls:     []*url.URL{},
			expectID: false,
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
			expectID: true,
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
			expectID: false,
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
			expectID: false,
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
			expectID: false,
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
			expectID: false,
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
			expectID: false,
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
			expectID: false,
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
			expectID: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{{URIs: tt.urls}}}
			id := SPIFFEIDFromState(state)
			if got, want := id != nil, tt.expectID; got != want {
				t.Errorf("want expectID = %v, but SPIFFE ID is %v", want, id)
			}
		})
	}
}

func (s) TestSPIFFEIDFromCert(t *testing.T) {
	data, err := ioutil.ReadFile(testdata.Path("x509/spiffe_cert.pem"))
	if err != nil {
		t.Fatalf("Failed to load the test credentials: %v", err)
	}
	block, rest := pem.Decode(data)
	if len(rest) != 0 {
		t.Fatalf("Failed to parse the certificate: invalid format")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("Failed to load the test credentials: %v", err)
	}
	uri := SPIFFEIDFromCert(cert)
	if uri == nil {
		t.Fatalf("SPIFFE ID is nil")
	}
	if uri.String() != "spiffe://foo.bar.com/client/workload/1" {
		t.Fatalf("SPIFFE ID not expected, got %s, want %s", uri.String(), "spiffe://foo.bar.com/client/workload/1")
	}
}
