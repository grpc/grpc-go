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
)

func TestParseTarget(t *testing.T) {
	// Register a test resolver for custom scheme
	Register(&testResolverBuilder{scheme: "dns"})
	Register(&testResolverBuilder{scheme: "passthrough"})

	tests := []struct {
		name          string
		target        string
		defaultScheme string
		wantScheme    string
		wantEndpoint  string
		wantErr       bool
		errContain    string
	}{
		{
			name:          "valid dns scheme",
			target:        "dns:///example.com:443",
			defaultScheme: "",
			wantScheme:    "dns",
			wantEndpoint:  "example.com:443",
			wantErr:       false,
		},
		{
			name:          "valid passthrough scheme",
			target:        "passthrough:///localhost:8080",
			defaultScheme: "",
			wantScheme:    "passthrough",
			wantEndpoint:  "localhost:8080",
			wantErr:       false,
		},
		{
			name:          "valid dns scheme with default",
			target:        "dns:///example.com:443",
			defaultScheme: "dns",
			wantScheme:    "dns",
			wantEndpoint:  "example.com:443",
			wantErr:       false,
		},
		{
			name:          "missing scheme with default",
			target:        "/path/to/socket",
			defaultScheme: "passthrough",
			wantScheme:    "passthrough",
			wantEndpoint:  "/path/to/socket",
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
			name:          "host:port with no default errors",
			target:        "localhost:8080",
			defaultScheme: "",
			wantErr:       true,
			errContain:    "no resolver registered for scheme",
		},
		{
			name:          "host:port with default succeeds",
			target:        "localhost:8080",
			defaultScheme: "dns",
			wantScheme:    "dns",
			wantEndpoint:  "localhost:8080",
			wantErr:       false,
		},
		{
			name:          "unregistered scheme without default",
			target:        "unknown:///example.com:443",
			defaultScheme: "",
			wantErr:       true,
			errContain:    "no resolver registered for scheme",
		},
		{
			name:          "unregistered scheme with default",
			target:        "unknown:///example.com:443",
			defaultScheme: "dns",
			wantScheme:    "dns",
			wantEndpoint:  "unknown:///example.com:443",
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
		{
			name:          "endpoint with path",
			target:        "dns:///service/path",
			defaultScheme: "",
			wantScheme:    "dns",
			wantEndpoint:  "service/path",
			wantErr:       false,
		},
		{
			name:          "endpoint with authority and path",
			target:        "dns://authority/service:8080",
			defaultScheme: "",
			wantScheme:    "dns",
			wantEndpoint:  "service:8080",
			wantErr:       false,
		},
		{
			name:          "scheme with three slashes and no endpoint",
			target:        "dns:///",
			defaultScheme: "",
			wantScheme:    "dns",
			wantEndpoint:  "",
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, err := ParseTarget(tt.target, tt.defaultScheme)
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
			if target.URL.Scheme != tt.wantScheme {
				t.Errorf("ParseTarget(%q, %q).URL.Scheme = %q, want %q", tt.target, tt.defaultScheme, target.URL.Scheme, tt.wantScheme)
			}
			if tt.wantEndpoint != "" && target.Endpoint() != tt.wantEndpoint {
				t.Errorf("ParseTarget(%q, %q).Endpoint() = %q, want %q", tt.target, tt.defaultScheme, target.Endpoint(), tt.wantEndpoint)
			}
		})
	}
}

func TestParseTargetWithRegistry(t *testing.T) {
	// Mock custom resolver builder
	customBuilder := &testResolverBuilder{scheme: "custom"}

	// Custom registry that knows about both standard and custom schemes
	customRegistry := func(scheme string) Builder {
		switch scheme {
		case "custom":
			return customBuilder
		case "dns":
			return Get("dns") // Fallback to global registry
		default:
			return nil
		}
	}

	tests := []struct {
		name          string
		target        string
		defaultScheme string
		registry      func(string) Builder
		wantScheme    string
		wantEndpoint  string
		wantErr       bool
		errContain    string
	}{
		{
			name:          "custom scheme with custom registry",
			target:        "custom:///service:8080",
			defaultScheme: "",
			registry:      customRegistry,
			wantScheme:    "custom",
			wantEndpoint:  "service:8080",
			wantErr:       false,
		},
		{
			name:          "dns scheme with custom registry fallback",
			target:        "dns:///example.com:443",
			defaultScheme: "",
			registry:      customRegistry,
			wantScheme:    "dns",
			wantEndpoint:  "example.com:443",
			wantErr:       false,
		},
		{
			name:          "unregistered scheme with custom registry",
			target:        "unknown:///service:8080",
			defaultScheme: "",
			registry:      customRegistry,
			wantErr:       true,
			errContain:    "no resolver registered for scheme",
		},
		{
			name:          "custom scheme with default scheme",
			target:        "service:8080",
			defaultScheme: "custom",
			registry:      customRegistry,
			wantScheme:    "custom",
			wantEndpoint:  "service:8080",
			wantErr:       false,
		},
		{
			name:          "global registry Get function",
			target:        "dns:///example.com:443",
			defaultScheme: "",
			registry:      Get,
			wantScheme:    "dns",
			wantEndpoint:  "example.com:443",
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, err := ParseTargetWithRegistry(tt.target, tt.defaultScheme, tt.registry)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTargetWithRegistry(%q, %q) error = %v, wantErr %v", tt.target, tt.defaultScheme, err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("ParseTargetWithRegistry(%q, %q) error = %v, want error containing %q", tt.target, tt.defaultScheme, err, tt.errContain)
				}
				return
			}
			if target.URL.Scheme != tt.wantScheme {
				t.Errorf("ParseTargetWithRegistry(%q, %q).URL.Scheme = %q, want %q", tt.target, tt.defaultScheme, target.URL.Scheme, tt.wantScheme)
			}
			if tt.wantEndpoint != "" && target.Endpoint() != tt.wantEndpoint {
				t.Errorf("ParseTargetWithRegistry(%q, %q).Endpoint() = %q, want %q", tt.target, tt.defaultScheme, target.Endpoint(), tt.wantEndpoint)
			}
		})
	}
}

// testResolverBuilder is a mock resolver builder for testing
type testResolverBuilder struct {
	scheme string
}

func (b *testResolverBuilder) Build(target Target, cc ClientConn, opts BuildOptions) (Resolver, error) {
	return &testResolver{}, nil
}

func (b *testResolverBuilder) Scheme() string {
	return b.scheme
}

// testResolver is a mock resolver for testing
type testResolver struct{}

func (r *testResolver) ResolveNow(ResolveNowOptions) {}

func (r *testResolver) Close() {}
