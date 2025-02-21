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

// Binary server is a server for the OpenTelemetry example.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/features/proto/echo"
	oteltracing "google.golang.org/grpc/experimental/opentelemetry"
	"google.golang.org/grpc/stats/opentelemetry"
)

var (
	addr               = flag.String("addr", ":50051", "the server address to connect to")
	prometheusEndpoint = flag.String("prometheus_endpoint", ":9464", "the Prometheus exporter endpoint")
	otlpEndpoint       = flag.String("otlp_endpoint", "localhost:4318", "the OTLP collector endpoint")
	serviceName        = "grpc-server"
)

type echoServer struct {
	pb.UnimplementedEchoServer
	addr string
}

func (s *echoServer) UnaryEcho(_ context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	return &pb.EchoResponse{Message: fmt.Sprintf("%s (from %s)", req.Message, s.addr)}, nil
}

func main() {
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	exporter, err := prometheus.New()
	if err != nil {
		log.Fatalf("Failed to start prometheus exporter: %v", err)
	}
	provider := metric.NewMeterProvider(metric.WithReader(exporter))

	client := otlptracehttp.NewClient(
		otlptracehttp.WithEndpoint(*otlpEndpoint),
		otlptracehttp.WithInsecure(),
	)
	traceExporter, err := otlptrace.New(context.Background(), client)
	if err != nil {
		log.Fatalf("Failed to create otlp trace exporter: %v", err)
	}

	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		log.Fatalf("Could not set resources: %v", err)
	}
	spanProcessor := trace.NewSimpleSpanProcessor(traceExporter)
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(spanProcessor), trace.WithResource(res))
	otel.SetTracerProvider(tp)

	textMapPropagator := propagation.TraceContext{}

	traceOptions := oteltracing.TraceOptions{
		TracerProvider:    tp,
		TextMapPropagator: textMapPropagator,
	}

	go http.ListenAndServe(*prometheusEndpoint, promhttp.Handler())

	so := opentelemetry.ServerOption(opentelemetry.Options{MetricsOptions: opentelemetry.MetricsOptions{MeterProvider: provider}, TraceOptions: traceOptions})

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	s := grpc.NewServer(so)
	pb.RegisterEchoServer(s, &echoServer{addr: *addr})

	log.Printf("Serving on %s\n", *addr)

	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
