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

package stats

import (
	estats "google.golang.org/grpc/experimental/stats"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/stats"
)

var logger = grpclog.Component("metrics-recorder-list")

// MetricRecorderList forwards Record calls to all of it's metricsRecorders.
//
// It eats any record calls where the label values provided do not match the
// number of label keys.
type MetricsRecorderList struct {
	// metricsRecorders are the metrics recorders this list will forward to.
	metricsRecorders []estats.MetricsRecorder
}

// NewMetricsRecorderList creates a new metric recorder list with all the stats
// handlers provided which implement the MetricsRecorder interface.
// If no stats handlers provided implement the MetricsRecorder interface,
// the MetricsRecorder list returned is a no-op.
func NewMetricsRecorderList(shs []stats.Handler) *MetricsRecorderList {
	var mrs []estats.MetricsRecorder
	for _, sh := range shs {
		if mr, ok := sh.(estats.MetricsRecorder); ok {
			mrs = append(mrs, mr)
		}
	}
	return &MetricsRecorderList{
		metricsRecorders: mrs,
	}
}

func (l *MetricsRecorderList) RecordInt64Count(handle *estats.Int64CountHandle, incr int64, labels ...string) {
	if got, want := len(handle.Labels)+len(handle.OptionalLabels), len(labels); got != want {
		logger.Infof("length of labels passed to RecordInt64Count incorrect got: %v, want: %v", got, want)
	}

	for _, metricRecorder := range l.metricsRecorders {
		metricRecorder.RecordInt64Count(handle, incr, labels...)
	}
}

func (l *MetricsRecorderList) RecordFloat64Count(handle *estats.Float64CountHandle, incr float64, labels ...string) {
	if got, want := len(handle.Labels)+len(handle.OptionalLabels), len(labels); got != want {
		logger.Infof("length of labels passed to RecordFloat64Count incorrect got: %v, want: %v", got, want)
	}

	for _, metricRecorder := range l.metricsRecorders {
		metricRecorder.RecordFloat64Count(handle, incr, labels...)
	}
}

func (l *MetricsRecorderList) RecordInt64Histo(handle *estats.Int64HistoHandle, incr int64, labels ...string) {
	if got, want := len(handle.Labels)+len(handle.OptionalLabels), len(labels); got != want {
		logger.Infof("length of labels passed to RecordInt64Histo incorrect got: %v, want: %v", got, want)
	}

	for _, metricRecorder := range l.metricsRecorders {
		metricRecorder.RecordInt64Histo(handle, incr, labels...)
	}
}

func (l *MetricsRecorderList) RecordFloat64Histo(handle *estats.Float64HistoHandle, incr float64, labels ...string) {
	if got, want := len(handle.Labels)+len(handle.OptionalLabels), len(labels); got != want {
		logger.Infof("length of labels passed to RecordFloat64Histo incorrect got: %v, want: %v", got, want)
	}

	for _, metricRecorder := range l.metricsRecorders {
		metricRecorder.RecordFloat64Histo(handle, incr, labels...)
	}
}

func (l *MetricsRecorderList) RecordInt64Gauge(handle *estats.Int64GaugeHandle, incr int64, labels ...string) {
	if got, want := len(handle.Labels)+len(handle.OptionalLabels), len(labels); got != want {
		logger.Infof("length of labels passed to RecordInt64Gauge incorrect got: %v, want: %v", got, want)
	}

	for _, metricRecorder := range l.metricsRecorders {
		metricRecorder.RecordInt64Gauge(handle, incr, labels...)
	}
}
