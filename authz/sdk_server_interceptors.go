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

package authz

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/xds/rbac"
	"google.golang.org/grpc/status"
)

// StaticInterceptor contains engines used to make authorization decisions. It
// either contains two engines deny engine followed by an allow engine or only
// one allow engine.
type StaticInterceptor struct {
	engines rbac.ChainEngine
}

// NewStatic returns a new StaticInterceptor from a static authorization policy
// JSON string.
func NewStatic(authzPolicy string) (*StaticInterceptor, error) {
	rbacs, err := translatePolicy(authzPolicy)
	if err != nil {
		return nil, err
	}
	chainEngine, err := rbac.NewChainEngine(rbacs)
	if err != nil {
		return nil, err
	}
	return &StaticInterceptor{*chainEngine}, nil
}

// UnaryInterceptor intercepts incoming Unary RPC requests.
// Only authorized requests are allowed to pass. Otherwise, an unauthorized
// error is returned to the client.
func (i *StaticInterceptor) UnaryInterceptor(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	err := i.engines.IsAuthorized(ctx)
	if err != nil {
		if status.Code(err) == codes.PermissionDenied {
			return nil, status.Errorf(codes.PermissionDenied, "unauthorized RPC request rejected")
		}
		return nil, err
	}
	return handler(ctx, req)
}

// StreamInterceptor intercepts incoming Stream RPC requests.
// Only authorized requests are allowed to pass. Otherwise, an unauthorized
// error is returned to the client.
func (i *StaticInterceptor) StreamInterceptor(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	err := i.engines.IsAuthorized(ss.Context())
	if err != nil {
		if status.Code(err) == codes.PermissionDenied {
			return status.Errorf(codes.PermissionDenied, "unauthorized RPC request rejected")
		}
		return err
	}
	return handler(srv, ss)
}
