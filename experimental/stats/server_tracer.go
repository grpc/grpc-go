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

package stats

import (
	"context"

	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// This file defines the server side of the V2 stats API. See handler.go for
// the shared types.
//
// There is no attempt scope on the server: retries and hedging are client-side
// concepts, and each server call corresponds to exactly one stream. The server
// tracer therefore plays the role of both the call tracer and the attempt
// tracer, which is also how gRPC Core models it (ServerCallTracerInterface
// derives from the same CallTracerInterface as the client attempt tracer).

// ServerCallTracer traces one server call.
//
// Implementations must embed UnimplementedServerCallTracer, must be safe for
// concurrent use, and must not block.
type ServerCallTracer interface {
	// FilterContext is invoked once, before any interceptor or the application
	// handler runs, with the context that will serve the call. The returned
	// context becomes the base for the context the application observes, which
	// allows a plugin to attach state - an extracted trace span, for example -
	// without registering an interceptor. It is invoked after
	// RecordIncomingHeaders, so a plugin may use the request headers when
	// deriving the context.
	//
	// It must return a non-nil context. Returning the argument unchanged is
	// always valid. When several handlers are registered, each receives the
	// context returned by the previous one, in registration order.
	FilterContext(ctx context.Context) context.Context

	// RecordIncomingHeaders is invoked with the headers received from the
	// client, before FilterContext.
	RecordIncomingHeaders(info *HeadersInfo)

	// MutateOutgoingHeaders is invoked before the response headers are
	// serialized, with the mutable outgoing header metadata; additions go on
	// the wire. For a trailers-only response, where headers are never sent,
	// MutateOutgoingTrailers is invoked instead.
	MutateOutgoingHeaders(md metadata.MD)

	// RecordOutgoingHeaders is invoked with the headers sent to the client,
	// for observation only; the metadata must be treated as read-only.
	RecordOutgoingHeaders(info *HeadersInfo)

	// MutateOutgoingTrailers is invoked before the response trailers are
	// serialized, with the mutable outgoing trailer metadata; additions go on
	// the wire. This is the fallback injection point for a trailers-only
	// response.
	MutateOutgoingTrailers(md metadata.MD)

	// RecordOutgoingTrailers is invoked with the trailers sent to the client,
	// for observation only.
	RecordOutgoingTrailers(info *TrailersInfo)

	// RecordIncomingMessage is invoked once per message received.
	RecordIncomingMessage(info *MessageInfo)

	// RecordOutgoingMessage is invoked once per message sent.
	RecordOutgoingMessage(info *MessageInfo)

	// RecordEnd is invoked exactly once and is the last call on this object.
	RecordEnd(info *CallEndInfo)

	// EnforceServerCallTracerEmbedding is included to force implementers to
	// embed UnimplementedServerCallTracer.
	internal.EnforceServerCallTracerEmbedding
}

// ServerCallInfo describes a server call to Handler.ServerCallTracer.
type ServerCallInfo struct {
	// Method is the full RPC method string, i.e. /package.service/method. It is
	// the method named by the client and is populated before gRPC has checked
	// whether such a method is registered, so a tracer is created - and ended -
	// even for an unknown method.
	Method string
	// RegisteredMethod reports whether Method is actually registered on this
	// server. Plugins bucket unregistered methods (which a client can set to
	// any string) to a single value such as "other", to bound metric-label
	// cardinality.
	RegisteredMethod bool
	// Headers are the headers received from the client.
	Headers metadata.MD
	// Peer describes the remote end of the connection carrying this call.
	Peer *peer.Peer
}

// UnimplementedServerCallTracer must be embedded to have forward compatible
// implementations.
type UnimplementedServerCallTracer struct {
	internal.EnforceServerCallTracerEmbedding
}

// FilterContext provides a no-op implementation, returning ctx unchanged.
func (UnimplementedServerCallTracer) FilterContext(ctx context.Context) context.Context {
	return ctx
}

// RecordIncomingHeaders provides a no-op implementation.
func (UnimplementedServerCallTracer) RecordIncomingHeaders(*HeadersInfo) {}

// MutateOutgoingHeaders provides a no-op implementation.
func (UnimplementedServerCallTracer) MutateOutgoingHeaders(metadata.MD) {}

// RecordOutgoingHeaders provides a no-op implementation.
func (UnimplementedServerCallTracer) RecordOutgoingHeaders(*HeadersInfo) {}

// MutateOutgoingTrailers provides a no-op implementation.
func (UnimplementedServerCallTracer) MutateOutgoingTrailers(metadata.MD) {}

// RecordOutgoingTrailers provides a no-op implementation.
func (UnimplementedServerCallTracer) RecordOutgoingTrailers(*TrailersInfo) {}

// RecordIncomingMessage provides a no-op implementation.
func (UnimplementedServerCallTracer) RecordIncomingMessage(*MessageInfo) {}

// RecordOutgoingMessage provides a no-op implementation.
func (UnimplementedServerCallTracer) RecordOutgoingMessage(*MessageInfo) {}

// RecordEnd provides a no-op implementation.
func (UnimplementedServerCallTracer) RecordEnd(*CallEndInfo) {}

// NopServerCallTracer is the documented way for a Handler to decline tracing
// an individual server call.
type NopServerCallTracer struct{ UnimplementedServerCallTracer }
