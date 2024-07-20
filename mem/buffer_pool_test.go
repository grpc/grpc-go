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

package mem

import "testing"

func TestSharedBufferPool(t *testing.T) {
	pools := []BufferPool{
		NopBufferPool{},
		NewBufferPool(defaultBufferPoolSizes...),
	}

	testSizes := append(defaultBufferPoolSizes, 1<<20+1)

	for _, p := range pools {
		for _, l := range testSizes {
			bs := p.Get(l)
			if len(bs) != l {
				t.Fatalf("Expected buffer of length %d, got %d", l, len(bs))
			}

			p.Put(bs)
		}
	}
}

func TestTieredBufferPool(t *testing.T) {
	pool := &tieredBufferPool{
		sizedPools: []*sizedBufferPool{
			newBufferPool(10),
			newBufferPool(20),
		},
	}
	buf := pool.Get(1)
	if cap(buf) != 10 {
		t.Fatalf("Unexpected buffer capacity: %d", cap(buf))
	}

	// Insert a short buffer into the pool, which is currently empty.
	pool.Put(make([]byte, 1))
	// Then immediately request a buffer that would be pulled from the pool where the
	// short buffer would have been returned. If the short buffer is pulled from the
	// pool, it could cause a panic.
	pool.Get(10)
}
