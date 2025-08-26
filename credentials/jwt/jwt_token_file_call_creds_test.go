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
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/status"
)

const defaultTestTimeout = 5 * time.Second

type s struct {
	grpctest.Tester
}

func TestTokenFileCallCreds(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestNewTokenFileCallCredentials(t *testing.T) {
	tests := []struct {
		name          string
		tokenFilePath string
		wantErr       string
	}{
		{
			name:          "some filepath",
			tokenFilePath: "/path/to/token",
			wantErr:       "",
		},
		{
			name:          "empty filepath",
			tokenFilePath: "",
			wantErr:       "tokenFilePath cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := NewTokenFileCallCredentials(tt.tokenFilePath)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("NewTokenFileCallCredentials() expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NewTokenFileCallCredentials() error = %v, want error containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewTokenFileCallCredentials() unexpected error: %v", err)
			}
			if creds == nil {
				t.Fatal("NewTokenFileCallCredentials() returned nil credentials")
			}
		})
	}
}

func (s) TestTokenFileCallCreds_RequireTransportSecurity(t *testing.T) {
	creds, err := NewTokenFileCallCredentials("/path/to/token")
	if err != nil {
		t.Fatalf("NewTokenFileCallCredentials() failed: %v", err)
	}

	if !creds.RequireTransportSecurity() {
		t.Error("RequireTransportSecurity() = false, want true")
	}
}

func (s) TestTokenFileCallCreds_GetRequestMetadata(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "jwt_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	now := time.Now().Truncate(time.Second)
	tests := []struct {
		name            string
		tokenContent    string
		authInfo        credentials.AuthInfo
		wantErr         bool
		wantErrContains string
		wantMetadata    map[string]string
	}{
		{
			name:         "valid token with future expiration",
			tokenContent: createTestJWT(t, "https://example.com", now.Add(time.Hour)),
			authInfo:     &testAuthInfo{secLevel: credentials.PrivacyAndIntegrity},
			wantErr:      false,
			wantMetadata: map[string]string{"authorization": "Bearer " + createTestJWT(t, "https://example.com", now.Add(time.Hour))},
		},
		{
			name:            "insufficient security level",
			tokenContent:    createTestJWT(t, "", now.Add(time.Hour)),
			authInfo:        &testAuthInfo{secLevel: credentials.NoSecurity},
			wantErr:         true,
			wantErrContains: "unable to transfer JWT token file PerRPCCredentials",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenFile := writeTempFile(t, "token", tt.tokenContent)

			creds, err := NewTokenFileCallCredentials(tokenFile)
			if err != nil {
				t.Fatalf("NewTokenFileCallCredentials() failed: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
				AuthInfo: tt.authInfo,
			})

			metadata, err := creds.GetRequestMetadata(ctx)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("GetRequestMetadata() expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Fatalf("GetRequestMetadata() error = %v, want error containing %q", err, tt.wantErrContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("GetRequestMetadata() unexpected error: %v", err)
			}

			if len(metadata) != len(tt.wantMetadata) {
				t.Fatalf("GetRequestMetadata() returned %d metadata entries, want %d", len(metadata), len(tt.wantMetadata))
			}

			for k, v := range tt.wantMetadata {
				if metadata[k] != v {
					t.Errorf("GetRequestMetadata() metadata[%q] = %q, want %q", k, metadata[k], v)
				}
			}
		})
	}
}

func (s) TestTokenFileCallCreds_TokenCaching(t *testing.T) {
	token := createTestJWT(t, "", time.Now().Add(time.Hour))
	tokenFile := writeTempFile(t, "token", token)

	creds, err := NewTokenFileCallCredentials(tokenFile)
	if err != nil {
		t.Fatalf("NewTokenFileCallCredentials() failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &testAuthInfo{secLevel: credentials.PrivacyAndIntegrity},
	})

	// First call should read from file.
	metadata1, err := creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("First GetRequestMetadata() failed: %v", err)
	}

	// Update the file with a different token.
	newToken := createTestJWT(t, "", time.Now().Add(2*time.Hour))
	if err := os.WriteFile(tokenFile, []byte(newToken), 0600); err != nil {
		t.Fatalf("Failed to update token file: %v", err)
	}

	// Second call should return cached token (not the updated one).
	metadata2, err := creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("Second GetRequestMetadata() failed: %v", err)
	}

	if metadata1["authorization"] != metadata2["authorization"] {
		t.Error("Expected cached token to be returned, but got different token")
	}
}

// testAuthInfo implements credentials.AuthInfo for testing.
type testAuthInfo struct {
	secLevel credentials.SecurityLevel
}

func (t *testAuthInfo) AuthType() string {
	return "test"
}

func (t *testAuthInfo) GetCommonAuthInfo() credentials.CommonAuthInfo {
	return credentials.CommonAuthInfo{SecurityLevel: t.secLevel}
}

// Tests that cached token expiration is set to 30 seconds before actual token
// expiration.
func (s) TestTokenFileCallCreds_CacheExpirationIsBeforeTokenExpiration(t *testing.T) {
	// Create token that expires in 2 hours.
	tokenExp := time.Now().Truncate(time.Second).Add(2 * time.Hour)
	token := createTestJWT(t, "", tokenExp)
	tokenFile := writeTempFile(t, "token", token)

	creds, err := NewTokenFileCallCredentials(tokenFile)
	if err != nil {
		t.Fatalf("NewTokenFileCallCredentials() failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &testAuthInfo{secLevel: credentials.PrivacyAndIntegrity},
	})

	// Get token to trigger caching.
	_, err = creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("GetRequestMetadata() failed: %v", err)
	}

	// Verify cached expiration is 30 seconds before actual token expiration.
	impl := creds.(*jwtTokenFileCallCreds)
	impl.mu.Lock()
	cachedExp := impl.cachedExpiry
	impl.mu.Unlock()

	expectedExp := tokenExp.Add(-30 * time.Second)
	if !cachedExp.Equal(expectedExp) {
		t.Errorf("cache expiration = %v, want %v", cachedExp, expectedExp)
	}
}

// Tests that pre-emptive refresh is triggered within 1 minute of expiration.
func (s) TestTokenFileCallCreds_PreemptiveRefreshIsTriggered(t *testing.T) {
	// Create token that expires in 80 seconds (=> cache expires in ~50s).
	// This ensures pre-emptive refresh triggers since 50s < the 1 minute check.
	tokenExp := time.Now().Add(80 * time.Second)
	expiringToken := createTestJWT(t, "", tokenExp)
	tokenFile := writeTempFile(t, "token", expiringToken)

	creds, err := NewTokenFileCallCredentials(tokenFile)
	if err != nil {
		t.Fatalf("NewTokenFileCallCredentials() failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &testAuthInfo{secLevel: credentials.PrivacyAndIntegrity},
	})

	// Get token - should trigger pre-emptive refresh.
	metadata1, err := creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("GetRequestMetadata() failed: %v", err)
	}

	// Verify token was cached and check if refresh should be triggered.
	impl := creds.(*jwtTokenFileCallCreds)
	impl.mu.Lock()
	cacheExp := impl.cachedExpiry
	tokenCached := impl.cachedAuthHeader != ""
	shouldTriggerRefresh := impl.needsPreemptiveRefreshLocked()
	impl.mu.Unlock()

	if !tokenCached {
		t.Error("token should be cached after successful GetRequestMetadata")
	}

	if !shouldTriggerRefresh {
		timeUntilExp := time.Until(cacheExp)
		t.Errorf("cache expires in %v, should be < 1 minute to trigger pre-emptive refresh", timeUntilExp)
	}

	// Create new token file with different expiration while refresh is
	// happening.
	newToken := createTestJWT(t, "", time.Now().Add(2*time.Hour))
	if err := os.WriteFile(tokenFile, []byte(newToken), 0600); err != nil {
		t.Fatalf("Failed to write updated token file: %v", err)
	}

	// Get token again - should trigger a refresh given that the first one was
	// cached but expiring soon.
	// However, the function should have returned right away with the current
	// cached token.
	metadata2, err := creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("Second GetRequestMetadata() failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Now should get the new token.
	metadata3, err := creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("Second GetRequestMetadata() failed: %v", err)
	}

	// If pre-emptive refresh worked, we should get the new token.
	expectedAuth1 := "Bearer " + expiringToken
	expectedAuth2 := "Bearer " + expiringToken
	expectedAuth3 := "Bearer " + newToken

	actualAuth1 := metadata1["authorization"]
	actualAuth2 := metadata2["authorization"]
	actualAuth3 := metadata3["authorization"]

	if actualAuth1 != expectedAuth1 {
		t.Errorf("First call should return original token: got %q, want %q", actualAuth1, expectedAuth1)
	}

	if actualAuth2 != expectedAuth2 {
		t.Errorf("Second call should return the original token: got %q, want %q", actualAuth2, expectedAuth2)
	}
	if actualAuth3 != expectedAuth3 {
		t.Errorf("Third call should return the new token: got %q, want %q", actualAuth3, expectedAuth3)
	}
}

// Tests that backoff behavior handles file read errors correctly.
func (s) TestTokenFileCallCreds_BackoffBehavior(t *testing.T) {
	// This test has the following expectations:
	// First call to GetRequestMetadata() fails with UNAVAILABLE due to a
	// missing file.
	// Second call to GetRequestMetadata() fails with UNAVAILABLE due backoff.
	// Third call to GetRequestMetadata() fails with UNAVAILABLE due to retry.
	// Fourth call to GetRequestMetadata() fails with UNAVAILABLE due to backoff
	// even though file exists.
	// Fifth call to GetRequestMetadata() succeeds after reading the file and
	// backoff has expired.
	tempDir := t.TempDir()
	nonExistentFile := filepath.Join(tempDir, "nonexistent")

	creds, err := NewTokenFileCallCredentials(nonExistentFile)
	if err != nil {
		t.Fatalf("NewTokenFileCallCredentials() failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &testAuthInfo{secLevel: credentials.PrivacyAndIntegrity},
	})

	// First call should fail with UNAVAILABLE.
	_, err1 := creds.GetRequestMetadata(ctx)
	if err1 == nil {
		t.Fatal("Expected error from nonexistent file")
	}
	if status.Code(err1) != codes.Unavailable {
		t.Fatalf("GetRequestMetadata() = %v, want UNAVAILABLE", status.Code(err1))
	}

	// Verify error is cached internally.
	impl := creds.(*jwtTokenFileCallCreds)
	impl.mu.Lock()
	cachedErr := impl.cachedError
	retryAttempt := impl.retryAttempt
	nextRetryTime := impl.nextRetryTime
	impl.mu.Unlock()

	if cachedErr == nil {
		t.Error("error should be cached internally after failed file read")
	}
	if retryAttempt != 1 {
		t.Errorf("Expected retry attempt to be 1, got %d", retryAttempt)
	}
	if nextRetryTime.IsZero() || nextRetryTime.Before(time.Now()) {
		t.Error("Next retry time should be set to future time")
	}

	// Second call should still return cached error.
	_, err2 := creds.GetRequestMetadata(ctx)
	if err2 == nil {
		t.Fatal("Expected cached error")
	}
	if status.Code(err2) != codes.Unavailable {
		t.Fatalf("GetRequestMetadata() = %v, want cached UNAVAILABLE", status.Code(err2))
	}
	if err1.Error() != err2.Error() {
		t.Errorf("cached error = %q, want %q", err2.Error(), err1.Error())
	}

	impl.mu.Lock()
	retryAttempt2 := impl.retryAttempt
	nextRetryTime2 := impl.nextRetryTime
	impl.mu.Unlock()

	if !nextRetryTime2.Equal(nextRetryTime) {
		t.Errorf("nextRetryTime should not change due to backoff. Got: %v, Want: %v", nextRetryTime2, nextRetryTime)
	}
	if retryAttempt2 != 1 {
		t.Error("retry attempt should not change due to backoff")
	}

	// Fast-forward the backoff retry time to allow next retry attempt.
	impl.mu.Lock()
	impl.nextRetryTime = time.Now().Add(-1 * time.Minute)
	impl.mu.Unlock()

	// Third call should retry but still fail with UNAVAILABLE.
	_, err3 := creds.GetRequestMetadata(ctx)
	if err3 == nil {
		t.Fatal("Expected cached error")
	}
	if status.Code(err3) != codes.Unavailable {
		t.Fatalf("GetRequestMetadata() = %v, want cached UNAVAILABLE", status.Code(err3))
	}
	if err3.Error() != err1.Error() {
		t.Errorf("cached error = %q, want %q", err3.Error(), err1.Error())
	}

	impl.mu.Lock()
	retryAttempt3 := impl.retryAttempt
	nextRetryTime3 := impl.nextRetryTime
	impl.mu.Unlock()

	if !nextRetryTime3.After(nextRetryTime2) {
		t.Error("nextRetryTime should not change due to backoff")
	}
	if retryAttempt3 != 2 {
		t.Error("retry attempt should not change due to backoff")
	}

	// Create valid token file.
	validToken := createTestJWT(t, "", time.Now().Add(time.Hour))
	if err := os.WriteFile(nonExistentFile, []byte(validToken), 0600); err != nil {
		t.Fatalf("Failed to create valid token file: %v", err)
	}

	// Fourth call should still fail even though the file now exists.
	_, err4 := creds.GetRequestMetadata(ctx)
	if err4 == nil {
		t.Fatal("Expected cached error")
	}
	if status.Code(err4) != codes.Unavailable {
		t.Fatalf("GetRequestMetadata() = %v, want cached UNAVAILABLE", status.Code(err4))
	}
	if err4.Error() != err3.Error() {
		t.Errorf("cached error = %q, want %q", err4.Error(), err3.Error())
	}

	impl.mu.Lock()
	retryAttempt4 := impl.retryAttempt
	nextRetryTime4 := impl.nextRetryTime
	impl.mu.Unlock()

	if !nextRetryTime4.Equal(nextRetryTime3) {
		t.Errorf("nextRetryTime should not change due to backoff. Got: %v, Want: %v", nextRetryTime4, nextRetryTime3)
	}
	if retryAttempt4 != retryAttempt3 {
		t.Error("retry attempt should not change due to backoff")
	}

	// Fast-forward the backoff retry time to allow next retry attempt.
	impl.mu.Lock()
	impl.nextRetryTime = time.Now().Add(-1 * time.Minute)
	impl.mu.Unlock()
	// Fifth call should succeed since the file now exists
	// and the backoff has expired.
	_, err5 := creds.GetRequestMetadata(ctx)
	if err5 != nil {
		t.Errorf("after creating valid token file, GetRequestMetadata() should eventually succeed, but got: %v", err5)
		t.Error("backoff should expire and trigger new attempt on next RPC")
	} else {
		// If successful, verify error cache and backoff state were cleared.
		impl.mu.Lock()
		clearedErr := impl.cachedError
		retryAttempt := impl.retryAttempt
		nextRetryTime := impl.nextRetryTime
		impl.mu.Unlock()

		if clearedErr != nil {
			t.Errorf("after successful retry, cached error should be cleared, got: %v", clearedErr)
		}
		if retryAttempt != 0 {
			t.Errorf("after successful retry, retry attempt should be reset, got: %d", retryAttempt)
		}
		if !nextRetryTime.IsZero() {
			t.Error("after successful retry, next retry time should be cleared")
		}
	}
}

// createTestJWT creates a test JWT token with the specified audience and
// expiration.
func createTestJWT(t *testing.T, audience string, expiration time.Time) string {
	t.Helper()

	header := map[string]any{
		"typ": "JWT",
		"alg": "HS256",
	}

	claims := map[string]any{}
	if audience != "" {
		claims["aud"] = audience
	}
	if !expiration.IsZero() {
		claims["exp"] = expiration.Unix()
	}

	headerBytes, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("Failed to marshal header: %v", err)
	}

	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("Failed to marshal claims: %v", err)
	}

	headerB64 := base64.URLEncoding.EncodeToString(headerBytes)
	claimsB64 := base64.URLEncoding.EncodeToString(claimsBytes)

	// Remove padding for URL-safe base64
	headerB64 = strings.TrimRight(headerB64, "=")
	claimsB64 = strings.TrimRight(claimsB64, "=")

	// For testing, we'll use a fake signature
	signature := base64.URLEncoding.EncodeToString([]byte("fake_signature"))
	signature = strings.TrimRight(signature, "=")

	return fmt.Sprintf("%s.%s.%s", headerB64, claimsB64, signature)
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, name)
	if err := os.WriteFile(filePath, []byte(content), 0600); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	return filePath
}
