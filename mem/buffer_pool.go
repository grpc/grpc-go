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
	"math"
	"slices"

	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/mem"
)

// BufferPool is a pool of buffers that can be shared and reused, resulting in
// decreased memory allocation.
type BufferPool interface {
	// Get returns a buffer with specified length from the pool.
	Get(length int) *[]byte

	// Put returns a buffer to the pool.
	//
	// The provided pointer must hold a prefix of the buffer obtained via
	// BufferPool.Get to ensure the buffer's entire capacity can be re-used.
	Put(*[]byte)
}

const (
	// largeTiersPerDoubling is the number of evenly spaced tiers for every
	// doubling of the buffer size above the largest power-of-two tier in the
	// default buffer pool. A buffer from one of these tiers is at most
	// 1/largeTiersPerDoubling larger than requested.
	largeTiersPerDoubling = 4
	// maxGRPCMessageSize is an upper bound on the size of a gRPC message, whose
	// length prefix is 4 bytes long. It is the size of the largest tier in the
	// default buffer pool, since no message needs a larger buffer.
	maxGRPCMessageSize = 1 << 32
)

var (
	defaultBufferPoolSizeExponents = []uint8{
		8,
		12, // Go page size, 4KB
		14, // 16KB (max HTTP/2 frame size used by gRPC)
		15, // 32KB (default buffer size for io.Copy)
		20, // 1MB
	}
	defaultBufferPool BufferPool
)

func init() {
	// Buffers larger than the largest power-of-two tier are pooled in four
	// evenly spaced tiers for every doubling in size, up to 4GiB (or the
	// largest int on 32-bit platforms). Buffers larger than that are not
	// pooled.
	maxSize := min(uint64(maxGRPCMessageSize), uint64(math.MaxInt))
	var largeTierSizes []int
	for base := uint64(1 << slices.Max(defaultBufferPoolSizeExponents)); base < maxSize; base *= 2 {
		for i := uint64(1); i <= largeTiersPerDoubling; i++ {
			size := base + base*i/largeTiersPerDoubling
			if size <= maxSize {
				largeTierSizes = append(largeTierSizes, int(size))
			}
		}
	}
	fallbackPool := mem.NewTieredBufferPoolWithFallback(mem.NopBufferPool{}, largeTierSizes...)

	var err error
	defaultBufferPool, err = mem.NewBinaryTieredBufferPoolWithFallback(fallbackPool, defaultBufferPoolSizeExponents...)
	if err != nil {
		panic(fmt.Sprintf("Failed to create default buffer pool: %v", err))
	}

	internal.SetDefaultBufferPool = func(pool BufferPool) {
		defaultBufferPool = pool
	}
}

// DefaultBufferPool returns the current default buffer pool. It is a BufferPool
// created with NewBufferPool that uses a set of default sizes optimized for
// expected workflows.
//
// Buffers larger than 1MiB are pooled in four evenly spaced tiers for every
// doubling in size, up to 4GiB. Buffers larger than that are not pooled.
func DefaultBufferPool() BufferPool {
	return defaultBufferPool
}

// NewTieredBufferPool returns a BufferPool implementation that uses multiple
// underlying pools of the given pool sizes.
func NewTieredBufferPool(poolSizes ...int) BufferPool {
	return mem.NewTieredBufferPool(poolSizes...)
}

// NewBinaryTieredBufferPool returns a BufferPool backed by multiple sub-pools.
// This structure enables O(1) lookup time for Get and Put operations.
//
// The arguments provided are the exponents for the buffer capacities (powers
// of 2), not the raw byte sizes. For example, to create a pool of 16KB buffers
// (2^14 bytes), pass 14 as the argument.
func NewBinaryTieredBufferPool(powerOfTwoExponents ...uint8) (BufferPool, error) {
	return mem.NewBinaryTieredBufferPool(powerOfTwoExponents...)
}

// NopBufferPool is a buffer pool that returns new buffers without pooling.
type NopBufferPool struct {
	mem.NopBufferPool
}

var _ BufferPool = NopBufferPool{}
