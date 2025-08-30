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

package jwt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/internal/grpctest"
)

func TestJWTFileReader(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestJWTFileReader_ReadToken_FileErrors(t *testing.T) {
	tests := []struct {
		name            string
		create          bool
		contents        string
		wantErrContains string
	}{
		{
			name:            "nonexistent file",
			create:          false,
			contents:        "",
			wantErrContains: "failed to read token file",
		},
		{
			name:            "empty file",
			create:          true,
			contents:        "",
			wantErrContains: "token file",
		},
		{
			name:            "file with whitespace only",
			create:          true,
			contents:        "   \n\t  ",
			wantErrContains: "token file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tokenFile string
			if !tt.create {
				tokenFile = "/does-not-exixt"
			} else {
				tokenFile = writeTempFile(t, "token", tt.contents)
			}

			reader := jWTFileReader{tokenFilePath: tokenFile}
			_, _, err := reader.readToken()
			if err == nil {
				t.Fatal("ReadToken() expected error, got nil")
			}

			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("ReadToken() error = %v, want error containing %q", err, tt.wantErrContains)
			}
		})
	}
}

func (s) TestJWTFileReader_ReadToken_InvalidJWT(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	tests := []struct {
		name            string
		tokenContent    string
		wantErrContains string
	}{
		{
			name:            "valid token without expiration",
			tokenContent:    createTestJWT(t, time.Time{}),
			wantErrContains: "JWT token has no expiration claim",
		},
		{
			name:            "expired token",
			tokenContent:    createTestJWT(t, now.Add(-time.Hour)),
			wantErrContains: "JWT token is expired",
		},
		{
			name:            "malformed JWT - not enough parts",
			tokenContent:    "invalid.jwt",
			wantErrContains: "invalid JWT format: expected 3 parts, got 2",
		},
		{
			name:            "malformed JWT - invalid base64",
			tokenContent:    "header.invalid_base64!@#.signature",
			wantErrContains: "failed to decode JWT payload",
		},
		{
			name:            "malformed JWT - invalid JSON",
			tokenContent:    createInvalidJSONJWT(t),
			wantErrContains: "failed to unmarshal JWT claims",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenFile := writeTempFile(t, "token", tt.tokenContent)

			reader := jWTFileReader{tokenFilePath: tokenFile}
			if _, _, err := reader.readToken(); err == nil {
				t.Fatal("ReadToken() expected error, got nil")
			} else if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("ReadToken() error = %v, want error containing %q", err, tt.wantErrContains)
			}
		})
	}
}

func (s) TestJWTFileReader_ReadToken_ValidToken(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	tokenExp := now.Add(time.Hour)
	token := createTestJWT(t, tokenExp)
	tokenFile := writeTempFile(t, "token", token)

	reader := jWTFileReader{tokenFilePath: tokenFile}
	readToken, expiry, err := reader.readToken()
	if err != nil {
		t.Fatalf("ReadToken() unexpected error: %v", err)
	}

	if readToken != token {
		t.Errorf("ReadToken() token = %q, want %q", readToken, token)
	}

	if !expiry.Equal(tokenExp) {
		t.Errorf("ReadToken() expiry = %v, want %v", expiry, tokenExp)
	}
}

// createInvalidJSONJWT creates a JWT with invalid JSON in the payload.
func createInvalidJSONJWT(t *testing.T) string {
	t.Helper()

	header := map[string]any{
		"typ": "JWT",
		"alg": "HS256",
	}

	headerBytes, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("Failed to marshal header: %v", err)
	}

	headerB64 := base64.URLEncoding.EncodeToString(headerBytes)
	headerB64 = strings.TrimRight(headerB64, "=")

	// Create invalid JSON payload
	invalidJSON := "invalid json content"
	payloadB64 := base64.URLEncoding.EncodeToString([]byte(invalidJSON))
	payloadB64 = strings.TrimRight(payloadB64, "=")

	signature := base64.URLEncoding.EncodeToString([]byte("fake_signature"))
	signature = strings.TrimRight(signature, "=")

	return fmt.Sprintf("%s.%s.%s", headerB64, payloadB64, signature)
}
