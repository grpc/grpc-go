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

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/experimental/optional"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"

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

func (s) TestParseFilterConfig(t *testing.T) {
	origServerConfigFromGrpcService := serverConfigFromGrpcService
	defer func() { serverConfigFromGrpcService = origServerConfigFromGrpcService }()
	jsonBytes, _ := json.Marshal(insecure.NewCredentials())
	serverConfigFromGrpcService = func(grpcService *corepb.GrpcService) (httpfilter.ServerConfig, error) {
		if grpcService == nil {
			return httpfilter.ServerConfig{}, nil
		}
		if grpcService.GetGoogleGrpc() == nil {
			return httpfilter.ServerConfig{}, fmt.Errorf("only google_grpc grpc_service is supported")
		}
		if grpcService.GetGoogleGrpc().GetTargetUri() == "" {
			return httpfilter.ServerConfig{}, fmt.Errorf("targetURI must be a non-empty string")
		}

		sc := httpfilter.ServerConfig{
			TargetURI:          grpcService.GetGoogleGrpc().GetTargetUri(),
			ChannelCredentials: jsonBytes,
			CallCredentials:    nil,
		}
		if sc.TargetURI == "nil-creds" {
			sc.ChannelCredentials = nil
		}
		return sc, nil
	}

	b := builder{}

	tests := []struct {
		name    string
		cfg     proto.Message
		wantCfg httpfilter.FilterConfig
		wantErr string
	}{
		{
			name: "ValidConfig_default",
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
				config: interceptorConfig{
					server: httpfilter.ServerConfig{
						TargetURI:          "localhost:1234",
						ChannelCredentials: jsonBytes,
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
		},
		{
			name: "ValidConfig_GrpcMode",
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
				config: interceptorConfig{
					server: httpfilter.ServerConfig{
						TargetURI:          "localhost:1234",
						ChannelCredentials: jsonBytes,
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
		},
		{
			name: "ValidConfig_WithMutationRules",
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
				config: interceptorConfig{
					server: httpfilter.ServerConfig{
						TargetURI:          "localhost:1234",
						ChannelCredentials: jsonBytes,
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
						AllowExpr:    regexp.MustCompile(".*"),
						DisallowExpr: regexp.MustCompile("a"),
					},
					deferredCloseTimeout: defaultDeferredCloseTimeout,
				},
			},
		},
		{
			name: "ErrMissingGrpcService",
			cfg: func() proto.Message {
				m, _ := anypb.New(&fpb.ExternalProcessor{ProcessingMode: &fpb.ProcessingMode{}})
				return m
			}(),
			wantErr: "extproc: empty grpc_service provided",
		},
		{
			name: "ErrUnsupportedGrpcService_EnvoyGrpc",
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
			wantErr: "extproc: only google_grpc grpc_service is supported",
		},
		{
			name: "ErrMissingProcessingMode",
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
			name: "ErrInvalidProcessingMode_RequestBodyStreamed",
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
			name: "ErrInvalidProcessingMode_ResponseBodyStreamed",
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
			name: "ErrInvalidAllowExpression",
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
			wantErr: "extproc: error parsing regexp",
		},
		{
			name: "ErrInvalidDisallowExpression",
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
						DisallowExpression: &matcherpb.RegexMatcher{Regex: "["},
					},
				})
				return m
			}(),
			wantErr: "extproc: error parsing regexp",
		},
		{
			name: "ErrInvalidAllowedHeaders_EmptyPrefix",
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
			name: "ErrInvalidServerConfig_EmptyTargetURI",
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
			wantErr: "extproc: targetURI must be a non-empty string",
		},
		{
			name:    "ErrNilConfig",
			cfg:     nil,
			wantErr: "extproc: nil base configuration message provided",
		},
		{
			name:    "ErrInvalidConfigType",
			cfg:     &fpb.ExternalProcessor{}, // Not Any
			wantErr: "extproc: error parsing config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := b.ParseFilterConfig(tt.cfg)
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseFilterConfig() returned error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ParseFilterConfig() returned unexpected error: %v", err)
				}
				if diff := cmp.Diff(got, tt.wantCfg, cmp.AllowUnexported(baseConfig{}, interceptorConfig{}, httpfilter.ServerConfig{}, processingModes{}, httpfilter.HeaderMutationRules{}), protocmp.Transform(), cmp.Transformer("RegexpToString", func(r *regexp.Regexp) string {
					if r == nil {
						return ""
					}
					return r.String()
				})); diff != "" {
					t.Fatalf("ParseFilterConfig() returned unexpected config (-got +want):\n%s", diff)
				}
			}
		})
	}
}

func (s) TestParseFilterConfigOverride(t *testing.T) {
	b := builder{}

	tests := []struct {
		name            string
		override        proto.Message
		wantOverrideCfg httpfilter.FilterConfig
		wantErr         string
	}{
		{
			name: "ValidOverride",
			override: func() proto.Message {
				m, _ := anypb.New(&fpb.ExtProcPerRoute{})
				return m
			}(),
			wantOverrideCfg: overrideConfig{},
		},
		{
			name: "ValidOverride_Grpc",
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
				config: interceptorOverrideConfig{
					processingModes: optional.NewValue(processingModes{
						requestHeaderMode:   modeSend,
						responseHeaderMode:  modeSend,
						responseTrailerMode: modeSkip,
						requestBodyMode:     modeSend,
						responseBodyMode:    modeSend,
					}),
				},
			},
		},
		{
			name: "ErrInvalidProcessingMode_RequestBodyStreamed",
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
			name: "ErrInvalidProcessingMode_ResponseBodyStreamed",
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
			name:     "ErrNilOverride",
			override: nil,
			wantErr:  "extproc: nil override configuration provided",
		},
		{
			name:     "ErrInvalidOverrideType",
			override: &fpb.ExtProcOverrides{}, // Not Any
			wantErr:  "extproc: error parsing override",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := b.ParseFilterConfigOverride(tt.override)
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseFilterConfigOverride() returned error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ParseFilterConfigOverride() returned unexpected error: %v", err)
				}
				if diff := cmp.Diff(got, tt.wantOverrideCfg, cmp.AllowUnexported(overrideConfig{}, interceptorConfig{}, httpfilter.ServerConfig{}, processingModes{}, httpfilter.HeaderMutationRules{}, interceptorOverrideConfig{}, optional.Option[httpfilter.ServerConfig]{}, optional.Option[processingModes]{}, optional.Option[bool]{}), protocmp.Transform(), cmp.Transformer("RegexpToString", func(r *regexp.Regexp) string {
					if r == nil {
						return ""
					}
					return r.String()
				})); diff != "" {
					t.Fatalf("ParseFilterConfigOverride() returned unexpected config (-got +want):\n%s", diff)
				}
			}
		})
	}
}
