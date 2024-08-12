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

package mem

import (
	"fmt"
	"io"
	"slices"
	"unsafe"
)

const PoolingEnabled = false

// NewBuffer creates a new Buffer from the given data, initializing the
// reference counter to 1. The given free function is called when all references
// to the returned Buffer are released.
//
// Note that the backing array of the given data is not copied.
func NewBuffer(data *[]byte, pool BufferPool) Buffer {
	return sliceBuffer(*data)
}

// Copy creates a new Buffer from the given data, initializing the reference
// counter to 1.
//
// It acquires a []byte from the given pool and copies over the backing array
// of the given data. The []byte acquired from the pool is returned to the
// pool when all references to the returned Buffer are released.
func Copy(data []byte, pool BufferPool) Buffer {
	return sliceBuffer(slices.Clone(data))
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

// Ref invokes Buffer.Ref on each Buffer in the slice.
func (s BufferSlice) Ref() BufferSlice {
	return s
}

// Free invokes Buffer.Free() on each Buffer in the slice.
func (s BufferSlice) Free() {}

// Reader returns a new Reader for the input slice after taking references to
// each underlying buffer.
func (s BufferSlice) Reader() Reader {
	switch len(s) {
	case 0:
		return (*rawReader)(nil)
	case 1:
		data := s[0].ReadOnlyData()
		return (*rawReader)(&data)
	default:
		return &sliceReader{
			data: s.Ref(),
			len:  s.Len(),
		}
	}
}

type rawReader sliceBuffer

func (r *rawReader) Read(p []byte) (n int, err error) {
	if r == nil {
		return 0, io.EOF
	}

	switch len(*r) {
	case 0:
		return 0, io.EOF
	case len(p):
		n = copy(p, *r)
		*r = nil
	default:
		n = copy(p, *r)
		*r = (*r)[n:]
	}
	return n, nil
}

func (r *rawReader) ReadByte() (byte, error) {
	var b [1]byte
	_, err := r.Read(b[:])
	return b[0], err
}

func (r *rawReader) Close() error {
	return nil
}

func (r *rawReader) Remaining() int {
	if r == nil {
		return 0
	}
	return len(*r)
}

// String returns a string representation of the buffer. May be used for
// debugging purposes.
func (s sliceBuffer) String() string {
	return fmt.Sprintf("mem.Buffer(%p, data: %p, length: %d)", unsafe.SliceData(s), unsafe.SliceData(s), len(s))
}

func (p *tieredBufferPool) Get(size int) *[]byte {
	b := make([]byte, size)
	return &b
}

func (p *tieredBufferPool) Put(*[]byte) {
}
