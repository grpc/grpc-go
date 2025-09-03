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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

var (
	errTokenFileAccess = errors.New("token file access error")
	errJWTValidation   = errors.New("JWT validation failure")
)

// jwtClaims represents the JWT claims structure for extracting expiration time.
type jwtClaims struct {
	Exp int64 `json:"exp"`
}

// jWTFileReader handles reading and parsing JWT tokens from files.
type jWTFileReader struct {
	tokenFilePath string
}

// readToken reads and parses a JWT token from the configured file.
// Returns the token string, expiration time, and any error encountered.
func (r *jWTFileReader) readToken() (string, time.Time, error) {
	tokenBytes, err := os.ReadFile(r.tokenFilePath)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("%w: failed to read token file %q: %v", errTokenFileAccess, r.tokenFilePath, err)
	}

	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return "", time.Time{}, fmt.Errorf("%w: token file %q is empty", errJWTValidation, r.tokenFilePath)
	}

	exp, err := r.extractExpiration(token)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("%q: %w", r.tokenFilePath, err)
	}

	return token, exp, nil
}

// extractExpiration parses the JWT token to extract the expiration time.
func (r *jWTFileReader) extractExpiration(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("%w: expected 3 parts, got %d", errJWTValidation, len(parts))
	}

	payload := parts[1]
	// Add padding if necessary for base64 decoding.
	if m := len(payload) % 4; m != 0 {
		payload += strings.Repeat("=", 4-m)
	}

	payloadBytes, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: failed to decode JWT payload: %v", errJWTValidation, err)
	}

	var claims jwtClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return time.Time{}, fmt.Errorf("%w: failed to unmarshal JWT claims: %v", errJWTValidation, err)
	}

	if claims.Exp == 0 {
		return time.Time{}, fmt.Errorf("%w: JWT token has no expiration claim", errJWTValidation)
	}

	expTime := time.Unix(claims.Exp, 0)

	// Check if token is already expired.
	if expTime.Before(time.Now()) {
		return time.Time{}, fmt.Errorf("%w: JWT token is expired", errJWTValidation)
	}

	return expTime, nil
}
