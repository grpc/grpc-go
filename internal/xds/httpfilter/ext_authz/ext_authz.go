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
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/transport"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3extauthzpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
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

func parseFilterEnabled(fp *v3corepb.RuntimeFractionalPercent) (xdsresource.FractionalPercent, error) {
	if fp == nil {
		// An unset filter_enabled means the filter applies to all requests.
		return xdsresource.FractionalPercent{Numerator: 100, Denominator: 100, PPM: 1000000}, nil
	}
	fracPercent := fp.GetDefaultValue()
	if fracPercent == nil {
		return xdsresource.FractionalPercent{}, fmt.Errorf("extauthz: missing default_value in filter_enabled")
	}
	f, err := xdsresource.NewFractionalPercent(fracPercent)
	if err != nil {
		return xdsresource.FractionalPercent{}, fmt.Errorf("extauthz: filter_enabled contains %v", err)
	}
	return f, nil
}

// grpcStatusCode converts an HTTP status code to a gRPC status code.
func grpcStatusCode(httpStatus int32) codes.Code {
	if code, ok := transport.HTTPStatusConvTab[int(httpStatus)]; ok {
		return code
	}
	return codes.Unknown
}

func (builder) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	m, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("extauthz: error parsing config %v: unknown type %T, want *anypb.Any", cfg, cfg)
	}
	msg := new(v3extauthzpb.ExtAuthz)
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
	if denyAtDisableFlag := msg.GetDenyAtDisable(); denyAtDisableFlag != nil {
		if denyAtDisableFlag.GetDefaultValue() == nil {
			return nil, fmt.Errorf("extauthz: missing default_value in deny_at_disable")
		}
		denyAtDisable = denyAtDisableFlag.GetDefaultValue().GetValue()
	}

	httpStatus := int32(http.StatusForbidden)
	if st := msg.GetStatusOnError().GetCode(); st != 0 {
		httpStatus = int32(st)
	}
	statusOnError := grpcStatusCode(httpStatus)

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

	return config{
		grpcService:                server,
		filterEnabled:              filterEnabled,
		denyAtDisable:              denyAtDisable,
		failureModeAllow:           msg.GetFailureModeAllow(),
		failureModeAllowHeaderAdd:  msg.GetFailureModeAllowHeaderAdd(),
		statusOnError:              statusOnError,
		allowedHeaders:             allowedHeaders,
		disallowedHeaders:          disallowedHeaders,
		decoderHeaderMutationRules: mutationRules,
		includePeerCertificate:     msg.GetIncludePeerCertificate(),
	}, nil
}

// ParseFilterConfigOverride parses the provided override configuration.
//
// Note that ExtAuthzPerRoute is unmarshaled to verify its syntax during xDS
// resource validation, no filter configuration object is returned. Per-route
// disabling is supported via the generic FilterConfig wrapper mechanism rather
// than the ExtAuthzPerRoute.disabled field directly.
func (builder) ParseFilterConfigOverride(overrideCfg proto.Message) (httpfilter.FilterConfig, error) {
	m, ok := overrideCfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("extauthz: error parsing override config %v: unknown type %T, want *anypb.Any", overrideCfg, overrideCfg)
	}
	msg := new(v3extauthzpb.ExtAuthzPerRoute)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("extauthz: failed to unmarshal override config %v: %v", overrideCfg, err)
	}
	return nil, nil
}

func (builder) IsTerminal() bool {
	return false
}
