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
		desc    string
		target  string
		wantErr bool
	}{
		{
			desc:    "registered scheme with authority and endpoint",
			target:  "dns:///endpoint",
			wantErr: false,
		},
		{
			desc:    "uppercase registered scheme is canonicalized to lowercase",
			target:  "DNS:///endpoint",
			wantErr: false,
		},
		{
			desc:    "host:port without scheme falls back to default scheme",
			target:  "my-service:50051",
			wantErr: false,
		},
		{
			desc:    "dotted host:port without scheme falls back to default scheme",
			target:  "trafficdirector.googleapis.com:443",
			wantErr: false,
		},
		{
			desc:    "IP:port without scheme falls back to default scheme",
			target:  "127.0.0.1:443",
			wantErr: false,
		},
		{
			desc:    "registered-scheme opaque form falls back to default scheme",
			target:  "dns:endpoint",
			wantErr: false,
		},
		{
			desc:    "unparseable URI is accepted after default-scheme fallback",
			target:  "://bad",
			wantErr: false,
		},
		{
			desc:    "absolute path with empty scheme uses default scheme",
			target:  "/var/run/foo.sock",
			wantErr: false,
		},
		{
			desc:    "invalid percent-escape fails initial and fallback parsing",
			target:  "%zz",
			wantErr: true,
		},
		{
			desc:    "empty target is rejected",
			target:  "",
			wantErr: true,
		},
		{
			desc:    "authority-form URI with unregistered scheme is rejected to surface typos",
			target:  "no-such-scheme:///endpoint",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := ValidateTargetURI(tc.target)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateTargetURI(%q) = %v, wantErr %v", tc.target, err, tc.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), tc.target) && tc.target != "" {
				t.Errorf("ValidateTargetURI(%q) error %q does not mention target", tc.target, err)
			}
		})
	}
}

func TestValidateTargetURI_UserSetDefaultScheme(t *testing.T) {
	oldDefaultScheme := resolver.GetDefaultScheme()
	resolver.SetDefaultScheme("passthrough")
	defer func() {
		// Reset the default scheme as though it was never set by the user.
		resolver.SetDefaultScheme(oldDefaultScheme)
		internal.UserSetDefaultScheme = false
	}()
	if err := ValidateTargetURI("my-service:50051"); err != nil {
		t.Fatalf("ValidateTargetURI(%q) with user-set default scheme = %v, want nil", "my-service:50051", err)
	}
}
