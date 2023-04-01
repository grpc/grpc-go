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

// Package genericpubsub provides functionality to report and track
// target changes of struct which is registered as a subscriber.
package genericpubsub

import (
	"context"
	"sync"

	"google.golang.org/grpc/internal/grpcsync"
)

// Watcher wraps the functionality to be implemented by components
// interested in watching target changes.
type Watcher interface {
	// OnTargetChange is invoked to report target changes on the
	// entity being watched.
	OnTargetChange(target interface{})
}

// Tracker provides pubsub-like functionality for target changes.
//
// The entity whose target is being tracked publishes updates by
// calling the SetTarget() method.
//
// Components interested in target updates of the tracked entity
// subscribe to updates by calling the AddWatcher() method.
type Tracker struct {
	cs     *grpcsync.CallbackSerializer
	cancel context.CancelFunc

	// Access to the below fields are guarded by this mutex.
	mu       sync.Mutex
	target   interface{}
	watchers map[Watcher]bool
	stopped  bool
}

// NewTracker returns a new Tracker instance initialized with the provided
// target.
func NewTracker(target interface{}) *Tracker {
	ctx, cancel := context.WithCancel(context.Background())
	return &Tracker{
		cs:       grpcsync.NewCallbackSerializer(ctx),
		cancel:   cancel,
		target:   target,
		watchers: map[Watcher]bool{},
	}
}

// AddWatcher adds the provided watcher to the set of watchers in Tracker.
// The OnTargetChange() callback will be invoked asynchronously with the current
// state of the tracked entity to begin with, and subsequently for every target
// change.
//
// Returns a function to remove the provided watcher from the set of watchers.
// The caller of this method is responsible for invoking this function when it
// no longer needs to monitor the target changes on the channel.
func (t *Tracker) AddWatcher(watcher Watcher) func() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped {
		return func() {}
	}

	t.watchers[watcher] = true

	target := t.target
	t.cs.Schedule(func(context.Context) {
		t.mu.Lock()
		defer t.mu.Unlock()
		watcher.OnTargetChange(target)
	})

	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		delete(t.watchers, watcher)
	}
}

// SetTarget updates the target of the entity being tracked, and
// invokes the OnTargetChange callback of all registered watchers.
func (t *Tracker) SetTarget(target interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped {
		return
	}
	t.target = target
	for watcher := range t.watchers {
		t.cs.Schedule(func(context.Context) {
			t.mu.Lock()
			defer t.mu.Unlock()
			watcher.OnTargetChange(target)
		})
	}
}

// Stop shuts down the Tracker and releases any resources allocated by it.
// It is guaranteed that no Watcher callbacks would be invoked once this
// method returns.
func (t *Tracker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true

	t.cancel()
}
