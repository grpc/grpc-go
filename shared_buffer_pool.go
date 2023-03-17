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

package grpc

import "sync"

// SharedBufferPool is a pool of buffers that can be shared, resulting in
// decreased memory allocation. Currently, in gRPC-go, it is only utilized
// for parsing incoming messages.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
type SharedBufferPool interface {
	Get(length int) []byte
	Put(*[]byte)
}

// NewSimpleSharedBufferPool creates a new SimpleSharedBufferPool with buckets
// of different sizes to optimize memory usage. This prevents the pool from
// wasting large amounts of memory, even when handling messages of varying sizes.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
func NewSimpleSharedBufferPool() SharedBufferPool {
	return &simpleSharedBufferPool{
		pool0:   makeBytesPool(),
		pool1:   makeBytesPool(),
		pool2:   makeBytesPool(),
		pool3:   makeBytesPool(),
		pool4:   makeBytesPool(),
		poolMax: makeBytesPool(),
	}
}

// simpleSharedBufferPool is a simple implementation of SharedBufferPool.
type simpleSharedBufferPool struct {
	pool0   bufferPool
	pool1   bufferPool
	pool2   bufferPool
	pool3   bufferPool
	pool4   bufferPool
	poolMax bufferPool
}

func (p *simpleSharedBufferPool) Get(size int) []byte {
	switch {
	case size <= level0PoolMaxSize:
		return p.pool0.Get(size)
	case size <= level1PoolMaxSize:
		return p.pool1.Get(size)
	case size <= level2PoolMaxSize:
		return p.pool2.Get(size)
	case size <= level3PoolMaxSize:
		return p.pool3.Get(size)
	case size <= level4PoolMaxSize:
		return p.pool4.Get(size)
	default:
		return p.poolMax.Get(size)
	}
}

func (p *simpleSharedBufferPool) Put(bs *[]byte) {
	switch size := cap(*bs); {
	case size <= level0PoolMaxSize:
		p.pool0.Put(bs)
	case size <= level1PoolMaxSize:
		p.pool1.Put(bs)
	case size <= level2PoolMaxSize:
		p.pool2.Put(bs)
	case size <= level3PoolMaxSize:
		p.pool3.Put(bs)
	case size <= level4PoolMaxSize:
		p.pool4.Put(bs)
	default:
		p.poolMax.Put(bs)
	}
}

const (
	level0PoolMaxSize = 16                     //  16  B
	level1PoolMaxSize = level0PoolMaxSize * 16 // 256  B
	level2PoolMaxSize = level1PoolMaxSize * 16 //   4 KB
	level3PoolMaxSize = level2PoolMaxSize * 16 //  64 KB
	level4PoolMaxSize = level3PoolMaxSize * 16 //   1 MB
)

type bufferPool struct {
	sync.Pool
}

func (p *bufferPool) Get(size int) []byte {
	bs := p.Pool.Get().(*[]byte)
	if cap(*bs) < size {
		*bs = make([]byte, size)
		return *bs
	}

	return (*bs)[:size]
}

func (p *bufferPool) Put(bs *[]byte) {
	p.Pool.Put(bs)
}

func makeBytesPool() bufferPool {
	return bufferPool{
		sync.Pool{
			New: func() interface{} {
				return new([]byte)
			},
		},
	}
}
