/*
 *
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
 *
 */

// Binary client is a client for the OpenTelemetry example.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	otlptraceexp "go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	otlptracehttpexp "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	otelpropagation "go.opentelemetry.io/otel/propagation"
	otelmetric "go.opentelemetry.io/otel/sdk/metric"
	otelresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/examples/features/proto/echo"
	oteltracing "google.golang.org/grpc/experimental/opentelemetry"
	"google.golang.org/grpc/stats/opentelemetry"
)

var (
	addr               = flag.String("addr", ":50051", "the server address to connect to")
	prometheusEndpoint = flag.String("prometheus_endpoint", ":9465", "the Prometheus exporter endpoint for metrics")
	otlpEndpoint       = flag.String("otlp_endpoint", ":4319", "the OTLP collector endpoint for traces")
	serviceName        = "grpc-client"
)

func main() {
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metricExporter, err := prometheus.New()
	if err != nil {
		log.Fatalf("Failed to start prometheus exporter: %v", err)
	}
	// Initialize MeterProvider with Prometheus exporter.
	provider := otelmetric.NewMeterProvider(otelmetric.WithReader(metricExporter))

	// Create OTLP exporter for traces.
	otlpClient := otlptracehttpexp.NewClient(otlptracehttpexp.WithEndpoint(*otlpEndpoint), otlptracehttpexp.WithInsecure())
	// Create a trace exporter instance.
	traceExporter, err := otlptraceexp.New(ctx, otlpClient)
	if err != nil {
		log.Fatalf("Failed to create otlp trace exporter: %v", err)
	}
	// resource.New adds service metadata to telemetry, enabling context and
	// filtering in the backend.
	res, err := otelresource.New(ctx, otelresource.WithTelemetrySDK(), otelresource.WithAttributes(semconv.ServiceName(serviceName)))
	if err != nil {
		log.Fatalf("Could not set resources: %v", err)
	}

	// Create a simple span processor.
	spanProcessor := sdktrace.NewBatchSpanProcessor(traceExporter)
	// Create a TracerProvider instance.
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanProcessor), sdktrace.WithResource(res))
	textMapPropagator := otelpropagation.TraceContext{} // Using W3C Trace Context Propagator for interoperability.

	// Configure TraceOptions for gRPC OpenTelemetry integration.
	traceOptions := oteltracing.TraceOptions{TracerProvider: tp, TextMapPropagator: textMapPropagator}
	go http.ListenAndServe(*prometheusEndpoint, promhttp.Handler())

	do := opentelemetry.DialOption(opentelemetry.Options{MetricsOptions: opentelemetry.MetricsOptions{MeterProvider: provider}, TraceOptions: traceOptions})

	cc, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()), do)
	if err != nil {
		log.Fatalf("Failed to create a client: %v", err)
	}
	defer cc.Close()
	c := echo.NewEchoClient(cc)

	// Make an RPC every second. This should trigger traces in the otlptracer
	// exporter to be emitted from the client.
	for {
		r, err := c.UnaryEcho(ctx, &echo.EchoRequest{Message: "this is examples/opentelemetry"})
		if err != nil {
			log.Fatalf("UnaryEcho failed: %v", err)
		}
		fmt.Println(r)
		time.Sleep(time.Second)
	}
}
