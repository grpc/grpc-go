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
	mu   sync.Mutex
	msgs []string
}

func (ts *testSubscriber) OnMessage(msg interface{}) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.msgs = append(ts.msgs, msg.(string))
}

func (s) TestPubSub_PublishNoMsg(t *testing.T) {
	pubsub := NewPubSub()
	defer pubsub.Stop()

	done := make(chan struct{})
	ts := &testSubscriber{
		msgs: []string{},
	}
	pubsub.Subscribe(ts)

	go func() {
		time.Sleep(10 * time.Millisecond)
		done <- struct{}{}
	}()

	select {
	case <-done:
		if len(ts.msgs) != 0 {
			t.Fatalf("The callback was invoked within 10ms")
		}
	case <-time.After(20 * time.Millisecond):
		t.Fatalf("The callback was invoked within 20ms")
	}
}

func (s) TestPubSub_PublishOneMsg(t *testing.T) {
	pubsub := NewPubSub()
	defer pubsub.Stop()

	done := make(chan struct{})
	ts := &testSubscriber{
		msgs: []string{},
	}
	pubsub.Subscribe(ts)

	pubsub.Publish("p1")

	go func() {
		time.Sleep(10 * time.Millisecond)
		done <- struct{}{}
	}()

	expectedMsg := []string{"p1"}
	select {
	case <-done:
		if diff := cmp.Diff(ts.msgs, expectedMsg); diff != "" {
			t.Errorf("Difference between ts.msgs and expectedMsg. diff(-want, +got):\n%s", diff)
		}
	case <-time.After(20 * time.Millisecond):
		t.Fatalf("The callback was invoked within 20ms")
	}
}

func (s) TestPubSub_PublishMultiMsgs_And_Stop(t *testing.T) {
	pubsub := NewPubSub()

	done := make(chan struct{})
	ts := &testSubscriber{
		msgs: []string{},
	}
	pubsub.Subscribe(ts)

	pubsub.Publish("p1")
	pubsub.Publish("p2")
	pubsub.Publish("p3")

	go func() {
		time.Sleep(10 * time.Millisecond)
		done <- struct{}{}
	}()

	expectedMsg := []string{"p1", "p2", "p3"}
	select {
	case <-done:
		if diff := cmp.Diff(ts.msgs, expectedMsg); diff != "" {
			t.Errorf("Difference between ts.msgs and expectedMsg. diff(-want, +got):\n%s", diff)
		}
	case <-time.After(20 * time.Millisecond):
		t.Fatalf("The callback was invoked within 20ms")
	}

	pubsub.Stop()
	time.Sleep(10 * time.Millisecond)

	go func() {
		time.Sleep(10 * time.Millisecond)
		done <- struct{}{}
	}()

	pubsub.Publish("p4")

	select {
	case <-done:
		if diff := cmp.Diff(ts.msgs, expectedMsg); diff != "" {
			t.Errorf("Difference between ts.msgs and expectedMsg. diff(-want, +got):\n%s", diff)
		}
	case <-time.After(20 * time.Millisecond):
		t.Fatalf("The callback was invoked within 20ms")
	}
}

func (s) TestPubSub_PublishMultiMsgs_BeforeRegisterSubscriber(t *testing.T) {
	pubsub := NewPubSub()
	defer pubsub.Stop()

	done := make(chan struct{})
	ts := &testSubscriber{
		msgs: []string{},
	}

	pubsub.Publish("p1")
	pubsub.Publish("p2")
	pubsub.Publish("p3")

	pubsub.Subscribe(ts)

	go func() {
		time.Sleep(10 * time.Millisecond)
		done <- struct{}{}
	}()

	expectedMsg := []string{"p3"}
	select {
	case <-done:
		if diff := cmp.Diff(ts.msgs, expectedMsg); diff != "" {
			t.Errorf("Difference between ts.msgs and expectedMsg. diff(-want, +got):\n%s", diff)
		}
	case <-time.After(20 * time.Millisecond):
		t.Fatalf("The callback was invoked within 20ms")
	}
}

func (s) TestPutSub_PubslishMultiMsgs_RegisterMultiSubs(t *testing.T) {
	pubsub := NewPubSub()
	defer pubsub.Stop()

	done := make(chan struct{})
	ts := &testSubscriber{
		msgs: []string{},
	}
	pubsub.Subscribe(ts)

	pubsub.Publish("p1")
	pubsub.Publish("p2")

	go func() {
		time.Sleep(10 * time.Millisecond)
		done <- struct{}{}
	}()

	expectedMsg := []string{"p1", "p2"}
	select {
	case <-done:
		if diff := cmp.Diff(ts.msgs, expectedMsg); diff != "" {
			t.Errorf("Difference between ts.msgs and expectedMsg. diff(-want, +got):\n%s", diff)
		}
	case <-time.After(20 * time.Millisecond):
		t.Fatalf("The callback was invoked within 20ms")
	}

	ts2 := &testSubscriber{
		msgs: []string{},
	}
	pubsub.Subscribe(ts2)

	pubsub.Publish("p3")

	go func() {
		time.Sleep(10 * time.Millisecond)
		done <- struct{}{}
	}()

	expectedMsg = []string{"p1", "p2", "p3"}
	expectedMsg2 := []string{"p2", "p3"}
	select {
	case <-done:
		if diff := cmp.Diff(ts.msgs, expectedMsg); diff != "" {
			t.Errorf("Difference between ts.msgs and expectedMsg. diff(-want, +got):\n%s", diff)
		}
		if diff := cmp.Diff(ts2.msgs, expectedMsg2); diff != "" {
			t.Errorf("Difference between ts2.msgs and expectedMsg2. diff(-want, +got):\n%s", diff)
		}
	case <-time.After(20 * time.Millisecond):
		t.Fatalf("The callback was invoked within 20ms")
	}
}
