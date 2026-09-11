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
	"bytes"
	"context"
	"testing"
	"time"

	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/metadata"
)

// staticPerRPCCreds returns a fixed set of per-RPC credential headers.
type staticPerRPCCreds map[string]string

func (c staticPerRPCCreds) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return c, nil
}

func (staticPerRPCCreds) RequireTransportSecurity() bool { return false }

// setNeverIndexHeaders overrides the env-derived never-index set for the
// duration of the test. The set is parsed into a package variable at init
// time, so it has to be replaced directly rather than via the environment.
func setNeverIndexHeaders(t *testing.T, names ...string) {
	t.Helper()
	orig := envconfig.HPACKNeverIndexHeaders
	t.Cleanup(func() { envconfig.HPACKNeverIndexHeaders = orig })
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	envconfig.HPACKNeverIndexHeaders = set
}

// TestCreateHeaderFieldsNeverIndex verifies that headers named by
// GRPC_GO_EXPERIMENTAL_HPACK_NEVER_INDEX_HEADERS are marked sensitive across
// every source of outgoing headers - outgoing context metadata, metadata
// appended via AppendToOutgoingContext, transport-level metadata and per-RPC
// credentials - while headers outside the set stay indexable.
func (s) TestCreateHeaderFieldsNeverIndex(t *testing.T) {
	setNeverIndexHeaders(t, "x-request-id", "x-appended-id", "x-transport-id", "authorization")

	tr := &http2Client{
		scheme:      "https",
		userAgent:   "grpc-go/test",
		md:          metadata.MD{"x-transport-id": []string{"transport-value"}, "x-transport-stable": []string{"stable"}},
		perRPCCreds: []credentials.PerRPCCredentials{staticPerRPCCreds{"authorization": "Bearer token", "x-creds-stable": "stable"}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
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

	// wantSensitive maps a header name to whether it must be marked sensitive.
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
// sensitive when the environment variable is unset, i.e. that the feature does
// not change the wire format unless it is explicitly opted into.
func (s) TestCreateHeaderFieldsNeverIndexDefaultOff(t *testing.T) {
	setNeverIndexHeaders(t) // empty set, matching an unset env var

	tr := &http2Client{
		scheme:      "https",
		userAgent:   "grpc-go/test",
		md:          metadata.MD{},
		perRPCCreds: []credentials.PerRPCCredentials{staticPerRPCCreds{"authorization": "Bearer token"}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
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

// TestNeverIndexHPACKEncoding verifies the effect the Sensitive flag actually
// has on the wire: a never-indexed header is not added to the encoder's dynamic
// table, so encoding it a second time produces the same bytes rather than a
// short reference to a table entry, and it decodes back as sensitive.
func (s) TestNeverIndexHPACKEncoding(t *testing.T) {
	encodeTwice := func(f hpack.HeaderField) (first, second []byte) {
		var buf bytes.Buffer
		enc := hpack.NewEncoder(&buf)
		if err := enc.WriteField(f); err != nil {
			t.Fatalf("WriteField(%v) failed: %v", f, err)
		}
		first = bytes.Clone(buf.Bytes())
		buf.Reset()
		if err := enc.WriteField(f); err != nil {
			t.Fatalf("WriteField(%v) failed: %v", f, err)
		}
		return first, bytes.Clone(buf.Bytes())
	}

	const name, value = "x-request-id", "abc-123"

	indexedFirst, indexedSecond := encodeTwice(hpack.HeaderField{Name: name, Value: value})
	if len(indexedSecond) >= len(indexedFirst) {
		t.Errorf("indexed header: second encoding is %d bytes, want fewer than the first (%d); "+
			"the header should have been served from the dynamic table",
			len(indexedSecond), len(indexedFirst))
	}

	neverFirst, neverSecond := encodeTwice(hpack.HeaderField{Name: name, Value: value, Sensitive: true})
	if !bytes.Equal(neverFirst, neverSecond) {
		t.Errorf("never-indexed header: encodings differ (%v vs %v); want identical bytes, "+
			"as the header must not enter the dynamic table", neverFirst, neverSecond)
	}
	// A never-indexed literal costs the full header name on every request,
	// which is the bandwidth side of the trade-off documented on
	// envconfig.HPACKNeverIndexHeaders.
	if len(neverSecond) <= len(indexedSecond) {
		t.Errorf("never-indexed repeat encoding is %d bytes, indexed repeat is %d; "+
			"want never-indexed to be larger", len(neverSecond), len(indexedSecond))
	}

	var decoded []hpack.HeaderField
	dec := hpack.NewDecoder(4096, func(f hpack.HeaderField) { decoded = append(decoded, f) })
	if _, err := dec.Write(neverFirst); err != nil {
		t.Fatalf("Decoder.Write() failed: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("decoded %d header fields, want 1", len(decoded))
	}
	if !decoded[0].Sensitive {
		t.Errorf("decoded header %q is not sensitive; want the never-indexed representation", decoded[0].Name)
	}
}
