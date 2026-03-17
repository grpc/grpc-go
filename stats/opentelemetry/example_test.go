/*
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
 */

package opentelemetry_test

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/stats/opentelemetry"

	"go.opentelemetry.io/otel/sdk/metric"
)

func ExampleDialOption_basic() {
	// This is setting default bounds for a view. Setting these bounds through
	// meter provider from SDK is recommended, as API calls in this module
	// provide default bounds, but these calls are not guaranteed to be stable
	// and API implementors are not required to implement bounds. Setting bounds
	// through SDK ensures the bounds get picked up. The specific fields in
	// Aggregation take precedence over defaults from API. For any fields unset
	// in aggregation, defaults get picked up, so can have a mix of fields from
	// SDK and fields created from API call. The overridden views themselves
	// also follow same logic, only the specific views being created in the SDK
	// use SDK information, the rest are created from API call.
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(
		metric.WithReader(reader),
		metric.WithView(metric.NewView(metric.Instrument{
			Name: "grpc.client.call.duration",
		},
			metric.Stream{
				Aggregation: metric.AggregationExplicitBucketHistogram{
					Boundaries: opentelemetry.DefaultSizeBounds, // The specific fields set in SDK take precedence over API.
				},
			},
		)),
	)

	opts := opentelemetry.Options{
		MetricsOptions: opentelemetry.MetricsOptions{
			MeterProvider: provider,
			Metrics:       opentelemetry.DefaultMetrics(), // equivalent to unset - distinct from empty
		},
	}
	do := opentelemetry.DialOption(opts)
	cc, err := grpc.NewClient("<target string>", do, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		// Handle err.
	}
	defer cc.Close()
}

func ExampleServerOption_methodFilter() {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	opts := opentelemetry.Options{
		MetricsOptions: opentelemetry.MetricsOptions{
			MeterProvider: provider,
			// Because Metrics is unset, the user will get default metrics.
			MethodAttributeFilter: func(str string) bool {
				// Will allow duplex/any other type of RPC.
				return str != "/grpc.testing.TestService/UnaryCall"
			},
		},
	}
	cc, err := grpc.NewClient("some-target", opentelemetry.DialOption(opts), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		// Handle err.
	}
	defer cc.Close()
}

func ExampleMetrics_excludeSome() {
	// To exclude specific metrics, initialize Options as follows:
	opts := opentelemetry.Options{
		MetricsOptions: opentelemetry.MetricsOptions{
			Metrics: opentelemetry.DefaultMetrics().Remove(opentelemetry.ClientAttemptDurationMetricName, opentelemetry.ClientAttemptRcvdCompressedTotalMessageSizeMetricName),
		},
	}
	do := opentelemetry.DialOption(opts)
	cc, err := grpc.NewClient("<target string>", do, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		// Handle err.
	}
	defer cc.Close()
}

func ExampleMetrics_disableAll() {
	// To disable all metrics, initialize Options as follows:
	opts := opentelemetry.Options{
		MetricsOptions: opentelemetry.MetricsOptions{
			Metrics: stats.NewMetricSet(), // Distinct to nil, which creates default metrics. This empty set creates no metrics.
		},
	}
	do := opentelemetry.DialOption(opts)
	cc, err := grpc.NewClient("<target string>", do, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		// Handle err.
	}
	defer cc.Close()
}

func ExampleMetrics_enableSome() {
	// To only create specific metrics, initialize Options as follows:
	opts := opentelemetry.Options{
		MetricsOptions: opentelemetry.MetricsOptions{
			Metrics: stats.NewMetricSet(opentelemetry.ClientAttemptDurationMetricName, opentelemetry.ClientAttemptRcvdCompressedTotalMessageSizeMetricName), // only create these metrics
		},
	}
	do := opentelemetry.DialOption(opts)
	cc, err := grpc.NewClient("<target string>", do, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil { // might fail vet
		// Handle err.
	}
	defer cc.Close()
}

func ExampleOptions_addExperimentalMetrics() {
	opts := opentelemetry.Options{
		MetricsOptions: opentelemetry.MetricsOptions{
			// These are example experimental gRPC metrics, which are disabled
			// by default and must be explicitly enabled. For the full,
			// up-to-date list of metrics, see:
			// https://grpc.io/docs/guides/opentelemetry-metrics/#instruments
			Metrics: opentelemetry.DefaultMetrics().Add(
				"grpc.lb.pick_first.connection_attempts_succeeded",
				"grpc.lb.pick_first.connection_attempts_failed",
			),
		},
	}
	do := opentelemetry.DialOption(opts)
	cc, err := grpc.NewClient("<target string>", do, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		// Handle error.
	}
	defer cc.Close()
}
