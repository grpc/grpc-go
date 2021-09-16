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
	"google.golang.org/grpc/internal/xds/env"
	"google.golang.org/grpc/internal/xds/rbac"
	"google.golang.org/grpc/xds/internal/httpfilter"
	"google.golang.org/protobuf/types/known/anypb"

	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	rpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
)

func init() {
	if env.RBACSupport {
		httpfilter.Register(builder{})
	}
}

// RegisterForTesting registers the RBAC HTTP Filter for testing purposes, regardless
// of the RBAC environment variable. This is needed because there is no way to set the RBAC
// environment variable to true in a test before init() in this package is run.
func RegisterForTesting() {
	httpfilter.Register(builder{})
}

// UnregisterForTesting unregisters the RBAC HTTP Filter for testing purposes. This is needed because
// there is no way to unregister the HTTP Filter after registering it solely for testing purposes using
// rbac.RegisterForTesting()
func UnregisterForTesting() {
	for _, typeURL := range builder.TypeURLs(builder{}) {
		httpfilter.UnregisterForTesting(typeURL)
	}
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
func parseConfig(rbacCfg *rpb.RBAC) (httpfilter.FilterConfig, error) {
	// All the validation logic described in A41.
	for _, policy := range rbacCfg.GetRules().GetPolicies() {
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

	return config{config: rbacCfg}, nil
}

func (builder) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("rbac: nil configuration message provided")
	}
	any, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("rbac: error parsing config %v: unknown type %T", cfg, cfg)
	}
	msg := new(rpb.RBAC)
	if err := ptypes.UnmarshalAny(any, msg); err != nil {
		return nil, fmt.Errorf("rbac: error parsing config %v: %v", cfg, err)
	}
	return parseConfig(msg)
}

func (builder) ParseFilterConfigOverride(override proto.Message) (httpfilter.FilterConfig, error) {
	if override == nil {
		return nil, fmt.Errorf("rbac: nil configuration message provided")
	}
	any, ok := override.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("rbac: error parsing override config %v: unknown type %T", override, override)
	}
	msg := new(rpb.RBACPerRoute)
	if err := ptypes.UnmarshalAny(any, msg); err != nil {
		return nil, fmt.Errorf("rbac: error parsing override config %v: %v", override, err)
	}
	return parseConfig(msg.Rbac)
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
		return nil, nil
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
	return i.chainEngine.IsAuthorized(ctx)
}
