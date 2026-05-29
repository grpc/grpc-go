/*
 *
 * Copyright 2026 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package oauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"testing"

	"google.golang.org/grpc/credentials"
)

// genServiceAccountJSON builds a self-contained service-account key JSON with a
// fresh 2048-bit RSA private key. Mirrors the schema google.JWTConfigFromJSON
// expects.
func genServiceAccountJSON(b *testing.B) []byte {
	b.Helper()
	pk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		b.Fatalf("rsa.GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(pk)
	if err != nil {
		b.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	sa := map[string]string{
		"type":           "service_account",
		"project_id":     "bench",
		"private_key_id": "kid-bench",
		"private_key":    string(pemBytes),
		"client_email":   "bench@bench.iam.gserviceaccount.com",
		"client_id":      "1",
		"token_uri":      "https://oauth2.googleapis.com/token",
	}
	out, err := json.Marshal(sa)
	if err != nil {
		b.Fatalf("json.Marshal: %v", err)
	}
	return out
}

// fakeAuthInfo reports PrivacyAndIntegrity so CheckSecurityLevel succeeds in
// the benchmark; returning ALTSAuthInfo would require importing alts.
type fakeAuthInfo struct{}

func (fakeAuthInfo) AuthType() string { return "fake" }
func (fakeAuthInfo) GetCommonAuthInfo() credentials.CommonAuthInfo {
	return credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}
}

func benchCtx() context.Context {
	return credentials.NewContextWithRequestInfo(context.Background(),
		credentials.RequestInfo{AuthInfo: fakeAuthInfo{}})
}

// BenchmarkJWTGetRequestMetadata measures the hot-path cost of building a
// per-RPC authorization header for jwtAccess. Pre-cache this required a JSON
// parse + RSA private key parse + RS256 signing every call (~1ms, ~120 allocs);
// the per-audience TokenSource cache reduces it to a map lookup (~340ns, 5 allocs).
func BenchmarkJWTGetRequestMetadata(b *testing.B) {
	creds, err := NewJWTAccessFromKey(genServiceAccountJSON(b))
	if err != nil {
		b.Fatal(err)
	}
	ctx := benchCtx()
	uri := "https://pubsub.googleapis.com/google.pubsub.v1.Publisher/Publish"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := creds.GetRequestMetadata(ctx, uri); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkJWTGetRequestMetadataParallel exercises contention behavior at QPS
// scale, since a real client has many concurrent in-flight RPCs sharing one
// jwtAccess instance.
func BenchmarkJWTGetRequestMetadataParallel(b *testing.B) {
	creds, err := NewJWTAccessFromKey(genServiceAccountJSON(b))
	if err != nil {
		b.Fatal(err)
	}
	ctx := benchCtx()
	uri := "https://pubsub.googleapis.com/google.pubsub.v1.Publisher/Publish"

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := creds.GetRequestMetadata(ctx, uri); err != nil {
				b.Fatal(err)
			}
		}
	})
}
