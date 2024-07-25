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
	"io"
)

// BufferSlice offers a means to represent data that spans one or more Buffer
// instances. A BufferSlice is meant to be immutable after creation, and methods
// like Ref create and return copies of the slice. This is why all methods have
// value receivers rather than pointer receivers.
//
// Note that any of the methods that read the underlying buffers such as Ref,
// Len or CopyTo etc., will panic if any underlying buffers have already been
// freed. It is recommended to not directly interact with any of the underlying
// buffers directly, rather such interactions should be mediated through the
// various methods on this type.
//
// By convention, any APIs that return (mem.BufferSlice, error) should reduce
// the burden on the caller by never returning a mem.BufferSlice that needs to
// be freed if the error is non-nil, unless explicitly stated.
type BufferSlice []*Buffer

// Len returns the sum of the length of all the Buffers in this slice.
//
// # Warning
//
// Invoking the built-in len on a BufferSlice will return the number of buffers
// in the slice, and *not* the value returned by this function.
func (s BufferSlice) Len() int {
	var length int
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

// Free invokes Buffer.Free() on each Buffer in the slice.
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
	l := s.Len()
	if l == 0 {
		return nil
	}
	out := make([]byte, l)
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

// Reader returns a new Reader for the input slice after taking references to
// each underlying buffer.
func (s BufferSlice) Reader() *Reader {
	return &Reader{
		data: s.Ref(),
		len:  s.Len(),
	}
}

var _ io.ReadCloser = (*Reader)(nil)

// Reader exposes a BufferSlice's data as an io.Reader, allowing it to interface
// with other parts systems. It also provides an additional convenience method
// Remaining(), which returns the number of unread bytes remaining in the slice.
//
// Note that reading data from the reader does not free the underlying buffers!
// Only calling Close once all data is read will free the buffers.
type Reader struct {
	data BufferSlice
	len  int
	// The index into data[0].ReadOnlyData().
	bufferIdx int
}

// Remaining returns the number of unread bytes remaining in the slice.
func (r *Reader) Remaining() int {
	return r.len
}

// Close frees the underlying BufferSlice and never returns an error. Subsequent
// calls to Read will return (0, io.EOF).
func (r *Reader) Close() error {
	r.data.Free()
	r.data = nil
	r.len = 0
	return nil
}

func (r *Reader) Read(buf []byte) (n int, _ error) {
	if r.len == 0 {
		return 0, io.EOF
	}

	for len(buf) != 0 && r.len != 0 {
		// Copy as much as possible from the first Buffer in the slice into the
		// given byte slice.
		data := r.data[0].ReadOnlyData()
		copied := copy(buf, data[r.bufferIdx:])
		r.len -= copied       // Reduce len by the number of bytes copied.
		r.bufferIdx += copied // Increment the buffer index.
		n += copied           // Increment the total number of bytes read.
		buf = buf[copied:]    // Shrink the given byte slice.

		// If we have copied all of the data from the first Buffer, free it and
		// advance to the next in the slice.
		if r.bufferIdx == len(data) {
			oldBuffer := r.data[0]
			oldBuffer.Free()
			r.data = r.data[1:]
			r.bufferIdx = 0
		}
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
//	var out mem.BufferSlice
//	n, err := io.Copy(mem.NewWriter(&out, pool), req.Body)
func NewWriter(buffers *BufferSlice, pool BufferPool) io.Writer {
	return &writer{buffers: buffers, pool: pool}
}
