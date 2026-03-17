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
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
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
		switch data := val.Data.(type) {
		case metricdata.Histogram[int64]:
			if len(wantMetric.Data.(metricdata.Histogram[int64]).DataPoints) > len(data.DataPoints) {
				continue
			}
		case metricdata.Histogram[float64]:
			if len(wantMetric.Data.(metricdata.Histogram[float64]).DataPoints) > len(data.DataPoints) {
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
			Unit:        "{attempt}",
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
			Unit:        "{call}",
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
			Unit:        "{attempt}",
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
			Unit:        "{call}",
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
			Unit:        "{attempt}",
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
			Unit:        "{call}",
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

// CompareMetrics asserts wantMetrics are what we expect. For duration metrics
// makes sure the data point is within possible testing time (five seconds from
// context timeout).
func CompareMetrics(t *testing.T, gotMetrics map[string]metricdata.Metrics, wantMetrics []metricdata.Metrics) {
	for _, metric := range wantMetrics {
		val, ok := gotMetrics[metric.Name]
		if !ok {
			t.Errorf("Metric %v not present in recorded metrics", metric.Name)
			continue
		}

		if metric.Name == "grpc.server.call.sent_total_compressed_message_size" || metric.Name == "grpc.server.call.rcvd_total_compressed_message_size" {
			val := gotMetrics[metric.Name]
			if !metricdatatest.AssertEqual(t, metric, val, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars()) {
				t.Errorf("Metrics data type not equal for metric: %v", metric.Name)
			}
			continue
		}

		// If one of the duration metrics, ignore the bucket counts, and make
		// sure it count falls within a bucket <= 5 seconds (maximum duration of
		// test due to context).
		if metric.Name == "grpc.client.attempt.duration" || metric.Name == "grpc.client.call.duration" || metric.Name == "grpc.server.call.duration" {
			val := gotMetrics[metric.Name]
			if !metricdatatest.AssertEqual(t, metric, val, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars(), metricdatatest.IgnoreValue()) {
				t.Errorf("Metrics data type not equal for metric: %v", metric.Name)
			}
			if err := checkDataPointWithinFiveSeconds(val); err != nil {
				t.Errorf("Data point not within five seconds for metric %v: %v", metric.Name, err)
			}
			continue
		}

		if !metricdatatest.AssertEqual(t, metric, val, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars()) {
			t.Errorf("Metrics data type not equal for metric: %v", metric.Name)
		}
	}
}

// WaitForServerMetrics waits for eventual server metrics (not emitted
// synchronously with client side rpc returning).
func WaitForServerMetrics(ctx context.Context, t *testing.T, mr *metric.ManualReader, gotMetrics map[string]metricdata.Metrics, wantMetrics []metricdata.Metrics) map[string]metricdata.Metrics {
	terminalMetrics := []string{
		"grpc.server.call.sent_total_compressed_message_size",
		"grpc.server.call.rcvd_total_compressed_message_size",
		"grpc.client.attempt.duration",
		"grpc.client.call.duration",
		"grpc.server.call.duration",
	}
	for _, metric := range wantMetrics {
		if !slices.Contains(terminalMetrics, metric.Name) {
			continue
		}
		// Sync the metric reader to see the event because stats.End is
		// handled async server side. Thus, poll until metrics created from
		// stats.End show up.
		var err error
		if gotMetrics, err = waitForServerCompletedRPCs(ctx, mr, metric); err != nil { // move to shared helper
			t.Fatal(err)
		}
	}

	return gotMetrics
}
