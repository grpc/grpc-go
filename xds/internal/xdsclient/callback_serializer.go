/*
 *
 * Copyright 2022 gRPC authors.
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

package xdsclient

import (
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/grpcsync"
)

// callbackSerializer provides a mechanism to schedule callbacks in a
// synchronized manner. It provides a FIFO guarantee on the order of execution
// of scheduled callbacks.
//
// New callbacks can be scheduled by invoking the Schedule() method. To cleanup
// resources used by the serializer and to discard any queued callbacks, the
// Close() method needs to be called.
//
// This type is safe for concurrent access.
type callbackSerializer struct {
	closed    *grpcsync.Event
	done      *grpcsync.Event
	callbacks *buffer.Unbounded
}

// newCallbackSerializer returns a new CallbackSerializer instance.
func newCallbackSerializer() *callbackSerializer {
	t := &callbackSerializer{
		closed:    grpcsync.NewEvent(),
		done:      grpcsync.NewEvent(),
		callbacks: buffer.NewUnbounded(),
	}
	go t.run()
	return t
}

// Close shuts down the CallbackSerializer. It is guaranteed that no callbacks
// will be scheduled once this function returns. If a callback is currently
// being executed, this functions blocks until execution of that callback
// finishes before returning.
func (t *callbackSerializer) Close() {
	if t.closed.HasFired() {
		return
	}
	t.closed.Fire()
	<-t.done.Done()
}

// Schedule adds a callback to be scheduled after existing callbacks are run.
func (t *callbackSerializer) Schedule(f func()) {
	if t.closed.HasFired() {
		return
	}
	t.callbacks.Put(f)
}

func (t *callbackSerializer) run() {
	defer func() {
		t.done.Fire()
	}()
	for {
		select {
		case <-t.closed.Done():
			return
		case callback := <-t.callbacks.Get():
			if t.closed.HasFired() {
				return
			}
			t.callbacks.Load()
			callback.(func())()
		}
	}
}
