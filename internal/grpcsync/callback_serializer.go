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

package grpcsync

import (
	"context"

	"google.golang.org/grpc/internal/buffer"
)

// CallbackSerializer provides a mechanism to schedule callbacks in a
// synchronized manner. It provides a FIFO guarantee on the order of execution
// of scheduled callbacks. New callbacks can be scheduled by invoking the
// Schedule() method.
//
// This type is safe for concurrent access.
type CallbackSerializer struct {
	// Done is closed once the serializer is shut down completely, i.e a
	// scheduled callback, if any, that was running when the context passed to
	// NewCallbackSerializer is cancelled, has completed and the serializer has
	// deallocated all its resources.
	Done chan struct{}

	callbacks *buffer.Unbounded
}

// NewCallbackSerializer returns a new CallbackSerializer instance. The provided
// context will be passed to the scheduled callbacks. Users should cancel the
// provided context to shutdown the CallbackSerializer. It is guaranteed that no
// callbacks will be executed once this context is canceled.
func NewCallbackSerializer(ctx context.Context) *CallbackSerializer {
	t := &CallbackSerializer{
		Done:      make(chan struct{}),
		callbacks: buffer.NewUnbounded(),
	}
	go t.run(ctx)
	return t
}

// Schedule adds a callback to be scheduled after existing callbacks are run.
//
// Callbacks are expected to honor the context when performing any blocking
// operations, and should return early when the context is canceled.
func (t *CallbackSerializer) Schedule(f func(ctx context.Context)) {
	t.callbacks.Put(f)
}

func (t *CallbackSerializer) run(ctx context.Context) {
	defer close(t.Done)
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			t.callbacks.Close()
			return
		case callback, ok := <-t.callbacks.Get():
			if !ok {
				return
			}
			t.callbacks.Load()
			callback.(func(ctx context.Context))(ctx)
		}
	}
}
