/*
 *
 * Copyright 2026 gRPC authors.
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
	"testing"
)

var defaultBufferPoolSizeExponents = []uint8{
	8,
	12,
	14, // 16KB (max HTTP/2 frame size used by gRPC)
	15, // 32KB (default buffer size for io.Copy)
	20, // 1MB
	21, // 2MB
	22, // 4MB (default max receive message size)
}

func TestNewBinaryTieredBufferPool_WordSize(t *testing.T) {
	origUintSize := uintSize
	defer func() { uintSize = origUintSize }()

	tests := []struct {
		name      string
		wordSize  int
		exponents []uint8
		wantErr   bool
	}{
		{
			name:      "32-bit_valid_exponent",
			wordSize:  32,
			exponents: []uint8{30},
			wantErr:   false,
		},
		{
			name:      "32-bit_invalid_exponent",
			wordSize:  32,
			exponents: []uint8{31},
			wantErr:   true,
		},
		{
			name:      "64-bit_valid_exponent",
			wordSize:  64,
			exponents: []uint8{62},
			wantErr:   false,
		},
		{
			name:      "64-bit_invalid_exponent",
			wordSize:  64,
			exponents: []uint8{63},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uintSize = tt.wordSize
			pool, err := NewBinaryTieredBufferPool(tt.exponents...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewBinaryTieredBufferPool() error = %t, wantErr %t", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if len(pool.exponentToNextLargestPoolMap) != tt.wordSize-1 {
				t.Errorf("exponentToNextLargestPoolMap length = %d, want %d", len(pool.exponentToNextLargestPoolMap), tt.wordSize)
			}
			if len(pool.exponentToPreviousLargestPoolMap) != tt.wordSize-1 {
				t.Errorf("exponentToPreviousLargestPoolMap length = %d, want %d", len(pool.exponentToPreviousLargestPoolMap), tt.wordSize)
			}
		})
	}
}

// BenchmarkTieredPool benchmarks the performance of the tiered buffer pool
// implementations, specifically focusing on the overhead of selecting the
// correct bucket for a given size.
func BenchmarkTieredPool(b *testing.B) {
	defaultBufferPoolSizes := make([]int, len(defaultBufferPoolSizeExponents))
	for i, exp := range defaultBufferPoolSizeExponents {
		defaultBufferPoolSizes[i] = 1 << exp
	}

	b.Run("pool=Tiered", func(b *testing.B) {
		p := NewTieredBufferPool(defaultBufferPoolSizes...)
		for b.Loop() {
			for size := range 1 << 19 {
				// One for get, one for put.
				_ = p.getPool(size)
				_ = p.getPool(size)
			}
		}
	})

	b.Run("pool=BinaryTiered", func(b *testing.B) {
		pool, err := NewBinaryTieredBufferPool(defaultBufferPoolSizeExponents...)
		if err != nil {
			b.Fatalf("Failed to create buffer pool: %v", err)
		}
		for b.Loop() {
			for size := range 1 << 19 {
				_ = pool.poolForGet(size)
				_ = pool.poolForPut(size)
			}
		}
	})
}

func TestNewBinaryTieredBufferPool_Duplicates(t *testing.T) {
	exponents := []uint8{1, 2, 3, 4, 5, 6, 6, 5, 4, 3, 2, 1}
	pool, err := NewBinaryTieredBufferPool(exponents...)
	if err != nil {
		t.Fatalf("NewBinaryTieredBufferPool() error = %v", err)
	}
	if len(pool.sizedPools) != 6 {
		t.Errorf("sized buffer pool count = %d, want %d", len(pool.sizedPools), 6)
	}
}

// TestBinaryTieredBufferPool_GetZero verifies that a zero-length request does
// not route to the fallback pool. The fallback clears the entire capacity of
// the buffer it holds, so after a large buffer was pooled there, Get(0) would
// pay to clear it.
func TestBinaryTieredBufferPool_GetZero(t *testing.T) {
	pool, err := NewBinaryTieredBufferPool(defaultBufferPoolSizeExponents...)
	if err != nil {
		t.Fatalf("NewBinaryTieredBufferPool() error = %v", err)
	}

	// Routing check: poolForGet(0) must be the no-op pool, matching
	// poolForPut, which already discards zero-capacity buffers via
	// NopBufferPool.
	if _, ok := pool.poolForGet(0).(NopBufferPool); !ok {
		t.Errorf("poolForGet(0) = %T, want NopBufferPool", pool.poolForGet(0))
	}
}

func TestBinaryTieredBufferPoolWithFallback_PoolSelection(t *testing.T) {
	pool, err := NewBinaryTieredBufferPoolWithFallback(NopBufferPool{}, 3, 4) // 8 and 16 bytes
	if err != nil {
		t.Fatalf("Failed to create buffer pool: %v", err)
	}

	tierSize := func(p bufferPool, size int) int {
		t.Helper()
		switch p := p.(type) {
		case *sizedBufferPool:
			return p.defaultSize
		case NopBufferPool:
			return 0
		}
		t.Fatalf("Unexpected pool of type %T for buffers of size %d", p, size)
		return 0
	}

	getTests := []struct {
		size     int
		wantTier int // 0 if the buffer must not be pooled.
	}{
		{size: 0, wantTier: 0},
		{size: 1, wantTier: 8},
		{size: 8, wantTier: 8},
		{size: 9, wantTier: 16},
		{size: 16, wantTier: 16},
		{size: 17, wantTier: 0},
	}

	for _, tt := range getTests {
		if got := tierSize(pool.poolForGet(tt.size), tt.size); got != tt.wantTier {
			t.Errorf("poolForGet(%d) returned pool with buffers of capacity %d, want %d", tt.size, got, tt.wantTier)
		}
	}

	putTests := []struct {
		bufCap   int
		wantTier int // 0 if the buffer must not be pooled.
	}{
		{bufCap: 0, wantTier: 0},
		{bufCap: 7, wantTier: 0},
		{bufCap: 8, wantTier: 8},
		{bufCap: 15, wantTier: 8},
		{bufCap: 16, wantTier: 16},
		{bufCap: 17, wantTier: 0},
	}

	for _, tt := range putTests {
		if got := tierSize(pool.poolForPut(tt.bufCap), tt.bufCap); got != tt.wantTier {
			t.Errorf("poolForPut(%d) returned pool with buffers of capacity %d, want %d", tt.bufCap, got, tt.wantTier)
		}
	}
}
