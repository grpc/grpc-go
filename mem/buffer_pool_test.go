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
	"testing"

	"github.com/google/go-cmp/cmp"
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
			if len(bs) != l {
				t.Fatalf("Get(%d) returned buffer of length %d, want %d", l, len(bs), l)
			}

			p.Put(bs)
		}
	}
}

func (s) TestBufferPoolClears(t *testing.T) {
	pool := mem.NewTieredBufferPool(4)

	buf := pool.Get(4)
	copy(buf, "1234")
	pool.Put(buf)

	if !cmp.Equal(buf, make([]byte, 4)) {
		t.Fatalf("buffer not cleared")
	}
}

func (s) TestBufferPoolIgnoresShortBuffers(t *testing.T) {
	pool := mem.NewTieredBufferPool(10, 20)
	buf := pool.Get(1)
	if cap(buf) != 10 {
		t.Fatalf("Get(1) returned buffer with capacity: %d, want 10", cap(buf))
	}

	// Insert a short buffer into the pool, which is currently empty.
	pool.Put(make([]byte, 1))
	// Then immediately request a buffer that would be pulled from the pool where the
	// short buffer would have been returned. If the short buffer is pulled from the
	// pool, it could cause a panic.
	pool.Get(10)
}
