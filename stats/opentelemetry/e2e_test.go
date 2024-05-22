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

package opentelemetry

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
)

var defaultTestTimeout = 5 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

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

// setup creates a stub server with OpenTelemetry component configured on client
// and server side. It returns a reader for metrics emitted from OpenTelemetry
// component and the server.
func setup(t *testing.T, tafOn bool, maf func(string) bool) (*metric.ManualReader, *stubserver.StubServer) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(
		metric.WithReader(reader),
	)
	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{Payload: &testpb.Payload{
				Body: make([]byte, 10000),
			}}, nil
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				_, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
			}
		},
	}
	var taf func(string) bool
	if tafOn {
		taf = func(str string) bool {
			return str != ss.Target
		}
	}
	if err := ss.Start([]grpc.ServerOption{ServerOption(Options{
		MetricsOptions: MetricsOptions{
			MeterProvider:         provider,
			Metrics:               DefaultMetrics,
			TargetAttributeFilter: taf,
			MethodAttributeFilter: maf,
		}})}, DialOption(Options{
		MetricsOptions: MetricsOptions{
			MeterProvider:         provider,
			Metrics:               DefaultMetrics,
			TargetAttributeFilter: taf,
			MethodAttributeFilter: maf,
		},
	})); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	return reader, ss
}

// TestMethodTargetAttributeFilter tests the method and target attribute filter.
// The method and target filter set should bucket the grpc.method/grpc.target
// attribute into "other" if filter specifies.
func (s) TestMethodTargetAttributeFilter(t *testing.T) {
	maf := func(str string) bool {
		// Will allow duplex/any other type of RPC.
		return str != "/grpc.testing.TestService/UnaryCall"
	}
	// pull out setup into a helper
	reader, ss := setup(t, true, maf)
	defer ss.Stop()

	// make a single RPC (unary rpc), and filter out the target and method
	// that would correspond.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{
		Body: make([]byte, 10000),
	}}); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}

	stream.CloseSend()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("unexpected error: %v, expected an EOF error", err)
	}
	rm := &metricdata.ResourceMetrics{}
	reader.Collect(ctx, rm)

	wantMetrics := []metricdata.Metrics{
		{
			Name:        "grpc.client.attempt.started",
			Description: "Number of client call attempts started.",
			Unit:        "attempt",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("grpc.method", "grpc.testing.TestService/UnaryCall"), attribute.String("grpc.target", "other")),
						Value:      1,
					},
					{
						Attributes: attribute.NewSet(attribute.String("grpc.method", "grpc.testing.TestService/FullDuplexCall"), attribute.String("grpc.target", "other")),
						Value:      1,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
	}
	gotMetrics := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			gotMetrics[m.Name] = m
		}
	}

	for _, metric := range wantMetrics {
		val, ok := gotMetrics[metric.Name]
		if !ok {
			t.Fatalf("metric %v not present in recorded metrics", metric.Name)
		}
		if !metricdatatest.AssertEqual(t, metric, val, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars()) {
			t.Fatalf("metrics data type not equal for metric: %v", metric.Name)
		}
	}
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

// TestAllMetricsOneFunction tests emitted metrics from OpenTelemetry
// instrumentation component. It then configures a system with a gRPC Client and
// gRPC server with the OpenTelemetry Dial and Server Option configured
// specifying all the metrics provided by this package, and makes a Unary RPC
// and a Streaming RPC. These two RPCs should cause certain recording for each
// registered metric observed through a Manual Metrics Reader on the provided
// OpenTelemetry SDK's Meter Provider. It then makes an RPC that is unregistered
// on the Client (no StaticMethodCallOption set) and Server. The method
// attribute on subsequent metrics should be bucketed in "other".
func (s) TestAllMetricsOneFunction(t *testing.T) {
	reader, ss := setup(t, false, nil)
	defer ss.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Make two RPC's, a unary RPC and a streaming RPC. These should cause
	// certain metrics to be emitted, which should be able to be observed
	// through the Metric Reader.
	if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{
		Body: make([]byte, 10000),
	}}, grpc.UseCompressor(gzip.Name)); err != nil { // Deterministic compression.
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}

	stream.CloseSend()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("unexpected error: %v, expected an EOF error", err)
	}

	rm := &metricdata.ResourceMetrics{}
	reader.Collect(ctx, rm)

	gotMetrics := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			gotMetrics[m.Name] = m
		}
	}

	unaryMethodAttr := attribute.String("grpc.method", "grpc.testing.TestService/UnaryCall")
	duplexMethodAttr := attribute.String("grpc.method", "grpc.testing.TestService/FullDuplexCall")

	targetAttr := attribute.String("grpc.target", ss.Target)
	statusAttr := attribute.String("grpc.status", "OK")

	wantMetrics := []metricdata.Metrics{
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
						Attributes: attribute.NewSet(unaryMethodAttr, targetAttr, statusAttr),
						Count:      1,
						Bounds:     DefaultLatencyBounds,
					},
					{
						Attributes: attribute.NewSet(duplexMethodAttr, targetAttr, statusAttr),
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
						Attributes:   attribute.NewSet(unaryMethodAttr, targetAttr, statusAttr),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: []uint64{0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
						Min:          metricdata.NewExtrema(int64(57)),
						Max:          metricdata.NewExtrema(int64(57)),
						Sum:          57,
					},
					{
						Attributes:   attribute.NewSet(duplexMethodAttr, targetAttr, statusAttr),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: []uint64{0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
						Min:          metricdata.NewExtrema(int64(0)),
						Max:          metricdata.NewExtrema(int64(0)),
						Sum:          0,
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
						Attributes:   attribute.NewSet(unaryMethodAttr, targetAttr, statusAttr),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: []uint64{0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
						Min:          metricdata.NewExtrema(int64(57)),
						Max:          metricdata.NewExtrema(int64(57)),
						Sum:          57,
					},
					{
						Attributes:   attribute.NewSet(duplexMethodAttr, targetAttr, statusAttr),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: []uint64{0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
						Min:          metricdata.NewExtrema(int64(0)),
						Max:          metricdata.NewExtrema(int64(0)),
						Sum:          0,
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
						Attributes: attribute.NewSet(unaryMethodAttr, targetAttr, statusAttr),
						Count:      1,
						Bounds:     DefaultLatencyBounds,
					},
					{
						Attributes: attribute.NewSet(duplexMethodAttr, targetAttr, statusAttr),
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
						Attributes:   attribute.NewSet(unaryMethodAttr, statusAttr),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: []uint64{0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
						Min:          metricdata.NewExtrema(int64(57)),
						Max:          metricdata.NewExtrema(int64(57)),
						Sum:          57,
					},
					{
						Attributes:   attribute.NewSet(duplexMethodAttr, statusAttr),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: []uint64{0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
						Min:          metricdata.NewExtrema(int64(0)),
						Max:          metricdata.NewExtrema(int64(0)),
						Sum:          0,
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
						Attributes:   attribute.NewSet(unaryMethodAttr, statusAttr),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: []uint64{0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
						Min:          metricdata.NewExtrema(int64(57)),
						Max:          metricdata.NewExtrema(int64(57)),
						Sum:          57,
					},
					{
						Attributes:   attribute.NewSet(duplexMethodAttr, statusAttr),
						Count:        1,
						Bounds:       DefaultSizeBounds,
						BucketCounts: []uint64{0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
						Min:          metricdata.NewExtrema(int64(0)),
						Max:          metricdata.NewExtrema(int64(0)),
						Sum:          0,
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
						Attributes: attribute.NewSet(unaryMethodAttr, statusAttr),
						Count:      1,
						Bounds:     DefaultLatencyBounds,
					},
					{
						Attributes: attribute.NewSet(duplexMethodAttr, statusAttr),
						Count:      1,
						Bounds:     DefaultLatencyBounds,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
	}

	for _, metric := range wantMetrics {
		if metric.Name == "grpc.server.call.sent_total_compressed_message_size" || metric.Name == "grpc.server.call.rcvd_total_compressed_message_size" {
			// Sync the metric reader to see the event because stats.End is
			// handled async server side. Thus, poll until metrics created from
			// stats.End show up.
			if gotMetrics, err = waitForServerCompletedRPCs(ctx, reader, metric, t); err != nil {
				t.Fatalf("error waiting for sent total compressed message size for metric: %v", metric.Name)
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

	stream, err = ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}

	stream.CloseSend()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("unexpected error: %v, expected an EOF error", err)
	}
	// This Invoke doesn't pass the StaticMethodCallOption. Thus, the method
	// attribute should become "other" on client side metrics. Since it is also
	// not registered on the server either, it should also become "other" on the
	// server metrics method attribute.
	ss.CC.Invoke(ctx, "/grpc.testing.TestService/UnregisteredCall", nil, nil, []grpc.CallOption{}...)
	ss.CC.Invoke(ctx, "/grpc.testing.TestService/UnregisteredCall", nil, nil, []grpc.CallOption{}...)
	ss.CC.Invoke(ctx, "/grpc.testing.TestService/UnregisteredCall", nil, nil, []grpc.CallOption{}...)

	rm = &metricdata.ResourceMetrics{}
	reader.Collect(ctx, rm)
	gotMetrics = map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			gotMetrics[m.Name] = m
		}
	}
	otherMethodAttr := attribute.String("grpc.method", "other")
	wantMetrics = []metricdata.Metrics{
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
						Value:      2,
					},
					{
						Attributes: attribute.NewSet(otherMethodAttr, targetAttr),
						Value:      3,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
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
						Value:      2,
					},
					{
						Attributes: attribute.NewSet(otherMethodAttr),
						Value:      3,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
	}
	for _, metric := range wantMetrics {
		val, ok := gotMetrics[metric.Name]
		if !ok {
			t.Fatalf("metric %v not present in recorded metrics", metric.Name)
		}
		if !metricdatatest.AssertEqual(t, metric, val, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars()) {
			t.Fatalf("metrics data type not equal for metric: %v", metric.Name)
		}
	}
}
