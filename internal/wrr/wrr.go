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
 */

// Package wrr contains the interface and common implementations of wrr
// algorithms.
package wrr

// WRR defines an interface that implements weighted round robin.
type WRR interface {
	// UpdateOrAdd update the item with new `weight` or add the
	// item to the WRR set if item is not existed.
	//
	// UpdateOrAdd, Remove and Next need to be thread safe.
	UpdateOrAdd(item interface{}, weight int64)
	// Next returns the next picked item.
	//
	// UpdateOrAdd, Remove and Next need to be thread safe.
	Next() interface{}

	// UpdateOrAdd, Remove and Next need to be thread safe.
	Remove(item interface{})
}
