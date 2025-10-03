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

package jwtcreds

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"
)

const defaultCtxTimeout = 5 * time.Second

func TestNewCallCredentialsWithInValidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config string
	}{
		{
			name:   "empty_file",
			config: `""`,
		},
		{
			name:   "empty_config",
			config: `{}`,
		},
		{
			name: "empty_path",
			config: `{
				"jwt_token_file": ""
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCreds, cleanup, err := NewCallCredentials(json.RawMessage(tt.config))

			if err == nil {
				t.Fatal("NewCallCredentials: expected error, got nil")
			}
			if callCreds != nil {
				t.Error("NewCallCredentials: Expected nil bundle to be returned")
			}
			if cleanup != nil {
				t.Error("NewCallCredentials: Expected nil cleanup function to be returned")
			}
		})
	}
}
func TestNewCallCredentialsWithValidConfig(t *testing.T) {
	token := createTestJWT(t)
	tokenFile := writeTempFile(t, token)
	config := `{"jwt_token_file": "` + tokenFile + `"}`

	callCreds, cleanup, err := NewCallCredentials(json.RawMessage(config))
	if err != nil {
		t.Fatalf("NewCallCredentials failed: %v", err)
	}
	if callCreds == nil {
		t.Fatal("NewCallCredentials: Expected non-nil bundle to be returned")
	}
	if cleanup == nil {
		t.Error("NewCallCredentials: Expected non-nil cleanup function to be returned")
	} else {
		defer cleanup()
	}

	// Test that call credentials get used
	ctx, cancel := context.WithTimeout(context.Background(), defaultCtxTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &testAuthInfo{secLevel: credentials.PrivacyAndIntegrity},
	})
	metadata, err := callCreds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("GetRequestMetadata failed: %v", err)
	}
	if len(metadata) == 0 {
		t.Fatal("GetRequestMetadata: Expected metadata to be returned")
	}
	authHeader, ok := metadata["authorization"]
	if !ok {
		t.Fatal("GetRequestMetadata: Expected authorization header in metadata")
	}
	if !strings.HasPrefix(authHeader, "Bearer ") {
		t.Errorf("GetRequestMetadata: Authorization header should start with 'Bearer ', got %q", authHeader)
	}
}

func TestCallCredentials_Cleanup(t *testing.T) {
	token := createTestJWT(t)
	tokenFile := writeTempFile(t, token)
	config := `{"jwt_token_file": "` + tokenFile + `"}`
	_, cleanup, err := NewCallCredentials(json.RawMessage(config))
	if err != nil {
		t.Fatalf("NewCallCredentials failed: %v", err)
	}
	if cleanup == nil {
		t.Fatal("NewCallCredentials: Expected non-nil cleanup function")
	}
	// Cleanup should not panic
	cleanup()
	// Multiple cleanup calls should be safe
	cleanup()
}

// testAuthInfo implements credentials.AuthInfo for testing
type testAuthInfo struct {
	secLevel credentials.SecurityLevel
}

func (t *testAuthInfo) AuthType() string {
	return "test"
}

func (t *testAuthInfo) GetCommonAuthInfo() credentials.CommonAuthInfo {
	return credentials.CommonAuthInfo{SecurityLevel: t.secLevel}
}

// createTestJWT creates a test JWT token for testing
func createTestJWT(t *testing.T) string {
	t.Helper()

	// Header: {"typ":"JWT","alg":"HS256"}
	header := "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9"
	// Claims: {"aud":"https://example.com","exp":future_timestamp}
	claims := "eyJhdWQiOiJodHRwczovL2V4YW1wbGUuY29tIiwiZXhwIjoyMDAwMDAwMDAwfQ"
	signature := "fake_signature_for_testing"

	return header + "." + claims + "." + signature
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "tempfile")
	if err := os.WriteFile(filePath, []byte(content), 0600); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	return filePath
}
