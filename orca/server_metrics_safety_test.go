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

// Safety / characterization tests for orca.ServerMetricsRecorder.
//
// These tests lock the concurrent invariants implied by the package-level
// contract documented at server_metrics.go:148-150:
//
//   // NewServerMetricsRecorder returns an in-memory store for ServerMetrics
//   // and allows for safe setting and retrieving of ServerMetrics.
//
// Go convention: "safe" without qualification means thread-safe for
// concurrent use. The natural deployment pattern for ORCA OOB load
// reporting is one sampler goroutine per metric family (CPU, Memory,
// Application, plus per-RPC SetNamedUtilization for backpressure
// signals), all writing concurrently to a single shared recorder.
//
// These tests must pass with -race both before and after any change to
// orca/server_metrics.go.

package orca

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestServerMetricsRecorderSafety_DistinctScalars_LastWriteSurvives asserts
// that when N writer goroutines each monotonically write distinct scalar
// fields, the final state reflects each writer's last value. This is the
// most basic invariant of a thread-safe recorder.
//
// pre-fix expectation: FAIL — atomic.Pointer load/modify/store is not
// transactional, so the last-storer overwrites every other concurrent
// writer's final update with a stale snapshot.
//
// post-fix expectation: PASS.
func (s) TestServerMetricsRecorderSafety_DistinctScalars_LastWriteSurvives(t *testing.T) {
	const iters = 5_000
	smr := NewServerMetricsRecorder()
	// Initialise everything to 0 so the final-value assertion has a
	// well-defined baseline (unset = -1 confuses the "max" comparison).
	smr.SetCPUUtilization(0)
	smr.SetMemoryUtilization(0)
	smr.SetApplicationUtilization(0)
	smr.SetQPS(0)
	smr.SetEPS(0)

	start := make(chan struct{})
	var wg sync.WaitGroup
	writer := func(set func(float64)) {
		defer wg.Done()
		<-start
		for i := 1; i <= iters; i++ {
			set(float64(i) / float64(iters)) // monotonic in (0, 1]
		}
	}
	wg.Add(5)
	go writer(smr.SetCPUUtilization)
	// SetMemoryUtilization range is [0,1], rest are val >= 0.
	go writer(smr.SetMemoryUtilization)
	go writer(smr.SetApplicationUtilization)
	go writer(smr.SetQPS)
	go writer(smr.SetEPS)

	close(start)
	wg.Wait()

	sm := smr.ServerMetrics()
	const want = 1.0
	for _, c := range []struct {
		name string
		got  float64
	}{
		{"CPUUtilization", sm.CPUUtilization},
		{"MemUtilization", sm.MemUtilization},
		{"AppUtilization", sm.AppUtilization},
		{"QPS", sm.QPS},
		{"EPS", sm.EPS},
	} {
		if c.got != want {
			t.Errorf("lost-update: %s = %.6f, want %.6f (writer's last value)", c.name, c.got, want)
		}
	}
}

// TestServerMetricsRecorderSafety_NamedUtilization_DistinctKeys_LastWriteSurvives
// asserts that concurrent SetNamedUtilization on distinct keys preserves
// every writer's last value. Each key is written by exactly one goroutine,
// so the per-key "last value" is well-defined.
//
// pre-fix expectation: FAIL — when one goroutine's load-modify-store overlaps
// another's, the later store overwrites the former's whole map, including
// the entry the former just added or just updated.
//
// post-fix expectation: PASS.
func (s) TestServerMetricsRecorderSafety_NamedUtilization_DistinctKeys_LastWriteSurvives(t *testing.T) {
	const (
		writers = 8
		iters   = 2_000
	)
	smr := NewServerMetricsRecorder()

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(writers)
	for w := 0; w < writers; w++ {
		key := keyFor(w)
		go func() {
			defer wg.Done()
			<-start
			for i := 1; i <= iters; i++ {
				smr.SetNamedUtilization(key, float64(i)/float64(iters))
			}
		}()
	}
	close(start)
	wg.Wait()

	sm := smr.ServerMetrics()
	for w := 0; w < writers; w++ {
		k := keyFor(w)
		v, ok := sm.Utilization[k]
		if !ok {
			t.Errorf("lost-update: NamedUtilization[%q] missing entirely", k)
			continue
		}
		if v != 1.0 {
			t.Errorf("lost-update: NamedUtilization[%q] = %.6f, want 1.0 (writer's last value)", k, v)
		}
	}
}

// TestServerMetricsRecorderSafety_Monotonicity_ReaderObservesNoRollback
// asserts that, given N writer goroutines each monotonically increasing
// distinct scalar fields, a concurrent reader of ServerMetrics() never
// observes any single field move backwards.
//
// This invariant is strictly stronger than the previous two: even if the
// final state happens to be correct, intermediate observations under
// lost-update show fields rolling back to older values when one writer's
// store carries a stale snapshot of other fields.
//
// pre-fix expectation: FAIL — thousands of rollbacks observed per second.
//
// post-fix expectation: PASS (0 rollbacks).
func (s) TestServerMetricsRecorderSafety_Monotonicity_ReaderObservesNoRollback(t *testing.T) {
	const iters = 10_000
	smr := NewServerMetricsRecorder()
	smr.SetCPUUtilization(0)
	smr.SetMemoryUtilization(0)
	smr.SetApplicationUtilization(0)

	stop := make(chan struct{})
	var rollbacks atomic.Int64
	var firstViolation atomic.Pointer[string]

	// Reader goroutine: polls and checks each scalar field never decreases.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		var maxCPU, maxMem, maxApp float64
		for {
			select {
			case <-stop:
				return
			default:
			}
			sm := smr.ServerMetrics()
			if sm.CPUUtilization < maxCPU {
				rollbacks.Add(1)
				recordFirstViolation(&firstViolation, "CPU", maxCPU, sm.CPUUtilization)
			} else {
				maxCPU = sm.CPUUtilization
			}
			if sm.MemUtilization < maxMem {
				rollbacks.Add(1)
				recordFirstViolation(&firstViolation, "Mem", maxMem, sm.MemUtilization)
			} else {
				maxMem = sm.MemUtilization
			}
			if sm.AppUtilization < maxApp {
				rollbacks.Add(1)
				recordFirstViolation(&firstViolation, "App", maxApp, sm.AppUtilization)
			} else {
				maxApp = sm.AppUtilization
			}
		}
	}()

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)
	for _, set := range []func(float64){
		smr.SetCPUUtilization,
		smr.SetMemoryUtilization,
		smr.SetApplicationUtilization,
	} {
		set := set
		go func() {
			defer wg.Done()
			<-start
			for i := 1; i <= iters; i++ {
				set(float64(i) / float64(iters))
			}
		}()
	}
	close(start)
	wg.Wait()
	close(stop)
	<-readerDone

	if n := rollbacks.Load(); n > 0 {
		msg := "<none captured>"
		if p := firstViolation.Load(); p != nil {
			msg = *p
		}
		t.Errorf("lost-update detected: %d monotonicity rollbacks observed; first violation: %s", n, msg)
	}
}

// --- helpers ---

func keyFor(i int) string {
	// Simple, stable key per writer; avoids fmt to keep the hot loop allocation-free.
	return string([]byte{'k', '0' + byte(i)})
}

func recordFirstViolation(p *atomic.Pointer[string], field string, max, observed float64) {
	if p.Load() != nil {
		return
	}
	s := field + ": max=" + ftoa(max) + " observed=" + ftoa(observed) + " at " + time.Now().Format(time.RFC3339Nano)
	p.CompareAndSwap(nil, &s)
}

func ftoa(f float64) string {
	// Avoid fmt import dance; precision sufficient for diagnostics.
	const prec = 1e9
	if f < 0 {
		return "-" + ftoa(-f)
	}
	whole := int64(f)
	frac := int64((f - float64(whole)) * prec)
	s := itoa(whole) + "."
	digits := itoa(frac)
	for len(digits) < 9 {
		digits = "0" + digits
	}
	return s + digits
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
