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

// Package testingutils contains helpers for OpenTelemetry tests.
package testingutils

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
)

var (
	// DefaultLatencyBounds are the default bounds for latency metrics.
	DefaultLatencyBounds = []float64{0, 0.00001, 0.00005, 0.0001, 0.0003, 0.0006, 0.0008, 0.001, 0.002, 0.003, 0.004, 0.005, 0.006, 0.008, 0.01, 0.013, 0.016, 0.02, 0.025, 0.03, 0.04, 0.05, 0.065, 0.08, 0.1, 0.13, 0.16, 0.2, 0.25, 0.3, 0.4, 0.5, 0.65, 0.8, 1, 2, 5, 10, 20, 50, 100} // provide "advice" through API, SDK should set this too
	// DefaultSizeBounds are the default bounds for metrics which record size.
	DefaultSizeBounds = []float64{0, 1024, 2048, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864, 268435456, 1073741824, 4294967296}
)

// waitForServerCompletedRPCs waits until the unary and streaming stats.End
// calls are finished processing. It does this by waiting for the expected
// metric triggered by stats.End to appear through the passed in metrics reader.
func waitForServerCompletedRPCs(ctx context.Context, reader metric.Reader, wantMetric metricdata.Metrics, t *testing.T) (map[string]metricdata.Metrics, error) {
	for ; ctx.Err() == nil; <-time.After(time.Millisecond) {
		rm := &metricdata.ResourceMetrics{}
		reader.Collect(ctx, rm)
		gotMetrics := map[string]metricdata.Metrics{}
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				gotMetrics[m.Name] = m
			}
		}
		val, ok := gotMetrics[wantMetric.Name]
		if !ok {
			continue
		}
		if !metricdatatest.AssertEqual(t, wantMetric, val, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars()) {
			continue
		}
		return gotMetrics, nil
	}
	return nil, fmt.Errorf("error waiting for metric %v: %v", wantMetric, ctx.Err())
}

// assertDataPointWithinFiveSeconds asserts the metric passed in contains
// a histogram with dataPoints that fall within buckets that are <=5.
func assertDataPointWithinFiveSeconds(metric metricdata.Metrics) error {
	histo, ok := metric.Data.(metricdata.Histogram[float64])
	if !ok {
		return fmt.Errorf("metric data is not histogram")
	}
	for _, dataPoint := range histo.DataPoints {
		var boundWithFive int
		for i, bucket := range dataPoint.Bounds {
			if bucket >= 5 {
				boundWithFive = i
			}
		}
		foundPoint := false
		for i, bucket := range dataPoint.BucketCounts {
			if i >= boundWithFive {
				return fmt.Errorf("data point not found in bucket <=5 seconds")
			}
			if bucket == 1 {
				foundPoint = true
				break
			}
		}
		if !foundPoint {
			return fmt.Errorf("no data point found for metric")
		}
	}
	return nil
}

// MetricDataOptions are the options used to configure the metricData emissions
// of expected metrics data from NewMetricData. (rename function? this feels
// like the different config state spaces for xDS haha).
type MetricDataOptions struct {
	// CSMLabels are the csm labels to attach to metrics which receive csm
	// labels (all A66 expect client call and started RPC's client and server
	// side).
	CSMLabels []attribute.KeyValue
	// Target is the target of the client and server.
	Target string
	// UnaryMessageSent is whether a message was sent for the unary RPC or not.
	// This unary message is assumed to be 10000 bytes and the RPC is assumed to
	// have a gzip compressor call option set. This assumes both client and peer
	// sent a message.
	UnaryMessageSent bool
	// StreamingMessageSent is whether a message was sent for the streaming RPC
	// or not. This unary message is assumed to be 10000 bytes and the RPC is
	// assumed to have a gzip compressor call option set. This assumes both
	// client and peer sent a message.
	StreamingMessageSent bool
	// UnaryCallFailed is whether the Unary Call failed, which would trigger
	// trailers only.
	UnaryCallFailed bool
}

// MetricData returns a metricsDataSlice for A66 metrics for client and server
// with a unary RPC and streaming RPC with certain compression and message flow
// sent. If csmAttributes is set to true, the corresponding CSM Metrics (not
// client side call metrics, or started on client and server side).
func MetricData(options MetricDataOptions) []metricdata.Metrics {
	unaryMethodAttr := attribute.String("grpc.method", "grpc.testing.TestService/UnaryCall")
	duplexMethodAttr := attribute.String("grpc.method", "grpc.testing.TestService/FullDuplexCall")

	targetAttr := attribute.String("grpc.target", options.Target)

	unaryStatusAttr := attribute.String("grpc.status", "OK")
	streamingStatusAttr := attribute.String("grpc.status", "OK")
	if options.UnaryCallFailed {
		unaryStatusAttr = attribute.String("grpc.status", "UNKNOWN")
	}

	unaryMethodClientSideEnd := []attribute.KeyValue{
		unaryMethodAttr,
		targetAttr,
		unaryStatusAttr,
	}
	streamingMethodClientSideEnd := []attribute.KeyValue{
		duplexMethodAttr,
		targetAttr,
		streamingStatusAttr,
	}
	unaryMethodServerSideEnd := []attribute.KeyValue{
		unaryMethodAttr,
		unaryStatusAttr,
	}

	streamingMethodServerSideEnd := []attribute.KeyValue{
		duplexMethodAttr,
		streamingStatusAttr,
	}

	unaryMethodClientSideEnd = append(unaryMethodClientSideEnd, options.CSMLabels...)
	streamingMethodClientSideEnd = append(streamingMethodClientSideEnd, options.CSMLabels...)
	unaryMethodServerSideEnd = append(unaryMethodServerSideEnd, options.CSMLabels...)
	streamingMethodServerSideEnd = append(streamingMethodServerSideEnd, options.CSMLabels...)
	unaryCompressedBytesSentRecv := int64(0)
	unaryBucketCounts := []uint64{0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}
	unaryExtrema := metricdata.NewExtrema(int64(0))
	if options.UnaryMessageSent {
		unaryCompressedBytesSentRecv = 57 // Fixed 10000 bytes with gzip assumption.
		unaryBucketCounts = []uint64{0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}
		unaryExtrema = metricdata.NewExtrema(int64(57))
	}

	var streamingCompressedBytesSentRecv int64
	streamingBucketCounts := []uint64{0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}
	streamingExtrema := metricdata.NewExtrema(int64(0))
	if options.StreamingMessageSent {
		streamingCompressedBytesSentRecv = 57 // Fixed 10000 bytes with gzip assumption.
		streamingBucketCounts = []uint64{0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}
		streamingExtrema = metricdata.NewExtrema(int64(57))
	}

	return []metricdata.Metrics{
		{
			Name:        "grpc.client.attempt.started",
			Description: "Number of client call attempts started.",
			Unit:        "attempt",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(unaryMethodAttr, targetAttr),
						Value:      1,
					},
					{
						Attributes: attribute.NewSet(duplexMethodAttr, targetAttr),
						Value:      1,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
		{
			Name:        "grpc.client.attempt.duration",
			Description: "End-to-end time taken to complete a client call attempt.",
			Unit:        "s",
			Data: metricdata.Histogram[float64]{
				DataPoints: []metricdata.HistogramDataPoint[float64]{
					{
						Attributes: attribute.NewSet(unaryMethodClientSideEnd...),
						Count:      1,
						Bounds:     DefaultLatencyBounds,
					},
					{
						Attributes: attribute.NewSet(streamingMethodClientSideEnd...),
						Count:      1,
						Bounds:     DefaultLatencyBounds,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
		{
			Name:        "grpc.client.attempt.sent_total_compressed_message_size",
			Description: "Compressed message bytes sent per client call attempt.",
			Unit:        "By",
			Data: metricdata.Histogram[int64]{
				DataPoints: []metricdata.HistogramDataPoint[int64]{
					{
						Attributes:   attribute.NewSet(unaryMethodClientSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: unaryBucketCounts,
						Min:          unaryExtrema,
						Max:          unaryExtrema,
						Sum:          unaryCompressedBytesSentRecv,
					},
					{
						Attributes:   attribute.NewSet(streamingMethodClientSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: streamingBucketCounts,
						Min:          streamingExtrema,
						Max:          streamingExtrema,
						Sum:          streamingCompressedBytesSentRecv,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
		{
			Name:        "grpc.client.attempt.rcvd_total_compressed_message_size",
			Description: "Compressed message bytes received per call attempt.",
			Unit:        "By",
			Data: metricdata.Histogram[int64]{
				DataPoints: []metricdata.HistogramDataPoint[int64]{
					{
						Attributes:   attribute.NewSet(unaryMethodClientSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: unaryBucketCounts,
						Min:          unaryExtrema,
						Max:          unaryExtrema,
						Sum:          unaryCompressedBytesSentRecv,
					},
					{
						Attributes:   attribute.NewSet(streamingMethodClientSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: streamingBucketCounts,
						Min:          streamingExtrema,
						Max:          streamingExtrema,
						Sum:          streamingCompressedBytesSentRecv,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
		{
			Name:        "grpc.client.call.duration",
			Description: "Time taken by gRPC to complete an RPC from application's perspective.",
			Unit:        "s",
			Data: metricdata.Histogram[float64]{
				DataPoints: []metricdata.HistogramDataPoint[float64]{
					{
						Attributes: attribute.NewSet(unaryMethodAttr, targetAttr, unaryStatusAttr),
						Count:      1,
						Bounds:     DefaultLatencyBounds,
					},
					{
						Attributes: attribute.NewSet(duplexMethodAttr, targetAttr, streamingStatusAttr),
						Count:      1,
						Bounds:     DefaultLatencyBounds,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
		{
			Name:        "grpc.server.call.started",
			Description: "Number of server calls started.",
			Unit:        "call",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(unaryMethodAttr),
						Value:      1,
					},
					{
						Attributes: attribute.NewSet(duplexMethodAttr),
						Value:      1,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
		{
			Name:        "grpc.server.call.sent_total_compressed_message_size",
			Unit:        "By",
			Description: "Compressed message bytes sent per server call.",
			Data: metricdata.Histogram[int64]{
				DataPoints: []metricdata.HistogramDataPoint[int64]{
					{
						Attributes:   attribute.NewSet(unaryMethodServerSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: unaryBucketCounts,
						Min:          unaryExtrema,
						Max:          unaryExtrema,
						Sum:          unaryCompressedBytesSentRecv,
					},
					{
						Attributes:   attribute.NewSet(streamingMethodServerSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: streamingBucketCounts,
						Min:          streamingExtrema,
						Max:          streamingExtrema,
						Sum:          streamingCompressedBytesSentRecv,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
		{
			Name:        "grpc.server.call.rcvd_total_compressed_message_size",
			Unit:        "By",
			Description: "Compressed message bytes received per server call.",
			Data: metricdata.Histogram[int64]{
				DataPoints: []metricdata.HistogramDataPoint[int64]{
					{
						Attributes:   attribute.NewSet(unaryMethodServerSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: unaryBucketCounts,
						Min:          unaryExtrema,
						Max:          unaryExtrema,
						Sum:          unaryCompressedBytesSentRecv,
					},
					{
						Attributes:   attribute.NewSet(streamingMethodServerSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: streamingBucketCounts,
						Min:          streamingExtrema,
						Max:          streamingExtrema,
						Sum:          streamingCompressedBytesSentRecv,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
		{
			Name:        "grpc.server.call.duration",
			Description: "End-to-end time taken to complete a call from server transport's perspective.",
			Unit:        "s",
			Data: metricdata.Histogram[float64]{
				DataPoints: []metricdata.HistogramDataPoint[float64]{
					{
						Attributes: attribute.NewSet(unaryMethodServerSideEnd...),
						Count:      1,
						Bounds:     DefaultLatencyBounds,
					},
					{
						Attributes: attribute.NewSet(streamingMethodServerSideEnd...),
						Count:      1,
						Bounds:     DefaultLatencyBounds,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
	}
}

// CompareGotWantMetrics asserts wantMetrics are what we expect. It polls for
// eventual server metrics (not emitted synchronously with client side rpc
// returning), and for duration metrics makes sure the data point is within
// possible testing time (five seconds from context timeout).
func CompareGotWantMetrics(ctx context.Context, t *testing.T, mr *metric.ManualReader, gotMetrics map[string]metricdata.Metrics, wantMetrics []metricdata.Metrics) { // return an error instead of t...
	for _, metric := range wantMetrics {
		if metric.Name == "grpc.server.call.sent_total_compressed_message_size" || metric.Name == "grpc.server.call.rcvd_total_compressed_message_size" {
			// Sync the metric reader to see the event because stats.End is
			// handled async server side. Thus, poll until metrics created from
			// stats.End show up.
			var err error
			if gotMetrics, err = waitForServerCompletedRPCs(ctx, mr, metric, t); err != nil { // move to shared helper
				t.Fatalf("error waiting for sent total compressed message size for metric %v: %v", metric.Name, err)
			}
			continue
		}

		// If one of the duration metrics, ignore the bucket counts, and make
		// sure it count falls within a bucket <= 5 seconds (maximum duration of
		// test due to context).
		val, ok := gotMetrics[metric.Name]
		if !ok {
			t.Fatalf("metric %v not present in recorded metrics", metric.Name)
		}
		if metric.Name == "grpc.client.attempt.duration" || metric.Name == "grpc.client.call.duration" || metric.Name == "grpc.server.call.duration" {
			if !metricdatatest.AssertEqual(t, metric, val, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars(), metricdatatest.IgnoreValue()) {
				t.Fatalf("metrics data type not equal for metric: %v", metric.Name)
			}
			if err := assertDataPointWithinFiveSeconds(val); err != nil {
				t.Fatalf("Data point not within five seconds for metric %v: %v", metric.Name, err)
			}
			continue
		}

		if !metricdatatest.AssertEqual(t, metric, val, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars()) {
			t.Fatalf("metrics data type not equal for metric: %v", metric.Name)
		}
	}
}
