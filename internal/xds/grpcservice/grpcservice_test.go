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
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
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
			want:   &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin))},
		},
		{
			name:   "trusted_xds_creds_resolve_to_fallback",
			gs:     googleGrpcService(target, []*anypb.Any{xdsWithInsecureFallback}, nil, nil),
			sc:     trustedServerConfig(t),
			config: bootstrapConfig(t, "{}"),
			want:   &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, protoIdentity(xdsWithInsecureFallback))},
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
				ChannelCredentials: xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin)),
				CallCredentials:    []xdscreds.BuildableCallCreds{xdscreds.NewCallCreds(nil, protoIdentity(tokenPlugin))},
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
			want:   &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"})},
		},
		{
			// The proto's call-creds plugins are ignored along with its
			// channel-creds plugins: the allowlist's call creds are used.
			name:   "untrusted_allowlisted_uses_allowlist_call_creds",
			gs:     googleGrpcService(target, []*anypb.Any{insecurePlugin}, []*anypb.Any{tokenPlugin}, nil),
			sc:     untrustedServerConfig(t),
			config: bootstrapConfig(t, `{"dns:///my-service:443":{"channel_creds":[{"type":"insecure"}],"call_creds":[{"type":"jwt_token_file","config":{"jwt_token_file":"/tokens/token"}}]}}`),
			want: &Config{
				TargetURI:          target,
				ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"}),
				CallCredentials: []xdscreds.BuildableCallCreds{xdscreds.NewCallCreds(nil, xdscreds.Identity{
					Type: "jwt_token_file",
					// Bootstrap parsing normalizes the configuration JSON
					// via json.Indent, and the identity carries the
					// normalized bytes.
					Data: json.RawMessage("{\n\"jwt_token_file\": \"/tokens/token\"\n}"),
				})},
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
				ChannelCredentials: xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin)),
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
				ChannelCredentials: xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin)),
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
	if _, _, err := cfg.Dial(); err == nil || !strings.Contains(err.Error(), "transport level security") {
		t.Fatalf("Dial() returned error %v, want transport security error", err)
	}
}

// Tests that a Config carrying TLS channel credentials and access token call
// credentials, built from a trusted server's GrpcService proto, can dial a
// TLS-serving backend, and that RPCs on the resulting channel succeed and
// carry the token.
func (s) TestConfigDialTLS(t *testing.T) {
	// Start a TLS-serving backend that verifies the access token arrives.
	const wantAuthorization = "Bearer test-token"
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
	cfg, err := Parse(gs, bc, trustedServerConfig(t))
	if err != nil {
		t.Fatalf("Parse() returned unexpected error: %v", err)
	}

	// The backend certificate's SANs cover x.test.example.com.
	cc, release, err := cfg.Dial(grpc.WithAuthority("x.test.example.com"))
	if err != nil {
		t.Fatalf("Dial() returned unexpected error: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := testgrpc.NewTestServiceClient(cc).EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}
}

func (s) TestConfigEqualAndKey(t *testing.T) {
	insecurePlugin := &anypb.Any{TypeUrl: insecureCredsTypeURL}
	protoInsecure := xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin))
	jsonInsecure := xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"})
	callCredA := xdscreds.NewCallCreds(nil, xdscreds.Identity{Type: "access_token", Data: []byte("token-a")})
	callCredB := xdscreds.NewCallCreds(nil, xdscreds.Identity{Type: "access_token", Data: []byte("token-b")})

	tests := []struct {
		name string
		a, b *Config
		want bool
	}{
		{
			name: "equal",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, Timeout: 1, InitialMetadata: metadata.Pairs("k", "v"), CallCredentials: []xdscreds.BuildableCallCreds{callCredA}},
			b:    &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, protoIdentity(insecurePlugin)), Timeout: 1, InitialMetadata: metadata.Pairs("k", "v"), CallCredentials: []xdscreds.BuildableCallCreds{xdscreds.NewCallCreds(nil, xdscreds.Identity{Type: "access_token", Data: []byte("token-a")})}},
			want: true,
		},
		{
			name: "equal_nil_and_empty_identity_data",
			a:    &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure", Data: nil})},
			b:    &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure", Data: []byte{}})},
			want: true,
		},
		{
			name: "equal_nil_and_empty_metadata_and_call_creds",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, InitialMetadata: nil, CallCredentials: nil},
			b:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, InitialMetadata: metadata.MD{}, CallCredentials: []xdscreds.BuildableCallCreds{}},
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
			a:    &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "tls", Data: []byte("a")})},
			b:    &Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "tls", Data: []byte("b")})},
			want: false,
		},
		{
			name: "different_call_creds",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure},
			b:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, CallCredentials: []xdscreds.BuildableCallCreds{callCredA}},
			want: false,
		},
		{
			name: "different_call_creds_data",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, CallCredentials: []xdscreds.BuildableCallCreds{callCredA}},
			b:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, CallCredentials: []xdscreds.BuildableCallCreds{callCredB}},
			want: false,
		},
		{
			name: "different_call_creds_order",
			a:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, CallCredentials: []xdscreds.BuildableCallCreds{callCredA, callCredB}},
			b:    &Config{TargetURI: target, ChannelCredentials: protoInsecure, CallCredentials: []xdscreds.BuildableCallCreds{callCredB, callCredA}},
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

// lifecycleChannelCreds counts per-channel acquisitions independently of
// provider deduplication.
type lifecycleChannelCreds struct {
	builds    atomic.Int32
	releases  atomic.Int32
	fail      bool
	onRelease func()
}

func (*lifecycleChannelCreds) Identity() xdscreds.Identity {
	return xdscreds.Identity{Type: "test-channel"}
}

func (c *lifecycleChannelCreds) Build() (credentials.Bundle, func(), error) {
	c.builds.Add(1)
	if c.fail {
		return nil, nil, errors.New("channel build failed")
	}
	return insecure.NewBundle(), func() {
		c.releases.Add(1)
		if c.onRelease != nil {
			c.onRelease()
		}
	}, nil
}

type lifecycleCallCreds struct {
	builds   atomic.Int32
	releases atomic.Int32
	fail     bool
	secure   bool
}

func (*lifecycleCallCreds) Identity() xdscreds.Identity {
	return xdscreds.Identity{Type: "test-call"}
}

func (c *lifecycleCallCreds) Build() (credentials.PerRPCCredentials, func(), error) {
	c.builds.Add(1)
	if c.fail {
		return nil, nil, errors.New("call build failed")
	}
	return &lifecyclePerRPCCreds{secure: c.secure}, func() { c.releases.Add(1) }, nil
}

type lifecyclePerRPCCreds struct{ secure bool }

func (*lifecyclePerRPCCreds) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return nil, nil
}
func (c *lifecyclePerRPCCreds) RequireTransportSecurity() bool { return c.secure }

// Every successful Build is balanced on success, partial failure, and client
// construction failure. Release closes the channel first, exactly once.
func (s) TestConfigDialLifecycle(t *testing.T) {
	tests := []struct {
		name                string
		channelFail         bool
		callFail            int
		clientFail          bool
		wantChannelReleases int32
		wantCallBuilds      [2]int32
		wantCallReleases    [2]int32
	}{
		{name: "success", callFail: -1, wantChannelReleases: 1, wantCallBuilds: [2]int32{1, 1}, wantCallReleases: [2]int32{1, 1}},
		{name: "channel_build_failure", channelFail: true, callFail: -1},
		{name: "first_call_failure", callFail: 0, wantChannelReleases: 1, wantCallBuilds: [2]int32{1, 0}},
		{name: "later_call_failure", callFail: 1, wantChannelReleases: 1, wantCallBuilds: [2]int32{1, 1}, wantCallReleases: [2]int32{1, 0}},
		{name: "client_creation_failure", callFail: -1, clientFail: true, wantChannelReleases: 1, wantCallBuilds: [2]int32{1, 1}, wantCallReleases: [2]int32{1, 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := &lifecycleChannelCreds{fail: test.channelFail}
			calls := [2]*lifecycleCallCreds{{fail: test.callFail == 0, secure: test.clientFail}, {fail: test.callFail == 1}}
			cfg := &Config{TargetURI: target, ChannelCredentials: channel, CallCredentials: []xdscreds.BuildableCallCreds{calls[0], calls[1]}}
			conn, release, err := cfg.Dial()
			wantErr := test.channelFail || test.callFail >= 0 || test.clientFail
			if (err != nil) != wantErr {
				t.Fatalf("Dial error = %v, want failure %v", err, wantErr)
			}
			if wantErr {
				if conn != nil || release != nil {
					t.Fatal("Dial returned a channel or release with an error")
				}
			} else {
				t.Cleanup(release)
				channel.onRelease = func() {
					if state := conn.(*grpc.ClientConn).GetState(); state != connectivity.Shutdown {
						t.Errorf("Credential cleanup ran before channel shutdown: %v", state)
					}
				}
				var wg sync.WaitGroup
				for range 10 {
					wg.Add(1)
					go func() { defer wg.Done(); release() }()
				}
				wg.Wait()
				release()
			}
			if got := channel.builds.Load(); got != 1 {
				t.Errorf("Channel builds = %d, want 1", got)
			}
			if got := channel.releases.Load(); got != test.wantChannelReleases {
				t.Errorf("Channel releases = %d, want %d", got, test.wantChannelReleases)
			}
			for i, call := range calls {
				if got := call.builds.Load(); got != test.wantCallBuilds[i] {
					t.Errorf("Call %d builds = %d, want %d", i, got, test.wantCallBuilds[i])
				}
				if got := call.releases.Load(); got != test.wantCallReleases[i] {
					t.Errorf("Call %d releases = %d, want %d", i, got, test.wantCallReleases[i])
				}
			}
		})
	}
}

func (s) TestConfigDialIndependentLifetimes(t *testing.T) {
	channel := &lifecycleChannelCreds{}
	call := &lifecycleCallCreds{}
	cfg := &Config{TargetURI: target, ChannelCredentials: channel, CallCredentials: []xdscreds.BuildableCallCreds{call}}
	_, release1, err := cfg.Dial()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release1)
	conn2, release2, err := cfg.Dial()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release2)
	release1()
	if state := conn2.(*grpc.ClientConn).GetState(); state == connectivity.Shutdown {
		t.Fatal("Closing the first dial closed the second channel")
	}
	if got := channel.releases.Load(); got != 1 {
		t.Fatalf("Channel releases = %d, want 1", got)
	}
	release2()
	if channel.builds.Load() != 2 || channel.releases.Load() != 2 || call.builds.Load() != 2 || call.releases.Load() != 2 {
		t.Fatal("Independent channel acquisitions were not all released")
	}
}

const lifecycleChannelTypeURL = "type.googleapis.com/grpc.testing.LifecycleChannelCredentials"

func init() {
	// This parser is intentionally resource-free. Tests can inspect its Build
	// counter to detect accidental acquisition by grpcservice.Parse.
	xdscreds.RegisterChannelCredsParser(lifecycleChannelTypeURL, func(*anypb.Any, xdscreds.CertProviderConfigResolver) (xdscreds.BuildableChannelCreds, error) {
		return &lifecycleChannelCreds{}, nil
	})
}

func (s) TestParseDoesNotBuildCredentials(t *testing.T) {
	bc, sc := bootstrapConfig(t, "{}"), trustedServerConfig(t)
	for range 10 {
		gs := googleGrpcService(target, []*anypb.Any{{TypeUrl: lifecycleChannelTypeURL}}, nil, nil)
		cfg, err := Parse(gs, bc, sc)
		if err != nil {
			t.Fatal(err)
		}
		if got := cfg.ChannelCredentials.(*lifecycleChannelCreds).builds.Load(); got != 0 {
			t.Fatalf("Parse built credentials %d times, want zero", got)
		}
		// A rejected call plugin after a valid channel plugin must not acquire
		// anything either. No Config.Close is necessary on this error path.
		gs.GetGoogleGrpc().CallCredentialsPlugin = []*anypb.Any{accessTokenPlugin(t, "")}
		if _, err := Parse(gs, bc, sc); err == nil {
			t.Fatal("Parse accepted an empty access token")
		}
	}
}

func (s) TestParseCopiesCredentialIdentity(t *testing.T) {
	channelPlugin := &anypb.Any{TypeUrl: insecureCredsTypeURL}
	callPlugin := accessTokenPlugin(t, "test-token")
	cfg, err := Parse(googleGrpcService(target, []*anypb.Any{channelPlugin}, []*anypb.Any{callPlugin}, nil), bootstrapConfig(t, "{}"), trustedServerConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	key := cfg.Key()
	callPlugin.Value[0] ^= 0xff
	id := cfg.CallCredentials[0].Identity()
	id.Data[0] ^= 0xff
	if cfg.Key() != key {
		t.Fatal("Mutating input or returned identity changed the config key")
	}
}

// Closing a dial only releases the borrow. Bootstrap remains the owner of
// allowlisted credentials, which are safe to use for another dial.
func (s) TestConfigDialBootstrapCredentials(t *testing.T) {
	bc := bootstrapConfig(t, fmt.Sprintf("{%q:{\"channel_creds\":[{\"type\":\"insecure\"}]}}", target))
	cfg, err := Parse(googleGrpcService(target, nil, nil, nil), bc, untrustedServerConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	bundle1, release1, err := cfg.ChannelCredentials.Build()
	if err != nil {
		t.Fatal(err)
	}
	release1()
	bundle2, release2, err := cfg.ChannelCredentials.Build()
	if err != nil {
		t.Fatal(err)
	}
	defer release2()
	if bundle1 != bundle2 {
		t.Fatal("Bootstrap credentials were rebuilt instead of borrowed")
	}
	_, close1, err := cfg.Dial()
	if err != nil {
		t.Fatal(err)
	}
	close1()
	_, close2, err := cfg.Dial()
	if err != nil {
		t.Fatal(err)
	}
	close2()
}
