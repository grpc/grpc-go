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
	"sync"
	"sync/atomic"
	"unsafe"
)

// A Buffer represents a reference counted piece of data (in bytes) that can be
// acquired by a call to NewBuffer() or Copy(). A reference to a Buffer may be
// released by calling Free(), which invokes the free function given at creation
// only after all references are released.
//
// Note that a Buffer is not safe for concurrent access and instead each
// goroutine should use its own reference to the data, which can be acquired via
// a call to Ref().
//
// Attempts to access the underlying data after releasing the reference to the
// Buffer will panic.
type Buffer interface {
	// ReadOnlyData returns the underlying byte slice. Note that it is undefined
	// behavior to modify the contents of this slice in any way.
	ReadOnlyData() []byte
	// Ref increases the reference counter for this Buffer.
	Ref()
	// Free decrements this Buffer's reference counter and frees the underlying
	// byte slice if the counter reaches 0 as a result of this call.
	Free()
	// Len returns the Buffer's size.
	Len() int

	split(n int) (left, right Buffer)
	read(buf []byte) (int, Buffer)
}

var bufferObjectPool = sync.Pool{New: func() any {
	return new(buffer)
}}

type buffer struct {
	data []byte
	refs *atomic.Int32
	free func()
}

func newBuffer() *buffer {
	return bufferObjectPool.Get().(*buffer)
}

var magic = 1 << 10

func SetMagic(m int) {
	magic = m
}

// NewBuffer creates a new Buffer from the given data, initializing the
// reference counter to 1. The given free function is called when all references
// to the returned Buffer are released.
//
// Note that the backing array of the given data is not copied.
func NewBuffer(data []byte, onFree func(*[]byte)) Buffer {
	if len(data) < magic {
		return (sliceBuffer)(data)
	}
	b := newBuffer()
	b.data = data
	b.refs = new(atomic.Int32)
	if onFree != nil {
		b.free = func() { onFree(&data) }
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
func Copy(data []byte, pool BufferPool) Buffer {
	buf := pool.Get(len(data))
	copy(buf, data)
	return NewBuffer(buf, pool.Put)
}

func (b *buffer) ReadOnlyData() []byte {
	if b.refs == nil {
		panic("Cannot read freed buffer")
	}
	return b.data
}

func (b *buffer) Ref() {
	if b.refs == nil {
		panic("Cannot ref freed buffer")
	}
	b.refs.Add(1)
}

func (b *buffer) Free() {
	if b.refs == nil {
		panic("Cannot free freed buffer")
	}

	refs := b.refs.Add(-1)
	if refs == 0 && b.free != nil {
		b.free()
	}

	b.data = nil
	b.refs = nil
	b.free = nil
	bufferObjectPool.Put(b)
}

func (b *buffer) Len() int {
	return len(b.ReadOnlyData())
}

func (b *buffer) split(n int) (Buffer, Buffer) {
	if b.refs == nil {
		panic("Cannot split freed buffer")
	}

	b.refs.Add(1)
	split := newBuffer()
	split.data = b.data[n:]
	split.refs = b.refs
	split.free = b.free

	b.data = b.data[:n]

	return b, split
}

func (b *buffer) read(buf []byte) (int, Buffer) {
	if b.refs == nil {
		panic("Cannot read freed buffer")
	}

	n := copy(buf, b.data)
	if n == len(b.data) {
		b.Free()
		return n, nil
	}

	b.data = b.data[n:]
	return n, b
}

// String returns a string representation of the buffer. May be used for
// debugging purposes.
func (b *buffer) String() string {
	return fmt.Sprintf("mem.Buffer(%p, data: %p, length: %d)", b, b.ReadOnlyData(), len(b.ReadOnlyData()))
}

type sliceBuffer []byte

func (s sliceBuffer) ReadOnlyData() []byte {
	return s
}

func (s sliceBuffer) Ref() {}

func (s sliceBuffer) Free() {
}

func (s sliceBuffer) Len() int {
	return len(s)
}

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

func ReadUnsafe(dst []byte, buf Buffer) (int, Buffer) {
	return buf.read(dst)
}

// SplitUnsafe modifies the receiver to point to the first n bytes while it
// returns a new reference to the remaining bytes. The returned Buffer functions
// just like a normal reference acquired using Ref().
func SplitUnsafe(buf Buffer, n int) (left, right Buffer) {
	return buf.split(n)
}
