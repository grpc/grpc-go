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

/*
Package mem does memory things
*/
package mem

import (
	"io"
	"sync/atomic"
)

type BufferSlice []*Buffer

type Buffer struct {
	data  []byte
	refs  *atomic.Int32
	free  func([]byte)
	freed bool
}

func NewBuffer(data []byte, free func([]byte)) *Buffer {
	return (&Buffer{data: data, refs: new(atomic.Int32), free: free}).Ref()
}

func Copy(data []byte, pool BufferPool) *Buffer {
	buf := pool.Get(len(data))
	copy(buf, data)
	return NewBuffer(buf, pool.Put)
}

func (b *Buffer) ReadOnlyData() []byte {
	if b.freed {
		panic("Cannot read freed buffer")
	}
	return b.data
}

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

func (b *Buffer) Free() {
	if b.freed {
		return
	}

	b.freed = true
	refs := b.refs.Add(-1)
	if refs == 0 && b.free != nil {
		b.free(b.data)
	}
	b.data = nil
}

func (b *Buffer) Len() int {
	// Convenience: io.Reader returns (n int, err error), and n is often checked
	// before err is checked. To mimic this, Len() should work on nil Buffers.
	if b == nil {
		return 0
	}
	return len(b.ReadOnlyData())
}

func (b *Buffer) Split(n int) *Buffer {
	if b.freed {
		panic("Cannot split freed buffer")
	}

	b.refs.Add(1)

	data := b.data
	var free func([]byte)
	if f := b.free; f != nil {
		free = func(_ []byte) {
			f(data)
		}
	}

	b.data = data[:n]
	b.free = nil

	return &Buffer{
		data: data[n:],
		refs: b.refs,
		free: free,
	}
}

type Writer struct {
	buffers *BufferSlice
	pool    BufferPool
}

func (s *Writer) Write(p []byte) (n int, err error) {
	buf := s.pool.Get(len(p))
	n = copy(buf, p)

	*s.buffers = append(*s.buffers, NewBuffer(buf, s.pool.Put))

	return n, nil
}

func NewWriter(buffers *BufferSlice, pool BufferPool) *Writer {
	return &Writer{buffers: buffers, pool: pool}
}

type Reader struct {
	data BufferSlice
	len  int
	idx  int
}

func (r *Reader) Len() int {
	return r.len
}

func (r *Reader) Read(buf []byte) (n int, _ error) {
	for len(buf) != 0 && r.len != 0 {
		data := r.data[0].ReadOnlyData()
		copied := copy(buf, data[r.idx:])
		r.len -= copied

		buf = buf[copied:]

		if copied == len(data) {
			r.data = r.data[1:]
			r.idx = 0
		} else {
			r.idx += copied
		}
		n += copied
	}

	if n == 0 {
		return 0, io.EOF
	}

	return n, nil
}

func (s BufferSlice) Reader() *Reader {
	return &Reader{
		data: s,
		len:  s.Len(),
	}
}

func (s BufferSlice) Len() (length int) {
	for _, b := range s {
		length += b.Len()
	}
	return length
}

func (s BufferSlice) Ref() BufferSlice {
	out := make(BufferSlice, len(s))
	for i, b := range s {
		out[i] = b.Ref()
	}
	return out
}

func (s BufferSlice) Free() {
	for _, b := range s {
		b.Free()
	}
}

func (s BufferSlice) WriteTo(out []byte) {
	out = out[:0]
	for _, b := range s {
		out = append(out, b.ReadOnlyData()...)
	}
}

func (s BufferSlice) Materialize() []byte {
	out := make([]byte, s.Len())
	s.WriteTo(out)
	return out
}

func (s BufferSlice) LazyMaterialize(pool BufferPool) *Buffer {
	if len(s) == 1 {
		return s[0].Ref()
	}
	buf := pool.Get(s.Len())
	s.WriteTo(buf)
	return NewBuffer(buf, pool.Put)
}
