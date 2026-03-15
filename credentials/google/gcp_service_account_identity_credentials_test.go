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
	"testing"
	"time"

	"cloud.google.com/go/auth"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/backoff"
)

type gcpTestAuthInfo struct {
	credentials.CommonAuthInfo
}

func (t *gcpTestAuthInfo) AuthType() string {
	return "test"
}

type mockTokenProvider struct {
	err   error
	token *auth.Token
	delay time.Duration
}

func (c *mockTokenProvider) Token(ctx context.Context) (*auth.Token, error) {
	if c.delay > 0 {
		select {
		case <-time.After(c.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.token, c.err
}

// TestGcpServiceAccountIdentity_GetRequestMetadata verifies the successful
// retrieval of an ID token from the underlying token provider. It ensures
// that a valid token is correctly retrieved and formatted into the required
// "authorization" metadata header with the "Bearer " prefix.
func (s) TestGcpServiceAccountIdentity_GetRequestMetadata(t *testing.T) {
	mockTp := &mockTokenProvider{
		token: &auth.Token{Value: "good-token", Expiry: time.Now().Add(1 * time.Hour)},
		err:   nil,
	}
	creds := &gcpServiceAccountIdentityCallCreds{
		audience: "my-audience",
		ts: auth.NewCredentials(&auth.CredentialsOptions{
			TokenProvider: auth.NewCachedTokenProvider(mockTp, &auth.CachedTokenProviderOptions{
				ExpireEarly: 5 * time.Minute,
			})}),
		backoff: backoff.DefaultExponential,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	md, err := creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("GetRequestMetadata failed: %v", err)
	}

	want := "Bearer good-token"

	if got := md["authorization"]; got != want {
		t.Errorf("GetRequestMetadata returned %q, want %q", got, want)
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
	mockTp := &mockTokenProvider{
		token: nil,
		err:   fmt.Errorf("failed while fetching idToken"),
	}
	creds := &gcpServiceAccountIdentityCallCreds{
		audience: "audience",
		ts: auth.NewCredentials(&auth.CredentialsOptions{
			TokenProvider: auth.NewCachedTokenProvider(mockTp, &auth.CachedTokenProviderOptions{
				ExpireEarly: 5 * time.Minute,
			}),
		}),
		backoff: backoff.DefaultExponential,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	if _, err := creds.GetRequestMetadata(ctx); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("GetRequestMetadata() failed with err = %v, want error containing %q", err, wantErr)
	}

	creds.mu.Lock()
	// Verify that backoff time and lastErr were populated
	if creds.lastErr == nil || creds.nextRetryTime.IsZero() {
		creds.mu.Unlock()
		t.Fatalf("Expected lastErr and nextRetryTime to be non-nil and set")
	}
	if creds.retryAttempt != 1 {
		creds.mu.Unlock()
		t.Fatalf("Expected retryAttempt to be 1, got %d", creds.retryAttempt)
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
		t.Fatalf("Expected retryAttempt to increment to 2, got %d", creds.retryAttempt)
	}
	creds.nextRetryTime = time.Now().Add(-1 * time.Millisecond)
	creds.mu.Unlock()

	mockTp.err = nil
	mockTp.token = &auth.Token{Value: "good-token", Expiry: time.Now().Add(1 * time.Hour)}

	// Successfully fetch the valid token
	md, err3 := creds.GetRequestMetadata(ctx)
	if err3 != nil {
		t.Fatalf("Expected success after backoff completed, got %v", err3)
	}
	want := "Bearer good-token"
	if got := md["authorization"]; got != want {
		t.Errorf("GetRequestMetadata returned %q, want %q", got, want)
	}
}

// TestGcpServiceAccountIdentity_ConcurrentCalls verifies the concurrency
// guarantees of the credentials wrapper when multiple goroutines request a
// token simultaneously. It ensures that only a single fetch is executed at a
// time, and all blocked requests share the exact same result once the fetch
// completes.
//
// The test verifies this behavior in two phases:
//  1. A single fetch is initiated that will fail after a delay. All concurrents
//     requests must block until the initial fetch finishes, and all must
//     return the exact same error.
//  2. After resetting the backoff timer, a new fetch is initiated that will
//     succeed after a delay. Concurrent requests are launched again, and all
//     must successfully receive the same valid token.
func (s) TestGcpServiceAccountIdentity_ConcurrentCalls(t *testing.T) {
	wantErr := "failed while fetching idToken"
	mockTp := &mockTokenProvider{
		delay: 100 * time.Millisecond,
		err:   fmt.Errorf("failed while fetching idToken"),
	}
	creds := &gcpServiceAccountIdentityCallCreds{
		audience: "audience",
		ts:       auth.NewCredentials(&auth.CredentialsOptions{TokenProvider: mockTp}),
		backoff:  backoff.DefaultExponential,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &gcpTestAuthInfo{credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}},
	})

	// Helper function to launch a concurrent GetRequestMetadata call
	// and return a channel that will receive the resulting error.
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

	mockTp.err = nil
	mockTp.token = &auth.Token{Value: "good-token", Expiry: time.Now().Add(1 * time.Hour)}

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
