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

// Package accesstokencreds implements static access token CallCredentials for
// xDS-configured side channels, as specified in gRFC A102.
package accesstokencreds

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/credentials"
)

// NewCallCredentials returns call credentials that attach a static bearer
// token to outgoing RPCs. The config must be a JSON object of the form
// {"token": <non-empty string>}.
//
// The credentials require transport security: the token is only ever sent on
// connections that provide privacy and integrity, and RPCs on weaker
// connections fail.
func NewCallCredentials(configJSON json.RawMessage) (credentials.PerRPCCredentials, error) {
	var cfg struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal access token call credentials config: %v", err)
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("token is required in access token call credentials config")
	}
	return &callCreds{token: cfg.Token}, nil
}

// callCreds implements credentials.PerRPCCredentials by attaching a static
// bearer token to each RPC.
type callCreds struct {
	token string
}

// GetRequestMetadata returns the token as an authorization header. It fails
// if the connection does not provide privacy and integrity.
func (c *callCreds) GetRequestMetadata(ctx context.Context, _ ...string) (map[string]string, error) {
	ri, _ := credentials.RequestInfoFromContext(ctx)
	if err := credentials.CheckSecurityLevel(ri.AuthInfo, credentials.PrivacyAndIntegrity); err != nil {
		return nil, fmt.Errorf("unable to transfer access token PerRPCCredentials: %v", err)
	}
	return map[string]string{"authorization": "Bearer " + c.token}, nil
}

// RequireTransportSecurity indicates whether the credentials requires
// transport security.
func (c *callCreds) RequireTransportSecurity() bool {
	return true
}
