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

func ReadUnsafe(dst []byte, buf Buffer) (int, Buffer) {
	return buf.read(dst)
}

// SplitUnsafe modifies the receiver to point to the first n bytes while it
// returns a new reference to the remaining bytes. The returned Buffer functions
// just like a normal reference acquired using Ref().
func SplitUnsafe(buf Buffer, n int) (left, right Buffer) {
	return buf.split(n)
}

type emptyBuffer struct{}

func (e emptyBuffer) ReadOnlyData() []byte {
	return nil
}

func (e emptyBuffer) Ref()  {}
func (e emptyBuffer) Free() {}

func (e emptyBuffer) Len() int {
	return 0
}

func (e emptyBuffer) split(n int) (left, right Buffer) {
	return e, e
}

func (e emptyBuffer) read(buf []byte) (int, Buffer) {
	return 0, e
}

type sliceBuffer []byte

func (s sliceBuffer) ReadOnlyData() []byte { return s }
func (s sliceBuffer) Ref()                 {}
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
