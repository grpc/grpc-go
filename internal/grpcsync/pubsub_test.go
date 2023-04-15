/*
 *
 * Copyright 2023 gRPC authors.
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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

type testSubscriber struct {
	mu      sync.Mutex
	msgs    []int
	onMsgCh chan struct{}
}

func newTestSubscriber() *testSubscriber {
	return &testSubscriber{onMsgCh: make(chan struct{})}
}

func (ts *testSubscriber) OnMessage(msg interface{}) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.msgs = append(ts.msgs, msg.(int))
	ts.onMsgCh <- struct{}{}
}

func (ts *testSubscriber) receivedMsgs() []int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.msgs
}

func (s) TestPubSub_PublishNoMsg(t *testing.T) {
	// Create a new pubsub.
	pubsub := NewPubSub()
	defer pubsub.Stop()

	ts := newTestSubscriber()
	pubsub.Subscribe(ts)

	select {
	case <-ts.onMsgCh:
		t.Fatalf("Subscriber callback invoked when no message was published")
	case <-time.After(defaultTestShortTimeout):
	}
}

func (s) TestPutSub_PublishMsgs_RegisterSubs_And_Stop(t *testing.T) {
	// This flag becomes true when waiting callback is timed out.
	isTimeout := false
	// Create a new pubsub.
	pubsub := NewPubSub()

	ts := newTestSubscriber()

	pubsub.Subscribe(ts)
	wantMsgs := []int{}

	const numPublished = 10
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		for i := 0; i < numPublished; i++ {
			pubsub.Publish(i)
		}
		wg.Done()
	}()
	go func() {
		for i := 0; i < numPublished; i++ {
			select {
			case <-ts.onMsgCh:
				wantMsgs = append(wantMsgs, i)
			case <-time.After(defaultTestShortTimeout):
				isTimeout = true
			}
		}
		wg.Done()
	}()
	wg.Wait()
	if isTimeout {
		t.Fatalf("Timeout when expecting the onMessage() callback to be invoked")
	}
	if gotMsgs := ts.receivedMsgs(); !cmp.Equal(gotMsgs, wantMsgs) {
		t.Fatalf("Received messages is %v, want %v", gotMsgs, wantMsgs)
	}

	ts2 := newTestSubscriber()
	pubsub.Subscribe(ts2)
	wantMsgs2 := []int{}

	select {
	case <-ts2.onMsgCh:
		wantMsgs2 = append(wantMsgs2, numPublished-1)
	case <-time.After(defaultTestShortTimeout):
		t.Fatalf("Timeout when expecting the onMessage() callback to be invoked")
	}
	if gotMsgs2 := ts2.receivedMsgs(); !cmp.Equal(gotMsgs2, wantMsgs2) {
		t.Fatalf("Received messages is %v, want %v", gotMsgs2, wantMsgs2)
	}

	var wg2 sync.WaitGroup
	wg2.Add(3)
	go func() {
		for i := 0; i < numPublished; i++ {
			pubsub.Publish(i)
		}
		wg2.Done()
	}()
	go func() {
		for i := 0; i < numPublished; i++ {
			select {
			case <-ts.onMsgCh:
				wantMsgs = append(wantMsgs, i)
			case <-time.After(defaultTestShortTimeout):
				isTimeout = true
			}
		}
		wg2.Done()
	}()
	go func() {
		for i := 0; i < numPublished; i++ {
			select {
			case <-ts2.onMsgCh:
				wantMsgs2 = append(wantMsgs2, i)
			case <-time.After(defaultTestShortTimeout):
				isTimeout = true
			}
		}
		wg2.Done()
	}()
	wg2.Wait()
	if isTimeout {
		t.Fatalf("Timeout when expecting the onMessage() callback to be invoked")
	}
	if gotMsgs := ts.receivedMsgs(); !cmp.Equal(gotMsgs, wantMsgs) {
		t.Fatalf("Received messages is %v, want %v", gotMsgs, wantMsgs)
	}
	if gotMsgs2 := ts2.receivedMsgs(); !cmp.Equal(gotMsgs2, wantMsgs2) {
		t.Fatalf("Received messages is %v, want %v", gotMsgs2, wantMsgs2)
	}

	pubsub.Stop()

	go func() {
		pubsub.Publish(99)
	}()
	// Ensure that the subscriber callback is not invoked as instantiated
	// pubsub has already closed.
	select {
	case <-ts.onMsgCh:
		t.Fatalf("The callback was invoked after pubsub being stopped")
	case <-ts2.onMsgCh:
		t.Fatalf("The callback was invoked after pubsub being stopped")
	case <-time.After(defaultTestShortTimeout):
	}
}

func (s) TestPubSub_PublishMsgs_BeforeRegisterSub(t *testing.T) {
	// Create a new pubsub.
	pubsub := NewPubSub()
	defer pubsub.Stop()

	ts := newTestSubscriber()

	pubsub.Publish(1)
	pubsub.Publish(2)
	pubsub.Publish(3)

	pubsub.Subscribe(ts)

	wantMsgs := []int{3}
	// Ensure that the subscriber callback is invoked with a previously
	// published message.
	select {
	case <-ts.onMsgCh:
		if gotMsgs := ts.receivedMsgs(); !cmp.Equal(gotMsgs, wantMsgs) {
			t.Fatalf("Received messages is %v, want %v", gotMsgs, wantMsgs)
		}
	case <-time.After(defaultTestShortTimeout):
		t.Fatalf("Timeout when expecting the onMessage() callback to be invoked")
	}
}
