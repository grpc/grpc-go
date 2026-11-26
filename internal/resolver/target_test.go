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

package resolver

import (
	"strings"
	"testing"

	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/resolver"

	// Register resolvers used by the validation tests.
	_ "google.golang.org/grpc/internal/resolver/dns"
	_ "google.golang.org/grpc/internal/resolver/passthrough"
)

func TestValidateTargetURI(t *testing.T) {
	tests := []struct {
		desc   string
		target string
	}{
		{
			desc:   "registered scheme with authority and endpoint",
			target: "dns:///endpoint",
		},
		{
			desc:   "uppercase registered scheme is canonicalized to lowercase",
			target: "DNS:///endpoint",
		},
		{
			desc:   "host:port without scheme falls back to default scheme",
			target: "my-service:50051",
		},
		{
			desc:   "dotted host:port without scheme falls back to default scheme",
			target: "trafficdirector.googleapis.com:443",
		},
		{
			desc:   "IP:port without scheme falls back to default scheme",
			target: "127.0.0.1:443",
		},
		{
			desc:   "registered-scheme opaque form uses registered scheme",
			target: "dns:endpoint",
		},
		{
			desc:   "unparseable URI is accepted after default-scheme fallback",
			target: "://bad",
		},
		{
			desc:   "absolute path with empty scheme uses default scheme",
			target: "/var/run/foo.sock",
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			if err := ValidateTargetURI(tc.target); err != nil {
				t.Fatalf("ValidateTargetURI(%q) = %v, want nil", tc.target, err)
			}
		})
	}
}

func TestValidateTargetURI_Error(t *testing.T) {
	tests := []struct {
		desc    string
		target  string
		wantErr string
	}{
		{
			desc:    "invalid percent-escape fails initial and fallback parsing",
			target:  "%zz",
			wantErr: "invalid URL escape",
		},
		{
			desc:    "empty target is rejected",
			target:  "",
			wantErr: "target URI cannot be empty",
		},
		{
			desc:    "authority-form URI with unregistered scheme is rejected to surface typos",
			target:  "no-such-scheme:///endpoint",
			wantErr: `uses scheme "no-such-scheme" which has no registered resolver`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := ValidateTargetURI(tc.target)
			if err == nil {
				t.Fatalf("ValidateTargetURI(%q) succeeded, want error containing %q", tc.target, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateTargetURI(%q) = %v, want error containing %q", tc.target, err, tc.wantErr)
			}
		})
	}
}

func TestValidateTargetURI_UserSetDefaultScheme(t *testing.T) {
	oldDefaultScheme := resolver.GetDefaultScheme()
	defer func() {
		// Reset the default scheme as though it was never set by the user.
		resolver.SetDefaultScheme(oldDefaultScheme)
		internal.UserSetDefaultScheme = false
	}()

	tests := []struct {
		desc          string
		defaultScheme string
		target        string
	}{
		{
			desc:          "host:port uses user-set default scheme",
			defaultScheme: "passthrough",
			target:        "my-service:50051",
		},
		{
			desc:          "registered opaque scheme takes precedence over default scheme",
			defaultScheme: "no-such-scheme",
			target:        "dns:endpoint",
		},
		{
			desc:          "uppercase default scheme is canonicalized to lowercase",
			defaultScheme: "DNS",
			target:        "endpoint",
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			resolver.SetDefaultScheme(tc.defaultScheme)
			if err := ValidateTargetURI(tc.target); err != nil {
				t.Fatalf("ValidateTargetURI(%q) with default scheme %q = %v, want nil", tc.target, tc.defaultScheme, err)
			}
		})
	}
}
