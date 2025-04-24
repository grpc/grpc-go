/*
 *
 * Copyright 2025 gRPC authors.
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

package xdsclient

// MetricsReporter provides a way for XDSClient to register MetricHandle(s)
// and obtain a MetricsRecorder to record metrics.
type MetricsReporter interface {
	// RegisterMetric registers a metric instrument based on the
	// MetricDescriptor. It Returns a MetricHandle to be used with the
	// MetricsRecorder.
	RegisterMetric(descriptor MetricDescriptor) MetricHandle

	// Recorder returns the MetricsRecorder implementation associated with
	// this reporter. The returned recorder is used by XDSClient to record
	// values for MetricHandle(s) registered through this reporter.
	Recorder() MetricsRecorder
}

// MetricHandle is a metric instrument registered by the XDSClient. It enables
// MetricsRecorder implementation to associate values to be recorded with the
// correct MetricType.
type MetricHandle interface {
	// Descriptor returns the MetricDescriptor used for registration.
	Descriptor() MetricDescriptor
}

// MetricsRecorder records metrics for the XDSClient.
type MetricsRecorder interface {
	// Record processes a metric value for the instrument associated with
	// the MetricHandle.
	Record(handle MetricHandle, value any, labels ...string)
}

// MetricDescriptor is used by the XDSClient to provide the data for a
// registered MetricHandle that can be used by the MetricsRecorder.
type MetricDescriptor struct {
	// The name of this metric. This name must be unique.
	Name string
	// The description of this metric.
	Description string
	// The unit (e.g. entries, seconds) of this metric.
	Unit string
	// The required label keys for this metric.
	Labels []string
	// The type of metric.
	Type MetricType
	// Bounds are the bounds of this metric. This only applies to histogram
	// metrics. If unset or set with length 0, stats handlers will fall back to
	// default bounds.
	Bounds []float64
}

// MetricType is the type of metric.
type MetricType int

// Type of metric supported by XDSClient.
const (
	MetricTypeIntCount MetricType = iota
	MetricTypeFloatCount
	MetricTypeIntHisto
	MetricTypeFloatHisto
)
