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

// SharedBufferPool is a pool of buffers that can be shared.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
type SharedBufferPool interface {
	Get(length int) []byte
	Put(*[]byte)
}

// NewSimpleSharedBufferPool creates a new SimpleSharedBufferPool.
//
// # Experimental
//
// Notice: This API is EXPERIMENTAL and may be changed or removed in a
// later release.
func NewSimpleSharedBufferPool() SharedBufferPool {
	return &simpleSharedBufferPool{
		Pool: sync.Pool{
			New: func() interface{} {
				bs := make([]byte, 0)
				return &bs
			},
		},
	}
}

// simpleSharedBufferPool is a simple implementation of SharedBufferPool.
type simpleSharedBufferPool struct {
	sync.Pool
}

func (p *simpleSharedBufferPool) Get(size int) []byte {
	bs := p.Pool.Get().(*[]byte)
	if cap(*bs) < size {
		*bs = make([]byte, size)
		return *bs
	}

	return (*bs)[:size]
}

func (p *simpleSharedBufferPool) Put(bs *[]byte) {
	p.Pool.Put(bs)
}
