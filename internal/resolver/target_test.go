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

package resolver_test

import (
	"strings"
	"testing"

	iresolver "google.golang.org/grpc/internal/resolver"
	_ "google.golang.org/grpc/internal/resolver/passthrough" // Register passthrough resolver.
	"google.golang.org/grpc/resolver"
	_ "google.golang.org/grpc/resolver/dns" // Register dns resolver.
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name          string
		target        string
		defaultScheme string
		wantScheme    string
		wantErr       bool
		errContain    string
	}{
		{
			name:       "valid_dns_scheme",
			target:     "dns:///example.com:443",
			wantScheme: "dns",
		},
		{
			name:       "valid_passthrough_scheme",
			target:     "passthrough:///localhost:8080",
			wantScheme: "passthrough",
		},
		{
			name:          "valid_dns_scheme_with_default",
			target:        "dns:///example.com:443",
			defaultScheme: "dns",
			wantScheme:    "dns",
		},
		{
			name:          "missing_scheme_falls_back_to_default",
			target:        "/path/to/socket",
			defaultScheme: "passthrough",
			wantScheme:    "passthrough",
		},
		{
			name:       "missing_scheme_without_default",
			target:     "/path/to/socket",
			wantErr:    true,
			errContain: "has no scheme",
		},
		{
			name:          "host_port_retries_with_default_scheme",
			target:        "localhost:8080",
			defaultScheme: "passthrough",
			wantScheme:    "passthrough",
		},
		{
			name:       "host_port_without_default",
			target:     "localhost:8080",
			wantErr:    true,
			errContain: "no resolver registered for scheme",
		},
		{
			name:       "unregistered_scheme",
			target:     "unknown:///example.com:443",
			wantErr:    true,
			errContain: "no resolver registered for scheme",
		},
		{
			name:          "unregistered_scheme_falls_back_to_default",
			target:        "unknown:///foo",
			defaultScheme: "passthrough",
			wantScheme:    "passthrough",
		},
		{
			name:       "invalid_URI",
			target:     "dns:///example\x00.com",
			wantErr:    true,
			errContain: "invalid target URI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := iresolver.ParseTarget(tt.target, tt.defaultScheme, resolver.Get)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTarget(%q, %q) error = %v, wantErr %v", tt.target, tt.defaultScheme, err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("ParseTarget(%q, %q) error = %q, want it to contain %q", tt.target, tt.defaultScheme, err, tt.errContain)
				}
				return
			}
			if got.URL.Scheme != tt.wantScheme {
				t.Errorf("ParseTarget(%q, %q).URL.Scheme = %q, want %q", tt.target, tt.defaultScheme, got.URL.Scheme, tt.wantScheme)
			}
		})
	}
}

func TestParseTargetWithCustomBuilder(t *testing.T) {
	// A registry that only recognises "passthrough". This mirrors the
	// cc.getResolver pattern in ClientConn, which may include resolvers
	// registered via dial options that are invisible to resolver.Get.
	passthroughOnly := func(scheme string) resolver.Builder {
		if scheme == "passthrough" {
			return resolver.Get("passthrough")
		}
		return nil
	}

	tests := []struct {
		name          string
		target        string
		defaultScheme string
		wantScheme    string
		wantErr       bool
		errContain    string
	}{
		{
			name:       "known_scheme_resolves",
			target:     "passthrough:///service:8080",
			wantScheme: "passthrough",
		},
		{
			name:       "dns_not_in_custom_registry",
			target:     "dns:///example.com:443",
			wantErr:    true,
			errContain: "no resolver registered for scheme",
		},
		{
			name:          "unregistered_scheme_falls_back_to_default",
			target:        "dns:///example.com:443",
			defaultScheme: "passthrough",
			wantScheme:    "passthrough",
		},
		{
			// Opaque URI (host:port form) falls back to the default scheme.
			name:          "host_port_falls_back_to_custom_default",
			target:        "service:8080",
			defaultScheme: "passthrough",
			wantScheme:    "passthrough",
		},
		{
			name:       "missing_scheme_without_default",
			target:     "/path",
			wantErr:    true,
			errContain: "has no scheme",
		},
		{
			name:          "missing_scheme_uses_default",
			target:        "/path",
			defaultScheme: "passthrough",
			wantScheme:    "passthrough",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := iresolver.ParseTarget(tt.target, tt.defaultScheme, passthroughOnly)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTarget(%q, %q) error = %v, wantErr %v", tt.target, tt.defaultScheme, err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("ParseTarget(%q, %q) error = %q, want it to contain %q", tt.target, tt.defaultScheme, err, tt.errContain)
				}
				return
			}
			if got.URL.Scheme != tt.wantScheme {
				t.Errorf("ParseTarget(%q, %q).URL.Scheme = %q, want %q", tt.target, tt.defaultScheme, got.URL.Scheme, tt.wantScheme)
			}
		})
	}
}
