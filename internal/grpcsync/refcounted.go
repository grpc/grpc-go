/*
 *
 * Copyright 2026 gRPC authors.
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
	"fmt"
	"sync/atomic"
)

// RefCounted manages the lifecycle of a resource using atomic reference
// counting.
type RefCounted[V any] struct {
	val      V
	refCount atomic.Int32
	onZero   func()
}

// NewRefCounted creates a new RefCounted instance wrapping the given value with
// initial refcount of one. The provided onZero callback must not be nil, and is
// executed exactly once when the reference count drops to zero.
//
// The value should typically be a pointer, interface, or handle rather than a
// plain value type (such as a struct or primitive value).
func NewRefCounted[V any](val V, onZero func()) (*RefCounted[V], error) {
	if onZero == nil {
		return nil, fmt.Errorf("grpcsync: onZero callback cannot be nil")
	}
	rc := &RefCounted[V]{
		val:    val,
		onZero: onZero,
	}
	rc.refCount.Store(1)
	return rc, nil
}

// Value returns the encapsulated resource.
func (rc *RefCounted[V]) Value() V {
	return rc.val
}

// TryIncrement attempts to increment the reference count, returning true if
// successful. It returns false if the count has already reached 0, indicating
// the resource has been cleaned up and cannot be resurrected.
func (rc *RefCounted[V]) TryIncrement() bool {
	// Utilize a CompareAndSwap loop to prevent race conditions where a
	// concurrent decrement could drop the count to zero between the read and
	// the increment operation, which would otherwise inadvertently resurrect a
	// closed resource.
	for {
		val := rc.refCount.Load()
		if val <= 0 {
			return false // Already dead or dying
		}
		if rc.refCount.CompareAndSwap(val, val+1) {
			return true
		}
	}
}

// Increment increments the reference count.
//
// The caller must already hold an active reference, ensuring the resource is
// not dead. If the resource might already be dead, use TryIncrement instead.
func (rc *RefCounted[V]) Increment() {
	rc.refCount.Add(1)
}

// Decrement decrements the reference count. If it drops to zero, the onZero
// callback is executed synchronously before this method returns.
func (rc *RefCounted[V]) Decrement() {
	if v := rc.refCount.Add(-1); v == 0 {
		rc.onZero()
	}
}
