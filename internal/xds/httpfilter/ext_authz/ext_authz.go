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

// Package extauthz implements the xDS External Authorization HTTP filter.
package extauthz

import (
	"fmt"

	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3extauthzfilterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
)

func init() {
	if envconfig.XDSClientExtAuthzEnabled {
		httpfilter.Register(builder{})
	}
}

var (
	// TODO: Remove this once gRFC A102 is implemented.
	parseGRPCServiceConfig = func(*v3corepb.GrpcService) (xdsresource.GRPCServiceConfig, error) {
		return xdsresource.GRPCServiceConfig{}, fmt.Errorf("parseGRPCServiceConfig not implemented")
	}
)

type builder struct{}

func (builder) TypeURLs() []string {
	return []string{
		"type.googleapis.com/envoy.extensions.filters.http.ext_authz.v3.ExtAuthz",
		"type.googleapis.com/envoy.extensions.filters.http.ext_authz.v3.ExtAuthzPerRoute",
	}
}

func parseFilterEnabled(fp *v3corepb.RuntimeFractionalPercent) (fraction, error) {
	if fp == nil {
		return fraction{numerator: 100, denominator: 100}, nil
	}
	pct := fp.GetDefaultValue()
	if pct == nil {
		return fraction{}, fmt.Errorf("extauthz: missing default_value in filter_enabled")
	}

	num := pct.GetNumerator()
	switch pct.GetDenominator() {
	case v3typepb.FractionalPercent_HUNDRED:
		return fraction{numerator: num, denominator: 100}, nil
	case v3typepb.FractionalPercent_TEN_THOUSAND:
		return fraction{numerator: num, denominator: 10000}, nil
	case v3typepb.FractionalPercent_MILLION:
		return fraction{numerator: num, denominator: 1000000}, nil
	}
	return fraction{numerator: num, denominator: 100}, nil
}

func (builder) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	m, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("extauthz: error parsing config %v: unknown type %T, want *anypb.Any", cfg, cfg)
	}
	msg := new(v3extauthzfilterpb.ExtAuthz)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("extauthz: failed to unmarshal config: %v", err)
	}

	if msg.GetGrpcService() == nil {
		return nil, fmt.Errorf("extauthz: empty grpc_service provided in config %v", cfg)
	}
	server, err := parseGRPCServiceConfig(msg.GetGrpcService())
	if err != nil {
		return nil, fmt.Errorf("extauthz: failed to parse grpc_service: %v", err)
	}

	filterEnabled, err := parseFilterEnabled(msg.GetFilterEnabled())
	if err != nil {
		return nil, err
	}

	var denyAtDisable bool
	if df := msg.GetDenyAtDisable(); df != nil {
		if df.GetDefaultValue() == nil {
			return nil, fmt.Errorf("extauthz: missing default_value in deny_at_disable")
		}
		denyAtDisable = df.GetDefaultValue().GetValue()
	}

	mutationRules, err := httpfilter.HeaderMutationRulesFromProto(msg.GetDecoderHeaderMutationRules())
	if err != nil {
		return nil, err
	}

	var allowedHeaders, disallowedHeaders []matcher.StringMatcher
	if allowed := msg.GetAllowedHeaders(); allowed != nil {
		allowedHeaders, err = httpfilter.ConvertStringMatchers(allowed.GetPatterns())
		if err != nil {
			return nil, err
		}
	}

	if disallowed := msg.GetDisallowedHeaders(); disallowed != nil {
		disallowedHeaders, err = httpfilter.ConvertStringMatchers(disallowed.GetPatterns())
		if err != nil {
			return nil, err
		}
	}

	return baseConfig{
		grpcService:                server,
		filterEnabled:              filterEnabled,
		denyAtDisable:              denyAtDisable,
		failureModeAllow:           msg.GetFailureModeAllow(),
		failureModeAllowHeaderAdd:  msg.GetFailureModeAllowHeaderAdd(),
		statusOnError:              int32(msg.GetStatusOnError().GetCode()),
		allowedHeaders:             allowedHeaders,
		disallowedHeaders:          disallowedHeaders,
		decoderHeaderMutationRules: mutationRules,
		includePeerCertificate:     msg.GetIncludePeerCertificate(),
	}, nil
}

func (builder) ParseFilterConfigOverride(overrideCfg proto.Message) (httpfilter.FilterConfig, error) {
	m, ok := overrideCfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("extauthz: error parsing override config %v: unknown type %T, want *anypb.Any", overrideCfg, overrideCfg)
	}
	msg := new(v3extauthzfilterpb.ExtAuthzPerRoute)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("extauthz: failed to unmarshal override config %v: %v", overrideCfg, err)
	}
	return nil, nil
}

func (builder) IsTerminal() bool {
	return false
}
