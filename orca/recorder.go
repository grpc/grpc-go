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

// OutOfBandMetricRecorder provides the functionality to record and report
// metrics as required by the OpenRcaService implementation.
//
// Safe for concurrent use.
type OutOfBandMetricRecorder struct {
	mu          sync.RWMutex
	cpu         float64
	memory      float64
	utilization map[string]float64
}

// NewOutOfBandMetricRecorder creates a new OutOfBandMetricRecorder.
func NewOutOfBandMetricRecorder() *OutOfBandMetricRecorder {
	return &OutOfBandMetricRecorder{
		utilization: make(map[string]float64),
	}
}

// SetCPUUtilization records a measurement for the CPU utilization metric.
func (m *OutOfBandMetricRecorder) SetCPUUtilization(val float64) {
	m.mu.Lock()
	m.cpu = val
	m.mu.Unlock()
}

// DeleteCPUUtilization deletes a previously recorded measurement for the CPU
// utilization metric.
func (m *OutOfBandMetricRecorder) DeleteCPUUtilization() {
	m.mu.Lock()
	m.cpu = 0
	m.mu.Unlock()
}

// SetMemoryUtilization records a measurement for the memory utilization metric.
func (m *OutOfBandMetricRecorder) SetMemoryUtilization(val float64) {
	m.mu.Lock()
	m.memory = val
	m.mu.Unlock()
}

// DeleteMemoryUtilization deletes a previously recorded measurement for the
// memory utilization metric.
func (m *OutOfBandMetricRecorder) DeleteMemoryUtilization() {
	m.mu.Lock()
	m.memory = 0
	m.mu.Unlock()
}

// SetUtilization records a measurement for a utilization metric uniquely
// identifiable by name.
func (m *OutOfBandMetricRecorder) SetUtilization(name string, val float64) {
	m.mu.Lock()
	m.utilization[name] = val
	m.mu.Unlock()
}

// DeleteUtilization deletes any previously recorded measurement for a
// utilization metric uniquely identifiable by name.
func (m *OutOfBandMetricRecorder) DeleteUtilization(name string) {
	m.mu.Lock()
	delete(m.utilization, name)
	m.mu.Unlock()
}

// toLoadReportProto dumps the recorded measurements as an OrcaLoadReport proto.
func (m *OutOfBandMetricRecorder) toLoadReportProto() *v3orcapb.OrcaLoadReport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	util := make(map[string]float64, len(m.utilization))
	for k, v := range m.utilization {
		util[k] = v
	}
	return &v3orcapb.OrcaLoadReport{
		CpuUtilization: m.cpu,
		MemUtilization: m.memory,
		Utilization:    util,
	}
}
