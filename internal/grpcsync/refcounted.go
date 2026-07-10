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

// RefCounted tracks how many consumers hold a value of type V and runs a
// cleanup when the last reference is released
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
//
// WARNING: onZero runs synchronously inside Decrement; it must not acquire
// locks held by Decrement callers or invert lock ordering.
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
//
// WARNING: Avoid calling TryIncrement on hot paths when an active reference is
// already guaranteed by the caller; use Increment instead to bypass the
// CompareAndSwap loop overhead. TryIncrement should be reserved for speculative
// lookups (such as fetching from a cache or map) where the resource might be
// dead.
func (rc *RefCounted[V]) TryIncrement() bool {
	// Utilize a CompareAndSwap loop to prevent race conditions where a concurrent
	// decrement could drop the count to zero between the read and the increment
	// operation, which would otherwise inadvertently resurrect a closed resource.
	for {
		count := rc.refCount.Load()
		if count <= 0 {
			return false // Already dead or dying
		}
		if rc.refCount.CompareAndSwap(count, count+1) {
			return true
		}
	}
}

// Increment increments the reference count.
//
// WARNING: Call Increment only when there is a guarantee that an active
// reference is already present, ensuring the resource is not dead. If there is
// a possibility that the resource is dead or its reference count might have
// reached zero, call TryIncrement instead.
func (rc *RefCounted[V]) Increment() error {
	if rc.refCount.Add(1) <= 1 {
		return fmt.Errorf("grpcsync: resource already closed or dead")
	}
	return nil
}

// Decrement decrements the reference count. If it drops to zero, the onZero
// callback is executed synchronously before this method returns.
func (rc *RefCounted[V]) Decrement() error {
	if v := rc.refCount.Add(-1); v < 0 {
		return fmt.Errorf("grpcsync: refcount cannot be negative")
	} else if v == 0 {
		rc.onZero()
	}
	return nil
}
