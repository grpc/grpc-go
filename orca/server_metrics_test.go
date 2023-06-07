/*
 *
 * Copyright 2023 gRPC authors.
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

package orca_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/orca"
)

func (s) TestServerMetrics_Setters(t *testing.T) {
	smr := orca.NewServerMetricsRecorder()

	smr.SetCPUUtilization(0.1)
	smr.SetMemoryUtilization(0.2)
	smr.SetApplicationUtilization(0.3)
	smr.SetQPS(0.4)
	smr.SetEPS(0.5)
	smr.SetNamedUtilization("x", 0.6)

	want := &orca.ServerMetrics{
		CPUUtilization: 0.1,
		MemUtilization: 0.2,
		AppUtilization: 0.3,
		QPS:            0.4,
		EPS:            0.5,
		Utilization:    map[string]float64{"x": 0.6},
		NamedMetrics:   map[string]float64{},
		RequestCost:    map[string]float64{},
	}

	got := smr.ServerMetrics()
	if d := cmp.Diff(got, want); d != "" {
		t.Fatalf("unexpected server metrics: -got +want: %v", d)
	}
}

func (s) TestServerMetrics_Deleters(t *testing.T) {
	smr := orca.NewServerMetricsRecorder()

	smr.SetCPUUtilization(0.1)
	smr.SetMemoryUtilization(0.2)
	smr.SetApplicationUtilization(0.3)
	smr.SetQPS(0.4)
	smr.SetEPS(0.5)
	smr.SetNamedUtilization("x", 0.6)
	smr.SetNamedUtilization("y", 0.7)

	// Now delete everything except named_utilization "y".
	smr.DeleteCPUUtilization()
	smr.DeleteMemoryUtilization()
	smr.DeleteApplicationUtilization()
	smr.DeleteQPS()
	smr.DeleteEPS()
	smr.DeleteNamedUtilization("x")

	want := &orca.ServerMetrics{
		CPUUtilization: -1,
		MemUtilization: -1,
		AppUtilization: -1,
		QPS:            -1,
		EPS:            -1,
		Utilization:    map[string]float64{"y": 0.7},
		NamedMetrics:   map[string]float64{},
		RequestCost:    map[string]float64{},
	}

	got := smr.ServerMetrics()
	if d := cmp.Diff(got, want); d != "" {
		t.Fatalf("unexpected server metrics: -got +want: %v", d)
	}
}

func (s) TestServerMetrics_Setters_Range(t *testing.T) {
	smr := orca.NewServerMetricsRecorder()

	smr.SetCPUUtilization(0.1)
	smr.SetMemoryUtilization(0.2)
	smr.SetApplicationUtilization(0.3)
	smr.SetQPS(0.4)
	smr.SetEPS(0.5)
	smr.SetNamedUtilization("x", 0.6)

	// Negatives for all these fields should be ignored.
	smr.SetCPUUtilization(-2)
	smr.SetMemoryUtilization(-3)
	smr.SetApplicationUtilization(-4)
	smr.SetQPS(-0.1)
	smr.SetEPS(-0.6)
	smr.SetNamedUtilization("x", -2)

	// Memory and named utilizations are max of 1.
	smr.SetMemoryUtilization(1.1)
	smr.SetNamedUtilization("x", 1.1)

	want := &orca.ServerMetrics{
		CPUUtilization: 0.1,
		MemUtilization: 0.2,
		AppUtilization: 0.3,
		QPS:            0.4,
		EPS:            0.5,
		Utilization:    map[string]float64{"x": 0.6},
		NamedMetrics:   map[string]float64{},
		RequestCost:    map[string]float64{},
	}

	got := smr.ServerMetrics()
	if d := cmp.Diff(got, want); d != "" {
		t.Fatalf("unexpected server metrics: -got +want: %v", d)
	}
}
