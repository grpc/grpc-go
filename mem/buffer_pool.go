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
	"sort"
	"sync"
	"sync/atomic"
)

// BufferPool is a pool of buffers that can be shared, resulting in
// decreased memory allocation. Currently, in gRPC-go, it is only utilized
// for parsing incoming messages.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
type BufferPool interface {
	// Get returns a buffer with specified length from the pool.
	//
	// The returned byte slice may be not zero initialized.
	Get(length int) []byte

	// Put returns a buffer to the pool.
	Put([]byte)
}

var defaultBufferPoolSizes = []int{
	256,
	4 << 10,  // 4KB
	16 << 10, // 16KB (max HTTP/2 frame size used by gRPC)
	64 << 10, // 64KB
	1 << 20,  // 1MB
}

var defaultBufferPool = func() *atomic.Pointer[BufferPool] {
	pool := NewBufferPool(defaultBufferPoolSizes...)
	ptr := new(atomic.Pointer[BufferPool])
	ptr.Store(&pool)
	return ptr
}()

func DefaultBufferPool() BufferPool {
	return *defaultBufferPool.Load()
}

func SetDefaultBufferPool(pool BufferPool) {
	defaultBufferPool.Store(&pool)
}

func NewBufferPool(poolSizes ...int) BufferPool {
	sort.Ints(poolSizes)
	pools := make([]*bufferPool, len(poolSizes))
	for i, s := range poolSizes {
		pools[i] = newBufferPool(s)
	}
	return &simpleBufferPool{
		sizedPools:   pools,
		fallbackPool: newBufferPool(0),
	}
}

// simpleBufferPool is a simple implementation of BufferPool.
type simpleBufferPool struct {
	sizedPools   []*bufferPool
	fallbackPool *bufferPool
}

func (p *simpleBufferPool) Get(size int) []byte {
	return p.getPool(size).Get(size)
}

func (p *simpleBufferPool) Put(buf []byte) {
	p.getPool(len(buf)).Put(buf)
}

func (p *simpleBufferPool) getPool(size int) *bufferPool {
	poolIdx := sort.Search(len(p.sizedPools), func(i int) bool {
		return p.sizedPools[i].defaultSize >= size
	})

	if poolIdx == len(p.sizedPools) {
		return p.fallbackPool
	}

	return p.sizedPools[poolIdx]
}

type bufferPool struct {
	pool        sync.Pool
	defaultSize int
}

func (p *bufferPool) Get(size int) []byte {
	bs := *p.pool.Get().(*[]byte)

	if cap(bs) < size {
		p.pool.Put(&bs)

		return make([]byte, size)
	}

	return bs[:size]
}

func (p *bufferPool) Put(buf []byte) {
	buf = buf[:cap(buf)]
	// TODO: replace this loop with `clear`, though the compiler should be smart
	// enough to optimize this.
	for i := range buf {
		buf[i] = 0
	}
	p.pool.Put(&buf)
}

func newBufferPool(size int) *bufferPool {
	return &bufferPool{
		pool: sync.Pool{
			New: func() any {
				buf := make([]byte, size)
				return &buf
			},
		},
		defaultSize: size,
	}
}

var _ BufferPool = NopBufferPool{}

// NopBufferPool is a buffer pool just makes new buffer without pooling.
type NopBufferPool struct{}

func (NopBufferPool) Get(length int) []byte {
	return make([]byte, length)
}

func (NopBufferPool) Put([]byte) {
}
