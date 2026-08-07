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

	_ "google.golang.org/grpc/internal/resolver/dns" // Register the default (dns) resolver for fallback tests.
)

// testResolverBuilder is a minimal resolver.Builder used only to register
// schemes for ValidateTargetURI tests.
type testResolverBuilder struct{ scheme string }

func (b *testResolverBuilder) Build(resolver.Target, resolver.ClientConn, resolver.BuildOptions) (resolver.Resolver, error) {
	return nil, nil
}

func (b *testResolverBuilder) Scheme() string { return b.scheme }

func init() {
	resolver.Register(&testResolverBuilder{scheme: "iresolver-test"})
}

func TestValidateTargetURI(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{name: "registered scheme with authority and endpoint", target: "iresolver-test:///endpoint", wantErr: false},
		// url.Parse canonicalizes the scheme to lowercase (RFC 3986 3.1),
		// so an uppercase scheme still matches the registered lowercase one.
		{name: "registered scheme uppercase input", target: "IRESOLVER-TEST:///endpoint", wantErr: false},
		// Opaque (host:port) forms fall back to the default scheme, as
		// grpc.NewClient does.
		{name: "host:port without scheme", target: "my-service:50051", wantErr: false},
		{name: "host:port with dotted host", target: "trafficdirector.googleapis.com:443", wantErr: false},
		{name: "ip:port without scheme", target: "127.0.0.1:443", wantErr: false},
		{name: "registered scheme opaque form", target: "iresolver-test:endpoint", wantErr: false},
		// A string that does not parse as a URI is accepted if it parses
		// after the default-scheme fallback, matching grpc.NewClient.
		{name: "unparseable URI accepted via fallback", target: "://bad", wantErr: false},
		// Parses with an empty scheme (not opaque), so the default scheme is
		// applied directly.
		{name: "absolute path without scheme", target: "/var/run/foo.sock", wantErr: false},
		// An invalid percent-escape fails to parse both as-is and after the
		// default-scheme fallback.
		{name: "invalid percent-escape", target: "%zz", wantErr: true},
		{name: "empty target", target: "", wantErr: true},
		// Authority-form URIs with an unregistered scheme are rejected, so
		// that scheme typos in configuration surface as errors.
		{name: "unregistered scheme", target: "no-such-scheme:///endpoint", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
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
	resolver.SetDefaultScheme("iresolver-test")
	defer func() {
		// Reset the default scheme as though it was never set by the user.
		resolver.SetDefaultScheme("passthrough")
		internal.UserSetDefaultScheme = false
	}()
	if err := ValidateTargetURI("my-service:50051"); err != nil {
		t.Fatalf("ValidateTargetURI(%q) with user-set default scheme = %v, want nil", "my-service:50051", err)
	}
}
