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

func (ts *testSubscriber) OnMessage(msg interface{}) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.msgs = append(ts.msgs, msg.(int))
	ts.onMsgCh <- struct{}{}
}

func (s) TestPubSub_PublishNoMsg(t *testing.T) {
	// Create a new pubsub.
	pubsub := NewPubSub()
	defer pubsub.Stop()

	ts := &testSubscriber{
		msgs:    []int{},
		onMsgCh: make(chan struct{}),
	}
	pubsub.Subscribe(ts)

	select {
	case <-ts.onMsgCh:
		t.Fatalf("Subscriber callback invoked when no message was published")
	case <-time.After(defaultTestShortTimeout):
	}
}

func (s) TestPubSub_PublishOneMsg(t *testing.T) {
	// Create a new pubsub.
	pubsub := NewPubSub()
	defer pubsub.Stop()

	ts := &testSubscriber{
		msgs:    []int{},
		onMsgCh: make(chan struct{}),
	}
	pubsub.Subscribe(ts)

	pubsub.Publish(1)

	expectedMsg := []int{1}
	select {
	case <-ts.onMsgCh:
		if diff := cmp.Diff(ts.msgs, expectedMsg); diff != "" {
			t.Errorf("Difference between ts.msgs and expectedMsg. diff(-want, +got):\n%s", diff)
		}
	case <-time.After(defaultTestShortTimeout):
		t.Fatalf("The callback was invoked within defaultTestShortTimeout")
	}
}

func (s) TestPubSub_PublishMultiMsgs_And_Stop(t *testing.T) {
	// Create a new pubsub.
	pubsub := NewPubSub()

	ts := &testSubscriber{
		msgs:    []int{},
		onMsgCh: make(chan struct{}),
	}
	pubsub.Subscribe(ts)

	expectedMsg := []int{}

	const numPublished = 10
	for i := 0; i < numPublished; i++ {
		pubsub.Publish(i)
		expectedMsg = append(expectedMsg, i)
		select {
		case <-ts.onMsgCh:
			if diff := cmp.Diff(ts.msgs, expectedMsg); diff != "" {
				t.Errorf("Difference between ts.msgs and expectedMsg. diff(-want, +got):\n%s", diff)
			}
		case <-time.After(defaultTestShortTimeout):
			t.Fatalf("The callback was invoked within defaultTestShortTimeout")
		}
	}

	pubsub.Stop()
	time.Sleep(defaultTestShortTimeout)

	pubsub.Publish(99)
	// Ensure that the subscriber callback is not invoked as instantiated
	// pubsub has already closed.
	select {
	case <-ts.onMsgCh:
		if diff := cmp.Diff(ts.msgs, expectedMsg); diff != "" {
			t.Errorf("Difference between ts.msgs and expectedMsg. diff(-want, +got):\n%s", diff)
		}
	case <-time.After(defaultTestShortTimeout):
	}
}

func (s) TestPubSub_PublishMultiMsgs_BeforeRegisterSubscriber(t *testing.T) {
	// Create a new pubsub.
	pubsub := NewPubSub()
	defer pubsub.Stop()

	ts := &testSubscriber{
		msgs:    []int{},
		onMsgCh: make(chan struct{}),
	}

	pubsub.Publish(1)
	pubsub.Publish(2)
	pubsub.Publish(3)

	pubsub.Subscribe(ts)

	expectedMsg := []int{3}
	// Ensure that the subscriber callback is invoked with a previously
	// published message.
	select {
	case <-ts.onMsgCh:
		if diff := cmp.Diff(ts.msgs, expectedMsg); diff != "" {
			t.Errorf("Difference between ts.msgs and expectedMsg. diff(-want, +got):\n%s", diff)
		}
	case <-time.After(defaultTestShortTimeout):
		t.Fatalf("The callback was invoked within defaultTestShortTimeout")
	}
}

func (s) TestPutSub_PubslishMultiMsgs_RegisterMultiSubs(t *testing.T) {
	// Create a new pubsub.
	pubsub := NewPubSub()
	defer pubsub.Stop()

	onMsgCh := make(chan struct{})

	ts := &testSubscriber{
		msgs:    []int{},
		onMsgCh: onMsgCh,
	}
	pubsub.Subscribe(ts)
	expectedMsg := []int{}

	const numPublished = 10
	for i := 0; i < numPublished; i++ {
		pubsub.Publish(i)
		expectedMsg = append(expectedMsg, i)
		select {
		case <-ts.onMsgCh:
			if diff := cmp.Diff(ts.msgs, expectedMsg); diff != "" {
				t.Errorf("Difference between ts.msgs and expectedMsg. diff(-want, +got):\n%s", diff)
			}
		case <-time.After(defaultTestShortTimeout):
			t.Fatalf("The callback was invoked within defaultTestShortTimeout")
		}
	}

	ts2 := &testSubscriber{
		msgs:    []int{},
		onMsgCh: onMsgCh,
	}
	pubsub.Subscribe(ts2)
	expectedMsg2 := []int{}

	expectedMsg2 = append(expectedMsg2, numPublished-1)
	select {
	case <-ts.onMsgCh:
		if diff := cmp.Diff(ts.msgs, expectedMsg); diff != "" {
			t.Errorf("Difference between ts.msgs and expectedMsg. diff(-want, +got):\n%s", diff)
		}
	case <-time.After(defaultTestShortTimeout):
		t.Fatalf("The callback was invoked within defaultTestShortTimeout")
	}

	for i := 0; i < numPublished; i++ {
		pubsub.Publish(i)

		expectedMsg = append(expectedMsg, i)
		select {
		case <-ts.onMsgCh:
			if diff := cmp.Diff(ts.msgs, expectedMsg); diff != "" {
				t.Errorf("Difference between ts.msgs and expectedMsg. diff(-want, +got):\n%s", diff)
			}
		case <-time.After(defaultTestShortTimeout):
			t.Fatalf("The callback was invoked within defaultTestShortTimeout")
		}

		expectedMsg2 = append(expectedMsg2, i)
		select {
		case <-ts.onMsgCh:
			if diff := cmp.Diff(ts2.msgs, expectedMsg2); diff != "" {
				t.Errorf("Difference between ts2.msgs and expectedMsg2. diff(-want, +got):\n%s", diff)
			}
		case <-time.After(defaultTestShortTimeout):
			t.Fatalf("The callback was invoked within defaultTestShortTimeout")
		}
	}
}
