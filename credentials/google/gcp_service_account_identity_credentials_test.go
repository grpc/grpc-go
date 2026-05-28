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

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
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
func setupStubTokenProvider(token string, err error, tokenExpiry time.Time, delay time.Duration) *stubTokenProvider {
	return &stubTokenProvider{
		err:   err,
		token: &auth.Token{Value: token, Expiry: tokenExpiry},
		delay: delay,
	}
}

// setupTestGCPServiceAccountIdentityCreds constructs a credentials instance.
// It returns a credentials.PerRPCCredentials with the injected
// stubTokenProvider.
//
// It overrides:
//   - NewIDTokenCredentials: Injecting a mock ID Token credentials provider.
//   - BackoffStrategy: Injecting a mock backoff strategy with a fast,
//     deterministic 20ms timeout.
//
// All mocked package-level hooks and states are automatically cleaned up and
// restored after the test finishes using t.Cleanup().
func setupTestGCPServiceAccountIdentityCreds(t *testing.T, tp *stubTokenProvider, backoffDelay time.Duration) credentials.PerRPCCredentials {
	// Override the ID token credentials to use stub token provider.
	origNewIDTokenCredentials := internal.NewIDTokenCredentials
	internal.NewIDTokenCredentials = func(*idtoken.Options) (*auth.Credentials, error) {
		return auth.NewCredentials(&auth.CredentialsOptions{
			TokenProvider: auth.NewCachedTokenProvider(tp, &auth.CachedTokenProviderOptions{
				ExpireEarly: 1 * time.Minute,
			})}), nil
	}
	t.Cleanup(func() { internal.NewIDTokenCredentials = origNewIDTokenCredentials })

	// Override the backoff to work with a shorter timeout.
	origBackoff := internal.BackoffStrategy
	internal.BackoffStrategy = &stubBackoff{backoffDelay: backoffDelay}
	t.Cleanup(func() { internal.BackoffStrategy = origBackoff })

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
	tp := setupStubTokenProvider(token, nil, defaultTokenExpiry, time.Duration(0))
	creds := setupTestGCPServiceAccountIdentityCreds(t, tp, 20*time.Millisecond)

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

// Test verifies the backoff and retry behavior of the credentials. It ensures
// that initial token fetch failures record the error and trigger the backoff
// timer. Then consecutive requests before the backoff window expires return
// the cached error immediately without attempting another fetch. And a
// subsequent request after backoff resets properly attempts a new fetch and
// eventually returns the successful token when the fetch is successful.
func (s) TestGCPServiceAccountIdentityCallCreds_Backoff1(t *testing.T) {
	const (
		wantErr = "failed while fetching idToken"
		token   = "token"
	)
	tp := setupStubTokenProvider(token, errors.New(wantErr), defaultTokenExpiry, time.Duration(0))
	creds := setupTestGCPServiceAccountIdentityCreds(t, tp, 20*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	if _, err := creds.GetRequestMetadata(ctx); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("GetRequestMetadata() failed with err = %v, want error %q", err, wantErr)
	}

	// Verify that calls within the backoff window fail immediately with the
	// cached error without attempting a new token fetch.
	if _, err := creds.GetRequestMetadata(ctx); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("GetRequestMetadata() failed with err = %v, want error %q", err, wantErr)
	}

	if got := tp.getCallCount(); got != 1 {
		t.Fatalf("unexpected call count to token provider: got %d, want 1", got)
	}

	// Update tokenprovider to return a second failure so we can distinguish the
	// new attempt.
	wantErr2 := "second attempt to fetch token"
	tp.setErr(fmt.Errorf("%s", wantErr2))

	// Wait to receive the next error after backoff timeout expires and a new
	// fetch request is made.
	for {
		if _, err := creds.GetRequestMetadata(ctx); err != nil && strings.Contains(err.Error(), wantErr2) {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("timeout while waiting for GetRequestMetadata to fail with new error")
		}
	}

	if got := tp.getCallCount(); got != 2 {
		t.Fatalf("unexpected call count to token provider: got %d, want 2", got)
	}

	// Update token provider to return nil error and a valid token.
	tp.setErr(nil)

	// Wait to receive the token from the successful token fetch after backoff
	// timer expires.
	var md map[string]string
	var err error
	for {
		if md, err = creds.GetRequestMetadata(ctx); err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("timeout while waiting for successful token fetch")
		}
	}
	want := "Bearer " + token
	if got := md["authorization"]; got != want {
		t.Errorf("GetRequestMetadata() returned %q, want %q", got, want)
	}

	if got := tp.getCallCount(); got != 3 {
		t.Fatalf("unexpected call count to token provider: got %d, want 3", got)
	}
}

// Test verifies that when a backoff delay is set, a token fetch failure caches
// the failure error, and subsequent calls to GetRequestMetadata before the
// backoff window expires return the cached error immediately without
// initiating another fetch request to the provider.
func (s) TestGCPServiceAccountIdentityCallCreds_Backoff(t *testing.T) {
	const (
		wantErr      = "failed while fetching idToken"
		backoffDelay = 2 * defaultTestTimeout // Set backoffDelay to a very large value to guarantee it doesn't expire during this test.
	)

	tp := setupStubTokenProvider("token", errors.New(wantErr), defaultTokenExpiry, time.Duration(0))
	creds := setupTestGCPServiceAccountIdentityCreds(t, tp, backoffDelay)

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
	if got := tp.getCallCount(); got != 1 {
		t.Fatalf("unexpected call count to token provider: got %d, want 1", got)
	}
}

// Test verifies that when backoff expired, a subsequent request after an
// initial failure triggers a new fetch attempt and returns the new error.
func (s) TestGCPServiceAccountIdentityCallCreds_BackoffExpired(t *testing.T) {
	const (
		wantErr      = "failed while fetching idToken"
		wantErr2     = "second attempt to fetch token"
		backoffDelay = time.Duration(0) // 0s delay means backoff expires immediately.
	)

	tp := setupStubTokenProvider("token", errors.New(wantErr), defaultTokenExpiry, time.Duration(0))
	creds := setupTestGCPServiceAccountIdentityCreds(t, tp, backoffDelay)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	// First call triggers token fetch and fails.
	if _, err := creds.GetRequestMetadata(ctx); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("GetRequestMetadata() failed with err = %v, want error %q", err, wantErr)
	}

	// Update token provider with second failure error to distinguish the new attempt.
	tp.setErr(errors.New(wantErr2))

	// Since backoff is 0s (already expired), the next request immediately
	// triggers a new fetch attempt.
	if _, err := creds.GetRequestMetadata(ctx); err == nil || !strings.Contains(err.Error(), wantErr2) {
		t.Fatalf("GetRequestMetadata() failed with err = %v, want error %q", err, wantErr2)
	}

	// Verify that a second token fetch actually happened (callCount is exactly 2).
	if got := tp.getCallCount(); got != 2 {
		t.Fatalf("unexpected call count to token provider: got %d, want 2", got)
	}
}

// Test verifies that when backoff expires, a subsequent request after an
// initial failure triggers a new fetch attempt and returns the new token.
func (s) TestGCPServiceAccountIdentityCallCreds_BackoffExpiredRecovery(t *testing.T) {
	const (
		wantErr      = "failed while fetching idToken"
		token        = "token"
		backoffDelay = time.Duration(0) // 0s delay means backoff expires immediately.
	)

	tp := setupStubTokenProvider(token, errors.New(wantErr), defaultTokenExpiry, time.Duration(0))
	creds := setupTestGCPServiceAccountIdentityCreds(t, tp, backoffDelay)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	// First call triggers token fetch and fails.
	if _, err := creds.GetRequestMetadata(ctx); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("GetRequestMetadata() failed with err = %v, want error %q", err, wantErr)
	}

	// Update token provider to return a valid token and nil error.
	tp.setErr(nil)

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
	if got := tp.getCallCount(); got != 2 {
		t.Fatalf("unexpected call count to token provider: got %d, want 2", got)
	}
}

// Test verifies the concurrency guarantees of the credentials wrapper when
// multiple goroutines request a token simultaneously. It ensures that only
// a single fetch is executed at a time, and all blocked requests share the
// exact same result once the fetch completes.
//
// The test verifies this behavior in two phases:
//   - A single fetch is initiated that will fail after a delay. All concurrent
//     requests must block until the initial fetch finishes, and all must
//     return the exact same error.
//   - After resetting the backoff timer, a new fetch is initiated that will
//     succeed after a delay. Concurrent requests are launched again, and all
//     must successfully receive the same valid token.
func (s) TestGCPServiceAccountIdentityCallCreds_ConcurrentCalls(t *testing.T) {
	const wantErr = "failed while fetching idToken"
	tp := setupStubTokenProvider("token", errors.New(wantErr), defaultTokenExpiry, 100*time.Millisecond)
	creds := setupTestGCPServiceAccountIdentityCreds(t, tp, 20*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	runConcurrentCall := func() <-chan error {
		errCh := make(chan error, 1)
		go func() {
			_, err := creds.GetRequestMetadata(ctx)
			errCh <- err
		}()
		return errCh
	}

	// Start concurrent calls while the first fetch is still in progress.
	concurrency := 5
	errChannels := make([]<-chan error, concurrency)
	for i := 0; i < concurrency; i++ {
		errChannels[i] = runConcurrentCall()
	}

	// Verify that the first call failed with the expected error and all blocked
	// concurrent calls failed with exact same error.
	for i, ch := range errChannels {
		if err := <-ch; err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("concurrent call %d to GetRequestMetadata() failed with err = %v, want error containing %q", i, err, wantErr)
		}
	}

	if got := tp.getCallCount(); got != 1 {
		t.Fatalf("expected 1 call to token provider, got %d", got)
	}
	tp.setErr(nil)

	for {
		if _, err := creds.GetRequestMetadata(ctx); err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("timeout while waiting for successful token fetch")
		}
	}

	successChannels := make([]<-chan error, concurrency)
	for i := 0; i < concurrency; i++ {
		successChannels[i] = runConcurrentCall()
	}

	for i, ch := range successChannels {
		if err := <-ch; err != nil {
			t.Fatalf("Concurrent call %d to GetRequestMetadata() failed with err = %v", i, err)
		}
	}
}

// Test verifies that credentials fail to return metadata when the security
// level of the connection is not secure.
func (s) TestGCPServiceAccountIdentityCallCreds_SecurityLevelFailure(t *testing.T) {
	tp := setupStubTokenProvider("token", nil, defaultTokenExpiry, time.Duration(0))
	creds := setupTestGCPServiceAccountIdentityCreds(t, tp, 20*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.NoSecurity}},
	})

	wantErr := "cannot send secure credentials on an insecure connection"
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
	tp := setupStubTokenProvider(firstToken, nil, time.Now().Add(30*time.Second), time.Duration(0))
	creds := setupTestGCPServiceAccountIdentityCreds(t, tp, 20*time.Millisecond)

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
	tp.setToken(&auth.Token{Value: secondToken, Expiry: time.Now().Add(1 * time.Hour)})
	tp.setDelay(100 * time.Millisecond)

	// The cached token has not expired yet, so we get the first token to avoid
	// blocking the RPC while the background refresh is in flight.
	md, err = creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("GetRequestMetadata() failed: %v", err)
	}
	if got := md["authorization"]; got != wantfirstToken {
		t.Errorf("GetRequestMetadata() returned %q, want %q", got, wantfirstToken)
	}

	tp.setDelay(time.Duration(0))
	wantSecondToken := "Bearer " + secondToken

	for {
		md, err = creds.GetRequestMetadata(ctx)
		if err != nil {
			t.Fatalf("GetRequestMetadata() failed: %v", err)
		}
		if md["authorization"] == wantSecondToken {
			break
		}

		if ctx.Err() != nil {
			t.Fatal("timed out waiting for background fetch to update token")
		}
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
			tp := setupStubTokenProvider("token", nil, defaultTokenExpiry, 0)
			creds := setupTestGCPServiceAccountIdentityCreds(t, tp, 20*time.Millisecond)
			tp.setErr(tc.err)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
				AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
			})

			_, err := creds.GetRequestMetadata(ctx)
			if status.Code(err) != tc.want {
				t.Errorf("GetRequestMetadata() failed with gRPC status code %v, want %v for error %v", status.Code(err), tc.want, tc.err)
			}
		})
	}
}
