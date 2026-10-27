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
			cb := newControlBuffer(done, clientSide)
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

// TestOutStream_DataQueue verifies that the intrusive queue of data frames in
// an outStream is FIFO, that frames are unlinked when dequeued, and that
// dequeuing from an empty queue returns nil.
func (s) TestOutStream_DataQueue(t *testing.T) {
	str := &outStream{}
	if got := str.dequeueData(); got != nil {
		t.Fatalf("dequeueData() on empty queue = %p, want nil", got)
	}
	dfs := []*dataFrame{{streamID: 1}, {streamID: 2}, {streamID: 3}}

	// Interleave enqueues and dequeues so the queue goes from empty to
	// non-empty more than once.
	str.enqueueData(dfs[0])
	if str.dataHead != dfs[0] || str.dataTail != dfs[0] {
		t.Fatalf("After first enqueue: head=%p, tail=%p, want both %p", str.dataHead, str.dataTail, dfs[0])
	}
	if got := str.dequeueData(); got != dfs[0] {
		t.Fatalf("dequeueData() = %p, want %p", got, dfs[0])
	}
	if str.dataHead != nil || str.dataTail != nil {
		t.Fatalf("After draining: head=%p, tail=%p, want both nil", str.dataHead, str.dataTail)
	}

	// Dequeue while the queue is non-empty, then enqueue again, to cover
	// enqueues after a partial drain (2 -> 1 -> 2).
	str.enqueueData(dfs[0])
	str.enqueueData(dfs[1])
	if got := str.dequeueData(); got != dfs[0] {
		t.Fatalf("dequeueData() = %p, want %p", got, dfs[0])
	}
	if dfs[0].next != nil {
		t.Fatalf("Dequeued frame still linked to %p", dfs[0].next)
	}
	if str.dataHead != dfs[1] || str.dataTail != dfs[1] {
		t.Fatalf("After partial drain: head=%p, tail=%p, want both %p", str.dataHead, str.dataTail, dfs[1])
	}
	str.enqueueData(dfs[2])
	if str.dataHead != dfs[1] || str.dataTail != dfs[2] {
		t.Fatalf("After enqueue following partial drain: head=%p, tail=%p, want head=%p, tail=%p", str.dataHead, str.dataTail, dfs[1], dfs[2])
	}

	for i, want := range dfs[1:] {
		got := str.dequeueData()
		if got != want {
			t.Fatalf("dequeueData() at position %d = %p, want %p", i, got, want)
		}
		if got.next != nil {
			t.Fatalf("Dequeued frame %d still linked to %p", i, got.next)
		}
	}
	if got := str.dequeueData(); got != nil {
		t.Fatalf("dequeueData() on drained queue = %p, want nil", got)
	}
	if str.dataHead != nil || str.dataTail != nil {
		t.Fatalf("After draining: head=%p, tail=%p, want both nil", str.dataHead, str.dataTail)
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

// Tests that the items that count towards the limit on a given side of the
// transport throttle the control buffer once the limit is reached, and that
// consuming one of them stops the throttling.
//
// Between them, this test and TestControlBuffer_NoThrottleForNonThrottledItems
// list every cbItem type for each side. Add new cbItem types to one of them.
func (s) TestControlBuffer_Throttle(t *testing.T) {
	tests := []struct {
		name string
		side side
		item cbItem
	}{
		// All pings count, not only acks, since the peer can trigger a
		// BDP ping with every DATA frame.
		{name: "client_ping", side: clientSide, item: &ping{}},
		{name: "client_pingAck", side: clientSide, item: &ping{ack: true}},
		{name: "client_incomingSettings", side: clientSide, item: &incomingSettings{}},
		{name: "client_incomingWindowUpdate", side: clientSide, item: &incomingWindowUpdate{}},
		{name: "server_ping", side: serverSide, item: &ping{}},
		{name: "server_pingAck", side: serverSide, item: &ping{ack: true}},
		{name: "server_incomingSettings", side: serverSide, item: &incomingSettings{}},
		{name: "server_incomingWindowUpdate", side: serverSide, item: &incomingWindowUpdate{}},
		{name: "server_cleanupStream", side: serverSide, item: &cleanupStream{}},
		{name: "server_earlyAbortStream", side: serverSide, item: &earlyAbortStream{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			done := make(chan struct{})
			defer close(done)
			cb := newControlBuffer(done, test.side)

			// Fill the control buffer with items up to the limit.
			for range maxQueuedTransportResponseFrames {
				cb.put(test.item)
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
			case <-time.After(defaultTestTimeout):
				t.Fatal("throttle() did not unblock after an item was consumed")
			}
		})
	}
}

// Tests that the items that do not count towards the limit on a given side of
// the transport never throttle the control buffer, however many are queued.
//
// Between them, this test and TestControlBuffer_Throttle list every cbItem
// type for each side. Add new cbItem types to one of them.
func (s) TestControlBuffer_NoThrottleForNonThrottledItems(t *testing.T) {
	tests := []struct {
		name string
		side side
		item cbItem
	}{
		{name: "client_registerStream", side: clientSide, item: &registerStream{}},
		{name: "client_clientHeaders", side: clientSide, item: &clientHeaders{}},
		{name: "client_serverHeaders", side: clientSide, item: &serverHeaders{}},
		// The client queues at most one cleanupStream per stream it
		// creates. Counting them can deadlock the client and the
		// server (see https://github.com/grpc/grpc-go/issues/9330).
		{name: "client_cleanupStream", side: clientSide, item: &cleanupStream{}},
		// Streams cancelled by the client also send a RST_STREAM.
		{name: "client_cleanupStreamWithRST", side: clientSide, item: &cleanupStream{rst: true}},
		{name: "client_earlyAbortStream", side: clientSide, item: &earlyAbortStream{}},
		{name: "client_dataFrame", side: clientSide, item: &dataFrame{}},
		{name: "client_outgoingWindowUpdate", side: clientSide, item: &outgoingWindowUpdate{}},
		{name: "client_outgoingSettings", side: clientSide, item: &outgoingSettings{}},
		{name: "client_incomingGoAway", side: clientSide, item: &incomingGoAway{}},
		{name: "client_goAway", side: clientSide, item: &goAway{}},
		{name: "client_outFlowControlSizeRequest", side: clientSide, item: &outFlowControlSizeRequest{}},
		{name: "client_outStreamRequestForTesting", side: clientSide, item: &outStreamRequestForTesting{}},
		{name: "client_closeConnection", side: clientSide, item: closeConnection{}},
		{name: "server_registerStream", side: serverSide, item: &registerStream{}},
		{name: "server_clientHeaders", side: serverSide, item: &clientHeaders{}},
		{name: "server_serverHeaders", side: serverSide, item: &serverHeaders{}},
		{name: "server_dataFrame", side: serverSide, item: &dataFrame{}},
		{name: "server_outgoingWindowUpdate", side: serverSide, item: &outgoingWindowUpdate{}},
		{name: "server_outgoingSettings", side: serverSide, item: &outgoingSettings{}},
		{name: "server_incomingGoAway", side: serverSide, item: &incomingGoAway{}},
		{name: "server_goAway", side: serverSide, item: &goAway{}},
		{name: "server_outFlowControlSizeRequest", side: serverSide, item: &outFlowControlSizeRequest{}},
		{name: "server_outStreamRequestForTesting", side: serverSide, item: &outStreamRequestForTesting{}},
		{name: "server_closeConnection", side: serverSide, item: closeConnection{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			done := make(chan struct{})
			defer close(done)
			cb := newControlBuffer(done, test.side)

			// Queue more items than the limit.
			for range maxQueuedTransportResponseFrames + 10 {
				cb.put(test.item)
			}

			// This returns nil right away, since the control
			// buffer is not throttled. Unlike throttle(), it
			// fails instead of hanging if it is throttled.
			if err := cb.throttleWithTimeout(defaultTestShortTimeout); err != nil {
				t.Fatalf("throttleWithTimeout() = %v, want <nil>", err)
			}
		})
	}
}

// Tests the different ways in which throttleWithTimeout returns.
func (s) TestControlBuffer_ThrottleWithTimeout(t *testing.T) {
	// newThrottledControlBuffer returns a control buffer that holds as
	// many counted items as the limit, so it is throttled.
	newThrottledControlBuffer := func(done <-chan struct{}) *controlBuffer {
		cb := newControlBuffer(done, clientSide)
		for range maxQueuedTransportResponseFrames {
			cb.put(&ping{ack: true})
		}
		return cb
	}

	// ensureThrottleWithTimeoutBlocks calls throttleWithTimeout in a new
	// goroutine, and verifies that the call blocks. It returns a channel
	// on which the call's return value is sent.
	ensureThrottleWithTimeoutBlocks := func(t *testing.T, cb *controlBuffer) <-chan error {
		t.Helper()
		// The callers expect the call to return for another reason.
		// This timeout never expires during the test.
		const longTimeout = time.Hour
		errCh := make(chan error, 1)
		go func() {
			errCh <- cb.throttleWithTimeout(longTimeout)
		}()
		select {
		case err := <-errCh:
			t.Fatalf("throttleWithTimeout() = %v, want it to block", err)
		case <-time.After(defaultTestShortTimeout):
		}
		return errCh
	}

	// waitForNilError waits for throttleWithTimeout to return, and
	// verifies that it returned nil.
	waitForNilError := func(t *testing.T, errCh <-chan error) {
		t.Helper()
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("throttleWithTimeout() = %v, want <nil>", err)
			}
		case <-time.After(defaultTestTimeout):
			t.Fatal("Timeout when waiting for throttleWithTimeout() to return")
		}
	}

	// Tests that throttleWithTimeout returns nil right away when the
	// control buffer is not throttled.
	t.Run("NotThrottled", func(t *testing.T) {
		done := make(chan struct{})
		defer close(done)
		cb := newControlBuffer(done, clientSide)
		for range maxQueuedTransportResponseFrames - 1 {
			cb.put(&ping{ack: true})
		}
		// If it blocks instead, it times out with an error.
		if err := cb.throttleWithTimeout(defaultTestTimeout); err != nil {
			t.Fatalf("throttleWithTimeout() = %v, want <nil>", err)
		}
	})

	// Tests that throttleWithTimeout returns an error when the control
	// buffer is still throttled after the timeout.
	t.Run("TimesOut", func(t *testing.T) {
		done := make(chan struct{})
		defer close(done)
		cb := newThrottledControlBuffer(done)
		start := time.Now()
		if err := cb.throttleWithTimeout(defaultTestShortTimeout); err == nil {
			t.Fatal("throttleWithTimeout() = <nil>, want an error")
		}
		if elapsed := time.Since(start); elapsed < defaultTestShortTimeout {
			t.Fatalf("throttleWithTimeout() returned after %v, want at least %v", elapsed, defaultTestShortTimeout)
		}
	})

	// Tests that consuming an item that does not count towards the limit
	// does not stop the throttling, so throttleWithTimeout times out.
	t.Run("UncountedItemConsumed", func(t *testing.T) {
		done := make(chan struct{})
		defer close(done)
		cb := newControlBuffer(done, clientSide)
		cb.put(&outgoingWindowUpdate{})
		for range maxQueuedTransportResponseFrames {
			cb.put(&ping{ack: true})
		}
		// Consume the outgoingWindowUpdate, the first item queued.
		if _, err := cb.get(false); err != nil {
			t.Fatalf("cb.get(false) failed: %v", err)
		}
		if err := cb.throttleWithTimeout(defaultTestShortTimeout); err == nil {
			t.Fatal("throttleWithTimeout() = <nil>, want an error")
		}
	})

	// Tests that throttleWithTimeout returns nil when an item that counts
	// towards the limit is consumed, which stops the throttling.
	t.Run("CountedItemConsumed", func(t *testing.T) {
		done := make(chan struct{})
		defer close(done)
		cb := newThrottledControlBuffer(done)
		errCh := ensureThrottleWithTimeoutBlocks(t, cb)
		if _, err := cb.get(false); err != nil {
			t.Fatalf("cb.get(false) failed: %v", err)
		}
		waitForNilError(t, errCh)
	})

	// Tests that throttleWithTimeout returns nil when the done channel is
	// closed, which happens when the transport closes.
	t.Run("TransportDone", func(t *testing.T) {
		done := make(chan struct{})
		cb := newThrottledControlBuffer(done)
		errCh := ensureThrottleWithTimeoutBlocks(t, cb)
		close(done)
		waitForNilError(t, errCh)
	})

	// Tests that throttleWithTimeout returns nil when the control buffer
	// is finished, which happens when the loopy writer exits, for example
	// because a write to the connection failed.
	t.Run("ControlBufferFinished", func(t *testing.T) {
		done := make(chan struct{})
		defer close(done)
		cb := newThrottledControlBuffer(done)
		errCh := ensureThrottleWithTimeoutBlocks(t, cb)
		cb.finish()
		waitForNilError(t, errCh)
	})
}
