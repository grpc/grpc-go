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

// Characterization tests for jwtAccess.GetRequestMetadata, written to lock the
// observable contract of the current (pre-cache) implementation. These tests
// MUST pass both before and after the sync.Map token-cache refactor. If a test
// here would only pass after the refactor, it does not belong here — it is a
// behavior change, not a characterization.

package oauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc/credentials"
)

// --- shared fixtures ----------------------------------------------------

// newTestJSONKey generates a fresh service-account JSON blob with an embedded
// 2048-bit RSA private key. We need a real key because
// google.JWTAccessTokenSourceFromJSON does ParseKey on it.
func newTestJSONKey(t testing.TB) []byte {
	t.Helper()
	pk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(pk)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	sa := map[string]string{
		"type":           "service_account",
		"project_id":     "safe-refactor",
		"private_key_id": "kid-safe",
		"private_key":    string(pemBytes),
		"client_email":   "safe@safe.iam.gserviceaccount.com",
		"client_id":      "1",
		"token_uri":      "https://oauth2.googleapis.com/token",
	}
	out, err := json.Marshal(sa)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return out
}

// secureAuthInfo reports PrivacyAndIntegrity so CheckSecurityLevel succeeds.
type secureAuthInfo struct{}

func (secureAuthInfo) AuthType() string { return "safe-refactor-secure" }
func (secureAuthInfo) GetCommonAuthInfo() credentials.CommonAuthInfo {
	return credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}
}

// insecureAuthInfo reports IntegrityOnly (below the required PrivacyAndIntegrity).
type insecureAuthInfo struct{}

func (insecureAuthInfo) AuthType() string { return "safe-refactor-insecure" }
func (insecureAuthInfo) GetCommonAuthInfo() credentials.CommonAuthInfo {
	return credentials.CommonAuthInfo{SecurityLevel: credentials.IntegrityOnly}
}

func secureCtx() context.Context {
	return credentials.NewContextWithRequestInfo(context.Background(),
		credentials.RequestInfo{AuthInfo: secureAuthInfo{}})
}

// extractJWTAudClaim parses a "Bearer <jwt>" authorization header value and
// returns the JWT's `aud` claim. It does NOT verify the signature — we only
// care that the in-band audience claim matches what GetRequestMetadata was
// asked for.
func extractJWTAudClaim(t testing.TB, authzHeader string) string {
	t.Helper()
	const prefix = "Bearer "
	if !strings.HasPrefix(authzHeader, prefix) {
		t.Fatalf("authorization header missing %q prefix: %q", prefix, authzHeader)
	}
	jwt := strings.TrimPrefix(authzHeader, prefix)
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed JWT: expected 3 segments, got %d", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}
	var claims struct {
		Aud string `json:"aud"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal JWT claims: %v", err)
	}
	return claims.Aud
}

// --- 1. Interface contract ----------------------------------------------

func TestJWTAccessSafety_InterfaceContract(t *testing.T) {
	creds, err := NewJWTAccessFromKey(newTestJSONKey(t))
	if err != nil {
		t.Fatalf("NewJWTAccessFromKey: %v", err)
	}
	var _ credentials.PerRPCCredentials = creds // compile-time assertion
	if !creds.RequireTransportSecurity() {
		t.Error("RequireTransportSecurity() = false, want true")
	}
}

// --- 2. Metadata shape --------------------------------------------------

func TestJWTAccessSafety_MetadataShape(t *testing.T) {
	creds, err := NewJWTAccessFromKey(newTestJSONKey(t))
	if err != nil {
		t.Fatalf("NewJWTAccessFromKey: %v", err)
	}
	md, err := creds.GetRequestMetadata(secureCtx(),
		"https://pubsub.googleapis.com/google.pubsub.v1.Publisher/Publish")
	if err != nil {
		t.Fatalf("GetRequestMetadata: %v", err)
	}
	if got, want := len(md), 1; got != want {
		t.Errorf("metadata key count = %d, want %d (keys=%v)", got, want, md)
	}
	v, ok := md["authorization"]
	if !ok {
		t.Fatalf(`metadata missing "authorization" key: %v`, md)
	}
	if !strings.HasPrefix(v, "Bearer ") {
		t.Errorf(`authorization value = %q, want prefix "Bearer "`, v)
	}
}

// --- 3. Audience claim matches the URI (no cross-talk) ------------------

func TestJWTAccessSafety_AudienceClaimMatchesURI(t *testing.T) {
	creds, err := NewJWTAccessFromKey(newTestJSONKey(t))
	if err != nil {
		t.Fatalf("NewJWTAccessFromKey: %v", err)
	}
	cases := []struct {
		uri    string
		wantAd string // audience is host-level: removeServiceNameFromJWTURI sets path="/"
	}{
		{"https://pubsub.googleapis.com/google.pubsub.v1.Publisher/Publish", "https://pubsub.googleapis.com/"},
		{"https://spanner.googleapis.com/google.spanner.v1.Spanner/CreateSession", "https://spanner.googleapis.com/"},
	}
	for _, c := range cases {
		md, err := creds.GetRequestMetadata(secureCtx(), c.uri)
		if err != nil {
			t.Fatalf("GetRequestMetadata(%q): %v", c.uri, err)
		}
		if got := extractJWTAudClaim(t, md["authorization"]); got != c.wantAd {
			t.Errorf("aud claim for uri=%q = %q, want %q", c.uri, got, c.wantAd)
		}
	}
}

// --- 4. Same URI yields stable aud claim across calls -------------------

func TestJWTAccessSafety_SameURIStableAudClaim(t *testing.T) {
	creds, err := NewJWTAccessFromKey(newTestJSONKey(t))
	if err != nil {
		t.Fatalf("NewJWTAccessFromKey: %v", err)
	}
	const uri = "https://pubsub.googleapis.com/google.pubsub.v1.Publisher/Publish"
	const wantAud = "https://pubsub.googleapis.com/"
	for i := 0; i < 3; i++ {
		md, err := creds.GetRequestMetadata(secureCtx(), uri)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if got := extractJWTAudClaim(t, md["authorization"]); got != wantAud {
			t.Errorf("call %d: aud = %q, want %q", i, got, wantAud)
		}
	}
}

// --- 5. Invalid URI returns error, no metadata --------------------------

func TestJWTAccessSafety_InvalidURIError(t *testing.T) {
	creds, err := NewJWTAccessFromKey(newTestJSONKey(t))
	if err != nil {
		t.Fatalf("NewJWTAccessFromKey: %v", err)
	}
	// "ht tp://foo.com" — same shape as existing oauth_test.go exercises in
	// TestRemoveServiceNameFromJWTURI: literal space makes url.Parse fail.
	md, err := creds.GetRequestMetadata(secureCtx(), "ht tp://foo.com")
	if err == nil {
		t.Fatalf("GetRequestMetadata(invalid URI) returned nil error, md=%v", md)
	}
	if md != nil {
		t.Errorf("metadata on error = %v, want nil", md)
	}
}

// --- 6. Insecure context returns error, no metadata ---------------------

func TestJWTAccessSafety_InsecureContextError(t *testing.T) {
	creds, err := NewJWTAccessFromKey(newTestJSONKey(t))
	if err != nil {
		t.Fatalf("NewJWTAccessFromKey: %v", err)
	}
	cases := []struct {
		name string
		ctx  context.Context
	}{
		{
			name: "nil AuthInfo (no RequestInfo)",
			ctx:  context.Background(),
		},
		{
			name: "IntegrityOnly AuthInfo",
			ctx: credentials.NewContextWithRequestInfo(context.Background(),
				credentials.RequestInfo{AuthInfo: insecureAuthInfo{}}),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			md, err := creds.GetRequestMetadata(c.ctx,
				"https://pubsub.googleapis.com/google.pubsub.v1.Publisher/Publish")
			if err == nil {
				t.Fatalf("GetRequestMetadata returned nil error, md=%v", md)
			}
			if md != nil {
				t.Errorf("metadata on error = %v, want nil", md)
			}
		})
	}
}

// --- 7. Concurrent calls: no race, no data corruption ------------------

func TestJWTAccessSafety_ConcurrentNoDataRace(t *testing.T) {
	creds, err := NewJWTAccessFromKey(newTestJSONKey(t))
	if err != nil {
		t.Fatalf("NewJWTAccessFromKey: %v", err)
	}
	// A handful of hosts so the cache (post-fix) sees both cold-fill and
	// hot-hit paths concurrently. Pre-fix, every goroutine re-signs — still
	// must not race.
	uris := []string{
		"https://pubsub.googleapis.com/google.pubsub.v1.Publisher/Publish",
		"https://spanner.googleapis.com/google.spanner.v1.Spanner/CreateSession",
		"https://bigtable.googleapis.com/google.bigtable.v2.Bigtable/ReadRows",
		"https://storage.googleapis.com/google.storage.v1.Storage/GetObject",
	}
	wantAuds := map[string]string{
		uris[0]: "https://pubsub.googleapis.com/",
		uris[1]: "https://spanner.googleapis.com/",
		uris[2]: "https://bigtable.googleapis.com/",
		uris[3]: "https://storage.googleapis.com/",
	}

	const goroutines = 32
	const iterations = 25 // ~800 total calls; enough for race detector to see interleavings

	var wg sync.WaitGroup
	var failures atomic.Int64
	var firstErr atomic.Value // error

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				uri := uris[(g+i)%len(uris)]
				md, err := creds.GetRequestMetadata(secureCtx(), uri)
				if err != nil {
					failures.Add(1)
					firstErr.CompareAndSwap(nil, fmt.Errorf("g=%d i=%d uri=%s: %v", g, i, uri, err))
					return
				}
				got := extractJWTAudClaim(t, md["authorization"])
				if want := wantAuds[uri]; got != want {
					failures.Add(1)
					firstErr.CompareAndSwap(nil, fmt.Errorf("g=%d i=%d uri=%s: aud=%q want %q", g, i, uri, got, want))
					return
				}
			}
		}(g)
	}
	wg.Wait()

	if n := failures.Load(); n > 0 {
		err, _ := firstErr.Load().(error)
		if err == nil {
			err = errors.New("unknown")
		}
		t.Fatalf("concurrent calls: %d failures, first: %v", n, err)
	}
}
