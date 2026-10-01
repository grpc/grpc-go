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

	"github.com/google/go-cmp/cmp"
)

// TestItemList_FIFO verifies FIFO ordering across buffer growth, wrap-around
// and shrinking by interleaving enqueues and dequeues.
func (s) TestItemList_FIFO(t *testing.T) {
	var il itemList[any]
	// enqueue adds n consecutive integers to the list, starting with next. It
	// returns the integer that follows the last one enqueued, which should be
	// passed as next in the following call.
	enqueue := func(next, n int) int {
		for range n {
			il.enqueue(next)
			next++
		}
		return next
	}
	// dequeue removes n items from the list and verifies that they are
	// consecutive integers starting with want, i.e. that items come out in the
	// order they were enqueued. It returns the integer expected to be dequeued
	// next, which should be passed as want in the following call.
	dequeue := func(want, n int) int {
		t.Helper()
		for range n {
			if il.isEmpty() {
				t.Fatalf("isEmpty() = true, want false when expecting item %d", want)
			}
			if got := il.peek(); got != want {
				t.Fatalf("peek() = %v, want %v", got, want)
			}
			if got := il.dequeue(); got != want {
				t.Fatalf("dequeue() = %v, want %v", got, want)
			}
			want++
		}
		return want
	}

	// Offset the head so that subsequent growth has to handle wrap-around.
	next := enqueue(0, 3)
	want := dequeue(0, 2)
	next = enqueue(next, 1000)
	if got, wantCap := len(il.buf), 1024; got != wantCap {
		t.Fatalf("len(il.buf) = %d, want %d", got, wantCap)
	}
	want = dequeue(want, 900)
	if got := len(il.buf); got >= 1024 {
		t.Fatalf("len(il.buf) = %d after draining, want buffer to have shrunk", got)
	}
	next = enqueue(next, 50)
	dequeue(want, next-want)
	if !il.isEmpty() {
		t.Fatalf("isEmpty() = false, want true")
	}
	if got := il.dequeue(); got != nil {
		t.Fatalf("dequeue() on empty list = %v, want nil", got)
	}
	if got := len(il.buf); got > itemListShrinkThreshold {
		t.Fatalf("len(il.buf) = %d after draining, want <= %d", got, itemListShrinkThreshold)
	}
	for i, v := range il.buf {
		if v != nil {
			t.Fatalf("il.buf[%d] = %v after draining, want nil", i, v)
		}
	}
}

func (s) TestItemList_DequeueAll(t *testing.T) {
	var il itemList[any]
	il.dequeueAll(func(any) { t.Fatal("dequeueAll() called f on an empty list") })

	// Offset the head so that the items wrap around the end of the buffer.
	for i := range 3 {
		il.enqueue(i)
	}
	il.dequeue()
	il.dequeue()
	for i := 3; i < 6; i++ {
		il.enqueue(i)
	}
	var got []int
	il.dequeueAll(func(it any) { got = append(got, it.(int)) })
	want := []int{2, 3, 4, 5}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("dequeueAll() visited items diff (-want +got):\n%s", diff)
	}
	if !il.isEmpty() || il.buf != nil {
		t.Fatalf("list not reset after dequeueAll(): %+v", il)
	}
}

func (s) TestControlBuffer_Throttle(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	cb := newControlBuffer(done, true)

	// Fill the control buffer up to the limit with throttled items.
	for range maxQueuedTransportResponseFrames {
		cb.put(&ping{ack: true})
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
	cb := newControlBuffer(done, true)

	// Fill the control buffer with many more than limit number of non-throttled
	// items.
	for i := 0; i < maxQueuedTransportResponseFrames+10; i++ {
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

func (s) TestControlBuffer_ThrottlingDisabled(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	cb := newControlBuffer(done, false)

	// Fill the control buffer beyond the limit with throttled items.
	for i := 0; i < maxQueuedTransportResponseFrames+10; i++ {
		cb.put(&ping{ack: true})
	}

	// throttle() should not block when throttling is disabled.
	throttled := make(chan struct{})
	go func() {
		cb.throttle()
		close(throttled)
	}()

	select {
	case <-throttled:
	case <-time.After(defaultTestShortTimeout):
		t.Fatal("throttle() blocked when throttling was disabled")
	}
}
