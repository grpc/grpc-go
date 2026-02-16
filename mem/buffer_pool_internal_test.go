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
			exponents: []uint8{31},
			wantErr:   false,
		},
		{
			name:      "32-bit_invalid_exponent",
			wordSize:  32,
			exponents: []uint8{32},
			wantErr:   true,
		},
		{
			name:      "64-bit_valid_exponent",
			wordSize:  64,
			exponents: []uint8{63},
			wantErr:   false,
		},
		{
			name:      "64-bit_invalid_exponent",
			wordSize:  64,
			exponents: []uint8{64},
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
			bp := pool.(*binaryTieredBufferPool)
			if len(bp.exponentToNextLargestPoolMap) != tt.wordSize {
				t.Errorf("exponentToNextLargestPoolMap length = %d, want %d", len(bp.exponentToNextLargestPoolMap), tt.wordSize)
			}
			if len(bp.exponentToPreviousLargestPoolMap) != tt.wordSize {
				t.Errorf("exponentToPreviousLargestPoolMap length = %d, want %d", len(bp.exponentToPreviousLargestPoolMap), tt.wordSize)
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
		p := NewTieredBufferPool(defaultBufferPoolSizes...).(*tieredBufferPool)
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
		p := pool.(*binaryTieredBufferPool)
		for b.Loop() {
			for size := range 1 << 19 {
				_ = p.poolForGet(size)
				_ = p.poolForPut(size)
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
	binaryPool := pool.(*binaryTieredBufferPool)
	if len(binaryPool.sizedPools) != 6 {
		t.Errorf("sized buffer pool count = %d, want %d", len(binaryPool.sizedPools), 6)
	}
}
