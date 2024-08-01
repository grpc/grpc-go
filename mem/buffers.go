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
	"sync/atomic"
)

// A Buffer represents a reference counted piece of data (in bytes) that can be
// acquired by a call to NewBuffer() or Copy(). A reference to a Buffer may be
// released by calling Free(), which invokes the given free function only after
// all references are released.
//
// Note that a Buffer is not safe for concurrent access and instead each
// goroutine should use its own reference to the data, which can be acquired via
// a call to Ref().
//
// Attempts to access the underlying data after releasing the reference to the
// Buffer will panic.
type Buffer struct {
	data  []byte
	refs  *atomic.Int32
	free  func()
	freed bool
}

// NewBuffer creates a new Buffer from the given data, initializing the
// reference counter to 1. The given free function is called when all references
// to the returned Buffer are released.
//
// Note that the backing array of the given data is not copied.
func NewBuffer(data []byte, onFree func([]byte)) *Buffer {
	b := &Buffer{data: data, refs: new(atomic.Int32)}
	if onFree != nil {
		b.free = func() { onFree(data) }
	}
	b.refs.Add(1)
	return b
}

// Copy creates a new Buffer from the given data, initializing the reference
// counter to 1.
//
// It acquires a []byte from the given pool and copies over the backing array
// of the given data. The []byte acquired from the pool is returned to the
// pool when all references to the returned Buffer are released.
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

// Ref returns a new reference to this Buffer's underlying byte slice.
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

// Free decrements this Buffer's reference counter and frees the underlying
// byte slice if the counter reaches 0 as a result of this call.
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
// a normal reference acquired using Ref().
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
