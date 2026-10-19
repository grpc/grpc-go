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

package server

import (
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/transport"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/metadata"
)

// RouteAndProcess routes the incoming RPC to a configured route in the route
// table and also processes the RPC by running the incoming RPC through any HTTP
// Filters configured.
func RouteAndProcess(ss grpc.ServerStream) (grpc.ServerStream, error) {
	ctx := ss.Context()
	conn := transport.GetConnection(ctx)
	cw, ok := conn.(*connWrapper)
	if !ok {
		return nil, errors.New("missing virtual hosts in incoming context")
	}

	rc := cw.urc.Load()
	// Error out at routing l7 level with a status code UNAVAILABLE, represents
	// an nack before usable route configuration or resource not found for RDS
	// or error combining LDS + RDS (Shouldn't happen).
	if rc.err != nil {
		if logger.V(2) {
			logger.Infof("RPC on connection with xDS Configuration error: %v", rc.err)
		}
		return nil, rc.statusErrWithNodeID(codes.Unavailable, "error from xDS configuration for matched route configuration: %v", rc.err)
	}

	mn, ok := grpc.Method(ctx)
	if !ok {
		return nil, rc.statusErrWithNodeID(codes.Internal, "missing method name in incoming context")
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, rc.statusErrWithNodeID(codes.Internal, "missing metadata in incoming context")
	}
	// A41 added logic to the core grpc implementation to guarantee that once the
	// RPC gets to this point, there will be a single, unambiguous authority
	// present in the header map. But add a defensive check to ensure authority
	// header is present.
	authority := md.Get(":authority")
	if len(authority) == 0 {
		return nil, rc.statusErrWithNodeID(codes.Internal, "no :authority header present")
	}
	idx := xdsresource.FindBestMatchingVirtualHostIndex(authority[0], rc.vhosts)
	if idx == -1 {
		return nil, rc.statusErrWithNodeID(codes.Unavailable, "the incoming RPC did not match a configured Virtual Host")
	}
	vh := &rc.vhs[idx]

	var rwi *routeWithInterceptors
	for _, r := range vh.routes {
		if r.matcher.Match(mn, md) {
			// "NonForwardingAction is expected for all Routes used on
			// server-side; a route with an inappropriate action causes RPCs
			// matching that route to fail with UNAVAILABLE." - A36
			if r.actionType != xdsresource.RouteActionNonForwardingAction {
				return nil, rc.statusErrWithNodeID(codes.Unavailable, "the incoming RPC matched to a route that was not of action type non forwarding")
			}
			rwi = &r
			break
		}
	}
	if rwi == nil {
		return nil, rc.statusErrWithNodeID(codes.Unavailable, "the incoming RPC did not match a configured Route")
	}
	if rwi.interceptor != nil {
		return rwi.interceptor.InterceptRPC(ss)
	}
	return ss, nil
}
