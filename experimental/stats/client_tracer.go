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
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// This file defines the client side of the V2 stats API: a per-call tracer
// that vends a per-attempt tracer. See handler.go for the shared types.

// ClientCallTracer traces one client call, across all of its attempts.
//
// Lifecycle: gRPC creates one ClientCallTracer per call, at the very start of
// the call (before name resolution is awaited), so a call that blocks waiting
// for the first resolver update is observable. StartAttempt is then invoked
// once per attempt. RecordEnd is invoked exactly once, after RecordEnd has
// been invoked on every attempt tracer this call produced, and is the final
// call on the object; gRPC makes no further use of it afterwards.
//
// A call may produce zero attempts - stream creation can fail before any
// attempt exists - and RecordEnd is still invoked in that case.
//
// Implementations must embed UnimplementedClientCallTracer, must be safe for
// concurrent use, and must not block: methods may be invoked while gRPC holds
// internal locks.
type ClientCallTracer interface {
	// StartAttempt returns a tracer for a new attempt of this call. It is
	// invoked before the load balancer pick, so a delayed pick is observable.
	// It must not return nil; return NopClientAttemptTracer{} to decline.
	//
	// Attempts may overlap: hedging produces concurrent attempts.
	StartAttempt(info *AttemptInfo) ClientAttemptTracer

	// RecordAnnotation records a one-shot, call-level annotation. gRPC uses it
	// for AnnotationNameResolutionComplete; the value set is open (see the
	// Annotation* constants), and an implementation should ignore annotations
	// it does not recognize.
	RecordAnnotation(annotation string)

	// RecordEnd is invoked exactly once, as the last call on this object.
	RecordEnd(info *CallEndInfo)

	// EnforceClientCallTracerEmbedding is included to force implementers to
	// embed UnimplementedClientCallTracer.
	internal.EnforceClientCallTracerEmbedding
}

// ClientAttemptTracer traces one attempt of one client call.
//
// Implementations must embed UnimplementedClientAttemptTracer, must be safe
// for concurrent use, and must not block. Note that events on a single attempt
// originate from more than one goroutine - the application's calling goroutine
// and the transport's reader - so an implementation may observe, for example,
// a message and a trailer concurrently.
type ClientAttemptTracer interface {
	// MutateOutgoingHeaders is invoked before this attempt's headers are
	// serialized, with the mutable outgoing header metadata. An implementation
	// may add entries (for example to inject a trace-context or
	// metadata-exchange header); additions go on the wire. It is the only hook
	// whose metadata is writable - the Record* header hooks are read-only. It
	// is attempt-scoped, so a retry re-injects for its own attempt.
	MutateOutgoingHeaders(md metadata.MD)

	// RecordOutgoingHeaders is invoked with the headers sent for this attempt,
	// for observation only; the metadata must be treated as read-only.
	RecordOutgoingHeaders(info *HeadersInfo)

	// RecordIncomingHeaders is invoked with the headers received from the
	// server. It does not fire for an RPC that fails before headers arrive,
	// including a trailers-only response.
	RecordIncomingHeaders(info *HeadersInfo)

	// RecordIncomingTrailers is invoked with the trailers received from the
	// server.
	RecordIncomingTrailers(info *TrailersInfo)

	// RecordOutgoingMessage is invoked once per message sent on this attempt,
	// after it has been written to the transport.
	RecordOutgoingMessage(info *MessageInfo)

	// RecordIncomingMessage is invoked once per message received on this
	// attempt, after it has been successfully parsed.
	RecordIncomingMessage(info *MessageInfo)

	// AddOptionalLabel attaches an optional telemetry label to the metrics this
	// attempt produces. It is invoked by gRPC during the load balancing pick,
	// so it may be called before any other method on this tracer, and more
	// than once. Use the Label* constants for the keys gRPC itself produces; a
	// plugin decides which labels it actually emits and ignores keys it does
	// not recognize.
	AddOptionalLabel(key, value string)

	// RecordAnnotation records a one-shot, attempt-level annotation. gRPC uses
	// it for AnnotationDelayedPickComplete; the value set is open (see the
	// Annotation* constants).
	RecordAnnotation(annotation string)

	// RecordEnd is invoked exactly once and is the last call on this object.
	RecordEnd(info *AttemptEndInfo)

	// EnforceClientAttemptTracerEmbedding is included to force implementers to
	// embed UnimplementedClientAttemptTracer.
	internal.EnforceClientAttemptTracerEmbedding
}

// Keys for the optional telemetry labels gRPC produces itself, passed to
// ClientAttemptTracer.AddOptionalLabel. They are spelled identically in gRPC
// Core and gRPC Java, and match the strings a plugin lists in a metric's
// OptionalLabels. The key is a plain string rather than a closed enumeration
// to match the other implementations' public vocabulary and grpc-go's own
// MetricDescriptor.OptionalLabels ([]string).
const (
	// LabelLocality is the xDS locality an attempt was routed to.
	LabelLocality = "grpc.lb.locality"
	// LabelBackendService is the xDS cluster an attempt was routed to.
	LabelBackendService = "grpc.lb.backend_service"
)

// ClientCallInfo describes a client call to Handler.ClientCallTracer.
type ClientCallInfo struct {
	// Method is the full RPC method string, i.e. /package.service/method.
	Method string
	// RegisteredMethod reports whether Method was known at compile time rather
	// than constructed dynamically. Plugins use this to decide whether Method
	// is safe to use as a metric label, since a dynamically built method name
	// can be unbounded in cardinality.
	RegisteredMethod bool
	// IsClientStream and IsServerStream describe the call's streaming shape.
	IsClientStream bool
	IsServerStream bool
	// CustomLabel is the application-supplied custom metric label for this
	// call, as defined by gRFC A108, or "" if none was set. It is lifted out
	// of the context and onto this struct because gRPC owns the key.
	CustomLabel string
	// Target is the canonical target of the channel, as returned by
	// ClientConn.CanonicalTarget.
	Target string
}

// AttemptInfo describes one attempt to ClientCallTracer.StartAttempt.
type AttemptInfo struct {
	// PreviousAttempts is the number of attempts of this call that preceded
	// this one and were not transparent retries. Transparent retries are
	// excluded because they are not attributable to the retry policy; use
	// IsTransparentRetry to distinguish them.
	PreviousAttempts int
	// IsTransparentRetry reports whether this attempt was initiated because a
	// previous attempt failed in a way that permits transparent retry.
	IsTransparentRetry bool
	// IsHedging reports whether this attempt was initiated by a hedging policy,
	// in which case it may run concurrently with other attempts.
	IsHedging bool
	// WaitForReady reports the effective wait-for-ready setting for this
	// attempt. It is reported per attempt rather than per call because it is
	// derived from the service config, which is not resolved until after the
	// call tracer has been created.
	WaitForReady bool
	// Authority is the authority used for this attempt. It is reported per
	// attempt because a load balancer may override it for an individual
	// attempt.
	Authority string
}

// AttemptEndInfo describes the completion of one client attempt.
type AttemptEndInfo struct {
	// Error is the error the attempt ended with, or nil on success.
	Error error
	// Peer describes the remote end this attempt used, or nil if the attempt
	// ended before a transport was chosen.
	Peer *peer.Peer
}

// UnimplementedClientCallTracer must be embedded to have forward compatible
// implementations.
type UnimplementedClientCallTracer struct {
	internal.EnforceClientCallTracerEmbedding
}

// StartAttempt provides a no-op implementation.
func (UnimplementedClientCallTracer) StartAttempt(*AttemptInfo) ClientAttemptTracer {
	return NopClientAttemptTracer{}
}

// RecordAnnotation provides a no-op implementation.
func (UnimplementedClientCallTracer) RecordAnnotation(string) {}

// RecordEnd provides a no-op implementation.
func (UnimplementedClientCallTracer) RecordEnd(*CallEndInfo) {}

// UnimplementedClientAttemptTracer must be embedded to have forward compatible
// implementations.
type UnimplementedClientAttemptTracer struct {
	internal.EnforceClientAttemptTracerEmbedding
}

// MutateOutgoingHeaders provides a no-op implementation.
func (UnimplementedClientAttemptTracer) MutateOutgoingHeaders(metadata.MD) {}

// RecordOutgoingHeaders provides a no-op implementation.
func (UnimplementedClientAttemptTracer) RecordOutgoingHeaders(*HeadersInfo) {}

// RecordIncomingHeaders provides a no-op implementation.
func (UnimplementedClientAttemptTracer) RecordIncomingHeaders(*HeadersInfo) {}

// RecordIncomingTrailers provides a no-op implementation.
func (UnimplementedClientAttemptTracer) RecordIncomingTrailers(*TrailersInfo) {}

// RecordOutgoingMessage provides a no-op implementation.
func (UnimplementedClientAttemptTracer) RecordOutgoingMessage(*MessageInfo) {}

// RecordIncomingMessage provides a no-op implementation.
func (UnimplementedClientAttemptTracer) RecordIncomingMessage(*MessageInfo) {}

// AddOptionalLabel provides a no-op implementation.
func (UnimplementedClientAttemptTracer) AddOptionalLabel(string, string) {}

// RecordAnnotation provides a no-op implementation.
func (UnimplementedClientAttemptTracer) RecordAnnotation(string) {}

// RecordEnd provides a no-op implementation.
func (UnimplementedClientAttemptTracer) RecordEnd(*AttemptEndInfo) {}

// NopClientCallTracer is the documented way for a Handler to decline tracing
// an individual client call.
type NopClientCallTracer struct{ UnimplementedClientCallTracer }

// NopClientAttemptTracer is the documented way for a ClientCallTracer to
// decline tracing an individual attempt.
type NopClientAttemptTracer struct {
	UnimplementedClientAttemptTracer
}
