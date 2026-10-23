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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// TestControlBuffer_ThrottleStress runs many producers that call throttle()
// and then put a transport response frame, concurrently with a consumer that
// drains the control buffer. The throttling limit is set very low so that the
// number of queued transport response frames crosses it constantly, which
// exercises the races between producers creating the throttling channel and
// the consumer closing it. The test fails if a producer gets stuck in
// throttle(), or if the throttling state doesn't match the number of queued
// frames once everything has been consumed.
func (s) TestControlBuffer_ThrottleStress(t *testing.T) {
	const (
		numProducers      = 8
		framesPerProducer = 20000
	)
	for _, limit := range []int{1, 2, 4} {
		t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
			origLimit := maxQueuedTransportResponseFrames
			maxQueuedTransportResponseFrames = limit
			defer func() { maxQueuedTransportResponseFrames = origLimit }()

			done := make(chan struct{})
			var closeDone sync.Once
			defer closeDone.Do(func() { close(done) })
			cb := newControlBuffer(done, true)
			it := &ping{ack: true} // A transport response frame.

			var wg sync.WaitGroup
			for range numProducers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for range framesPerProducer {
						cb.throttle()
						if err := cb.put(it); err != nil {
							return // Only happens if the test timed out.
						}
					}
				}()
			}
			producersDone := make(chan struct{})
			go func() {
				wg.Wait()
				close(producersDone)
			}()

			consumerDone := make(chan struct{})
			go func() {
				defer close(consumerDone)
				for range numProducers * framesPerProducer {
					if _, err := cb.get(true); err != nil {
						return // Only happens if the test timed out.
					}
				}
			}()

			timer := time.NewTimer(defaultTestTimeout)
			defer timer.Stop()
			for _, ch := range []chan struct{}{producersDone, consumerDone} {
				select {
				case <-ch:
				case <-timer.C:
					// Unblock the producers and the consumer before failing.
					closeDone.Do(func() { close(done) })
					t.Fatalf("Timed out with %d transport response frames queued; producers stuck in throttle()?", cb.transportResponseFrames.Load())
				}
			}

			if got := cb.transportResponseFrames.Load(); got != 0 {
				t.Errorf("transportResponseFrames = %d after consuming all frames, want 0", got)
			}
			if cb.trfChan.Load() != nil {
				t.Errorf("Throttling channel is set after consuming all frames, want nil")
			}
		})
	}
}

// TestItemList_FIFO verifies FIFO ordering across buffer growth, wrap-around
// and shrinking by interleaving enqueues and dequeues.
func (s) TestItemList_FIFO(t *testing.T) {
	var il itemList[int]
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
	if got := il.dequeue(); got != 0 {
		t.Fatalf("dequeue() on empty list = %v, want nil", got)
	}
	if got := len(il.buf); got > itemListShrinkFloor {
		t.Fatalf("len(il.buf) = %d after draining, want <= %d", got, itemListShrinkFloor)
	}
	if diff := cmp.Diff(make([]int, len(il.buf)), il.buf); diff != "" {
		t.Fatalf("il.buf mismatch after draining (-want +got):\n%s", diff)
	}
	if got, want := il.peek(), 0; got != want {
		t.Fatalf("peek() = %d, want %d", got, want)
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
		t.Errorf("list not reset after dequeueAll(): %+v", il)
	}
	if got := il.peek(); got != nil {
		t.Fatalf("peek() = %v, want nil", got)
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
