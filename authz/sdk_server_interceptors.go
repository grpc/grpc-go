/*
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
 */

// Package authz exposes methods to manage authorization within gRPC.
//
// Experimental
//
// Notice: This package is EXPERIMENTAL and may be changed or removed
// in a later release.
package authz

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/xds/rbac"
	"google.golang.org/grpc/status"
)

type StaticInterceptor struct {
	// Either contains two engines deny engine followed by allow engine or only one allow engine.
	engines rbac.ChainEngine
}

// NewStatic returns a new staticInterceptor from a static authorization policy JSON string.
func NewStatic(authzPolicy string) (*StaticInterceptor, error) {
	RBACPolicies, err := translatePolicy(authzPolicy)
	if err != nil {
		return nil, err
	}
	chainEngine, err := rbac.NewChainEngine(RBACPolicies)
	if err != nil {
		return nil, err
	}
	if chainEngine.IsEmpty() {
		return nil, fmt.Errorf("Failed to initialize RBAC engines.")
	}
	return &StaticInterceptor{*chainEngine}, nil
}

// UnaryInterceptor intercepts incoming Unary RPC request.
// Only authorized requests are allowed to pass. Otherwise, unauthorized error is returned to client.
func (i *StaticInterceptor) UnaryInterceptor(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	if err := i.engines.IsAuthorized(ctx); status.Code(err) != codes.OK {
		// TODO(ashithasantosh): Currently we return errors like "incoming RPC matched a deny policy ..".
		// Instead I think this should be more general error messages like "Unauthorized RPC request denied."
		// and not expose details. And maybe we could just log the informative messages. Similarly for StreamInterceptor.
		return nil, err
	}
	return handler(ctx, req)
}

// StreamInterceptor intercepts incoming Stream RPC request.
// Only authorized requests are allowed to pass. Otherwise, unauthorized error is returned to client.
func (i *StaticInterceptor) StreamInterceptor(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
	if err := i.engines.IsAuthorized(ss.Context()); status.Code(err) != codes.OK {
		return err
	}
	return handler(srv, ss)
}
