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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func (s) TestJWTFileReader_ReadToken_FileErrors(t *testing.T) {
	tests := []struct {
		name     string
		create   bool
		contents string
		wantErr  error
	}{
		{
			name:     "nonexistent_file",
			create:   false,
			contents: "",
			wantErr:  errTokenFileAccess,
		},
		{
			name:     "empty_file",
			create:   true,
			contents: "",
			wantErr:  errJWTValidation,
		},
		{
			name:     "file_with_whitespace_only",
			create:   true,
			contents: "   \n\t  ",
			wantErr:  errJWTValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tokenFile string
			if !tt.create {
				tokenFile = "/does-not-exist"
			} else {
				tokenFile = writeTempFile(t, "token", tt.contents)
			}

			reader := jwtFileReader{tokenFilePath: tokenFile}
			if _, _, err := reader.readToken(); err == nil {
				t.Fatal("ReadToken() expected error, got nil")
			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ReadToken() error = %v, want error %v", err, tt.wantErr)
			}
		})
	}
}

func (s) TestJWTFileReader_ReadToken_InvalidJWT(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	tests := []struct {
		name         string
		tokenContent string
		wantErr      error
	}{
		{
			name:         "valid_token_without_expiration",
			tokenContent: createTestJWT(t, time.Time{}),
			wantErr:      errJWTValidation,
		},
		{
			name:         "expired_token",
			tokenContent: createTestJWT(t, now.Add(-time.Hour)),
			wantErr:      errJWTValidation,
		},
		{
			name:         "malformed_JWT_not_enough_parts",
			tokenContent: "invalid.jwt",
			wantErr:      errJWTValidation,
		},
		{
			name:         "malformed_JWT_invalid_base64",
			tokenContent: "header.invalid_base64!@#.signature",
			wantErr:      errJWTValidation,
		},
		{
			name:         "malformed_JWT_invalid_JSON",
			tokenContent: createInvalidJWT(t),
			wantErr:      errJWTValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenFile := writeTempFile(t, "token", tt.tokenContent)

			reader := jwtFileReader{tokenFilePath: tokenFile}
			if _, _, err := reader.readToken(); err == nil {
				t.Fatal("ReadToken() expected error, got nil")
			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ReadToken() error = %v, want error %v", err, tt.wantErr)
			}
		})
	}
}

func (s) TestJWTFileReader_ReadToken_ValidToken(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	tokenExp := now.Add(time.Hour)
	token := createTestJWT(t, tokenExp)
	tokenFile := writeTempFile(t, "token", token)

	reader := jwtFileReader{tokenFilePath: tokenFile}
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

// createInvalidJWT creates a JWT with invalid JSON in the payload.
func createInvalidJWT(t *testing.T) string {
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
