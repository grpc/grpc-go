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

func TestNewBundle(t *testing.T) {
	token := createTestJWT(t)
	tokenFile := writeTempFile(t, token)

	tests := []struct {
		name            string
		config          string
		wantErr         bool
		wantErrContains string
	}{
		{
			name: "valid RFC A97 config with jwt_token_file",
			config: `{
				"jwt_token_file": "` + tokenFile + `"
			}`,
			wantErr: false,
		},
		{
			name:            "empty config",
			config:          `""`,
			wantErr:         true,
			wantErrContains: "unmarshal",
		},
		{
			name:            "empty config",
			config:          `{}`,
			wantErr:         true,
			wantErrContains: "jwt_token_file is required",
		},
		{
			name: "empty path",
			config: `{
				"jwt_token_file": ""
			}`,
			wantErr:         true,
			wantErrContains: "jwt_token_file is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle, cleanup, err := NewBundle(json.RawMessage(tt.config))

			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("Error %v should contain %q", err, tt.wantErrContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if bundle == nil {
				t.Fatal("Expected non-nil bundle")
			}

			if cleanup == nil {
				t.Error("Expected non-nil cleanup function")
			} else {
				defer cleanup()
			}

			// JWT bundle only deals with PerRPCCredentials, not TransportCredentials
			if bundle.TransportCredentials() != nil {
				t.Error("Expected nil transport credentials for JWT call creds bundle")
			}

			if bundle.PerRPCCredentials() == nil {
				t.Error("Expected non-nil per-RPC credentials for valid JWT config")
			}

			// Test that call credentials work
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
				AuthInfo: &testAuthInfo{secLevel: credentials.PrivacyAndIntegrity},
			})

			metadata, err := bundle.PerRPCCredentials().GetRequestMetadata(ctx)
			if err != nil {
				t.Fatalf("GetRequestMetadata failed: %v", err)
			}

			if len(metadata) == 0 {
				t.Error("Expected metadata to be returned")
			}

			authHeader, ok := metadata["authorization"]
			if !ok {
				t.Error("Expected authorization header in metadata")
			}

			if !strings.HasPrefix(authHeader, "Bearer ") {
				t.Errorf("Authorization header should start with 'Bearer ', got %q", authHeader)
			}
		})
	}
}

func TestBundle_NewWithMode(t *testing.T) {
	token := createTestJWT(t)
	tokenFile := writeTempFile(t, token)
	config := `{"jwt_token_file": "` + tokenFile + `"}`
	bundle, cleanup, err := NewBundle(json.RawMessage(config))
	if err != nil {
		t.Fatalf("NewBundle failed: %v", err)
	}
	defer cleanup()

	_, err = bundle.NewWithMode("test_mode")
	if err == nil {
		t.Error("Expected error from NewWithMode, got nil")
	}
	if !strings.Contains(err.Error(), "does not support mode switching") {
		t.Errorf("Error should mention mode switching, got: %v", err)
	}
}

func TestBundle_Cleanup(t *testing.T) {
	token := createTestJWT(t)
	tokenFile := writeTempFile(t, token)
	config := `{"jwt_token_file": "` + tokenFile + `"}`
	_, cleanup, err := NewBundle(json.RawMessage(config))
	if err != nil {
		t.Fatalf("NewBundle failed: %v", err)
	}

	if cleanup == nil {
		t.Fatal("Expected non-nil cleanup function")
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

	// Create a valid JWT with proper base64 encoding for testing
	// Header: {"typ":"JWT","alg":"HS256"}
	header := "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9"

	// Claims: {"aud":"https://example.com","exp":future_timestamp}
	claims := "eyJhdWQiOiJodHRwczovL2V4YW1wbGUuY29tIiwiZXhwIjoyMDAwMDAwMDAwfQ"

	// Fake signature for testing
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
