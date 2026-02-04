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

package grpcutil

import (
	"strings"
	"testing"

	_ "google.golang.org/grpc/resolver/dns"         // Register dns resolver
	_ "google.golang.org/grpc/resolver/passthrough" // Register passthrough resolver
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
			name:          "valid dns scheme",
			target:        "dns:///example.com:443",
			defaultScheme: "",
			wantScheme:    "dns",
			wantErr:       false,
		},
		{
			name:          "valid passthrough scheme",
			target:        "passthrough:///localhost:8080",
			defaultScheme: "",
			wantScheme:    "passthrough",
			wantErr:       false,
		},
		{
			name:          "missing scheme with default",
			target:        "/path/to/socket",
			defaultScheme: "unix",
			wantScheme:    "unix",
			wantErr:       false,
		},
		{
			name:          "missing scheme without default",
			target:        "/path/to/socket",
			defaultScheme: "",
			wantErr:       true,
			errContain:    "has no scheme",
		},
		{
			name:          "host:port parsed as scheme",
			target:        "localhost:8080",
			defaultScheme: "dns",
			wantScheme:    "localhost",
			wantErr:       false,
		},
		{
			name:          "invalid URI without default",
			target:        "dns:///example\x00.com",
			defaultScheme: "",
			wantErr:       true,
			errContain:    "invalid target URI",
		},
		{
			name:          "invalid URI with default still fails",
			target:        "dns:///example\x00.com",
			defaultScheme: "dns",
			wantErr:       true,
			errContain:    "invalid target URI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := ParseTarget(tt.target, tt.defaultScheme)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTarget(%q, %q) error = %v, wantErr %v", tt.target, tt.defaultScheme, err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("ParseTarget(%q, %q) error = %v, want error containing %q", tt.target, tt.defaultScheme, err, tt.errContain)
				}
				return
			}
			if u.Scheme != tt.wantScheme {
				t.Errorf("ParseTarget(%q, %q).Scheme = %q, want %q", tt.target, tt.defaultScheme, u.Scheme, tt.wantScheme)
			}
		})
	}
}

func TestValidateTargetURI(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		wantErr    bool
		errContain string
	}{
		{
			name:    "valid dns scheme",
			target:  "dns:///example.com:443",
			wantErr: false,
		},
		{
			name:    "valid passthrough scheme",
			target:  "passthrough:///localhost:8080",
			wantErr: false,
		},
		{
			name:       "missing scheme",
			target:     "/path/to/socket",
			wantErr:    true,
			errContain: "has no scheme",
		},
		{
			name:       "host:port parsed as scheme",
			target:     "example.com:443",
			wantErr:    true,
			errContain: "no resolver registered for scheme",
		},
		{
			name:       "unregistered scheme",
			target:     "unknown:///example.com:443",
			wantErr:    true,
			errContain: "no resolver registered for scheme",
		},
		{
			name:       "invalid URI with control character",
			target:     "dns:///example\x00.com",
			wantErr:    true,
			errContain: "invalid target URI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTargetURI(tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTargetURI(%q) error = %v, wantErr %v", tt.target, err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContain != "" {
				if !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("ValidateTargetURI(%q) error = %v, want error containing %q", tt.target, err, tt.errContain)
				}
			}
		})
	}
}
