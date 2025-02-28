/*
 *
 * Copyright 2022 gRPC authors.
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

// Binary client is an interop client for Observability.
//
// See interop test case descriptions [here].
//
// [here]: https://github.com/grpc/grpc/blob/master/doc/interop-test-descriptions.md
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/gcp/observability"
	"google.golang.org/grpc/interop"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

var (
	serverHost = flag.String("server_host", "localhost", "The server host name")
	serverPort = flag.Int("server_port", 10000, "The server port number")
	testCase   = flag.String("test_case", "large_unary", "The action to perform")
	numTimes   = flag.Int("num_times", 1, "Number of times to run the test case")
)

func main() {
	err := observability.Start(context.Background())
	if err != nil {
		log.Fatalf("observability start failed: %v", err)
	}
	defer observability.End()
	flag.Parse()
	serverAddr := *serverHost
	if *serverPort != 0 {
		serverAddr = net.JoinHostPort(*serverHost, strconv.Itoa(*serverPort))
	}
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("grpc.NewClient(%q) = %v", serverAddr, err)
	}
	defer conn.Close()
	tc := testgrpc.NewTestServiceClient(conn)
	ctx := context.Background()
	for i := 0; i < *numTimes; i++ {
		if *testCase == "ping_pong" {
			interop.DoPingPong(ctx, tc)
		} else if *testCase == "large_unary" {
			interop.DoLargeUnaryCall(ctx, tc)
		} else if *testCase == "custom_metadata" {
			interop.DoCustomMetadata(ctx, tc)
		} else {
			log.Fatalf("Invalid test case: %s", *testCase)
		}
	}
	// TODO(stanleycheung): remove this once the observability exporter plugin is able to
	//                      gracefully flush observability data to cloud at shutdown
	// TODO(stanleycheung): see if we can reduce the number 65
	const exporterSleepDuration = 65 * time.Second
	log.Printf("Sleeping %v before closing...", exporterSleepDuration)
	time.Sleep(exporterSleepDuration)
}
