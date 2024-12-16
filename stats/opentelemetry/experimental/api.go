package experimental

import (
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TraceOptions are the tracing options for OpenTelemetry instrumentation.
type TraceOptions struct {
	// TracerProvider is the OpenTelemetry tracer which is required to
	// record traces/trace spans for instrumentation
	TracerProvider trace.TracerProvider

	// TextMapPropagator propagates span context through text map carrier.
	TextMapPropagator propagation.TextMapPropagator
}
