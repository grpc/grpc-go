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

package bootstrap

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	access_tokenpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/call_credentials/access_token/v3"
	google_defaultpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/google_default/v3"
	insecurepb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/insecure/v3"
)

func TestInsecureCredsBuilder(t *testing.T) {
	b := GetChannelCredentials("insecure")
	if b == nil {
		t.Fatal("GetChannelCredentials(\"insecure\") returned nil")
	}

	bundle, cancel, err := b.Build(nil)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}
	cancel()
	if bundle == nil {
		t.Fatal("Build() returned nil bundle")
	}

	pbBuilder, ok := b.(ChannelCredentialsWithProto)
	if !ok {
		t.Fatal("insecure builder does not implement ChannelCredentialsWithProto")
	}

	if got := pbBuilder.TypeURL(); got != "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.insecure.v3.InsecureCredentials" {
		t.Errorf("TypeURL() got: %q, want: %q", got, "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.insecure.v3.InsecureCredentials")
	}

	cfg := pbBuilder.NewProtoConfig()
	if _, ok := cfg.(*insecurepb.InsecureCredentials); !ok {
		t.Errorf("NewProtoConfig() returned type: %T, want: *insecurepb.InsecureCredentials", cfg)
	}

	jsonCfg, err := pbBuilder.MarshalProtoConfig(cfg)
	if err != nil {
		t.Fatalf("MarshalProtoConfig() returned error: %v", err)
	}
	if string(jsonCfg) != "{}" {
		t.Errorf("MarshalProtoConfig() got: %s, want: {}", string(jsonCfg))
	}
}

func TestGoogleDefaultCredsBuilder(t *testing.T) {
	b := GetChannelCredentials("google_default")
	if b == nil {
		t.Fatal("GetChannelCredentials(\"google_default\") returned nil")
	}

	bundle, cancel, err := b.Build(nil)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}
	cancel()
	if bundle == nil {
		t.Fatal("Build() returned nil bundle")
	}

	pbBuilder, ok := b.(ChannelCredentialsWithProto)
	if !ok {
		t.Fatal("google_default builder does not implement ChannelCredentialsWithProto")
	}

	if got := pbBuilder.TypeURL(); got != "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.google_default.v3.GoogleDefaultCredentials" {
		t.Errorf("TypeURL() got: %q, want: %q", got, "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.google_default.v3.GoogleDefaultCredentials")
	}

	cfg := pbBuilder.NewProtoConfig()
	if _, ok := cfg.(*google_defaultpb.GoogleDefaultCredentials); !ok {
		t.Errorf("NewProtoConfig() returned type: %T, want: *google_defaultpb.GoogleDefaultCredentials", cfg)
	}

	jsonCfg, err := pbBuilder.MarshalProtoConfig(cfg)
	if err != nil {
		t.Fatalf("MarshalProtoConfig() returned error: %v", err)
	}
	if string(jsonCfg) != "{}" {
		t.Errorf("MarshalProtoConfig() got: %s, want: {}", string(jsonCfg))
	}
}

func TestAccessTokenCallCredsBuilder(t *testing.T) {
	b := GetCallCredentials("access_token")
	if b == nil {
		t.Fatal("GetCallCredentials(\"access_token\") returned nil")
	}

	t.Run("valid config", func(t *testing.T) {
		creds, cancel, err := b.Build(json.RawMessage(`{"token": "test-token"}`))
		if err != nil {
			t.Fatalf("Build() returned error: %v", err)
		}
		cancel()
		if creds == nil {
			t.Fatal("Build() returned nil credentials")
		}

		if !creds.RequireTransportSecurity() {
			t.Error("RequireTransportSecurity() got: false, want: true")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		meta, err := creds.GetRequestMetadata(ctx)
		if err != nil {
			t.Fatalf("GetRequestMetadata() returned error: %v", err)
		}
		if got := meta["authorization"]; got != "Bearer test-token" {
			t.Errorf("authorization got: %q, want: %q", got, "Bearer test-token")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		_, _, err := b.Build(json.RawMessage(`invalid-json`))
		if err == nil {
			t.Fatal("Build() succeeded for invalid JSON, want error")
		}
	})

	t.Run("empty token", func(t *testing.T) {
		_, _, err := b.Build(json.RawMessage(`{"token": ""}`))
		if err == nil || !strings.Contains(err.Error(), "access token must be non-empty") {
			t.Fatalf("Build() returned error: %v, want error containing 'access token must be non-empty'", err)
		}
	})

	t.Run("proto builder", func(t *testing.T) {
		pbBuilder, ok := b.(CallCredentialsWithProto)
		if !ok {
			t.Fatal("access_token builder does not implement CallCredentialsWithProto")
		}

		if got := pbBuilder.TypeURL(); got != "type.googleapis.com/envoy.extensions.grpc_service.call_credentials.access_token.v3.AccessTokenCredentials" {
			t.Errorf("TypeURL() got: %q, want: %q", got, "type.googleapis.com/envoy.extensions.grpc_service.call_credentials.access_token.v3.AccessTokenCredentials")
		}

		cfg := pbBuilder.NewProtoConfig()
		if _, ok := cfg.(*access_tokenpb.AccessTokenCredentials); !ok {
			t.Errorf("NewProtoConfig() returned type: %T, want: *access_tokenpb.AccessTokenCredentials", cfg)
		}

		t.Run("marshal success", func(t *testing.T) {
			tokenMsg := &access_tokenpb.AccessTokenCredentials{Token: "test-token"}
			jsonCfg, err := pbBuilder.MarshalProtoConfig(tokenMsg)
			if err != nil {
				t.Fatalf("MarshalProtoConfig() returned error: %v", err)
			}
			var parsed map[string]string
			if err := json.Unmarshal(jsonCfg, &parsed); err != nil {
				t.Fatalf("Failed to parse json: %v", err)
			}
			if parsed["token"] != "test-token" {
				t.Errorf("token got: %q, want: %q", parsed["token"], "test-token")
			}
		})

		t.Run("marshal type mismatch", func(t *testing.T) {
			_, err := pbBuilder.MarshalProtoConfig(&insecurepb.InsecureCredentials{})
			if err == nil || !strings.Contains(err.Error(), "unexpected config type") {
				t.Fatalf("MarshalProtoConfig() returned error: %v, want error containing 'unexpected config type'", err)
			}
		})

		t.Run("marshal empty token", func(t *testing.T) {
			tokenMsg := &access_tokenpb.AccessTokenCredentials{Token: ""}
			_, err := pbBuilder.MarshalProtoConfig(tokenMsg)
			if err == nil || !strings.Contains(err.Error(), "access token must be non-empty") {
				t.Fatalf("MarshalProtoConfig() returned error: %v, want error containing 'access token must be non-empty'", err)
			}
		})
	})
}
