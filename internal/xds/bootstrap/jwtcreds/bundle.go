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

// Package jwtcreds implements JWT Call Credentials in xDS Bootstrap File.
// See gRFC A97: https://github.com/grpc/proposal/blob/master/A97-xds-jwt-call-creds.md
package jwtcreds

import (
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/jwt"
)

// bundle is an implementation of credentials.Bundle which implements JWT
// Call Credentials in xDS Bootstrap File per gRFC A97.
// This bundle only provides call credentials, not transport credentials.
type bundle struct {
	callCreds credentials.PerRPCCredentials
}

// NewBundle returns a credentials.Bundle which implements JWT Call Credentials
// in xDS Bootstrap File per gRFC A97. This implementation focuses on call credentials
// only and expects the config to match gRFC A97 structure.
// See gRFC A97: https://github.com/grpc/proposal/blob/master/A97-xds-jwt-call-creds.md
func NewBundle(configJSON json.RawMessage) (credentials.Bundle, func(), error) {
	var cfg struct {
		JWTTokenFile string `json:"jwt_token_file"`
	}

	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal JWT call credentials config: %v", err)
	}

	if cfg.JWTTokenFile == "" {
		return nil, nil, fmt.Errorf("jwt_token_file is required in JWT call credentials config")
	}

	callCreds, err := jwt.NewTokenFileCallCredentials(cfg.JWTTokenFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create JWT call credentials: %v", err)
	}

	bundle := &bundle{
		callCreds: callCreds,
	}

	return bundle, func() {}, nil
}

func (b *bundle) TransportCredentials() credentials.TransportCredentials {
	// Transport credentials should be configured separately via channel_creds
	return nil
}

func (b *bundle) PerRPCCredentials() credentials.PerRPCCredentials {
	return b.callCreds
}

func (b *bundle) NewWithMode(_ string) (credentials.Bundle, error) {
	return nil, fmt.Errorf("JWT call credentials bundle does not support mode switching")
}
