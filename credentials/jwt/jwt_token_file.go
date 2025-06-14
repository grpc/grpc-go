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

// Package jwt implements gRPC credentials using JWT tokens from files.
package jwt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/status"
)

// jwtClaims represents the JWT claims structure for extracting expiration time.
type jwtClaims struct {
	Exp int64 `json:"exp"`
}

// jwtTokenFileCallCreds provides JWT token-based PerRPCCredentials that reads
// tokens from a file.
// This implementation follows the A97 JWT Call Credentials specification.
type jwtTokenFileCallCreds struct {
	tokenFilePath string

	// Cached token data
	mu               sync.RWMutex
	cachedToken      string
	cachedExpiration time.Time // Slightly reduced expiration time compared to the actual exp

	// Error caching with backoff
	cachedError     error            // Cached error from last failed attempt
	cachedErrorTime time.Time        // When the error was cached
	backoffStrategy backoff.Strategy // Backoff strategy when error occurs
	retryAttempt    int              // Current retry attempt number
	nextRetryTime   time.Time        // When next retry is allowed

	// Pre-emptive refresh mutex
	refreshMu sync.Mutex
}

// NewTokenFileCallCredentials creates PerRPCCredentials that reads JWT tokens
// from the specified file path.
//
// tokenFilePath is the filepath to the JWT token file.
func NewTokenFileCallCredentials(tokenFilePath string) (credentials.PerRPCCredentials, error) {
	if tokenFilePath == "" {
		return nil, fmt.Errorf("tokenFilePath cannot be empty")
	}

	return &jwtTokenFileCallCreds{
		tokenFilePath:   tokenFilePath,
		backoffStrategy: backoff.DefaultExponential,
	}, nil
}

// GetRequestMetadata gets the current request metadata, refreshing tokens
// if required. This implementation follows the PerRPCCredentials interface.
// The tokens will get automatically refreshed if they are about to expire or if
// they haven't been loaded successfully yet. In the latter case, a backoff is
// applied before retrying.
// If it's not possible to extract a token from the file, UNAVAILABLE is returned.
// If the token is extracted but invalid, then UNAUTHENTICATED is returned.
func (c *jwtTokenFileCallCreds) GetRequestMetadata(ctx context.Context, _ ...string) (map[string]string, error) {
	ri, _ := credentials.RequestInfoFromContext(ctx)
	if err := credentials.CheckSecurityLevel(ri.AuthInfo, credentials.PrivacyAndIntegrity); err != nil {
		return nil, fmt.Errorf("unable to transfer JWT token file PerRPCCredentials: %v", err)
	}

	// this may be delayed if the token needs to be refreshed from file
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"authorization": "Bearer " + token,
	}, nil
}

// RequireTransportSecurity indicates whether the credentials requires
// transport security.
func (c *jwtTokenFileCallCreds) RequireTransportSecurity() bool {
	return true
}

// getToken returns a valid JWT token, reading from file if necessary.
// Implements pre-emptive refresh and caches errors with backoff.
func (c *jwtTokenFileCallCreds) getToken(ctx context.Context) (string, error) {
	c.mu.RLock()

	if c.isTokenValid() {
		token := c.cachedToken
		shouldRefresh := c.needsPreemptiveRefresh()
		c.mu.RUnlock()

		if shouldRefresh {
			c.triggerPreemptiveRefresh()
		}
		return token, nil
	}

	// if still within backoff period, return cached error to avoid repeated file reads
	if c.cachedError != nil && time.Now().Before(c.nextRetryTime) {
		err := c.cachedError
		c.mu.RUnlock()
		return "", err
	}

	c.mu.RUnlock()
	// Token is expired or missing or the retry backoff period has expired. So
	// refresh synchronously.
	// NOTE: refreshTokenSync itself acquires the write lock
	return c.refreshTokenSync(ctx, false)
}

// isTokenValid checks if the cached token is still valid.
// Caller must hold c.mu.RLock().
func (c *jwtTokenFileCallCreds) isTokenValid() bool {
	if c.cachedToken == "" {
		return false
	}
	return c.cachedExpiration.After(time.Now())
}

// needsPreemptiveRefresh checks if a pre-emptive refresh should be triggered.
// Returns true if the cached token is valid but expires within 1 minute.
// We only trigger pre-emptive refresh for valid tokens - if the token is invalid
// or expired, the next RPC will handle synchronous refresh instead.
// Caller must hold c.mu.RLock().
func (c *jwtTokenFileCallCreds) needsPreemptiveRefresh() bool {
	return c.isTokenValid() && time.Until(c.cachedExpiration) < time.Minute
}

// triggerPreemptiveRefresh starts a background refresh if needed.
// Multiple concurrent calls are safe - only one refresh will run at a time.
// The refresh runs in a separate goroutine and does not block the caller.
func (c *jwtTokenFileCallCreds) triggerPreemptiveRefresh() {
	go func() {
		c.refreshMu.Lock()
		defer c.refreshMu.Unlock()

		// Re-check if refresh is still needed under mutex
		c.mu.RLock()
		stillNeeded := c.needsPreemptiveRefresh()
		c.mu.RUnlock()

		if !stillNeeded {
			return // Another goroutine already refreshed or token expired
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Force refresh to read new token even if current one is still valid
		_, _ = c.refreshTokenSync(ctx, true)
	}()
}

// refreshTokenSync reads a new token from the file and updates the cache.  If
// preemptiveRefresh is true, bypasses the validity check of the currently cached
// token and always reads from file.
// This is used for pre-emptive refresh to ensure new tokens are loaded even when
// the cached token is still valid. If preemptiveRefresh is false, skips file read
// when cached token is still valid, optimizing concurrent synchronous refresh calls
// where one RPC may have already updated the cache while another was waiting on the lock.
func (c *jwtTokenFileCallCreds) refreshTokenSync(_ context.Context, preemptiveRefresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check under write lock but skip if preemptive refresh is requested
	if !preemptiveRefresh && c.isTokenValid() {
		return c.cachedToken, nil
	}

	tokenBytes, err := os.ReadFile(c.tokenFilePath)
	if err != nil {
		err = status.Errorf(codes.Unavailable, "failed to read token file %q: %v", c.tokenFilePath, err)
		c.setErrorWithBackoff(err)
		return "", err
	}

	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		err := status.Errorf(codes.Unavailable, "token file %q is empty", c.tokenFilePath)
		c.setErrorWithBackoff(err)
		return "", err
	}

	// Parse JWT to extract expiration
	exp, err := c.extractExpiration(token)
	if err != nil {
		err = status.Errorf(codes.Unauthenticated, "failed to parse JWT from token file %q: %v", c.tokenFilePath, err)
		c.setErrorWithBackoff(err)
		return "", err
	}

	// Success - clear any cached error and backoff state, update token cache
	c.clearErrorAndBackoff()
	c.cachedToken = token
	// Per RFC A97: consider token invalid if it expires within the next 30
	// seconds to accommodate for clock skew and server processing time.
	c.cachedExpiration = exp.Add(-30 * time.Second)

	return token, nil
}

// extractExpiration parses the JWT token to extract the expiration time.
func (c *jwtTokenFileCallCreds) extractExpiration(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}

	// Decode the payload (second part)
	payload := parts[1]

	// Add padding if necessary for base64 decoding
	for len(payload)%4 != 0 {
		payload += "="
	}

	payloadBytes, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to decode JWT payload: %v", err)
	}

	var claims jwtClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return time.Time{}, fmt.Errorf("failed to unmarshal JWT claims: %v", err)
	}

	if claims.Exp == 0 {
		return time.Time{}, fmt.Errorf("JWT token has no expiration claim")
	}

	expTime := time.Unix(claims.Exp, 0)

	// Check if token is already expired
	if expTime.Before(time.Now()) {
		return time.Time{}, fmt.Errorf("JWT token is expired")
	}

	return expTime, nil
}

// setErrorWithBackoff caches an error and calculates the next retry time using exponential backoff.
// Caller must hold c.mu write lock.
func (c *jwtTokenFileCallCreds) setErrorWithBackoff(err error) {
	c.cachedError = err
	c.cachedErrorTime = time.Now()
	c.retryAttempt++
	backoffDelay := c.backoffStrategy.Backoff(c.retryAttempt - 1)
	c.nextRetryTime = time.Now().Add(backoffDelay)
}

// clearErrorAndBackoff clears the cached error and resets backoff state.
// Caller must hold c.mu write lock.
func (c *jwtTokenFileCallCreds) clearErrorAndBackoff() {
	c.cachedError = nil
	c.cachedErrorTime = time.Time{}
	c.retryAttempt = 0
	c.nextRetryTime = time.Time{}
}
