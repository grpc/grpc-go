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
	"errors"
	"fmt"
	"sync"
	"time"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials/idtoken"
	"cloud.google.com/go/compute/metadata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/internal/transport"
	"google.golang.org/grpc/status"
)

// earlyExpiry matches the hardcoded 5-minute early expiry used by the
// cloud.google.com/go/auth/credentials/idtoken package.
const earlyExpiry = 5 * time.Minute

type gcpServiceAccountIdentityCallCreds struct {
	// The following fields are initialized at creation time and are read-only
	// after that.
	audience string
	creds    *auth.Credentials
	backoff  backoff.Strategy

	// The following fields are protected by mu.
	mu            sync.Mutex
	token         *auth.Token
	fetching      chan struct{} // used to deduplicate concurrent background token fetches
	nextRetryTime time.Time     // timestamp after which we can attempt the next token fetch
	retryAttempt  int           // consecutive fetch failure count used to compute backoff delay
	lastErr       error         // cached error returned from the most recent token fetch attempt
}

var defaultBackoffStrategy backoff.Strategy = backoff.DefaultExponential

var newIDTokenCredentials = func(opts *idtoken.Options) (*auth.Credentials, error) {
	return idtoken.NewCredentials(opts)
}

// NewGcpServiceAccountIdentity creates a PerRPCCredentials that authenticates
// using a GCP Service Account Identity JWT token for the given audience.
//
// This credential fetches the ID token from the GCE metadata server and is only
// valid for use in environments running on GCP. The audience parameter cannot be
// empty.
func NewGcpServiceAccountIdentity(audience string) (credentials.PerRPCCredentials, error) {
	if audience == "" {
		return nil, fmt.Errorf("credentials: audience cannot be empty")
	}

	creds, err := newIDTokenCredentials(&idtoken.Options{
		Audience: audience,
	})
	if err != nil {
		return nil, fmt.Errorf("credentials: failed to create ID token credentials: %v", err)
	}

	return &gcpServiceAccountIdentityCallCreds{
		audience: audience,
		creds:    creds,
		backoff:  defaultBackoffStrategy,
	}, nil
}

// GetRequestMetadata gets the current request metadata, refreshing tokens if
// required. This implementation follows the PerRPCCredentials interface.
//
// It guarantees that only one underlying token fetch will be executed
// concurrently. If a valid token is cached, it is returned immediately. If
// a fetch recently failed, the cached error is returned until the backoff
// interval expires. Otherwise, it initiates a new token fetch or blocks
// waiting for an already-in-progress fetch to complete.
func (c *gcpServiceAccountIdentityCallCreds) GetRequestMetadata(ctx context.Context, _ ...string) (map[string]string, error) {
	ri, _ := credentials.RequestInfoFromContext(ctx)
	if err := credentials.CheckSecurityLevel(ri.AuthInfo, credentials.PrivacyAndIntegrity); err != nil {
		return nil, fmt.Errorf("credentials: cannot send secure credentials on an insecure connection: %v", err)
	}

	c.mu.Lock()

	// If token is valid, return it. If it's also stale, trigger a background
	// refresh if not already running.
	if c.token != nil && c.isTokenValidLocked() {
		if c.isTokenStaleLocked() && c.fetching == nil {
			c.fetching = make(chan struct{})
			go c.startFetch()
		}
		defer c.mu.Unlock()
		return map[string]string{
			"authorization": "Bearer " + c.token.Value,
		}, nil
	}

	if c.lastErr != nil && time.Now().Before(c.nextRetryTime) {
		c.mu.Unlock()
		return nil, c.lastErr
	}

	if c.fetching == nil {
		c.fetching = make(chan struct{})
		go c.startFetch()
	}
	wait := c.fetching
	c.mu.Unlock()

	select {
	case <-wait:
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.token != nil && c.isTokenValidLocked() {
			return map[string]string{
				"authorization": "Bearer " + c.token.Value,
			}, nil
		}
		if c.lastErr != nil {
			return nil, c.lastErr
		}
		return nil, status.Error(codes.Unauthenticated, "fetched token is expired")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// RequireTransportSecurity indicates whether the credentials requires
// transport security.
func (c *gcpServiceAccountIdentityCallCreds) RequireTransportSecurity() bool {
	return true
}

// isTokenStaleLocked checks if the token falls within the
// early expiry window. It must be called with mu locked.
func (c *gcpServiceAccountIdentityCallCreds) isTokenStaleLocked() bool {
	return c.token.Expiry.Round(0).Add(-earlyExpiry).Before(time.Now())
}

// isTokenValidLocked checks if the token is not expired yet. It must be called
// with mu locked.
func (c *gcpServiceAccountIdentityCallCreds) isTokenValidLocked() bool {
	return c.token.Expiry.After(time.Now())
}

// startFetch initiates a token fetch and updates the credential
// state upon completion.
func (c *gcpServiceAccountIdentityCallCreds) startFetch() {
	token, err := c.creds.TokenProvider.Token(context.Background())

	c.mu.Lock()
	defer c.mu.Unlock()

	close(c.fetching)
	c.fetching = nil
	c.handleFetchResultLocked(token, err)
}

// handleFetchResultLocked updates the credentials local token cache and
// backoff state based on the outcome of a background fetch attempt.
//
// If the fetch succeeded, the cached token is updated, and the backoff timers
// and error are reset.
//
// If the fetch failed, backoff attempts are calculated and the error is mapped
// to a gRPC status.
//   - If the HTTP request fails with a status that maps to gRPC UNAVAILABLE
//     according to HTTP to gRPC status code mappings, it returns UNAVAILABLE.
//   - All other HTTP error status codes map to UNAUTHENTICATED.
//   - Non-HTTP request failures are mapped to UNAVAILABLE.
//
// It must be called with mu locked.
func (c *gcpServiceAccountIdentityCallCreds) handleFetchResultLocked(token *auth.Token, err error) {
	if err != nil {
		var mappedErr error
		var metadataErr *metadata.Error
		if errors.As(err, &metadataErr) {
			switch transport.HTTPStatusConvTab[metadataErr.Code] {
			case codes.Unavailable:
				mappedErr = status.Errorf(codes.Unavailable, "credentials: failed to fetch token from metadata server: %v", err)
			default:
				mappedErr = status.Errorf(codes.Unauthenticated, "credentials: failed to fetch token from metadata server: %v", err)
			}
		} else if _, ok := err.(metadata.NotDefinedError); ok {
			mappedErr = status.Errorf(codes.Unauthenticated, "credentials: requested metadata not defined: %v", err)
		} else {
			mappedErr = status.Errorf(codes.Unavailable, "credentials: failed to fetch ID token: %v", err)
		}

		c.lastErr = mappedErr
		backoffDelay := c.backoff.Backoff(c.retryAttempt)
		c.retryAttempt++
		c.nextRetryTime = time.Now().Add(backoffDelay)
		return
	}
	c.lastErr = nil
	c.retryAttempt = 0
	c.nextRetryTime = time.Time{}
	c.token = token
}
