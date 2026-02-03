/*
 *
 * Copyright 2023 gRPC authors.
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
	"fmt"
	"testing"
	"unsafe"

	"google.golang.org/grpc/mem"
)

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
	const poolSize = 4
	pool := mem.NewTieredBufferPool(poolSize)
	tests := []struct {
		name       string
		bufferSize int
	}{
		{
			name:       "sized_buffer_pool",
			bufferSize: poolSize,
		},
		{
			name:       "simple_buffer_pool",
			bufferSize: poolSize + 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for {
				buf1 := pool.Get(tc.bufferSize)
				copy(*buf1, "1234")
				pool.Put(buf1)

				buf2 := pool.Get(tc.bufferSize)
				if unsafe.SliceData(*buf1) != unsafe.SliceData(*buf2) {
					pool.Put(buf2)
					// This test is only relevant if a buffer is reused, otherwise try again. This
					// can happen if a GC pause happens between putting the buffer back in the pool
					// and getting a new one.
					continue
				}

				if !bytes.Equal(*buf1, make([]byte, tc.bufferSize)) {
					t.Fatalf("buffer not cleared")
				}
				break
			}
		})
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

func TestBinaryBufferPool(t *testing.T) {
	poolSizes := []uint8{0, 2, 3, 4}

	testCases := []struct {
		requestSize  int
		wantCapacity int
	}{
		{requestSize: 0, wantCapacity: 0},
		{requestSize: 1, wantCapacity: 1},
		{requestSize: 2, wantCapacity: 4},
		{requestSize: 3, wantCapacity: 4},
		{requestSize: 4, wantCapacity: 4},
		{requestSize: 5, wantCapacity: 8},
		{requestSize: 6, wantCapacity: 8},
		{requestSize: 7, wantCapacity: 8},
		{requestSize: 8, wantCapacity: 8},
		{requestSize: 9, wantCapacity: 16},
		{requestSize: 15, wantCapacity: 16},
		{requestSize: 16, wantCapacity: 16},
		{requestSize: 17, wantCapacity: 4096}, // fallback pool returns sizes in multiples of 4096.
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("requestSize=%d", tc.requestSize), func(t *testing.T) {
			pool, err := mem.NewBinaryTieredBufferPool(poolSizes...)
			if err != nil {
				t.Fatalf("Failed to create buffer pool: %v", err)
			}
			buf := pool.Get(tc.requestSize)
			if cap(*buf) != tc.wantCapacity {
				t.Errorf("Get(%d) returned buffer with capacity: %d, want %d", tc.requestSize, cap(*buf), tc.wantCapacity)
			}
			if len(*buf) != tc.requestSize {
				t.Errorf("Get(%d) returned buffer with length: %d, want %d", tc.requestSize, len(*buf), tc.requestSize)
			}
			pool.Put(buf)
		})
	}
}
