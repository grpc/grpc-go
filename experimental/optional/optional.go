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

// Package optional adds generic optional types.
//
// All APIs in this package are experimental.
package optional

// Option represents an optional value of type T.
type Option[T any] struct {
	val   T
	isSet bool
}

// New creates a new Option that does not have a value set. This can also be
// done implicitly using a zero-value declaration: `var opt optional.Option[T]`
func New[T any]() Option[T] {
	return Option[T]{}
}

// NewValue creates a new Option with the provided value.
func NewValue[T any](value T) Option[T] {
	return Option[T]{
		val:   value,
		isSet: true,
	}
}

// Value returns the underlying value and a boolean indicating if the value is
// set. If the value is not set, it returns the zero value of T and false.
func (o Option[T]) Value() (T, bool) {
	return o.val, o.isSet
}

// WithValue returns a new Option containing the provided value.
func (o Option[T]) WithValue(value T) Option[T] {
	return Option[T]{
		val:   value,
		isSet: true,
	}
}

// Clear returns an empty Option.
func (o Option[T]) Clear() Option[T] {
	return Option[T]{}
}
