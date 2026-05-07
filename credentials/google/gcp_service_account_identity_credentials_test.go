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

package google

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/compute/metadata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/status"
)

var (
	defaultTokenExpiry = time.Now().Add(1 * time.Hour)
	defaultEarlyExpiry = 5 * time.Minute
	audience           = "audience"
	token              = "good-token"
	defaultDelay       = 100 * time.Millisecond
)

type gcpTestAuthInfo struct {
	credentials.CommonAuthInfo
}

func (t *gcpTestAuthInfo) AuthType() string {
	return "test"
}

type mockTokenProvider struct {
	mu    sync.Mutex
	err   error
	token *auth.Token
	delay time.Duration
}

func (c *mockTokenProvider) Token(ctx context.Context) (*auth.Token, error) {
	c.mu.Lock()
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

func newTestCreds(audience string, token string, tokenExpiry time.Time, err string, delay, earlyExpiry time.Duration) (*gcpServiceAccountIdentityCallCreds, *mockTokenProvider) {
	var e error
	if err != "" {
		e = fmt.Errorf("%s", err)
	}
	mockTp := &mockTokenProvider{
		err:   e,
		token: &auth.Token{Value: token, Expiry: tokenExpiry},
		delay: delay,
	}
	creds := &gcpServiceAccountIdentityCallCreds{
		audience: audience,
		creds: auth.NewCredentials(&auth.CredentialsOptions{
			TokenProvider: auth.NewCachedTokenProvider(mockTp, &auth.CachedTokenProviderOptions{
				ExpireEarly: earlyExpiry,
			})}),
		backoff: backoff.DefaultExponential,
	}
	return creds, mockTp
}

// TestGcpServiceAccountIdentity_GetRequestMetadata verifies the successful
// retrieval of an ID token from the underlying token provider. It ensures
// that a valid token is correctly retrieved and formatted into the required
// "authorization" metadata header with the "Bearer " prefix.
func (s) TestGcpServiceAccountIdentity_GetRequestMetadata(t *testing.T) {
	creds, _ := newTestCreds(audience, token, defaultTokenExpiry, "", time.Duration(0), defaultEarlyExpiry)

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

// TestGcpServiceAccountIdentity_Backoff verifies the backoff and retry
// behavior of the credentials wrapper.
//
// It ensures that initial token fetch failures record the error and initialize
// the backoff timer. Then a consecutive failure after backoff reset properly
// evaluate the timer and increment the retry counter. And then make a
// successful fetch and return the token.
func (s) TestGcpServiceAccountIdentity_Backoff(t *testing.T) {
	wantErr := "failed while fetching idToken"
	creds, mockTp := newTestCreds(audience, token, defaultTokenExpiry, wantErr, time.Duration(0), defaultEarlyExpiry)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	if _, err := creds.GetRequestMetadata(ctx); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("GetRequestMetadata() failed with err = %v, want error %q", err, wantErr)
	}

	creds.mu.Lock()
	// Verify that backoff time and lastErr were populated
	if creds.lastErr == nil || creds.nextRetryTime.IsZero() {
		creds.mu.Unlock()
		t.Fatalf("expected lastErr and nextRetryTime to be non-nil")
	}
	if creds.retryAttempt != 1 {
		creds.mu.Unlock()
		t.Fatalf("expected retryAttempt to be 1, got %d", creds.retryAttempt)
	}

	// Move the backoff timer into the past to force the credentials to bypass
	// the wait check and immediately attempt to fetch a new token instead of
	// returning the cached error.
	creds.nextRetryTime = time.Now().Add(-1 * time.Millisecond)
	creds.mu.Unlock()

	if _, err := creds.GetRequestMetadata(ctx); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("GetRequestMetadata() failed with err = %v, want error containing %q", err, wantErr)
	}

	creds.mu.Lock()
	if creds.retryAttempt != 2 {
		creds.mu.Unlock()
		t.Fatalf("expected retryAttempt to be 2, got %d", creds.retryAttempt)
	}
	creds.nextRetryTime = time.Now().Add(-1 * time.Millisecond)
	creds.mu.Unlock()

	mockTp.mu.Lock()
	mockTp.err = nil
	mockTp.mu.Unlock()

	// Successfully fetch the valid token
	md, err3 := creds.GetRequestMetadata(ctx)
	if err3 != nil {
		t.Fatalf("GetRequestMetadata() failed: %v", err3)
	}
	want := "Bearer " + token
	if got := md["authorization"]; got != want {
		t.Errorf("Unexpected token from GetRequestMetadata(), got %q, want %q", got, want)
	}
}

// TestGcpServiceAccountIdentity_ConcurrentCalls verifies the concurrency
// guarantees of the credentials wrapper when multiple goroutines request a
// token simultaneously. It ensures that only a single fetch is executed at a
// time, and all blocked requests share the exact same result once the fetch
// completes.
//
// The test verifies this behavior in two phases:
//   - A single fetch is initiated that will fail after a delay. All concurrents
//     requests must block until the initial fetch finishes, and all must
//     return the exact same error.
//   - After resetting the backoff timer, a new fetch is initiated that will
//     succeed after a delay. Concurrent requests are launched again, and all
//     must successfully receive the same valid token.
func (s) TestGcpServiceAccountIdentity_ConcurrentCalls(t *testing.T) {
	wantErr := "failed while fetching idToken"
	creds, mockTp := newTestCreds(audience, token, defaultTokenExpiry, wantErr, defaultDelay, defaultEarlyExpiry)

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

	// Start the first call which will trigger the initial fetch.
	firstErrCh := runConcurrentCall()

	// Start concurrent calls while the first fetch is still in progress
	concurrency := 5
	errChannels := make([]<-chan error, concurrency)
	for i := 0; i < concurrency; i++ {
		errChannels[i] = runConcurrentCall()
	}

	// Verify that the first call failed with the expected error
	if err := <-firstErrCh; err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("GetRequestMetadata() failed with err = %v, want error containing %q", err, wantErr)
	}

	// Verify that all blocked concurrent calls also failed with the exact same error
	for i, ch := range errChannels {
		if err := <-ch; err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("concurrent call %d to GetRequestMetadata() failed with err = %v, want error containing %q", i, err, wantErr)
		}
	}

	// Reset the backoff so the next call immediately fetches the token
	creds.mu.Lock()
	creds.nextRetryTime = time.Now().Add(-1 * time.Millisecond)
	creds.mu.Unlock()

	mockTp.mu.Lock()
	mockTp.err = nil
	mockTp.mu.Unlock()

	firstSuccessCh := runConcurrentCall()

	successChannels := make([]<-chan error, concurrency)
	for i := 0; i < concurrency; i++ {
		successChannels[i] = runConcurrentCall()
	}

	if err := <-firstSuccessCh; err != nil {
		t.Fatalf("GetRequestMetadata() failed with err = %v", err)
	}

	for i, ch := range successChannels {
		if err := <-ch; err != nil {
			t.Fatalf("Concurrent call %d to GetRequestMetadata() failed with err = %v", i, err)
		}
	}
}

// TestGcpServiceAccountIdentity_SecurityLevelFailure verifies that the
// credentials fail to return metadata when the security level of the
// connection is not secure.
func (s) TestGcpServiceAccountIdentity_SecurityLevelFailure(t *testing.T) {
	creds, _ := newTestCreds(audience, token, defaultTokenExpiry, "", time.Duration(0), defaultEarlyExpiry)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.NoSecurity}},
	})

	_, err := creds.GetRequestMetadata(ctx)
	if err == nil {
		t.Fatalf("GetRequestMetadata() succeeded on insecure connection, want error")
	}

	want := "cannot send secure credentials on an insecure connection"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("GetRequestMetadata() failed with error = %v, want error %q", err, want)
	}
}

// TestGcpServiceAccountIdentity_EarlyExpiry verifies the asynchronous refresh
// behavior when a cached token falls within the early expiration window (stale
// but still valid).
//
// The test simulates a scenario where the cached token's remaining lifetime is
// less than the 5-minute early expiry buffer. It verifies that:
//   - A call to GetRequestMetadata immediately returns the stale but valid
//     token to avoid blocking the RPC with network latency.
//   - Simultaneously, the Auth library triggers an asynchronous fetch for a
//     new token in the background.
//   - A subsequent call, after waiting for the background fetch to complete,
//     returns the newly acquired token.
func (s) TestGcpServiceAccountIdentity_EarlyExpiry(t *testing.T) {
	tokenExpiry := time.Now().Add(2 * time.Minute)
	firstToken := "token-A"
	secondToken := "token-B"
	creds, mockTp := newTestCreds(audience, firstToken, tokenExpiry, "", time.Duration(0), defaultEarlyExpiry)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	md, err := creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("GetRequestMetadata() failed: %v", err)
	}
	wantToken := "Bearer " + firstToken
	if got := md["authorization"]; got != wantToken {
		t.Errorf("Unexpected token from GetRequestMetadata(), got %q, want %q", got, wantToken)
	}

	// Update mock to return a new token
	mockTp.mu.Lock()
	mockTp.token = &auth.Token{Value: secondToken, Expiry: time.Now().Add(1 * time.Hour)}
	mockTp.delay = defaultDelay
	mockTp.mu.Unlock()
	md, err = creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("GetRequestMetadata() failed: %v", err)
	}
	if got := md["authorization"]; got != wantToken {
		t.Errorf("GetRequestMetadata() returned %q, want %q", got, wantToken)
	}

	mockTp.mu.Lock()
	mockTp.delay = time.Duration(0)
	mockTp.mu.Unlock()
	wantToken = "Bearer " + secondToken

	for {
		md, err = creds.GetRequestMetadata(ctx)
		if err != nil {
			t.Fatalf("GetRequestMetadata() failed: %v", err)
		}
		if md["authorization"] == wantToken {
			break // Success!
		}

		select {
		case <-ctx.Done():
			t.Fatal("Timed out waiting for background fetch to update token")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// TestGcpServiceAccountIdentity_ContextCanceled verifies that
// independent timeouts are respected for concurrent RPCs sharing the same fetch.
//
// The test simulates the following scenario:
//   - The first call (initiator) starts an asynchronous fetch but has a very short
//     timeout. It is expected to fail early due to ctx deadline exceeded.
//   - A second call (waiter) arrives while the fetch is in progress and uses a
//     default (longer) timeout.
//   - Even though the first call timed out, the background fetch is not canceled
//     and is allowed to finish, enabling the second call to eventually succeed and
//     return the correct token.
func (s) TestGcpServiceAccountIdentity_ContextCanceled(t *testing.T) {
	creds, _ := newTestCreds(audience, token, defaultTokenExpiry, "", 2*defaultDelay, defaultEarlyExpiry)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	runConcurrentCall := func(cErr context.Context) (<-chan string, <-chan error) {
		tokenCh := make(chan string, 1)
		errCh := make(chan error, 1)
		go func() {
			md, err := creds.GetRequestMetadata(cErr)
			if err != nil {
				errCh <- err
				return
			}
			tokenCh <- md["authorization"]
		}()
		return tokenCh, errCh
	}

	quickCtx, cancelQuick := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer cancelQuick()

	// First call which triggers initial fetch with short timeout
	_, firstErrCh := runConcurrentCall(quickCtx)

	// Verify the first call to GetRequestMetadata fails with deadline exceeded.
	select {
	case err := <-firstErrCh:
		if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
			t.Fatalf("GetRequestedMetadata() failed with err = %v, want error deadline exceeded", err)
		}
	case <-ctx.Done():
		t.Fatal("Timeout while waiting for first call to fail")
	}

	// Create a long timeout context for the second call (waiter)
	longCtx, cancelLong := context.WithTimeout(ctx, defaultTestTimeout)
	defer cancelLong()
	secondTokenCh, secondErrCh := runConcurrentCall(longCtx)

	select {
	case err := <-secondErrCh:
		t.Fatalf("GetRequestMetadata() failed unexpectedly in second call with err = %v", err)
	case secondtoken := <-secondTokenCh:
		want := "Bearer " + token
		if secondtoken != want {
			t.Errorf("Unexpected token from GetRequestMetadata(), got %q, want %q", secondtoken, want)
		}
	case <-longCtx.Done():
		t.Fatal("Timeout while waiting for second call to succeed")
	}
}

// TestGcpServiceAccountIdentity_ErrorMapping verifies that different types of
// errors from the metadata server are mapped to the correct gRPC status codes
// when returned through GetRequestMetadata.
func (s) TestGcpServiceAccountIdentity_ErrorMapping(t *testing.T) {
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
			name: "not_defined_error",
			err:  metadata.NotDefinedError("suffix not found"),
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
			creds, mockTp := newTestCreds(audience, token, defaultTokenExpiry, "", 0, defaultEarlyExpiry)
			mockTp.mu.Lock()
			mockTp.err = tc.err
			mockTp.mu.Unlock()

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
