//go:build !buffer_pooling

/*
 *
 * Copyright 2024 gRPC authors.
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

// Package mem provides utilities that facilitate memory reuse in byte slices
// that are used as buffers.
//
// # Experimental
//
// Notice: All APIs in this package are EXPERIMENTAL and may be changed or
// removed in a later release.
package mem

import (
	"fmt"
	"unsafe"
)

// NewBuffer creates a new Buffer from the given data, initializing the
// reference counter to 1. The given free function is called when all references
// to the returned Buffer are released.
//
// Note that the backing array of the given data is not copied.
func NewBuffer(data []byte, onFree func(*[]byte)) Buffer {
	return sliceBuffer(data)
}

type sliceBuffer []byte

func (s sliceBuffer) ReadOnlyData() []byte { return s }
func (s sliceBuffer) Ref() Buffer          { return s }
func (s sliceBuffer) Free()                {}
func (s sliceBuffer) Len() int             { return len(s) }

func (s sliceBuffer) split(n int) (left, right Buffer) {
	return s[:n], s[n:]
}

func (s sliceBuffer) read(buf []byte) (int, Buffer) {
	n := copy(buf, s)
	if n == len(s) {
		return n, nil
	}
	return n, s[n:]
}

// String returns a string representation of the buffer. May be used for
// debugging purposes.
func (s sliceBuffer) String() string {
	return fmt.Sprintf("mem.Buffer(%p, data: %p, length: %d)", unsafe.SliceData(s), unsafe.SliceData(s), len(s))
}
