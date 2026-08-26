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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	xdscreds "google.golang.org/grpc/internal/xds/credentials"
	"google.golang.org/grpc/internal/xds/grpcservice"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3extauthzpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
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
func testParseGRPCServiceConfig(grpcService *corepb.GrpcService) (grpcservice.Config, error) {
	if grpcService == nil {
		return grpcservice.Config{}, nil
	}
	if grpcService.GetGoogleGrpc() == nil {
		return grpcservice.Config{}, fmt.Errorf("only google_grpc grpc_service is supported")
	}
	if grpcService.GetGoogleGrpc().GetTargetUri() == "" {
		return grpcservice.Config{}, fmt.Errorf("targetURI must be a non-empty string")
	}

	sc := grpcservice.Config{
		TargetURI: grpcService.GetGoogleGrpc().GetTargetUri(),
	}
	return sc, nil
}

var cmpOpts = []cmp.Option{
	cmp.AllowUnexported(
		config{},
		grpcservice.Config{},
		fraction{},
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
		desc    string
		cfg     proto.Message
		wantCfg httpfilter.FilterConfig
	}{
		{
			name: "DefaultConfig",
			desc: "verifies default fallback values when only grpc_service is provided",
			cfg: testutils.MarshalAny(t, &v3extauthzpb.ExtAuthz{
				Services: &v3extauthzpb.ExtAuthz_GrpcService{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "localhost:1234",
							},
						},
					},
				},
			}),
			wantCfg: config{
				grpcService: grpcservice.Config{
					TargetURI: "localhost:1234",
				},
				filterEnabled: fraction{
					numerator:   100,
					denominator: 100,
				},
				statusOnError: codes.PermissionDenied,
			},
		},
		{
			name: "FullConfig",
			desc: "verifies all config fields are parsed and mapped correctly when explicitly configured",
			cfg: testutils.MarshalAny(t, &v3extauthzpb.ExtAuthz{
				Services: &v3extauthzpb.ExtAuthz_GrpcService{
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
					Code: v3typepb.StatusCode_Unauthorized,
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
			}),
			wantCfg: config{
				grpcService: grpcservice.Config{
					TargetURI: "localhost:5678",
				},
				filterEnabled: fraction{
					numerator:   50,
					denominator: 10000,
				},
				denyAtDisable:             true,
				failureModeAllow:          true,
				failureModeAllowHeaderAdd: true,
				statusOnError:             codes.Unauthenticated,
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Log(tt.desc)
			b := builder{}
			got, err := b.ParseFilterConfig(tt.cfg, httpfilter.ParseOptions{})
			if err != nil {
				t.Fatalf("ParseFilterConfig() failed with unexpected error: %v", err)
			}
			if diff := cmp.Diff(tt.wantCfg, got, cmpOpts...); diff != "" {
				t.Fatalf("ParseFilterConfig() returned unexpected config (-want, +got):\n%s", diff)
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
		desc    string
		cfg     proto.Message
		wantErr string
	}{
		{
			name:    "InvalidConfigType",
			desc:    "verifies error when input message is not a valid proto message",
			cfg:     &v3extauthzpb.ExtAuthz{},
			wantErr: "extauthz: error parsing config",
		},
		{
			name: "Config_Unmarshaling_Failed",
			desc: "verifies error when input proto message cannot be unmarshaled into an ExtAuthz message",
			cfg: &anypb.Any{
				TypeUrl: "type.googleapis.com/invalid",
				Value:   []byte("invalid"),
			},
			wantErr: "extauthz: failed to unmarshal config",
		},
		{
			name:    "MissingGrpcService",
			desc:    "verifies error when required grpc_service is missing",
			cfg:     testutils.MarshalAny(t, &v3extauthzpb.ExtAuthz{}),
			wantErr: "extauthz: empty grpc_service provided",
		},
		{
			name: "UnsupportedGrpcService_EnvoyGrpc",
			desc: "verifies error when unsupported envoy_grpc service is used",
			cfg: testutils.MarshalAny(t, &v3extauthzpb.ExtAuthz{
				Services: &v3extauthzpb.ExtAuthz_GrpcService{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_EnvoyGrpc_{
							EnvoyGrpc: &corepb.GrpcService_EnvoyGrpc{
								ClusterName: "cluster",
							},
						},
					},
				},
			}),
			wantErr: "extauthz: failed to parse grpc_service: only google_grpc grpc_service is supported",
		},
		{
			name: "InvalidServerConfig_EmptyTargetURI",
			desc: "verifies error when google_grpc has empty target URI",
			cfg: testutils.MarshalAny(t, &v3extauthzpb.ExtAuthz{
				Services: &v3extauthzpb.ExtAuthz_GrpcService{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "",
							},
						},
					},
				},
			}),
			wantErr: "extauthz: failed to parse grpc_service: targetURI must be a non-empty string",
		},
		{
			name: "MissingDefaultValueInFilterEnabled",
			desc: "verifies error when filter_enabled lacks default value",
			cfg: testutils.MarshalAny(t, &v3extauthzpb.ExtAuthz{
				Services: &v3extauthzpb.ExtAuthz_GrpcService{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "localhost:1234",
							},
						},
					},
				},
				FilterEnabled: &corepb.RuntimeFractionalPercent{},
			}),
			wantErr: "extauthz: missing default_value in filter_enabled",
		},
		{
			name: "MissingDefaultValueInDenyAtDisable",
			desc: "verifies error when deny_at_disable lacks default value",
			cfg: testutils.MarshalAny(t, &v3extauthzpb.ExtAuthz{
				Services: &v3extauthzpb.ExtAuthz_GrpcService{
					GrpcService: &corepb.GrpcService{
						TargetSpecifier: &corepb.GrpcService_GoogleGrpc_{
							GoogleGrpc: &corepb.GrpcService_GoogleGrpc{
								TargetUri: "localhost:1234",
							},
						},
					},
				},
				DenyAtDisable: &corepb.RuntimeFeatureFlag{},
			}),
			wantErr: "extauthz: missing default_value in deny_at_disable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Log(tt.desc)
			b := builder{}
			if _, err := b.ParseFilterConfig(tt.cfg, httpfilter.ParseOptions{}); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseFilterConfig() returned error = %v, wantErr containing %v", err, tt.wantErr)
			}
		})
	}
}

// Test verifies that ParseFilterConfigOverride successfully unmarshals valid
// per-route override configurations.
func (s) TestParseFilterConfigOverride_Success(t *testing.T) {
	override := testutils.MarshalAny(t, &v3extauthzpb.ExtAuthzPerRoute{
		Override: &v3extauthzpb.ExtAuthzPerRoute_Disabled{
			Disabled: true,
		},
	})

	b := builder{}
	got, err := b.ParseFilterConfigOverride(override, httpfilter.ParseOptions{})
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
		desc     string
		override proto.Message
		wantErr  string
	}{
		{
			name:     "InvalidOverrideType",
			desc:     "verifies error when input override is not an Any message",
			override: &v3extauthzpb.ExtAuthzPerRoute{},
			wantErr:  "extauthz: error parsing override config",
		},
		{
			name: "Unmarshal_Failed",
			desc: "verifies error when Any message unmarshal to ExtAuthzPerRoute fails",
			override: &anypb.Any{
				TypeUrl: "type.googleapis.com/invalid",
				Value:   []byte("invalid"),
			},
			wantErr: "extauthz: failed to unmarshal override config",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Log(tt.desc)
			b := builder{}
			if _, err := b.ParseFilterConfigOverride(tt.override, httpfilter.ParseOptions{}); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseFilterConfigOverride() returned error = %v, wantErr containing %v", err, tt.wantErr)
			}
		})
	}
}

// Test verifies that parseFilterEnabled correctly parses the
// RuntimeFractionalPercent configuration into its internal representation.
func (s) TestParseFilterEnabled(t *testing.T) {
	tests := []struct {
		name string
		fp   *corepb.RuntimeFractionalPercent
		want fraction
	}{
		{
			name: "NilFraction",
			fp:   nil,
			want: fraction{numerator: 100, denominator: 100},
		},
		{
			name: "DenominatorHundred",
			fp: &corepb.RuntimeFractionalPercent{
				DefaultValue: &v3typepb.FractionalPercent{
					Numerator:   10,
					Denominator: v3typepb.FractionalPercent_HUNDRED,
				},
			},
			want: fraction{numerator: 10, denominator: 100},
		},
		{
			name: "DenominatorMillion",
			fp: &corepb.RuntimeFractionalPercent{
				DefaultValue: &v3typepb.FractionalPercent{
					Numerator:   5,
					Denominator: v3typepb.FractionalPercent_MILLION,
				},
			},
			want: fraction{numerator: 5, denominator: 1000000},
		},
		{
			name: "DefaultDenominator",
			fp: &corepb.RuntimeFractionalPercent{
				DefaultValue: &v3typepb.FractionalPercent{
					Numerator: 25,
				},
			},
			want: fraction{numerator: 25, denominator: 100},
		},
		{
			name: "CappedToHundredPercent",
			fp: &corepb.RuntimeFractionalPercent{
				DefaultValue: &v3typepb.FractionalPercent{
					Numerator:   200,
					Denominator: v3typepb.FractionalPercent_HUNDRED,
				},
			},
			want: fraction{numerator: 100, denominator: 100},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFilterEnabled(tt.fp)
			if err != nil {
				t.Fatalf("parseFilterEnabled(%v) failed: %v", tt.fp, err)
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(fraction{})); diff != "" {
				t.Fatalf("parseFilterEnabled(%v) returned unexpected fraction (-want, +got):\n%s", tt.fp, diff)
			}
		})
	}
}
