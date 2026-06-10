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

	// mu protects data and consumed.
	mu sync.Mutex
	// data contains all recorded updates for each metric name in order.
	data map[string][]MetricsData
	// consumed tracks the number of updates consumed per metric name.
	consumed map[string]int
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

		data:     make(map[string][]MetricsData),
		consumed: make(map[string]int),
	}
}

// Metric returns the most recent data for a metric, and whether this recorder
// has received data for a metric.
func (r *TestMetricsRecorder) Metric(name string) (float64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	slice, ok := r.data[name]
	if !ok || len(slice) == 0 {
		return 0, false
	}
	data := slice[len(slice)-1]
	switch data.Handle.Type {
	case estats.MetricTypeIntCount, estats.MetricTypeIntGauge, estats.MetricTypeIntUpDownCount, estats.MetricTypeIntAsyncGauge:
		return float64(data.IntIncr), true
	case estats.MetricTypeFloatCount:
		return data.FloatIncr, true
	default:
		return 0, false
	}
}

// ClearMetrics clears the metrics data store of the test metrics recorder.
func (r *TestMetricsRecorder) ClearMetrics() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = make(map[string][]MetricsData)
	r.consumed = make(map[string]int)
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

func (r *TestMetricsRecorder) waitForMetric(ctx context.Context, ch *testutils.Channel, name string) (MetricsData, error) {
	for {
		r.mu.Lock()
		if slice, ok := r.data[name]; ok && len(slice) > r.consumed[name] {
			md := slice[r.consumed[name]]
			r.consumed[name]++
			r.mu.Unlock()
			return md, nil
		}
		r.mu.Unlock()

		_, err := ch.Receive(ctx)
		if err != nil {
			return MetricsData{}, err
		}
	}
}

// WaitForInt64Count waits for an int64 count metric and verifies that it
// matches want. Returns an error if failed to wait or received wrong data.
func (r *TestMetricsRecorder) WaitForInt64Count(ctx context.Context, want MetricsData) error {
	got, err := r.waitForMetric(ctx, r.intCountCh, want.Handle.Name)
	if err != nil {
		return fmt.Errorf("timeout waiting for int64Count")
	}
	if got.IntIncr != want.IntIncr {
		return fmt.Errorf("int64count metricsData received unexpected value: got %v, want %v", got.IntIncr, want.IntIncr)
	}
	if want.LabelKeys != nil {
		if diff := cmp.Diff(got.LabelKeys, want.LabelKeys); diff != "" {
			return fmt.Errorf("int64count metricsData received unexpected label keys: %v", diff)
		}
	}
	if want.LabelVals != nil {
		if diff := cmp.Diff(got.LabelVals, want.LabelVals); diff != "" {
			return fmt.Errorf("int64count metricsData received unexpected label values: %v", diff)
		}
	}
	return nil
}

// WaitForInt64UpDownCount waits for an int64 up-down count metric and
// verifies that it matches want. Returns an error if failed to wait or
// received wrong data.
func (r *TestMetricsRecorder) WaitForInt64UpDownCount(ctx context.Context, want MetricsData) error {
	got, err := r.waitForMetric(ctx, r.intUpDownCountCh, want.Handle.Name)
	if err != nil {
		return fmt.Errorf("timeout waiting for int64UpDownCount")
	}
	if got.IntIncr != want.IntIncr {
		return fmt.Errorf("int64UpDownCount metricsData received unexpected value: got %v, want %v", got.IntIncr, want.IntIncr)
	}
	if want.LabelKeys != nil {
		if diff := cmp.Diff(got.LabelKeys, want.LabelKeys); diff != "" {
			return fmt.Errorf("int64UpDownCount metricsData received unexpected label keys: %v", diff)
		}
	}
	if want.LabelVals != nil {
		if diff := cmp.Diff(got.LabelVals, want.LabelVals); diff != "" {
			return fmt.Errorf("int64UpDownCount metricsData received unexpected label values: %v", diff)
		}
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
	md := MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	}
	r.intCountCh.ReceiveOrFail()
	r.intCountCh.Send(md)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[handle.Name] = append(r.data[handle.Name], md)
}

// RecordInt64UpDownCount sends the metrics data to the intUpDownCountCh channel and updates
// the internal data map with the recorded value.
func (r *TestMetricsRecorder) RecordInt64UpDownCount(handle *estats.Int64UpDownCountHandle, incr int64, labels ...string) {
	md := MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	}
	r.intUpDownCountCh.ReceiveOrFail()
	r.intUpDownCountCh.Send(md)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[handle.Name] = append(r.data[handle.Name], md)
}

// WaitForFloat64Count waits for a float count metric and verifies that it
// matches want. Returns an error if failed to wait or received wrong data.
func (r *TestMetricsRecorder) WaitForFloat64Count(ctx context.Context, want MetricsData) error {
	got, err := r.waitForMetric(ctx, r.floatCountCh, want.Handle.Name)
	if err != nil {
		return fmt.Errorf("timeout waiting for float64Count")
	}
	if diff := cmp.Diff(got, want); diff != "" {
		return fmt.Errorf("float64count metricsData received unexpected value (-got, +want): %v", diff)
	}
	return nil
}

// RecordFloat64Count sends the metrics data to the floatCountCh channel and
// updates the internal data map with the recorded value.
func (r *TestMetricsRecorder) RecordFloat64Count(handle *estats.Float64CountHandle, incr float64, labels ...string) {
	md := MetricsData{
		Handle:    handle.Descriptor(),
		FloatIncr: incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	}
	r.floatCountCh.ReceiveOrFail()
	r.floatCountCh.Send(md)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[handle.Name] = append(r.data[handle.Name], md)
}

// WaitForInt64Histo waits for an int histo metric and verifies that it matches
// want. Returns an error if failed to wait or received wrong data.
func (r *TestMetricsRecorder) WaitForInt64Histo(ctx context.Context, want MetricsData) error {
	got, err := r.waitForMetric(ctx, r.intHistoCh, want.Handle.Name)
	if err != nil {
		return fmt.Errorf("timeout waiting for int64Histo")
	}
	if diff := cmp.Diff(got, want); diff != "" {
		return fmt.Errorf("int64Histo metricsData received unexpected value (-got, +want): %v", diff)
	}
	return nil
}

// RecordInt64Histo sends the metrics data to the intHistoCh channel and updates
// the internal data map with the recorded value.
func (r *TestMetricsRecorder) RecordInt64Histo(handle *estats.Int64HistoHandle, incr int64, labels ...string) {
	md := MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	}
	r.intHistoCh.ReceiveOrFail()
	r.intHistoCh.Send(md)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[handle.Name] = append(r.data[handle.Name], md)
}

// WaitForFloat64Histo waits for a float histo metric and verifies that it
// matches want. Returns an error if failed to wait or received wrong data.
func (r *TestMetricsRecorder) WaitForFloat64Histo(ctx context.Context, want MetricsData) error {
	got, err := r.waitForMetric(ctx, r.floatHistoCh, want.Handle.Name)
	if err != nil {
		return fmt.Errorf("timeout waiting for float64Histo")
	}
	if diff := cmp.Diff(got, want); diff != "" {
		return fmt.Errorf("float64Histo metricsData received unexpected value (-got, +want): %v", diff)
	}
	return nil
}

// RecordFloat64Histo sends the metrics data to the floatHistoCh channel and
// updates the internal data map with the recorded value.
func (r *TestMetricsRecorder) RecordFloat64Histo(handle *estats.Float64HistoHandle, incr float64, labels ...string) {
	md := MetricsData{
		Handle:    handle.Descriptor(),
		FloatIncr: incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	}
	r.floatHistoCh.ReceiveOrFail()
	r.floatHistoCh.Send(md)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[handle.Name] = append(r.data[handle.Name], md)
}

// WaitForInt64Gauge waits for an int gauge metric and verifies that it matches
// want.
func (r *TestMetricsRecorder) WaitForInt64Gauge(ctx context.Context, want MetricsData) error {
	got, err := r.waitForMetric(ctx, r.intGaugeCh, want.Handle.Name)
	if err != nil {
		return fmt.Errorf("timeout waiting for int64Gauge")
	}
	if diff := cmp.Diff(got, want); diff != "" {
		return fmt.Errorf("int64Gauge metricsData received unexpected value (-got, +want): %v", diff)
	}
	return nil
}

// RecordInt64Gauge sends the metrics data to the intGaugeCh channel and updates
// the internal data map with the recorded value.
func (r *TestMetricsRecorder) RecordInt64Gauge(handle *estats.Int64GaugeHandle, incr int64, labels ...string) {
	md := MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	}
	r.intGaugeCh.ReceiveOrFail()
	r.intGaugeCh.Send(md)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[handle.Name] = append(r.data[handle.Name], md)
}

// To implement a estats.AsyncMetricsRecorder, which allows it to be used in async metrics:

// RecordInt64AsyncGauge sends the metrics data to the intGaugeCh channel and updates
// the internal data map with the recorded value.
func (r *TestMetricsRecorder) RecordInt64AsyncGauge(handle *estats.Int64AsyncGaugeHandle, incr int64, labels ...string) {
	md := MetricsData{
		Handle:    handle.Descriptor(),
		IntIncr:   incr,
		LabelKeys: append(handle.Labels, handle.OptionalLabels...),
		LabelVals: labels,
	}
	r.intGaugeCh.ReceiveOrFail()
	r.intGaugeCh.Send(md)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[handle.Name] = append(r.data[handle.Name], md)
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
