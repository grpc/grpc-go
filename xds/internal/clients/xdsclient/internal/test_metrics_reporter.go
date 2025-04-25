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

// Package internal contains helpers for xdsclient.
package internal

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/xds/internal/clients/internal/testutils"
	"google.golang.org/grpc/xds/internal/clients/xdsclient"
)

// TestMetricsReporter is a MetricsReporter to be used in tests. It sends
// recording events on channels and provides helpers to check if certain events
// have taken place. It also persists metrics data keyed on the metrics
// type.
type TestMetricsReporter struct {
	intCountCh *testutils.Channel

	// mu protects data.
	mu sync.Mutex
	// data is the most recent update for each metric type.
	data map[string]float64
}

// NewTestMetricsReporter returns a new TestMetricsReporter.
func NewTestMetricsReporter() *TestMetricsReporter {
	return &TestMetricsReporter{
		intCountCh: testutils.NewChannelWithSize(10),

		data: make(map[string]float64),
	}
}

// Metric returns the most recent data for a metric, and whether this recorder
// has received data for a metric.
func (r *TestMetricsReporter) Metric(name string) (float64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, ok := r.data[name]
	return data, ok
}

// ClearMetrics clears the metrics data store of the test metrics recorder.
func (r *TestMetricsReporter) ClearMetrics() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = make(map[string]float64)
}

// MetricsData represents data associated with a metric.
type MetricsData struct {
	IntIncr int64    // Count to be incremented.
	Name    string   // Name of the metric.
	Labels  []string // Labels associated with the metric.
}

// WaitForInt64Count waits for an int64 count metric to be recorded and verifies
// that the recorded metrics data matches the expected metricsDataWant. Returns
// an error if failed to wait or received wrong data.
func (r *TestMetricsReporter) WaitForInt64Count(ctx context.Context, metricsDataWant MetricsData) error {
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
func (r *TestMetricsReporter) WaitForInt64CountIncr(ctx context.Context, incrWant int64) error {
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

// ReportMetric sends the metrics data to the intCountCh channel and updates
// the internal data map with the recorded value.
func (r *TestMetricsReporter) ReportMetric(m xdsclient.Metric) {
	r.intCountCh.ReceiveOrFail()

	var metricName string
	var incr int64

	switch metric := m.(type) {
	case xdsclient.MetricResourceUpdateValid:
		r.intCountCh.Send(MetricsData{
			IntIncr: metric.Incr,
			Name:    "xds_client.resource_updates_valid",
			Labels:  []string{metric.Target(), metric.ServerURI, metric.ResourceType},
		})
		metricName = "xds_client.resource_updates_valid"
		incr = metric.Incr

	case xdsclient.MetricResourceUpdateInvalid:
		r.intCountCh.Send(MetricsData{
			IntIncr: metric.Incr,
			Name:    "xds_client.resource_updates_invalid",
			Labels:  []string{metric.Target(), metric.ServerURI, metric.ResourceType},
		})
		metricName = "xds_client.resource_updates_invalid"
		incr = metric.Incr

	case xdsclient.MetricServerFailure:
		r.intCountCh.Send(MetricsData{
			IntIncr: metric.Incr,
			Name:    "xds_client.server_failure",
			Labels:  []string{metric.Target(), metric.ServerURI},
		})
		metricName = "xds_client.server_failure"
		incr = metric.Incr
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[metricName] = float64(incr)
}
