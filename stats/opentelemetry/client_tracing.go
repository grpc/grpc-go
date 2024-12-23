package opentelemetry

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"google.golang.org/grpc/stats"
	otelinternaltracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
)

func (h *clientStatsHandler) initializeTracing() {
	if !h.options.isTracingEnabled() {
		return
	}

	otel.SetTextMapPropagator(h.options.TraceOptions.TextMapPropagator)
	otel.SetTracerProvider(h.options.TraceOptions.TracerProvider)
}

// traceTagRPC populates provided context with a new span using the
// TextMapPropagator supplied in trace options and internal itracing.carrier.
// It creates a new outgoing carrier which serializes information about this
// span into gRPC Metadata, if TextMapPropagator is provided in the trace
// options. if TextMapPropagator is not provided, it returns the context as is.
func (h *clientStatsHandler) traceTagRPC(ctx context.Context, rti *stats.RPCTagInfo, ai *attemptInfo) (context.Context, *attemptInfo) {
	if h.options.TraceOptions.TextMapPropagator == nil {
		return ctx, nil
	}

	mn := "Attempt." + strings.Replace(removeLeadingSlash(rti.FullMethodName), "/", ".", -1)
	tracer := otel.Tracer("grpc-open-telemetry")
	ctx, span := tracer.Start(ctx, mn)
	carrier := otelinternaltracing.NewOutgoingCarrier(ctx)
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	ai.traceSpan = span
	return carrier.Context(), ai
}
