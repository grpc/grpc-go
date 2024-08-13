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

package mem_test

import (
	"bytes"
	"testing"
	"unsafe"

	"google.golang.org/grpc/mem"
)

// Tests that a buffer created with NewBuffer, which when later freed, invokes
// the free function with the correct data.
func (s) TestBuffer_NewBufferAndFree(t *testing.T) {
	data := "abcd"
	freed := false
	freeF := poolFunc(func(got *[]byte) {
		if !bytes.Equal(*got, []byte(data)) {
			t.Fatalf("Free function called with bytes %s, want %s", string(*got), data)
		}
		freed = true
	})

	buf := newBuffer([]byte(data), freeF)
	if got := buf.ReadOnlyData(); !bytes.Equal(got, []byte(data)) {
		t.Fatalf("Buffer contains data %s, want %s", string(got), string(data))
	}

	// Verify that the free function is invoked when all references are freed.
	buf.Free()
	if !freed {
		t.Fatalf("Buffer not freed")
	}
}

// Tests that a buffer created with NewBuffer, on which an additional reference
// is acquired, which when later freed, invokes the free function with the
// correct data, but only after all references are released.
func (s) TestBuffer_NewBufferRefAndFree(t *testing.T) {
	data := "abcd"
	freed := false
	freeF := poolFunc(func(got *[]byte) {
		if !bytes.Equal(*got, []byte(data)) {
			t.Fatalf("Free function called with bytes %s, want %s", string(*got), string(data))
		}
		freed = true
	})

	buf := newBuffer([]byte(data), freeF)
	if got := buf.ReadOnlyData(); !bytes.Equal(got, []byte(data)) {
		t.Fatalf("Buffer contains data %s, want %s", string(got), string(data))
	}

	buf.Ref()
	if got := buf.ReadOnlyData(); !bytes.Equal(got, []byte(data)) {
		t.Fatalf("New reference to the Buffer contains data %s, want %s", string(got), string(data))
	}

	// Verify that the free function is not invoked when all references are yet
	// to be freed.
	buf.Free()
	if freed {
		t.Fatalf("Free function called before all references freed")
	}

	// Verify that the free function is invoked when all references are freed.
	buf.Free()
	if !freed {
		t.Fatalf("Buffer not freed")
	}
}

func (s) TestBuffer_FreeAfterFree(t *testing.T) {
	buf := newBuffer([]byte("abcd"), mem.NopBufferPool{})
	if buf.Len() != 4 {
		t.Fatalf("Buffer length is %d, want 4", buf.Len())
	}

	// Ensure that a double free does panic.
	buf.Free()
	defer checkForPanic(t, "Cannot free freed buffer")
	buf.Free()
}

type singleBufferPool struct {
	t    *testing.T
	data *[]byte
}

func (s *singleBufferPool) Get(length int) *[]byte {
	if len(*s.data) != length {
		s.t.Fatalf("Invalid requested length, got %d want %d", length, len(*s.data))
	}
	return s.data
}

func (s *singleBufferPool) Put(b *[]byte) {
	if s.data != b {
		s.t.Fatalf("Wrong buffer returned to pool, got %p want %p", b, s.data)
	}
	s.data = nil
}

// Tests that a buffer created with Copy, which when later freed, returns the underlying
// byte slice to the buffer pool.
func (s) TestBuffer_CopyAndFree(t *testing.T) {
	data := []byte("abcd")
	testPool := &singleBufferPool{
		t:    t,
		data: &data,
	}

	buf := mem.Copy(data, testPool)
	if got := buf.ReadOnlyData(); !bytes.Equal(got, data) {
		t.Fatalf("Buffer contains data %s, want %s", string(got), string(data))
	}

	// Verify that the free function is invoked when all references are freed.
	buf.Free()
	if testPool.data != nil {
		t.Fatalf("Buffer not freed")
	}
}

// Tests that a buffer created with Copy, on which an additional reference is
// acquired, which when later freed, returns the underlying byte slice to the
// buffer pool.
func (s) TestBuffer_CopyRefAndFree(t *testing.T) {
	data := []byte("abcd")
	testPool := &singleBufferPool{
		t:    t,
		data: &data,
	}

	buf := mem.Copy(data, testPool)
	if got := buf.ReadOnlyData(); !bytes.Equal(got, data) {
		t.Fatalf("Buffer contains data %s, want %s", string(got), string(data))
	}

	buf.Ref()
	if got := buf.ReadOnlyData(); !bytes.Equal(got, []byte(data)) {
		t.Fatalf("New reference to the Buffer contains data %s, want %s", string(got), string(data))
	}

	// Verify that the free function is not invoked when all references are yet
	// to be freed.
	buf.Free()
	if testPool.data == nil {
		t.Fatalf("Free function called before all references freed")
	}

	// Verify that the free function is invoked when all references are freed.
	buf.Free()
	if testPool.data != nil {
		t.Fatalf("Free never called")
	}
}

func (s) TestBuffer_ReadOnlyDataAfterFree(t *testing.T) {
	// Verify that reading before freeing does not panic.
	buf := newBuffer([]byte("abcd"), mem.NopBufferPool{})
	buf.ReadOnlyData()

	buf.Free()
	defer checkForPanic(t, "Cannot read freed buffer")
	buf.ReadOnlyData()
}

func (s) TestBuffer_RefAfterFree(t *testing.T) {
	// Verify that acquiring a ref before freeing does not panic.
	buf := newBuffer([]byte("abcd"), mem.NopBufferPool{})
	buf.Ref()

	// This first call should not panc and bring the ref counter down to 1
	buf.Free()
	// This second call actually frees the buffer
	buf.Free()
	defer checkForPanic(t, "Cannot ref freed buffer")
	buf.Ref()
}

func (s) TestBuffer_SplitAfterFree(t *testing.T) {
	// Verify that splitting before freeing does not panic.
	buf := newBuffer([]byte("abcd"), mem.NopBufferPool{})
	buf, bufSplit := mem.SplitUnsafe(buf, 2)
	defer bufSplit.Free()

	buf.Free()
	defer checkForPanic(t, "Cannot split freed buffer")
	mem.SplitUnsafe(buf, 2)
}

func (s) TestBufferPool(t *testing.T) {
	var poolSizes = []int{4, 8, 16, 32}
	pools := []mem.BufferPool{
		mem.NopBufferPool{},
		mem.NewTieredBufferPool(poolSizes...),
	}

	testSizes := append([]int{1}, poolSizes...)
	testSizes = append(testSizes, 64)

	for _, p := range pools {
		for _, l := range testSizes {
			bs := p.Get(l)
			if len(*bs) != l {
				t.Fatalf("Get(%d) returned buffer of length %d, want %d", l, len(*bs), l)
			}

			p.Put(bs)
		}
	}
}

func (s) TestBufferPoolClears(t *testing.T) {
	pool := mem.NewTieredBufferPool(4)

	for {
		buf1 := pool.Get(4)
		copy(*buf1, "1234")
		pool.Put(buf1)

		buf2 := pool.Get(4)
		if unsafe.SliceData(*buf1) != unsafe.SliceData(*buf2) {
			pool.Put(buf2)
			// This test is only relevant if a buffer is reused, otherwise try again. This
			// can happen if a GC pause happens between putting the buffer back in the pool
			// and getting a new one.
			continue
		}

		if !bytes.Equal(*buf1, make([]byte, 4)) {
			t.Fatalf("buffer not cleared")
		}
		break
	}
}

func (s) TestBufferPoolIgnoresShortBuffers(t *testing.T) {
	pool := mem.NewTieredBufferPool(10, 20)
	buf := pool.Get(1)
	if cap(*buf) != 10 {
		t.Fatalf("Get(1) returned buffer with capacity: %d, want 10", cap(*buf))
	}

	// Insert a short buffer into the pool, which is currently empty.
	short := make([]byte, 1)
	pool.Put(&short)
	// Then immediately request a buffer that would be pulled from the pool where the
	// short buffer would have been returned. If the short buffer is pulled from the
	// pool, it could cause a panic.
	pool.Get(10)
}
