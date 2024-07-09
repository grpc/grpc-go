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

package stats

import (
	"log"

	"google.golang.org/grpc/stats"
)

// DefaultMetrics are the default metrics registered through global metrics
// registry. This is written to at initialization time only, and is read only
// after initialization.
var DefaultMetrics = stats.NewMetrics()

// MetricDescriptor is the data for a registered metric.
type MetricDescriptor struct {
	// Name is the name of this metric. This name must be unique across whole
	// binary (including any per call metrics). See
	// https://github.com/grpc/proposal/blob/master/A79-non-per-call-metrics-architecture.md#metric-instrument-naming-conventions
	// for metric naming conventions.
	Name stats.Metric
	// Description is the description of this metric.
	Description string
	// Unit is the unit of this metric (e.g. entries, milliseconds).
	Unit string
	// Labels are the required label keys for this metric. These are intended to
	// metrics emitted from a stats handler.
	Labels []string
	// OptionalLabels are the optional label keys for this metric. These are
	// intended to attached to metrics emitted from a stats handler if
	// configured.
	OptionalLabels []string
	// Default is whether this metric is on by default.
	Default bool
	// Type is the type of metric. This is set by the metric registry, and not
	// intended to be set by a component registering a metric.
	Type MetricType
}

// Int64CountHandle is a typed handle for a float count metric. This handle
// is passed at the recording point in order to know which metric to record
// on.
type Int64CountHandle struct {
	MetricDescriptor *MetricDescriptor
}

// Record records the int64 count value on the metrics recorder provided.
func (h Int64CountHandle) Record(recorder MetricsRecorder, incr int64, labels ...string) {
	recorder.RecordIntCount(h, incr, labels...)
}

// Float64CountHandle is a typed handle for a float count metric. This handle is
// passed at the recording point in order to know which metric to record on.
type Float64CountHandle struct {
	MetricDescriptor *MetricDescriptor
}

// Record records the float64 count value on the metrics recorder provided.
func (h Float64CountHandle) Record(recorder MetricsRecorder, incr float64, labels ...string) {
	recorder.RecordFloatCount(h, incr, labels...)
}

// Int64HistoHandle is a typed handle for an int histogram metric. This handle
// is passed at the recording point in order to know which metric to record on.
type Int64HistoHandle struct {
	MetricDescriptor *MetricDescriptor
}

// Record records the int64 histo value on the metrics recorder provided.
func (h Int64HistoHandle) Record(recorder MetricsRecorder, incr int64, labels ...string) {
	recorder.RecordIntHisto(h, incr, labels...)
}

// Float64HistoHandle is a typed handle for a float histogram metric. This
// handle is passed at the recording point in order to know which metric to
// record on.
type Float64HistoHandle struct {
	MetricDescriptor *MetricDescriptor
}

// Record records the float64 histo value on the metrics recorder provided.
func (h Float64HistoHandle) Record(recorder MetricsRecorder, incr float64, labels ...string) {
	recorder.RecordFloatHisto(h, incr, labels...)
}

// Int64GaugeHandle is a typed handle for an int gauge metric. This handle is
// passed at the recording point in order to know which metric to record on.
type Int64GaugeHandle struct {
	MetricDescriptor *MetricDescriptor
}

// Record records the int64 histo value on the metrics recorder provided.
func (h Int64GaugeHandle) Record(recorder MetricsRecorder, incr int64, labels ...string) {
	recorder.RecordIntGauge(h, incr, labels...)
}

// registeredMetrics are the registered metric descriptor names.
var registeredMetrics = make(map[stats.Metric]bool)

// MetricsRegistry are all of the registered metrics.
//
// This is written to only at init time, and read only after that.
var MetricsRegistry = make(map[stats.Metric]*MetricDescriptor)

func registerMetric(name stats.Metric, def bool) {
	if registeredMetrics[name] {
		log.Panicf("metric %v already registered", name)
	}
	registeredMetrics[name] = true
	if def {
		DefaultMetrics = DefaultMetrics.Add(name)
	}
}

// RegisterInt64Count registers the metric description onto the global registry.
// It returns a typed handle to use to recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple metrics are
// registered with the same name, this function will panic.
func RegisterInt64Count(descriptor MetricDescriptor) Int64CountHandle {
	registerMetric(descriptor.Name, descriptor.Default)
	descriptor.Type = MetricTypeIntCount
	handle := Int64CountHandle{
		MetricDescriptor: &descriptor,
	}
	MetricsRegistry[descriptor.Name] = &descriptor
	return handle
}

// RegisterFloat64Count registers the metric description onto the global
// registry. It returns a typed handle to use to recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple metrics are
// registered with the same name, this function will panic.
func RegisterFloat64Count(descriptor MetricDescriptor) Float64CountHandle {
	registerMetric(descriptor.Name, descriptor.Default)
	descriptor.Type = MetricTypeFloatCount
	handle := Float64CountHandle{
		MetricDescriptor: &descriptor,
	}
	MetricsRegistry[descriptor.Name] = &descriptor
	return handle
}

// RegisterInt64Histo registers the metric description onto the global registry.
// It returns a typed handle to use to recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple metrics are
// registered with the same name, this function will panic.
func RegisterInt64Histo(descriptor MetricDescriptor) Int64HistoHandle {
	registerMetric(descriptor.Name, descriptor.Default)
	descriptor.Type = MetricTypeIntHisto
	handle := Int64HistoHandle{
		MetricDescriptor: &descriptor,
	}
	MetricsRegistry[descriptor.Name] = &descriptor
	return handle
}

// RegisterFloat64Histo registers the metric description onto the global
// registry. It returns a typed handle to use to recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple metrics are
// registered with the same name, this function will panic.
func RegisterFloat64Histo(descriptor MetricDescriptor) Float64HistoHandle {
	registerMetric(descriptor.Name, descriptor.Default)
	descriptor.Type = MetricTypeFloatHisto
	handle := Float64HistoHandle{
		MetricDescriptor: &descriptor,
	}
	MetricsRegistry[descriptor.Name] = &descriptor
	return handle
}

// RegisterInt64Gauge registers the metric description onto the global registry.
// It returns a typed handle to use to recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple metrics are
// registered with the same name, this function will panic.
func RegisterInt64Gauge(descriptor MetricDescriptor) Int64GaugeHandle {
	registerMetric(descriptor.Name, descriptor.Default)
	descriptor.Type = MetricTypeIntGauge
	handle := Int64GaugeHandle{
		MetricDescriptor: &descriptor,
	}
	MetricsRegistry[descriptor.Name] = &descriptor
	return handle
}

// MetricType is the type of metric.
type MetricType int

const (
	MetricTypeIntCount MetricType = iota
	MetricTypeFloatCount
	MetricTypeIntHisto
	MetricTypeFloatHisto
	MetricTypeIntGauge
)

// clearMetricsRegistryForTesting clears the global data of the metrics
// registry. It returns a closure to be invoked that sets the metrics registry
// to its original state. Only called in testing functions.
func clearMetricsRegistryForTesting() func() {
	oldDefaultMetrics := DefaultMetrics
	oldRegisteredMetrics := registeredMetrics
	oldMetricsRegistry := MetricsRegistry

	DefaultMetrics = stats.NewMetrics()
	registeredMetrics = make(map[stats.Metric]bool)
	MetricsRegistry = make(map[stats.Metric]*MetricDescriptor)

	return func() {
		DefaultMetrics = oldDefaultMetrics
		registeredMetrics = oldRegisteredMetrics
		MetricsRegistry = oldMetricsRegistry
	}
}
