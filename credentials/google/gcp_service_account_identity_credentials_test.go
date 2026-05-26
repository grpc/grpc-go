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

// stubBackoff implements a backoff strategy interface for testing.
type stubBackoff struct {
	backoffDelay time.Duration
}

func (m *stubBackoff) Backoff(int) time.Duration {
	return m.backoffDelay
}

// setupStubTokenProvider initializes and returns a stubTokenProvider
// configured with the specified token value, token expiry, error and delay.
func setupStubTokenProvider(token, err string, tokenExpiry time.Time, delay time.Duration) *stubTokenProvider {
	var e error
	if err != "" {
		e = fmt.Errorf("%s", err)
	}
	return &stubTokenProvider{
		err:   e,
		token: &auth.Token{Value: token, Expiry: tokenExpiry},
		delay: delay,
	}
}

// setupTestGcpServiceAccountIdentityCreds constructs a credentials instance.
// It returns a credentials.PerRPCCredentials with the injected
// stubTokenProvider.
//
// It overrides:
//   - newIDTokenCredentials: Injecting a mock ID Token credentials provider.
//   - defaultBackoffStrategy: Injecting a mock backoff strategy with a fast,
//     deterministic 20ms timeout.
//
// All mocked package-level hooks and states are automatically cleaned up and
// restored after the test finishes using t.Cleanup().
func setupTestGcpServiceAccountIdentityCreds(t *testing.T, tp *stubTokenProvider) credentials.PerRPCCredentials {
	// Override the ID token credentials to use stub token provider.
	origNewIDTokenCredentials := internal.NewIDTokenCredentials
	t.Cleanup(func() { internal.NewIDTokenCredentials = origNewIDTokenCredentials })
	internal.NewIDTokenCredentials = func(*idtoken.Options) (*auth.Credentials, error) {
		return auth.NewCredentials(&auth.CredentialsOptions{
			TokenProvider: auth.NewCachedTokenProvider(tp, &auth.CachedTokenProviderOptions{
				ExpireEarly: 1 * time.Minute,
			})}), nil
	}

	// Override the backoff to work with a shorter timeout.
	origBackoff := internal.DefaultBackoffStrategy
	t.Cleanup(func() { internal.DefaultBackoffStrategy = origBackoff })
	internal.DefaultBackoffStrategy = &stubBackoff{backoffDelay: 20 * time.Millisecond}

	creds, cErr := google.NewGcpServiceAccountIdentity("audience")
	if cErr != nil {
		t.Fatalf("NewGcpServiceAccountIdentity() failed: %v", cErr)
	}

	return creds
}

// TestNewGcpServiceAccountIdentity_EmptyAudience verifies that
// NewGcpServiceAccountIdentity returns error when called with empty audience.
func (s) TestNewGcpServiceAccountIdentity_EmptyAudience(t *testing.T) {
	wantErr := "credentials: audience cannot be empty"
	if _, err := google.NewGcpServiceAccountIdentity(""); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("NewGcpServiceAccountIdentity() returned error = %v, want error containing %q", err, wantErr)
	}
}

// TestGcpServiceAccountIdentityCallCreds_GetRequestMetadata verifies the
// successful retrieval of an ID token from the underlying token provider.
// It ensures that a valid token is correctly retrieved and formatted into
// the required "authorization" metadata header with the "Bearer " prefix.
func (s) TestGcpServiceAccountIdentityCallCreds_GetRequestMetadata(t *testing.T) {
	token := "token"
	tp := setupStubTokenProvider(token, "", defaultTokenExpiry, time.Duration(0))
	creds := setupTestGcpServiceAccountIdentityCreds(t, tp)

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

// TestGcpServiceAccountIdentityCallCreds_Backoff verifies the backoff and
// retry behavior of the credentials.
//
// It ensures that initial token fetch failures record the error and trigger
// the backoff timer. Then consecutive requests before the backoff window
// expires return the cached error immediately without attempting another
// fetch. And a subsequent request after backoff resets properly attempts a
// new fetch and eventually returns the successful token when the fetch is
// successful.
func (s) TestGcpServiceAccountIdentityCallCreds_Backoff(t *testing.T) {
	wantErr := "failed while fetching idToken"
	token := "token"
	tp := setupStubTokenProvider(token, wantErr, defaultTokenExpiry, time.Duration(0))
	creds := setupTestGcpServiceAccountIdentityCreds(t, tp)

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

	tp.mu.Lock()
	if tp.callCount != 1 {
		t.Fatalf("unexpected call count to token provider: got %d, want 1", tp.callCount)
	}
	tp.mu.Unlock()

	// Update tokenprovider to return a second failure so we can distinguish the
	// new attempt.
	wantErr2 := "second attempt to fetch token"
	tp.mu.Lock()
	tp.err = fmt.Errorf("%s", wantErr2)
	tp.mu.Unlock()

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

	tp.mu.Lock()
	if tp.callCount != 2 {
		t.Fatalf("unexpected call count to token provider: got %d, want 2", tp.callCount)
	}
	tp.mu.Unlock()

	// Update token provider to return nil error and a valid token.
	tp.mu.Lock()
	tp.err = nil
	tp.mu.Unlock()

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

	tp.mu.Lock()
	if tp.callCount != 3 {
		t.Fatalf("unexpected call count to token provider: got %d, want 3", tp.callCount)
	}
	tp.mu.Unlock()
}

// TestGcpServiceAccountIdentityCallCreds_ConcurrentCalls verifies the
// concurrency guarantees of the credentials wrapper when multiple goroutines
// request a token simultaneously. It ensures that only a single fetch is
// executed at a time, and all blocked requests share the exact same result
// once the fetch completes.
//
// The test verifies this behavior in two phases:
//   - A single fetch is initiated that will fail after a delay. All concurrent
//     requests must block until the initial fetch finishes, and all must
//     return the exact same error.
//   - After resetting the backoff timer, a new fetch is initiated that will
//     succeed after a delay. Concurrent requests are launched again, and all
//     must successfully receive the same valid token.
func (s) TestGcpServiceAccountIdentityCallCreds_ConcurrentCalls(t *testing.T) {
	wantErr := "failed while fetching idToken"
	defaultDelay := 100 * time.Millisecond
	token := "token"
	tp := setupStubTokenProvider(token, wantErr, defaultTokenExpiry, defaultDelay)
	creds := setupTestGcpServiceAccountIdentityCreds(t, tp)

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

	// Start concurrent calls while the first fetch is still in progress.
	concurrency := 5
	errChannels := make([]<-chan error, concurrency)
	for i := 0; i < concurrency; i++ {
		errChannels[i] = runConcurrentCall()
	}

	// Verify that the first call failed with the expected error.
	if err := <-firstErrCh; err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("GetRequestMetadata() failed with err = %v, want error containing %q", err, wantErr)
	}

	// Verify that all blocked concurrent calls failed with exact same error.
	for i, ch := range errChannels {
		if err := <-ch; err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("concurrent call %d to GetRequestMetadata() failed with err = %v, want error containing %q", i, err, wantErr)
		}
	}

	tp.mu.Lock()
	if tp.callCount != 1 {
		t.Fatalf("expected 1 call to token provider, got %d", tp.callCount)
	}
	tp.err = nil
	tp.mu.Unlock()

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

// TestGcpServiceAccountIdentityCallCreds_SecurityLevelFailure verifies that
// credentials fail to return metadata when the security level of the
// connection is not secure.
func (s) TestGcpServiceAccountIdentityCallCreds_SecurityLevelFailure(t *testing.T) {
	tp := setupStubTokenProvider("token", "", defaultTokenExpiry, time.Duration(0))
	creds := setupTestGcpServiceAccountIdentityCreds(t, tp)

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

// TestGcpServiceAccountIdentityCallCreds_EarlyExpiry verifies the asynchronous
// refresh behavior when a cached token falls within the early expiration
// window (stale but still valid).
//
// The test simulates a scenario where the cached token's remaining lifetime is
// less than the 1-minute early expiry buffer. It verifies that:
//   - A call to GetRequestMetadata immediately returns the stale but valid
//     token to avoid blocking the RPC with network latency.
//   - Simultaneously, the Auth library triggers an asynchronous fetch for a
//     new token in the background.
//   - A subsequent call, after waiting for the background fetch to complete,
//     returns the newly acquired token.
func (s) TestGcpServiceAccountIdentityCallCreds_EarlyExpiry(t *testing.T) {
	tokenExpiry := time.Now().Add(30 * time.Second)
	firstToken := "token-A"
	secondToken := "token-B"
	defaultDelay := 100 * time.Millisecond
	tp := setupStubTokenProvider(firstToken, "", tokenExpiry, time.Duration(0))
	creds := setupTestGcpServiceAccountIdentityCreds(t, tp)

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
	tp.mu.Lock()
	tp.token = &auth.Token{Value: secondToken, Expiry: time.Now().Add(1 * time.Hour)}
	tp.delay = defaultDelay
	tp.mu.Unlock()

	// The cached token has not expired yet, so we get the first token to avoid
	// blocking the RPC while the background refresh is in flight.
	md, err = creds.GetRequestMetadata(ctx)
	if err != nil {
		t.Fatalf("GetRequestMetadata() failed: %v", err)
	}
	if got := md["authorization"]; got != wantfirstToken {
		t.Errorf("GetRequestMetadata() returned %q, want %q", got, wantfirstToken)
	}

	tp.mu.Lock()
	tp.delay = time.Duration(0)
	tp.mu.Unlock()
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

// TestGcpServiceAccountIdentityCallCreds_ErrorMapping verifies that different
// types of errors from the metadata server are mapped to the correct gRPC
// status codes when returned through GetRequestMetadata.
func (s) TestGcpServiceAccountIdentityCallCreds_ErrorMapping(t *testing.T) {
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
			tp := setupStubTokenProvider("token", "", defaultTokenExpiry, 0)
			creds := setupTestGcpServiceAccountIdentityCreds(t, tp)
			tp.mu.Lock()
			tp.err = tc.err
			tp.mu.Unlock()

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
