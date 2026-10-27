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
