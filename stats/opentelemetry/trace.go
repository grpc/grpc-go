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

// attemptTraceSpan is data used for recording traces. It holds a reference to the
// current span, message counters for sent and received messages (used for
// generating message IDs), and the number of previous RPC attempts for the
// associated call.
type attemptTraceSpan struct {
	span                trace.Span
	countSentMsg        uint32
	countRecvMsg        uint32
	previousRpcAttempts uint32
}

// traceTagRPC populates context with a new span, and serializes information
// about this span into gRPC Metadata.
func (csh *clientStatsHandler) traceTagRPC(ctx context.Context, rti *stats.RPCTagInfo) (context.Context, *attemptTraceSpan) {
	if csh.options.TraceOptions.TextMapPropagator == nil {
		return ctx, nil
	}

	mn := "Attempt." + strings.Replace(removeLeadingSlash(rti.FullMethodName), "/", ".", -1)
	tracer := otel.Tracer("grpc-open-telemetry")
	ctx, span := tracer.Start(ctx, mn)

	carrier := otelinternaltracing.NewCustomCarrier(ctx) // Use internal custom carrier to inject
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	return carrier.Context(), &attemptTraceSpan{
		span:         span,
		countSentMsg: 0, // msg events scoped to scope of context, per attempt client side
		countRecvMsg: 0,
	}
}

// traceTagRPC populates context with new span data, with a parent based on the
// spanContext deserialized from context passed in (wire data in gRPC metadata)
// if present.
func (ssh *serverStatsHandler) traceTagRPC(ctx context.Context, rti *stats.RPCTagInfo) (context.Context, *attemptTraceSpan) {
	if ssh.options.TraceOptions.TextMapPropagator == nil {
		return ctx, nil
	}

	mn := strings.Replace(removeLeadingSlash(rti.FullMethodName), "/", ".", -1)
	var span trace.Span
	tracer := otel.Tracer("grpc-open-telemetry")
	ctx = otel.GetTextMapPropagator().Extract(ctx, otelinternaltracing.NewCustomCarrier(ctx))
	// If the context.Context provided in `ctx` to tracer.Start(), contains a
	// Span then the newly-created Span will be a child of that span,
	// otherwise it will be a root span.
	ctx, span = tracer.Start(ctx, mn, trace.WithSpanKind(trace.SpanKindServer))
	return ctx, &attemptTraceSpan{
		span:         span,
		countSentMsg: 0,
		countRecvMsg: 0,
	}
}

// populateSpan populates span information based on stats passed in, representing
// invariants of the RPC lifecycle. It ends the span, triggering its export.
// This function handles attempt spans on the client-side and call spans on the
// server-side.
func populateSpan(_ context.Context, rs stats.RPCStats, ti *attemptTraceSpan) {
	if ti == nil || ti.span == nil {
		// Shouldn't happen, tagRPC call comes before this function gets called
		// which populates this information.
		logger.Error("ctx passed into stats handler tracing event handling has no span present")
		return
	}
	span := ti.span

	switch rs := rs.(type) {
	case *stats.Begin:
		// Note: Go always added Client and FailFast attributes even though they are not
		// defined by the OpenCensus gRPC spec. Thus, they are unimportant for
		// correctness.
		span.SetAttributes(
			attribute.Bool("Client", rs.Client),
			attribute.Bool("FailFast", rs.Client),
			attribute.Int64("previous-rpc-attempts", int64(ti.previousRpcAttempts)),
			attribute.Bool("transparent-retry", rs.IsTransparentRetryAttempt),
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
			attribute.Int64("message-size-compressed", int64(rs.CompressedLength)),
		))
	case *stats.OutPayload:
		mi := atomic.AddUint32(&ti.countSentMsg, 1)
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
