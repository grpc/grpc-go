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

package mem

import (
	"fmt"
	"sync"
	"sync/atomic"
)

const PoolingEnabled = true

var bufferObjectPool = sync.Pool{New: func() any {
	return new(buffer)
}}

var refObjectPool = sync.Pool{New: func() any {
	return new(atomic.Int32)
}}

type buffer struct {
	origData *[]byte
	data     []byte
	refs     *atomic.Int32
	pool     BufferPool
}

func newBuffer() *buffer {
	return bufferObjectPool.Get().(*buffer)
}

// NewBuffer creates a new Buffer from the given data, initializing the reference
// counter to 1. The data will then be returned to the given pool when all
// references to the returned Buffer are released. As a special case to avoid
// additional allocations, if the given buffer pool is nil, the returned buffer
// will be a "no-op" Buffer where invoking Buffer.Free() does nothing and the
// underlying data is never freed.
//
// Note that the backing array of the given data is not copied.
func NewBuffer(data *[]byte, pool BufferPool) Buffer {
	if pool == nil {
		return (sliceBuffer)(*data)
	}
	b := newBuffer()
	b.origData = data
	b.data = *data
	b.pool = pool
	b.refs = refObjectPool.Get().(*atomic.Int32)
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
	copy(*buf, data)
	return NewBuffer(buf, pool)
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
	switch {
	case refs > 0:
		return
	case refs == 0:
		if b.pool != nil {
			b.pool.Put(b.origData)
		}

		refObjectPool.Put(b.refs)
		b.origData = nil
		b.data = nil
		b.refs = nil
		b.pool = nil
		bufferObjectPool.Put(b)
	default:
		panic("Cannot free freed buffer")
	}
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
	split.origData = b.origData
	split.data = b.data[n:]
	split.refs = b.refs
	split.pool = b.pool

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

// Reader returns a new Reader for the input slice after taking references to
// each underlying buffer.
func (s BufferSlice) Reader() Reader {
	s.Ref()
	return &sliceReader{
		data: s,
		len:  s.Len(),
	}
}

func (p *tieredBufferPool) Get(size int) *[]byte {
	return p.getPool(size).Get(size)
}

func (p *tieredBufferPool) Put(buf *[]byte) {
	p.getPool(cap(*buf)).Put(buf)
}
