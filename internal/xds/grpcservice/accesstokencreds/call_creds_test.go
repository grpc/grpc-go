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

package accesstokencreds

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestNewCallCredentialsWithInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config string
	}{
		{
			name:   "not_an_object",
			config: `""`,
		},
		{
			name:   "empty_config",
			config: `{}`,
		},
		{
			name:   "empty_token",
			config: `{"token": ""}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCreds, err := NewCallCredentials(json.RawMessage(tt.config))
			if err == nil {
				t.Fatalf("NewCallCredentials(%s): got nil, want error", tt.config)
			}
			if callCreds != nil {
				t.Errorf("NewCallCredentials(%s): returned non-nil call credentials", tt.config)
			}
		})
	}
}

// Tests that the token is attached as a bearer authorization header on
// connections providing privacy and integrity, and that an error is returned
// on weaker connections.
func (s) TestGetRequestMetadata(t *testing.T) {
	const config = `{"token": "test-token"}`
	callCreds, err := NewCallCredentials(json.RawMessage(config))
	if err != nil {
		t.Fatalf("NewCallCredentials(%s) failed: %v", config, err)
	}

	if !callCreds.RequireTransportSecurity() {
		t.Error("RequireTransportSecurity() = false, want true")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// The token must be attached on a connection with privacy and integrity.
	secureCtx := credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &testAuthInfo{secLevel: credentials.PrivacyAndIntegrity},
	})
	md, err := callCreds.GetRequestMetadata(secureCtx)
	if err != nil {
		t.Fatalf("GetRequestMetadata() on a secure connection failed: %v", err)
	}
	if got, want := md["authorization"], "Bearer test-token"; got != want {
		t.Fatalf("GetRequestMetadata() on a secure connection returned authorization header %q, want %q", got, want)
	}

	// An error must be returned on a connection that does not provide
	// privacy and integrity.
	insecureCtx := credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &testAuthInfo{secLevel: credentials.NoSecurity},
	})
	if md, err := callCreds.GetRequestMetadata(insecureCtx); err == nil {
		t.Fatalf("GetRequestMetadata() on an insecure connection returned metadata %v, want error", md)
	}
}

// testAuthInfo implements credentials.AuthInfo for testing.
type testAuthInfo struct {
	secLevel credentials.SecurityLevel
}

func (t *testAuthInfo) AuthType() string {
	return "test"
}

func (t *testAuthInfo) GetCommonAuthInfo() credentials.CommonAuthInfo {
	return credentials.CommonAuthInfo{SecurityLevel: t.secLevel}
}
