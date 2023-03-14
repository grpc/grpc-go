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
	"google.golang.org/grpc/internal/callbackserializer"
)

// Watcher defines the functions which is executed as soon as a connectivity state changes
// of Tracker is reported.
type Watcher interface {
	// OnStateChange is invoked when connectivity state changes on ClientConn is reported.
	OnStateChange(state connectivity.State)
}

// Tracker manages watchers and their status. It holds a previous connecitivity state of
// ClientConns and SubConns.
type Tracker struct {
	mu       sync.Mutex
	state    connectivity.State
	watchers map[Watcher]bool
	cs       *callbackserializer.CallbackSerializer
}

// NewTracker returns a new Tracker instance.
func NewTracker(state connectivity.State) *Tracker {
	ctx := context.Background()
	return &Tracker{
		state:    state,
		watchers: map[Watcher]bool{},
		cs:       callbackserializer.New(ctx),
	}
}

// AddWatcher adds a provided watcher to the set of watchers in Tracker.
// Schedules a callback on the provided watcher with current state.
// Returns a function to remove the provided watcher from the set of watchers. The caller
// of this method is responsible for invoking this function when it no longer needs to
// monitor the connectivity state changes on the channel.
func (t *Tracker) AddWatcher(watcher Watcher) func() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.watchers[watcher] = true

	t.cs.Schedule(func(_ context.Context) {
		watcher.OnStateChange(t.state)
	})

	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.watchers[watcher] = false
	}
}

// SetState is called to publish the connectivity.State changes on ClientConn
// to watchers.
func (t *Tracker) SetState(state connectivity.State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Update the cached state
	t.state = state
	// Invoke callbacks on all registered watchers.
	for watcher, isEffective := range t.watchers {
		if isEffective {
			t.cs.Schedule(func(_ context.Context) {
				watcher.OnStateChange(t.state)
			})
		}
	}
}
