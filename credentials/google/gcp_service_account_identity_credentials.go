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
	"sync"
	"time"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials/idtoken"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/backoff"
)

// earlyExpiry matches the hardcoded 5-minute early expiry used by the
// cloud.google.com/go/auth/credentials/idtoken package.
var earlyExpiry = 5 * time.Minute

type gcpServiceAccountIdentityCallCreds struct {
	audience string
	ts       *auth.Credentials
	backoff  backoff.Strategy

	mu    sync.Mutex
	token *auth.Token

	fetching      chan struct{}
	nextRetryTime time.Time // When we can try next (backoff)
	retryAttempt  int       // consecutive failures
	lastErr       error     // error from last attempt
}

// NewGcpServiceAccountIdentity creates a PerRPCCredentials that authenticates
// using a GCP Service Account Identity JWT token for the given audience.
//
// It uses the cloud.google.com/go/auth/credentials/idtoken package to
// automatically fetch ID token from the GCE metadata server. This credential
// is only valid to use in an environment running on GCP.
func NewGcpServiceAccountIdentity(audience string) (credentials.PerRPCCredentials, error) {
	if audience == "" {
		return nil, fmt.Errorf("audience cannot be empty")
	}

	creds, err := idtoken.NewCredentials(&idtoken.Options{
		Audience: audience,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create auth.Credentials for idtoken: %v", err)
	}

	return &gcpServiceAccountIdentityCallCreds{
		audience: audience,
		ts:       creds,
		backoff:  backoff.DefaultExponential,
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
		return nil, fmt.Errorf("cannot send secure credentials on an insecure connection: %v", err)
	}

	c.mu.Lock()

	if c.isTokenValid() {
		c.mu.Unlock()
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
		c.mu.Unlock()

		token, err := c.ts.TokenProvider.Token(context.Background())

		c.mu.Lock()

		if err != nil {
			c.setBackoff(err)
			close(c.fetching)
			c.fetching = nil
			c.mu.Unlock()
			return nil, err
		}

		c.setBackoff(nil)
		c.token = token
		close(c.fetching)
		c.fetching = nil
		c.mu.Unlock()
		return map[string]string{
			"authorization": "Bearer " + c.token.Value,
		}, nil
	}
	wait := c.fetching
	c.mu.Unlock()
	select {
	case <-wait:
		return func() (map[string]string, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.isTokenValid() {
				return map[string]string{
					"authorization": "Bearer " + c.token.Value,
				}, nil
			}
			return nil, c.lastErr
		}()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// RequireTransportSecurity indicates whether the credentials requires
// transport security.
func (c *gcpServiceAccountIdentityCallCreds) RequireTransportSecurity() bool {
	return true
}

// isTokenValid checks if the cached token is still valid.
func (c *gcpServiceAccountIdentityCallCreds) isTokenValid() bool {
	if c.token == nil {
		return false
	}
	return !c.token.Expiry.Round(0).Add(-earlyExpiry).Before(time.Now())
}

func (c *gcpServiceAccountIdentityCallCreds) setBackoff(err error) {
	if err == nil {
		c.lastErr = nil
		c.retryAttempt = 0
		c.nextRetryTime = time.Time{}
		return
	}
	c.lastErr = err
	backoffDelay := c.backoff.Backoff(c.retryAttempt)
	c.retryAttempt++
	c.nextRetryTime = time.Now().Add(backoffDelay)
}
