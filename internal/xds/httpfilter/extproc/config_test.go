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
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/optional"
	"google.golang.org/grpc/internal/xds/httpfilter"
	iextproc "google.golang.org/grpc/internal/xds/httpfilter/extproc/internal"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
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

const testBaseURI = "base-uri"

// testParseGRPCServiceConfig is a helper function that parses a GrpcService
// proto message into a GRPCServiceConfig. This is a temporary test
// implementation that will be removed once gRFC A102 is implemented.
func testParseGRPCServiceConfig(grpcService *corepb.GrpcService) (xdsresource.GRPCServiceConfig, error) {
	if grpcService == nil {
		return xdsresource.GRPCServiceConfig{}, nil
	}
	if grpcService.GetGoogleGrpc() == nil {
		return xdsresource.GRPCServiceConfig{}, fmt.Errorf("only google_grpc grpc_service is supported")
	}
	if grpcService.GetGoogleGrpc().GetTargetUri() == "" {
		return xdsresource.GRPCServiceConfig{}, fmt.Errorf("targetURI must be a non-empty string")
	}

	sc := xdsresource.GRPCServiceConfig{
		TargetURI: grpcService.GetGoogleGrpc().GetTargetUri(),
	}
	return sc, nil
}

var cmpOpts = []cmp.Option{
	cmp.AllowUnexported(
		baseConfig{},
		overrideConfig{},
		xdsresource.GRPCServiceConfig{},
		processingModes{},
		httpfilter.HeaderMutationRules{},
		optional.Optional[xdsresource.GRPCServiceConfig]{},
		optional.Optional[processingModes]{},
		optional.Optional[bool]{},
	),
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
	origParseGRPCServiceConfig := iextproc.ParseGRPCServiceConfig
	defer func() { iextproc.ParseGRPCServiceConfig = origParseGRPCServiceConfig }()
	iextproc.ParseGRPCServiceConfig = testParseGRPCServiceConfig

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
				server: xdsresource.GRPCServiceConfig{
					TargetURI:          "localhost:1234",
					ChannelCredentials: "",
				},
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
						RequestBodyMode:  fpb.ProcessingMode_GRPC,
						ResponseBodyMode: fpb.ProcessingMode_GRPC,
					},
				})
				return m
			}(),
			wantCfg: baseConfig{
				server: xdsresource.GRPCServiceConfig{
					TargetURI:          "localhost:1234",
					ChannelCredentials: "",
				},
				processingModes: processingModes{
					requestHeaderMode:   modeSend,
					responseHeaderMode:  modeSend,
					responseTrailerMode: modeSkip,
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
				server: xdsresource.GRPCServiceConfig{
					TargetURI:          "localhost:1234",
					ChannelCredentials: "",
				},
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
			got, err := b.ParseFilterConfig(tt.cfg)
			if err != nil {
				t.Fatalf("ParseFilterConfig() returned unexpected error: %v", err)
			}
			if diff := cmp.Diff(got, tt.wantCfg, cmpOpts...); diff != "" {
				t.Fatalf("ParseFilterConfig() returned unexpected config (-got +want):\n%s", diff)
			}
		})
	}
}

func (s) TestParseFilterConfig_Errors(t *testing.T) {
	origParseGRPCServiceConfig := iextproc.ParseGRPCServiceConfig
	defer func() { iextproc.ParseGRPCServiceConfig = origParseGRPCServiceConfig }()
	iextproc.ParseGRPCServiceConfig = testParseGRPCServiceConfig

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
			wantErr: "extproc: failed to parse grpc_service only google_grpc grpc_service is supported",
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
			wantErr: "extproc: failed to parse grpc_service targetURI must be a non-empty string",
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
			_, err := builder.ParseFilterConfig(tt.cfg)
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
									RequestBodyMode:  fpb.ProcessingMode_GRPC,
									ResponseBodyMode: fpb.ProcessingMode_GRPC,
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
					responseTrailerMode: modeSkip,
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
			got, err := builder.ParseFilterConfigOverride(tt.override)
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
			name:     "InvalidOverrideType",
			override: &fpb.ExtProcOverrides{}, // Not Any
			wantErr:  "extproc: error parsing override",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := builder{}
			_, err := builder.ParseFilterConfigOverride(tt.override)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseFilterConfigOverride() returned error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func (s) TestBuildClientInterceptor_Success(t *testing.T) {
	origCreateExtProcChannel := iextproc.CreateExtProcChannel
	iextproc.CreateExtProcChannel = func(cfg xdsresource.GRPCServiceConfig) (grpc.ClientConnInterface, func() error, error) {
		conn, _ := grpc.NewClient(cfg.TargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
		return conn, conn.Close, nil
	}
	defer func() { iextproc.CreateExtProcChannel = origCreateExtProcChannel }()

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
				server: xdsresource.GRPCServiceConfig{
					TargetURI:          testBaseURI,
					ChannelCredentials: "test-channel-creds",
					CallCredentials:    "test-call-creds",
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
				server: xdsresource.GRPCServiceConfig{
					TargetURI:          testBaseURI,
					ChannelCredentials: "test-channel-creds",
					CallCredentials:    "test-call-creds",
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
				server: xdsresource.GRPCServiceConfig{
					TargetURI:       testBaseURI,
					Timeout:         time.Second,
					InitialMetadata: metadata.MD(metadata.Pairs("key1", "value1")),
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
				server: optional.New(xdsresource.GRPCServiceConfig{
					TargetURI: "override-uri",
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
				server: xdsresource.GRPCServiceConfig{
					TargetURI: "override-uri",
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
				server: xdsresource.GRPCServiceConfig{
					TargetURI:       testBaseURI,
					Timeout:         time.Second,
					InitialMetadata: metadata.MD(metadata.Pairs("key1", "value1")),
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
				server: xdsresource.GRPCServiceConfig{
					TargetURI:       testBaseURI,
					Timeout:         time.Second,
					InitialMetadata: metadata.MD(metadata.Pairs("key1", "value1")),
				},
				allowedHeaders:    []matcher.StringMatcher{matcher.NewExactStringMatcher("allow-header", false)},
				disallowedHeaders: []matcher.StringMatcher{matcher.NewExactStringMatcher("disallow-header", false)},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			builder := builder{}
			filter := builder.BuildClientFilter()
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
	origCreateExtProcChannel := iextproc.CreateExtProcChannel
	iextproc.CreateExtProcChannel = func(cfg xdsresource.GRPCServiceConfig) (grpc.ClientConnInterface, func() error, error) {
		if cfg.TargetURI == "error-uri" {
			return nil, nil, fmt.Errorf("dial error")
		}
		conn, _ := grpc.NewClient(cfg.TargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
		return conn, conn.Close, nil
	}
	defer func() { iextproc.CreateExtProcChannel = origCreateExtProcChannel }()

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
				server: xdsresource.GRPCServiceConfig{
					TargetURI: "error-uri",
				},
			},
			wantErr: "extproc: failed to create channel to the external processor server \"error-uri\": dial error",
		},
		{
			name: "ChannelCreationFailureInOverride",
			cfg: baseConfig{
				server: xdsresource.GRPCServiceConfig{
					TargetURI: testBaseURI,
				},
			},
			override: overrideConfig{
				server: optional.New(xdsresource.GRPCServiceConfig{
					TargetURI: "error-uri",
				}),
			},
			wantErr: "extproc: failed to create channel to the external processor server \"error-uri\": dial error",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			builder := builder{}
			filter := builder.BuildClientFilter()
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
