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

// Package stats implements a TestMetricsRecorder utility.
package stats

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	estats "google.golang.org/grpc/experimental/stats"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/stats"
)

// TestMetricsRecorder is a MetricsRecorder to be used in tests. It sends
// recording events on channels and provides helpers to check if certain events
// have taken place. It also persists metrics data keyed on the metrics
// descriptor.
type TestMetricsRecorder struct {
	t *testing.T

	intCountCh   *testutils.Channel
	floatCountCh *testutils.Channel
	intHistoCh   *testutils.Channel
	floatHistoCh *testutils.Channel
	intGaugeCh   *testutils.Channel
}

func NewTestMetricsRecorder(t *testing.T, metrics []string) *TestMetricsRecorder {
	return &TestMetricsRecorder{
		t: t,

		intCountCh:   testutils.NewChannelWithSize(10),
		floatCountCh: testutils.NewChannelWithSize(10),
		intHistoCh:   testutils.NewChannelWithSize(10),
		floatHistoCh: testutils.NewChannelWithSize(10),
		intGaugeCh:   testutils.NewChannelWithSize(10),
	}
}

type MetricsData struct {
	Handle *estats.MetricDescriptor

	// Only set based on the type of metric. So only one of IntIncr or FloatIncr
	// is set.
	IntIncr   int64
	FloatIncr float64

	LabelKeys []string
	LabelVals []string
}

func (r *TestMetricsRecorder) WaitForInt64Count(ctx context.Context, metricsDataWant MetricsData) {
	got, err := r.intCountCh.Receive(ctx)
	if err != nil {
		r.t.Fatalf("timeout waiting for int64Count")
	}
	metricsDataGot := got.(MetricsData)
	if diff := cmp.Diff(metricsDataGot, metricsDataWant); diff != "" {
		r.t.Fatalf("int64count metricsData received unexpected value (-got, +want): %v", diff)
	}
}

func (r *TestMetricsRecorder) RecordInt64Count(handle *estats.Int64CountHandle, incr int64, labels ...string) {
	r.intCountCh.Send(MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})
}

func (r *TestMetricsRecorder) WaitForFloat64Count(ctx context.Context, metricsDataWant MetricsData) {
	got, err := r.floatCountCh.Receive(ctx)
	if err != nil {
		r.t.Fatalf("timeout waiting for float64Count")
	}
	metricsDataGot := got.(MetricsData)
	if diff := cmp.Diff(metricsDataGot, metricsDataWant); diff != "" {
		r.t.Fatalf("float64count metricsData received unexpected value (-got, +want): %v", diff)
	}
}

func (r *TestMetricsRecorder) RecordFloat64Count(handle *estats.Float64CountHandle, incr float64, labels ...string) {
	r.floatCountCh.Send(MetricsData{
		Handle:    handle.Descriptor(),
		FloatIncr: incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})
}

func (r *TestMetricsRecorder) WaitForInt64Histo(ctx context.Context, metricsDataWant MetricsData) {
	got, err := r.intHistoCh.Receive(ctx)
	if err != nil {
		r.t.Fatalf("timeout waiting for int64Histo")
	}
	metricsDataGot := got.(MetricsData)
	if diff := cmp.Diff(metricsDataGot, metricsDataWant); diff != "" {
		r.t.Fatalf("int64Histo metricsData received unexpected value (-got, +want): %v", diff)
	}
}

func (r *TestMetricsRecorder) RecordInt64Histo(handle *estats.Int64HistoHandle, incr int64, labels ...string) {
	r.intHistoCh.Send(MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})
}

func (r *TestMetricsRecorder) WaitForFloat64Histo(ctx context.Context, metricsDataWant MetricsData) {
	got, err := r.floatHistoCh.Receive(ctx)
	if err != nil {
		r.t.Fatalf("timeout waiting for float64Histo")
	}
	metricsDataGot := got.(MetricsData)
	if diff := cmp.Diff(metricsDataGot, metricsDataWant); diff != "" {
		r.t.Fatalf("float64Histo metricsData received unexpected value (-got, +want): %v", diff)
	}
}

func (r *TestMetricsRecorder) RecordFloat64Histo(handle *estats.Float64HistoHandle, incr float64, labels ...string) {
	r.floatHistoCh.Send(MetricsData{
		Handle:    handle.Descriptor(),
		FloatIncr: incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})
}

func (r *TestMetricsRecorder) WaitForInt64Gauge(ctx context.Context, metricsDataWant MetricsData) {
	got, err := r.intGaugeCh.Receive(ctx)
	if err != nil {
		r.t.Fatalf("timeout waiting for int64Gauge")
	}
	metricsDataGot := got.(MetricsData)
	if diff := cmp.Diff(metricsDataGot, metricsDataWant); diff != "" {
		r.t.Fatalf("int64Gauge metricsData received unexpected value (-got, +want): %v", diff)
	}
}

func (r *TestMetricsRecorder) RecordInt64Gauge(handle *estats.Int64GaugeHandle, incr int64, labels ...string) {
	r.intGaugeCh.Send(MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})
}

// To implement a stats.Handler, which allows it to be set as a dial option:

func (r *TestMetricsRecorder) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return ctx
}

func (r *TestMetricsRecorder) HandleRPC(context.Context, stats.RPCStats) {}

func (r *TestMetricsRecorder) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (r *TestMetricsRecorder) HandleConn(context.Context, stats.ConnStats) {}
