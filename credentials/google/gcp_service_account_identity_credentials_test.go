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

package google_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials/idtoken"
	"cloud.google.com/go/compute/metadata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/google"
	"google.golang.org/grpc/credentials/google/internal"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/status"
)

const defaultTestTimeout = 10 * time.Second

var defaultTokenExpiry = time.Now().Add(1 * time.Hour)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// gcpTestAuthInfo implements credentials.AuthInfo for testing.
type gcpTestAuthInfo struct {
	credentials.CommonAuthInfo
}

func (t *gcpTestAuthInfo) AuthType() string {
	return "test"
}

// stubBackoff implements a backoff strategy interface for testing.
type stubBackoff struct {
	backoffDelay time.Duration
}

func (m *stubBackoff) Backoff(int) time.Duration {
	return m.backoffDelay
}

// stubTokenProvider implements auth.TokenProvider for unit testing.
// It supplies mocked token payloads, simulates processing delay, and tracks
// invocation frequency to verify caching and backoff behaviors.
type stubTokenProvider struct {
	mu        sync.Mutex
	err       error
	token     *auth.Token
	delay     time.Duration // Simulates processing delays for testing purposes.
	callCount int           // tracks fetch attempts to verify caching and backoff behaviors.
}

func (c *stubTokenProvider) Token(ctx context.Context) (*auth.Token, error) {
	c.mu.Lock()
	c.callCount++
	delay := c.delay
	token := c.token
	err := c.err
	c.mu.Unlock()

	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return token, err
}

func (c *stubTokenProvider) setErr(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = err
}

func (c *stubTokenProvider) setToken(token *auth.Token) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
}

func (c *stubTokenProvider) setDelay(delay time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.delay = delay
}

func (c *stubTokenProvider) getCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callCount
}

// setupStubTokenProvider initializes and returns a stubTokenProvider
// configured with the specified token value, token expiry, error and delay.
func setupStubTokenProvider(token string, err error) *stubTokenProvider {
	return &stubTokenProvider{
		err:   err,
		token: &auth.Token{Value: token, Expiry: defaultTokenExpiry},
		delay: time.Duration(0),
	}
}

// setupTestGCPServiceAccountIdentityCreds constructs a GCP service account
// identity credentials instance with an injected stub token provider.
//
// It overrides internal.NewIDTokenCredentials to use the stubTokenProvider,
// and registers a cleanup function to restore original hook after the test.
func setupTestGCPServiceAccountIdentityCreds(t *testing.T, stubToken *stubTokenProvider) credentials.PerRPCCredentials {
	// Override the ID token credentials to use stub token provider.
	origNewIDTokenCredentials := internal.NewIDTokenCredentials
	internal.NewIDTokenCredentials = func(*idtoken.Options) (*auth.Credentials, error) {
		return auth.NewCredentials(&auth.CredentialsOptions{
			TokenProvider: auth.NewCachedTokenProvider(stubToken, &auth.CachedTokenProviderOptions{})}), nil
	}
	t.Cleanup(func() { internal.NewIDTokenCredentials = origNewIDTokenCredentials })

	creds, err := google.NewServiceAccountIdentityCredentials("audience")
	if err != nil {
		t.Fatalf("NewServiceAccountIdentityCredentials() failed: %v", err)
	}

	return creds
}

// Test verifies that NewServiceAccountIdentityCredentials returns an error
// when called with empty audience.
func (s) TestNewServiceAccountIdentityCredentials_EmptyAudience(t *testing.T) {
	const wantErr = "credentials: audience cannot be empty"
	if _, err := google.NewServiceAccountIdentityCredentials(""); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("NewServiceAccountIdentityCredentials() returned error = %v, want error containing %q", err, wantErr)
	}
}

// Test verifies the successful retrieval of an ID token from the underlying
// token provider. It ensures that a valid token is correctly retrieved and
// formatted into the required "authorization" metadata header with the
// "Bearer " prefix.
func (s) TestGCPServiceAccountIdentityCallCreds_GetRequestMetadata(t *testing.T) {
	const token = "token"
	stubToken := setupStubTokenProvider(token, nil)
	creds := setupTestGCPServiceAccountIdentityCreds(t, stubToken)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	md, err := creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("GetRequestMetadata() failed: %v", err)
	}

	want := "Bearer " + token
	if got := md["authorization"]; got != want {
		t.Errorf("Unexpected token from GetRequestMetadata(), got %q, want %q", got, want)
	}
}

// Test verifies that when a backoff delay is set, a token fetch failure caches
// the failure error, and subsequent calls to GetRequestMetadata before the
// backoff window expires return the cached error immediately without
// initiating another fetch request to the provider.
func (s) TestGCPServiceAccountIdentityCallCreds_Backoff(t *testing.T) {
	const wantErr = "failed while fetching idToken"

	// Override the backoff strategy with a very large value to guarantee it doesn't expire during this test.
	origBackoff := internal.BackoffStrategy
	internal.BackoffStrategy = &stubBackoff{backoffDelay: 2 * defaultTestTimeout}
	t.Cleanup(func() { internal.BackoffStrategy = origBackoff })

	stubToken := setupStubTokenProvider("token", errors.New(wantErr))
	creds := setupTestGCPServiceAccountIdentityCreds(t, stubToken)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	// First call triggers initial fetch, which fails.
	if _, err := creds.GetRequestMetadata(ctx); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("GetRequestMetadata() failed with err = %v, want error %q", err, wantErr)
	}

	// Verify that calls within the backoff window fail immediately with the
	// cached error without attempting a new token fetch.
	const numCalls = 3
	for i := 0; i < numCalls; i++ {
		if _, err := creds.GetRequestMetadata(ctx); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("GetRequestMetadata() attempt %d failed with err = %v, want error %q", i, err, wantErr)
		}
	}

	// Verify that no subsequent fetch attempt made.
	if got := stubToken.getCallCount(); got != 1 {
		t.Fatalf("Unexpected call count to token provider: got %d, want 1", got)
	}
}

// Test verifies that when backoff expired, a subsequent request after an
// initial failure triggers a new fetch attempt and returns the new error.
func (s) TestGCPServiceAccountIdentityCallCreds_BackoffExpired(t *testing.T) {
	const (
		wantErr  = "failed while fetching idToken"
		wantErr2 = "second attempt to fetch token"
	)

	// Override the backoff strategy with a 0s delay to expires immediately.
	origBackoff := internal.BackoffStrategy
	internal.BackoffStrategy = &stubBackoff{backoffDelay: 0}
	t.Cleanup(func() { internal.BackoffStrategy = origBackoff })

	stubToken := setupStubTokenProvider("token", errors.New(wantErr))
	creds := setupTestGCPServiceAccountIdentityCreds(t, stubToken)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	// First call triggers token fetch and fails.
	if _, err := creds.GetRequestMetadata(ctx); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("GetRequestMetadata() failed with err = %v, want error %q", err, wantErr)
	}

	if got := stubToken.getCallCount(); got != 1 {
		t.Fatalf("Expected 1 call to token provider, got %d", got)
	}

	// Update token provider with second failure error to distinguish the new attempt.
	stubToken.setErr(errors.New(wantErr2))

	// Since backoff is 0s (already expired), the next request immediately
	// triggers a new fetch attempt.
	if _, err := creds.GetRequestMetadata(ctx); err == nil || !strings.Contains(err.Error(), wantErr2) {
		t.Fatalf("GetRequestMetadata() failed with err = %v, want error %q", err, wantErr2)
	}

	// Verify that a second token fetch actually happened (callCount is exactly 2).
	if got := stubToken.getCallCount(); got != 2 {
		t.Fatalf("Unexpected call count to token provider: got %d, want 2", got)
	}
}

// Test verifies that when backoff expires, a subsequent request after an
// initial failure triggers a new fetch attempt and returns the new token.
func (s) TestGCPServiceAccountIdentityCallCreds_BackoffExpiredRecovery(t *testing.T) {
	const (
		wantErr = "failed while fetching idToken"
		token   = "token"
	)

	// Override the backoff strategy with a 0s delay to expires immediately.
	origBackoff := internal.BackoffStrategy
	internal.BackoffStrategy = &stubBackoff{backoffDelay: 0}
	t.Cleanup(func() { internal.BackoffStrategy = origBackoff })

	stubToken := setupStubTokenProvider(token, errors.New(wantErr))
	creds := setupTestGCPServiceAccountIdentityCreds(t, stubToken)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	// First call triggers token fetch and fails.
	if _, err := creds.GetRequestMetadata(ctx); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("GetRequestMetadata() failed with err = %v, want error %q", err, wantErr)
	}

	if got := stubToken.getCallCount(); got != 1 {
		t.Fatalf("Expected 1 call to token provider, got %d", got)
	}

	// Update token provider to return a valid token and nil error.
	stubToken.setErr(nil)

	// Since backoff is 0s (already expired), the next request triggers a new
	// fetch which succeeds
	md, err := creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("GetRequestMetadata() failed unexpectedly: %v", err)
	}

	want := "Bearer " + token
	if got := md["authorization"]; got != want {
		t.Errorf("GetRequestMetadata() got %q, want %q", got, want)
	}

	// Verify that the second fetch happened successfully (callCount is exactly 2).
	if got := stubToken.getCallCount(); got != 2 {
		t.Fatalf("Unexpected call count to token provider: got %d, want 2", got)
	}
}

// Test verifies that when multiple goroutines request a token concurrently,
// only a single fetch is executed and all blocked requests successfully
// receive the same valid token.
func (s) TestGCPServiceAccountIdentityCallCreds_ConcurrentCalls_Success(t *testing.T) {
	stubToken := setupStubTokenProvider("token", nil)
	stubToken.setDelay(100 * time.Millisecond)
	creds := setupTestGCPServiceAccountIdentityCreds(t, stubToken)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	const numCalls = 5
	errs := make([]error, numCalls)
	var wg sync.WaitGroup
	for i := range numCalls {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := creds.GetRequestMetadata(ctx)
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			t.Fatalf("Concurrent call to GetRequestMetadata() failed with err = %v", err)
		}
	}

	if got := stubToken.getCallCount(); got != 1 {
		t.Fatalf("Expected 1 call to token provider, got %d", got)
	}
}

// Test verifies that when multiple goroutines request a token concurrently,
// and the first fetch fails, all blocked requests receives the exact same
// error after the fetch.
func (s) TestGCPServiceAccountIdentityCallCreds_ConcurrentCalls_Failure(t *testing.T) {
	const wantErr = "failed while fetching idToken"
	stubToken := setupStubTokenProvider("token", errors.New(wantErr))
	stubToken.setDelay(100 * time.Millisecond)
	creds := setupTestGCPServiceAccountIdentityCreds(t, stubToken)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	const numCalls = 5
	errs := make([]error, numCalls)
	var wg sync.WaitGroup
	for i := range numCalls {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := creds.GetRequestMetadata(ctx)
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("Concurrent call to GetRequestMetadata() failed with err = %v, want error containing %q", err, wantErr)
		}
	}

	if got := stubToken.getCallCount(); got != 1 {
		t.Fatalf("expected 1 call to token provider, got %d", got)
	}
}

// Test verifies that credentials fail to return metadata when the security
// level of the connection is not secure.
func (s) TestGCPServiceAccountIdentityCallCreds_SecurityLevelFailure(t *testing.T) {
	stubToken := setupStubTokenProvider("token", nil)
	creds := setupTestGCPServiceAccountIdentityCreds(t, stubToken)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.NoSecurity}},
	})

	const wantErr = "cannot send secure credentials on an insecure connection"
	_, err := creds.GetRequestMetadata(ctx)
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Errorf("GetRequestMetadata() failed with error = %v, want error %q", err, wantErr)
	}
}

// Test verifies the asynchronous refresh behavior when a cached token falls
// within the early expiration window (stale but still valid).
//
// The test simulates a scenario where the cached token's remaining lifetime is
// less than the 1-minute early expiry buffer. It verifies that:
//   - A call to GetRequestMetadata immediately returns the stale but valid
//     token to avoid blocking the RPC with network latency.
//   - Simultaneously, the Auth library triggers an asynchronous fetch for a
//     new token in the background.
//   - A subsequent call, after waiting for the background fetch to complete,
//     returns the newly acquired token.
func (s) TestGCPServiceAccountIdentityCallCreds_EarlyExpiry(t *testing.T) {
	const (
		firstToken  = "token-A"
		secondToken = "token-B"
	)
	stubToken := setupStubTokenProvider(firstToken, nil)
	stubToken.setToken(&auth.Token{Value: firstToken, Expiry: time.Now().Add(30 * time.Second)})
	creds := setupTestGCPServiceAccountIdentityCreds(t, stubToken)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	md, err := creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("GetRequestMetadata() failed: %v", err)
	}
	wantfirstToken := "Bearer " + firstToken
	if got := md["authorization"]; got != wantfirstToken {
		t.Errorf("GetRequestMetadata() returned %q, want %q", got, wantfirstToken)
	}

	// Update stub token provider to return a new token
	stubToken.setToken(&auth.Token{Value: secondToken, Expiry: time.Now().Add(1 * time.Hour)})
	stubToken.setDelay(100 * time.Millisecond)

	// The cached token has not expired yet, so we get the first token to avoid
	// blocking the RPC while the background refresh is in flight.
	md, err = creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("GetRequestMetadata() failed: %v", err)
	}
	if got := md["authorization"]; got != wantfirstToken {
		t.Errorf("GetRequestMetadata() returned %q, want %q", got, wantfirstToken)
	}

	stubToken.setDelay(time.Duration(0))
	wantSecondToken := "Bearer " + secondToken

	for ; ctx.Err() == nil; <-time.After(10 * time.Millisecond) {
		md, err = creds.GetRequestMetadata(ctx)
		if err != nil {
			t.Fatalf("GetRequestMetadata() failed: %v", err)
		}
		if md["authorization"] == wantSecondToken {
			// Verify that only 2 fetch calls are made.
			if got := stubToken.getCallCount(); got != 2 {
				t.Fatalf("Unexpected call count to token provider: got %d, want 2", got)
			}
			break
		}
	}
	if ctx.Err() != nil {
		t.Fatal("timed out waiting for background fetch to update token")
	}
}

// Test verifies that different types of errors from the metadata server are
// mapped to the correct gRPC status codes when returned through
// GetRequestMetadata.
func (s) TestGCPServiceAccountIdentityCallCreds_ErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{
			name: "429_too_many_requests",
			err:  &metadata.Error{Code: 429, Message: "too many requests"},
			want: codes.Unavailable,
		},
		{
			name: "503_service_unavailable",
			err:  &metadata.Error{Code: 503, Message: "unavailable"},
			want: codes.Unavailable,
		},
		{
			name: "403_forbidden",
			err:  &metadata.Error{Code: 403, Message: "forbidden"},
			want: codes.Unauthenticated,
		},
		{
			name: "generic_protocol_error",
			err:  fmt.Errorf("generic connection error"),
			want: codes.Unavailable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubToken := setupStubTokenProvider("token", nil)
			creds := setupTestGCPServiceAccountIdentityCreds(t, stubToken)
			stubToken.setErr(tc.err)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
				AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
			})

			if _, err := creds.GetRequestMetadata(ctx); status.Code(err) != tc.want {
				t.Errorf("GetRequestMetadata() failed with gRPC status code %v, want %v for error %v", status.Code(err), tc.want, tc.err)
			}
		})
	}
}
