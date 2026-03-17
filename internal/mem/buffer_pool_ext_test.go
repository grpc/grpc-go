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

package mem_test

import (
	"testing"
	"unsafe"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/mem"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestBufferPool_Clears(t *testing.T) {
	poolConfigs := []struct {
		name        string
		factory     func() (*mem.BinaryTieredBufferPool, error)
		wantCleared bool
		bufferSize  int
	}{
		{
			name: "regular_sized",
			factory: func() (*mem.BinaryTieredBufferPool, error) {
				return mem.NewBinaryTieredBufferPool(3) // 8 bytes
			},
			bufferSize:  8,
			wantCleared: true,
		},
		{
			name: "regular_fallback",
			factory: func() (*mem.BinaryTieredBufferPool, error) {
				return mem.NewBinaryTieredBufferPool(3)
			},
			bufferSize:  10,
			wantCleared: true,
		},
		{
			name: "dirty_sized",
			factory: func() (*mem.BinaryTieredBufferPool, error) {
				return mem.NewDirtyBinaryTieredBufferPool(3)
			},
			bufferSize:  8,
			wantCleared: false,
		},
		{
			name: "dirty_fallback",
			factory: func() (*mem.BinaryTieredBufferPool, error) {
				return mem.NewDirtyBinaryTieredBufferPool(3)
			},
			bufferSize:  10,
			wantCleared: false,
		},
	}

	for _, tc := range poolConfigs {
		t.Run(tc.name, func(t *testing.T) {
			pool, err := tc.factory()
			if err != nil {
				t.Fatalf("Failed to create pool: %v", err)
			}

			for {
				buf1 := pool.Get(tc.bufferSize)
				// Mark the buffer with data.
				for i := range *buf1 {
					(*buf1)[i] = 0xAA
				}
				pool.Put(buf1)

				buf2 := pool.Get(tc.bufferSize)
				// Check if we got the same underlying array.
				if unsafe.SliceData(*buf1) != unsafe.SliceData(*buf2) {
					pool.Put(buf2)
					continue
				}

				// We have a reused buffer. Check if it's cleared.
				gotCleared := true
				for _, b := range *buf2 {
					if b != 0 {
						gotCleared = false
						break
					}
				}

				if tc.wantCleared != gotCleared {
					t.Fatalf("buffer cleared state mismatch: want %t, got %v", tc.wantCleared, gotCleared)
				}

				pool.Put(buf2)
				break
			}
		})
	}
}
