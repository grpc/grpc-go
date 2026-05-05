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

// Package extproc implements the Envoy external processing filter.
package extproc

import (
	"fmt"
	"time"

	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3procfilterpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
)

func init() {
	if envconfig.XDSClientExtProcEnabled {
		httpfilter.Register(builder{})
	}
}

var serverConfigFromGrpcService = func(*v3corepb.GrpcService) (*httpfilter.ServerConfig, error) {
	return nil, fmt.Errorf("extproc: serverConfigFromGrpcService not implemented")
}

const defaultDeferredCloseTimeout = 5 * time.Second

type builder struct{}

func (builder) TypeURLs() []string {
	return []string{"type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor"}
}

// validateBodyProcessingMode ensures that the body processing mode is either
// NONE or GRPC.
func validateBodyProcessingMode(mode *v3procfilterpb.ProcessingMode) error {
	if m := mode.GetRequestBodyMode(); m != v3procfilterpb.ProcessingMode_NONE && m != v3procfilterpb.ProcessingMode_GRPC {
		return fmt.Errorf("extproc: invalid request body mode %v: want %q or %q", m, "NONE", "GRPC")
	}
	if m := mode.GetResponseBodyMode(); m != v3procfilterpb.ProcessingMode_NONE && m != v3procfilterpb.ProcessingMode_GRPC {
		return fmt.Errorf("extproc: invalid response body mode %v: want %q or %q", m, "NONE", "GRPC")
	}
	return nil
}

func validateServerConfig(cfg *httpfilter.ServerConfig) error {
	// TODO(https://github.com/grpc/grpc-go/issues/8747): Once we have a common
	// way to validate a target URI, switch to that.
	if cfg.TargetURI == "" {
		return fmt.Errorf("extproc: targetURI must be a non-empty string")
	}
	if cfg.ChannelCredentials == nil {
		return fmt.Errorf("extproc: channelCredentials must be non-nil")
	}
	if cfg.Timeout < 0 {
		return fmt.Errorf("extproc: timeout must be non-negative")
	}
	for k, vals := range cfg.InitialMetadata {
		if len(k) == 0 || len(k) >= 16384 {
			return fmt.Errorf("extproc: initialMetadata key %q has invalid length %d; must be in range [1, 16384)", k, len(k))
		}
		for _, v := range vals {
			if len(v) >= 16384 {
				return fmt.Errorf("extproc: initialMetadata value for key %q has invalid length %d; must be less than 16384", k, len(v))
			}
		}
	}
	return nil
}

func (builder) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("extproc: nil base configuration message provided")
	}
	m, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("extproc: error parsing config %v: unknown type %T , want *anypb.Any", cfg, cfg)
	}
	msg := new(v3procfilterpb.ExternalProcessor)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("extproc: failed to unmarshal config %v: %v", cfg, err)
	}
	if msg.GetProcessingMode() == nil {
		return nil, fmt.Errorf("extproc: missing processing_mode in config %v", cfg)
	}
	if err := validateBodyProcessingMode(msg.GetProcessingMode()); err != nil {
		return nil, err
	}
	iCfg := interceptorConfig{
		processingModes:          processingModesFromProto(msg.GetProcessingMode()),
		requestAttributes:        msg.GetRequestAttributes(),
		responseAttributes:       msg.GetResponseAttributes(),
		disableImmediateResponse: msg.GetDisableImmediateResponse(),
		observabilityMode:        msg.GetObservabilityMode(),
	}

	failureModeAllow := msg.GetFailureModeAllow()
	iCfg.failureModeAllow = &failureModeAllow

	if msg.GetGrpcService() == nil {
		return nil, fmt.Errorf("extproc: empty grpc_service provided in config %v", cfg)
	}
	if msg.GetGrpcService().GetGoogleGrpc() == nil {
		return nil, fmt.Errorf("extproc: only google_grpc grpc_service is supported, got %v in config %v", msg.GrpcService.GetTargetSpecifier(), cfg)
	}
	server, err := serverConfigFromGrpcService(msg.GetGrpcService())
	if err != nil {
		return nil, err
	}
	if err := validateServerConfig(server); err != nil {
		return nil, err
	}
	iCfg.server = server

	mr, err := httpfilter.HeaderMutationRulesFromProto(msg.GetMutationRules())
	if err != nil {
		return nil, err
	}
	iCfg.mutationRules = mr

	if allowed := msg.GetForwardRules().GetAllowedHeaders(); allowed != nil {
		allowedHeaders, err := httpfilter.ConvertStringMatchers(allowed.GetPatterns())
		if err != nil {
			return nil, err
		}
		iCfg.allowedHeaders = allowedHeaders
	}

	if disallowed := msg.GetForwardRules().GetDisallowedHeaders(); disallowed != nil {
		disallowedHeaders, err := httpfilter.ConvertStringMatchers(disallowed.GetPatterns())
		if err != nil {
			return nil, err
		}
		iCfg.disallowedHeaders = disallowedHeaders
	}

	if msg.GetDeferredCloseTimeout() != nil {
		iCfg.deferredCloseTimeout = msg.GetDeferredCloseTimeout().AsDuration()
	} else {
		iCfg.deferredCloseTimeout = defaultDeferredCloseTimeout
	}

	return baseConfig{config: iCfg}, nil
}

func (builder) ParseFilterConfigOverride(ov proto.Message) (httpfilter.FilterConfig, error) {
	if ov == nil {
		return nil, fmt.Errorf("extproc: nil override configuration provided")
	}
	m, ok := ov.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("extproc: error parsing override %v: unknown type %T, want *anypb.Any", ov, ov)
	}
	msg := new(v3procfilterpb.ExtProcPerRoute)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("extproc: failed to unmarshal override %v: %v", ov, err)
	}
	override := msg.GetOverrides()

	if pm := override.GetProcessingMode(); pm != nil {
		if err := validateBodyProcessingMode(pm); err != nil {
			return nil, err
		}
	}
	iCfg := interceptorConfig{
		processingModes:    processingModesFromProto(override.GetProcessingMode()),
		requestAttributes:  override.GetRequestAttributes(),
		responseAttributes: override.GetResponseAttributes(),
	}

	if override.GetGrpcService() != nil {
		// GrpcService can be optionally provided in the override config. If
		// provided, it must be of type google_grpc.
		if override.GrpcService.GetGoogleGrpc() == nil {
			return nil, fmt.Errorf("extproc: only google_grpc grpc_service is supported, got %v in override %v", override.GrpcService.GetTargetSpecifier(), override)
		}
		server, err := serverConfigFromGrpcService(override.GetGrpcService())
		if err != nil {
			return nil, err
		}
		if err := validateServerConfig(server); err != nil {
			return nil, err
		}
		iCfg.server = server
	}

	return overrideConfig{config: iCfg}, nil
}

func (builder) IsTerminal() bool {
	return false
}
