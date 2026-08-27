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
	"time"

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

// untrustedServerConfig returns a server config without the
// trusted_xds_server server feature.
func untrustedServerConfig(t *testing.T) *bootstrap.ServerConfig {
	t.Helper()
	sc, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{URI: "untrusted-server:443"})
	if err != nil {
		t.Fatalf("ServerConfigForTesting() failed: %v", err)
	}
	return sc
}

func googleGrpcService(target string, channelPlugins, callPlugins []*anypb.Any, timeout *durationpb.Duration) *v3corepb.GrpcService {
	return &v3corepb.GrpcService{
		TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
			GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
				TargetUri:                target,
				ChannelCredentialsPlugin: channelPlugins,
				CallCredentialsPlugin:    callPlugins,
			},
		},
		Timeout: timeout,
	}
}

// protoIdentity returns the identity of a proto plugin config.
func protoIdentity(a *anypb.Any) xdscreds.Identity {
	return xdscreds.Identity{Type: a.GetTypeUrl(), Data: a.GetValue()}
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
	tokenPlugin := accessTokenPlugin(t, "test-token")
	unsupportedCallPlugin := &anypb.Any{TypeUrl: "type.googleapis.com/unsupported.CallCredentials"}
	allowedInsecure := `{"dns:///my-service:443":{"channel_creds":[{"type":"insecure"}]}}`

	tests := []struct {
		name   string
		gs     *v3corepb.GrpcService
		sc     *bootstrap.ServerConfig
		config *bootstrap.Config
		// want carries the expected target and credential identities;
		// comparisons use Config.Equal via cmp.Diff.
		want    *Config
		wantErr string
	}{
		{
			name:   "trusted_insecure_channel_creds",
			gs:     googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil, nil),
			sc:     trustedServerConfig(t),
			config: bootstrapConfig(t, "{}"),
			want:   &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin), nil)},
		},
		{
			name:   "trusted_xds_creds_resolve_to_fallback",
			gs:     googleGrpcService(target, []*anypb.Any{xdsWithInsecureFallback}, nil, nil),
			sc:     trustedServerConfig(t),
			config: bootstrapConfig(t, "{}"),
			want:   &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, protoIdentity(xdsWithInsecureFallback), nil)},
		},
		{
			// Call-creds plugins of unsupported types are skipped, while
			// supported ones are built in order.
			name:   "trusted_with_call_creds",
			gs:     googleGrpcService(target, []*anypb.Any{insecurePlugin}, []*anypb.Any{tokenPlugin, unsupportedCallPlugin}, nil),
			sc:     trustedServerConfig(t),
			config: bootstrapConfig(t, "{}"),
			want: &Config{
				TargetURI:          target,
				ChannelCredentials: xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin), nil),
				CallCredentials:    []*xdscreds.CallCreds{xdscreds.NewCallCreds(nil, protoIdentity(tokenPlugin), nil)},
			},
		},
		{
			name:    "trusted_empty_call_creds_token",
			gs:      googleGrpcService(target, []*anypb.Any{insecurePlugin}, []*anypb.Any{accessTokenPlugin(t, "")}, nil),
			sc:      trustedServerConfig(t),
			config:  bootstrapConfig(t, "{}"),
			wantErr: "access token must be non-empty",
		},
		{
			name:    "trusted_no_supported_channel_creds",
			gs:      googleGrpcService(target, nil, nil, nil),
			sc:      trustedServerConfig(t),
			config:  bootstrapConfig(t, "{}"),
			wantErr: "no supported channel credentials",
		},
		{
			name:   "untrusted_allowlisted_uses_allowlist_creds",
			gs:     googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil, nil),
			sc:     untrustedServerConfig(t),
			config: bootstrapConfig(t, allowedInsecure),
			want:   &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"}, nil)},
		},
		{
			name:    "untrusted_not_allowlisted",
			gs:      googleGrpcService(target, nil, nil, nil),
			sc:      untrustedServerConfig(t),
			config:  bootstrapConfig(t, "{}"),
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
			gs:      googleGrpcService("", nil, nil, nil),
			sc:      trustedServerConfig(t),
			config:  bootstrapConfig(t, "{}"),
			wantErr: "target_uri must be non-empty",
		},
		{
			name:   "valid_timeout",
			gs:     googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil, durationpb.New(10*time.Second)),
			sc:     trustedServerConfig(t),
			config: bootstrapConfig(t, "{}"),
			want: &Config{
				TargetURI:          target,
				Timeout:            10 * time.Second,
				ChannelCredentials: xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin), nil),
			},
		},
		{
			name:    "zero_timeout_rejected",
			gs:      googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil, durationpb.New(0)),
			sc:      trustedServerConfig(t),
			config:  bootstrapConfig(t, "{}"),
			wantErr: "timeout must be strictly positive",
		},
		{
			name: "initial_metadata",
			gs: func() *v3corepb.GrpcService {
				gs := googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil, nil)
				gs.InitialMetadata = []*v3corepb.HeaderValue{
					{Key: "key-b", Value: "b"},
					// raw_value takes precedence over the legacy value field.
					{Key: "key-a", Value: "legacy", RawValue: []byte("raw-a")},
				}
				return gs
			}(),
			sc:     trustedServerConfig(t),
			config: bootstrapConfig(t, "{}"),
			want: &Config{
				TargetURI:          target,
				InitialMetadata:    metadata.MD{"key-b": {"b"}, "key-a": {"raw-a"}},
				ChannelCredentials: xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin), nil),
			},
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
			if got.ChannelCredentials.Bundle() == nil {
				t.Error("Parse() returned channel credentials without a built bundle")
			}
			for i, cc := range got.CallCredentials {
				if cc.Credentials() == nil {
					t.Errorf("Parse() call credentials[%d] have no built credentials", i)
				}
			}
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Parse() returned unexpected config (-want +got):\n%s", diff)
			}
		})
	}
}

// Tests that Dial applies the config's credentials to the created channel:
// call credentials that require transport security combined with insecure
// channel credentials must fail channel creation.
func (s) TestConfigDial(t *testing.T) {
	gs := googleGrpcService(target, []*anypb.Any{{TypeUrl: insecureCredsTypeURL}}, []*anypb.Any{accessTokenPlugin(t, "test-token")}, nil)
	cfg, err := Parse(gs, bootstrapConfig(t, "{}"), trustedServerConfig(t))
	if err != nil {
		t.Fatalf("Parse() returned unexpected error: %v", err)
	}
	defer cfg.Close()
	if _, err := cfg.Dial(); err == nil || !strings.Contains(err.Error(), "transport level security") {
		t.Fatalf("Dial() returned error %v, want transport security error", err)
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
			name: "equal",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, Timeout: 1, InitialMetadata: metadata.Pairs("k", "v")},
			b:    &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin), nil), Timeout: 1, InitialMetadata: metadata.Pairs("k", "v")},
			want: true,
		},
		{
			name: "different_timeouts",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, Timeout: 1},
			b:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, Timeout: 2},
			want: false,
		},
		{
			name: "different_initial_metadata",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure},
			b:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, InitialMetadata: metadata.Pairs("k", "v")},
			want: false,
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
