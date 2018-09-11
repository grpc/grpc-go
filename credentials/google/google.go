/*
 *
 * Copyright 2018 gRPC authors.
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
	"errors"
	"fmt"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/alts"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/internal"
)

const tokenRequestTimeout = 30 * time.Second

var vmOnGCP bool

func init() {
	vmOnGCP = alts.IsRunningOnGCP()
}

// NewDefaultCredentials returns a credentials bundle that is configured to work
// with google services.
//
// This API is experimental.
func NewDefaultCredentials(tokenScope ...string) credentials.Bundle {
	return new(tokenScope...)
}

// creds implements credentials.Bundle.
type creds struct {
	// Supported modes are defined in internal/internal.go.
	mode string
	// OAuth token scope.
	scope []string
}

func (c *creds) TransportCredentials() (credentials.TransportCredentials, error) {
	switch c.mode {
	case internal.CredsBundleModeTLS:
		return credentials.NewTLS(nil), nil
	case internal.CredsBundleModeALTS:
		if vmOnGCP {
			return alts.NewClientCreds(alts.DefaultClientOptions()), nil

		} else {
			return nil, errors.New("ALTS, as part of google default credentials, is only supported on GCP")
		}
	}
	return nil, fmt.Errorf("unsupported mode: %v", c.mode)
}

func (c *creds) PerRPCCredentials() (credentials.PerRPCCredentials, error) {
	// For the time being, we required per RPC credentials for both TLS and
	// ALTS. In the future, this will only be required for TLS.
	ctx, _ := context.WithTimeout(context.Background(), tokenRequestTimeout)
	return oauth.NewApplicationDefault(ctx, c.scope...)
}

// SwitchMode should make a copy of Bundle, and switch mode. Modifying the
// existing Bundle may cause races.
func (c *creds) SwitchMode(mode string) credentials.Bundle {
	return &creds{mode: mode}
}

// new creates a new instance of GoogleDefaultCreds.
func new(tokenScope ...string) credentials.Bundle {
	return &creds{
		mode:  internal.CredsBundleModeTLS,
		scope: tokenScope,
	}
}
