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

// Package stats contains experimental metrics/stats API's.
package stats

import "google.golang.org/grpc/stats"

// MetricsRecorder records on metrics derived from metric registry.
type MetricsRecorder interface {
	// RecordInt64Count records the measurement alongside labels on the int
	// count associated with the provided handle.
	RecordInt64Count(handle *Int64CountHandle, incr int64, labels ...string)
	// RecordFloat64Count records the measurement alongside labels on the float
	// count associated with the provided handle.
	RecordFloat64Count(handle *Float64CountHandle, incr float64, labels ...string)
	// RecordInt64Histo records the measurement alongside labels on the int
	// histo associated with the provided handle.
	RecordInt64Histo(handle *Int64HistoHandle, incr int64, labels ...string)
	// RecordFloat64Histo records the measurement alongside labels on the float
	// histo associated with the provided handle.
	RecordFloat64Histo(handle *Float64HistoHandle, incr float64, labels ...string)
	// RecordInt64Gauge records the measurement alongside labels on the int
	// gauge associated with the provided handle.
	RecordInt64Gauge(handle *Int64GaugeHandle, incr int64, labels ...string)
	// RecordInt64UpDownCounter records the measurement alongside labels on the int
	// count associated with the provided handle.
	RecordInt64UpDownCount(handle *Int64UpDownCountHandle, incr int64, labels ...string)
	// RegisterAsyncReporter registers a reporter to produce metric values for
	// only the listed descriptors. The returned function must be called when
	// the metrics are no longer needed, which will remove the reporter. The
	// returned method needs to be idempotent and concurrent safe.
	RegisterAsyncReporter(reporter AsyncMetricReporter, descriptors ...AsyncMetric) func()
}

// AsyncMetricReporter is an interface for types that record metrics asynchronously
// for the set of descriptors they are registered with. The AsyncMetricsRecorder
// parameter is used to record values for these metrics.
//
// Implementations must make unique recordings across all registered
// AsyncMetricReporters. Meaning, they should not report values for a metric with
// the same attributes as another AsyncMetricReporter will report.
//
// Implementations must be concurrent-safe.
type AsyncMetricReporter interface {
	// Report records metric values using the provided recorder.
	Report(AsyncMetricsRecorder) error
}

// AsyncMetricReporterFunc is an adapter to allow the use of ordinary functions as
// AsyncMetricReporters.
type AsyncMetricReporterFunc func(AsyncMetricsRecorder) error

// Report calls f(r).
func (f AsyncMetricReporterFunc) Report(r AsyncMetricsRecorder) error {
	return f(r)
}

// AsyncMetricsRecorder records on asynchronous metrics derived from metric registry.
type AsyncMetricsRecorder interface {
	// RecordInt64AsyncGauge records the measurement alongside labels on the int
	// count associated with the provided handle asynchronously
	RecordInt64AsyncGauge(handle *Int64AsyncGaugeHandle, incr int64, labels ...string)
}

// Metrics is an experimental legacy alias of the now-stable stats.MetricSet.
// Metrics will be deleted in a future release.
type Metrics = stats.MetricSet

// Metric was replaced by direct usage of strings.
type Metric = string

// NewMetrics is an experimental legacy alias of the now-stable
// stats.NewMetricSet.  NewMetrics will be deleted in a future release.
func NewMetrics(metrics ...Metric) *Metrics {
	return stats.NewMetricSet(metrics...)
}
