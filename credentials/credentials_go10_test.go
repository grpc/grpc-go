// +build go1.10

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
	"net/url"
	"testing"
)

func (s) TestParseSpiffeID(t *testing.T) {
	tests := []struct {
		name string
		urls []*url.URL
		// If we expect ParseSpiffeID to return an error.
		expectError bool
		// If we expect TLSInfo.SpiffeID to be plumbed.
		expectID bool
	}{
		{
			name:        "empty URIs",
			urls:        []*url.URL{},
			expectError: false,
			expectID:    false,
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
				{
					Scheme:  "https",
					Host:    "foo.bar.com",
					Path:    "workload/wl1",
					RawPath: "workload/wl1",
				},
			},
			expectError: false,
			expectID:    true,
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
			expectError: true,
			expectID:    false,
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
			expectError: true,
			expectID:    false,
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
			expectError: true,
			expectID:    false,
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
			expectError: true,
			expectID:    false,
		},
		{
			name: "multiple SPIFFE IDs",
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
			},
			expectError: false,
			expectID:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := TLSInfo{
				State: tls.ConnectionState{PeerCertificates: []*x509.Certificate{{URIs: tt.urls}}}}
			err := info.ParseSpiffeID()
			if got, want := err != nil, tt.expectError; got != want {
				t.Errorf("want expectError = %v, but got expectError = %v, with error %v", want, got, err)
			}
			if got, want := info.SpiffeID != nil, tt.expectID; got != want {
				t.Errorf("want expectID = %v, but spiffe ID is %v", want, info.SpiffeID)
			}
		})
	}
}
