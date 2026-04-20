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

	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	fpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
)

func init() {
	if envconfig.XDSClientExtProcEnabled {
		httpfilter.Register(builder{})
	}
}

type builder struct{}

func (builder) TypeURLs() []string {
	return []string{"type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor"}
}

// validateBodyProcessingMode ensures that the body processing mode is either
// NONE or GRPC.
func validateBodyProcessingMode(pMode *fpb.ProcessingMode) error {
	if reqMode := pMode.GetRequestBodyMode(); reqMode != fpb.ProcessingMode_NONE && reqMode != fpb.ProcessingMode_GRPC {
		return fmt.Errorf("ext_proc: invalid request body mode %v: want 'NONE' or 'GRPC'", reqMode)
	}
	if resMode := pMode.GetResponseBodyMode(); resMode != fpb.ProcessingMode_NONE && resMode != fpb.ProcessingMode_GRPC {
		return fmt.Errorf("ext_proc: invalid response body mode %v: want 'NONE' or 'GRPC'", resMode)
	}
	return nil
}

func (builder) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("ext_proc: nil base configuration message provided")
	}
	m, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("ext_proc: error parsing config %v: unknown type %T", cfg, cfg)
	}
	msg := new(fpb.ExternalProcessor)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("ext_proc: failed to unmarshal config %v: %v", cfg, err)
	}
	if msg.GetGrpcService() == nil {
		return nil, fmt.Errorf("ext_proc: empty grpc_service provided in config %v", cfg)
	}
	if msg.GetGrpcService().GetGoogleGrpc() == nil {
		return nil, fmt.Errorf("ext_proc: only google_grpc grpc_service is supported, got %v in config %v", msg.GrpcService.GetTargetSpecifier(), cfg)
	}
	if msg.GetProcessingMode() == nil {
		return nil, fmt.Errorf("ext_proc: missing processing_mode in config %v", cfg)
	}
	if err := validateBodyProcessingMode(msg.GetProcessingMode()); err != nil {
		return nil, err
	}
	if mr := msg.GetMutationRules(); mr != nil {
		if err := mr.GetAllowExpression().Validate(); err != nil {
			return nil, fmt.Errorf("ext_proc: %v", err)
		}
		if err := mr.GetDisallowExpression().Validate(); err != nil {
			return nil, fmt.Errorf("ext_proc: %v", err)
		}
	}
	return baseConfig{config: msg}, nil
}

func (builder) ParseFilterConfigOverride(ov proto.Message) (httpfilter.FilterConfig, error) {
	if ov == nil {
		return nil, fmt.Errorf("ext_proc: nil override configuration provided")
	}
	m, ok := ov.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("ext_proc: error parsing override %v: unknown type %T", ov, ov)
	}
	msg := new(fpb.ExtProcPerRoute)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("ext_proc: failed to unmarshal override %v: %v", ov, err)
	}
	override := msg.GetOverrides()
	// GrpcService can be optionally provided in the override config. If
	// provided, it must be of type google_grpc.
	if override.GetGrpcService() != nil && override.GrpcService.GetGoogleGrpc() == nil {
		return nil, fmt.Errorf("ext_proc: only google_grpc grpc_service is supported, got %v in override %v", override.GrpcService.GetTargetSpecifier(), override)
	}
	if pm := override.GetProcessingMode(); pm != nil {
		if err := validateBodyProcessingMode(pm); err != nil {
			return nil, err
		}
	}
	return overrideConfig{config: override}, nil
}

func (builder) IsTerminal() bool {
	return false
}
