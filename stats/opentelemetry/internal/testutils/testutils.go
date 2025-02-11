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

// Package testutils contains helpers for OpenTelemetry tests.
package testutils

import (
	"context"
	"fmt"
	"testing"
	"time"

	otelcodes "go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Redefine default bounds here to avoid a cyclic dependency with top level
// opentelemetry package. Could define once through internal, but would make
// external opentelemetry godoc less readable.
var (
	// DefaultLatencyBounds are the default bounds for latency metrics.
	DefaultLatencyBounds = []float64{0, 0.00001, 0.00005, 0.0001, 0.0003, 0.0006, 0.0008, 0.001, 0.002, 0.003, 0.004, 0.005, 0.006, 0.008, 0.01, 0.013, 0.016, 0.02, 0.025, 0.03, 0.04, 0.05, 0.065, 0.08, 0.1, 0.13, 0.16, 0.2, 0.25, 0.3, 0.4, 0.5, 0.65, 0.8, 1, 2, 5, 10, 20, 50, 100}
	// DefaultSizeBounds are the default bounds for metrics which record size.
	DefaultSizeBounds = []float64{0, 1024, 2048, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864, 268435456, 1073741824, 4294967296}
)

// TraceSpanInfo is the information received about the trace span. It contains
// subset of information that is needed to verify if correct trace is being
// attributed to the rpc.
type TraceSpanInfo struct {
	SpanKind   string
	Name       string
	Events     []trace.Event
	Attributes []attribute.KeyValue
}

// waitForServerCompletedRPCs waits until the unary and streaming stats.End
// calls are finished processing. It does this by waiting for the expected
// metric triggered by stats.End to appear through the passed in metrics reader.
//
// Returns a new gotMetrics map containing the metric data being polled for, or
// an error if failed to wait for metric.
func waitForServerCompletedRPCs(ctx context.Context, reader metric.Reader, wantMetric metricdata.Metrics) (map[string]metricdata.Metrics, error) {
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
		if _, ok := val.Data.(metricdata.Histogram[int64]); ok {
			if len(wantMetric.Data.(metricdata.Histogram[int64]).DataPoints) != len(val.Data.(metricdata.Histogram[int64]).DataPoints) {
				continue
			}
		}
		if _, ok := val.Data.(metricdata.Histogram[float64]); ok {
			if len(wantMetric.Data.(metricdata.Histogram[float64]).DataPoints) != len(val.Data.(metricdata.Histogram[float64]).DataPoints) {
				continue
			}
		}
		return gotMetrics, nil
	}
	return nil, fmt.Errorf("error waiting for metric %v: %v", wantMetric, ctx.Err())
}

// checkDataPointWithinFiveSeconds checks if the metric passed in contains a
// histogram with dataPoints that fall within buckets that are <=5. Returns an
// error if check fails.
func checkDataPointWithinFiveSeconds(metric metricdata.Metrics) error {
	histo, ok := metric.Data.(metricdata.Histogram[float64])
	if !ok {
		return fmt.Errorf("metric data is not histogram")
	}
	for _, dataPoint := range histo.DataPoints {
		var boundWithFive int
		for i, bound := range dataPoint.Bounds {
			if bound >= 5 {
				boundWithFive = i
			}
		}
		foundPoint := false
		for i, count := range dataPoint.BucketCounts {
			if i >= boundWithFive {
				return fmt.Errorf("data point not found in bucket <=5 seconds")
			}
			if count == 1 {
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
// of expected metrics data from NewMetricData.
type MetricDataOptions struct {
	// CSMLabels are the csm labels to attach to metrics which receive csm
	// labels (all A66 expect client call and started RPC's client and server
	// side).
	CSMLabels []attribute.KeyValue
	// Target is the target of the client and server.
	Target string
	// UnaryCallFailed is whether the Unary Call failed, which would trigger
	// trailers only.
	UnaryCallFailed bool
	// UnaryCompressedMessageSize is the compressed message size of the Unary
	// RPC. This assumes both client and server sent the same message size.
	UnaryCompressedMessageSize float64
	// StreamingCompressedMessageSize is the compressed message size of the
	// Streaming RPC. This assumes both client and server sent the same message
	// size.
	StreamingCompressedMessageSize float64
}

// createBucketCounts creates a list of bucket counts based off the
// recordingPoints and bounds. Both recordingPoints and bounds are assumed to be
// in order.
func createBucketCounts(recordingPoints []float64, bounds []float64) []uint64 {
	var bucketCounts []uint64
	var recordingPointIndex int
	for _, bound := range bounds {
		var bucketCount uint64
		if recordingPointIndex >= len(recordingPoints) {
			bucketCounts = append(bucketCounts, bucketCount)
			continue
		}
		for recordingPoints[recordingPointIndex] <= bound {
			bucketCount++
			recordingPointIndex++
			if recordingPointIndex >= len(recordingPoints) {
				break
			}
		}
		bucketCounts = append(bucketCounts, bucketCount)
	}
	// The rest of the recording points are last bound -> infinity.
	bucketCounts = append(bucketCounts, uint64(len(recordingPoints)-recordingPointIndex))
	return bucketCounts
}

// MetricDataUnary returns a list of expected metrics defined in A66 for a
// client and server for one unary RPC.
func MetricDataUnary(options MetricDataOptions) []metricdata.Metrics {
	methodAttr := attribute.String("grpc.method", "grpc.testing.TestService/UnaryCall")
	targetAttr := attribute.String("grpc.target", options.Target)
	statusAttr := attribute.String("grpc.status", "OK")
	if options.UnaryCallFailed {
		statusAttr = attribute.String("grpc.status", "UNKNOWN")
	}
	clientSideEnd := []attribute.KeyValue{
		methodAttr,
		targetAttr,
		statusAttr,
	}
	serverSideEnd := []attribute.KeyValue{
		methodAttr,
		statusAttr,
	}
	clientSideEnd = append(clientSideEnd, options.CSMLabels...)
	serverSideEnd = append(serverSideEnd, options.CSMLabels...)
	compressedBytesSentRecv := int64(options.UnaryCompressedMessageSize)
	bucketCounts := createBucketCounts([]float64{options.UnaryCompressedMessageSize}, DefaultSizeBounds)
	extrema := metricdata.NewExtrema(int64(options.UnaryCompressedMessageSize))
	return []metricdata.Metrics{
		{
			Name:        "grpc.client.attempt.started",
			Description: "Number of client call attempts started.",
			Unit:        "attempt",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(methodAttr, targetAttr),
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
						Attributes: attribute.NewSet(clientSideEnd...),
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
						Attributes:   attribute.NewSet(clientSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: bucketCounts,
						Min:          extrema,
						Max:          extrema,
						Sum:          compressedBytesSentRecv,
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
						Attributes:   attribute.NewSet(clientSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: bucketCounts,
						Min:          extrema,
						Max:          extrema,
						Sum:          compressedBytesSentRecv,
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
						Attributes: attribute.NewSet(methodAttr, targetAttr, statusAttr),
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
						Attributes: attribute.NewSet(methodAttr),
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
						Attributes:   attribute.NewSet(serverSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: bucketCounts,
						Min:          extrema,
						Max:          extrema,
						Sum:          compressedBytesSentRecv,
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
						Attributes:   attribute.NewSet(serverSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: bucketCounts,
						Min:          extrema,
						Max:          extrema,
						Sum:          compressedBytesSentRecv,
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
						Attributes: attribute.NewSet(serverSideEnd...),
						Count:      1,
						Bounds:     DefaultLatencyBounds,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
	}
}

// MetricDataStreaming returns a list of expected metrics defined in A66 for a
// client and server for one streaming RPC.
func MetricDataStreaming(options MetricDataOptions) []metricdata.Metrics {
	methodAttr := attribute.String("grpc.method", "grpc.testing.TestService/FullDuplexCall")
	targetAttr := attribute.String("grpc.target", options.Target)
	statusAttr := attribute.String("grpc.status", "OK")
	clientSideEnd := []attribute.KeyValue{
		methodAttr,
		targetAttr,
		statusAttr,
	}
	serverSideEnd := []attribute.KeyValue{
		methodAttr,
		statusAttr,
	}
	clientSideEnd = append(clientSideEnd, options.CSMLabels...)
	serverSideEnd = append(serverSideEnd, options.CSMLabels...)
	compressedBytesSentRecv := int64(options.StreamingCompressedMessageSize)
	bucketCounts := createBucketCounts([]float64{options.StreamingCompressedMessageSize}, DefaultSizeBounds)
	extrema := metricdata.NewExtrema(int64(options.StreamingCompressedMessageSize))
	return []metricdata.Metrics{
		{
			Name:        "grpc.client.attempt.started",
			Description: "Number of client call attempts started.",
			Unit:        "attempt",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(methodAttr, targetAttr),
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
						Attributes: attribute.NewSet(clientSideEnd...),
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
						Attributes:   attribute.NewSet(clientSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: bucketCounts,
						Min:          extrema,
						Max:          extrema,
						Sum:          compressedBytesSentRecv,
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
						Attributes:   attribute.NewSet(clientSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: bucketCounts,
						Min:          extrema,
						Max:          extrema,
						Sum:          compressedBytesSentRecv,
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
						Attributes: attribute.NewSet(methodAttr, targetAttr, statusAttr),
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
						Attributes: attribute.NewSet(methodAttr),
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
						Attributes:   attribute.NewSet(serverSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: bucketCounts,
						Min:          extrema,
						Max:          extrema,
						Sum:          compressedBytesSentRecv,
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
						Attributes:   attribute.NewSet(serverSideEnd...),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: bucketCounts,
						Min:          extrema,
						Max:          extrema,
						Sum:          compressedBytesSentRecv,
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
						Attributes: attribute.NewSet(serverSideEnd...),
						Count:      1,
						Bounds:     DefaultLatencyBounds,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
	}
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
	unaryCompressedBytesSentRecv := int64(options.UnaryCompressedMessageSize)
	unaryBucketCounts := createBucketCounts([]float64{options.UnaryCompressedMessageSize}, DefaultSizeBounds)
	unaryExtrema := metricdata.NewExtrema(int64(options.UnaryCompressedMessageSize))

	streamingCompressedBytesSentRecv := int64(options.StreamingCompressedMessageSize)
	streamingBucketCounts := createBucketCounts([]float64{options.StreamingCompressedMessageSize}, DefaultSizeBounds)
	streamingExtrema := metricdata.NewExtrema(int64(options.StreamingCompressedMessageSize))

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

// CompareMetrics asserts wantMetrics are what we expect. It polls for eventual
// server metrics (not emitted synchronously with client side rpc returning),
// and for duration metrics makes sure the data point is within possible testing
// time (five seconds from context timeout).
func CompareMetrics(ctx context.Context, t *testing.T, mr *metric.ManualReader, gotMetrics map[string]metricdata.Metrics, wantMetrics []metricdata.Metrics) {
	for _, metric := range wantMetrics {
		if metric.Name == "grpc.server.call.sent_total_compressed_message_size" || metric.Name == "grpc.server.call.rcvd_total_compressed_message_size" {
			// Sync the metric reader to see the event because stats.End is
			// handled async server side. Thus, poll until metrics created from
			// stats.End show up.
			var err error
			if gotMetrics, err = waitForServerCompletedRPCs(ctx, mr, metric); err != nil { // move to shared helper
				t.Fatal(err)
			}
			val := gotMetrics[metric.Name]
			if !metricdatatest.AssertEqual(t, metric, val, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars()) {
				t.Fatalf("Metrics data type not equal for metric: %v", metric.Name)
			}
			continue
		}

		// If one of the duration metrics, ignore the bucket counts, and make
		// sure it count falls within a bucket <= 5 seconds (maximum duration of
		// test due to context).
		if metric.Name == "grpc.client.attempt.duration" || metric.Name == "grpc.client.call.duration" || metric.Name == "grpc.server.call.duration" {
			var err error
			if gotMetrics, err = waitForServerCompletedRPCs(ctx, mr, metric); err != nil { // move to shared helper
				t.Fatal(err)
			}
			val := gotMetrics[metric.Name]
			if !metricdatatest.AssertEqual(t, metric, val, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars(), metricdatatest.IgnoreValue()) {
				t.Fatalf("Metrics data type not equal for metric: %v", metric.Name)
			}
			if err := checkDataPointWithinFiveSeconds(val); err != nil {
				t.Fatalf("Data point not within five seconds for metric %v: %v", metric.Name, err)
			}
			continue
		}

		val, ok := gotMetrics[metric.Name]
		if !ok {
			t.Fatalf("Metric %v not present in recorded metrics", metric.Name)
		}
		if !metricdatatest.AssertEqual(t, metric, val, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars()) {
			t.Fatalf("Metrics data type not equal for metric: %v", metric.Name)
		}
	}
}

// waitForCompleteTraceSpans waits until the in-memory span exporter has
// received all the expected number of spans.  It polls the exporter at a short
// interval until the desired number of spans are available or the context is
// cancelled.
//
// Returns the collected spans or an error if the context deadline is exceeded
// before the expected number of spans are exported.
func waitForCompleteTraceSpans(ctx context.Context, exporter *tracetest.InMemoryExporter, wantSpans int) (tracetest.SpanStubs, error) {
	var spans []tracetest.SpanStub
	for ; ctx.Err() == nil; <-time.After(time.Millisecond) {
		spans = exporter.GetSpans()
		if len(spans) == wantSpans {
			return spans, nil
		}
	}
	return nil, fmt.Errorf("error waiting for complete trace spans %d: %v", wantSpans, ctx.Err())
}

// TraceDataWithCompressor returns a TraceSpanInfo for a unary RPC and
// streaming RPC with certain compression and message flow sent, when
// compressor is used
func TraceDataWithCompressor() []TraceSpanInfo {
	return []TraceSpanInfo{
		{
			Name:     "grpc.testing.TestService.UnaryCall",
			SpanKind: oteltrace.SpanKindServer.String(),
			Attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "FailFast",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.IntValue(0),
				},
			},
			Events: []trace.Event{
				{
					Name: "Inbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(57),
						},
					},
				},
				{
					Name: "Outbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(57),
						},
					},
				},
			},
		},
		{
			Name:     "Attempt.grpc.testing.TestService.UnaryCall",
			SpanKind: oteltrace.SpanKindInternal.String(),
			Attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.IntValue(1),
				},
				{
					Key:   "FailFast",
					Value: attribute.IntValue(1),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.IntValue(0),
				},
			},
			Events: []trace.Event{
				{
					Name: "Outbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(57),
						},
					},
				},
				{
					Name: "Inbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(57),
						},
					},
				},
			},
		},
		{
			Name:       "grpc.testing.TestService.UnaryCall",
			SpanKind:   oteltrace.SpanKindClient.String(),
			Attributes: []attribute.KeyValue{},
			Events:     []trace.Event{},
		},
		{
			Name:     "grpc.testing.TestService.FullDuplexCall",
			SpanKind: oteltrace.SpanKindServer.String(),
			Attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "FailFast",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.IntValue(0),
				},
			},
			Events: []trace.Event{},
		},
		{
			Name:       "grpc.testing.TestService.FullDuplexCall",
			SpanKind:   oteltrace.SpanKindClient.String(),
			Attributes: []attribute.KeyValue{},
			Events:     []trace.Event{},
		},
		{
			Name:     "Attempt.grpc.testing.TestService.FullDuplexCall",
			SpanKind: oteltrace.SpanKindInternal.String(),
			Attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.IntValue(1),
				},
				{
					Key:   "FailFast",
					Value: attribute.IntValue(1),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.IntValue(0),
				},
			},
			Events: []trace.Event{},
		},
	}
}

// TraceDataWithoutCompressor returns a TraceSpanInfo for a unary RPC and
// streaming RPC with certain compression and message flow sent, when
// compressor is not used.
func TraceDataWithoutCompressor() []TraceSpanInfo {
	return []TraceSpanInfo{
		{
			Name:     "grpc.testing.TestService.UnaryCall",
			SpanKind: oteltrace.SpanKindServer.String(),
			Attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "FailFast",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.IntValue(0),
				},
			},
			Events: []trace.Event{
				{
					Name: "Inbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(10006),
						},
					},
				},
				{
					Name: "Outbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(10006),
						},
					},
				},
			},
		},
		{
			Name:     "Attempt.grpc.testing.TestService.UnaryCall",
			SpanKind: oteltrace.SpanKindInternal.String(),
			Attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.IntValue(1),
				},
				{
					Key:   "FailFast",
					Value: attribute.IntValue(1),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.IntValue(0),
				},
			},
			Events: []trace.Event{
				{
					Name: "Outbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(10006),
						},
					},
				},
				{
					Name: "Inbound compressed message",
					Attributes: []attribute.KeyValue{
						{
							Key:   "sequence-number",
							Value: attribute.IntValue(1),
						},
						{
							Key:   "message-size",
							Value: attribute.IntValue(10006),
						},
						{
							Key:   "message-size-compressed",
							Value: attribute.IntValue(10006),
						},
					},
				},
			},
		},
		{
			Name:       "grpc.testing.TestService.UnaryCall",
			SpanKind:   oteltrace.SpanKindClient.String(),
			Attributes: []attribute.KeyValue{},
			Events:     []trace.Event{},
		},
		{
			Name:     "grpc.testing.TestService.FullDuplexCall",
			SpanKind: oteltrace.SpanKindServer.String(),
			Attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "FailFast",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.IntValue(0),
				},
			},
			Events: []trace.Event{},
		},
		{
			Name:       "grpc.testing.TestService.FullDuplexCall",
			SpanKind:   oteltrace.SpanKindClient.String(),
			Attributes: []attribute.KeyValue{},
			Events:     []trace.Event{},
		},
		{
			Name:     "Attempt.grpc.testing.TestService.FullDuplexCall",
			SpanKind: oteltrace.SpanKindInternal.String(),
			Attributes: []attribute.KeyValue{
				{
					Key:   "Client",
					Value: attribute.IntValue(1),
				},
				{
					Key:   "FailFast",
					Value: attribute.IntValue(1),
				},
				{
					Key:   "previous-rpc-attempts",
					Value: attribute.IntValue(0),
				},
				{
					Key:   "transparent-retry",
					Value: attribute.IntValue(0),
				},
			},
			Events: []trace.Event{},
		},
	}
}

// VerifyAndCompareTraces first waits for the exporter to receive the expected
// number of spans. It then groups the received spans by their TraceID. For
// each trace group, it identifies the client, server, and attempt spans for
// both unary and streaming RPCs. It checks that the expected spans are
// present and that the server spans have the correct parent (attempt span).
// Finally, it compares the content of each span (name, kind, attributes,
// events) against the provided expected spans information.
func VerifyAndCompareTraces(ctx context.Context, t *testing.T, exporter *tracetest.InMemoryExporter, wantSpans int, wantSpanInfos []TraceSpanInfo) {
	spans, err := waitForCompleteTraceSpans(ctx, exporter, wantSpans)
	if err != nil {
		t.Fatal(err)
	}

	// Group spans by TraceID
	traceSpans := make(map[oteltrace.TraceID][]tracetest.SpanStub)
	for _, span := range spans {
		traceID := span.SpanContext.TraceID()
		traceSpans[traceID] = append(traceSpans[traceID], span)
	}

	// For each trace group, verify relationships and content
	for traceID, spans := range traceSpans {
		var unaryClient, unaryServer, unaryAttempt *tracetest.SpanStub
		var streamClient, streamServer, streamAttempt *tracetest.SpanStub
		var isUnary, isStream bool

		for _, span := range spans {
			switch {
			case span.Name == "grpc.testing.TestService.UnaryCall":
				isUnary = true
				if span.SpanKind == oteltrace.SpanKindClient {
					unaryClient = &span
				} else {
					unaryServer = &span
				}
			case span.Name == "Attempt.grpc.testing.TestService.UnaryCall":
				isUnary = true
				unaryAttempt = &span
			case span.Name == "grpc.testing.TestService.FullDuplexCall":
				isStream = true
				if span.SpanKind == oteltrace.SpanKindClient {
					streamClient = &span
				} else {
					streamServer = &span
				}
			case span.Name == "Attempt.grpc.testing.TestService.FullDuplexCall":
				isStream = true
				streamAttempt = &span
			}
		}

		if isUnary {
			// Verify Unary Call Spans
			if unaryClient == nil {
				t.Error("Unary call client span not found")
			}
			if unaryServer == nil {
				t.Error("Unary call server span not found")
			}
			if unaryAttempt == nil {
				t.Error("Unary call attempt span not found")
			}
			// Check TraceID consistency
			if unaryClient != nil && unaryClient.SpanContext.TraceID() != traceID || unaryServer.SpanContext.TraceID() != traceID {
				t.Error("Unary call spans have inconsistent TraceIDs")
			}
			// Check parent-child relationship via SpanID
			if unaryServer != nil && unaryServer.Parent.SpanID() != unaryAttempt.SpanContext.SpanID() {
				t.Error("Unary server span parent does not match attempt span ID")
			}
		}

		if isStream {
			// Verify Streaming Call Spans
			if streamClient == nil {
				t.Error("Streaming call client span not found")
			}
			if streamServer == nil {
				t.Error("Streaming call server span not found")
			}
			if streamAttempt == nil {
				t.Error("Streaming call attempt span not found")
			}
			// Check TraceID consistency
			if streamClient != nil && streamClient.SpanContext.TraceID() != traceID || streamServer.SpanContext.TraceID() != traceID {
				t.Error("Streaming call spans have inconsistent TraceIDs")
			}
			if streamServer != nil && streamServer.Parent.SpanID() != streamAttempt.SpanContext.SpanID() {
				t.Error("Streaming server span parent does not match attempt span ID")
			}
		}
	}

	compareTraces(t, spans, wantSpanInfos)
}

func compareTraces(t *testing.T, spans tracetest.SpanStubs, wantSpanInfos []TraceSpanInfo) {
	// Validate attributes/events by span type instead of index
	for _, span := range spans {
		// Check that the attempt span has the correct status
		if got, want := span.Status.Code, otelcodes.Ok; got != want {
			t.Errorf("Got status code %v, want %v", got, want)
		}

		var want TraceSpanInfo
		switch {
		case span.Name == "grpc.testing.TestService.UnaryCall" && span.SpanKind == oteltrace.SpanKindServer:
			want = wantSpanInfos[0] // Reference expected unary server span
		case span.Name == "Attempt.grpc.testing.TestService.UnaryCall" && span.SpanKind == oteltrace.SpanKindInternal:
			want = wantSpanInfos[1]
		case span.Name == "grpc.testing.TestService.UnaryCall" && span.SpanKind == oteltrace.SpanKindClient:
			want = wantSpanInfos[2]
		case span.Name == "grpc.testing.TestService.FullDuplexCall" && span.SpanKind == oteltrace.SpanKindServer:
			want = wantSpanInfos[3]
		case span.Name == "grpc.testing.TestService.FullDuplexCall" && span.SpanKind == oteltrace.SpanKindClient:
			want = wantSpanInfos[4]
		case span.Name == "Attempt.grpc.testing.TestService.FullDuplexCall" && span.SpanKind == oteltrace.SpanKindInternal:
			want = wantSpanInfos[5]
		default:
			t.Errorf("Unexpected span Name: %q", span.Name)
			continue
		}

		// name
		if got, want := span.Name, want.Name; got != want {
			t.Errorf("Span name is %q, want %q", got, want)
		}
		// spanKind
		if got, want := span.SpanKind.String(), want.SpanKind; got != want {
			t.Errorf("Got span kind %q, want %q", got, want)
		}
		// attributes
		if got, want := len(span.Attributes), len(want.Attributes); got != want {
			t.Errorf("Got attributes list of size %q, want %q", got, want)
		}
		for idx, att := range span.Attributes {
			if got, want := att.Key, want.Attributes[idx].Key; got != want {
				t.Errorf("Got attribute key for span name %v as %v, want %v", span.Name, got, want)
			}
		}
		// events
		if got, want := len(span.Events), len(want.Events); got != want {
			t.Errorf("Event length is %q, want %q", got, want)
		}
		for eventIdx, event := range span.Events {
			if got, want := event.Name, want.Events[eventIdx].Name; got != want {
				t.Errorf("Got event name for span name %q as %q, want %q", span.Name, got, want)
			}
			for idx, att := range event.Attributes {
				if got, want := att.Key, want.Events[eventIdx].Attributes[idx].Key; got != want {
					t.Errorf("Got attribute key for span name %q with event name %v, as %v, want %v", span.Name, event.Name, got, want)
				}
				if got, want := att.Value, want.Events[eventIdx].Attributes[idx].Value; got != want {
					t.Errorf("Got attribute value for span name %v with event name %v, as %v, want %v", span.Name, event.Name, got, want)
				}
			}
		}
	}
}
