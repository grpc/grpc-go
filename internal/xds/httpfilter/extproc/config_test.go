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

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/optional"
	"google.golang.org/grpc/internal/xds/httpfilter"
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

func (s) TestParseFilterConfig_Success(t *testing.T) {
	origServerConfigFromGrpcService := serverConfigFromGrpcService
	defer func() { serverConfigFromGrpcService = origServerConfigFromGrpcService }()
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
			TargetURI: grpcService.GetGoogleGrpc().GetTargetUri(),
		}
		return sc, nil
	}

	b := builder{}

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
				server: httpfilter.ServerConfig{
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
				server: httpfilter.ServerConfig{
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
				server: httpfilter.ServerConfig{
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
					AllowExpr:    regexp.MustCompile(".*"),
					DisallowExpr: regexp.MustCompile("a"),
				},
				deferredCloseTimeout: defaultDeferredCloseTimeout,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := b.ParseFilterConfig(tt.cfg)
			if err != nil {
				t.Fatalf("ParseFilterConfig() returned unexpected error: %v", err)
			}
			if diff := cmp.Diff(got, tt.wantCfg, cmp.AllowUnexported(baseConfig{}, httpfilter.ServerConfig{}, processingModes{}, httpfilter.HeaderMutationRules{}), protocmp.Transform(), cmp.Transformer("RegexpToString", func(r *regexp.Regexp) string {
				if r == nil {
					return ""
				}
				return r.String()
			})); diff != "" {
				t.Fatalf("ParseFilterConfig() returned unexpected config (-got +want):\n%s", diff)
			}
		})
	}
}

func (s) TestParseFilterConfig_Errors(t *testing.T) {
	origServerConfigFromGrpcService := serverConfigFromGrpcService
	defer func() { serverConfigFromGrpcService = origServerConfigFromGrpcService }()
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
			TargetURI: grpcService.GetGoogleGrpc().GetTargetUri(),
		}
		return sc, nil
	}

	b := builder{}

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
			name:    "NilConfig",
			cfg:     nil,
			wantErr: "extproc: nil base configuration message provided",
		},
		{
			name:    "InvalidConfigType",
			cfg:     &fpb.ExternalProcessor{}, // Not Any
			wantErr: "extproc: error parsing config",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := b.ParseFilterConfig(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseFilterConfig() returned error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func (s) TestParseFilterConfigOverride_Success(t *testing.T) {
	b := builder{}

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
				processingModes: optional.NewValue(processingModes{
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
				failureModeAllow: optional.NewValue(true),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := b.ParseFilterConfigOverride(tt.override)
			if err != nil {
				t.Fatalf("ParseFilterConfigOverride() returned unexpected error: %v", err)
			}
			if diff := cmp.Diff(got, tt.wantOverrideCfg, cmp.AllowUnexported(overrideConfig{}, httpfilter.ServerConfig{}, processingModes{}, httpfilter.HeaderMutationRules{}, optional.Optional[httpfilter.ServerConfig]{}, optional.Optional[processingModes]{}, optional.Optional[bool]{}), protocmp.Transform(), cmp.Transformer("RegexpToString", func(r *regexp.Regexp) string {
				if r == nil {
					return ""
				}
				return r.String()
			})); diff != "" {
				t.Fatalf("ParseFilterConfigOverride() returned unexpected config (-got +want):\n%s", diff)
			}
		})
	}
}

func (s) TestParseFilterConfigOverride_Errors(t *testing.T) {
	b := builder{}

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
			name:     "NilOverride",
			override: nil,
			wantErr:  "extproc: nil override configuration provided",
		},
		{
			name:     "InvalidOverrideType",
			override: &fpb.ExtProcOverrides{}, // Not Any
			wantErr:  "extproc: error parsing override",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := b.ParseFilterConfigOverride(tt.override)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseFilterConfigOverride() returned error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
