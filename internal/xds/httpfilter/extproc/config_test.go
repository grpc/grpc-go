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

package extproc

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/optional"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/bootstrap"
	xdscreds "google.golang.org/grpc/internal/xds/credentials"
	"google.golang.org/grpc/internal/xds/grpcservice"
	"google.golang.org/grpc/internal/xds/httpfilter"
	iextproc "google.golang.org/grpc/internal/xds/httpfilter/extproc/internal"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	fpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	testBaseURI = "base-uri"

	insecureCredsTypeURL = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.insecure.v3.InsecureCredentials"
)

// allowlistInsecureCreds carries the identity of the insecure channel
// credentials configured for allowlisted targets by testParseOptions; want
// configs compare against it by identity.
var allowlistInsecureCreds = xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"}, nil)

// testParseOptions returns ParseOptions whose bootstrap
// configuration allowlists the given side-channel targets with insecure
// channel credentials. The returned options carry a ServerConfig without the
// trusted_xds_server feature, so the delivering server is untrusted and
// GrpcService parsing takes the allowed_grpc_services path.
func testParseOptions(t *testing.T, targets ...string) httpfilter.ParseOptions {
	t.Helper()

	// The allowed_grpc_services bootstrap field is parsed only when a
	// consuming feature is enabled.
	testutils.SetEnvConfig(t, &envconfig.XDSClientExtProcEnabled, true)

	allowed := make(map[string]json.RawMessage, len(targets))
	for _, target := range targets {
		allowed[target] = json.RawMessage(`{"channel_creds": [{"type": "insecure"}]}`)
	}
	allowedJSON, err := json.Marshal(allowed)
	if err != nil {
		t.Fatalf("Failed to marshal allowed_grpc_services: %v", err)
	}
	contents, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers:             []byte(`[{"server_uri": "passthrough:///unused", "channel_creds": [{"type": "insecure"}]}]`),
		Node:                []byte(`{"id": "test-node"}`),
		AllowedGRPCServices: allowedJSON,
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap contents: %v", err)
	}
	config, err := bootstrap.NewConfigFromContents(contents)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %v", err)
	}
	untrustedServer, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{URI: "untrusted-server:1234"})
	if err != nil {
		t.Fatalf("ServerConfigForTesting() failed: %v", err)
	}
	return httpfilter.ParseOptions{BootstrapConfig: config, ServerConfig: untrustedServer}
}

// overrideCreateExtProcChannel overrides the channel creation seam to create
// insecure channels regardless of the config's credentials, and to fail
// channel creation for failTarget. The override is restored when the test
// ends.
func overrideCreateExtProcChannel(t *testing.T, failTarget string) {
	t.Helper()
	origCreateExtProcChannel := iextproc.CreateExtProcChannel
	iextproc.CreateExtProcChannel = func(cfg *grpcservice.Config) (grpc.ClientConnInterface, func(), error) {
		if failTarget != "" && cfg.TargetURI == failTarget {
			return nil, nil, fmt.Errorf("dial error")
		}
		cc, err := grpc.NewClient(cfg.TargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, nil, err
		}
		return cc, func() { cc.Close() }, nil
	}
	t.Cleanup(func() { iextproc.CreateExtProcChannel = origCreateExtProcChannel })
}

// Tests that channel sharing considers the target and the credential
// identities, and ignores the per-RPC timeout and initial metadata.
func (s) TestSharesChannel(t *testing.T) {
	insecureCreds := func() *xdscreds.ChannelCreds {
		return xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "insecure"}, nil)
	}
	const target = "dns:///proc-server:443"

	tests := []struct {
		name string
		a, b *grpcservice.Config
		want bool
	}{
		{
			name: "equal_identities_share_despite_timeout_and_metadata",
			a:    &grpcservice.Config{TargetURI: target, ChannelCredentials: insecureCreds(), Timeout: time.Second},
			b:    &grpcservice.Config{TargetURI: target, ChannelCredentials: insecureCreds(), InitialMetadata: metadata.Pairs("k", "v")},
			want: true,
		},
		{
			name: "different_targets",
			a:    &grpcservice.Config{TargetURI: target, ChannelCredentials: insecureCreds()},
			b:    &grpcservice.Config{TargetURI: "dns:///other:443", ChannelCredentials: insecureCreds()},
			want: false,
		},
		{
			name: "different_channel_creds",
			a:    &grpcservice.Config{TargetURI: target, ChannelCredentials: insecureCreds()},
			b:    &grpcservice.Config{TargetURI: target, ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "other"}, nil)},
			want: false,
		},
		{
			name: "different_call_creds",
			a:    &grpcservice.Config{TargetURI: target, ChannelCredentials: insecureCreds()},
			b: &grpcservice.Config{TargetURI: target, ChannelCredentials: insecureCreds(), CallCredentials: []*xdscreds.CallCreds{
				xdscreds.NewCallCreds(nil, xdscreds.Identity{Type: "access_token"}, nil),
			}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sharesChannel(tt.a, tt.b); got != tt.want {
				t.Errorf("sharesChannel() = %v, want %v", got, tt.want)
			}
		})
	}
}

var cmpOpts = []cmp.Option{
	cmp.AllowUnexported(
		baseConfig{},
		overrideConfig{},
		processingModes{},
		httpfilter.HeaderMutationRules{},
		optional.Optional[grpcservice.Config]{},
		optional.Optional[processingModes]{},
		optional.Optional[bool]{},
	),
	cmp.Comparer(func(a, b *xdscreds.ChannelCreds) bool {
		if a == nil || b == nil {
			return a == b
		}
		return a.Equal(b)
	}),
	cmp.Comparer(func(a, b *xdscreds.CallCreds) bool {
		if a == nil || b == nil {
			return a == b
		}
		return a.Equal(b)
	}),
	protocmp.Transform(),
	cmp.Transformer("RegexpToString", func(r *regexp.Regexp) string {
		if r == nil {
			return ""
		}
		return r.String()
	}),
	cmp.Comparer(func(x, y matcher.StringMatcher) bool {
		return x.Equal(y)
	}),
}

func (s) TestParseFilterConfig_Success(t *testing.T) {
	opts := testParseOptions(t, "localhost:1234")

	tests := []struct {
		name    string
		cfg     proto.Message
		wantCfg httpfilter.FilterConfig
	}{
		{
			name: "DefaultConfig",
			cfg: func() proto.Message {
				m, _ := anypb.New(&fpb.ExternalProcessor{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "localhost:1234",
							},
						},
					},
					ProcessingMode: &fpb.ProcessingMode{},
				})
				return m
			}(),
			wantCfg: baseConfig{
				server: grpcservice.Config{TargetURI: "localhost:1234", ChannelCredentials: allowlistInsecureCreds},
				processingModes: processingModes{
					requestHeaderMode:   modeSend,
					responseHeaderMode:  modeSend,
					responseTrailerMode: modeSkip,
					requestBodyMode:     modeSkip,
					responseBodyMode:    modeSkip,
				},
				failureModeAllow:     false,
				deferredCloseTimeout: defaultDeferredCloseTimeout,
			},
		},
		{
			name: "ConfigWithGrpcMode",
			cfg: func() proto.Message {
				m, _ := anypb.New(&fpb.ExternalProcessor{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "localhost:1234",
							},
						},
					},
					ProcessingMode: &fpb.ProcessingMode{
						RequestBodyMode:     fpb.ProcessingMode_GRPC,
						ResponseBodyMode:    fpb.ProcessingMode_GRPC,
						ResponseTrailerMode: fpb.ProcessingMode_SEND,
					},
				})
				return m
			}(),
			wantCfg: baseConfig{
				server: grpcservice.Config{TargetURI: "localhost:1234", ChannelCredentials: allowlistInsecureCreds},
				processingModes: processingModes{
					requestHeaderMode:   modeSend,
					responseHeaderMode:  modeSend,
					responseTrailerMode: modeSend,
					requestBodyMode:     modeSend,
					responseBodyMode:    modeSend,
				},
				failureModeAllow:     false,
				deferredCloseTimeout: defaultDeferredCloseTimeout,
			},
		},
		{
			name: "ConfigWithMutationRules",
			cfg: func() proto.Message {
				m, _ := anypb.New(&fpb.ExternalProcessor{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "localhost:1234",
							},
						},
					},
					ProcessingMode: &fpb.ProcessingMode{},
					MutationRules: &mutationpb.HeaderMutationRules{
						AllowExpression:    &matcherpb.RegexMatcher{Regex: ".*"},
						DisallowExpression: &matcherpb.RegexMatcher{Regex: "a"},
					},
				})
				return m
			}(),
			wantCfg: baseConfig{
				server: grpcservice.Config{TargetURI: "localhost:1234", ChannelCredentials: allowlistInsecureCreds},
				processingModes: processingModes{
					requestHeaderMode:   modeSend,
					responseHeaderMode:  modeSend,
					responseTrailerMode: modeSkip,
					requestBodyMode:     modeSkip,
					responseBodyMode:    modeSkip,
				},
				failureModeAllow: false,
				mutationRules: httpfilter.HeaderMutationRules{
					AllowExpr:    regexp.MustCompile("^(?:.*)$"),
					DisallowExpr: regexp.MustCompile("^(?:a)$"),
				},
				deferredCloseTimeout: defaultDeferredCloseTimeout,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := builder{}
			got, err := b.ParseFilterConfig(tt.cfg, opts)
			if err != nil {
				t.Fatalf("ParseFilterConfig() returned unexpected error: %v", err)
			}
			if diff := cmp.Diff(got, tt.wantCfg, cmpOpts...); diff != "" {
				t.Fatalf("ParseFilterConfig() returned unexpected config (-got +want):\n%s", diff)
			}
		})
	}
}

// Tests the gRFC A102 trust policy applied when parsing the grpc_service:
// credentials from the proto are honored only when the xDS management server
// that delivered the resource is trusted; for untrusted management servers
// the target must be present in the bootstrap allowed_grpc_services map,
// whose credentials are used instead.
func (s) TestParseFilterConfig_TrustPolicy(t *testing.T) {
	trustedServer, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{
		URI:            "trusted-server:1234",
		ServerFeatures: []string{"trusted_xds_server"},
	})
	if err != nil {
		t.Fatalf("ServerConfigForTesting() failed: %v", err)
	}
	untrustedOpts := testParseOptions(t, "localhost:1234")
	trustedOpts := httpfilter.ParseOptions{BootstrapConfig: untrustedOpts.BootstrapConfig, ServerConfig: trustedServer}

	extProcConfig := func(targetURI string, channelPlugins ...*anypb.Any) proto.Message {
		m, _ := anypb.New(&fpb.ExternalProcessor{
			GrpcService: &corepb.GrpcService{
				TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
					GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
						TargetUri:                targetURI,
						ChannelCredentialsPlugin: channelPlugins,
					},
				},
			},
			ProcessingMode: &fpb.ProcessingMode{},
		})
		return m
	}
	insecurePlugin := &anypb.Any{TypeUrl: insecureCredsTypeURL}

	tests := []struct {
		name string
		cfg  proto.Message
		opts httpfilter.ParseOptions
		// wantCreds carries only the expected credentials identity;
		// comparisons use Identity().
		wantCreds *xdscreds.ChannelCreds
		wantErr   string
	}{
		{
			name:      "trusted_uses_proto_creds",
			cfg:       extProcConfig("localhost:1234", insecurePlugin),
			opts:      trustedOpts,
			wantCreds: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: insecurePlugin.GetTypeUrl(), Data: insecurePlugin.GetValue()}, nil),
		},
		{
			name:    "trusted_requires_supported_creds",
			cfg:     extProcConfig("localhost:1234"),
			opts:    trustedOpts,
			wantErr: "no supported channel credentials found",
		},
		{
			name: "untrusted_allowlisted_uses_allowlist_creds",
			cfg:  extProcConfig("localhost:1234", insecurePlugin),
			opts: untrustedOpts,
			// The proto's credentials must be ignored in favor of the
			// allowlisted (bootstrap JSON) ones.
			wantCreds: allowlistInsecureCreds,
		},
		{
			name:    "untrusted_not_allowlisted",
			cfg:     extProcConfig("other-target:1234", insecurePlugin),
			opts:    untrustedOpts,
			wantErr: "not present in allowed_grpc_services",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := builder{}.ParseFilterConfig(tt.cfg, tt.opts)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseFilterConfig() returned error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFilterConfig() returned unexpected error: %v", err)
			}
			if gotCreds := got.(baseConfig).server.ChannelCredentials; !gotCreds.Equal(tt.wantCreds) {
				t.Fatalf("ParseFilterConfig() returned channel credentials %+v, want %+v", gotCreds, tt.wantCreds)
			}
		})
	}
}

func (s) TestParseFilterConfig_Errors(t *testing.T) {
	opts := testParseOptions(t, "localhost:1234")

	tests := []struct {
		name    string
		cfg     proto.Message
		wantErr string
	}{
		{
			name: "MissingGrpcService",
			cfg: func() proto.Message {
				m, _ := anypb.New(&fpb.ExternalProcessor{ProcessingMode: &fpb.ProcessingMode{}})
				return m
			}(),
			wantErr: "extproc: empty grpc_service provided",
		},
		{
			name: "UnsupportedGrpcService_EnvoyGrpc",
			cfg: func() proto.Message {
				m, _ := anypb.New(&fpb.ExternalProcessor{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_EnvoyGrpc_{
							EnvoyGrpc: &corepb.GrpcService_EnvoyGrpc{
								ClusterName: "cluster",
							},
						},
					},
					ProcessingMode: &fpb.ProcessingMode{},
				})
				return m
			}(),
			wantErr: "only google_grpc GrpcService config is supported",
		},
		{
			name: "MissingProcessingMode",
			cfg: func() proto.Message {
				m, _ := anypb.New(&fpb.ExternalProcessor{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "localhost:1234",
							},
						},
					},
				})
				return m
			}(),
			wantErr: "extproc: missing processing_mode",
		},
		{
			name: "InvalidProcessingMode_RequestBodyStreamed",
			cfg: func() proto.Message {
				m, _ := anypb.New(&fpb.ExternalProcessor{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "localhost:1234",
							},
						},
					},
					ProcessingMode: &fpb.ProcessingMode{RequestBodyMode: fpb.ProcessingMode_STREAMED},
				})
				return m
			}(),
			wantErr: "extproc: invalid request body mode STREAMED",
		},
		{
			name: "InvalidProcessingMode_ResponseBodyStreamed",
			cfg: func() proto.Message {
				m, _ := anypb.New(&fpb.ExternalProcessor{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "localhost:1234",
							},
						},
					},
					ProcessingMode: &fpb.ProcessingMode{ResponseBodyMode: fpb.ProcessingMode_STREAMED},
				})
				return m
			}(),
			wantErr: "extproc: invalid response body mode STREAMED",
		},
		{
			name: "InvalidProcessingMode_ResponseBodySendTrailerDefault",
			cfg: func() proto.Message {
				m, _ := anypb.New(&fpb.ExternalProcessor{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "localhost:1234",
							},
						},
					},
					ProcessingMode: &fpb.ProcessingMode{
						ResponseBodyMode:    fpb.ProcessingMode_GRPC,
						ResponseTrailerMode: fpb.ProcessingMode_DEFAULT,
					},
				})
				return m
			}(),
			wantErr: fmt.Sprintf("extproc: invalid response trailer mode DEFAULT: must be %q when response body mode is %q", "SEND", "GRPC"),
		},
		{
			name: "InvalidProcessingMode_ResponseBodySendTrailerSkip",
			cfg: func() proto.Message {
				m, _ := anypb.New(&fpb.ExternalProcessor{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "localhost:1234",
							},
						},
					},
					ProcessingMode: &fpb.ProcessingMode{
						ResponseBodyMode:    fpb.ProcessingMode_GRPC,
						ResponseTrailerMode: fpb.ProcessingMode_SKIP,
					},
				})
				return m
			}(),
			wantErr: fmt.Sprintf("extproc: invalid response trailer mode SKIP: must be %q when response body mode is %q", "SEND", "GRPC"),
		},
		{
			name: "InvalidMutationRules",
			cfg: func() proto.Message {
				m, _ := anypb.New(&fpb.ExternalProcessor{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "localhost:1234",
							},
						},
					},
					ProcessingMode: &fpb.ProcessingMode{},
					MutationRules: &mutationpb.HeaderMutationRules{
						AllowExpression: &matcherpb.RegexMatcher{Regex: "["},
					},
				})
				return m
			}(),
			wantErr: "httpfilter: error parsing regexp",
		},
		{
			name: "InvalidAllowedHeaders_EmptyPrefix",
			cfg: func() proto.Message {
				m, _ := anypb.New(&fpb.ExternalProcessor{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "localhost:1234",
							},
						},
					},
					ProcessingMode: &fpb.ProcessingMode{},
					ForwardRules: &fpb.HeaderForwardingRules{
						AllowedHeaders: &matcherpb.ListStringMatcher{
							Patterns: []*matcherpb.StringMatcher{
								{
									MatchPattern: &matcherpb.StringMatcher_Prefix{Prefix: ""},
								},
							},
						},
					},
				})
				return m
			}(),
			wantErr: "empty prefix is not allowed",
		},
		{
			name: "InvalidServerConfig_EmptyTargetURI",
			cfg: func() proto.Message {
				m, _ := anypb.New(&fpb.ExternalProcessor{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "",
							},
						},
					},
					ProcessingMode: &fpb.ProcessingMode{},
				})
				return m
			}(),
			wantErr: "target_uri must be non-empty",
		},
		{
			name:    "InvalidConfigType",
			cfg:     &fpb.ExternalProcessor{}, // Not Any
			wantErr: "extproc: error parsing config",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := builder{}
			_, err := builder.ParseFilterConfig(tt.cfg, opts)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseFilterConfig() returned error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func (s) TestParseFilterConfigOverride_Success(t *testing.T) {
	tests := []struct {
		name            string
		override        proto.Message
		wantOverrideCfg httpfilter.FilterConfig
	}{
		{
			name: "EmptyOverride",
			override: func() proto.Message {
				m, _ := anypb.New(&fpb.ExtProcPerRoute{})
				return m
			}(),
			wantOverrideCfg: overrideConfig{},
		},
		{
			name: "GrpcProcessingMode",
			override: func() proto.Message {
				m, _ := anypb.New(
					&fpb.ExtProcPerRoute{
						Override: &fpb.ExtProcPerRoute_Overrides{
							Overrides: &fpb.ExtProcOverrides{
								ProcessingMode: &fpb.ProcessingMode{
									RequestBodyMode:     fpb.ProcessingMode_GRPC,
									ResponseBodyMode:    fpb.ProcessingMode_GRPC,
									ResponseTrailerMode: fpb.ProcessingMode_SEND,
								},
							},
						},
					})
				return m
			}(),
			wantOverrideCfg: overrideConfig{
				processingModes: optional.New(processingModes{
					requestHeaderMode:   modeSend,
					responseHeaderMode:  modeSend,
					responseTrailerMode: modeSend,
					requestBodyMode:     modeSend,
					responseBodyMode:    modeSend,
				}),
			},
		},
		{
			name: "FailureModeAllow",
			override: func() proto.Message {
				m, _ := anypb.New(
					&fpb.ExtProcPerRoute{
						Override: &fpb.ExtProcPerRoute_Overrides{
							Overrides: &fpb.ExtProcOverrides{
								FailureModeAllow: wrapperspb.Bool(true),
							},
						},
					})
				return m
			}(),
			wantOverrideCfg: overrideConfig{
				failureModeAllow: optional.New(true),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := builder{}
			got, err := builder.ParseFilterConfigOverride(tt.override, httpfilter.ParseOptions{})
			if err != nil {
				t.Fatalf("ParseFilterConfigOverride() returned unexpected error: %v", err)
			}
			if diff := cmp.Diff(got, tt.wantOverrideCfg, cmpOpts...); diff != "" {
				t.Fatalf("ParseFilterConfigOverride() returned unexpected config (-got +want):\n%s", diff)
			}
		})
	}
}

func (s) TestParseFilterConfigOverride_Errors(t *testing.T) {
	tests := []struct {
		name     string
		override proto.Message
		wantErr  string
	}{
		{
			name: "ProcessingMode_RequestBodyStreamed",
			override: func() proto.Message {
				m, _ := anypb.New(&fpb.ExtProcPerRoute{
					Override: &fpb.ExtProcPerRoute_Overrides{
						Overrides: &fpb.ExtProcOverrides{
							ProcessingMode: &fpb.ProcessingMode{
								RequestBodyMode: fpb.ProcessingMode_STREAMED,
							},
						},
					},
				})
				return m
			}(),
			wantErr: "extproc: invalid request body mode STREAMED",
		},
		{
			name: "ProcessingMode_ResponseBodyStreamed",
			override: func() proto.Message {
				m, _ := anypb.New(&fpb.ExtProcPerRoute{
					Override: &fpb.ExtProcPerRoute_Overrides{
						Overrides: &fpb.ExtProcOverrides{
							ProcessingMode: &fpb.ProcessingMode{
								ResponseBodyMode: fpb.ProcessingMode_STREAMED,
							},
						},
					},
				})
				return m
			}(),
			wantErr: "extproc: invalid response body mode STREAMED",
		},
		{
			name: "ProcessingMode_ResponseBodySendTrailerDefault",
			override: func() proto.Message {
				m, _ := anypb.New(&fpb.ExtProcPerRoute{
					Override: &fpb.ExtProcPerRoute_Overrides{
						Overrides: &fpb.ExtProcOverrides{
							ProcessingMode: &fpb.ProcessingMode{
								ResponseBodyMode:    fpb.ProcessingMode_GRPC,
								ResponseTrailerMode: fpb.ProcessingMode_DEFAULT,
							},
						},
					},
				})
				return m
			}(),
			wantErr: fmt.Sprintf("extproc: invalid response trailer mode DEFAULT: must be %q when response body mode is %q", "SEND", "GRPC"),
		},
		{
			name: "ProcessingMode_ResponseBodySendTrailerSkip",
			override: func() proto.Message {
				m, _ := anypb.New(&fpb.ExtProcPerRoute{
					Override: &fpb.ExtProcPerRoute_Overrides{
						Overrides: &fpb.ExtProcOverrides{
							ProcessingMode: &fpb.ProcessingMode{
								ResponseBodyMode:    fpb.ProcessingMode_GRPC,
								ResponseTrailerMode: fpb.ProcessingMode_SKIP,
							},
						},
					},
				})
				return m
			}(),
			wantErr: fmt.Sprintf("extproc: invalid response trailer mode SKIP: must be %q when response body mode is %q", "SEND", "GRPC"),
		},
		{
			name:     "InvalidOverrideType",
			override: &fpb.ExtProcOverrides{}, // Not Any
			wantErr:  "extproc: error parsing override",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := builder{}
			_, err := builder.ParseFilterConfigOverride(tt.override, httpfilter.ParseOptions{})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseFilterConfigOverride() returned error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func (s) TestBuildClientInterceptor_Success(t *testing.T) {
	tests := []struct {
		name       string
		cfg        httpfilter.FilterConfig
		override   httpfilter.FilterConfig
		wantConfig baseConfig
	}{
		{
			name: "ConfigUsingOnlyBase",
			cfg: baseConfig{
				failureModeAllow:         true,
				requestAttributes:        []string{"attr1"},
				responseAttributes:       []string{"attr2"},
				observabilityMode:        true,
				disableImmediateResponse: true,
				deferredCloseTimeout:     10 * time.Second,
				processingModes: processingModes{
					requestHeaderMode:   modeSend,
					responseHeaderMode:  modeSkip,
					responseTrailerMode: modeSend,
					requestBodyMode:     modeSend,
					responseBodyMode:    modeSkip,
				},
				server: grpcservice.Config{
					TargetURI:          testBaseURI,
					ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "test-channel-creds"}, nil),
					CallCredentials:    []*xdscreds.CallCreds{xdscreds.NewCallCreds(nil, xdscreds.Identity{Type: "test-call-creds"}, nil)},
					InitialMetadata:    metadata.MD(metadata.Pairs("key1", "value1")),
					Timeout:            5 * time.Second,
				},
				mutationRules: httpfilter.HeaderMutationRules{
					AllowExpr:       regexp.MustCompile("^(?:allow-.*)$"),
					DisallowExpr:    regexp.MustCompile("^(?:disallow-.*)$"),
					DisallowAll:     true,
					DisallowIsError: true,
				},
				allowedHeaders: []matcher.StringMatcher{matcher.NewExactStringMatcher("allow-header", false)},
			},
			wantConfig: baseConfig{
				failureModeAllow:   true,
				requestAttributes:  []string{"attr1"},
				responseAttributes: []string{"attr2"},
				mutationRules: httpfilter.HeaderMutationRules{
					AllowExpr:       regexp.MustCompile("^(?:allow-.*)$"),
					DisallowExpr:    regexp.MustCompile("^(?:disallow-.*)$"),
					DisallowAll:     true,
					DisallowIsError: true,
				},
				observabilityMode:        true,
				disableImmediateResponse: true,
				deferredCloseTimeout:     10 * time.Second,
				processingModes: processingModes{
					requestHeaderMode:   modeSend,
					responseHeaderMode:  modeSkip,
					responseTrailerMode: modeSend,
					requestBodyMode:     modeSend,
					responseBodyMode:    modeSkip,
				},
				server: grpcservice.Config{
					TargetURI:          testBaseURI,
					ChannelCredentials: xdscreds.NewChannelCreds(nil, xdscreds.Identity{Type: "test-channel-creds"}, nil),
					CallCredentials:    []*xdscreds.CallCreds{xdscreds.NewCallCreds(nil, xdscreds.Identity{Type: "test-call-creds"}, nil)},
					InitialMetadata:    metadata.MD(metadata.Pairs("key1", "value1")),
					Timeout:            5 * time.Second,
				},
				allowedHeaders: []matcher.StringMatcher{matcher.NewExactStringMatcher("allow-header", false)},
			},
		},
		{
			name: "ConfigUsingBaseAndOverride",
			cfg: baseConfig{
				failureModeAllow:         false,
				requestAttributes:        []string{"base-attr1"},
				responseAttributes:       []string{"base-attr2"},
				observabilityMode:        true,
				disableImmediateResponse: true,
				processingModes: processingModes{
					requestHeaderMode:   modeSend,
					responseHeaderMode:  modeSkip,
					responseTrailerMode: modeSend,
					requestBodyMode:     modeSend,
					responseBodyMode:    modeSkip,
				},
				server: grpcservice.Config{
					TargetURI:          testBaseURI,
					ChannelCredentials: allowlistInsecureCreds,
					Timeout:            time.Second,
					InitialMetadata:    metadata.MD(metadata.Pairs("key1", "value1")),
				},
				mutationRules: httpfilter.HeaderMutationRules{
					AllowExpr:       regexp.MustCompile("^(?:allow-.*)$"),
					DisallowExpr:    regexp.MustCompile("^(?:disallow-.*)$"),
					DisallowAll:     true,
					DisallowIsError: true,
				},
				allowedHeaders:       []matcher.StringMatcher{matcher.NewExactStringMatcher("allow-header", false)},
				disallowedHeaders:    []matcher.StringMatcher{matcher.NewExactStringMatcher("disallow-header", false)},
				deferredCloseTimeout: 10 * time.Second,
			},
			override: overrideConfig{
				failureModeAllow:   optional.New(true),
				requestAttributes:  []string{"override-attr1"},
				responseAttributes: []string{"override-attr2"},
				processingModes: optional.New(processingModes{
					requestHeaderMode:   modeSkip,
					responseHeaderMode:  modeSend,
					responseTrailerMode: modeSkip,
					requestBodyMode:     modeSkip,
					responseBodyMode:    modeSend,
				}),
				server: optional.New(grpcservice.Config{
					TargetURI:          "override-uri",
					ChannelCredentials: allowlistInsecureCreds,
				}),
			},
			wantConfig: baseConfig{
				failureModeAllow:   true,
				requestAttributes:  []string{"override-attr1"},
				responseAttributes: []string{"override-attr2"},
				mutationRules: httpfilter.HeaderMutationRules{
					AllowExpr:       regexp.MustCompile("^(?:allow-.*)$"),
					DisallowExpr:    regexp.MustCompile("^(?:disallow-.*)$"),
					DisallowAll:     true,
					DisallowIsError: true,
				},
				observabilityMode:        true,
				disableImmediateResponse: true,
				deferredCloseTimeout:     10 * time.Second,
				processingModes: processingModes{
					requestHeaderMode:   modeSkip,
					responseHeaderMode:  modeSend,
					responseTrailerMode: modeSkip,
					requestBodyMode:     modeSkip,
					responseBodyMode:    modeSend,
				},
				server: grpcservice.Config{
					TargetURI:          "override-uri",
					ChannelCredentials: allowlistInsecureCreds,
				},
				allowedHeaders:    []matcher.StringMatcher{matcher.NewExactStringMatcher("allow-header", false)},
				disallowedHeaders: []matcher.StringMatcher{matcher.NewExactStringMatcher("disallow-header", false)},
			},
		},
		{
			name: "ConfigUsingBaseAndPartialOverride",
			cfg: baseConfig{
				failureModeAllow:         false,
				requestAttributes:        []string{"base-attr1"},
				responseAttributes:       []string{"base-attr2"},
				observabilityMode:        true,
				disableImmediateResponse: true,
				deferredCloseTimeout:     10 * time.Second,
				processingModes: processingModes{
					requestHeaderMode:   modeSend,
					responseHeaderMode:  modeSkip,
					responseTrailerMode: modeSend,
					requestBodyMode:     modeSend,
					responseBodyMode:    modeSkip,
				},
				server: grpcservice.Config{
					TargetURI:          testBaseURI,
					ChannelCredentials: allowlistInsecureCreds,
					Timeout:            time.Second,
					InitialMetadata:    metadata.MD(metadata.Pairs("key1", "value1")),
				},
				mutationRules: httpfilter.HeaderMutationRules{
					AllowExpr:       regexp.MustCompile("^(?:allow-.*)$"),
					DisallowExpr:    regexp.MustCompile("^(?:disallow-.*)$"),
					DisallowAll:     true,
					DisallowIsError: true,
				},
				allowedHeaders:    []matcher.StringMatcher{matcher.NewExactStringMatcher("allow-header", false)},
				disallowedHeaders: []matcher.StringMatcher{matcher.NewExactStringMatcher("disallow-header", false)},
			},
			override: overrideConfig{
				failureModeAllow: optional.New(true),
			},
			wantConfig: baseConfig{
				failureModeAllow:   true,
				requestAttributes:  []string{"base-attr1"},
				responseAttributes: []string{"base-attr2"},
				mutationRules: httpfilter.HeaderMutationRules{
					AllowExpr:       regexp.MustCompile("^(?:allow-.*)$"),
					DisallowExpr:    regexp.MustCompile("^(?:disallow-.*)$"),
					DisallowAll:     true,
					DisallowIsError: true,
				},
				observabilityMode:        true,
				disableImmediateResponse: true,
				deferredCloseTimeout:     10 * time.Second,
				processingModes: processingModes{
					requestHeaderMode:   modeSend,
					responseHeaderMode:  modeSkip,
					responseTrailerMode: modeSend,
					requestBodyMode:     modeSend,
					responseBodyMode:    modeSkip,
				},
				server: grpcservice.Config{
					TargetURI:          testBaseURI,
					ChannelCredentials: allowlistInsecureCreds,
					Timeout:            time.Second,
					InitialMetadata:    metadata.MD(metadata.Pairs("key1", "value1")),
				},
				allowedHeaders:    []matcher.StringMatcher{matcher.NewExactStringMatcher("allow-header", false)},
				disallowedHeaders: []matcher.StringMatcher{matcher.NewExactStringMatcher("disallow-header", false)},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			overrideCreateExtProcChannel(t, "")
			builder := builder{}
			filter := builder.BuildClientFilter(httpfilter.ClientFilterOptions{})
			defer filter.Close()

			intptr, err := filter.BuildClientInterceptor(tc.cfg, tc.override)
			if err != nil {
				t.Fatalf("BuildClientInterceptor() returned unexpected error: %v", err)
			}
			defer intptr.Close()
			ic, _ := intptr.(*clientInterceptor)
			if diff := cmp.Diff(ic.config, tc.wantConfig, cmpOpts...); diff != "" {
				t.Fatalf("Interceptor config returned unexpected diff (-got +want):\n%s", diff)
			}
		})
	}
}

func (s) TestBuildClientInterceptor_Failure(t *testing.T) {
	// incorrectFilterConfig embeds httpfilter.FilterConfig but is not of type
	// baseConfig/overrideConfig, and is used to test incorrect config types being
	// passed to BuildClientInterceptor.
	type incorrectFilterConfig struct {
		httpfilter.FilterConfig
	}

	tests := []struct {
		name     string
		cfg      httpfilter.FilterConfig
		override httpfilter.FilterConfig
		wantErr  string
	}{
		{
			name:    "NilConfig",
			cfg:     nil,
			wantErr: "extproc: incorrect config type provided",
		},
		{
			name:    "IncorrectConfigType",
			cfg:     incorrectFilterConfig{},
			wantErr: "extproc: incorrect config type provided",
		},
		{
			name:     "IncorrectOverrideType",
			cfg:      baseConfig{},
			override: incorrectFilterConfig{},
			wantErr:  "extproc: incorrect override config type provided",
		},
		{
			name: "ChannelCreationFailure",
			cfg: baseConfig{
				server: grpcservice.Config{
					TargetURI: "error-uri",
				},
			},
			wantErr: fmt.Sprintf("extproc: failed to create channel to the external processor server %q: dial error", "error-uri"),
		},
		{
			name: "ChannelCreationFailureInOverride",
			cfg: baseConfig{
				server: grpcservice.Config{
					TargetURI: testBaseURI,
				},
			},
			override: overrideConfig{
				server: optional.New(grpcservice.Config{
					TargetURI: "error-uri",
				}),
			},
			wantErr: fmt.Sprintf("extproc: failed to create channel to the external processor server %q: dial error", "error-uri"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			overrideCreateExtProcChannel(t, "error-uri")
			builder := builder{}
			filter := builder.BuildClientFilter(httpfilter.ClientFilterOptions{})
			defer filter.Close()

			_, err := filter.BuildClientInterceptor(tc.cfg, tc.override)
			if err == nil {
				t.Fatalf("BuildClientInterceptor() returned nil error, want error %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("BuildClientInterceptor() returned error: %v, want %v", err, tc.wantErr)
			}
		})
	}
}
