/*
 *
 * Copyright 2023 gRPC authors.
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

package grpc

import (
	"google.golang.org/grpc/encoding"
)

// SharedBufferPool is a pool of buffers that can be shared, resulting in
// decreased memory allocation. Currently, in gRPC-go, it is only utilized
// for parsing incoming messages.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
type SharedBufferPool = encoding.SharedBufferPool

// NewSharedBufferPool creates a simple SharedBufferPool with buckets
// of different sizes to optimize memory usage. This prevents the pool from
// wasting large amounts of memory, even when handling messages of varying sizes.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
func NewSharedBufferPool() SharedBufferPool {
	return encoding.NewSharedBufferPool()
}
