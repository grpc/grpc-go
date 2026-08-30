/*
 *
 * Copyright 2026 gRPC authors.
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

package google

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Test verifies JWT payload extraction and expiration claim parsing across
// various valid and malformed token inputs.
func (s) TestParseJWTExpiry(t *testing.T) {
	tests := []struct {
		name    string
		jwtStr  string
		wantExp time.Time
		wantErr bool
	}{
		{
			name:    "valid_jwt",
			jwtStr:  "eyJhbGciOiJSUzI1NiJ9.eyJleHAiOjI1MjQ2MDgwMDB9.sig",
			wantExp: time.Unix(2524608000, 0),
			wantErr: false,
		},
		{
			name:    "invalid_format_single_part",
			jwtStr:  "invalidtoken",
			wantErr: true,
		},
		{
			name:    "invalid_format_two_parts",
			jwtStr:  "part1.part2",
			wantErr: true,
		},
		{
			name:    "invalid_format_four_parts",
			jwtStr:  "part1.part2.part3.part4",
			wantErr: true,
		},
		{
			name:    "invalid_base64_payload",
			jwtStr:  "eyJhbGciOiJSUzI1NiJ9.!!!invalid-base64!!!.sig",
			wantErr: true,
		},
		{
			name:    "invalid_json_payload",
			jwtStr:  "eyJhbGciOiJSUzI1NiJ9.bm90LWpzb24.sig", // "not-json" encoded
			wantErr: true,
		},
		{
			name:    "missing_exp_claim",
			jwtStr:  "eyJhbGciOiJSUzI1NiJ9.e30.sig", // "{}" encoded
			wantErr: true,
		},
		{
			name:    "zero_exp_claim",
			jwtStr:  "eyJhbGciOiJSUzI1NiJ9.eyJleHAiOjB9.sig", // '{"exp":0}' encoded
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exp, err := parseJWTExpiry(tc.jwtStr)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseJWTExpiry(%q) error = %v, wantErr %v", tc.jwtStr, err, tc.wantErr)
			}
			if !tc.wantErr && !exp.Equal(tc.wantExp) {
				t.Errorf("parseJWTExpiry(%q) = %v, want %v", tc.jwtStr, exp, tc.wantExp)
			}
		})
	}
}

// Test verifies that a successful token fetch from the metadata server sets
// the expected HTTP headers, passes the correct query parameters, and
// correctly parses the returned ID token.
func (s) TestFetchIDTokenFromMetadataServer_Success(t *testing.T) {
	const (
		audience   = "https://example.com"
		tokenValue = "eyJhbGciOiJSUzI1NiJ9.eyJleHAiOjI1MjQ2MDgwMDB9.sig"
	)
	wantExp := time.Unix(2524608000, 0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify mandatory GCP Metadata Server request header.
		if got := r.Header.Get("Metadata-Flavor"); got != "Google" {
			t.Errorf("Request Metadata-Flavor header = %q, want %q", got, "Google")
		}
		// Verify expected query parameters.
		if got := r.URL.Query().Get("audience"); got != audience {
			t.Errorf("Request audience query param = %q, want %q", got, audience)
		}
		if got := r.URL.Query().Get("format"); got != "full" {
			t.Errorf("Request format query param = %q, want %q", got, "full")
		}
		w.Write([]byte(tokenValue))
	}))
	defer server.Close()

	// Redirect GCE metadata server requests to our local mock HTTP server.
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(server.URL, "http://"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	val, exp, err := fetchIDTokenFromMetadataServer(ctx, audience)
	if err != nil {
		t.Fatalf("fetchIDTokenFromMetadataServer() failed: %v", err)
	}

	if val != tokenValue {
		t.Errorf("fetchIDTokenFromMetadataServer() val = %q, want %q", val, tokenValue)
	}

	if !exp.Equal(wantExp) {
		t.Errorf("fetchIDTokenFromMetadataServer() exp = %v, want %v", exp, wantExp)
	}
}

// Test verifies that HTTP status codes returned by the metadata server
// are mapped to the correct gRPC status error codes.
func (s) TestFetchIDTokenFromMetadataServer_HTTPStatusErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantCode   codes.Code
	}{
		{
			name:       "403_forbidden",
			statusCode: http.StatusForbidden,
			wantCode:   codes.Unauthenticated,
		},
		{
			name:       "429_too_many_requests",
			statusCode: http.StatusTooManyRequests,
			wantCode:   codes.Unavailable,
		},
		{
			name:       "503_service_unavailable",
			statusCode: http.StatusServiceUnavailable,
			wantCode:   codes.Unavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "error", tc.statusCode)
			}))
			defer server.Close()

			t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(server.URL, "http://"))

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, _, err := fetchIDTokenFromMetadataServer(ctx, "https://example.com")
			if gotCode := status.Code(err); gotCode != tc.wantCode {
				t.Errorf("fetchIDTokenFromMetadataServer() gRPC status code = %v, want %v (err: %v)", gotCode, tc.wantCode, err)
			}
		})
	}
}

// Test verifies that receiving a malformed, non-JWT token response from the
// metadata server results in a token fetching failure.
func (s) TestFetchIDTokenFromMetadataServer_MalformedToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("not-a-valid-jwt"))
	}))
	defer server.Close()

	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(server.URL, "http://"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, err := fetchIDTokenFromMetadataServer(ctx, "https://example.com")
	if gotCode := status.Code(err); gotCode != codes.Unavailable {
		t.Errorf("fetchIDTokenFromMetadataServer() gRPC status code = %v, want %v (err: %v)", gotCode, codes.Unavailable, err)
	}
}
