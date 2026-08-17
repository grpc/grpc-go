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
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	iextauthz "google.golang.org/grpc/internal/xds/httpfilter/ext_authz/internal"

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

var cmpOpts = []cmp.Option{
	cmp.AllowUnexported(
		config{},
		xdsresource.GRPCServiceConfig{},
		fraction{},
	),
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
	origParseGRPCServiceConfig := iextauthz.ParseGRPCServiceConfig
	defer func() { iextauthz.ParseGRPCServiceConfig = origParseGRPCServiceConfig }()
	iextauthz.ParseGRPCServiceConfig = iextauthz.ParseGRPCServiceConfigForTesting

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
				grpcService: xdsresource.GRPCServiceConfig{
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
	origParseGRPCServiceConfig := iextauthz.ParseGRPCServiceConfig
	defer func() { iextauthz.ParseGRPCServiceConfig = origParseGRPCServiceConfig }()
	iextauthz.ParseGRPCServiceConfig = iextauthz.ParseGRPCServiceConfigForTesting

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

// Test verifies that BuildClientInterceptor returns appropriate errors for
// invalid inputs or failures.
func (s) TestBuildClientInterceptor_Failure(t *testing.T) {
	tests := []struct {
		name       string
		cfg        httpfilter.FilterConfig
		wantErrStr string
	}{
		{
			name:       "InvalidConfigType",
			cfg:        httpfilter.DisabledFilterConfig{},
			wantErrStr: "extauthz: incorrect config type provided",
		},
		{
			name: "ChannelCreationFailure",
			cfg: config{
				grpcService: xdsresource.GRPCServiceConfig{TargetURI: "localhost:1234"},
			},
			wantErrStr: "extauthz: failed to create channel to the external authorization server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := iextauthz.CreateExtAuthzChannel
			iextauthz.CreateExtAuthzChannel = func(xdsresource.GRPCServiceConfig) (grpc.ClientConnInterface, func() error, error) {
				return nil, nil, fmt.Errorf("injected error")
			}
			t.Cleanup(func() { iextauthz.CreateExtAuthzChannel = orig })

			cf := builder{}.BuildClientFilter(httpfilter.ClientFilterOptions{})
			defer cf.Close()

			if _, err := cf.BuildClientInterceptor(tt.cfg, nil); err == nil || !strings.Contains(err.Error(), tt.wantErrStr) {
				t.Fatalf("BuildClientInterceptor() returned error = %v, want error containing %q", err, tt.wantErrStr)
			}
		})
	}
}

func buildInterceptor(t *testing.T, cf httpfilter.ClientFilter, cfg httpfilter.FilterConfig) httpfilter.ClientInterceptor {
	t.Helper()
	intptr, err := cf.BuildClientInterceptor(cfg, nil)
	if err != nil {
		t.Fatalf("BuildClientInterceptor() failed: %v", err)
	}
	return intptr
}

// Test verifies that channels are shared when configurations have identical
// service config (matching TargetURI, ChannelCredentials, and CallCredentials)
// and isolated when any of these three fields differ.
func (s) TestBuildClientInterceptor_ChannelSharingAndIsolation(t *testing.T) {
	// Override createExtAuthzChannel for testing to track how many times a new
	// channel is dialed.
	origCreateExtAuthzChannel := iextauthz.CreateExtAuthzChannel
	var dialCount int
	iextauthz.CreateExtAuthzChannel = func(cfg xdsresource.GRPCServiceConfig) (grpc.ClientConnInterface, func() error, error) {
		dialCount++
		conn, err := grpc.NewClient(cfg.TargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, nil, err
		}
		return conn, conn.Close, nil
	}
	defer func() { iextauthz.CreateExtAuthzChannel = origCreateExtAuthzChannel }()

	cf := builder{}.BuildClientFilter(httpfilter.ClientFilterOptions{})
	defer cf.Close()

	cfg1 := config{
		grpcService: xdsresource.GRPCServiceConfig{
			TargetURI:          "localhost:1234",
			ChannelCredentials: "creds1",
			CallCredentials:    "callCreds1",
		},
	}
	cfg2 := cfg1

	// cfg3 has a different TargetURI than cfg1 and cfg2.
	cfg3 := config{
		grpcService: xdsresource.GRPCServiceConfig{
			TargetURI:          "localhost:5678",
			ChannelCredentials: "creds1",
			CallCredentials:    "callCreds1",
		},
	}

	// cfg4 has a different ChannelCredentials than cfg1 and cfg2.
	cfg4 := config{
		grpcService: xdsresource.GRPCServiceConfig{
			TargetURI:          "localhost:1234",
			ChannelCredentials: "creds2",
			CallCredentials:    "callCreds1",
		},
	}

	// Build the first interceptor with cfg1. Since this is the first request for
	// this configuration key, a new channel should be created, incrementing
	// dialCount to 1.
	interceptor1 := buildInterceptor(t, cf, cfg1)
	defer interceptor1.Close()
	if dialCount != 1 {
		t.Fatalf("Unexpected dialCount: got %d, want 1", dialCount)
	}

	// Build an interceptor with cfg2, which has the exact same service config
	// as cfg1. The client filter should share the existing gRPC channel
	// instead of creating a new one, so dialCount should remain 1.
	interceptor2 := buildInterceptor(t, cf, cfg2)
	defer interceptor2.Close()
	if dialCount != 1 {
		t.Fatalf("Unexpected dialCount: got %d, want 1", dialCount)
	}

	// Build an interceptor with cfg3, which has a different TargetURI.
	// Since no cached channel exists for this new key, a new gRPC channel
	// must be created, incrementing the dialCount to 2.
	interceptor3 := buildInterceptor(t, cf, cfg3)
	defer interceptor3.Close()
	if dialCount != 2 {
		t.Fatalf("Unexpected dialCount: got %d, want 2", dialCount)
	}

	// Build an interceptor with cfg4, which has different ChannelCredentials.
	// Since the channel key includes ChannelCredentials, a new gRPC channel
	// must be created, incrementing dialCount to 3.
	interceptor4 := buildInterceptor(t, cf, cfg4)
	defer interceptor4.Close()
	if dialCount != 3 {
		t.Fatalf("Unexpected dialCount: got %d, want 3", dialCount)
	}
}

// Test verifies that channels are cleaned up when reference count reaches
// zero.
func (s) TestBuildClientInterceptor_ChannelCleanup(t *testing.T) {
	// Override createExtAuthzChannel for testing to track how many times a new
	// channel is dialed and closed.
	origCreateExtAuthzChannel := iextauthz.CreateExtAuthzChannel
	defer func() { iextauthz.CreateExtAuthzChannel = origCreateExtAuthzChannel }()

	var dialCount int
	var closeCount int
	iextauthz.CreateExtAuthzChannel = func(cfg xdsresource.GRPCServiceConfig) (grpc.ClientConnInterface, func() error, error) {
		dialCount++
		conn, err := grpc.NewClient(cfg.TargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, nil, err
		}
		return conn, func() error {
			closeCount++
			return conn.Close()
		}, nil
	}

	cf := builder{}.BuildClientFilter(httpfilter.ClientFilterOptions{})
	defer cf.Close()

	cfg := config{
		grpcService: xdsresource.GRPCServiceConfig{
			TargetURI:          "localhost:1234",
			ChannelCredentials: "creds1",
			CallCredentials:    "callCreds1",
		},
	}

	// Build the first interceptor with cfg. Since this is the initial request
	// for this configuration key, a new channel is created.
	intptr1 := buildInterceptor(t, cf, cfg)
	if dialCount != 1 {
		t.Fatalf("Unexpected dialCount: got %d, want 1", dialCount)
	}

	// Build a second interceptor with the exact same config key. The existing
	// gRPC channel should be shared and its reference count incremented to 2.
	intptr2 := buildInterceptor(t, cf, cfg)
	if dialCount != 1 {
		t.Fatalf("Unexpected dialCount: got %d, want 1", dialCount)
	}

	// Close the first interceptor, decrementing the reference count from 2 to 1.
	// Because the reference count is still greater than zero, the channel should
	// not be closed yet.
	intptr1.Close()
	if closeCount != 0 {
		t.Fatalf("Unexpected closeCount: got %d, want 0", closeCount)
	}

	// Close the second interceptor, decrementing the reference count to zero.
	// This should trigger the cleanup callback, deleting the channel from the
	// cache and closing the underlying gRPC connection.
	intptr2.Close()
	if closeCount != 1 {
		t.Fatalf("Unexpected closeCount: got %d, want 1", closeCount)
	}

	// Recreating an interceptor with the same config after the previous channel
	// was cleaned up and removed from the cache should trigger a new dial.
	intptr3 := buildInterceptor(t, cf, cfg)
	defer intptr3.Close()
	if dialCount != 2 {
		t.Fatalf("Unexpected dialCount: got %d, want 2", dialCount)
	}
}

// Test verifies that NewStream returns an error when the interceptor is closed.
func (s) TestClientInterceptor_Closed(t *testing.T) {
	origCreateExtAuthzChannel := iextauthz.CreateExtAuthzChannel
	iextauthz.CreateExtAuthzChannel = func(cfg xdsresource.GRPCServiceConfig) (grpc.ClientConnInterface, func() error, error) {
		conn, err := grpc.NewClient(cfg.TargetURI, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, nil, err
		}
		return conn, conn.Close, nil
	}
	defer func() { iextauthz.CreateExtAuthzChannel = origCreateExtAuthzChannel }()

	cf := builder{}.BuildClientFilter(httpfilter.ClientFilterOptions{})
	defer cf.Close()

	cfg := config{
		grpcService: xdsresource.GRPCServiceConfig{
			TargetURI: "localhost:1234",
		},
	}

	intptr := buildInterceptor(t, cf, cfg)
	intptr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const wantErr = "extauthz: interceptor is closed"
	newStream := func(context.Context, ...grpc.CallOption) (grpc.ClientStream, error) {
		return nil, nil
	}
	if _, err := intptr.NewStream(ctx, resolver.RPCInfo{}, newStream); err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("NewStream() returned unexpected results, got %q , want error containing %q", err, wantErr)
	}
}
