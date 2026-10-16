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

// This file defines the V2 stats handler API, the intended successor to
// google.golang.org/grpc/stats.Handler.
//
// V1 delivers every event through a single HandleRPC(context.Context,
// RPCStats) method, correlates state by stashing it in the context, and
// discriminates events with a type switch. It is also attempt-scoped only:
// there is no object or event representing the call as a whole, which is why
// V1 telemetry plugins must additionally install interceptors to observe a
// call boundary.
//
// V2 replaces both mechanisms with one tracer object per scope, mirroring
// gRPC Core's StatsPlugin/ClientCallTracer/CallAttemptTracer and gRPC Java's
// ClientStreamTracer/ServerStreamTracer: gRPC creates a tracer for each call
// and asks that tracer for a child tracer per attempt, then reports events as
// ordinary typed method calls - no context values, no type switches.
//
// # Experimental
//
// Notice: All types and functions in this API are EXPERIMENTAL and may be
// changed or removed in a later release. They are additionally gated: unless
// the GRPC_EXPERIMENTAL_ENABLE_STATS_HANDLER_V2 environment variable is set to
// "true", registering a Handler has no effect and no tracer is ever created.

// Handler is the unit of registration for the V2 stats API. A single Handler
// provides per-call tracers for client and server calls.
//
// A Handler that also wants to receive the non-per-call metrics recorded by LB
// policies, resolvers and the channel itself (gRFC A79) additionally
// implements MetricsRecorder - typically by embedding it, exactly as a V1
// stats.Handler does today. gRPC discovers that the same way it does for V1
// handlers, by a type assertion at registration; MetricsRecorder is
// deliberately NOT embedded in Handler, so that a tracing-only Handler is not
// forced to be a recorder and the recorder list stays free of no-op entries.
//
// A Handler may be registered on multiple channels and servers. gRPC does not
// manage its lifecycle, nor that of any telemetry backend it uses; flushing
// and shutdown are the application's responsibility. There is no Close method,
// matching V1.
//
// Implementations must embed UnimplementedHandler.
type Handler interface {
	// ClientCallTracer returns a tracer for a new client call.
	//
	// ctx is the application's context for the call, provided so that a plugin
	// can read values the application placed there - an ambient trace span to
	// parent from, propagation baggage, and similar. It is derived with
	// context.WithoutCancel, so it carries the application's values but is
	// never cancelled and has no deadline: a plugin cannot wait on it, and
	// retaining it cannot extend or observe the call's lifetime. Cancellation
	// is reported as an error to RecordEnd instead. Values gRPC itself defines
	// are lifted onto ClientCallInfo rather than left in ctx.
	//
	// It must not return nil. To decline tracing an individual call, return
	// NopClientCallTracer{}. nil is not the opt-out because a nil pointer
	// stored in a non-nil interface (a "typed nil") is indistinguishable from
	// a real tracer at the call site and panics when a method is invoked on
	// it; requiring a non-nil value removes that failure mode from the API.
	ClientCallTracer(ctx context.Context, info *ClientCallInfo) ClientCallTracer

	// ServerCallTracer returns a tracer for a new server call. It must not
	// return nil; return NopServerCallTracer{} to decline.
	ServerCallTracer(info *ServerCallInfo) ServerCallTracer

	// EnforceStatsHandlerV2Embedding is included to force implementers to embed
	// UnimplementedHandler, allowing gRPC to add methods without breaking
	// users.
	internal.EnforceStatsHandlerV2Embedding
}

// HeadersInfo describes a set of headers observed on a call.
type HeadersInfo struct {
	// Headers is the header metadata. It must be treated as read-only; to add
	// headers to an outgoing request or response use the mutate hook
	// (MutateOutgoingHeaders), not this field.
	Headers metadata.MD
	// WireLength is the size of the headers on the wire. It is 0 when the size
	// is not known at the point the event is reported, which is the case for
	// outgoing headers, because HPACK compression happens afterwards.
	WireLength int
	// Compression is the message compression algorithm negotiated for the
	// call, as named by the grpc-encoding header. It is reported explicitly
	// because gRPC strips that header before the metadata reaches a tracer, so
	// it cannot be recovered from Headers.
	Compression string
	// Peer describes the remote end of the connection. It is nil on outgoing
	// server headers.
	Peer *peer.Peer
}

// TrailersInfo describes a set of trailers observed on a call.
type TrailersInfo struct {
	// Trailers is the trailer metadata. It must be treated as read-only.
	Trailers metadata.MD
	// WireLength is the size of the trailers on the wire, or 0 when not known
	// at the point the event is reported.
	WireLength int
}

// MessageInfo describes one message sent or received.
//
// Unlike V1's InPayload and OutPayload, it does not expose the decoded
// application message: no in-tree telemetry consumer reads it, exposing it
// invites mutation of a message gRPC still owns, and retaining it extends the
// message's lifetime unpredictably. Code that needs message bodies should use
// binary logging or an interceptor.
type MessageInfo struct {
	// SeqNo is the 0-based index of this message within its own direction on
	// this attempt or stream. Send and receive are numbered independently.
	SeqNo int
	// Length is the size of the uncompressed message, excluding any framing.
	Length int
	// CompressedLength is the size of the message as compressed, excluding any
	// framing. It equals Length when no compression is applied.
	CompressedLength int
	// WireLength is CompressedLength plus gRPC's 5-byte message framing. It
	// excludes HTTP/2 framing.
	WireLength int
}

// CallEndInfo describes the completion of a call. It is used by both the
// client call tracer and the server call tracer.
//
// It carries no timestamps: a tracer is notified at the moment each event
// occurs, so a plugin that needs a duration takes its own reading when the
// tracer is created and again here. This keeps gRPC out of the business of
// choosing a clock.
type CallEndInfo struct {
	// Error is the error the call ended with, or nil on success. It can be
	// converted to a status with status.FromError.
	Error error
}

// Annotation values gRPC records via the RecordAnnotation methods. The set is
// open and documented rather than a closed enum, so new one-shot annotations
// can be added without an API change; a plugin should ignore annotations it
// does not recognize.
const (
	// AnnotationDelayedPickComplete marks the moment a client attempt that was
	// blocked waiting for an LB pick becomes unblocked. Recorded on the
	// ClientAttemptTracer.
	AnnotationDelayedPickComplete = "Delayed LB pick complete"
	// AnnotationNameResolutionComplete marks the moment a call that was blocked
	// waiting for the first name resolution result becomes unblocked. Recorded
	// on the ClientCallTracer.
	AnnotationNameResolutionComplete = "Delayed name resolution complete"
)

// UnimplementedHandler must be embedded to have forward compatible
// implementations.
//
// It does not provide MetricsRecorder: that is an optional, separately
// discovered capability (see Handler), so a Handler that wants it embeds
// MetricsRecorder in its own type.
type UnimplementedHandler struct {
	internal.EnforceStatsHandlerV2Embedding
}

// ClientCallTracer provides a no-op implementation.
func (UnimplementedHandler) ClientCallTracer(context.Context, *ClientCallInfo) ClientCallTracer {
	return NopClientCallTracer{}
}

// ServerCallTracer provides a no-op implementation.
func (UnimplementedHandler) ServerCallTracer(*ServerCallInfo) ServerCallTracer {
	return NopServerCallTracer{}
}
