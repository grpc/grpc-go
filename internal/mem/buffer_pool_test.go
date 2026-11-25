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

type spyBufferPool struct {
	getCalls []int
	putCaps  []int
}

func (s *spyBufferPool) Get(length int) *[]byte {
	s.getCalls = append(s.getCalls, length)
	b := make([]byte, length)
	return &b
}

func (s *spyBufferPool) Put(buf *[]byte) {
	s.putCaps = append(s.putCaps, cap(*buf))
}

func TestMaxSizeBufferPool(t *testing.T) {
	const maxSize = 1024
	spy := &spyBufferPool{}
	pool := NewMaxSizeBufferPool(spy, maxSize)

	getTests := []struct {
		size       int
		wantPooled bool
	}{
		{size: 0, wantPooled: true},
		{size: maxSize - 1, wantPooled: true},
		{size: maxSize, wantPooled: true},
		{size: maxSize + 1, wantPooled: false},
		{size: maxSize * 2, wantPooled: false},
	}

	for _, tt := range getTests {
		prevCalls := len(spy.getCalls)
		buf := pool.Get(tt.size)
		if len(*buf) != tt.size {
			t.Errorf("Get(%d) returned buffer with length %d, want %d", tt.size, len(*buf), tt.size)
		}
		if cap(*buf) < tt.size {
			t.Errorf("Get(%d) returned buffer with capacity %d, want >= %d", tt.size, cap(*buf), tt.size)
		}
		gotPooled := len(spy.getCalls) == prevCalls+1
		if gotPooled != tt.wantPooled {
			t.Errorf("Get(%d) delegated to wrapped pool = %t, want %t", tt.size, gotPooled, tt.wantPooled)
		}
		if !tt.wantPooled && cap(*buf) != tt.size {
			t.Errorf("Get(%d) unpooled buffer capacity = %d, want %d", tt.size, cap(*buf), tt.size)
		}
	}

	putTests := []struct {
		length     int
		capacity   int
		wantPooled bool
	}{
		{length: 0, capacity: 0, wantPooled: true},
		{length: maxSize, capacity: maxSize, wantPooled: true},
		{length: 1, capacity: maxSize, wantPooled: true},
		{length: maxSize + 1, capacity: maxSize + 1, wantPooled: false},
		// Even when len <= maxSize, Put must check cap.
		{length: 1, capacity: maxSize + 1, wantPooled: false},
	}

	for _, tt := range putTests {
		prevCalls := len(spy.putCaps)
		buf := make([]byte, tt.length, tt.capacity)
		pool.Put(&buf)
		gotPooled := len(spy.putCaps) == prevCalls+1
		if gotPooled != tt.wantPooled {
			t.Errorf("Put(len=%d, cap=%d) delegated to wrapped pool = %t, want %t", tt.length, tt.capacity, gotPooled, tt.wantPooled)
		}
	}
}
