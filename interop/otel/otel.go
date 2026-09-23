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

// Package otel contains the OpenTelemetry tracing setup shared by the
// OpenTelemetry-enabled interop client and server binaries in this module.
//
// This code lives in its own Go module (google.golang.org/grpc/interop/otel)
// rather than in the root grpc-go module because the OTLP exporter pulls
// go.opentelemetry.io/proto/otlp, grpc-ecosystem/grpc-gateway and friends into
// the module graph of every consumer of google.golang.org/grpc if imported
// from the root module.
package otel

import (
	"context"
	"crypto/tls"
	"strings"
	"time"

	otelapi "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
)

// batchTimeout is the maximum delay between a span ending and it being
// exported. The default (5s) is longer than the interop test harness waits
// for spans to show up in the collector, so use a shorter delay. A batching
// processor (as opposed to a synchronous one) is used so that exporting never
// happens on the RPC completion path, which matters for the soak tests.
const batchTimeout = 100 * time.Millisecond

// Setup configures OpenTelemetry tracing for an interop binary.
//
// If enabled is false and collectorAddress is empty, tracing is not configured
// and a nil TracerProvider is returned.
//
// collectorAddress is the OTLP/gRPC endpoint of the trace collector. It may be
// given as "host:port" (plaintext), "http://host:port" (plaintext) or
// "https://host:port" (TLS using the system roots). If empty, the standard
// OTEL_EXPORTER_OTLP_ENDPOINT / OTEL_EXPORTER_OTLP_TRACES_ENDPOINT environment
// variables are honoured by the exporter, including their scheme.
//
// The returned shutdown function flushes all pending spans and must be called
// before the process exits.
func Setup(enabled bool, collectorAddress string, logger grpclog.DepthLoggerV2) (*sdktrace.TracerProvider, propagation.TextMapPropagator, func()) {
	if !enabled && collectorAddress == "" {
		return nil, nil, func() {}
	}

	var exporterOpts []otlptracegrpc.Option
	if collectorAddress != "" {
		switch {
		case strings.HasPrefix(collectorAddress, "https://"):
			exporterOpts = append(exporterOpts,
				otlptracegrpc.WithEndpoint(strings.TrimPrefix(collectorAddress, "https://")),
				otlptracegrpc.WithTLSCredentials(credentials.NewTLS(&tls.Config{})),
			)
		case strings.HasPrefix(collectorAddress, "http://"):
			exporterOpts = append(exporterOpts,
				otlptracegrpc.WithEndpoint(strings.TrimPrefix(collectorAddress, "http://")),
				otlptracegrpc.WithInsecure(),
			)
		default:
			// A bare host:port is treated as plaintext, matching the other
			// languages' interop binaries and the interop test collector.
			exporterOpts = append(exporterOpts,
				otlptracegrpc.WithEndpoint(collectorAddress),
				otlptracegrpc.WithInsecure(),
			)
		}
	}

	exp, err := otlptracegrpc.New(context.Background(), exporterOpts...)
	if err != nil {
		logger.Fatalf("Failed to create OTLP trace exporter: %v", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(batchTimeout)),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// Surface exporter errors (e.g. an unreachable collector) in the interop
	// logs instead of the OTel SDK's default stderr handler.
	otelapi.SetErrorHandler(otelapi.ErrorHandlerFunc(func(err error) {
		logger.Errorf("OpenTelemetry error: %v", err)
	}))

	logger.Infof("OpenTelemetry tracing enabled, exporting to %q", collectorAddress)

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			logger.Errorf("Failed to shutdown TracerProvider: %v", err)
		}
	}
	return tp, propagation.TraceContext{}, shutdown
}
