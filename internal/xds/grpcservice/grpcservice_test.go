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
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/bootstrap"
	xdscreds "google.golang.org/grpc/internal/xds/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/testdata"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	accesstokenpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/call_credentials/access_token/v3"
	tlscredspb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/tls/v3"
	xdscredspb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/xds/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
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

	defaultTestTimeout = 10 * time.Second
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

// withInitialMetadata sets the initial metadata of gs and returns it.
func withInitialMetadata(gs *v3corepb.GrpcService, headers ...*v3corepb.HeaderValue) *v3corepb.GrpcService {
	gs.InitialMetadata = headers
	return gs
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
	headers := []*v3corepb.HeaderValue{
		{Key: "key-b", Value: "b"},
		// raw_value takes precedence over the legacy value field.
		{Key: "key-a", Value: "legacy", RawValue: []byte("raw-a")},
		// The values of "-bin" headers are base64-encoded in the proto.
		{Key: "key-bin", RawValue: []byte("AP8=")},
	}
	invalidHeader := &v3corepb.HeaderValue{Key: "grpc-key", Value: "v"}
	invalidBinHeader := &v3corepb.HeaderValue{Key: "key-bin", RawValue: []byte("not base64!")}

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
			// Call-creds plugins of unsupported types are skipped,
			// while supported ones are built in order.
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
			// The proto's call-creds plugins are ignored along with
			// its channel-creds plugins: the allowlist's call creds
			// are used.
			name:   "untrusted_allowlisted_uses_allowlist_call_creds",
			gs:     googleGrpcService(target, []*anypb.Any{insecurePlugin}, []*anypb.Any{tokenPlugin}, nil),
			sc:     untrustedServerConfig(t),
			config: bootstrapConfig(t, `{"dns:///my-service:443":{"channel_creds":[{"type":"insecure"}],"call_creds":[{"type":"jwt_token_file","config":{"jwt_token_file":"/tokens/token"}}]}}`),
			want: &Config{
				TargetURI:          target,
				ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"}, nil),
				CallCredentials: []*xdscreds.CallCreds{xdscreds.NewCallCreds(nil, xdscreds.Identity{
					Type: "jwt_token_file",
					// Bootstrap parsing normalizes the
					// configuration JSON via json.Indent,
					// and the identity carries the
					// normalized bytes.
					Data: json.RawMessage("{\n\"jwt_token_file\": \"/tokens/token\"\n}"),
				}, nil)},
			},
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
			// Unlike initial metadata and credentials, the timeout
			// does not depend on trust.
			name:   "untrusted_timeout_honored",
			gs:     googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil, durationpb.New(10*time.Second)),
			sc:     untrustedServerConfig(t),
			config: bootstrapConfig(t, allowedInsecure),
			want: &Config{
				TargetURI:          target,
				Timeout:            10 * time.Second,
				ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"}, nil),
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
			name:    "untrusted_zero_timeout_rejected",
			gs:      googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil, durationpb.New(0)),
			sc:      untrustedServerConfig(t),
			config:  bootstrapConfig(t, allowedInsecure),
			wantErr: "timeout must be strictly positive",
		},
		{
			name:   "trusted_initial_metadata",
			gs:     withInitialMetadata(googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil, nil), headers...),
			sc:     trustedServerConfig(t),
			config: bootstrapConfig(t, "{}"),
			want: &Config{
				TargetURI:          target,
				InitialMetadata:    metadata.MD{"key-b": {"b"}, "key-a": {"raw-a"}, "key-bin": {"\x00\xff"}},
				ChannelCredentials: xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin), nil),
			},
		},
		{
			name:    "trusted_invalid_initial_metadata",
			gs:      withInitialMetadata(googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil, nil), invalidHeader),
			sc:      trustedServerConfig(t),
			config:  bootstrapConfig(t, "{}"),
			wantErr: "invalid header key",
		},
		{
			name:    "trusted_invalid_binary_initial_metadata",
			gs:      withInitialMetadata(googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil, nil), invalidBinHeader),
			sc:      trustedServerConfig(t),
			config:  bootstrapConfig(t, "{}"),
			wantErr: "not valid base64",
		},
		{
			// The proto's initial metadata is ignored along with
			// its credentials.
			name:   "untrusted_initial_metadata_ignored",
			gs:     withInitialMetadata(googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil, nil), headers...),
			sc:     untrustedServerConfig(t),
			config: bootstrapConfig(t, allowedInsecure),
			want:   &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"}, nil)},
		},
		{
			// Ignored initial metadata is not validated.
			name:   "untrusted_invalid_initial_metadata_ignored",
			gs:     withInitialMetadata(googleGrpcService(target, []*anypb.Any{insecurePlugin}, nil, nil), invalidHeader, invalidBinHeader),
			sc:     untrustedServerConfig(t),
			config: bootstrapConfig(t, allowedInsecure),
			want:   &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"}, nil)},
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
			// The cmp.Diff below compares credentials by identity
			// only, so it cannot tell a built credential from a nil
			// one; verify the credentials were built.
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

// Tests the validation of initial_metadata HeaderValue protos and their
// conversion to metadata.MD.
func (s) TestParseInitialMetadata(t *testing.T) {
	// 12288 bytes are 16384 bytes once base64-encoded, i.e. the limit.
	maxBinValue := base64.StdEncoding.EncodeToString(make([]byte, 12288))

	tests := []struct {
		name    string
		headers []*v3corepb.HeaderValue
		want    metadata.MD
		wantErr string
	}{
		{
			name: "none",
		},
		{
			name:    "value",
			headers: []*v3corepb.HeaderValue{{Key: "key", Value: "v"}},
			want:    metadata.MD{"key": {"v"}},
		},
		{
			name:    "raw_value_takes_precedence",
			headers: []*v3corepb.HeaderValue{{Key: "key", Value: "legacy", RawValue: []byte("raw")}},
			want:    metadata.MD{"key": {"raw"}},
		},
		{
			name:    "repeated_key_preserves_order",
			headers: []*v3corepb.HeaderValue{{Key: "key", Value: "1"}, {Key: "key", Value: "2"}},
			want:    metadata.MD{"key": {"1", "2"}},
		},
		{
			// Only the values of "-bin" headers are base64-decoded.
			name:    "value_that_looks_like_base64",
			headers: []*v3corepb.HeaderValue{{Key: "key", Value: "AP8="}},
			want:    metadata.MD{"key": {"AP8="}},
		},
		{
			name:    "bin_padded",
			headers: []*v3corepb.HeaderValue{{Key: "key-bin", RawValue: []byte("AP8=")}},
			want:    metadata.MD{"key-bin": {"\x00\xff"}},
		},
		{
			name:    "bin_unpadded",
			headers: []*v3corepb.HeaderValue{{Key: "key-bin", RawValue: []byte("AP8")}},
			want:    metadata.MD{"key-bin": {"\x00\xff"}},
		},
		{
			name:    "bin_legacy_value",
			headers: []*v3corepb.HeaderValue{{Key: "key-bin", Value: "AP8="}},
			want:    metadata.MD{"key-bin": {"\x00\xff"}},
		},
		{
			name:    "bin_raw_value_takes_precedence",
			headers: []*v3corepb.HeaderValue{{Key: "key-bin", Value: "bGVnYWN5", RawValue: []byte("AP8=")}},
			want:    metadata.MD{"key-bin": {"\x00\xff"}},
		},
		{
			name:    "bin_empty",
			headers: []*v3corepb.HeaderValue{{Key: "key-bin"}},
			want:    metadata.MD{"key-bin": {""}},
		},
		{
			name:    "bin_max_length",
			headers: []*v3corepb.HeaderValue{{Key: "key-bin", RawValue: []byte(maxBinValue)}},
			want:    metadata.MD{"key-bin": {string(make([]byte, 12288))}},
		},
		{
			// The length limit applies to the value as encoded in
			// the proto: these 16388 bytes would decode to 12291.
			name:    "bin_too_long",
			headers: []*v3corepb.HeaderValue{{Key: "key-bin", RawValue: []byte(maxBinValue + "AAAA")}},
			wantErr: "exceeds maximum allowed length",
		},
		{
			name:    "bin_invalid_base64",
			headers: []*v3corepb.HeaderValue{{Key: "key-bin", RawValue: []byte("not base64!")}},
			wantErr: "not valid base64",
		},
		{
			name:    "bin_url_safe_alphabet",
			headers: []*v3corepb.HeaderValue{{Key: "key-bin", RawValue: []byte("AP8-_w")}},
			wantErr: "not valid base64",
		},
		{
			name:    "value_too_long",
			headers: []*v3corepb.HeaderValue{{Key: "key", Value: strings.Repeat("v", maxHeaderValueLen+1)}},
			wantErr: "exceeds maximum allowed length",
		},
		{
			name:    "value_not_printable",
			headers: []*v3corepb.HeaderValue{{Key: "key", RawValue: []byte{0x00, 0xff}}},
			wantErr: "non-printable",
		},
		{
			name:    "key_empty",
			headers: []*v3corepb.HeaderValue{{Value: "v"}},
			wantErr: "invalid header key",
		},
		{
			name:    "key_uppercase",
			headers: []*v3corepb.HeaderValue{{Key: "Key", Value: "v"}},
			wantErr: "invalid header key",
		},
		{
			name:    "key_too_long",
			headers: []*v3corepb.HeaderValue{{Key: strings.Repeat("k", maxHeaderKeyLen+1), Value: "v"}},
			wantErr: "exceeds maximum allowed length",
		},
		{
			name:    "key_host",
			headers: []*v3corepb.HeaderValue{{Key: "host", Value: "v"}},
			wantErr: `cannot be "host"`,
		},
		{
			name:    "key_pseudo_header",
			headers: []*v3corepb.HeaderValue{{Key: ":authority", Value: "v"}},
			wantErr: `cannot start with ":"`,
		},
		{
			name:    "key_grpc_prefix",
			headers: []*v3corepb.HeaderValue{{Key: "grpc-key", Value: "v"}},
			wantErr: `cannot start with "grpc-"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseInitialMetadata(test.headers)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("parseInitialMetadata() error = %v, want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseInitialMetadata() returned unexpected error: %v", err)
			}
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("parseInitialMetadata() returned unexpected metadata (-want +got):\n%s", diff)
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

// Tests that a Config carrying TLS channel credentials and access token call
// credentials, built from a trusted server's GrpcService proto, can dial a
// TLS-serving backend, and that RPCs on the resulting channel succeed and
// carry the token and the initial metadata.
func (s) TestConfigDialTLS(t *testing.T) {
	// Start a TLS-serving backend that verifies the access token and the
	// initial metadata arrive.
	const wantAuthorization = "Bearer test-token"
	const wantBinValue = "\x00\xff"
	serverCreds, err := credentials.NewServerTLSFromFile(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		t.Fatalf("Failed to create server TLS credentials: %v", err)
	}
	backend := stubserver.StartTestService(t, &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return nil, fmt.Errorf("no incoming metadata")
			}
			if got := md.Get("authorization"); len(got) != 1 || got[0] != wantAuthorization {
				return nil, fmt.Errorf("authorization metadata = %v, want [%q]", got, wantAuthorization)
			}
			if got := md.Get("key-bin"); len(got) != 1 || got[0] != wantBinValue {
				return nil, fmt.Errorf("key-bin metadata = %q, want [%q]", got, wantBinValue)
			}
			return &testpb.Empty{}, nil
		},
	}, grpc.Creds(serverCreds))
	defer backend.Stop()

	// Bootstrap configuration with a certificate provider instance watching
	// the CA certificate that signed the backend's certificate.
	contents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: json.RawMessage(`[{"server_uri":"td.googleapis.com:443","channel_creds":[{"type":"insecure"}]}]`),
		Node:    json.RawMessage(`{}`),
		CertificateProviders: map[string]json.RawMessage{
			"root-instance": json.RawMessage(fmt.Sprintf(`{
				"plugin_name": "file_watcher",
				"config": {"ca_certificate_file": %q}
			}`, testdata.Path("x509/server_ca_cert.pem"))),
		},
	})
	if err != nil {
		t.Fatalf("NewContentsForTesting() failed: %v", err)
	}
	bc, err := bootstrap.NewConfigFromContents(contents)
	if err != nil {
		t.Fatalf("NewConfigFromContents() failed: %v", err)
	}

	tlsPlugin, err := anypb.New(&tlscredspb.TlsCredentials{
		RootCertificateProvider: &v3tlspb.CommonTlsContext_CertificateProviderInstance{InstanceName: "root-instance"},
	})
	if err != nil {
		t.Fatalf("Failed to marshal TlsCredentials: %v", err)
	}
	gs := googleGrpcService(backend.Address, []*anypb.Any{tlsPlugin}, []*anypb.Any{accessTokenPlugin(t, "test-token")}, nil)
	// The value is base64-encoded in the proto and decoded by Parse; the
	// transport encodes it again on the wire.
	gs = withInitialMetadata(gs, &v3corepb.HeaderValue{Key: "key-bin", RawValue: []byte(base64.StdEncoding.EncodeToString([]byte(wantBinValue)))})
	cfg, err := Parse(gs, bc, trustedServerConfig(t))
	if err != nil {
		t.Fatalf("Parse() returned unexpected error: %v", err)
	}
	defer cfg.Close()

	// The backend certificate's SANs cover x.test.example.com.
	cc, err := cfg.Dial(grpc.WithAuthority("x.test.example.com"))
	if err != nil {
		t.Fatalf("Dial() returned unexpected error: %v", err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Consumers of a Config attach its initial metadata to each RPC.
	ctx = metadata.NewOutgoingContext(ctx, cfg.InitialMetadata)
	if _, err := testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

func (s) TestConfigEqualAndKey(t *testing.T) {
	insecurePlugin := &anypb.Any{TypeUrl: insecureCredsTypeURL}
	protoInsecure := xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin), nil)
	jsonInsecure := xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"}, nil)
	callCredA := xdscreds.NewCallCreds(nil, xdscreds.Identity{Type: "access_token", Data: []byte("token-a")}, nil)
	callCredB := xdscreds.NewCallCreds(nil, xdscreds.Identity{Type: "access_token", Data: []byte("token-b")}, nil)

	tests := []struct {
		name string
		a, b *Config
		want bool
	}{
		{
			name: "equal",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, Timeout: 1, InitialMetadata: metadata.Pairs("k", "v"), CallCredentials: []*xdscreds.CallCreds{callCredA}},
			b:    &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin), nil), Timeout: 1, InitialMetadata: metadata.Pairs("k", "v"), CallCredentials: []*xdscreds.CallCreds{xdscreds.NewCallCreds(nil, xdscreds.Identity{Type: "access_token", Data: []byte("token-a")}, nil)}},
			want: true,
		},
		{
			name: "equal_nil_and_empty_identity_data",
			a:    &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure", Data: nil}, nil)},
			b:    &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure", Data: []byte{}}, nil)},
			want: true,
		},
		{
			name: "equal_nil_and_empty_metadata_and_call_creds",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, InitialMetadata: nil, CallCredentials: nil},
			b:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, InitialMetadata: metadata.MD{}, CallCredentials: []*xdscreds.CallCreds{}},
			want: true,
		},
		{
			name: "equal_multiple_metadata_keys",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, InitialMetadata: metadata.Pairs("k1", "v1", "k2", "v2", "k1", "v3")},
			b:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, InitialMetadata: metadata.Pairs("k2", "v2", "k1", "v1", "k1", "v3")},
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
			name: "different_initial_metadata_value_order",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, InitialMetadata: metadata.Pairs("k", "v1", "k", "v2")},
			b:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, InitialMetadata: metadata.Pairs("k", "v2", "k", "v1")},
			want: false,
		},
		{
			name: "equal_nil_and_empty_metadata_values",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, InitialMetadata: metadata.MD{"k": nil}},
			b:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, InitialMetadata: metadata.MD{"k": {}}},
			want: true,
		},
		{
			// Both values are invalid UTF-8: encoding them as JSON strings
			// would map both to "\ufffd\u0001".
			name: "different_binary_metadata_invalid_utf8",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, InitialMetadata: metadata.Pairs("k-bin", "\x80\x01")},
			b:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, InitialMetadata: metadata.Pairs("k-bin", "\x81\x01")},
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
			name: "different_channel_creds_data",
			a:    &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "tls", Data: []byte("a")}, nil)},
			b:    &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "tls", Data: []byte("b")}, nil)},
			want: false,
		},
		{
			name: "different_call_creds",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure},
			b:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, CallCredentials: []*xdscreds.CallCreds{callCredA}},
			want: false,
		},
		{
			name: "different_call_creds_data",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, CallCredentials: []*xdscreds.CallCreds{callCredA}},
			b:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, CallCredentials: []*xdscreds.CallCreds{callCredB}},
			want: false,
		},
		{
			name: "different_call_creds_order",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, CallCredentials: []*xdscreds.CallCreds{callCredA, callCredB}},
			b:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, CallCredentials: []*xdscreds.CallCreds{callCredB, callCredA}},
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.a.Equal(test.b); got != test.want {
				t.Errorf("Equal() = %v, want %v", got, test.want)
			}
			keyA, keyB := test.a.Key(), test.b.Key()
			if got := (keyA == keyB); got != test.want {
				t.Errorf("Key() equality = %v (a.Key() = %q, b.Key() = %q), want %v", got, keyA, keyB, test.want)
			}
		})
	}
}
