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
	"sync"
	"sync/atomic"

	v3orcapb "github.com/cncf/xds/go/xds/data/orca/v3"
)

// CallMetricRecorder provides functionality to record per-RPC custom backend
// metrics. See CallMetricsServerOption() for more details.
//
// Safe for concurrent use.
type CallMetricRecorder struct {
	cpu    atomic.Value // float64
	memory atomic.Value // float64

	mu          sync.RWMutex
	requestCost map[string]float64
	utilization map[string]float64
}

func newCallMetricRecorder() *CallMetricRecorder {
	return &CallMetricRecorder{
		requestCost: make(map[string]float64),
		utilization: make(map[string]float64),
	}
}

// SetCPUUtilization records a measurement for the CPU utilization metric.
func (c *CallMetricRecorder) SetCPUUtilization(val float64) {
	c.cpu.Store(val)
}

// SetMemoryUtilization records a measurement for the memory utilization metric.
func (c *CallMetricRecorder) SetMemoryUtilization(val float64) {
	c.memory.Store(val)
}

// SetRequestCost records a measurement for a request cost metric,
// uniquely identifiable by name.
func (c *CallMetricRecorder) SetRequestCost(name string, val float64) {
	c.mu.Lock()
	c.requestCost[name] = val
	c.mu.Unlock()
}

// SetUtilization records a measurement for a utilization metric uniquely
// identifiable by name.
func (c *CallMetricRecorder) SetUtilization(name string, val float64) {
	c.mu.Lock()
	c.utilization[name] = val
	c.mu.Unlock()
}

// toLoadReportProto dumps the recorded measurements as an OrcaLoadReport proto.
func (c *CallMetricRecorder) toLoadReportProto() *v3orcapb.OrcaLoadReport {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cost := make(map[string]float64, len(c.requestCost))
	for k, v := range c.requestCost {
		cost[k] = v
	}
	util := make(map[string]float64, len(c.utilization))
	for k, v := range c.utilization {
		util[k] = v
	}
	cpu, _ := c.cpu.Load().(float64)
	mem, _ := c.memory.Load().(float64)
	return &v3orcapb.OrcaLoadReport{
		CpuUtilization: cpu,
		MemUtilization: mem,
		RequestCost:    cost,
		Utilization:    util,
	}
}

type callMetricRecorderCtxKey struct{}

// CallMetricRecorderFromContext returns the RPC specific custom metrics
// recorder [CallMetricRecorder] embedded in the provided RPC context.
//
// Returns nil if no custom metrics recorder is found in the provided context,
// which will be the case when custom metrics reporting is not enabled.
func CallMetricRecorderFromContext(ctx context.Context) *CallMetricRecorder {
	rw, ok := ctx.Value(callMetricRecorderCtxKey{}).(*recorderWrapper)
	if !ok {
		return nil
	}
	return rw.recorder()
}

func newContextWithRecorderWrapper(ctx context.Context, r *recorderWrapper) context.Context {
	return context.WithValue(ctx, callMetricRecorderCtxKey{}, r)
}

// recorderWrapper is a wrapper around a CallMetricRecorder to ensures that
// concurrent calls to CallMetricRecorderFromContext() results in only one
// allocation of the underlying metric recorder.
type recorderWrapper struct {
	once sync.Once
	r    *CallMetricRecorder
}

func (rw *recorderWrapper) recorder() *CallMetricRecorder {
	rw.once.Do(func() {
		rw.r = newCallMetricRecorder()
	})
	return rw.r
}
