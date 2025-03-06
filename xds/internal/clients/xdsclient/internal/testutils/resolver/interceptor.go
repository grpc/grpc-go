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

// Package resolver provides resolver-related functionality for xDS client
// testing.
package resolver

import (
	"context"

	"google.golang.org/grpc"
)

// RPCInfo contains RPC information needed by a ConfigSelector.
type RPCInfo struct {
	// Context is the user's context for the RPC and contains headers and
	// application timeout.  It is passed for interception purposes and for
	// efficiency reasons.  SelectConfig should not be blocking.
	Context context.Context
	Method  string // i.e. "/Service/Method"
}

// ClientInterceptor is an interceptor for gRPC client streams.
type ClientInterceptor interface {
	// NewStream produces a ClientStream for an RPC which may optionally use
	// the provided function to produce a stream for delegation.  Note:
	// RPCInfo.Context should not be used (will be nil).
	//
	// done is invoked when the RPC is finished using its connection, or could
	// not be assigned a connection.  RPC operations may still occur on
	// ClientStream after done is called, since the interceptor is invoked by
	// application-layer operations.  done must never be nil when called.
	NewStream(ctx context.Context, ri RPCInfo, done func(), newStream func(ctx context.Context, done func()) (grpc.ClientStream, error)) (grpc.ClientStream, error)
}

// ServerInterceptor is an interceptor for incoming RPC's on gRPC server side.
type ServerInterceptor interface {
	// AllowRPC checks if an incoming RPC is allowed to proceed based on
	// information about connection RPC was received on, and HTTP Headers. This
	// information will be piped into context.
	AllowRPC(ctx context.Context) error // TODO: Make this a real interceptor for filters such as rate limiting.
}
