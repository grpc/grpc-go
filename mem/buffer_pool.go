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
// decreased memory allocation.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
type BufferPool interface {
	// Get returns a buffer with specified length from the pool.
	Get(length int) []byte

	// Put returns a buffer to the pool.
	Put([]byte)
}

var defaultBufferPoolSizes = []int{
	256,
	4 << 10,  // 4KB (go page size)
	16 << 10, // 16KB (max HTTP/2 frame size used by gRPC)
	32 << 10, // 32KB (default buffer size for io.Copy)
	// TODO: Report the buffer sizes requested with Get to tune this properly.
	1 << 20, // 1MB
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
	pools := make([]*sizedBufferPool, len(poolSizes))
	for i, s := range poolSizes {
		pools[i] = newBufferPool(s)
	}
	return &tieredBufferPool{
		sizedPools:   pools,
		fallbackPool: new(simpleBufferPool),
	}
}

// tieredBufferPool implements the BufferPool interface with multiple tiers of
// buffer pools for different sizes of buffers.
type tieredBufferPool struct {
	sizedPools   []*sizedBufferPool
	fallbackPool *simpleBufferPool
}

func (p *tieredBufferPool) Get(size int) []byte {
	return p.getPool(size).Get(size)
}

func (p *tieredBufferPool) Put(buf []byte) {
	p.getPool(len(buf)).Put(buf)
}

func (p *tieredBufferPool) getPool(size int) BufferPool {
	poolIdx := sort.Search(len(p.sizedPools), func(i int) bool {
		return p.sizedPools[i].defaultSize >= size
	})

	if poolIdx == len(p.sizedPools) {
		return p.fallbackPool
	}

	return p.sizedPools[poolIdx]
}

// sizedBufferPool is a BufferPool implementation that is optimized for specific
// buffer sizes. For example, HTTP/2 frames within grpc are always 16kb and a
// sizedBufferPool can be configured to only return buffers with a capacity of
// 16kb. Note that however it does not support returning larger buffers and in
// fact panics if such a buffer is requested.
type sizedBufferPool struct {
	pool        sync.Pool
	defaultSize int
}

func (p *sizedBufferPool) Get(size int) []byte {
	bs := *p.pool.Get().(*[]byte)
	return bs[:size]
}

func (p *sizedBufferPool) Put(buf []byte) {
	buf = buf[:cap(buf)]
	clear(buf)
	p.pool.Put(&buf)
}

func newBufferPool(size int) *sizedBufferPool {
	return &sizedBufferPool{
		pool: sync.Pool{
			New: func() any {
				buf := make([]byte, size)
				return &buf
			},
		},
		defaultSize: size,
	}
}

var _ BufferPool = (*simpleBufferPool)(nil)

// simpleBufferPool is an implementation of the BufferPool interface that
// attempts to pool buffers with a sync.Pool. When Get is invoked, it tries to
// acquire a buffer from the pool but if that buffer is too small, it returns it
// to the pool and creates a new one.
type simpleBufferPool struct {
	pool sync.Pool
}

func (p *simpleBufferPool) Get(size int) []byte {
	bs, ok := p.pool.Get().(*[]byte)
	if ok && cap(*bs) >= size {
		return (*bs)[:size]
	}

	if ok {
		p.pool.Put(bs)
	}

	return make([]byte, size)
}

func (p *simpleBufferPool) Put(buf []byte) {
	buf = buf[:cap(buf)]
	clear(buf)
	p.pool.Put(&buf)
}

var _ BufferPool = NopBufferPool{}

// NopBufferPool is a buffer pool just makes new buffer without pooling.
type NopBufferPool struct{}

func (NopBufferPool) Get(length int) []byte {
	return make([]byte, length)
}

func (NopBufferPool) Put([]byte) {
}
