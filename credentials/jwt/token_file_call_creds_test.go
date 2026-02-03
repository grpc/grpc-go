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

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/status"
)

const defaultTestTimeout = 5 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestNewTokenFileCallCredentialsValidFilepath(t *testing.T) {
	creds, err := NewTokenFileCallCredentials("/path/to/token")
	if err != nil {
		t.Fatalf("NewTokenFileCallCredentials() unexpected error: %v", err)
	}
	if creds == nil {
		t.Fatal("NewTokenFileCallCredentials() returned nil credentials")
	}
}

func (s) TestNewTokenFileCallCredentialsMissingFilepath(t *testing.T) {
	if _, err := NewTokenFileCallCredentials(""); err == nil {
		t.Fatalf("NewTokenFileCallCredentials() expected error, got nil")
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
	now := time.Now().Truncate(time.Second)
	tests := []struct {
		name             string
		invalidTokenPath bool
		tokenContent     string
		authInfo         credentials.AuthInfo
		wantCode         codes.Code
		wantMetadata     map[string]string
	}{
		{
			name:         "valid_token_with_future_expiration",
			tokenContent: createTestJWT(t, now.Add(time.Hour)),
			authInfo:     &testAuthInfo{secLevel: credentials.PrivacyAndIntegrity},
			wantCode:     codes.OK,
			wantMetadata: map[string]string{"authorization": "Bearer " + createTestJWT(t, now.Add(time.Hour))},
		},
		{
			name:         "insufficient_security_level",
			tokenContent: createTestJWT(t, now.Add(time.Hour)),
			authInfo:     &testAuthInfo{secLevel: credentials.NoSecurity},
			wantCode:     codes.Unknown, // http2Client.getCallAuthData actually transforms such errors into into Unauthenticated
		},
		{
			name:             "unreachable_token_file",
			invalidTokenPath: true,
			authInfo:         &testAuthInfo{secLevel: credentials.PrivacyAndIntegrity},
			wantCode:         codes.Unavailable,
		},
		{
			name:         "empty_file",
			tokenContent: "",
			authInfo:     &testAuthInfo{secLevel: credentials.PrivacyAndIntegrity},
			wantCode:     codes.Unauthenticated,
		},
		{
			name:         "malformed_JWT_token",
			tokenContent: "bad contents",
			authInfo:     &testAuthInfo{secLevel: credentials.PrivacyAndIntegrity},
			wantCode:     codes.Unauthenticated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tokenFile string
			if tt.invalidTokenPath {
				tokenFile = "/does-not-exist"
			} else {
				tokenFile = writeTempFile(t, "token", tt.tokenContent)
			}
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
			if gotCode := status.Code(err); gotCode != tt.wantCode {
				t.Fatalf("GetRequestMetadata() = %v, want %v", gotCode, tt.wantCode)
			}

			if diff := cmp.Diff(tt.wantMetadata, metadata); diff != "" {
				t.Errorf("GetRequestMetadata() returned unexpected metadata (-want +got):\n%s", diff)
			}
		})
	}
}

func (s) TestTokenFileCallCreds_TokenCaching(t *testing.T) {
	token := createTestJWT(t, time.Now().Add(time.Hour))
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
	wantMetadata := map[string]string{"authorization": "Bearer " + token}
	if diff := cmp.Diff(wantMetadata, metadata1); diff != "" {
		t.Errorf("First GetRequestMetadata() returned unexpected metadata (-want +got):\n%s", diff)
	}

	// Update the file with a different token.
	newToken := createTestJWT(t, time.Now().Add(2*time.Hour))
	if err := os.WriteFile(tokenFile, []byte(newToken), 0600); err != nil {
		t.Fatalf("Failed to update token file: %v", err)
	}

	// Second call should return cached token (not the updated one).
	metadata2, err := creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("Second GetRequestMetadata() failed: %v", err)
	}

	if diff := cmp.Diff(metadata1, metadata2); diff != "" {
		t.Errorf("Second GetRequestMetadata() returned unexpected metadata (-want +got):\n%s", diff)
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
// TODO: Refactor the test to avoid inspecting and mutating internal state.
func (s) TestTokenFileCallCreds_CacheExpirationIsBeforeTokenExpiration(t *testing.T) {
	// Create token that expires in 2 hours.
	tokenExp := time.Now().Truncate(time.Second).Add(2 * time.Hour)
	token := createTestJWT(t, tokenExp)
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
	if _, err = creds.GetRequestMetadata(ctx); err != nil {
		t.Fatalf("GetRequestMetadata() failed: %v", err)
	}

	// Verify cached expiration is 30 seconds before actual token expiration.
	impl := creds.(*jwtTokenFileCallCreds)
	impl.mu.Lock()
	cachedExp := impl.cachedExpiry
	impl.mu.Unlock()

	wantExp := tokenExp.Add(-30 * time.Second)
	if !cachedExp.Equal(wantExp) {
		t.Errorf("Cache expiration = %v, want %v", cachedExp, wantExp)
	}
}

// Tests that pre-emptive refresh is triggered within 1 minute of expiration.
// This is tested as follows:
// * A token which expires "soon" is created.
// * On the first call to GetRequestMetadata, the token will get loaded and returned.
// * Another token is created and overwrites the file.
// * On the second call we will still return the (valid) first token but also
// detect that a refresh needs to happen and trigger it.
// * On the third call we confirm the new token has been loaded and returned.
func (s) TestTokenFileCallCreds_PreemptiveRefreshIsTriggered(t *testing.T) {
	// Create token that expires in 80 seconds (=> cache expires in ~50s).
	// This ensures pre-emptive refresh triggers since 50s < the 1 minute check.
	tokenExp := time.Now().Add(80 * time.Second)
	expiringToken := createTestJWT(t, tokenExp)
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

	// First call should read from file synchronously.
	metadata1, err := creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("GetRequestMetadata() failed: %v", err)
	}
	wantAuth1 := "Bearer " + expiringToken
	gotAuth1 := metadata1["authorization"]
	if gotAuth1 != wantAuth1 {
		t.Fatalf("First call should return original token: got %q, want %q", gotAuth1, wantAuth1)
	}

	// Verify token was cached and confirm expectation that refresh should be
	// triggered.
	impl := creds.(*jwtTokenFileCallCreds)
	impl.mu.Lock()
	cacheExp := impl.cachedExpiry
	tokenCached := impl.cachedAuthHeader != ""
	shouldTriggerRefresh := time.Until(cacheExp) < preemptiveRefreshThreshold
	impl.mu.Unlock()

	if !tokenCached {
		t.Fatal("Token should be cached after successful GetRequestMetadata")
	}

	if !shouldTriggerRefresh {
		timeUntilExp := time.Until(cacheExp)
		t.Fatalf("Cache expires in %v; test precondition requires that this triggers preemptive refresh", timeUntilExp)
	}

	// Create new token file with different expiration while refresh is
	// happening.
	newToken := createTestJWT(t, time.Now().Add(2*time.Hour))
	if err := os.WriteFile(tokenFile, []byte(newToken), 0600); err != nil {
		t.Fatalf("Failed to write updated token file: %v", err)
	}

	// Get token again - this call should trigger a refresh given that the first
	// one was cached but expiring soon.
	// However, the function should have returned right away with the current
	// cached token because it is still valid and the preemptive refresh is
	// meant to happen without blocking the RPC.
	metadata2, err := creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("Second GetRequestMetadata() failed: %v", err)
	}
	wantAuth2 := wantAuth1
	gotAuth2 := metadata2["authorization"]
	if gotAuth2 != wantAuth2 {
		t.Fatalf("Second call should return the original token: got %q, want %q", gotAuth2, wantAuth2)
	}

	// Now should get the new token which was refreshed in the background.
	wantAuth3 := "Bearer " + newToken
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &testAuthInfo{secLevel: credentials.PrivacyAndIntegrity},
	})
	for ; ; <-time.After(time.Millisecond) {
		if ctx.Err() != nil {
			t.Fatal("Context deadline expired before pre-emptive refresh completed")
		}
		// If the newly returned metadata is different to the old one, verify
		// that it matches the token from the updated file. If not, fail the
		// test.
		metadata3, err := creds.GetRequestMetadata(ctx)
		if err != nil {
			t.Fatalf("Second GetRequestMetadata() failed: %v", err)
		}
		// Pre-emptive refresh not completed yet, try again.
		gotAuth3 := metadata3["authorization"]
		if gotAuth3 == gotAuth2 {
			continue
		}
		if gotAuth3 != wantAuth3 {
			t.Fatalf("Third call should return the new token: got %q, want %q", gotAuth3, wantAuth3)
		}
		break
	}
}

// Tests that backoff behavior handles file read errors correctly.
// It has the following expectations:
// First call to GetRequestMetadata() fails with UNAVAILABLE due to a
// missing file.
// Second call to GetRequestMetadata() fails with UNAVAILABLE due backoff.
// Third call to GetRequestMetadata() fails with UNAVAILABLE due to retry.
// Fourth call to GetRequestMetadata() fails with UNAVAILABLE due to backoff
// even though file exists.
// Fifth call to GetRequestMetadata() succeeds after reading the file and
// backoff has expired.
// TODO: Refactor the test to avoid inspecting and mutating internal state.
func (s) TestTokenFileCallCreds_BackoffBehavior(t *testing.T) {
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
	beforeCallRetryTime := time.Now()
	_, err = creds.GetRequestMetadata(ctx)
	if err == nil {
		t.Fatal("Expected error from nonexistent file")
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("GetRequestMetadata() = %v, want UNAVAILABLE", status.Code(err))
	}

	// Verify error is cached internally.
	impl := creds.(*jwtTokenFileCallCreds)
	impl.mu.Lock()
	retryAttempt := impl.retryAttempt
	nextRetryTime := impl.nextRetryTime
	impl.mu.Unlock()

	if retryAttempt != 1 {
		t.Errorf("Expected retry attempt to be 1, got %d", retryAttempt)
	}
	if !nextRetryTime.After(beforeCallRetryTime) {
		t.Error("Next retry time should be set to a time after the first call")
	}

	// Second call should still return cached error and not retry.
	// Set nextRetryTime far enough in the future to ensure that's the case.
	impl.mu.Lock()
	impl.nextRetryTime = time.Now().Add(1 * time.Minute)
	wantNextRetryTime := impl.nextRetryTime
	impl.mu.Unlock()
	_, err = creds.GetRequestMetadata(ctx)
	if err == nil {
		t.Fatalf("creds.GetRequestMetadata() = %v, want non-nil", err)
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("GetRequestMetadata() = %v, want cached UNAVAILABLE", status.Code(err))
	}

	impl.mu.Lock()
	retryAttempt2 := impl.retryAttempt
	nextRetryTime2 := impl.nextRetryTime
	impl.mu.Unlock()
	if !nextRetryTime2.Equal(wantNextRetryTime) {
		t.Errorf("nextRetryTime should not change due to backoff. Got: %v, Want: %v", nextRetryTime2, wantNextRetryTime)
	}
	if retryAttempt2 != 1 {
		t.Error("Retry attempt should not change due to backoff")
	}

	// Third call should retry but still fail with UNAVAILABLE.
	// Set the backoff retry time in the past to allow next retry attempt.
	impl.mu.Lock()
	impl.nextRetryTime = time.Now().Add(-1 * time.Minute)
	beforeCallRetryTime = impl.nextRetryTime
	impl.mu.Unlock()
	_, err = creds.GetRequestMetadata(ctx)
	if err == nil {
		t.Fatalf("creds.GetRequestMetadata() = %v, want non-nil", err)
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("GetRequestMetadata() = %v, want cached UNAVAILABLE", status.Code(err))
	}

	impl.mu.Lock()
	retryAttempt3 := impl.retryAttempt
	nextRetryTime3 := impl.nextRetryTime
	impl.mu.Unlock()

	if !nextRetryTime3.After(beforeCallRetryTime) {
		t.Error("nextRetryTime3 should have been updated after third call")
	}
	if retryAttempt3 != 2 {
		t.Error("Expected retry attempt to increase after retry")
	}

	// Create valid token file.
	validToken := createTestJWT(t, time.Now().Add(time.Hour))
	if err := os.WriteFile(nonExistentFile, []byte(validToken), 0600); err != nil {
		t.Fatalf("Failed to create valid token file: %v", err)
	}

	// Fourth call should still fail even though the file now exists due to backoff.
	// Set nextRetryTime far enough in the future to ensure that's the case.
	_, err = creds.GetRequestMetadata(ctx)
	impl.mu.Lock()
	impl.nextRetryTime = time.Now().Add(1 * time.Minute)
	wantNextRetryTime = impl.nextRetryTime
	impl.mu.Unlock()
	if err == nil {
		t.Fatalf("creds.GetRequestMetadata() = %v, want non-nil", err)
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("GetRequestMetadata() = %v, want cached UNAVAILABLE", status.Code(err))
	}

	impl.mu.Lock()
	retryAttempt4 := impl.retryAttempt
	nextRetryTime4 := impl.nextRetryTime
	impl.mu.Unlock()

	if !nextRetryTime4.Equal(wantNextRetryTime) {
		t.Errorf("nextRetryTime should not change due to backoff. Got: %v, Want: %v", nextRetryTime4, wantNextRetryTime)
	}
	if retryAttempt4 != retryAttempt3 {
		t.Error("Retry attempt should not change due to backoff")
	}

	// Fifth call should succeed since the file now exists and the backoff has
	// expired.
	// Set the backoff retry time in the past to allow next retry attempt.
	impl.mu.Lock()
	impl.nextRetryTime = time.Now().Add(-1 * time.Minute)
	impl.mu.Unlock()
	_, err = creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("After creating valid token file, backoff should expire and trigger a token reload on the next RPC. GetRequestMetadata() should eventually succeed, but got: %v", err)
	}
	// If successful, verify error cache and backoff state were cleared.
	impl.mu.Lock()
	clearedErr := impl.cachedError
	retryAttempt = impl.retryAttempt
	nextRetryTime = impl.nextRetryTime
	impl.mu.Unlock()

	if clearedErr != nil {
		t.Errorf("After successful retry, cached error should be cleared, got: %v", clearedErr)
	}
	if retryAttempt != 0 {
		t.Errorf("After successful retry, retry attempt should be reset, got: %d", retryAttempt)
	}
	if !nextRetryTime.IsZero() {
		t.Error("After successful retry, next retry time should be cleared")
	}
}

// createTestJWT creates a test JWT token with the specified expiration.
func createTestJWT(t *testing.T, expiration time.Time) string {
	t.Helper()

	claims := map[string]any{}
	if !expiration.IsZero() {
		claims["exp"] = expiration.Unix()
	}

	header := map[string]any{
		"typ": "JWT",
		"alg": "HS256",
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
