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

package credentials_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/credentials"
	xdscreds "google.golang.org/grpc/internal/xds/credentials"
	"google.golang.org/protobuf/types/known/anypb"

	accesstokenpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/call_credentials/access_token/v3"
)

const accessTokenCredsTypeURL = "type.googleapis.com/envoy.extensions.grpc_service.call_credentials.access_token.v3.AccessTokenCredentials"

func accessTokenConfig(t *testing.T, token string) *anypb.Any {
	t.Helper()
	a, err := anypb.New(&accesstokenpb.AccessTokenCredentials{Token: token})
	if err != nil {
		t.Fatalf("Failed to marshal AccessTokenCredentials: %v", err)
	}
	return a
}

// Tests that building access token call credentials fails on malformed
// configs and on an empty token.
func (s) TestAccessTokenCredsBuild_Errors(t *testing.T) {
	tests := []struct {
		name    string
		config  *anypb.Any
		wantErr string
	}{
		{
			name:    "unmarshal_failure",
			config:  &anypb.Any{TypeUrl: accessTokenCredsTypeURL, Value: []byte{0xff}},
			wantErr: "failed to unmarshal AccessTokenCredentials",
		},
		{
			name:    "empty_token",
			config:  accessTokenConfig(t, ""),
			wantErr: "access token must be non-empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCreds, _, err := xdscreds.GetCallCredsBuilder(accessTokenCredsTypeURL)(tt.config)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Build returned error %v, want error containing %q", err, tt.wantErr)
			}
			if callCreds != nil {
				t.Errorf("Build returned non-nil call credentials alongside error")
			}
		})
	}
}

// Tests that the token is attached as a bearer authorization header on
// connections providing privacy and integrity, and that an error is returned
// on weaker connections.
func (s) TestAccessTokenCredsGetRequestMetadata(t *testing.T) {
	callCreds, cleanup, err := xdscreds.GetCallCredsBuilder(accessTokenCredsTypeURL)(accessTokenConfig(t, "test-token"))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	defer cleanup()

	if !callCreds.RequireTransportSecurity() {
		t.Error("RequireTransportSecurity() = false, want true")
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
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

	// RPCs on a connection that does not provide privacy and integrity must
	// fail.
	insecureCtx := credentials.NewContextWithRequestInfo(ctx, credentials.RequestInfo{
		AuthInfo: &testAuthInfo{secLevel: credentials.NoSecurity},
	})
	if _, err := callCreds.GetRequestMetadata(insecureCtx); err == nil {
		t.Fatal("GetRequestMetadata() on an insecure connection succeeded, want error")
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
