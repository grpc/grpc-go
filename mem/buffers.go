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

// Package mem provides utilities that facilitate reusing memory. This involves
// pulling Buffer instances from a BufferPool and freeing the Buffers when done,
// returning them to the pool. Each buffer is reference counted, allowing different
// goroutines to concurrently access then free the data without explicit
// synchronization by leveraging multiple references.
//
// By convention, any APIs that return (mem.BufferSlice, error) should reduce the
// burden on the caller by never returning a mem.BufferSlice that needs to be freed
// if the error is non-nil, unless explicitly listed in the API's documentation.
//
// # Experimental
//
// Notice: All APIs in this package are EXPERIMENTAL and may be changed or
// removed in a later release.
package mem

import (
	"fmt"
	"sync/atomic"
)

// A Buffer represents a piece of data (in bytes) that can be referenced then
// later freed, via a given function. Note that a Buffer is not safe for
// concurrent access and instead each goroutine should use its own reference to
// the data (which can be acquired with Ref). Instances of Buffer can only be
// acquired by NewBuffer, and the given free function will only be invoked when
// Free is invoked on all references.
type Buffer struct {
	data  []byte
	refs  *atomic.Int32
	free  func()
	freed bool
}

// NewBuffer creates a new buffer from the given data, initializing the reference
// counter to 1. Note that the given data is not copied.
func NewBuffer(data []byte, free func([]byte)) *Buffer {
	b := &Buffer{data: data, refs: new(atomic.Int32)}
	if free != nil {
		b.free = func() { free(data) }
	}
	b.refs.Add(1)
	return b
}

// Copy first invokes BufferPool.Get to acquire a new buffer of the size of the
// given buffer, then copies the given buffer's data into the new buffer, and
// finally returns a Buffer that, when freed, will be returned to the pool.
func Copy(data []byte, pool BufferPool) *Buffer {
	buf := pool.Get(len(data))
	copy(buf, data)
	return NewBuffer(buf, pool.Put)
}

// ReadOnlyData returns the underlying byte slice. Note that it is undefined
// behavior to modify the contents of this slice in any way.
func (b *Buffer) ReadOnlyData() []byte {
	if b.freed {
		panic("Cannot read freed buffer")
	}
	return b.data
}

// Ref returns a new reference to this Buffer's underlying buffer. Calling Free
// on this reference will only free the buffer if all other references have been
// freed.
func (b *Buffer) Ref() *Buffer {
	if b.freed {
		panic("Cannot ref freed buffer")
	}

	b.refs.Add(1)
	return &Buffer{
		data: b.data,
		refs: b.refs,
		free: b.free,
	}
}

// Free decrements this Buffer's reference counter. It then frees the underlying
// buffer if the counter reaches 0 as a result of this call, does nothing
// otherwise, or if Free has already been called. All subsequent calls to
// ReadOnlyData will panic. Noop if receiver is nil.
func (b *Buffer) Free() {
	if b.freed {
		return
	}

	b.freed = true
	refs := b.refs.Add(-1)
	if refs == 0 && b.free != nil {
		b.free()
	}
	b.data = nil
}

// Len returns the Buffer's size.
func (b *Buffer) Len() int {
	// Convenience: io.Reader returns (n int, err error), and n is often checked
	// before err is checked. To mimic this, Len() should work on nil Buffers.
	if b == nil {
		return 0
	}
	return len(b.ReadOnlyData())
}

// Split modifies the receiver to point to the first n bytes while it returns a
// new reference to the remaining bytes. The returned Buffer functions just like
// a normal reference acquired using Ref in that the underlying buffer will only
// be freed once both the receiver and the returned Buffer are freed.
func (b *Buffer) Split(n int) *Buffer {
	if b.freed {
		panic("Cannot split freed buffer")
	}

	b.refs.Add(1)

	split := &Buffer{
		refs: b.refs,
		free: b.free,
	}

	b.data, split.data = b.data[:n], b.data[n:]

	return split
}

// String returns a string representation of the buffer. May be used for
// debugging purposes.
func (b *Buffer) String() string {
	return fmt.Sprintf("mem.Buffer(%p, data: %p, length: %d)", b, b.ReadOnlyData(), len(b.ReadOnlyData()))
}
