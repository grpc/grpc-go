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
	"fmt"
	"sync"

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
	intCountCh   *testutils.Channel
	floatCountCh *testutils.Channel
	intHistoCh   *testutils.Channel
	floatHistoCh *testutils.Channel
	intGaugeCh   *testutils.Channel

	// mu protects data.
	mu sync.Mutex
	// data is the most recent update for each metric name.
	data map[string]float64
}

// NewTestMetricsRecorder returns a new TestMetricsRecorder.
func NewTestMetricsRecorder() *TestMetricsRecorder {
	return &TestMetricsRecorder{
		intCountCh:   testutils.NewChannelWithSize(10),
		floatCountCh: testutils.NewChannelWithSize(10),
		intHistoCh:   testutils.NewChannelWithSize(10),
		floatHistoCh: testutils.NewChannelWithSize(10),
		intGaugeCh:   testutils.NewChannelWithSize(10),

		data: make(map[string]float64),
	}
}

// Metric returns the most recent data for a metric, and whether this recorder
// has received data for a metric.
func (r *TestMetricsRecorder) Metric(name string) (float64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, ok := r.data[name]
	return data, ok
}

// ClearMetrics clears the metrics data store of the test metrics recorder.
func (r *TestMetricsRecorder) ClearMetrics() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = make(map[string]float64)
}

// MetricsData represents data associated with a metric.
type MetricsData struct {
	Handle *estats.MetricDescriptor

	// Only set based on the type of metric. So only one of IntIncr or FloatIncr
	// is set.
	IntIncr   int64
	FloatIncr float64

	LabelKeys []string
	LabelVals []string
}

// WaitForInt64Count waits for an int64 count metric to be recorded and verifies
// that the recorded metrics data matches the expected metricsDataWant. Returns
// an error if failed to wait or received wrong data.
func (r *TestMetricsRecorder) WaitForInt64Count(ctx context.Context, metricsDataWant MetricsData) error {
	got, err := r.intCountCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout waiting for int64Count")
	}
	metricsDataGot := got.(MetricsData)
	if diff := cmp.Diff(metricsDataGot, metricsDataWant); diff != "" {
		return fmt.Errorf("int64count metricsData received unexpected value (-got, +want): %v", diff)
	}
	return nil
}

// WaitForInt64CountIncr waits for an int64 count metric to be recorded and
// verifies that the recorded metrics data incr matches the expected incr.
// Returns an error if failed to wait or received wrong data.
func (r *TestMetricsRecorder) WaitForInt64CountIncr(ctx context.Context, incrWant int64) error {
	got, err := r.intCountCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout waiting for int64Count")
	}
	metricsDataGot := got.(MetricsData)
	if diff := cmp.Diff(metricsDataGot.IntIncr, incrWant); diff != "" {
		return fmt.Errorf("int64count metricsData received unexpected value (-got, +want): %v", diff)
	}
	return nil
}

// RecordInt64Count sends the metrics data to the intCountCh channel and updates
// the internal data map with the recorded value.
func (r *TestMetricsRecorder) RecordInt64Count(handle *estats.Int64CountHandle, incr int64, labels ...string) {
	r.intCountCh.ReceiveOrFail()
	r.intCountCh.Send(MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})

	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[handle.Name] = float64(incr)
}

// WaitForFloat64Count waits for a float count metric to be recorded and
// verifies that the recorded metrics data matches the expected metricsDataWant.
// Returns an error if failed to wait or received wrong data.
func (r *TestMetricsRecorder) WaitForFloat64Count(ctx context.Context, metricsDataWant MetricsData) error {
	got, err := r.floatCountCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout waiting for float64Count")
	}
	metricsDataGot := got.(MetricsData)
	if diff := cmp.Diff(metricsDataGot, metricsDataWant); diff != "" {
		return fmt.Errorf("float64count metricsData received unexpected value (-got, +want): %v", diff)
	}
	return nil
}

// RecordFloat64Count sends the metrics data to the floatCountCh channel and
// updates the internal data map with the recorded value.
func (r *TestMetricsRecorder) RecordFloat64Count(handle *estats.Float64CountHandle, incr float64, labels ...string) {
	r.floatCountCh.ReceiveOrFail()
	r.floatCountCh.Send(MetricsData{
		Handle:    handle.Descriptor(),
		FloatIncr: incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})

	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[handle.Name] = incr
}

// WaitForInt64Histo waits for an int histo metric to be recorded and verifies
// that the recorded metrics data matches the expected metricsDataWant. Returns
// an error if failed to wait or received wrong data.
func (r *TestMetricsRecorder) WaitForInt64Histo(ctx context.Context, metricsDataWant MetricsData) error {
	got, err := r.intHistoCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout waiting for int64Histo")
	}
	metricsDataGot := got.(MetricsData)
	if diff := cmp.Diff(metricsDataGot, metricsDataWant); diff != "" {
		return fmt.Errorf("int64Histo metricsData received unexpected value (-got, +want): %v", diff)
	}
	return nil
}

// RecordInt64Histo sends the metrics data to the intHistoCh channel and updates
// the internal data map with the recorded value.
func (r *TestMetricsRecorder) RecordInt64Histo(handle *estats.Int64HistoHandle, incr int64, labels ...string) {
	r.intHistoCh.ReceiveOrFail()
	r.intHistoCh.Send(MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})

	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[handle.Name] = float64(incr)
}

// WaitForFloat64Histo waits for a float histo metric to be recorded and
// verifies that the recorded metrics data matches the expected metricsDataWant.
// Returns an error if failed to wait or received wrong data.
func (r *TestMetricsRecorder) WaitForFloat64Histo(ctx context.Context, metricsDataWant MetricsData) error {
	got, err := r.floatHistoCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout waiting for float64Histo")
	}
	metricsDataGot := got.(MetricsData)
	if diff := cmp.Diff(metricsDataGot, metricsDataWant); diff != "" {
		return fmt.Errorf("float64Histo metricsData received unexpected value (-got, +want): %v", diff)
	}
	return nil
}

// RecordFloat64Histo sends the metrics data to the floatHistoCh channel and
// updates the internal data map with the recorded value.
func (r *TestMetricsRecorder) RecordFloat64Histo(handle *estats.Float64HistoHandle, incr float64, labels ...string) {
	r.floatHistoCh.ReceiveOrFail()
	r.floatHistoCh.Send(MetricsData{
		Handle:    handle.Descriptor(),
		FloatIncr: incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})

	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[handle.Name] = incr
}

// WaitForInt64Gauge waits for a int gauge metric to be recorded and verifies
// that the recorded metrics data matches the expected metricsDataWant.
func (r *TestMetricsRecorder) WaitForInt64Gauge(ctx context.Context, metricsDataWant MetricsData) error {
	got, err := r.intGaugeCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout waiting for int64Gauge")
	}
	metricsDataGot := got.(MetricsData)
	if diff := cmp.Diff(metricsDataGot, metricsDataWant); diff != "" {
		return fmt.Errorf("int64Gauge metricsData received unexpected value (-got, +want): %v", diff)
	}
	return nil
}

// RecordInt64Gauge sends the metrics data to the intGaugeCh channel and updates
// the internal data map with the recorded value.
func (r *TestMetricsRecorder) RecordInt64Gauge(handle *estats.Int64GaugeHandle, incr int64, labels ...string) {
	r.intGaugeCh.ReceiveOrFail()
	r.intGaugeCh.Send(MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})

	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[handle.Name] = float64(incr)
}

// To implement a stats.Handler, which allows it to be set as a dial option:

// TagRPC is TestMetricsRecorder's implementation of TagRPC.
func (r *TestMetricsRecorder) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return ctx
}

// HandleRPC is TestMetricsRecorder's implementation of HandleRPC.
func (r *TestMetricsRecorder) HandleRPC(context.Context, stats.RPCStats) {}

// TagConn is TestMetricsRecorder's implementation of TagConn.
func (r *TestMetricsRecorder) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

// HandleConn is TestMetricsRecorder's implementation of HandleConn.
func (r *TestMetricsRecorder) HandleConn(context.Context, stats.ConnStats) {}

// NoopMetricsRecorder is a noop MetricsRecorder to be used in tests to prevent
// nil panics.
type NoopMetricsRecorder struct{}

// RecordInt64Count is a noop implementation of RecordInt64Count.
func (r *NoopMetricsRecorder) RecordInt64Count(*estats.Int64CountHandle, int64, ...string) {}

// RecordFloat64Count is a noop implementation of RecordFloat64Count.
func (r *NoopMetricsRecorder) RecordFloat64Count(*estats.Float64CountHandle, float64, ...string) {}

// RecordInt64Histo is a noop implementation of RecordInt64Histo.
func (r *NoopMetricsRecorder) RecordInt64Histo(*estats.Int64HistoHandle, int64, ...string) {}

// RecordFloat64Histo is a noop implementation of RecordFloat64Histo.
func (r *NoopMetricsRecorder) RecordFloat64Histo(*estats.Float64HistoHandle, float64, ...string) {}

// RecordInt64Gauge is a noop implementation of RecordInt64Gauge.
func (r *NoopMetricsRecorder) RecordInt64Gauge(*estats.Int64GaugeHandle, int64, ...string) {}
