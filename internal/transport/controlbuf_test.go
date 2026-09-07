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

package transport

import (
	"testing"
	"time"
)

func (s) TestControlBuffer_Throttle(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	cb := newControlBuffer(done)

	// Fill the control buffer up to the limit with throttled items.
	for i := 0; i < maxQueuedControlBufferItems; i++ {
		cb.put(&ping{})
	}

	// The next call to throttle should block.
	throttleDone := make(chan struct{})
	go func() {
		cb.throttle()
		close(throttleDone)
	}()

	select {
	case <-throttleDone:
		t.Fatal("throttle() did not block when the buffer was full")
	case <-time.After(defaultTestShortTimeout):
	}

	// Consume one item from the control buffer.
	if _, err := cb.get(true); err != nil {
		t.Fatalf("cb.get(true) failed: %v", err)
	}

	// Now throttle() should unblock.
	select {
	case <-throttleDone:
	case <-time.After(time.Second):
		t.Fatal("throttle() did not unblock after an item was consumed")
	}
}

func (s) TestControlBuffer_NoThrottleForNonThrottledItems(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	cb := newControlBuffer(done)

	// Fill the control buffer with many more than limit number of non-throttled
	// items.
	for i := 0; i < maxQueuedControlBufferItems+10; i++ {
		cb.put(&dataFrame{})
	}

	// throttle() should not block.
	throttled := make(chan struct{})
	go func() {
		cb.throttle()
		close(throttled)
	}()

	select {
	case <-throttled:
	case <-time.After(defaultTestShortTimeout):
		t.Fatal("throttle() blocked for non-throttled items")
	}
}
