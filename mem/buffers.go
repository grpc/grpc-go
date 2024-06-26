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
Package mem provides utilities that facilitate reusing memory. This involves
pulling Buffer instances from a BufferPool and freeing the Buffers when done,
returning them to the pool. Each buffer is reference counted, allowing different
goroutines to concurrently access then free the data without explicit
synchronization by leveraging multiple references.

By convention, any APIs that return (mem.BufferSlice, error) should reduce the
burden on the caller by never returning a mem.BufferSlice that needs to be freed
if the error is non-nil, unless explicitly listed in the API's documentation.
*/
package mem

import (
	"io"
	"sync/atomic"
)

// A Buffer represents a piece of data (in bytes) that can be referenced then
// later freed, returning it to its original pool. Note that a Buffer is not safe
// for concurrent access and instead each goroutine should use its own reference
// to the data (which can be acquired with Ref). Instances of Buffer can only be
// acquired by NewBuffer, and the given free function will only be invoked when
// Free is invoked on all references.
type Buffer struct {
	data  []byte
	refs  *atomic.Int32
	free  func([]byte)
	freed bool
}

// NewBuffer creates a new buffer from the given data, initializing the reference
// counter to 1.
func NewBuffer(data []byte, free func([]byte)) *Buffer {
	b := &Buffer{data: data, refs: new(atomic.Int32), free: free}
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
	if b == nil || b.freed {
		return
	}

	b.freed = true
	refs := b.refs.Add(-1)
	if refs == 0 && b.free != nil {
		b.free(b.data)
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

// Split creates a new reference to the first n bytes of the underlying buffer,
// and modifies the receiver to only point to the remaining bytes. The returned
// Buffer functions just like a normal reference acquired using Ref in that the
// underlying buffer will only be freed once both the receiver and the new Buffer
// are freed.
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

// BufferSlice offers a means to represent data that spans one or more Buffer
// instances.
type BufferSlice []*Buffer

// Len returns the sum of the length of all the Buffers in this slice. Warning:
// invoking the built-in len on a BufferSlice will return the number of buffers
// in the slice, and *not* the value returned by this function.
func (s BufferSlice) Len() (length int) {
	for _, b := range s {
		length += b.Len()
	}
	return length
}

// Ref returns a new BufferSlice containing a new reference of each Buffer in the
// input slice.
func (s BufferSlice) Ref() BufferSlice {
	out := make(BufferSlice, len(s))
	for i, b := range s {
		out[i] = b.Ref()
	}
	return out
}

// Free invokes Buffer.Free on each Buffer in the slice.
func (s BufferSlice) Free() {
	for _, b := range s {
		b.Free()
	}
}

// CopyTo copies each of the underlying Buffer's data into the given buffer,
// returning the number of bytes copied.
func (s BufferSlice) CopyTo(out []byte) int {
	off := 0
	for _, b := range s {
		off += copy(out[off:], b.ReadOnlyData())
	}
	return off
}

// Materialize concatenates all the underlying Buffer's data into a single
// contiguous buffer using CopyTo.
func (s BufferSlice) Materialize() []byte {
	out := make([]byte, s.Len())
	s.CopyTo(out)
	return out
}

// LazyMaterialize functions like Materialize except that it writes the data to a
// single Buffer pulled from the given BufferPool. As a special case, if the
// input BufferSlice only actually has one Buffer, this function has nothing to
// do and simply returns said Buffer, hence it being "lazy".
func (s BufferSlice) LazyMaterialize(pool BufferPool) *Buffer {
	if len(s) == 1 {
		return s[0].Ref()
	}
	buf := pool.Get(s.Len())
	s.CopyTo(buf)
	return NewBuffer(buf, pool.Put)
}

// Reader returns a new Reader for the input slice.
func (s BufferSlice) Reader() *Reader {
	return &Reader{
		data: s,
		len:  s.Len(),
	}
}

var _ io.Reader = (*Reader)(nil)

// Reader exposes a BufferSlice's data as an io.Reader, allowing it to interface
// with other parts systems. It also provides an additional convenience method
// Remaining which returns the number of unread bytes remaining in the slice.
type Reader struct {
	data BufferSlice
	len  int
	idx  int
}

// Remaining returns the number of unread bytes remaining in the slice.
func (r *Reader) Remaining() int {
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

var _ io.Writer = (*writer)(nil)

type writer struct {
	buffers *BufferSlice
	pool    BufferPool
}

func (w *writer) Write(p []byte) (n int, err error) {
	b := Copy(p, w.pool)
	*w.buffers = append(*w.buffers, b)
	return b.Len(), nil
}

// NewWriter wraps the given BufferSlice and BufferPool to implement the
// io.Writer interface. Every call to Write copies the contents of the given
// buffer into a new Buffer pulled from the given pool and the Buffer is added to
// the given BufferSlice. For example, in the context of a http.Handler, the
// following code can be used to copy the contents of a request into a
// BufferSlice:
//
//	var out BufferSlice
//	n, err := io.Copy(mem.NewWriter(&out, pool), req.Body)
func NewWriter(buffers *BufferSlice, pool BufferPool) io.Writer {
	return &writer{buffers: buffers, pool: pool}
}
