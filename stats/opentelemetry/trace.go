/*
 * Copyright 2024 gRPC authors.
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

package opentelemetry

import (
	"context"
	"strings"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
	otelinternaltracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
	"google.golang.org/grpc/status"
)

// traceInfo is data used for recording traces.
type traceInfo struct {
	span                trace.Span
	countSentMsg        uint32
	countRecvMsg        uint32
	previousRpcAttempts uint32
	isTransparentRetry  bool
}

// traceTagRPC populates context with a new span, and serializes information
// about this span into gRPC Metadata.
func (csh *clientStatsHandler) traceTagRPC(ctx context.Context, rti *stats.RPCTagInfo) (context.Context, *traceInfo) {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return ctx, nil
	}
	// TODO: get consensus on whether this method name of "s.m" is correct.
	mn := "Attempt." + strings.Replace(removeLeadingSlash(rti.FullMethodName), "/", ".", -1)
	// Returned context is ignored because will populate context with data that
	// wraps the span instead. Don't set span kind client on this attempt span
	// to prevent backend from prepending span name with "Sent.".
	tracer := otel.Tracer("grpc-open-telemetry")
	_, span := tracer.Start(ctx, mn)
	if rti.NameResolutionDelay {
		span.AddEvent("Delayed name resolution complete")
	}

	carrier := otelinternaltracing.NewCustomCarrier(md) // Use internal custom carrier to inject
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	return metadata.NewOutgoingContext(ctx, carrier.Md), // Return a new context with the updated metadata
		&traceInfo{
			span:         span,
			countSentMsg: 0, // msg events scoped to scope of context, per attempt client side
			countRecvMsg: 0,
		}
}

// traceTagRPC populates context with new span data, with a parent based on the
// spanContext deserialized from context passed in (wire data in gRPC metadata)
// if present.
func (ssh *serverStatsHandler) traceTagRPC(ctx context.Context, rti *stats.RPCTagInfo) (context.Context, *traceInfo) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx, nil
	}

	mn := strings.Replace(removeLeadingSlash(rti.FullMethodName), "/", ".", -1)

	var span trace.Span
	// Returned context is ignored because will populate context with data
	// that wraps the span instead.
	tracer := otel.Tracer("grpc-open-telemetry")

	ctx = otel.GetTextMapPropagator().Extract(ctx, otelinternaltracing.NewCustomCarrier(md))

	// If the context.Context provided in `ctx` to tracer.Start(), contains a
	// Span then the newly-created Span will be a child of that span,
	// otherwise it will be a root span.
	_, span = tracer.Start(ctx, mn, trace.WithSpanKind(trace.SpanKindServer))

	return ctx, &traceInfo{
		span:         span,
		countSentMsg: 0,
		countRecvMsg: 0,
	}
}

// populateSpan populates span information based on stats passed in (invariants
// of the RPC lifecycle), and also ends span which triggers the span to be
// exported.
func populateSpan(ctx context.Context, rs stats.RPCStats, ti *traceInfo) {
	if ti == nil || ti.span == nil {
		// Shouldn't happen, tagRPC call comes before this function gets called
		// which populates this information.
		logger.Error("ctx passed into stats handler tracing event handling has no span present")
		return
	}
	span := ti.span

	switch rs := rs.(type) {
	case *stats.Begin:
		// Note: Go always added these attributes even though they are not
		// defined by the OpenCensus gRPC spec. Thus, they are unimportant for
		// correctness.
		span.SetAttributes(
			attribute.Bool("Client", rs.Client),
			attribute.Bool("FailFast", rs.Client),
			attribute.Int64("previous-rpc-attempts", int64(ti.previousRpcAttempts)),
			attribute.Bool("transparent-retry", ti.isTransparentRetry),
		)
		// increment previous rpc attempts applicable for next attempt
		atomic.AddUint32(&ti.previousRpcAttempts, 1)
	case *stats.PickerUpdated:
		span.AddEvent("Delayed LB pick complete")
	case *stats.InPayload:
		// message id - "must be calculated as two different counters starting
		// from one for sent messages and one for received messages."
		mi := atomic.AddUint32(&ti.countRecvMsg, 1)
		span.AddEvent("Inbound compressed message", trace.WithAttributes(
			attribute.Int64("sequence-number", int64(mi)),
			attribute.Int64("message-size", int64(rs.Length)),
			attribute.Int64("message-size", int64(rs.CompressedLength)),
			attribute.Int64("message-size-compressed", int64(rs.CompressedLength)),
		))
	case *stats.OutPayload:
		mi := atomic.AddUint32(&ti.countSentMsg, 1)
		span.AddEvent("Outbound compressed message", trace.WithAttributes(
			attribute.Int64("sequence-number", int64(mi)),
			attribute.Int64("message-size", int64(rs.Length)),
			attribute.Int64("message-size", int64(rs.CompressedLength)),
			attribute.Int64("message-size-compressed", int64(rs.CompressedLength)),
		))
	case *stats.End:
		if rs.Error != nil {
			// "The mapping between gRPC canonical codes and OpenCensus codes
			// can be found here", which implies 1:1 mapping to gRPC statuses
			// (OpenCensus statuses are based off gRPC statuses and a subset).
			s := status.Convert(rs.Error)
			span.SetStatus(otelcodes.Error, s.Message())
		} else {
			span.SetStatus(otelcodes.Ok, "Ok") // could get rid of this else conditional and just leave as 0 value, but this makes it explicit
		}
		span.End()
	}
}
