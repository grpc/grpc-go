/*
 *
 * Copyright 2014 gRPC authors.
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

// Binary server is an interop server.
//
// See interop test case descriptions [here].
//
// [here]: https://github.com/grpc/grpc/blob/master/doc/interop-test-descriptions.md
package main

import (
	"context"
	"flag"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/alts"
	oteltracing "google.golang.org/grpc/experimental/opentelemetry"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/interop"
	"google.golang.org/grpc/orca"
	grpcotel "google.golang.org/grpc/stats/opentelemetry"
	"google.golang.org/grpc/testdata"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

var (
	useTLS               = flag.Bool("use_tls", false, "Connection uses TLS if true, else plain TCP")
	useALTS              = flag.Bool("use_alts", false, "Connection uses ALTS if true (this option can only be used on GCP)")
	altsHSAddr           = flag.String("alts_handshaker_service_address", "", "ALTS handshaker gRPC service address")
	certFile             = flag.String("tls_cert_file", "", "The TLS cert file")
	keyFile              = flag.String("tls_key_file", "", "The TLS key file")
	port                 = flag.Int("port", 10000, "The server port")
	enableOpenTelemetry  = flag.Bool("enable_opentelemetry", false, "Whether to enable OpenTelemetry")
	otelCollectorAddress = flag.String("otel_collector_address", "", "The OpenTelemetry collector address")

	logger = grpclog.Component("interop")
)

func main() {
	flag.Parse()
	if *useTLS && *useALTS {
		logger.Fatal("-use_tls and -use_alts cannot be both set to true")
	}
	p := strconv.Itoa(*port)
	lis, err := net.Listen("tcp", ":"+p)
	if err != nil {
		logger.Fatalf("failed to listen: %v", err)
	}
	logger.Infof("interop server listening on %v", lis.Addr())
	opts := []grpc.ServerOption{orca.CallMetricsServerOption(nil)}
	if *enableOpenTelemetry || *otelCollectorAddress != "" {
		ctx := context.Background()
		var exporterOpts []otlptracegrpc.Option
		if *otelCollectorAddress != "" {
			addr := *otelCollectorAddress
			addr = strings.TrimPrefix(addr, "http://")
			addr = strings.TrimPrefix(addr, "https://")
			exporterOpts = append(exporterOpts, otlptracegrpc.WithEndpoint(addr))
		}
		exporterOpts = append(exporterOpts, otlptracegrpc.WithInsecure())
		exp, err := otlptracegrpc.New(ctx, exporterOpts...)
		if err != nil {
			logger.Fatalf("Failed to create OTLP trace exporter: %v", err)
		}
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exp),
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
		)
		propagator := propagation.TraceContext{}
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagator)
		defer func() {
			if err := tp.Shutdown(context.Background()); err != nil {
				logger.Errorf("Failed to shutdown TracerProvider: %v", err)
			}
		}()
		opts = append(opts, grpcotel.ServerOption(grpcotel.Options{
			TraceOptions: oteltracing.TraceOptions{
				TracerProvider:    tp,
				TextMapPropagator: propagator,
			},
		}))
	}
	if *useTLS {
		if *certFile == "" {
			*certFile = testdata.Path("server1.pem")
		}
		if *keyFile == "" {
			*keyFile = testdata.Path("server1.key")
		}
		creds, err := credentials.NewServerTLSFromFile(*certFile, *keyFile)
		if err != nil {
			logger.Fatalf("Failed to generate credentials: %v", err)
		}
		opts = append(opts, grpc.Creds(creds))
	} else if *useALTS {
		altsOpts := alts.DefaultServerOptions()
		if *altsHSAddr != "" {
			altsOpts.HandshakerServiceAddress = *altsHSAddr
		}
		altsTC := alts.NewServerCreds(altsOpts)
		opts = append(opts, grpc.Creds(altsTC))
	}
	server := grpc.NewServer(opts...)
	metricsRecorder := orca.NewServerMetricsRecorder()
	sopts := orca.ServiceOptions{
		MinReportingInterval:  time.Second,
		ServerMetricsProvider: metricsRecorder,
	}
	internal.ORCAAllowAnyMinReportingInterval.(func(*orca.ServiceOptions))(&sopts)
	orca.Register(server, sopts)
	testgrpc.RegisterTestServiceServer(server, interop.NewTestServer(interop.NewTestServerOptions{MetricsRecorder: metricsRecorder}))
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		server.GracefulStop()
	}()
	server.Serve(lis)
}
