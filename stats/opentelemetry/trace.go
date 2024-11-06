package opentelemetry

import (
	"context"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/stats"
	otelinternaltracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
	"strings"
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
