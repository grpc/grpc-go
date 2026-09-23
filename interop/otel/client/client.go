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

// Binary client is an interop client with OpenTelemetry tracing support.
//
// It supports the subset of the interop client (interop/client in the root
// module) flags and test cases needed by the cross-language OpenTelemetry
// tracing interop tests, and additionally accepts -enable_opentelemetry and
// -otel_collector_address, which configure the gRPC OpenTelemetry plugin to
// export traces over OTLP/gRPC. It lives in its own module so that the OTLP
// exporter's dependencies do not leak into the root grpc-go module.
//
// See interop test case descriptions [here].
//
// [here]: https://github.com/grpc/grpc/blob/master/doc/interop-test-descriptions.md
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"net"
	"os"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/alts"
	"google.golang.org/grpc/credentials/insecure"
	oteltracing "google.golang.org/grpc/experimental/opentelemetry"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/interop"
	interopotel "google.golang.org/grpc/interop/otel"
	"google.golang.org/grpc/resolver"
	grpcotel "google.golang.org/grpc/stats/opentelemetry"
	"google.golang.org/grpc/testdata"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

var (
	caFile               = flag.String("ca_file", "", "The file containing the CA root cert file")
	useTLS               = flag.Bool("use_tls", false, "Connection uses TLS if true")
	useALTS              = flag.Bool("use_alts", false, "Connection uses ALTS if true (this option can only be used on GCP)")
	altsHSAddr           = flag.String("alts_handshaker_service_address", "", "ALTS handshaker gRPC service address")
	testCA               = flag.Bool("use_test_ca", false, "Whether to replace platform root CAs with test CA as the CA root")
	serverHost           = flag.String("server_host", "localhost", "The server host name")
	serverPort           = flag.Int("server_port", 10000, "The server port number")
	serviceConfigJSON    = flag.String("service_config_json", "", "Disables service config lookups and sets the provided string as the default service config.")
	tlsServerName        = flag.String("server_host_override", "", "The server name used to verify the hostname returned by TLS handshake if it is not empty. Otherwise, --server_host is used.")
	enableOpenTelemetry  = flag.Bool("enable_opentelemetry", false, "Whether to enable OpenTelemetry tracing")
	otelCollectorAddress = flag.String("otel_collector_address", "", "The OTLP/gRPC address of the OpenTelemetry trace collector, e.g. localhost:4317 or http://localhost:4317")
	testCase             = flag.String("test_case", "large_unary",
		`Configure different test cases. Valid options are:
        empty_unary : empty (zero bytes) request and response;
        large_unary : single request and (large) response;
        client_streaming : request streaming with single response;
        server_streaming : single request with response streaming;
        ping_pong : full-duplex streaming;
        empty_stream : full-duplex streaming with zero message;
        timeout_on_sleeping_server: fullduplex streaming on a sleeping server;
        cancel_after_begin: cancellation after metadata has been sent but before payloads are sent;
        cancel_after_first_response: cancellation after receiving 1st message from the server;
        status_code_and_message: status code propagated back to client;
        special_status_message: Unicode and whitespace is correctly processed in status message;
        custom_metadata: server will echo custom metadata;
        unimplemented_method: client attempts to call unimplemented method;
        unimplemented_service: client attempts to call unimplemented service;
        pick_first_unary: all requests are sent to one server despite multiple servers are resolved;
        orca_per_rpc: the client verifies ORCA per-RPC metrics are provided;
        orca_oob: the client verifies ORCA out-of-band metrics are provided.`)

	logger = grpclog.Component("interop")
)

func main() {
	flag.Parse()
	if *useTLS && *useALTS {
		logger.Fatal("-use_tls and -use_alts cannot be both set to true")
	}
	resolver.SetDefaultScheme("dns")
	serverAddr := *serverHost
	if *serverPort != 0 {
		serverAddr = net.JoinHostPort(*serverHost, strconv.Itoa(*serverPort))
	}

	var opts []grpc.DialOption
	switch {
	case *useTLS:
		var roots *x509.CertPool
		if *testCA {
			if *caFile == "" {
				*caFile = testdata.Path("ca.pem")
			}
			b, err := os.ReadFile(*caFile)
			if err != nil {
				logger.Fatalf("Failed to read root certificate file %q: %v", *caFile, err)
			}
			roots = x509.NewCertPool()
			if !roots.AppendCertsFromPEM(b) {
				logger.Fatalf("Failed to append certificates: %s", string(b))
			}
		}
		var creds credentials.TransportCredentials
		if *tlsServerName != "" {
			creds = credentials.NewClientTLSFromCert(roots, *tlsServerName)
		} else {
			creds = credentials.NewTLS(&tls.Config{RootCAs: roots})
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	case *useALTS:
		altsOpts := alts.DefaultClientOptions()
		if *altsHSAddr != "" {
			altsOpts.HandshakerServiceAddress = *altsHSAddr
		}
		opts = append(opts, grpc.WithTransportCredentials(alts.NewClientCreds(altsOpts)))
	default:
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	if len(*serviceConfigJSON) > 0 {
		opts = append(opts, grpc.WithDisableServiceConfig(), grpc.WithDefaultServiceConfig(*serviceConfigJSON))
	}

	tp, propagator, shutdownTracing := interopotel.Setup(*enableOpenTelemetry, *otelCollectorAddress, logger)
	if tp != nil {
		defer shutdownTracing()
		opts = append(opts, grpcotel.DialOption(grpcotel.Options{
			TraceOptions: oteltracing.TraceOptions{
				TracerProvider:    tp,
				TextMapPropagator: propagator,
			},
		}))
	}

	conn, err := grpc.NewClient(serverAddr, opts...)
	if err != nil {
		logger.Fatalf("grpc.NewClient(%q) = %v", serverAddr, err)
	}
	defer conn.Close()
	tc := testgrpc.NewTestServiceClient(conn)
	ctx := context.Background()
	switch *testCase {
	case "empty_unary":
		interop.DoEmptyUnaryCall(ctx, tc)
		logger.Infoln("EmptyUnaryCall done")
	case "large_unary":
		interop.DoLargeUnaryCall(ctx, tc)
		logger.Infoln("LargeUnaryCall done")
	case "client_streaming":
		interop.DoClientStreaming(ctx, tc)
		logger.Infoln("ClientStreaming done")
	case "server_streaming":
		interop.DoServerStreaming(ctx, tc)
		logger.Infoln("ServerStreaming done")
	case "ping_pong":
		interop.DoPingPong(ctx, tc)
		logger.Infoln("Pingpong done")
	case "empty_stream":
		interop.DoEmptyStream(ctx, tc)
		logger.Infoln("Emptystream done")
	case "timeout_on_sleeping_server":
		interop.DoTimeoutOnSleepingServer(ctx, tc)
		logger.Infoln("TimeoutOnSleepingServer done")
	case "cancel_after_begin":
		interop.DoCancelAfterBegin(ctx, tc)
		logger.Infoln("CancelAfterBegin done")
	case "cancel_after_first_response":
		interop.DoCancelAfterFirstResponse(ctx, tc)
		logger.Infoln("CancelAfterFirstResponse done")
	case "status_code_and_message":
		interop.DoStatusCodeAndMessage(ctx, tc)
		logger.Infoln("StatusCodeAndMessage done")
	case "special_status_message":
		interop.DoSpecialStatusMessage(ctx, tc)
		logger.Infoln("SpecialStatusMessage done")
	case "custom_metadata":
		interop.DoCustomMetadata(ctx, tc)
		logger.Infoln("CustomMetadata done")
	case "unimplemented_method":
		interop.DoUnimplementedMethod(ctx, conn)
		logger.Infoln("UnimplementedMethod done")
	case "unimplemented_service":
		interop.DoUnimplementedService(ctx, testgrpc.NewUnimplementedServiceClient(conn))
		logger.Infoln("UnimplementedService done")
	case "pick_first_unary":
		interop.DoPickFirstUnary(ctx, tc)
		logger.Infoln("PickFirstUnary done")
	case "orca_per_rpc":
		interop.DoORCAPerRPCTest(ctx, tc)
		logger.Infoln("ORCAPerRPC done")
	case "orca_oob":
		interop.DoORCAOOBTest(ctx, tc)
		logger.Infoln("ORCAOOB done")
	default:
		logger.Fatal("Unsupported test case: ", *testCase)
	}
}
