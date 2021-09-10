/*
 *
 * Copyright 2021 gRPC authors.
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

// Package rbac implements the Envoy RBAC HTTP filter.
package rbac

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/xds/rbac"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/xds/internal/httpfilter"
	"google.golang.org/protobuf/types/known/anypb"

	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	rpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
)

func init() {
	httpfilter.Register(builder{})
}

type builder struct {
}

type config struct {
	httpfilter.FilterConfig
	config *rpb.RBAC
}

func (builder) TypeURLs() []string {
	return []string{
		"type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBAC",
		"type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBACPerRoute",
	}
}

// Parsing is the same for the base config and the override config.
func parseConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("rbac: nil configuration message provided")
	}
	any, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("rbac: error parsing config %v: unknown type %T", cfg, cfg)
	}
	// All the validation logic described in A41.
	msg := new(rpb.RBAC)
	if err := ptypes.UnmarshalAny(any, msg); err != nil {
		return nil, fmt.Errorf("rbac: error parsing config %v: %v", cfg, err)
	}
	for _, policy := range msg.GetRules().GetPolicies() {
		// "Policy.condition and Policy.checked_condition must cause a
		// validation failure if present." - A41
		if policy.Condition != nil {
			return nil, errors.New("rbac: Policy.condition is present")
		}
		if policy.CheckedCondition != nil {
			return nil, errors.New("rbac: policy.CheckedCondition is present")
		}

		// "It is also a validation failure if Permission or Principal has a
		// header matcher for a grpc- prefixed header name or :scheme." - A41
		for _, principal := range policy.Principals {
			if principal.GetHeader() != nil {
				name := principal.GetHeader().GetName()
				if name == ":scheme" || strings.HasPrefix(name, "grpc-") {
					return nil, fmt.Errorf("rbac: principal header matcher for %v is :scheme or starts with grpc", name)
				}
			}
		}
		for _, permission := range policy.Permissions {
			if permission.GetHeader() != nil {
				name := permission.GetHeader().GetName()
				if name == ":scheme" || strings.HasPrefix(name, "grpc-") {
					return nil, fmt.Errorf("rbac: permission header matcher for %v is :scheme or starts with grpc", name)
				}
			}
		}
	}

	return config{config: msg}, nil
}

func (builder) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	return parseConfig(cfg)
}

func (builder) ParseFilterConfigOverride(override proto.Message) (httpfilter.FilterConfig, error) {
	any, ok := override.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("rbac: error parsing override config %v: unknown type %T", override, override)
	}
	msg := new(rpb.RBACPerRoute)
	if err := ptypes.UnmarshalAny(any, msg); err != nil {
		return nil, fmt.Errorf("rbac: error parsing override config %v: %v", override, err)
	}
	any, err := ptypes.MarshalAny(msg.Rbac)
	if err != nil {
		// This shouldn't happen.
		return nil, fmt.Errorf("rbac: error marshaling override config %v", err)
	}
	return parseConfig(any)
}

func (builder) IsTerminal() bool {
	return false
}

var _ httpfilter.ServerInterceptorBuilder = builder{}

// BuildServerInterceptor is an optional interface builder implements in order
// to signify it works server side.
func (builder) BuildServerInterceptor(cfg httpfilter.FilterConfig, override httpfilter.FilterConfig) (resolver.ServerInterceptor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("rbac: nil config provided")
	}

	c, ok := cfg.(config)
	if !ok {
		return nil, fmt.Errorf("rbac: incorrect config type provided (%T): %v", cfg, cfg)
	}

	if override != nil {
		// override completely replaces the listener configuration; but we
		// still validate the listener config type.
		c, ok = override.(config)
		if !ok {
			return nil, fmt.Errorf("rbac: incorrect override config type provided (%T): %v", override, override)
		}
	}

	icfg := c.config
	// "If absent, no enforcing RBAC policy will be applied" - RBAC
	// Documentation for Rules field.
	if icfg.Rules == nil {
		return &interceptor{}, nil
	}

	ce, err := rbac.NewChainEngine([]*v3rbacpb.RBAC{icfg.Rules})
	if err != nil {
		return nil, fmt.Errorf("error constructing matching engine: %v", err)
	}
	return &interceptor{chainEngine: ce}, nil
}

type interceptor struct {
	chainEngine *rbac.ChainEngine
}

func (i *interceptor) AllowRPC(ctx context.Context) error {
	// "If absent, no enforcing RBAC policy will be applied" - RBAC
	// Documentation.
	if i.chainEngine == nil {
		return nil
	}
	// ":method can be hard-coded to POST if unavailable" - A41
	ctx = metadata.AppendToIncomingContext(ctx, ":method", "POST")
	return i.chainEngine.IsAuthorized(ctx)
}
