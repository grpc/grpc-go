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

package transport

import (
	"context"
	"testing"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// staticPerRPCCreds returns a fixed set of per-RPC credential headers.
type staticPerRPCCreds map[string]string

func (c staticPerRPCCreds) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return c, nil
}

func (staticPerRPCCreds) RequireTransportSecurity() bool { return false }

// TestCreateHeaderFieldsNeverIndex verifies that configured keys are marked
// sensitive for every source of outgoing headers, and other headers are not.
func (s) TestCreateHeaderFieldsNeverIndex(t *testing.T) {
	tr := &http2Client{
		scheme:      "https",
		userAgent:   "grpc-go/test",
		md:          metadata.MD{"x-transport-id": []string{"transport-value"}, "x-transport-stable": []string{"stable"}},
		perRPCCreds: []credentials.PerRPCCredentials{staticPerRPCCreds{"authorization": "Bearer token", "x-creds-stable": "stable"}},
		hpackNeverIndexKeys: map[string]struct{}{
			"x-request-id":   {},
			"x-appended-id":  {},
			"x-transport-id": {},
			"authorization":  {},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.MD{
		"x-request-id":  []string{"abc-123"},
		"x-user-header": []string{"stable-value"},
	})
	ctx = metadata.AppendToOutgoingContext(ctx, "x-appended-id", "def-456", "x-appended-stable", "stable")
	callHdr := &CallHdr{
		Method: "/grpc.testing.TestService/UnaryCall",
		Host:   "server:443",
	}

	hf, err := tr.createHeaderFields(ctx, callHdr)
	if err != nil {
		t.Fatalf("createHeaderFields() failed: %v", err)
	}

	wantSensitive := map[string]bool{
		"x-request-id":       true,  // outgoing context metadata, in the set
		"x-appended-id":      true,  // AppendToOutgoingContext, in the set
		"x-transport-id":     true,  // transport metadata, in the set
		"authorization":      true,  // per-RPC credentials, in the set
		"x-user-header":      false, // outgoing context metadata, not in the set
		"x-appended-stable":  false, // AppendToOutgoingContext, not in the set
		"x-transport-stable": false, // transport metadata, not in the set
		"x-creds-stable":     false, // per-RPC credentials, not in the set
		"content-type":       false, // reserved header, never in the set
	}

	seen := map[string]bool{}
	for _, f := range hf {
		want, ok := wantSensitive[f.Name]
		if !ok {
			continue
		}
		seen[f.Name] = true
		if f.Sensitive != want {
			t.Errorf("header %q: Sensitive = %v, want %v", f.Name, f.Sensitive, want)
		}
	}
	for name := range wantSensitive {
		if !seen[name] {
			t.Errorf("expected header %q was not produced", name)
		}
	}
}

// TestCreateHeaderFieldsNeverIndexDefaultOff verifies that no header is marked
// sensitive when no keys are configured.
func (s) TestCreateHeaderFieldsNeverIndexDefaultOff(t *testing.T) {
	tr := &http2Client{
		scheme:      "https",
		userAgent:   "grpc-go/test",
		md:          metadata.MD{},
		perRPCCreds: []credentials.PerRPCCredentials{staticPerRPCCreds{"authorization": "Bearer token"}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.MD{"grpc-trace-bin": []string{"\x00spanid"}})
	ctx = metadata.AppendToOutgoingContext(ctx, "traceparent", "00-abc-def-01")

	hf, err := tr.createHeaderFields(ctx, &CallHdr{Method: "/grpc.testing.TestService/UnaryCall", Host: "server:443"})
	if err != nil {
		t.Fatalf("createHeaderFields() failed: %v", err)
	}
	for _, f := range hf {
		if f.Sensitive {
			t.Errorf("header %q is marked sensitive; want no sensitive headers by default", f.Name)
		}
	}
}
