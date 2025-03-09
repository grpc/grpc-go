/*
 *
 * Copyright 2025 gRPC authors.
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

// Package router implements the Envoy Router HTTP filter.
package router

import (
	"fmt"

	"google.golang.org/grpc/xds/internal/clients/xdsclient/internal/testutils/httpfilter"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	pb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
)

// TypeURL is the message type for the Router configuration.
const TypeURL = "type.googleapis.com/envoy.extensions.filters.http.router.v3.Router"

func init() {
	httpfilter.Register(builder{})
}

type builder struct {
}

func (builder) TypeURLs() []string { return []string{TypeURL} }

func (builder) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	// The router filter does not currently use any fields from the
	// config.  Verify type only.
	if cfg == nil {
		return nil, fmt.Errorf("router: nil configuration message provided")
	}
	m, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("router: error parsing config %v: unknown type %T", cfg, cfg)
	}
	msg := new(pb.Router)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("router: error parsing config %v: %v", cfg, err)
	}
	return config{}, nil
}

func (builder) ParseFilterConfigOverride(override proto.Message) (httpfilter.FilterConfig, error) {
	if override != nil {
		return nil, fmt.Errorf("router: unexpected config override specified: %v", override)
	}
	return config{}, nil
}

func (builder) IsTerminal() bool {
	return true
}

// The router filter does not currently support any configuration.  Verify
// type only.
type config struct {
	httpfilter.FilterConfig
}
