/*
 *
 * Copyright 2017 gRPC authors.
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

package leakcheck

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/internal"
)

type testLogger struct {
	errorCount int
	errors     []string
}

func (e *testLogger) Logf(string, ...any) {
}

func (e *testLogger) Errorf(format string, args ...any) {
	e.errors = append(e.errors, fmt.Sprintf(format, args...))
	e.errorCount++
}

func TestCheck(t *testing.T) {
	const leakCount = 3
	for i := 0; i < leakCount; i++ {
		go func() { time.Sleep(2 * time.Second) }()
	}
	if ig := interestingGoroutines(); len(ig) == 0 {
		t.Error("blah")
	}
	e := &testLogger{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	CheckGoroutines(ctx, e)
	if e.errorCount != leakCount {
		t.Errorf("CheckGoroutines found %v leaks, want %v leaks", e.errorCount, leakCount)
		t.Logf("leaked goroutines:\n%v", strings.Join(e.errors, "\n"))
	}
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	CheckGoroutines(ctx, t)
}

func ignoredTestingLeak(d time.Duration) {
	time.Sleep(d)
}

func TestCheckRegisterIgnore(t *testing.T) {
	RegisterIgnoreGoroutine("ignoredTestingLeak")
	const leakCount = 3
	for i := 0; i < leakCount; i++ {
		go func() { time.Sleep(2 * time.Second) }()
	}
	go func() { ignoredTestingLeak(3 * time.Second) }()
	if ig := interestingGoroutines(); len(ig) == 0 {
		t.Error("blah")
	}
	e := &testLogger{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	CheckGoroutines(ctx, e)
	if e.errorCount != leakCount {
		t.Errorf("CheckGoroutines found %v leaks, want %v leaks", e.errorCount, leakCount)
		t.Logf("leaked goroutines:\n%v", strings.Join(e.errors, "\n"))
	}
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	CheckGoroutines(ctx, t)
}

// TestTrackTimers verifies that only leaked timers are reported and expired,
// stopped timers are ignored.
func TestTrackTimers(t *testing.T) {
	TrackTimers()
	const leakCount = 3
	for i := 0; i < leakCount; i++ {
		internal.TimeAfterFunc(2*time.Second, func() {
			t.Logf("Timer %d fired.", i)
		})
	}
	wg := sync.WaitGroup{}
	// Let a couple of timers expire.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		internal.TimeAfterFunc(time.Millisecond, func() {
			wg.Done()
		})
	}
	wg.Wait()

	// Stop a couple of timers.
	for i := 0; i < leakCount; i++ {
		t := internal.TimeAfterFunc(time.Hour, func() {
			t.Error("Timer fired before test ended.")
		})
		t.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	e := &testLogger{}
	CheckTimers(ctx, e)
	if e.errorCount != leakCount {
		t.Errorf("CheckTimers found %v leaks, want %v leaks", e.errorCount, leakCount)
		t.Logf("leaked timers:\n%v", strings.Join(e.errors, "\n"))
	}
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	CheckTimers(ctx, t)
}
