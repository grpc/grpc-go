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
	"go.opentelemetry.io/otel/exporters/prometheus"
	otelstdouttrace "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	otelpropagation "go.opentelemetry.io/otel/propagation"
	otelmetric "go.opentelemetry.io/otel/sdk/metric"
	otelresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"google.golang.org/grpc"
	pb "google.golang.org/grpc/examples/features/proto/echo"
	oteltracing "google.golang.org/grpc/experimental/opentelemetry"
	"google.golang.org/grpc/stats/opentelemetry"
)

var (
	addr               = flag.String("addr", "localhost:50051", "the server address to connect to")
	prometheusEndpoint = flag.String("prometheus_endpoint", ":9464", "the Prometheus exporter endpoint for metrics")
)

type echoServer struct {
	pb.UnimplementedEchoServer
	addr string
}

func (s *echoServer) UnaryEcho(_ context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	return &pb.EchoResponse{Message: fmt.Sprintf("%s (from %s)", req.Message, s.addr)}, nil
}

func main() {
	exporter, err := prometheus.New()
	if err != nil {
		log.Fatalf("Failed to start prometheus exporter: %v", err)
	}
	// Configure meter provider for metrics
	meterProvider := otelmetric.NewMeterProvider(otelmetric.WithReader(exporter))
	// Configure exporter for traces
	traceExporter, err := otelstdouttrace.New(otelstdouttrace.WithPrettyPrint())
	if err != nil {
		log.Fatalf("Failed to create stdouttrace exporter: %v", err)
	}
	traceProvider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExporter), sdktrace.WithResource(otelresource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName("grpc-server"))))
	// Configure W3C Trace Context Propagator for traces
	textMapPropagator := otelpropagation.TraceContext{}
	so := opentelemetry.ServerOption(opentelemetry.Options{
		MetricsOptions: opentelemetry.MetricsOptions{
			MeterProvider: meterProvider,
			// These are example experimental gRPC metrics, which are disabled
			// by default and must be explicitly enabled. For the full,
			// up-to-date list of metrics, see:
			// https://grpc.io/docs/guides/opentelemetry-metrics/#instruments
			Metrics: opentelemetry.DefaultMetrics().Add(
				"grpc.lb.pick_first.connection_attempts_succeeded",
				"grpc.lb.pick_first.connection_attempts_failed",
			),
		},
		TraceOptions: oteltracing.TraceOptions{TracerProvider: traceProvider, TextMapPropagator: textMapPropagator}})

	go http.ListenAndServe(*prometheusEndpoint, promhttp.Handler())

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
