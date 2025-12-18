/*
 *
 * Copyright 2025 gRPC authors.
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

package xdsdepmgr

// xdsResourceWatcher is a generic implementation of the xdsresource.Watcher
// interface.
type xdsResourceWatcher[T any] struct {
	onUpdate       func(*T, func())
	onError        func(error, func())
	onAmbientError func(error, func())
}

func (x *xdsResourceWatcher[T]) ResourceChanged(update *T, onDone func()) {
	x.onUpdate(update, onDone)
}

func (x *xdsResourceWatcher[T]) ResourceError(err error, onDone func()) {
	x.onError(err, onDone)
}

func (x *xdsResourceWatcher[T]) AmbientError(err error, onDone func()) {
	x.onAmbientError(err, onDone)
}
