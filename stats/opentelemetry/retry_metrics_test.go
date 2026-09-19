/*
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
 */

package opentelemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

func TestRetryMetrics_Unit(t *testing.T) {
	reader := otelmetric.NewManualReader()
	provider := otelmetric.NewMeterProvider(otelmetric.WithReader(reader))
	opts := Options{
		MetricsOptions: MetricsOptions{
			MeterProvider: provider,
			Metrics: stats.NewMetricSet(
				ClientCallRetriesMetricName,
				ClientCallTransparentRetriesMetricName,
				ClientCallHedgesMetricName,
				ClientCallRetryDelayMetricName,
			),
		},
	}

	h := &clientMetricsHandler{options: opts}
	h.initializeMetrics()

	// Setup call info.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ci := &callInfo{
		target: "test-target",
		method: "test-method",
	}
	ctx = context.WithValue(ctx, callInfoKey{}, ci)

	now := time.Now()
	t1Start := now
	t1End := t1Start.Add(10 * time.Millisecond)

	t2Start := t1End.Add(50 * time.Millisecond) // 50ms delay
	t2End := t2Start.Add(10 * time.Millisecond)

	t3Start := t2End.Add(60 * time.Millisecond) // 60ms delay
	t3End := t3Start.Add(10 * time.Millisecond)

	// Regular attempt.
	ctx1 := h.TagRPC(ctx, &stats.RPCTagInfo{FullMethodName: "test-method"})
	h.HandleRPC(ctx1, &stats.Begin{Client: true, BeginTime: t1Start})
	h.HandleRPC(ctx1, &stats.End{Client: true, BeginTime: t1Start, EndTime: t1End, Error: status.Error(codes.Unavailable, "unavailable")})

	// Transparent retry.
	ctx2 := h.TagRPC(ctx, &stats.RPCTagInfo{FullMethodName: "test-method"})
	h.HandleRPC(ctx2, &stats.Begin{Client: true, BeginTime: t2Start, IsTransparentRetryAttempt: true})
	h.HandleRPC(ctx2, &stats.End{Client: true, BeginTime: t2Start, EndTime: t2End, Error: status.Error(codes.Unavailable, "unavailable")})

	// Regular retry.
	ctx3 := h.TagRPC(ctx, &stats.RPCTagInfo{FullMethodName: "test-method"})
	h.HandleRPC(ctx3, &stats.Begin{Client: true, BeginTime: t3Start})
	h.HandleRPC(ctx3, &stats.End{Client: true, BeginTime: t3Start, EndTime: t3End, Error: nil})

	h.perCallMetrics(ctx, nil, now, ci)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("failed to collect metrics: %v", err)
	}

	gotMetrics := make(map[string]metricdata.Metrics)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			gotMetrics[m.Name] = m
		}
	}

	// Verify grpc.client.call.retries.
	// We made 2 non-transparent attempts (Attempt 1 and Attempt 3), so
	// retries = 1.
	retriesMetric, ok := gotMetrics[ClientCallRetriesMetricName]
	if !ok {
		t.Fatalf("metric %q not found", ClientCallRetriesMetricName)
	}
	wantRetries := metricdata.Metrics{
		Name:        ClientCallRetriesMetricName,
		Description: "Number of retries during the client call. If there were no retries, 0 is not reported.",
		Unit:        "{retry}",
		Data: metricdata.Histogram[int64]{
			DataPoints: []metricdata.HistogramDataPoint[int64]{
				{
					Attributes:   attribute.NewSet(attribute.String("grpc.method", "test-method"), attribute.String("grpc.target", "test-target")),
					Count:        1,
					Bounds:       DefaultRetryBounds,
					BucketCounts: []uint64{1, 0, 0, 0, 0, 0},
					Min:          metricdata.NewExtrema[int64](1),
					Max:          metricdata.NewExtrema[int64](1),
					Sum:          1,
				},
			},
			Temporality: metricdata.CumulativeTemporality,
		},
	}
	metricdatatest.AssertEqual(t, wantRetries, retriesMetric, metricdatatest.IgnoreTimestamp())

	// Verify grpc.client.call.transparent_retries.
	// We made 1 transparent retry (Attempt 2), so transparent_retries = 1.
	transRetriesMetric, ok := gotMetrics[ClientCallTransparentRetriesMetricName]
	if !ok {
		t.Fatalf("metric %q not found", ClientCallTransparentRetriesMetricName)
	}
	wantTransRetries := metricdata.Metrics{
		Name:        ClientCallTransparentRetriesMetricName,
		Description: "Number of transparent retries during the client call. If there were no transparent retries, 0 is not reported.",
		Unit:        "{transparent_retry}",
		Data: metricdata.Histogram[int64]{
			DataPoints: []metricdata.HistogramDataPoint[int64]{
				{
					Attributes:   attribute.NewSet(attribute.String("grpc.method", "test-method"), attribute.String("grpc.target", "test-target")),
					Count:        1,
					Bounds:       DefaultTransparentRetryBounds,
					BucketCounts: []uint64{1, 0, 0, 0, 0, 0, 0},
					Min:          metricdata.NewExtrema[int64](1),
					Max:          metricdata.NewExtrema[int64](1),
					Sum:          1,
				},
			},
			Temporality: metricdata.CumulativeTemporality,
		},
	}
	metricdatatest.AssertEqual(t, wantTransRetries, transRetriesMetric, metricdatatest.IgnoreTimestamp())

	// Verify grpc.client.call.retry_delay.
	// Delay is between Attempt 1 end and Attempt 2 start (50ms),
	// and Attempt 2 end and Attempt 3 start (60ms).
	// So total delay should be 110ms (0.11s).
	delayMetric, ok := gotMetrics[ClientCallRetryDelayMetricName]
	if !ok {
		t.Fatalf("metric %q not found", ClientCallRetryDelayMetricName)
	}
	histo, ok := delayMetric.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("retry_delay type got: %T, want: metricdata.Histogram[float64]", delayMetric.Data)
	}
	if len(histo.DataPoints) != 1 {
		t.Fatalf("data points length got: %d, want: 1", len(histo.DataPoints))
	}
	dp := histo.DataPoints[0]
	// Floating point math might make it 0.110000001
	if dp.Sum < 0.109 || dp.Sum > 0.111 {
		t.Errorf("retry delay sum got: %v, want: ~0.11s", dp.Sum)
	}
}
