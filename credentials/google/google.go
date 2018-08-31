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
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal"
)

// NewDefaultCredentials returns a credentials bundle that is configured to work
// with google services.
//
// This API is experimental.
func NewDefaultCredentials() credentials.Bundle {
	return new()
}

// creds implements credentials.Bundle.
type creds struct {
	// Supported modes are defined in internal/internal.go.
	mode string
}

func (c *creds) TransportCredentials() credentials.TransportCredentials {
	return nil
}

func (c *creds) PerRPCCredentials() credentials.PerRPCCredentials {
	return nil
}

// SwitchMode should make a copy of Bundle, and switch mode. Modifying the
// existing Bundle may cause races.
func (c *creds) SwitchMode(mode string) credentials.Bundle {
	return &creds{mode: mode}
}

// new creates a new instance of GoogleDefaultCreds.
func new() credentials.Bundle {
	return &creds{mode: internal.CredsBundleModeTLS}
}
