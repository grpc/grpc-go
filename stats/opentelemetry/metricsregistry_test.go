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
	"testing"
	"time"

	estats "google.golang.org/grpc/experimental/stats"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpctest"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/sdk/metric"
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

type metricsRecorderForTest interface {
	estats.MetricsRecorder
	initializeMetrics()
}

func newClientStatsHandler(options MetricsOptions) metricsRecorderForTest {
	return &clientMetricsHandler{options: Options{MetricsOptions: options}}
}

func newServerStatsHandler(options MetricsOptions) metricsRecorderForTest {
	return &serverMetricsHandler{options: Options{MetricsOptions: options}}
}

// TestMetricsRegistryMetrics tests the OpenTelemetry behavior with respect to
// registered metrics. It registers metrics in the metrics registry. It then
// creates an OpenTelemetry client and server stats handler This test then makes
// measurements on those instruments using one of the stats handlers, then tests
// the expected metrics emissions, which includes default metrics and optional
// label assertions.
func (s) TestMetricsRegistryMetrics(t *testing.T) {
	cleanup := internal.SnapshotMetricRegistryForTesting()
	defer cleanup()

	intCountHandle1 := estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "int-counter-1",
		Description:    "Sum of calls from test",
		Unit:           "int",
		Labels:         []string{"int counter 1 label key"},
		OptionalLabels: []string{"int counter 1 optional label key"},
		Default:        true,
	})
	// A non default metric. If not specified in OpenTelemetry constructor, this
	// will become a no-op, so measurements recorded on it won't show up in
	// emitted metrics.
	intCountHandle2 := estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "int-counter-2",
		Description:    "Sum of calls from test",
		Unit:           "int",
		Labels:         []string{"int counter 2 label key"},
		OptionalLabels: []string{"int counter 2 optional label key"},
		Default:        false,
	})
	// Register another non default metric. This will get added to the default
	// metrics set in the OpenTelemetry constructor options, so metrics recorded
	// on this should show up in metrics emissions.
	intCountHandle3 := estats.RegisterInt64Count(estats.MetricDescriptor{
		Name:           "int-counter-3",
		Description:    "sum of calls from test",
		Unit:           "int",
		Labels:         []string{"int counter 3 label key"},
		OptionalLabels: []string{"int counter 3 optional label key"},
		Default:        false,
	})
	floatCountHandle := estats.RegisterFloat64Count(estats.MetricDescriptor{
		Name:           "float-counter",
		Description:    "sum of calls from test",
		Unit:           "float",
		Labels:         []string{"float counter label key"},
		OptionalLabels: []string{"float counter optional label key"},
		Default:        true,
	})
	bounds := []float64{0, 5, 10}
	intHistoHandle := estats.RegisterInt64Histo(estats.MetricDescriptor{
		Name:           "int-histo",
		Description:    "histogram of call values from tests",
		Unit:           "int",
		Labels:         []string{"int histo label key"},
		OptionalLabels: []string{"int histo optional label key"},
		Default:        true,
		Bounds:         bounds,
	})
	floatHistoHandle := estats.RegisterFloat64Histo(estats.MetricDescriptor{
		Name:           "float-histo",
		Description:    "histogram of call values from tests",
		Unit:           "float",
		Labels:         []string{"float histo label key"},
		OptionalLabels: []string{"float histo optional label key"},
		Default:        true,
		Bounds:         bounds,
	})
	intGaugeHandle := estats.RegisterInt64Gauge(estats.MetricDescriptor{
		Name:           "simple-gauge",
		Description:    "the most recent int emitted by test",
		Unit:           "int",
		Labels:         []string{"int gauge label key"},
		OptionalLabels: []string{"int gauge optional label key"},
		Default:        true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Only float optional labels are configured, so only float optional labels should show up.
	// All required labels should show up.
	wantMetrics := []metricdata.Metrics{
		{
			Name:        "int-counter-1",
			Description: "Sum of calls from test",
			Unit:        "int",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("int counter 1 label key", "int counter 1 label value")), // No optional label, not float.
						Value:      1,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
		{
			Name:        "int-counter-3",
			Description: "sum of calls from test",
			Unit:        "int",
			Data: metricdata.Sum[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("int counter 3 label key", "int counter 3 label value")), // No optional label, not float.
						Value:      4,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
		{
			Name:        "float-counter",
			Description: "sum of calls from test",
			Unit:        "float",
			Data: metricdata.Sum[float64]{
				DataPoints: []metricdata.DataPoint[float64]{
					{
						Attributes: attribute.NewSet(attribute.String("float counter label key", "float counter label value"), attribute.String("float counter optional label key", "float counter optional label value")),
						Value:      1.2,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
			},
		},
		{
			Name:        "int-histo",
			Description: "histogram of call values from tests",
			Unit:        "int",
			Data: metricdata.Histogram[int64]{
				DataPoints: []metricdata.HistogramDataPoint[int64]{
					{
						Attributes:   attribute.NewSet(attribute.String("int histo label key", "int histo label value")), // No optional label, not float.
						Count:        1,
						Bounds:       bounds,
						BucketCounts: []uint64{0, 1, 0, 0},
						Min:          metricdata.NewExtrema(int64(3)),
						Max:          metricdata.NewExtrema(int64(3)),
						Sum:          3,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
		{
			Name:        "float-histo",
			Description: "histogram of call values from tests",
			Unit:        "float",
			Data: metricdata.Histogram[float64]{
				DataPoints: []metricdata.HistogramDataPoint[float64]{
					{
						Attributes:   attribute.NewSet(attribute.String("float histo label key", "float histo label value"), attribute.String("float histo optional label key", "float histo optional label value")),
						Count:        1,
						Bounds:       bounds,
						BucketCounts: []uint64{0, 1, 0, 0},
						Min:          metricdata.NewExtrema(float64(4.3)),
						Max:          metricdata.NewExtrema(float64(4.3)),
						Sum:          4.3,
					},
				},
				Temporality: metricdata.CumulativeTemporality,
			},
		},
		{
			Name:        "simple-gauge",
			Description: "the most recent int emitted by test",
			Unit:        "int",
			Data: metricdata.Gauge[int64]{
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(attribute.String("int gauge label key", "int gauge label value")), // No optional label, not float.
						Value:      8,
					},
				},
			},
		},
	}

	for _, test := range []struct {
		name        string
		constructor func(options MetricsOptions) metricsRecorderForTest
	}{
		{
			name:        "client stats handler",
			constructor: newClientStatsHandler,
		},
		{
			name:        "server stats handler",
			constructor: newServerStatsHandler,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := otelmetric.NewManualReader()
			provider := otelmetric.NewMeterProvider(otelmetric.WithReader(reader))

			// This configures the defaults alongside int counter 3. All the instruments
			// registered except int counter 2 and 3 are default, so all measurements
			// recorded should show up in reader collected metrics except those for int
			// counter 2.
			// This also only toggles the float count and float histo optional labels,
			// so only those should show up in metrics emissions. All the required
			// labels should show up in metrics emissions.
			mo := MetricsOptions{
				Metrics:        DefaultMetrics().Add("int-counter-3"),
				OptionalLabels: []string{"float counter optional label key", "float histo optional label key"},
				MeterProvider:  provider,
			}
			mr := test.constructor(mo)
			mr.initializeMetrics()
			// These Record calls are guaranteed at a layer underneath OpenTelemetry for
			// labels emitted to match the length of labels + optional labels.
			intCountHandle1.Record(mr, 1, []string{"int counter 1 label value", "int counter 1 optional label value"}...)
			// int-counter-2 is not part of metrics specified (not default), so this
			// record call shouldn't show up in the reader.
			intCountHandle2.Record(mr, 2, []string{"int counter 2 label value", "int counter 2 optional label value"}...)
			// int-counter-3 is part of metrics specified, so this call should show up
			// in the reader.
			intCountHandle3.Record(mr, 4, []string{"int counter 3 label value", "int counter 3 optional label value"}...)

			// All future recording points should show up in emissions as all of these are defaults.
			floatCountHandle.Record(mr, 1.2, []string{"float counter label value", "float counter optional label value"}...)
			intHistoHandle.Record(mr, 3, []string{"int histo label value", "int histo optional label value"}...)
			floatHistoHandle.Record(mr, 4.3, []string{"float histo label value", "float histo optional label value"}...)
			intGaugeHandle.Record(mr, 7, []string{"int gauge label value", "int gauge optional label value"}...)
			// This second gauge call should take the place of the previous gauge call.
			intGaugeHandle.Record(mr, 8, []string{"int gauge label value", "int gauge optional label value"}...)
			rm := &metricdata.ResourceMetrics{}
			reader.Collect(ctx, rm)
			gotMetrics := map[string]metricdata.Metrics{}
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					gotMetrics[m.Name] = m
				}
			}

			for _, metric := range wantMetrics {
				val, ok := gotMetrics[metric.Name]
				if !ok {
					t.Fatalf("Metric %v not present in recorded metrics", metric.Name)
				}
				if !metricdatatest.AssertEqual(t, metric, val, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars()) {
					t.Fatalf("Metrics data type not equal for metric: %v", metric.Name)
				}
			}

			// int-counter-2 is not a default metric and wasn't specified in
			// constructor, so emissions should not show up.
			if _, ok := gotMetrics["int-counter-2"]; ok {
				t.Fatalf("Metric int-counter-2 present in recorded metrics, was not configured")
			}
		})
	}
}
