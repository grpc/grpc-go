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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/transport"
	"google.golang.org/grpc/status"
)

func getMetadataHost() string {
	if host := os.Getenv("GCE_METADATA_HOST"); host != "" {
		return host
	}
	return "metadata.google.internal"
}

// fetchIDTokenFromMetadataServer fetches an ID token for the given audience
// from the GCE Metadata Server.
func fetchIDTokenFromMetadataServer(ctx context.Context, audience string) (string, time.Time, error) {
	reqURL := fmt.Sprintf("http://%s/computeMetadata/v1/instance/service-accounts/default/identity?audience=%s&format=full", getMetadataHost(), audience)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", time.Time{}, status.Errorf(codes.Unavailable, "credentials: failed to fetch ID token: %v", err)
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", time.Time{}, status.Errorf(codes.Unavailable, "credentials: failed to fetch ID token: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, status.Errorf(codes.Unavailable, "credentials: failed to fetch ID token: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		switch transport.HTTPStatusConvTab[resp.StatusCode] {
		case codes.Unavailable:
			return "", time.Time{}, status.Errorf(codes.Unavailable, "credentials: failed to fetch token from metadata server: HTTP status %d: %s", resp.StatusCode, string(body))
		default:
			return "", time.Time{}, status.Errorf(codes.Unauthenticated, "credentials: failed to fetch token from metadata server: HTTP status %d: %s", resp.StatusCode, string(body))
		}
	}

	rawJWT := strings.TrimSpace(string(body))
	expiry, err := parseJWTExpiry(rawJWT)
	if err != nil {
		return "", time.Time{}, status.Errorf(codes.Unavailable, "credentials: failed to fetch ID token: %v", err)
	}

	return rawJWT, expiry, nil
}

type jwtPayload struct {
	Exp int64 `json:"exp"`
}

func parseJWTExpiry(jwtStr string) (time.Time, error) {
	parts := strings.Split(jwtStr, ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}

	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to decode JWT payload: %v", err)
	}

	var payload jwtPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return time.Time{}, fmt.Errorf("failed to unmarshal JWT payload: %v", err)
	}

	if payload.Exp == 0 {
		return time.Time{}, fmt.Errorf("missing or zero 'exp' claim in JWT payload")
	}

	return time.Unix(payload.Exp, 0), nil
}
