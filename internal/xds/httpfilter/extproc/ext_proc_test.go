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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"

	corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	fpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestParseFilterConfig(t *testing.T) {
	b := builder{}

	tests := []struct {
		name        string
		description string
		cfg         proto.Message
		wantCfg     httpfilter.FilterConfig
		wantErr     string
	}{
		{
			name:        "ValidConfig_default",
			description: "valid config with default body processing mode",
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
				config: &fpb.ExternalProcessor{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "localhost:1234",
							},
						},
					},
					ProcessingMode: &fpb.ProcessingMode{
						RequestHeaderMode:  fpb.ProcessingMode_DEFAULT,
						RequestBodyMode:    fpb.ProcessingMode_NONE,
						ResponseBodyMode:   fpb.ProcessingMode_NONE,
						ResponseHeaderMode: fpb.ProcessingMode_DEFAULT,
					},
				},
			},
		},
		{
			name:        "ValidConfig_GrpcMode",
			description: "valid config with GRPC processing mode",
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
				config: &fpb.ExternalProcessor{
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
				},
			},
		},
		{
			name:        "ErrMissingGrpcService",
			description: "config with missing grpc_service",
			cfg: func() proto.Message {
				m, _ := anypb.New(&fpb.ExternalProcessor{ProcessingMode: &fpb.ProcessingMode{}})
				return m
			}(),
			wantErr: "ext_proc: empty grpc_service provided",
		},
		{
			name:        "ErrUnsupportedGrpcService_EnvoyGrpc",
			description: "config with unsupported EnvoyGrpc service",
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
			wantErr: "ext_proc: only google_grpc grpc_service is supported",
		},
		{
			name:        "ErrMissingProcessingMode",
			description: "config with missing processing_mode",
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
			wantErr: "ext_proc: missing processing_mode",
		},
		{
			name:        "ErrInvalidProcessingMode_RequestBodyStreamed",
			description: "config with unsupported streamed request body mode",
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
			wantErr: "ext_proc: invalid request body mode STREAMED",
		},
		{
			name:        "ErrInvalidProcessingMode_ResponseBodyStreamed",
			description: "config with unsupported streamed response body mode",
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
			wantErr: "ext_proc: invalid response body mode STREAMED",
		},
		{
			name:        "ErrNilConfig",
			description: "nil config message",
			cfg:         nil,
			wantErr:     "ext_proc: nil base configuration message provided",
		},
		{
			name:        "ErrInvalidConfigType",
			description: "config message with invalid type (not Any)",
			cfg:         &fpb.ExternalProcessor{}, // Not Any
			wantErr:     "ext_proc: error parsing config",
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
				if diff := cmp.Diff(got, tt.wantCfg, cmp.AllowUnexported(baseConfig{}), protocmp.Transform()); diff != "" {
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
		description     string
		override        proto.Message
		wantOverrideCfg httpfilter.FilterConfig
		wantErr         string
	}{
		{
			name:        "ValidOverride",
			description: "valid empty override config",
			override: func() proto.Message {
				m, _ := anypb.New(&fpb.ExtProcOverrides{})
				return m
			}(),
			wantOverrideCfg: overrideConfig{
				config: &fpb.ExtProcOverrides{},
			},
		},
		{
			name:        "ValidOverride_Grpc",
			description: "valid override with GRPC processing mode",
			override: func() proto.Message {
				m, _ := anypb.New(&fpb.ExtProcOverrides{
					ProcessingMode: &fpb.ProcessingMode{
						RequestBodyMode:  fpb.ProcessingMode_GRPC,
						ResponseBodyMode: fpb.ProcessingMode_GRPC,
					},
				})
				return m
			}(),
			wantOverrideCfg: overrideConfig{
				config: &fpb.ExtProcOverrides{
					ProcessingMode: &fpb.ProcessingMode{
						RequestBodyMode:  fpb.ProcessingMode_GRPC,
						ResponseBodyMode: fpb.ProcessingMode_GRPC,
					},
				},
			},
		},
		{
			name:        "ErrUnsupportedGrpcService_EnvoyGrpc",
			description: "override with unsupported EnvoyGrpc service",
			override: func() proto.Message {
				m, _ := anypb.New(&fpb.ExtProcOverrides{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_EnvoyGrpc_{
							EnvoyGrpc: &corepb.GrpcService_EnvoyGrpc{
								ClusterName: "cluster",
							},
						},
					},
				})
				return m
			}(),
			wantErr: "ext_proc: only google_grpc grpc_service is supported",
		},
		{
			name:        "ErrInvalidProcessingMode_RequestBodyStreamed",
			description: "override with unsupported streamed request body mode",
			override: func() proto.Message {
				m, _ := anypb.New(&fpb.ExtProcOverrides{ProcessingMode: &fpb.ProcessingMode{RequestBodyMode: fpb.ProcessingMode_STREAMED}})
				return m
			}(),
			wantErr: "ext_proc: invalid request body mode STREAMED",
		},
		{
			name:        "ErrInvalidProcessingMode_ResponseBodyStreamed",
			description: "override with unsupported streamed response body mode",
			override: func() proto.Message {
				m, _ := anypb.New(&fpb.ExtProcOverrides{
					ProcessingMode: &fpb.ProcessingMode{
						ResponseBodyMode: fpb.ProcessingMode_STREAMED,
					},
				})
				return m
			}(),
			wantErr: "ext_proc: invalid response body mode STREAMED",
		},
		{
			name:        "ErrNilOverride",
			description: "nil override message",
			override:    nil,
			wantErr:     "ext_proc: nil override configuration provided",
		},
		{
			name:        "ErrInvalidOverrideType",
			description: "override message with invalid type (not Any)",
			override:    &fpb.ExtProcOverrides{}, // Not Any
			wantErr:     "ext_proc: error parsing override",
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
				if diff := cmp.Diff(got, tt.wantOverrideCfg, cmp.AllowUnexported(overrideConfig{}), protocmp.Transform()); diff != "" {
					t.Fatalf("ParseFilterConfigOverride() returned unexpected config (-got +want):\n%s", diff)
				}
			}
		})
	}
}
