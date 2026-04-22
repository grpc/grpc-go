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
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3procfilterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
)

const testBaseURI = "base-uri"

type incorrectFilterConfig struct {
	httpfilter.FilterConfig
}

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestBuildClientInterceptor(t *testing.T) {
	origServerConfigFromGrpcService := serverConfigFromGrpcService
	defer func() { serverConfigFromGrpcService = origServerConfigFromGrpcService }()

	serverConfigFromGrpcService = func(grpcService *v3corepb.GrpcService) (serverConfig, error) {
		if grpcService == nil {
			return serverConfig{}, nil
		}
		if grpcService.GetGoogleGrpc() == nil {
			return serverConfig{}, fmt.Errorf("missing google_grpc")
		}
		return serverConfig{
			targetURI:          grpcService.GetGoogleGrpc().GetTargetUri(),
			channelCredentials: insecure.NewCredentials(),
		}, nil
	}

	b := builder{}
	f := b.BuildClientFilter()
	defer f.Close()

	tests := []struct {
		name       string
		cfg        httpfilter.FilterConfig
		override   httpfilter.FilterConfig
		wantConfig *interceptorConfig
		wantErr    string
	}{
		{
			name:    "NilConfig",
			cfg:     nil,
			wantErr: "extproc: nil config provided",
		},
		{
			name:    "IncorrectConfigType",
			cfg:     incorrectFilterConfig{},
			wantErr: "extproc: incorrect config type provided",
		},
		{
			name:     "IncorrectOverrideType",
			cfg:      baseConfig{config: &v3procfilterpb.ExternalProcessor{}},
			override: incorrectFilterConfig{},
			wantErr:  "extproc: incorrect override config type provided",
		},
		{
			name: "DeferredCloseTimeoutDefault",
			cfg: baseConfig{config: &v3procfilterpb.ExternalProcessor{
				GrpcService: &v3corepb.GrpcService{
					TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
						GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
							TargetUri: testBaseURI,
						},
					},
				},
			}},
			wantConfig: &interceptorConfig{
				server: serverConfig{
					targetURI:          testBaseURI,
					channelCredentials: insecure.NewCredentials(),
				},
				deferredCloseTimeout: defaultDeferredCloseTimeout,
			},
		},
		{
			name: "CompleteBase",
			cfg: baseConfig{config: &v3procfilterpb.ExternalProcessor{
				FailureModeAllow:         true,
				AllowModeOverride:        true,
				RequestAttributes:        []string{"attr1"},
				ResponseAttributes:       []string{"attr2"},
				ObservabilityMode:        true,
				DisableImmediateResponse: true,
				DeferredCloseTimeout:     durationpb.New(10 * time.Second),
				ProcessingMode: &v3procfilterpb.ProcessingMode{
					RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
					ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SKIP,
					ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
					RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
					ResponseBodyMode:    v3procfilterpb.ProcessingMode_NONE,
				},
				GrpcService: &v3corepb.GrpcService{
					TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
						GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
							TargetUri: testBaseURI,
						},
					},
				},
				ForwardRules: &v3procfilterpb.HeaderForwardingRules{
					AllowedHeaders: &v3matcherpb.ListStringMatcher{
						Patterns: []*v3matcherpb.StringMatcher{
							{
								MatchPattern: &v3matcherpb.StringMatcher_Exact{
									Exact: "allow-header",
								},
							},
						},
					},
				},
			}},
			wantConfig: &interceptorConfig{
				failureModeAllow:         true,
				allowModeOverride:        true,
				requestAttributes:        []string{"attr1"},
				responseAttributes:       []string{"attr2"},
				observabilityMode:        true,
				disableImmediateResponse: true,
				deferredCloseTimeout:     10 * time.Second,
				processingMode: processingModes{
					requestHeaderMode:   modeSend,
					responseHeaderMode:  modeSkip,
					responseTrailerMode: modeSend,
					requestBodyMode:     modeSend,
					responseBodyMode:    modeSkip,
				},
				server: serverConfig{
					targetURI:          testBaseURI,
					channelCredentials: insecure.NewCredentials(),
				},
				allowedHeaders: []matcher.StringMatcher{matcher.NewExactStringMatcher("allow-header", false)},
			},
		},
		{
			name: "CompleteBaseAndOverride",
			cfg: baseConfig{config: &v3procfilterpb.ExternalProcessor{
				FailureModeAllow:         false,
				AllowModeOverride:        true,
				RequestAttributes:        []string{"base-attr1"},
				ResponseAttributes:       []string{"base-attr2"},
				ObservabilityMode:        true,
				DisableImmediateResponse: true,
				DeferredCloseTimeout:     durationpb.New(10 * time.Second),
				ProcessingMode: &v3procfilterpb.ProcessingMode{
					RequestHeaderMode:   v3procfilterpb.ProcessingMode_SEND,
					ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SKIP,
					ResponseTrailerMode: v3procfilterpb.ProcessingMode_SEND,
					RequestBodyMode:     v3procfilterpb.ProcessingMode_GRPC,
					ResponseBodyMode:    v3procfilterpb.ProcessingMode_NONE,
				},
				GrpcService: &v3corepb.GrpcService{
					TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
						GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
							TargetUri: testBaseURI,
						},
					},
				},
				ForwardRules: &v3procfilterpb.HeaderForwardingRules{
					AllowedHeaders: &v3matcherpb.ListStringMatcher{
						Patterns: []*v3matcherpb.StringMatcher{
							{
								MatchPattern: &v3matcherpb.StringMatcher_Exact{
									Exact: "allow-header",
								},
							},
						},
					},
				},
			}},
			override: overrideConfig{config: &v3procfilterpb.ExtProcOverrides{
				FailureModeAllow:   wrapperspb.Bool(true),
				RequestAttributes:  []string{"override-attr1"},
				ResponseAttributes: []string{"override-attr2"},
				ProcessingMode: &v3procfilterpb.ProcessingMode{
					RequestHeaderMode:   v3procfilterpb.ProcessingMode_SKIP,
					ResponseHeaderMode:  v3procfilterpb.ProcessingMode_SEND,
					ResponseTrailerMode: v3procfilterpb.ProcessingMode_SKIP,
					RequestBodyMode:     v3procfilterpb.ProcessingMode_NONE,
					ResponseBodyMode:    v3procfilterpb.ProcessingMode_GRPC,
				},
				GrpcService: &v3corepb.GrpcService{
					TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
						GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
							TargetUri: "override-uri",
						},
					},
				},
			}},
			wantConfig: &interceptorConfig{
				failureModeAllow:         true,
				allowModeOverride:        true,
				requestAttributes:        []string{"override-attr1"},
				responseAttributes:       []string{"override-attr2"},
				observabilityMode:        true,
				disableImmediateResponse: true,
				deferredCloseTimeout:     10 * time.Second,
				processingMode: processingModes{
					requestHeaderMode:   modeSkip,
					responseHeaderMode:  modeSend,
					responseTrailerMode: modeSkip,
					requestBodyMode:     modeSkip,
					responseBodyMode:    modeSend,
				},
				server: serverConfig{
					targetURI:          "override-uri",
					channelCredentials: insecure.NewCredentials(),
					// TODO : Remove these when timeout and metadata are used. Adding zero
					// values here to satisfy the vet.
					timeout:            0,
					initialMetadata:    nil,
				},
				allowedHeaders: []matcher.StringMatcher{matcher.NewExactStringMatcher("allow-header", false)},
			},
		},
		{
			name: "GrpcServiceError",
			cfg: baseConfig{config: &v3procfilterpb.ExternalProcessor{
				GrpcService: &v3corepb.GrpcService{},
			}},
			wantErr: "failed to parse gRPC service config",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			intptr, err := f.BuildClientInterceptor(tc.cfg, tc.override)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("BuildClientInterceptor() unexpected error: %v", err)
				}
				got, ok := intptr.(*interceptor)
				if !ok {
					t.Fatalf("BuildClientInterceptor() returned %T, want *interceptor", intptr)
				}
				if !reflect.DeepEqual(got.config, tc.wantConfig) {
					t.Fatalf("interceptor.config = %+v, want %+v", got.config, tc.wantConfig)
				}
				intptr.Close()
			} else {
				if err == nil {
					t.Fatalf("BuildClientInterceptor() expected error %v, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("BuildClientInterceptor() error = %v, want error containing %q", err, tc.wantErr)
				}
			}
		})
	}
}
