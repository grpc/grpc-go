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
	"context"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	v3orcapb "github.com/cncf/xds/go/xds/data/orca/v3"
)

// metricRecorder is the base implementation of a metric recorder which is used
// by both call and out-of-band metric recorders.
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

func (m *metricRecorder) setRequestCost(name string, val float64) {
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

// SetAllUtilizationMetrics records a measurement for a utilization metric
// uniquely identifiable by name.
//
// A copy of the input map is made to ensure that any modifications to it does
// not interfere with the values stored in the recorder, and vice versa.
func (m *metricRecorder) SetAllUtilizationMetrics(kvs map[string]float64) {
	utils := make(map[string]float64, len(kvs))
	for k, v := range kvs {
		utils[k] = v
	}
	m.mu.Lock()
	m.utilization = utils
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

const trailerMetadataKey = "endpoint-load-metrics-bin"

// setTrailerMetadata adds a trailer metadata entry with key being set to
// `trailerMetadataKey` and value being set to the binary-encoded
// orca.OrcaLoadReport protobuf message.
func (m *metricRecorder) setTrailerMetadata(ctx context.Context) error {
	b, err := proto.Marshal(m.toLoadReportProto())
	if err != nil {
		return fmt.Errorf("failed to marshal load report: %v", err)
	}
	if err := grpc.SetTrailer(ctx, metadata.Pairs(trailerMetadataKey, string(b))); err != nil {
		return fmt.Errorf("failed to set trailer metadata: %v", err)
	}
	return nil
}
