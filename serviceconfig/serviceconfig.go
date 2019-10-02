/*
 *
 * Copyright 2019 gRPC authors.
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

// Package serviceconfig defines types and methods for operating on gRPC
// service configs.
//
// This package is EXPERIMENTAL.
package serviceconfig

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/status"
)

// Config represents an opaque data structure holding a service config.
type Config interface {
	isServiceConfig()
}

// LoadBalancingConfig represents an opaque data structure holding a load
// balancing config.
type LoadBalancingConfig interface {
	isLoadBalancingConfig()
}

// ParseResult contains a service config or an error.
type ParseResult struct {
	Config Config
	Err    error
}

// NewParseResult returns a ParseResult returning the provided parameter as
// either the Config or Err field, depending upon its type.
func NewParseResult(configOrError interface{}) *ParseResult {
	if e, ok := configOrError.(error); ok {
		return &ParseResult{Err: e}
	}
	if c, ok := configOrError.(Config); ok {
		return &ParseResult{Config: c}
	}
	grpclog.Errorf("Unexpected configOrError type: %T", configOrError)
	return &ParseResult{Err: status.Errorf(codes.Internal, "unexpected configOrError type: %T", configOrError)}
}
