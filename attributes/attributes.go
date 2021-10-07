/*
 *
 * Copyright 2019 gRPC authors.
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

// Package attributes defines a generic key/value store used in various gRPC
// components.
//
// Experimental
//
// Notice: This package is EXPERIMENTAL and may be changed or removed in a
// later release.
package attributes

// Attributes is an immutable struct for storing and retrieving generic
// key/value pairs.  Keys must be hashable, and users should define their own
// types for keys.  Values should not be modified after they are added to an
// Attributes or if they were received from one.
type Attributes struct {
	m map[interface{}]Value
}

// Value must be implemented by all values stored in Attributes.  It allows
// comparing the values with other attributes matching the same key.
type Value interface {
	// IsEqual returns whether this Value is equivalent to o.
	IsEqual(o Value) bool
}

// New returns a new Attributes containing the key/value pair.
func New(key interface{}, value Value) *Attributes {
	return &Attributes{m: map[interface{}]Value{key: value}}
}

// WithValue returns a new Attributes containing the previous keys and values
// and the new key/value pair.  If the same key appears multiple times, the
// last value overwrites all previous values for that key.  To remove an
// existing key, use a nil value.  value should not be modified later.
func (a *Attributes) WithValue(key interface{}, value Value) *Attributes {
	if a == nil {
		return New(key, value)
	}
	n := &Attributes{m: make(map[interface{}]Value, len(a.m)+1)}
	for k, v := range a.m {
		n.m[k] = v
	}
	n.m[key] = value
	return n
}

// Value returns the value associated with these attributes for key, or nil if
// no value is associated with key.  The returned Value should not be modified.
func (a *Attributes) Value(key interface{}) Value {
	if a == nil {
		return nil
	}
	return a.m[key]
}

// IsEqual returns whether a and o are equivalent.
func (a *Attributes) IsEqual(o *Attributes) bool {
	if a == nil && o == nil {
		return true
	}
	if a == nil || o == nil {
		return false
	}
	if len(a.m) != len(o.m) {
		return false
	}
	for k, v := range a.m {
		ov, ok := o.m[k]
		if !ok {
			// o missing element of a
			return false
		}
		if !v.IsEqual(ov) {
			return false
		}
	}
	return true
}
