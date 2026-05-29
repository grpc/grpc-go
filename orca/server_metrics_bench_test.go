/*
 *
 * Copyright 2026 gRPC authors.
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

// Benchmarks for orca.ServerMetricsRecorder to compare the lock-free
// atomic.Pointer-only implementation (PR #6799) against the mutex-serialised
// writer implementation (PR-B1, lost-update fix).
//
// The benchmark surface covers the three workload shapes that matter:
//   1. ReaderOnly       — what OOB stream / smProvider.ServerMetrics() does
//                          (PR #6799's primary motivation; PR-B1 leaves this
//                          path lock-free, so we expect ~0 regression)
//   2. SingleWriter     — happy-path single sampler goroutine (PR #6799 saw
//                          this as the relevant write path)
//   3. ConcurrentWriters — N sampler goroutines (the workload PR #6799
//                          allowed lost-update to slip through; PR-B1 makes
//                          this correct at the cost of mutex contention)
//   4. ReadHeavyMixed   — 1 writer + N readers (the realistic ORCA OOB
//                          deployment: low write rate, many client OOB
//                          streams polling)

package orca

import (
	"sync/atomic"
	"testing"
)

// BenchmarkServerMetricsRecorder_ServerMetrics_ReaderOnly measures the
// reader path that ORCA's OOB stream invokes once per tick.
//
// Design intent: this must remain at parity with the pre-fix
// (atomic.Pointer-only) implementation — PR-B1's reader path does not
// acquire mu.
func BenchmarkServerMetricsRecorder_ServerMetrics_ReaderOnly(b *testing.B) {
	smr := NewServerMetricsRecorder()
	smr.SetCPUUtilization(0.5)
	smr.SetMemoryUtilization(0.4)
	smr.SetApplicationUtilization(0.3)
	smr.SetQPS(100)
	smr.SetEPS(1)
	smr.SetNamedUtilization("queue_depth", 0.2)
	smr.SetNamedUtilization("error_rate", 0.01)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = smr.ServerMetrics()
		}
	})
}

// BenchmarkServerMetricsRecorder_SetCPUUtilization_SingleWriter measures
// the single-writer happy path (one CPU sampler goroutine, no contention).
//
// PR-B1 adds an uncontended mutex acquire (~25ns) to this path. Expected
// regression is small in absolute terms vs the copyServerMetrics map
// allocations that dominate this path.
func BenchmarkServerMetricsRecorder_SetCPUUtilization_SingleWriter(b *testing.B) {
	smr := NewServerMetricsRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		smr.SetCPUUtilization(0.5)
	}
}

// BenchmarkServerMetricsRecorder_SetNamedUtilization_SingleWriter measures
// the map-write path (a per-RPC SetNamedUtilization call).
func BenchmarkServerMetricsRecorder_SetNamedUtilization_SingleWriter(b *testing.B) {
	smr := NewServerMetricsRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		smr.SetNamedUtilization("queue_depth", 0.5)
	}
}

// BenchmarkServerMetricsRecorder_ConcurrentWriters_DistinctScalars measures
// the N-writer-distinct-fields workload where lost-update was observed
// (CPU/Mem/App/QPS/EPS samplers).
//
// PR-B1 serialises these via mu. Pre-fix is "faster" but incorrect (loses
// updates). The right framing for this benchmark is "the cost of
// correctness", not "PR-B1 vs PR #6799 raw speed".
func BenchmarkServerMetricsRecorder_ConcurrentWriters_DistinctScalars(b *testing.B) {
	smr := NewServerMetricsRecorder()
	setters := []func(float64){
		smr.SetCPUUtilization,
		smr.SetMemoryUtilization,
		smr.SetApplicationUtilization,
		smr.SetQPS,
		smr.SetEPS,
	}
	// Pick which setter to call based on a per-goroutine index so distinct
	// goroutines hit distinct setter functions (matches the realistic
	// "one sampler per metric family" deployment).
	var idx atomic.Int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		setter := setters[idx.Add(1)%int64(len(setters))]
		for pb.Next() {
			setter(0.5)
		}
	})
}

// BenchmarkServerMetricsRecorder_ConcurrentWriters_NamedKeys measures the
// concurrent-distinct-key NamedUtilization workload (per-RPC handlers
// each writing their own custom metric).
func BenchmarkServerMetricsRecorder_ConcurrentWriters_NamedKeys(b *testing.B) {
	smr := NewServerMetricsRecorder()
	var idx atomic.Int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		// Each goroutine writes its own dedicated key.
		key := keyFor(int(idx.Add(1)) % 8)
		for pb.Next() {
			smr.SetNamedUtilization(key, 0.5)
		}
	})
}

// BenchmarkServerMetricsRecorder_ReadHeavyMixed_1Writer_NReaders measures
// the realistic ORCA OOB deployment shape: one slow writer (CPU sampler
// ticking at ~1-10 Hz logically; we hit it as fast as possible here to
// stress the reader path) plus many readers (each OOB stream's tick).
//
// PR-B1's design property: readers do not block on writers (lock-free
// reader path), so this benchmark should show parity for the reader path
// regardless of writer contention.
func BenchmarkServerMetricsRecorder_ReadHeavyMixed_1Writer_NReaders(b *testing.B) {
	smr := NewServerMetricsRecorder()
	smr.SetCPUUtilization(0.5)

	stop := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		v := 0.0
		for {
			select {
			case <-stop:
				return
			default:
			}
			v += 0.001
			if v > 1 {
				v = 0
			}
			smr.SetCPUUtilization(v)
		}
	}()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = smr.ServerMetrics()
		}
	})
	b.StopTimer()
	close(stop)
	<-writerDone
}
