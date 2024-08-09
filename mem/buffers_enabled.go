//go:build buffer_pooling

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
)

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

// NewBuffer creates a new Buffer from the given data, initializing the
// reference counter to 1. The given free function is called when all references
// to the returned Buffer are released.
//
// Note that the backing array of the given data is not copied.
func NewBuffer(data []byte, onFree func(*[]byte)) Buffer {
	b := newBuffer()
	b.data = data
	b.refs = new(atomic.Int32)
	if onFree != nil {
		b.free = func() { onFree(&data) }
	}
	b.refs.Add(1)
	return b
}

func (b *buffer) ReadOnlyData() []byte {
	if b.refs == nil {
		panic("Cannot read freed buffer")
	}
	return b.data
}

func (b *buffer) Ref() Buffer {
	if b.refs == nil {
		panic("Cannot ref freed buffer")
	}
	b.refs.Add(1)
	return b
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
