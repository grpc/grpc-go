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

package grpcsync

import (
	"sync"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc/internal/grpctest"
)

// Test verifies the scenario where a RefCounted instance is created and its
// reference count is decremented. It verifies that the encapsulated value is
// correctly retrieved and that the onZero callback is invoked when the
// reference count drops to zero.
func (s) TestRefCounted(t *testing.T) {
	const val = "test-value"
	var onZeroCalled atomic.Bool

	rc, err := NewRefCounted(val, func() {
		onZeroCalled.Store(true)
	})
	if err != nil {
		t.Fatalf("NewRefCounted() failed: %v", err)
	}

	if got := rc.Value(); got != val {
		t.Fatalf("Value() = %v, want %v", got, val)
	}

	if got := onZeroCalled.Load(); got {
		t.Fatalf("Before decrement, onZeroCalled = %v, want false", got)
	}

	rc.Decrement()

	if got := onZeroCalled.Load(); !got {
		t.Fatalf("After decrementing count to zero, onZeroCalled = %v, want true", got)
	}
}

// Test verifies the scenario where the reference count of a resource is
// explicitly incremented and decremented multiple times. It verifies that the
// onZero callback is only executed when the count drops to zero, and not
// beforehand.
func (s) TestRefCounted_IncrementDecrement(t *testing.T) {
	const val = 42
	var onZeroCount atomic.Int32

	rc, err := NewRefCounted(val, func() {
		onZeroCount.Add(1)
	})
	if err != nil {
		t.Fatalf("NewRefCounted() failed: %v", err)
	}

	rc.Increment()
	rc.Decrement()

	if got := onZeroCount.Load(); got != 0 {
		t.Fatal("OnZero called unexpectedly after decrementing refcount from 2 to 1")
	}

	rc.Decrement()

	if got := onZeroCount.Load(); got != 1 {
		t.Fatalf("After decrementing count from 1 to 0, onZero called = %v times, want 1", got)
	}
}

// Test verifies the scenario where TryIncrement is called on both an active and
// a dead resource. It verifies that TryIncrement successfully increments active
// resources but fails on resources whose reference count has already dropped to
// zero.
func (s) TestRefCounted_TryIncrement(t *testing.T) {
	var onZeroCount atomic.Int32
	rc, err := NewRefCounted("val", func() {
		onZeroCount.Add(1)
	})
	if err != nil {
		t.Fatalf("NewRefCounted() failed: %v", err)
	}

	if got := rc.TryIncrement(); !got {
		t.Fatalf("TryIncrement() on active resource = %v, want true", got)
	}

	rc.Decrement()
	rc.Decrement()

	if got := onZeroCount.Load(); got != 1 {
		t.Fatalf("After decrementing count to zero, onZero called = %v times, want 1", got)
	}

	if got := rc.TryIncrement(); got {
		t.Fatalf("TryIncrement() on dead resource = %v, want false", got)
	}
}

// Test verifies the scenario where multiple goroutines concurrently increment
// and decrement the reference count. It verifies that the reference counting is
// thread-safe and the onZero callback is invoked exactly once.
func (s) TestRefCounted_Concurrent(t *testing.T) {
	const numGoroutines = 100
	var onZeroCount atomic.Int32

	rc, err := NewRefCounted("concurrent-val", func() {
		onZeroCount.Add(1)
	})
	if err != nil {
		t.Fatalf("NewRefCounted() failed: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Go(func() {
			if rc.TryIncrement() {
				rc.Decrement()
			}
		})
	}
	rc.Decrement()
	wg.Wait()
	if got := onZeroCount.Load(); got != 1 {
		t.Fatalf("After concurrent increments/decrements and final decrement, onZeroCount = %v, want 1", got)
	}
}

// Test verifies that Decrementing a resource whose reference count has already
// dropped to zero results in a negative refcount log error.
func (s) TestRefCounted_DecrementNegative(t *testing.T) {
	rc, err := NewRefCounted("val", func() {})
	if err != nil {
		t.Fatalf("NewRefCounted() failed: %v", err)
	}
	rc.Decrement()

	grpctest.ExpectError("refcount cannot be negative")
	rc.Decrement()
}

// Test verifies that Incrementing a dead resource results in a closed resource
// log error.
func (s) TestRefCounted_IncrementDead(t *testing.T) {
	rc, err := NewRefCounted("val", func() {})
	if err != nil {
		t.Fatalf("NewRefCounted() failed: %v", err)
	}
	rc.Decrement()

	grpctest.ExpectError("resource already closed or dead")
	rc.Increment()
}

// Test verifies that NewRefCounted returns an error if the provided onZero
// callback is nil.
func (s) TestNilOnZero(t *testing.T) {
	const wantErr = "grpcsync: onZero callback cannot be nil"
	if _, err := NewRefCounted("val", nil); err == nil || err.Error() != wantErr {
		t.Fatalf("NewRefCounted(_, nil) returned error: %v, want: %q", err, wantErr)
	}
}
