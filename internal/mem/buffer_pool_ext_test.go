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

	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/mem"
	"google.golang.org/grpc/internal/testutils"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestBufferPool_Clears verifies that reused buffers are cleared when they are
// fetched using Get, and are not cleared when they are fetched using
// GetUninitialized.
func (s) TestBufferPool_Clears(t *testing.T) {
	const marker = 0xAA

	newBinaryTiered := func(t *testing.T) mem.UninitializedBufferPool {
		pool, err := mem.NewBinaryTieredBufferPool(3) // 8 bytes
		if err != nil {
			t.Fatalf("Failed to create pool: %v", err)
		}
		return pool
	}
	newTiered := func(*testing.T) mem.UninitializedBufferPool {
		return mem.NewTieredBufferPool(8)
	}
	poolConfigs := []struct {
		name       string
		factory    func(*testing.T) mem.UninitializedBufferPool
		bufferSize int
	}{
		{name: "binary_tiered_sized", factory: newBinaryTiered, bufferSize: 8},
		{name: "binary_tiered_fallback", factory: newBinaryTiered, bufferSize: 10},
		{name: "tiered_sized", factory: newTiered, bufferSize: 8},
		{name: "tiered_fallback", factory: newTiered, bufferSize: 10},
		{
			name:       "simple",
			factory:    func(*testing.T) mem.UninitializedBufferPool { return &mem.SimpleBufferPool{} },
			bufferSize: 8,
		},
	}
	getters := []struct {
		name        string
		get         func(pool mem.UninitializedBufferPool, size int) *[]byte
		wantCleared bool
	}{
		{
			name:        "Get",
			get:         func(pool mem.UninitializedBufferPool, size int) *[]byte { return pool.Get(size) },
			wantCleared: true,
		},
		{
			name:        "GetUninitialized",
			get:         func(pool mem.UninitializedBufferPool, size int) *[]byte { return pool.GetUninitialized(size) },
			wantCleared: false,
		},
	}

	for _, tc := range poolConfigs {
		for _, getter := range getters {
			t.Run(tc.name+"/"+getter.name, func(t *testing.T) {
				pool := tc.factory(t)

				for {
					buf1 := pool.Get(tc.bufferSize)
					// Mark the buffer with data.
					for i := range *buf1 {
						(*buf1)[i] = marker
					}
					pool.Put(buf1)

					buf2 := getter.get(pool, tc.bufferSize)
					if got, want := len(*buf2), tc.bufferSize; got != want {
						t.Fatalf("Buffer length = %d, want %d", got, want)
					}
					// Check if we got the same underlying array. This test is
					// only relevant if a buffer is reused, otherwise try
					// again. Buffers may not be reused if a GC cycle runs
					// between putting the buffer back in the pool and getting
					// a new one.
					if unsafe.SliceData(*buf1) != unsafe.SliceData(*buf2) {
						pool.Put(buf2)
						continue
					}

					wantByte := byte(marker)
					if getter.wantCleared {
						wantByte = 0
					}
					for i, b := range *buf2 {
						if b != wantByte {
							t.Fatalf("Reused buffer has byte %#x at index %d, want %#x (wantCleared = %t)", b, i, wantByte, getter.wantCleared)
						}
					}
					pool.Put(buf2)
					break
				}
			})
		}
	}
}

// getOnlyPool is a buffer pool that doesn't support uninitialized buffers. It
// records the number of calls to Get.
type getOnlyPool struct {
	gets int
}

func (p *getOnlyPool) Get(length int) *[]byte {
	p.gets++
	b := make([]byte, length)
	return &b
}

func (p *getOnlyPool) Put(*[]byte) {}

// uninitializedPool is a buffer pool that supports uninitialized buffers. It
// records the number of calls to Get and GetUninitialized.
type uninitializedPool struct {
	getOnlyPool
	uninitializedGets int
}

func (p *uninitializedPool) GetUninitialized(length int) *[]byte {
	p.uninitializedGets++
	b := make([]byte, length)
	return &b
}

// TestGetUninitialized verifies which method of the buffer pool is used by
// GetUninitialized and GetUninitializedUngated, based on whether the pool
// supports uninitialized buffers and whether they are enabled using the
// environment variable.
func (s) TestGetUninitialized(t *testing.T) {
	const bufferSize = 16
	tests := []struct {
		name              string
		envEnabled        bool
		get               func(pool *uninitializedPool) *[]byte
		wantUninitialized bool
	}{
		{
			name:              "gated_env_enabled",
			envEnabled:        true,
			get:               func(pool *uninitializedPool) *[]byte { return mem.GetUninitialized(pool, bufferSize) },
			wantUninitialized: true,
		},
		{
			name:              "gated_env_disabled",
			envEnabled:        false,
			get:               func(pool *uninitializedPool) *[]byte { return mem.GetUninitialized(pool, bufferSize) },
			wantUninitialized: false,
		},
		{
			name:              "ungated_env_enabled",
			envEnabled:        true,
			get:               func(pool *uninitializedPool) *[]byte { return mem.GetUninitializedUngated(pool, bufferSize) },
			wantUninitialized: true,
		},
		{
			name:              "ungated_env_disabled",
			envEnabled:        false,
			get:               func(pool *uninitializedPool) *[]byte { return mem.GetUninitializedUngated(pool, bufferSize) },
			wantUninitialized: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutils.SetEnvConfig(t, &envconfig.EnableUninitializedBuffers, tc.envEnabled)

			pool := &uninitializedPool{}
			buf := tc.get(pool)
			if got := len(*buf); got != bufferSize {
				t.Errorf("Buffer length = %d, want %d", got, bufferSize)
			}
			wantGets, wantUninitializedGets := 1, 0
			if tc.wantUninitialized {
				wantGets, wantUninitializedGets = 0, 1
			}
			if pool.gets != wantGets || pool.uninitializedGets != wantUninitializedGets {
				t.Errorf("Pool calls: Get = %d, GetUninitialized = %d; want Get = %d, GetUninitialized = %d", pool.gets, pool.uninitializedGets, wantGets, wantUninitializedGets)
			}
		})
	}
}

// TestGetUninitialized_UnsupportedPool verifies that GetUninitialized and
// GetUninitializedUngated fall back to Get when the pool doesn't support
// uninitialized buffers.
func (s) TestGetUninitialized_UnsupportedPool(t *testing.T) {
	const bufferSize = 16
	tests := []struct {
		name string
		get  func(pool *getOnlyPool) *[]byte
	}{
		{
			name: "gated",
			get:  func(pool *getOnlyPool) *[]byte { return mem.GetUninitialized(pool, bufferSize) },
		},
		{
			name: "ungated",
			get:  func(pool *getOnlyPool) *[]byte { return mem.GetUninitializedUngated(pool, bufferSize) },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutils.SetEnvConfig(t, &envconfig.EnableUninitializedBuffers, true)

			pool := &getOnlyPool{}
			buf := tc.get(pool)
			if got := len(*buf); got != bufferSize {
				t.Errorf("Buffer length = %d, want %d", got, bufferSize)
			}
			if pool.gets != 1 {
				t.Errorf("Pool calls: Get = %d, want 1", pool.gets)
			}
		})
	}
}
