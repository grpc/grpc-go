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

	"google.golang.org/grpc/resolver"
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
		{name: "registered scheme opaque form", target: "iresolver-test:endpoint", wantErr: false},
		// url.Parse canonicalizes the scheme to lowercase (RFC 3986 3.1),
		// so an uppercase scheme still matches the registered lowercase one.
		{name: "registered scheme uppercase input", target: "IRESOLVER-TEST:///endpoint", wantErr: false},
		{name: "empty target", target: "", wantErr: true},
		{name: "host:port without scheme", target: "my-service:50051", wantErr: true},
		{name: "unregistered scheme", target: "no-such-scheme:///endpoint", wantErr: true},
		{name: "unparseable URI", target: "://bad", wantErr: true},
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
