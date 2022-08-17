/*
 *
 * Copyright 2022 gRPC authors.
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

package orca

import (
	"sync"

	v3orcapb "github.com/cncf/xds/go/xds/data/orca/v3"
)

// metricRecorder provides the functionality to record and report metrics, and
// is used by both call and out-of-band metric recorders.
//
// Safe for concurrent use.
type metricRecorder struct {
	mu          sync.RWMutex
	cpu         float64
	memory      float64
	requestCost map[string]float64
	utilization map[string]float64
}

func newMetricRecorder() *metricRecorder {
	return &metricRecorder{
		requestCost: make(map[string]float64),
		utilization: make(map[string]float64),
	}
}

// SetCPUUtilizationMetric records a measurement for the CPU utilization metric.
func (m *metricRecorder) SetCPUUtilizationMetric(val float64) {
	m.mu.Lock()
	m.cpu = val
	m.mu.Unlock()
}

// DeleteCPUUtilizationMetric deletes a previously recorded measurement for the
// CPU utilization metric.
func (m *metricRecorder) DeleteCPUUtilizationMetric() {
	m.mu.Lock()
	m.cpu = 0
	m.mu.Unlock()
}

// SetMemoryUtilizationMetric records a measurement for the memory utilization
// metric.
func (m *metricRecorder) SetMemoryUtilizationMetric(val float64) {
	m.mu.Lock()
	m.memory = val
	m.mu.Unlock()
}

// DeleteMemoryUtilizationMetric deletes a previously recorded measurement for
// the memory utilization metric.
func (m *metricRecorder) DeleteMemoryUtilizationMetric() {
	m.mu.Lock()
	m.memory = 0
	m.mu.Unlock()
}

func (m *metricRecorder) SetRequestCostMetric(name string, val float64) {
	m.mu.Lock()
	m.requestCost[name] = val
	m.mu.Unlock()
}

// SetUtilizationMetric records a measurement for a utilization metric uniquely
// identifiable by name.
func (m *metricRecorder) SetUtilizationMetric(name string, val float64) {
	m.mu.Lock()
	m.utilization[name] = val
	m.mu.Unlock()
}

// DeleteUtilizationMetric deletes any previously recorded measurement for a
// utilization metric uniquely identifiable by name.
func (m *metricRecorder) DeleteUtilizationMetric(name string) {
	m.mu.Lock()
	delete(m.utilization, name)
	m.mu.Unlock()
}

// toLoadReportProto dumps the recorded measurements as an OrcaLoadReport proto.
func (m *metricRecorder) toLoadReportProto() *v3orcapb.OrcaLoadReport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cost := make(map[string]float64, len(m.requestCost))
	for k, v := range m.requestCost {
		cost[k] = v
	}
	util := make(map[string]float64, len(m.utilization))
	for k, v := range m.utilization {
		util[k] = v
	}
	return &v3orcapb.OrcaLoadReport{
		CpuUtilization: m.cpu,
		MemUtilization: m.memory,
		RequestCost:    cost,
		Utilization:    util,
	}
}
