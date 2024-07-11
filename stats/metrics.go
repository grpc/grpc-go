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

import "maps"

// Metric is an identifier for a metric.
type Metric string

// Metrics is a set of metrics to record. Once created, Metrics is immutable,
// however Add and Remove can make copies with specific metrics added or
// removed, respectively.
//
// Do not construct directly; use NewMetrics instead.
type Metrics struct {
	// metrics are the set of metrics to initialize.
	metrics map[Metric]bool
}

// NewMetrics returns a Metrics containing Metrics.
func NewMetrics(metrics ...Metric) *Metrics {
	newMetrics := make(map[Metric]bool)
	for _, metric := range metrics {
		newMetrics[metric] = true
	}
	return &Metrics{
		metrics: newMetrics,
	}
}

// Metrics returns the metrics set.
func (m *Metrics) Metrics() map[Metric]bool {
	return m.metrics
}

// Add adds the metrics to the metrics set and returns a new copy with the
// additional metrics.
func (m *Metrics) Add(metrics ...Metric) *Metrics {
	newMetrics := make(map[Metric]bool)
	for metric := range m.metrics {
		newMetrics[metric] = true
	}

	for _, metric := range metrics {
		newMetrics[metric] = true
	}
	return &Metrics{
		metrics: newMetrics,
	}
}

// Join joins the metrics passed in with the metrics set, and returns a new copy
// with the merged metrics.
func (m *Metrics) Join(metrics *Metrics) *Metrics { // Use this API...
	newMetrics := make(map[Metric]bool)
	maps.Copy(newMetrics, m.metrics)
	maps.Copy(newMetrics, metrics.metrics)
	return &Metrics{
		metrics: newMetrics,
	}
}

// Remove removes the metrics from the metrics set and returns a new copy with
// the metrics removed.
func (m *Metrics) Remove(metrics ...Metric) *Metrics {
	newMetrics := make(map[Metric]bool)
	for metric := range m.metrics {
		newMetrics[metric] = true
	}

	for _, metric := range metrics {
		delete(newMetrics, metric)
	}
	return &Metrics{
		metrics: newMetrics,
	}
}
