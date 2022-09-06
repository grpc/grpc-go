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

// CallbackSerializer TBD.
// TODO: Move out of xDS for other grpc code to have access to it.
type CallbackSerializer struct {
	closed    *grpcsync.Event
	done      *grpcsync.Event
	callbacks *buffer.Unbounded
}

// NewCallbackSerializer TDB.
func NewCallbackSerializer() *CallbackSerializer {
	t := &CallbackSerializer{
		closed:    grpcsync.NewEvent(),
		done:      grpcsync.NewEvent(),
		callbacks: buffer.NewUnbounded(),
	}
	go t.run()
	return t
}

// Close TBD.
func (t *CallbackSerializer) Close() {
	if t.closed.HasFired() {
		return
	}
	t.closed.Fire()
	<-t.done.Done()
}

// Schedule TBD.
func (t *CallbackSerializer) Schedule(f func()) {
	if t.closed.HasFired() {
		return
	}
	t.callbacks.Put(f)
}

func (t *CallbackSerializer) run() {
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
