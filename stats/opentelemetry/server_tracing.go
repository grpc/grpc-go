package opentelemetry

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/stats"
	otelinternaltracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
)

func (h *serverStatsHandler) initializeTracing() {
	if !h.options.isTracingEnabled() {
		return
	}

	otel.SetTextMapPropagator(h.options.TraceOptions.TextMapPropagator)
	otel.SetTracerProvider(h.options.TraceOptions.TracerProvider)
}

// traceTagRPC populates context with new span data using the TextMapPropagator
// supplied in trace options and internal itracing.Carrier. It creates a new
// incoming carrier which extracts an existing span context (if present) by
// deserializing from provided context. If valid span context is extracted, it
// is set as parent of the new span otherwise new span remains the root span.
// If TextMapPropagator is not provided in the trace options, it returns context
// as is.
func (h *serverStatsHandler) traceTagRPC(ctx context.Context, rti *stats.RPCTagInfo, ai *attemptInfo) (context.Context, *attemptInfo) {
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
	ai.traceSpan = span
	return ctx, ai
}
