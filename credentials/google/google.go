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
	"google.golang.org/grpc/grpclog"
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
	// The transport credentials associated with this bundle.
	transportCreds credentials.TransportCredentials
	// The per RPC credentials associated with this bundle.
	perRPCCreds credentials.PerRPCCredentials
}

func (c *creds) TransportCredentials() credentials.TransportCredentials {
	if c == nil {
		return nil
	}
	return c.transportCreds
}

func (c *creds) PerRPCCredentials() credentials.PerRPCCredentials {
	if c == nil {
		return nil
	}
	return c.perRPCCreds
}

// SwitchMode should make a copy of Bundle, and switch mode. Modifying the
// existing Bundle may cause races.
func (c *creds) SwitchMode(mode string) (credentials.Bundle, error) {
	if c == nil {
		return nil, nil
	}

	newCreds := &creds{mode: mode}

	// Create transport credentials.
	switch mode {
	case internal.CredsBundleModeTLS:
		newCreds.transportCreds = credentials.NewTLS(nil)
	case internal.CredsBundleModeALTS, internal.CredsBundleModeGRPCLB:
		if !vmOnGCP {
			return nil, errors.New("ALTS, as part of google default credentials, is only supported on GCP")
		}
		newCreds.transportCreds = alts.NewClientCreds(alts.DefaultClientOptions())
	default:
		return nil, fmt.Errorf("unsupported mode: %v", mode)
	}

	// Create per RPC credentials.
	// For the time being, we required per RPC credentials for both TLS and
	// ALTS. In the future, this will only be required for TLS.
	ctx, _ := context.WithTimeout(context.Background(), tokenRequestTimeout)
	var err error
	newCreds.perRPCCreds, err = oauth.NewApplicationDefault(ctx, c.scope...)
	if err != nil {
		grpclog.Warningf("google default creds: failed to create application oauth: %v", err)
	}

	return newCreds, nil
}

// new creates a new instance of GoogleDefaultCreds.
func new(tokenScope ...string) credentials.Bundle {
	c := &creds{
		scope: tokenScope,
	}
	bundle, err := c.SwitchMode(internal.CredsBundleModeTLS)
	if err != nil {
		grpclog.Warningf("google default creds: failed to create new creds: %v", err)
	}
	return bundle
}
