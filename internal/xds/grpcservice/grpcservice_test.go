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

package grpcservice

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/bootstrap"
	xdscreds "google.golang.org/grpc/internal/xds/credentials"
	"google.golang.org/grpc/metadata"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	accesstokenpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/call_credentials/access_token/v3"
	xdscredspb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/xds/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	target = "dns:///my-service:443"

	insecureCredsTypeURL = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.insecure.v3.InsecureCredentials"
)

// bootstrapConfig builds a bootstrap Config whose allowed_grpc_services is set
// to the provided JSON (a map from target URI to allowed service config).
func bootstrapConfig(t *testing.T, allowed string) *bootstrap.Config {
	t.Helper()
	// The allowed_grpc_services bootstrap field is parsed only when a
	// consuming feature is enabled.
	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)
	contents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers:             json.RawMessage(`[{"server_uri":"td.googleapis.com:443","channel_creds":[{"type":"insecure"}]}]`),
		Node:                json.RawMessage(`{}`),
		AllowedGRPCServices: json.RawMessage(allowed),
	})
	if err != nil {
		t.Fatalf("NewContentsForTesting() failed: %v", err)
	}
	cfg, err := bootstrap.NewConfigFromContents(contents)
	if err != nil {
		t.Fatalf("NewConfigFromContents() failed: %v", err)
	}
	return cfg
}

// trustedServerConfig returns a server config carrying the trusted_xds_server
// server feature.
func trustedServerConfig(t *testing.T) *bootstrap.ServerConfig {
	t.Helper()
	sc, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{
		URI:            "trusted-server:443",
		ServerFeatures: []string{"trusted_xds_server"},
	})
	if err != nil {
		t.Fatalf("ServerConfigForTesting() failed: %v", err)
	}
	return sc
}

func googleGrpcService(target string, channelPlugins []*anypb.Any, timeout *durationpb.Duration) *v3corepb.GrpcService {
	return &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
				TargetUri:                target,
				ChannelCredentialsPlugin: channelPlugins,
			},
		},
		Timeout: timeout,
	}
}

// protoIdentity returns the identity of a proto plugin config.
func protoIdentity(a *anypb.Any) xdscreds.Identity {
	return xdscreds.Identity{Type: a.GetTypeUrl(), Data: string(a.GetValue())}
}

func accessTokenPlugin(t *testing.T, token string) *anypb.Any {
	t.Helper()
	a, err := anypb.New(&accesstokenpb.AccessTokenCredentials{Token: token})
	if err != nil {
		t.Fatalf("Failed to marshal AccessTokenCredentials: %v", err)
	}
	return a
}

func (s) TestParse(t *testing.T) {
	insecurePlugin := &anypb.Any{TypeUrl: insecureCredsTypeURL}
	xdsWithInsecureFallback, err := anypb.New(&xdscredspb.XdsCredentials{FallbackCredentials: insecurePlugin})
	if err != nil {
		t.Fatalf("Failed to marshal XdsCredentials: %v", err)
	}
	allowedInsecure := `{"dns:///my-service:443":{"channel_creds":[{"type":"insecure"}]}}`

	tests := []struct {
		name   string
		gs     *v3corepb.GrpcService
		sc     *bootstrap.ServerConfig
		config *bootstrap.Config
		// wantChannelCreds carries only the expected credentials identity;
		// comparisons use Identity().
		wantChannelCreds *xdscreds.ChannelCreds
		wantErr          string
	}{
		{
			name:             "trusted_insecure_channel_creds",
			gs:               googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil),
			sc:               trustedServerConfig(t),
			config:           bootstrapConfig(t, "{}"),
			wantChannelCreds: xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin), nil),
		},
		{
			name:             "trusted_xds_creds_resolve_to_fallback",
			gs:               googleGrpcService(target, []*anypb.Any{xdsWithInsecureFallback}, nil),
			sc:               trustedServerConfig(t),
			config:           bootstrapConfig(t, "{}"),
			wantChannelCreds: xdscreds.NewChannelCreds(nil, protoIdentity(xdsWithInsecureFallback), nil),
		},
		{
			name:    "trusted_no_supported_channel_creds",
			gs:      googleGrpcService(target, nil, nil),
			sc:      trustedServerConfig(t),
			config:  bootstrapConfig(t, "{}"),
			wantErr: "no supported channel credentials",
		},
		{
			name:             "untrusted_allowlisted_uses_allowlist_creds",
			gs:               googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil),
			config:           bootstrapConfig(t, allowedInsecure),
			wantChannelCreds: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"}, nil),
		},
		{
			name:    "untrusted_not_allowlisted",
			gs:      googleGrpcService(target, nil, nil),
			config:  bootstrapConfig(t, "{}"),
			wantErr: "not present in allowed_grpc_services",
		},
		{
			name:    "untrusted_nil_bootstrap_config",
			gs:      googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil),
			config:  nil,
			wantErr: "not present in allowed_grpc_services",
		},
		{
			name:    "missing_google_grpc",
			gs:      &v3corepb.GrpcService{},
			sc:      trustedServerConfig(t),
			config:  bootstrapConfig(t, "{}"),
			wantErr: "only google_grpc",
		},
		{
			name:    "empty_target_uri",
			gs:      googleGrpcService("", nil, nil),
			sc:      trustedServerConfig(t),
			config:  bootstrapConfig(t, "{}"),
			wantErr: "target_uri must be non-empty",
		},
		{
			name:    "zero_timeout_rejected",
			gs:      googleGrpcService(target, []*anypb.Any{insecurePlugin}, durationpb.New(0)),
			sc:      trustedServerConfig(t),
			config:  bootstrapConfig(t, "{}"),
			wantErr: "timeout must be strictly positive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Parse(test.gs, test.config, test.sc)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Parse() error = %v, want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() returned unexpected error: %v", err)
			}
			if got.TargetURI != target {
				t.Errorf("Parse() TargetURI = %q, want %q", got.TargetURI, target)
			}
			if got.ChannelCredentials.Bundle() == nil {
				t.Error("Parse() returned channel credentials without a built bundle")
			}
			if got.ChannelCredentials.Identity() != test.wantChannelCreds.Identity() {
				t.Errorf("Parse() ChannelCredentials identity = %+v, want %+v", got.ChannelCredentials.Identity(), test.wantChannelCreds.Identity())
			}
		})
	}
}

func (s) TestParseCallCredentials(t *testing.T) {
	insecurePlugin := &anypb.Any{TypeUrl: insecureCredsTypeURL}
	tokenPlugin := accessTokenPlugin(t, "test-token")
	sc := trustedServerConfig(t)
	bc := bootstrapConfig(t, "{}")

	gs := googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil)
	gs.GetGoogleGrpc().CallCredentialsPlugin = []*anypb.Any{
		tokenPlugin,
		// Plugins of unsupported types are skipped.
		{TypeUrl: "type.googleapis.com/unsupported.CallCredentials"},
	}
	got, err := Parse(gs, bc, sc)
	if err != nil {
		t.Fatalf("Parse() returned unexpected error: %v", err)
	}
	if len(got.CallCredentials) != 1 {
		t.Fatalf("Parse() returned %d call credentials, want 1", len(got.CallCredentials))
	}
	if got.CallCredentials[0].Credentials() == nil {
		t.Error("Parse() returned call credentials without built credentials")
	}
	if want := protoIdentity(tokenPlugin); got.CallCredentials[0].Identity() != want {
		t.Errorf("Parse() CallCredentials identity = %+v, want %+v", got.CallCredentials[0].Identity(), want)
	}

	// An empty token must be rejected.
	gs.GetGoogleGrpc().CallCredentialsPlugin = []*anypb.Any{accessTokenPlugin(t, "")}
	if _, err := Parse(gs, bc, sc); err == nil || !strings.Contains(err.Error(), "access token must be non-empty") {
		t.Fatalf("Parse() error = %v, want substring %q", err, "access token must be non-empty")
	}
}

func (s) TestParseInitialMetadata(t *testing.T) {
	gs := googleGrpcService(target, []*anypb.Any{{TypeUrl: insecureCredsTypeURL}}, nil)
	gs.InitialMetadata = []*v3corepb.HeaderValue{
		{Key: "key-b", Value: "b"},
		{Key: "key-a", Value: "legacy", RawValue: []byte("raw-a")},
	}
	got, err := Parse(gs, bootstrapConfig(t, "{}"), trustedServerConfig(t))
	if err != nil {
		t.Fatalf("Parse() returned unexpected error: %v", err)
	}
	want := metadata.MD{"key-b": []string{"b"}, "key-a": []string{"raw-a"}}
	if diff := cmp.Diff(want, got.InitialMetadata); diff != "" {
		t.Errorf("Parse() InitialMetadata mismatch (-want +got):\n%s", diff)
	}
}

func (s) TestConfigEqual(t *testing.T) {
	insecurePlugin := &anypb.Any{TypeUrl: insecureCredsTypeURL}
	protoInsecure := xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin), nil)
	jsonInsecure := xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"}, nil)

	tests := []struct {
		name string
		a, b *Config
		want bool
	}{
		{
			name: "equal_identities_share",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure},
			b:    &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin), nil)},
			want: true,
		},
		{
			name: "timeout_and_metadata_do_not_affect_sharing",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, Timeout: 1},
			b:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, InitialMetadata: metadata.Pairs("k", "v")},
			want: true,
		},
		{
			name: "different_targets",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure},
			b:    &Config{TargetURI: "dns:///other:443", ChannelCredentials: protoInsecure},
			want: false,
		},
		{
			name: "different_identity_flavors",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure},
			b:    &Config{TargetURI: target, ChannelCredentials: jsonInsecure},
			want: false,
		},
		{
			name: "different_call_creds",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure},
			b: &Config{TargetURI: target, ChannelCredentials: protoInsecure, CallCredentials: []*xdscreds.CallCreds{
				xdscreds.NewCallCreds(nil, xdscreds.Identity{Type: "access_token"}, nil),
			}},
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.a.Equal(test.b); got != test.want {
				t.Errorf("Equal() = %v, want %v", got, test.want)
			}
		})
	}
}
