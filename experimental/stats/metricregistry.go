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

// DefaultMetrics are the default metrics registered through global instruments
// registry. This is written to at initialization time only, and is read
// only after initialization.
var DefaultMetrics = stats.NewMetrics()

// MetricDescriptor is the data for a registered metric.
type MetricDescriptor struct {
	// Name is the name of this metric.
	Name stats.Metric
	// Description is the description of this metric.
	Description string
	// Unit is the unit of this metric.
	Unit string
	// Labels are the required label keys for this metric.
	Labels []string
	// OptionalLabels are the optional label keys for this
	// metric.
	OptionalLabels []string
	// Default is whether this metric is on by default.
	Default bool
}

// Int64CountHandle is a typed handle for a float count instrument. This handle
// is passed at the recording point in order to know which instrument to record
// on.
type Int64CountHandle struct {
	MetricDescriptor *MetricDescriptor
}

// RecordInt64Count records the int64 count value on the metrics recorder
// provided.
func (h *Int64CountHandle) RecordInt64Count(recorder MetricsRecorder, labels []Label, optionalLabels []Label, incr int64) {
	recorder.RecordIntCount(*h, labels, optionalLabels, incr)
}

// Float64CountHandle is a typed handle for a float count instrument. This handle
// is passed at the recording point in order to know which instrument to record
// on.
type Float64CountHandle struct {
	MetricDescriptor *MetricDescriptor
}

// RecordFloat64Count records the float64 count value on the metrics recorder
// provided.
func (h *Float64CountHandle) RecordFloat64Count(recorder MetricsRecorder, labels []Label, optionalLabels []Label, incr float64) {
	recorder.RecordFloatCount(*h, labels, optionalLabels, incr)
}

// Int64HistoHandle is a typed handle for an int histogram instrument. This
// handle is passed at the recording point in order to know which instrument to
// record on.
type Int64HistoHandle struct {
	MetricDescriptor *MetricDescriptor
}

// RecordInt64Histo records the int64 histo value on the metrics recorder
// provided.
func (h *Int64HistoHandle) RecordInt64Histo(recorder MetricsRecorder, labels []Label, optionalLabels []Label, incr int64) {
	recorder.RecordIntHisto(*h, labels, optionalLabels, incr)
}

// Float64HistoHandle is a typed handle for a float histogram instrument. This
// handle is passed at the recording point in order to know which instrument to
// record on.
type Float64HistoHandle struct {
	MetricDescriptor *MetricDescriptor
}

// RecordFloat64Histo records the float64 histo value on the metrics recorder
// provided.
func (h *Float64HistoHandle) RecordFloat64Histo(recorder MetricsRecorder, labels []Label, optionalLabels []Label, incr float64) {
	recorder.RecordFloatHisto(*h, labels, optionalLabels, incr)
}

// Int64GaugeHandle is a typed handle for an int gauge instrument. This handle
// is passed at the recording point in order to know which instrument to record
// on.
type Int64GaugeHandle struct {
	MetricDescriptor *MetricDescriptor
}

// RecordInt64Histo records the int64 histo value on the metrics recorder
// provided.
func (h *Int64GaugeHandle) RecordInt64Gauge(recorder MetricsRecorder, labels []Label, optionalLabels []Label, incr int64) {
	recorder.RecordIntGauge(*h, labels, optionalLabels, incr)
}

// registeredInsts are the registered instrument descriptor names.
var registeredInsts = make(map[stats.Metric]bool)

// MetricsRegistry is all the registered metrics.
//
// This is written to only at init time, and read only after that.
var MetricsRegistry = make(map[stats.Metric]*MetricDescriptor)

func registerInst(name stats.Metric, def bool) {
	if registeredInsts[name] {
		log.Panicf("instrument %v already registered", name)
	}
	registeredInsts[name] = true
	if def {
		DefaultMetrics = DefaultMetrics.Add(name)
	}
}

// RegisterInt64Count registers the instrument description onto the global
// registry. It returns a typed handle to use to recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple instruments are
// registered with the same name, this function will panic.
func RegisterInt64Count(descriptor MetricDescriptor)  Int64CountHandle {
	registerInst(descriptor.Name, descriptor.Default)
	handle := Int64CountHandle{
		MetricDescriptor: &descriptor,
	}
	MetricsRegistry[descriptor.Name] = &descriptor
	return handle
}

// RegisterFloat64Count registers the instrument description onto the global
// registry. It returns a typed handle to use to recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple instruments are
// registered with the same name, this function will panic.
func RegisterFloat64Count(descriptor MetricDescriptor) Float64CountHandle {
	registerInst(descriptor.Name, descriptor.Default)
	handle := Float64CountHandle{
		MetricDescriptor: &descriptor,
	}
	MetricsRegistry[descriptor.Name] = &descriptor
	return handle
}

// RegisterInt64Histo registers the instrument description onto the global
// registry. It returns a typed handle to use to recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple instruments are
// registered with the same name, this function will panic.
func RegisterInt64Histo(descriptor MetricDescriptor) Int64HistoHandle {
	registerInst(descriptor.Name, descriptor.Default)
	handle := Int64HistoHandle{
		MetricDescriptor: &descriptor,
	}
	MetricsRegistry[descriptor.Name] = &descriptor
	return handle
}

// RegisterFloat64Histo registers the instrument description onto the global
// registry. It returns a typed handle to use to recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple instruments are
// registered with the same name, this function will panic.
func RegisterFloat64Histo(descriptor MetricDescriptor) Float64HistoHandle {
	registerInst(descriptor.Name, descriptor.Default)
	handle := Float64HistoHandle{
		MetricDescriptor: &descriptor,
	}
	MetricsRegistry[descriptor.Name] = &descriptor
	return handle
}

// RegisterInt64Gauge registers the instrument description onto the global
// registry. It returns a typed handle to use to recording data.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple instruments are
// registered with the same name, this function will panic.
func RegisterInt64Gauge(descriptor MetricDescriptor) Int64GaugeHandle {
	registerInst(descriptor.Name, descriptor.Default)
	handle := Int64GaugeHandle{
		MetricDescriptor: &descriptor,
	}
	MetricsRegistry[descriptor.Name] = &descriptor
	return handle
}

// Will take a list, write comments and rewrite tests/cleanup and then I think ready to send out...
// How do I even test this really?
// Internal only clear...I don't think it's worth it just for default set to do it in internal...

