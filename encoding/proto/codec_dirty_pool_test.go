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

package proto

import (
	"bytes"
	"testing"
	"unsafe"

	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/mem"
)

// TestCodecDirtyPoolInitialized verifies that init() creates the codec dirty
// pool.
func (s) TestCodecDirtyPoolInitialized(t *testing.T) {
	if codecDirtyPool == nil {
		t.Fatal("codecDirtyPool is nil; init() did not create the dirty buffer pool")
	}
}

// TestCodecBufferPoolSwitching verifies that codecBufferPool returns the dirty
// pool when enabled, and the default pool otherwise.
func (s) TestCodecBufferPoolSwitching(t *testing.T) {
	old := envconfig.EnableCodecDirtyBufferPooling
	defer func() { envconfig.EnableCodecDirtyBufferPooling = old }()

	envconfig.EnableCodecDirtyBufferPooling = true
	if got := codecBufferPool(); got != codecDirtyPool {
		t.Errorf("codecBufferPool() with dirty pooling enabled = %v; want codecDirtyPool %v", got, codecDirtyPool)
	}

	envconfig.EnableCodecDirtyBufferPooling = false
	if got := codecBufferPool(); got != mem.DefaultBufferPool() {
		t.Errorf("codecBufferPool() with dirty pooling disabled = %v; want DefaultBufferPool %v", got, mem.DefaultBufferPool())
	}
}

// TestCodecDirtyPoolDoesNotClear verifies that the codec dirty pool does not
// clear buffers on reuse.
func (s) TestCodecDirtyPoolDoesNotClear(t *testing.T) {
	for {
		buf1 := codecDirtyPool.Get(1024)
		// Mark the buffer with data.
		for i := range *buf1 {
			(*buf1)[i] = 0xAA
		}
		codecDirtyPool.Put(buf1)

		buf2 := codecDirtyPool.Get(1024)
		// Check if we got the same underlying array.
		if unsafe.SliceData(*buf1) != unsafe.SliceData(*buf2) {
			codecDirtyPool.Put(buf2)
			continue
		}

		// Check that the reused buffer was not cleared.
		for _, b := range *buf2 {
			if b == 0 {
				t.Fatalf("dirty pool returned a cleared buffer; want non-zeroed on reuse")
			}
		}

		codecDirtyPool.Put(buf2)
		break
	}
}

// TestCodecMarshalUnmarshalWithDirtyPool verifies that Marshal and Unmarshal
// round-trip correctly with dirty pooling enabled.
func (s) TestCodecMarshalUnmarshalWithDirtyPool(t *testing.T) {
	old := envconfig.EnableCodecDirtyBufferPooling
	defer func() { envconfig.EnableCodecDirtyBufferPooling = old }()

	envconfig.EnableCodecDirtyBufferPooling = true

	// Large enough to exceed the buffer pooling threshold, so the codec path
	// goes through the dirty pool.
	expectedBody := bytes.Repeat([]byte{0xAB}, 65536)
	marshalAndUnmarshal(t, &codecV2{}, expectedBody)
}

// TestCodecMarshalUnmarshalDefaultPool verifies that Marshal and Unmarshal
// round-trip correctly with dirty pooling disabled.
func (s) TestCodecMarshalUnmarshalDefaultPool(t *testing.T) {
	old := envconfig.EnableCodecDirtyBufferPooling
	defer func() { envconfig.EnableCodecDirtyBufferPooling = old }()

	envconfig.EnableCodecDirtyBufferPooling = false

	expectedBody := bytes.Repeat([]byte{0xCD}, 65536)
	marshalAndUnmarshal(t, &codecV2{}, expectedBody)
}
