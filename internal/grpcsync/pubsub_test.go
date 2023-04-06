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

type mockWatcher struct {
	mu   sync.Mutex
	msgs []string
	wg   *sync.WaitGroup
}

func (mw *mockWatcher) OnChange(target interface{}) {
	mw.mu.Lock()
	defer mw.mu.Unlock()
	mw.msgs = append(mw.msgs, target.(string))
	mw.wg.Done()
}

func (s) TestPubSub(t *testing.T) {
	pubsub := NewPubSub()
	defer pubsub.Stop()

	wg := sync.WaitGroup{}
	expectedMsg := []string{}
	mw := &mockWatcher{
		msgs: []string{},
		wg:   &wg,
	}
	pubsub.Subscribe(mw)
	if diff := cmp.Diff(mw.msgs, expectedMsg); diff != "" {
		t.Errorf("Difference between mw.msgs and expectedMsg for initial situation. diff(-want, +got):\n%s", diff)
	}

	// Update the target and verify that Publish is called again.
	pubsub.Publish("set")
	wg.Add(1)
	wg.Wait()
	expectedMsg = append(expectedMsg, "set")
	if diff := cmp.Diff(mw.msgs, expectedMsg); diff != "" {
		t.Errorf("Difference between mw.msgs and expectedMsg for updated watcher callback. diff(-want, +got):\n%s", diff)
	}

	// Add another watcher and verify that its Publish is called with the current target.
	expectedMsg2 := []string{}
	mw2 := &mockWatcher{
		msgs: []string{},
		wg:   &wg,
	}
	cancelFunc2 := pubsub.Subscribe(mw2)
	wg.Add(1)
	wg.Wait()
	expectedMsg2 = append(expectedMsg2, "set")
	if diff := cmp.Diff(mw.msgs, expectedMsg); diff != "" {
		t.Errorf("Difference between mw.msgs and expectedMsg after adding second watcher. diff(-want, +got):\n%s", diff)
	}
	if diff := cmp.Diff(mw2.msgs, expectedMsg2); diff != "" {
		t.Errorf("Difference between mw2.targets and expectedMsg2 after adding second watcher. diff(-want, +got):\n%s", diff)
	}

	// Update the target again and verify that both watchers receive the update.
	pubsub.Publish("set2")
	wg.Add(2)
	wg.Wait()
	expectedMsg = append(expectedMsg, "set2")
	expectedMsg2 = append(expectedMsg2, "set2")
	if diff := cmp.Diff(mw.msgs, expectedMsg); diff != "" {
		t.Errorf("Difference between mw.msgs and expectedMsg after sending message to both watchers. diff(-want, +got):\n%s", diff)
	}
	if diff := cmp.Diff(mw2.msgs, expectedMsg2); diff != "" {
		t.Errorf("Difference between mw2.targets and expectedMsg2 after sending message to both watchers. diff(-want, +got):\n%s", diff)
	}

	// Remove the second watcher and verify that its callback is no longer received.
	cancelFunc2()
	pubsub.Publish("set3")
	wg.Add(1)
	wg.Wait()
	expectedMsg = append(expectedMsg, "set3")
	if diff := cmp.Diff(mw.msgs, expectedMsg); diff != "" {
		t.Errorf("Difference between mw.msgs and expectedMsg after removing second watcher. diff(-want, +got):\n%s", diff)
	}
	if diff := cmp.Diff(mw2.msgs, expectedMsg2); diff != "" {
		t.Errorf("Difference between mw2.targets and expectedMsg2 after removing second watcher. diff(-want, +got):\n%s", diff)
	}

	// Stop the pubsub and verify that no more callbacks are received.
	pubsub.Stop()
	pubsub.Publish("set4")
	time.Sleep(10 * time.Millisecond)
	if diff := cmp.Diff(mw.msgs, expectedMsg); diff != "" {
		t.Errorf("Difference between mw.msgs and expectedMsg after stopping pubsub. diff(-want, +got):\n%s", diff)
	}
	if diff := cmp.Diff(mw2.msgs, expectedMsg2); diff != "" {
		t.Errorf("Difference between mw2.targets and expectedMsg2 after stopping pubsub. diff(-want, +got):\n%s", diff)
	}
}
