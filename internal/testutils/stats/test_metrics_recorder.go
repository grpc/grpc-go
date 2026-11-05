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
	estats.UnimplementedMetricsRecorder
	intCountCh       *testutils.Channel
	floatCountCh     *testutils.Channel
	intHistoCh       *testutils.Channel
	floatHistoCh     *testutils.Channel
	intGaugeCh       *testutils.Channel
	intUpDownCountCh *testutils.Channel

	// mu protects data and numRecords.
	mu sync.Mutex
	// data is the most recent update for each metric name.
	data map[string]*MetricsData
	// numRecords is the number of times each metric name has been recorded.
	numRecords map[string]int
}

// NewTestMetricsRecorder returns a new TestMetricsRecorder.
func NewTestMetricsRecorder() *TestMetricsRecorder {
	return &TestMetricsRecorder{
		intCountCh:       testutils.NewChannelWithSize(10),
		floatCountCh:     testutils.NewChannelWithSize(10),
		intHistoCh:       testutils.NewChannelWithSize(10),
		floatHistoCh:     testutils.NewChannelWithSize(10),
		intGaugeCh:       testutils.NewChannelWithSize(10),
		intUpDownCountCh: testutils.NewChannelWithSize(10),

		data:       make(map[string]*MetricsData),
		numRecords: make(map[string]int),
	}
}

// Metric returns the most recent data for a metric, and whether this recorder
// has received data for a metric.
func (r *TestMetricsRecorder) Metric(name string) (float64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	md, ok := r.data[name]
	if !ok {
		return 0, false
	}
	switch md.Handle.Type {
	case estats.MetricTypeFloatCount, estats.MetricTypeFloatHisto:
		return md.FloatIncr, true
	default:
		return float64(md.IntIncr), true
	}
}

// MetricsData returns the most recent data recorded for the named metric, or
// nil if none has been recorded.
func (r *TestMetricsRecorder) MetricsData(name string) *MetricsData {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data[name]
}

// NumRecords returns the number of times the named metric has been recorded.
func (r *TestMetricsRecorder) NumRecords(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.numRecords[name]
}

// ClearMetrics clears the metrics data store of the test metrics recorder.
func (r *TestMetricsRecorder) ClearMetrics() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = make(map[string]*MetricsData)
	r.numRecords = make(map[string]int)
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

// record sends md on ch, first dropping a value not yet received if there is
// one, stores md as the most recent data for its metric, and increments the
// metric's record count.
func (r *TestMetricsRecorder) record(ch *testutils.Channel, md MetricsData) {
	ch.ReceiveOrFail()
	ch.Send(md)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[md.Handle.Name] = &md
	r.numRecords[md.Handle.Name]++
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
	r.record(r.intCountCh, MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})
}

// RecordInt64UpDownCount sends the metrics data to the intUpDownCountCh channel and updates
// the internal data map with the recorded value.
func (r *TestMetricsRecorder) RecordInt64UpDownCount(handle *estats.Int64UpDownCountHandle, incr int64, labels ...string) {
	r.record(r.intUpDownCountCh, MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})
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
	r.record(r.floatCountCh, MetricsData{
		Handle:    handle.Descriptor(),
		FloatIncr: incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})
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
	r.record(r.intHistoCh, MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})
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
	r.record(r.floatHistoCh, MetricsData{
		Handle:    handle.Descriptor(),
		FloatIncr: incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})
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
	r.record(r.intGaugeCh, MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})
}

// To implement a estats.AsyncMetricsRecorder, which allows it to be used in async metrics:

// RecordInt64AsyncGauge sends the metrics data to the intGaugeCh channel and updates
// the internal data map with the recorded value.
func (r *TestMetricsRecorder) RecordInt64AsyncGauge(handle *estats.Int64AsyncGaugeHandle, incr int64, labels ...string) {
	r.record(r.intGaugeCh, MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	})
}

// ReadFloat64Histo waits for a float64 histogram metric to be recorded and returns its data.
func (r *TestMetricsRecorder) ReadFloat64Histo(ctx context.Context) (MetricsData, error) {
	got, err := r.floatHistoCh.Receive(ctx)
	if err != nil {
		return MetricsData{}, err
	}
	return got.(MetricsData), nil
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
