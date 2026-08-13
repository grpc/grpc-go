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
// The caller is expected to invoke the cancel function when they are done
// using the returned call creds. This cancel function is idempotent.
func NewCallCredentials(configJSON json.RawMessage) (credentials.PerRPCCredentials, func(), error) {
	var cfg struct {
		Token string `json:"token"`
	}
	emptyFn := func() {}

	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil, emptyFn, fmt.Errorf("failed to unmarshal access token call credentials config: %v", err)
	}
	if cfg.Token == "" {
		return nil, emptyFn, fmt.Errorf("token is required in access token call credentials config")
	}
	return &callCreds{token: cfg.Token}, emptyFn, nil
}

// callCreds implements credentials.PerRPCCredentials by attaching a static
// bearer token to each RPC.
type callCreds struct {
	token string
}

// GetRequestMetadata returns the token as an authorization header, but only
// when the connection provides privacy and integrity. On weaker connections
// the token is withheld without failing the RPC, as per gRFC A102.
func (c *callCreds) GetRequestMetadata(ctx context.Context, _ ...string) (map[string]string, error) {
	ri, ok := credentials.RequestInfoFromContext(ctx)
	if !ok || credentials.CheckSecurityLevel(ri.AuthInfo, credentials.PrivacyAndIntegrity) != nil {
		return nil, nil
	}
	return map[string]string{"authorization": "Bearer " + c.token}, nil
}

// RequireTransportSecurity returns false. The credentials may be used on any
// connection, but GetRequestMetadata withholds the token on connections that
// do not provide privacy and integrity.
func (c *callCreds) RequireTransportSecurity() bool {
	return false
}
