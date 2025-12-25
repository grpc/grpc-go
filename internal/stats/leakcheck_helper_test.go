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

package stats

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	estats "google.golang.org/grpc/experimental/stats"
	"google.golang.org/grpc/internal"
)

// Global tracker state
var (
	asyncReporterTracker *reporterTracker
	originalDelegate     RegisterAsyncReporterFuncType
)

// Logger is the interface that wraps the Logf and Errorf method. This allows
// to mimic the type of Logger that is used in Leakcheck and/or grpctest package.
type Logger interface {
	Logf(format string, args ...any)
	Errorf(format string, args ...any)
}

func wrappedRegisterAsyncReporter(l *MetricsRecorderList, r estats.AsyncMetricReporter, m ...estats.AsyncMetric) func() {
	// Register the location of this call
	token := asyncReporterTracker.register()
	// Safety check for the delegate
	if originalDelegate == nil {
		panic("leakcheck: original delegate is nil")
	}
	// Call the original logic to get the real cleanup function.
	realCleanup := originalDelegate(l, r, m...)
	// Return a wrapped cleanup that also unregisters the token
	return func() {
		if realCleanup != nil {
			realCleanup()
		}
		asyncReporterTracker.unregister(token)
	}
}

// trackAsyncReporters installs the tracking hook.
func trackAsyncReporters() {
	asyncReporterTracker = newReporterTracker()
	originalDelegate = setRegisterAsyncReporterDelegate(wrappedRegisterAsyncReporter)
}

// checkAsyncReporters verifies no leaks exist and restores the original delegate.
func checkAsyncReporters(log any) {
	logger, ok := log.(Logger)
	if !ok {
		return
	}
	// Restore the original delegate immediately to clean up state
	if originalDelegate != nil {
		setRegisterAsyncReporterDelegate(originalDelegate)
		originalDelegate = nil
	}

	if asyncReporterTracker == nil {
		return
	}

	leaks := asyncReporterTracker.leakedStackTraces()

	if len(leaks) > 0 {
		// Join all stack traces into one message
		allTraces := ""
		for _, trace := range leaks {
			allTraces += trace
		}
		logger.Errorf("Found %d leaked async reporters:%s", len(leaks), allTraces)
	}

	// Clean up global state
	asyncReporterTracker = nil
}

// reporterTracker encapsulates the state for tracking leaks.
type reporterTracker struct {
	mu          sync.Mutex
	allocations map[*int][]uintptr
}

// register records the stack trace.
func (rt *reporterTracker) register() *int {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	id := new(int)

	pcs := make([]uintptr, 32)
	n := runtime.Callers(4, pcs)
	rt.allocations[id] = pcs[:n]

	return id
}

// unregister removes the ID.
func (rt *reporterTracker) unregister(id *int) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.allocations, id)
}

func newReporterTracker() *reporterTracker {
	return &reporterTracker{
		allocations: make(map[*int][]uintptr),
	}
}

// getLeakedStackTraces returns formatted stack traces for all currently registered
// reporters. It handles locking internally.
func (rt *reporterTracker) leakedStackTraces() []string {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	var traces []string
	for _, pcs := range rt.allocations {
		frames := runtime.CallersFrames(pcs)
		var msg string
		msg += "\n--- Leaked Async Reporter Registration ---\n"
		for {
			frame, more := frames.Next()
			msg += fmt.Sprintf("%s\n\t%s:%d\n", frame.Function, frame.File, frame.Line)
			if !more {
				break
			}
		}
		traces = append(traces, msg)
	}
	return traces
}

// setRegisterAsyncReporterDelegate replaces the internal delegate with a new function.
// It returns the previous function so it can be restored or called by the wrapper.
func setRegisterAsyncReporterDelegate(newFunc RegisterAsyncReporterFuncType) RegisterAsyncReporterFuncType {
	oldFunc := registerAsyncReporterDelegate
	registerAsyncReporterDelegate = newFunc
	return oldFunc
}

func init() {
	internal.TrackAsyncReporters = trackAsyncReporters
	internal.CheckAsyncReporters = checkAsyncReporters
}

// mockLogger acts as a spy to capture errors from checkAsyncReporters
type mockLogger struct {
	failed  bool
	message string
}

func (m *mockLogger) Logf(string, ...any) {
	// No-op for info logs
}

func (m *mockLogger) Errorf(format string, args ...any) {
	m.failed = true
	m.message = fmt.Sprintf(format, args...)
}

func TestLeakChecker_DetectsLeak(t *testing.T) {
	savedDelegate := registerAsyncReporterDelegate
	defer func() { registerAsyncReporterDelegate = savedDelegate }()

	registerAsyncReporterDelegate = func(*MetricsRecorderList, estats.AsyncMetricReporter, ...estats.AsyncMetric) func() {
		return func() {} // no-op cleanup
	}
	trackAsyncReporters()

	_ = registerAsyncReporterDelegate(nil, nil)

	mock := &mockLogger{}
	checkAsyncReporters(mock)

	if !mock.failed {
		t.Error("Expected leak checker to report a leak, but it succeeded silently.")
	}
	if asyncReporterTracker != nil {
		t.Error("Expected checkAsyncReporters to cleanup global tracker, but it was not nil.")
	}
}

func TestLeakChecker_PassesOnCleanup(t *testing.T) {
	savedDelegate := registerAsyncReporterDelegate
	defer func() { registerAsyncReporterDelegate = savedDelegate }()

	registerAsyncReporterDelegate = func(*MetricsRecorderList, estats.AsyncMetricReporter, ...estats.AsyncMetric) func() {
		return func() {}
	}

	trackAsyncReporters()

	cleanup := registerAsyncReporterDelegate(nil, nil)
	cleanup() // <--- We call cleanup here, so no leak should exist.

	mock := &mockLogger{}
	checkAsyncReporters(mock)

	if mock.failed {
		t.Errorf("Expected no leaks, but got error: %s", mock.message)
	}
}
