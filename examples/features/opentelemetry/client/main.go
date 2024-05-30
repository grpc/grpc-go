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

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/examples/features/proto/echo"
	"google.golang.org/grpc/stats/opentelemetry"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
)

var (
	addr     = flag.String("addr", ":50051", "the server address to connect to")
	promAddr = flag.String("promAddr", ":9465", "the Prometheus exporter endpoint")
)

func main() {
	exporter, err := prometheus.New()
	if err != nil {
		log.Fatalf("Failed to start prometheus exporter: %v", err)
	}
	provider := metric.NewMeterProvider(
		metric.WithReader(exporter),
	)
	// why 64 for interop 65 for example?
	go http.ListenAndServe(*promAddr, promhttp.Handler())

	/*ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()*/
	// play around to see if examples pass with this - unbounded
	ctx := context.Background()

	do := opentelemetry.DialOption(opentelemetry.Options{MetricsOptions: opentelemetry.MetricsOptions{MeterProvider: provider}})

	cc, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()), do)
	if err != nil {
		log.Fatalf("Failed to start NewClient: %v", err)
	}
	defer cc.Close()
	c := echo.NewEchoClient(cc)

	// Make a RPC every second. This should trigger telemetry to be emitted from
	// the client and the server.
	for {
		r, err := c.UnaryEcho(ctx, &echo.EchoRequest{Message: "this is examples/opentelemetry"}) // do I need to rename, any call options? echopb etc. Requires 1.21, but not unstable exporter not in g3
		if err != nil {
			log.Fatalf("UnaryEcho failed: %v", err)
		}
		fmt.Println(r) // Does this need to exit to pass examples test? I think I just run bash script to run examples test. Does example test need determinstic output from this? Does the read me need to be deterministic output?
		time.Sleep(time.Second)
	}
}
