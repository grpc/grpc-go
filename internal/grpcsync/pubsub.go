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
	"context"
	"sync"
)

// Watcher wraps the functionality to be implemented by components
// interested in watching msg changes.
type Watcher interface {
	// OnChange is invoked to report msg changes on the
	// entity being watched.
	OnChange(msg interface{})
}

// PubSub is a simple one-to-many publish-subscribe system that supports messages
// of arbitrary type. 
//
// Publisher invokes the Publish() method to publish new messages, while
// subscribers interested in receiving these messages register a callback 
// via the Subscribe() method. It guarantees that messages are delivered in
// the same order in which they were published.
//
// Once a PubSub is stopped, no more messages can be published, and
// it is guaranteed that no more subscriber callback will be invoked.
type PubSub struct {
	cs     *CallbackSerializer
	cancel context.CancelFunc

	// Access to the below fields are guarded by this mutex.
	mu       sync.Mutex
	msg      interface{}
	subscribers map[Watcher]bool
	stopped  bool
}

// NewPubSub returns a new PubSub instance to track msg changes.
func NewPubSub() *PubSub {
	ctx, cancel := context.WithCancel(context.Background())
	return &PubSub{
		cs:       NewCallbackSerializer(ctx),
		cancel:   cancel,
		watchers: map[Watcher]bool{},
	}
}

// Subscribe adds the provided watcher to the set of watchers in PubSub.
// The Publish() callback will be invoked asynchronously with the current
// msg of the tracked entity to begin with, and subsequently for every msg
// change.
//
// Returns a function to remove the provided watcher from the set of watchers.
// The caller of this method is responsible for invoking this function when it
// no longer needs to monitor the msg changes on the channel.
func (ps *PubSub) Subscribe(watcher Watcher) func() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.stopped {
		return func() {}
	}

	ps.watchers[watcher] = true

	if ps.msg != nil {
		msg := ps.msg
		ps.cs.Schedule(func(context.Context) {
			ps.mu.Lock()
			defer ps.mu.Unlock()
			watcher.OnChange(msg)
		})
	}

	return func() {
		ps.mu.Lock()
		defer ps.mu.Unlock()
		delete(ps.watchers, watcher)
	}
}

// Publish updates the msg of the entity being tracked, and
// invokes the Publish callback of all registered watchers.
func (ps *PubSub) Publish(msg interface{}) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.stopped {
		return
	}

	ps.msg = msg

	for watcher := range ps.watchers {
		// Prevent the watcher which is passed to the closure function
		// from being changed while this loop.
		w := watcher
		ps.cs.Schedule(func(context.Context) {
			ps.mu.Lock()
			defer ps.mu.Unlock()
			w.OnChange(msg)
		})
	}
}

// Stop shuts down the PubSub and releases any resources allocated by it.
// It is guaranteed that no Watcher callbacks would be invoked once this
// method returns.
func (ps *PubSub) Stop() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.stopped = true

	ps.cancel()
}
