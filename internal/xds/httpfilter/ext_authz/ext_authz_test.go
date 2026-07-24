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

package extauthz

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3extauthzfilterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
	matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

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
		xdsresource.GRPCServiceConfig{},
		fraction{},
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

// Test verifies that ParseFilterConfig successfully parses valid external
// authorization filter configurations into their internal representation.
func (s) TestParseFilterConfig_Success(t *testing.T) {
	origParseGRPCServiceConfig := parseGRPCServiceConfig
	defer func() { parseGRPCServiceConfig = origParseGRPCServiceConfig }()
	parseGRPCServiceConfig = testParseGRPCServiceConfig

	tests := []struct {
		name    string
		cfg     proto.Message
		wantCfg httpfilter.FilterConfig
	}{
		{
			name: "DefaultConfig",
			cfg: func() proto.Message {
				m, _ := anypb.New(&v3extauthzfilterpb.ExtAuthz{
					Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
						GrpcService: &corepb.GrpcService{
							TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
								GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
									TargetUri: "localhost:1234",
								},
							},
						},
					},
				})
				return m
			}(),
			wantCfg: baseConfig{
				grpcService: xdsresource.GRPCServiceConfig{
					TargetURI: "localhost:1234",
				},
				filterEnabled: fraction{
					numerator:   100,
					denominator: 100,
				},
			},
		},
		{
			name: "FullConfig",
			cfg: func() proto.Message {
				m, _ := anypb.New(&v3extauthzfilterpb.ExtAuthz{
					Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
						GrpcService: &corepb.GrpcService{
							TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
								GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
									TargetUri: "localhost:5678",
								},
							},
						},
					},
					FilterEnabled: &corepb.RuntimeFractionalPercent{
						DefaultValue: &v3typepb.FractionalPercent{
							Numerator:   50,
							Denominator: v3typepb.FractionalPercent_TEN_THOUSAND,
						},
					},
					DenyAtDisable: &corepb.RuntimeFeatureFlag{
						DefaultValue: wrapperspb.Bool(true),
					},
					FailureModeAllow:          true,
					FailureModeAllowHeaderAdd: true,
					StatusOnError: &v3typepb.HttpStatus{
						Code: v3typepb.StatusCode_Forbidden,
					},
					DecoderHeaderMutationRules: &mutationpb.HeaderMutationRules{
						AllowExpression:    &matcherpb.RegexMatcher{Regex: ".*"},
						DisallowExpression: &matcherpb.RegexMatcher{Regex: "a"},
					},
					AllowedHeaders: &matcherpb.ListStringMatcher{
						Patterns: []*matcherpb.StringMatcher{{
							MatchPattern: &matcherpb.StringMatcher_Exact{Exact: "allow-header"},
						}},
					},
					DisallowedHeaders: &matcherpb.ListStringMatcher{
						Patterns: []*matcherpb.StringMatcher{
							{
								MatchPattern: &matcherpb.StringMatcher_Exact{Exact: "disallow-header"},
							},
						},
					},
					IncludePeerCertificate: true,
				})
				return m
			}(),
			wantCfg: baseConfig{
				grpcService: xdsresource.GRPCServiceConfig{
					TargetURI: "localhost:5678",
				},
				filterEnabled: fraction{
					numerator:   50,
					denominator: 10000,
				},
				denyAtDisable:             true,
				failureModeAllow:          true,
				failureModeAllowHeaderAdd: true,
				statusOnError:             int32(v3typepb.StatusCode_Forbidden),
				decoderHeaderMutationRules: httpfilter.HeaderMutationRules{
					AllowExpr:    regexp.MustCompile("^(?:.*)$"),
					DisallowExpr: regexp.MustCompile("^(?:a)$"),
				},
				allowedHeaders: []matcher.StringMatcher{
					matcher.NewExactStringMatcher("allow-header", false),
				},
				disallowedHeaders: []matcher.StringMatcher{
					matcher.NewExactStringMatcher("disallow-header", false),
				},
				includePeerCertificate: true,
			},
		},
		{
			name: "FilterEnabled_DenominatorHundred",
			cfg: func() proto.Message {
				m, _ := anypb.New(&v3extauthzfilterpb.ExtAuthz{
					Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
						GrpcService: &corepb.GrpcService{
							TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
								GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
									TargetUri: "localhost:1234",
								},
							},
						},
					},
					FilterEnabled: &corepb.RuntimeFractionalPercent{
						DefaultValue: &v3typepb.FractionalPercent{
							Numerator:   10,
							Denominator: v3typepb.FractionalPercent_HUNDRED,
						},
					},
				})
				return m
			}(),
			wantCfg: baseConfig{
				grpcService: xdsresource.GRPCServiceConfig{
					TargetURI: "localhost:1234",
				},
				filterEnabled: fraction{
					numerator:   10,
					denominator: 100,
				},
			},
		},
		{
			name: "FilterEnabled_DenominatorMillion",
			cfg: func() proto.Message {
				m, _ := anypb.New(&v3extauthzfilterpb.ExtAuthz{
					Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
						GrpcService: &corepb.GrpcService{
							TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
								GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
									TargetUri: "localhost:1234",
								},
							},
						},
					},
					FilterEnabled: &corepb.RuntimeFractionalPercent{
						DefaultValue: &v3typepb.FractionalPercent{
							Numerator:   5,
							Denominator: v3typepb.FractionalPercent_MILLION,
						},
					},
				})
				return m
			}(),
			wantCfg: baseConfig{
				grpcService: xdsresource.GRPCServiceConfig{
					TargetURI: "localhost:1234",
				},
				filterEnabled: fraction{
					numerator:   5,
					denominator: 1000000,
				},
			},
		},
		{
			name: "FilterEnabled_DefaultDenominator",
			cfg: func() proto.Message {
				m, _ := anypb.New(&v3extauthzfilterpb.ExtAuthz{
					Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
						GrpcService: &corepb.GrpcService{
							TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
								GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
									TargetUri: "localhost:1234",
								},
							},
						},
					},
					FilterEnabled: &corepb.RuntimeFractionalPercent{
						DefaultValue: &v3typepb.FractionalPercent{
							Numerator: 25,
						},
					},
				})
				return m
			}(),
			wantCfg: baseConfig{
				grpcService: xdsresource.GRPCServiceConfig{
					TargetURI: "localhost:1234",
				},
				filterEnabled: fraction{
					numerator:   25,
					denominator: 100,
				},
			},
		},
		{
			name: "FilterEnabled_CappedToHundredPercent",
			cfg: func() proto.Message {
				m, _ := anypb.New(&v3extauthzfilterpb.ExtAuthz{
					Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
						GrpcService: &corepb.GrpcService{
							TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
								GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
									TargetUri: "localhost:1234",
								},
							},
						},
					},
					FilterEnabled: &corepb.RuntimeFractionalPercent{
						DefaultValue: &v3typepb.FractionalPercent{
							Numerator:   200,
							Denominator: v3typepb.FractionalPercent_HUNDRED,
						},
					},
				})
				return m
			}(),
			wantCfg: baseConfig{
				grpcService: xdsresource.GRPCServiceConfig{
					TargetURI: "localhost:1234",
				},
				filterEnabled: fraction{
					numerator:   100,
					denominator: 100,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := builder{}
			got, err := b.ParseFilterConfig(tt.cfg)
			if err != nil {
				t.Fatalf("ParseFilterConfig() failed with unexpected error: %v", err)
			}
			if diff := cmp.Diff(got, tt.wantCfg, cmpOpts...); diff != "" {
				t.Fatalf("ParseFilterConfig() returned unexpected config (-got +want):\n%s", diff)
			}
		})
	}
}

// Test verifies that ParseFilterConfig returns an error when provided with
// invalid or unsupported configurations.
func (s) TestParseFilterConfig_Failure(t *testing.T) {
	origParseGRPCServiceConfig := parseGRPCServiceConfig
	defer func() { parseGRPCServiceConfig = origParseGRPCServiceConfig }()
	parseGRPCServiceConfig = testParseGRPCServiceConfig

	tests := []struct {
		name    string
		cfg     proto.Message
		wantErr string
	}{
		{
			name:    "InvalidConfigType",
			cfg:     &v3extauthzfilterpb.ExtAuthz{},
			wantErr: "extauthz: error parsing config",
		},
		{
			name: "Config_Unmarshaling_Failed",
			cfg: &anypb.Any{
				TypeUrl: "type.googleapis.com/invalid",
				Value:   []byte("invalid"),
			},
			wantErr: "extauthz: failed to unmarshal config",
		},
		{
			name: "MissingGrpcService",
			cfg: func() proto.Message {
				m, _ := anypb.New(&v3extauthzfilterpb.ExtAuthz{})
				return m
			}(),
			wantErr: "extauthz: empty grpc_service provided",
		},
		{
			name: "UnsupportedGrpcService_EnvoyGrpc",
			cfg: func() proto.Message {
				m, _ := anypb.New(&v3extauthzfilterpb.ExtAuthz{
					Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
						GrpcService: &corepb.GrpcService{
							TargetSpecifier: &corepb.GrpcService_EnvoyGrpc_{
								EnvoyGrpc: &corepb.GrpcService_EnvoyGrpc{
									ClusterName: "cluster",
								},
							},
						},
					},
				})
				return m
			}(),
			wantErr: "extauthz: failed to parse grpc_service: only google_grpc grpc_service is supported",
		},
		{
			name: "InvalidServerConfig_EmptyTargetURI",
			cfg: func() proto.Message {
				m, _ := anypb.New(&v3extauthzfilterpb.ExtAuthz{
					Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
						GrpcService: &corepb.GrpcService{
							TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
								GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
									TargetUri: "",
								},
							},
						},
					},
				})
				return m
			}(),
			wantErr: "extauthz: failed to parse grpc_service: targetURI must be a non-empty string",
		},
		{
			name: "MissingDefaultValueInFilterEnabled",
			cfg: func() proto.Message {
				m, _ := anypb.New(&v3extauthzfilterpb.ExtAuthz{
					Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
						GrpcService: &corepb.GrpcService{
							TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
								GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
									TargetUri: "localhost:1234",
								},
							},
						},
					},
					FilterEnabled: &corepb.RuntimeFractionalPercent{},
				})
				return m
			}(),
			wantErr: "extauthz: missing default_value in filter_enabled",
		},
		{
			name: "MissingDefaultValueInDenyAtDisable",
			cfg: func() proto.Message {
				m, _ := anypb.New(&v3extauthzfilterpb.ExtAuthz{
					Services: &v3extauthzfilterpb.ExtAuthz_GrpcService{
						GrpcService: &corepb.GrpcService{
							TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
								GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
									TargetUri: "localhost:1234",
								},
							},
						},
					},
					DenyAtDisable: &corepb.RuntimeFeatureFlag{},
				})
				return m
			}(),
			wantErr: "extauthz: missing default_value in deny_at_disable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := builder{}
			if _, err := b.ParseFilterConfig(tt.cfg); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseFilterConfig() returned error = %v, wantErr containing %v", err, tt.wantErr)
			}
		})
	}
}

// Test verifies that ParseFilterConfigOverride successfully unmarshals valid
// per-route override configurations.
func (s) TestParseFilterConfigOverride_Success(t *testing.T) {
	override, err := anypb.New(&v3extauthzfilterpb.ExtAuthzPerRoute{
		Override: &v3extauthzfilterpb.ExtAuthzPerRoute_Disabled{
			Disabled: true,
		},
	})
	if err != nil {
		t.Fatalf("Failed to marshal ExtAuthzPerRoute to anypb.Any: %v", err)
	}

	b := builder{}
	got, err := b.ParseFilterConfigOverride(override)
	if err != nil {
		t.Fatalf("ParseFilterConfigOverride() failed with unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("ParseFilterConfigOverride() = %v, want nil", got)
	}
}

// Test verifies that ParseFilterConfigOverride returns an error when provided
// with an invalid proto message type or a malformed Any message.
func (s) TestParseFilterConfigOverride_Failure(t *testing.T) {
	tests := []struct {
		name     string
		override proto.Message
		wantErr  string
	}{
		{
			name:     "InvalidOverrideType",
			override: &v3extauthzfilterpb.ExtAuthzPerRoute{},
			wantErr:  "extauthz: error parsing override config",
		},
		{
			name: "Unmarshal_Failed",
			override: &anypb.Any{
				TypeUrl: "type.googleapis.com/invalid",
				Value:   []byte("invalid"),
			},
			wantErr: "extauthz: failed to unmarshal override config",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := builder{}
			if _, err := b.ParseFilterConfigOverride(tt.override); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseFilterConfigOverride() returned error = %v, wantErr containing %v", err, tt.wantErr)
			}
		})
	}
}
