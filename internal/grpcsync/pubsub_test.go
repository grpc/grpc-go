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
	mu      sync.Mutex
	t       *testing.T
	targets []string
	wg      *sync.WaitGroup
}

func (mw *mockWatcher) OnTargetChange(target interface{}) {
	mw.mu.Lock()
	defer mw.mu.Unlock()
	mw.t.Logf("OnTargetChange %p", mw)
	mw.targets = append(mw.targets, target.(string))
	mw.wg.Done()
}

func (s) TestTracker(t *testing.T) {
	tracker := NewTracker("initial")
	defer tracker.Stop()

	// Add a watcher and verify that its OnTargetChange is called with the initial target.
	wg := sync.WaitGroup{}
	expectedTarget := []string{}
	mw := &mockWatcher{
		t:  t,
		wg: &wg,
	}

	tracker.AddWatcher(mw)
	wg.Add(1)
	wg.Wait()
	t.Log(tracker.watchers)
	expectedTarget = append(expectedTarget, "initial")
	if diff := cmp.Diff(mw.targets, expectedTarget); diff != "" {
		t.Errorf("Difference between mw.targets and expectedTarget for initial watcher callback. diff(-want, +got):\n%s", diff)
	}

	// Update the target and verify that OnTargetChange is called again.
	tracker.SetTarget("set")
	wg.Add(1)
	wg.Wait()
	t.Log(tracker.watchers)
	expectedTarget = append(expectedTarget, "set")
	if diff := cmp.Diff(mw.targets, expectedTarget); diff != "" {
		t.Errorf("Difference between mw.targets and expectedTarget for updated watcher callback. diff(-want, +got):\n%s", diff)
	}

	// Add another watcher and verify that its OnTargetChange is called with the current target.
	expectedTarget2 := []string{}
	mw2 := &mockWatcher{
		t:  t,
		wg: &wg,
	}
	cancelFunc2 := tracker.AddWatcher(mw2)
	wg.Add(1)
	wg.Wait()
	t.Log(tracker.watchers)
	expectedTarget2 = append(expectedTarget2, "set")
	if diff := cmp.Diff(mw.targets, expectedTarget); diff != "" {
		t.Errorf("Difference between mw.targets and expectedTarget after adding second  watcher. diff(-want, +got):\n%s", diff)
	}
	if diff := cmp.Diff(mw2.targets, expectedTarget2); diff != "" {
		t.Errorf("Difference between mw2.targets and expectedTarget2 after adding second watcher. diff(-want, +got):\n%s", diff)
	}

	// Update the target again and verify that both watchers receive the update.
	tracker.SetTarget("set2")
	wg.Add(2)
	wg.Wait()
	t.Log(tracker.watchers)
	expectedTarget = append(expectedTarget, "set2")
	expectedTarget2 = append(expectedTarget2, "set2")
	if diff := cmp.Diff(mw.targets, expectedTarget); diff != "" {
		t.Errorf("Difference between mw.targets and expectedTarget after sending message to both watchers. diff(-want, +got):\n%s", diff)
	}
	if diff := cmp.Diff(mw2.targets, expectedTarget2); diff != "" {
		t.Errorf("Difference between mw2.targets and expectedTarget2 after sending message to both watchers. diff(-want, +got):\n%s", diff)
	}

	// Remove the second watcher and verify that its callback is no longer received.
	cancelFunc2()
	tracker.SetTarget("set3")
	expectedTarget = append(expectedTarget, "set3")
	wg.Add(1)
	wg.Wait()
	t.Log(tracker.watchers)
	if diff := cmp.Diff(mw.targets, expectedTarget); diff != "" {
		t.Errorf("Difference between mw.targets and expectedTarget after removing second watcher. diff(-want, +got):\n%s", diff)
	}
	if diff := cmp.Diff(mw2.targets, expectedTarget2); diff != "" {
		t.Errorf("Difference between mw2.targets and expectedTarget2 after removing second watcher. diff(-want, +got):\n%s", diff)
	}

	// Stop the tracker and verify that no more callbacks are received.
	tracker.Stop()
	tracker.SetTarget("set4")
	time.Sleep(10 * time.Millisecond)
	t.Log(tracker.watchers)
	if diff := cmp.Diff(mw.targets, expectedTarget); diff != "" {
		t.Errorf("Difference between mw.targets and expectedTarget after stopping tracker. diff(-want, +got):\n%s", diff)
	}
	if diff := cmp.Diff(mw2.targets, expectedTarget2); diff != "" {
		t.Errorf("Difference between mw2.targets and expectedTarget2 after stopping tracker. diff(-want, +got):\n%s", diff)
	}
}
