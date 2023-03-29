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

// Package connectivitystate provides functionality to report and track
// connectivity state changes of ClientConns and SubConns.
package connectivitystate

import (
	"context"
	"sync"

	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/grpcsync"
)

// Watcher wraps the functionality to be implemented by components
// interested in watching connectivity state changes.
type Watcher interface {
	// OnStateChange is invoked to report connectivity state changes on the
	// entity being watched.
	OnStateChange(state connectivity.State)
}

// Tracker provides pubsub-like functionality for connectivity state changes.
//
// The entity whose connectivity state is being tracked publishes updates by
// calling the SetState() method.
//
// Components interested in connectivity state updates of the tracked entity
// subscribe to updates by calling the AddWatcher() method.
type Tracker struct {
	cs     *grpcsync.CallbackSerializer
	cancel context.CancelFunc

	// Access to the below fields are guarded by this mutex.
	mu       sync.Mutex
	state    connectivity.State
	watchers map[Watcher]bool
	stopped  bool
}

// NewTracker returns a new Tracker instance initialized with the provided
// connectivity state.
func NewTracker(state connectivity.State) *Tracker {
	ctx, cancel := context.WithCancel(context.Background())
	return &Tracker{
		cs:       grpcsync.NewCallbackSerializer(ctx),
		cancel:   cancel,
		state:    state,
		watchers: map[Watcher]bool{},
	}
}

// AddWatcher adds the provided watcher to the set of watchers in Tracker.
// The OnStateChange() callback will be invoked asynchronously with the current
// state of the tracked entity to begin with, and subsequently for every state
// change.
//
// Returns a function to remove the provided watcher from the set of watchers.
// The caller of this method is responsible for invoking this function when it
// no longer needs to monitor the connectivity state changes on the channel.
func (t *Tracker) AddWatcher(watcher Watcher) func() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped {
		return func() {}
	}

	t.watchers[watcher] = true

	state := t.state
	t.cs.Schedule(func(context.Context) {
		t.mu.Lock()
		defer t.mu.Unlock()
		watcher.OnStateChange(state)
	})

	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		delete(t.watchers, watcher)
	}
}

// SetState updates the connectivity state of the entity being tracked, and
// invokes the OnStateChange callback of all registered watchers.
func (t *Tracker) SetState(state connectivity.State) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped {
		return
	}
	t.state = state
	for watcher := range t.watchers {
		t.cs.Schedule(func(context.Context) {
			t.mu.Lock()
			defer t.mu.Unlock()
			watcher.OnStateChange(state)
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
