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

// Package optional implements a generic optional type.
package optional

// Optional represents an optional value of type T.
// This type is not safe for concurrent access.
type Optional[T any] struct {
	val T
	set bool
}

// New creates a new Optional type that does not have a value set. This can also
// be done implicitly using a zero-value declaration: `var opt
// optional.Optional[T]`
func New[T any]() Optional[T] {
	return Optional[T]{}
}

// NewValue creates a new Optional type with the provided value.
func NewValue[T any](value T) Optional[T] {
	return Optional[T]{
		val: value,
		set: true,
	}
}

// Get returns the underlying value and a boolean indicating if the value is
// set. If the value is not set, it returns the zero value of T and false.
func (o *Optional[T]) Get() (T, bool) {
	return o.val, o.set
}

// Set returns a new Option containing the provided value.
func (o *Optional[T]) Set(v T) {
	o.val = v
	o.set = true
}
