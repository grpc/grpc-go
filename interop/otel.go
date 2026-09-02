package interop

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/grpclog"
)

// SetupOpenTelemetry configures OpenTelemetry tracing for interop tests.
func SetupOpenTelemetry(enableOpenTelemetry bool, otelCollectorAddress string, logger grpclog.DepthLoggerV2) (*sdktrace.TracerProvider, propagation.TextMapPropagator, func()) {
	if !enableOpenTelemetry && otelCollectorAddress == "" {
		return nil, propagation.TraceContext{}, func() {}
	}

	ctx := context.Background()
	var exporterOpts []otlptracegrpc.Option
	if otelCollectorAddress != "" {
		addr := otelCollectorAddress
		if strings.HasPrefix(addr, "https://") {
			addr = strings.TrimPrefix(addr, "https://")
		} else {
			addr = strings.TrimPrefix(addr, "http://")
			exporterOpts = append(exporterOpts, otlptracegrpc.WithInsecure())
		}
		exporterOpts = append(exporterOpts, otlptracegrpc.WithEndpoint(addr))
	} else {
		exporterOpts = append(exporterOpts, otlptracegrpc.WithInsecure())
	}

	exp, err := otlptracegrpc.New(ctx, exporterOpts...)
	if err != nil {
		logger.Fatalf("Failed to create OTLP trace exporter: %v", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	propagator := propagation.TraceContext{}

	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Errorf("OpenTelemetry error: %v", err)
	}))

	shutdownFunc := func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			logger.Errorf("Failed to shutdown TracerProvider: %v", err)
		}
	}
	return tp, propagator, shutdownFunc
}
