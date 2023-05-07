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
	"fmt"
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

	msgs := make([]int, len(ts.msgs))
	copy(msgs, ts.msgs)

	return msgs
}

func (s) TestPubSub_PublishNoMsg(t *testing.T) {
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

func (s) TestPubSub_PublishMsgs_RegisterSubs_And_Stop(t *testing.T) {
	pubsub := NewPubSub()

	ts1 := newTestSubscriber()
	pubsub.Subscribe(ts1)
	wantMsgs := []int{}

	const numPublished = 10
	var wg sync.WaitGroup
	wg.Add(2)
	// Publish ten messages on the pubsub and ensure that they are received in order by the subscriber.
	go func() {
		for i := 0; i < numPublished; i++ {
			pubsub.Publish(i)
			wantMsgs = append(wantMsgs, i)
		}
		wg.Done()
	}()

	isTimeout := false
	go func() {
		for i := 0; i < numPublished; i++ {
			select {
			case <-ts1.onMsgCh:
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
	if gotMsgs := ts1.receivedMsgs(); !cmp.Equal(gotMsgs, wantMsgs) {
		t.Fatalf("Received messages is %v, want %v", gotMsgs, wantMsgs)
	}

	// Register another subscriber and ensure that it receives the last published message.
	ts2 := newTestSubscriber()
	pubsub.Subscribe(ts2)
	wantMsgs2 := []int{numPublished - 1}

	select {
	case <-ts2.onMsgCh:
	case <-time.After(defaultTestShortTimeout):
		t.Fatalf("Timeout when expecting the onMessage() callback to be invoked")
	}
	if gotMsgs2 := ts2.receivedMsgs(); !cmp.Equal(gotMsgs2, wantMsgs2) {
		t.Fatalf("Received messages is %v, want %v", gotMsgs2, wantMsgs2)
	}

	wg.Add(3)
	// Publish ten messages on the pubsub and ensure that they are received in order by the subscribers.
	go func() {
		for i := 0; i < numPublished; i++ {
			pubsub.Publish(i)
			wantMsgs = append(wantMsgs, i)
			wantMsgs2 = append(wantMsgs2, i)
		}
		wg.Done()
	}()
	errCh := make(chan error, 1)
	go func() {
		for i := 0; i < numPublished; i++ {
			select {
			case <-ts1.onMsgCh:
			case <-time.After(defaultTestShortTimeout):
				errCh <- fmt.Errorf("")
			}
		}
		wg.Done()
	}()
	go func() {
		for i := 0; i < numPublished; i++ {
			select {
			case <-ts2.onMsgCh:
			case <-time.After(defaultTestShortTimeout):
				errCh <- fmt.Errorf("")
			}
		}
		wg.Done()
	}()
	wg.Wait()
	select {
	case <-errCh:
		t.Fatalf("Timeout when expecting the onMessage() callback to be invoked")
	default:
	}
	if gotMsgs := ts1.receivedMsgs(); !cmp.Equal(gotMsgs, wantMsgs) {
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
	case <-ts1.onMsgCh:
		t.Fatalf("The callback was invoked after pubsub being stopped")
	case <-ts2.onMsgCh:
		t.Fatalf("The callback was invoked after pubsub being stopped")
	case <-time.After(defaultTestShortTimeout):
	}
}

func (s) TestPubSub_PublishMsgs_BeforeRegisterSub(t *testing.T) {
	pubsub := NewPubSub()
	defer pubsub.Stop()

	pubsub.Publish(1)
	pubsub.Publish(2)
	pubsub.Publish(3)

	ts := newTestSubscriber()
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
