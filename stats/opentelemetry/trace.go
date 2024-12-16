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
	"google.golang.org/grpc/stats"
	otelinternaltracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
	"google.golang.org/grpc/status"
)

// traceTagRPC populates provided context with a new span using the
// TextMapPropagator supplied in trace options and internal itracing.carrier.
// It creates a new outgoing carrier which serializes information about this
// span into gRPC Metadata, if TextMapPropagator is provided in the trace
// options. if TextMapPropagator is not provided, it returns the context as is.
func (h *clientStatsHandler) traceTagRPC(ctx context.Context, rti *stats.RPCTagInfo) (context.Context, *attemptInfo) {
	if h.options.TraceOptions.TextMapPropagator == nil {
		return ctx, nil
	}

	mn := "Attempt." + strings.Replace(removeLeadingSlash(rti.FullMethodName), "/", ".", -1)
	tracer := otel.Tracer("grpc-open-telemetry")
	ctx, span := tracer.Start(ctx, mn)
	carrier := otelinternaltracing.NewOutgoingCarrier(ctx)
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	return carrier.Context(), &attemptInfo{
		traceSpan:    span,
		countSentMsg: 0, // msg events scoped to scope of context, per attempt client side
		countRecvMsg: 0,
	}
}

// traceTagRPC populates context with new span data using the TextMapPropagator
// supplied in trace options and internal itracing.Carrier. It creates a new
// incoming carrier which extracts an existing span context (if present) by
// deserializing from provided context. If valid span context is extracted, it
// is set as parent of the new span otherwise new span remains the root span.
// If TextMapPropagator is not provided in the trace options, it returns context
// as is.
func (h *serverStatsHandler) traceTagRPC(ctx context.Context, rti *stats.RPCTagInfo) (context.Context, *attemptInfo) {
	if h.options.TraceOptions.TextMapPropagator == nil {
		return ctx, nil
	}

	mn := strings.Replace(removeLeadingSlash(rti.FullMethodName), "/", ".", -1)
	var span trace.Span
	tracer := otel.Tracer("grpc-open-telemetry")
	ctx = otel.GetTextMapPropagator().Extract(ctx, otelinternaltracing.NewIncomingCarrier(ctx))
	// If the context.Context provided in `ctx` to tracer.Start(), contains a
	// span then the newly-created Span will be a child of that span,
	// otherwise it will be a root span.
	ctx, span = tracer.Start(ctx, mn, trace.WithSpanKind(trace.SpanKindServer))
	return ctx, &attemptInfo{
		traceSpan:    span,
		countSentMsg: 0,
		countRecvMsg: 0,
	}
}

// statsHandler holds common functionality for both client and server stats
// handler.
type statsHandler struct{}

// populateSpan populates span information based on stats passed in, representing
// invariants of the RPC lifecycle. It ends the span, triggering its export.
// This function handles attempt spans on the client-side and call spans on the
// server-side.
func (h *statsHandler) populateSpan(_ context.Context, rs stats.RPCStats, ai *attemptInfo) {
	if ai == nil || ai.traceSpan == nil {
		// Shouldn't happen, tagRPC call comes before this function gets called
		// which populates this information.
		logger.Error("ctx passed into stats handler tracing event handling has no traceSpan present")
		return
	}
	span := ai.traceSpan

	switch rs := rs.(type) {
	case *stats.Begin:
		// Note: Go always added Client and FailFast attributes even though they are not
		// defined by the OpenCensus gRPC spec. Thus, they are unimportant for
		// correctness.
		span.SetAttributes(
			attribute.Bool("Client", rs.Client),
			attribute.Bool("FailFast", rs.Client),
			attribute.Int64("previous-rpc-attempts", int64(ai.previousRPCAttempts)),
			attribute.Bool("transparent-retry", rs.IsTransparentRetryAttempt),
		)
		// increment previous rpc attempts applicable for next attempt
		atomic.AddUint32(&ai.previousRPCAttempts, 1)
	case *stats.PickerUpdated:
		span.AddEvent("Delayed LB pick complete")
	case *stats.InPayload:
		// message id - "must be calculated as two different counters starting
		// from one for sent messages and one for received messages."
		mi := atomic.AddUint32(&ai.countRecvMsg, 1)
		span.AddEvent("Inbound compressed message", trace.WithAttributes(
			attribute.Int64("sequence-number", int64(mi)),
			attribute.Int64("message-size", int64(rs.Length)),
			attribute.Int64("message-size-compressed", int64(rs.CompressedLength)),
		))
	case *stats.OutPayload:
		mi := atomic.AddUint32(&ai.countSentMsg, 1)
		span.AddEvent("Outbound compressed message", trace.WithAttributes(
			attribute.Int64("sequence-number", int64(mi)),
			attribute.Int64("message-size", int64(rs.Length)),
			attribute.Int64("message-size-compressed", int64(rs.CompressedLength)),
		))
	case *stats.End:
		if rs.Error != nil {
			s := status.Convert(rs.Error)
			span.SetStatus(otelcodes.Error, s.Message())
		} else {
			span.SetStatus(otelcodes.Ok, "Ok")
		}
		span.End()
	}
}
