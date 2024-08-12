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

package mem_test

import (
	"bytes"
	"testing"
	"time"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/mem"
)

const (
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 100 * time.Millisecond
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// testBufferPool is a buffer pool that makes new buffer without pooling, and
// notifies on a channel that a buffer was returned to the pool.
type testBufferPool struct {
	putCh chan []byte
}

func (t *testBufferPool) Get(length int) *[]byte {
	data := make([]byte, length)
	return &data
}

func (t *testBufferPool) Put(data *[]byte) {
	t.putCh <- *data
}

func newTestBufferPool() *testBufferPool {
	return &testBufferPool{putCh: make(chan []byte, 1)}
}

func (s) TestBuffer_Split(t *testing.T) {
	ready := false
	freed := false
	data := []byte{1, 2, 3, 4}
	buf := mem.NewBuffer(&data, func(bytes *[]byte) {
		if !ready {
			t.Fatalf("Freed too early")
		}
		freed = true
	})
	checkBufData := func(b mem.Buffer, expected []byte) {
		t.Helper()
		if !bytes.Equal(b.ReadOnlyData(), expected) {
			t.Fatalf("Buffer did not contain expected data %v, got %v", expected, b.ReadOnlyData())
		}
	}

	// Take a ref of the original buffer
	ref1 := buf.Ref()

	buf, split1 := mem.SplitUnsafe(buf, 2)
	checkBufData(buf, data[:2])
	checkBufData(split1, data[2:])
	// Check that even though buf was split, the reference wasn't modified
	checkBufData(ref1, data)
	ref1.Free()

	// Check that splitting the buffer more than once works as intended.
	split1, split2 := mem.SplitUnsafe(split1, 1)
	checkBufData(split1, data[2:3])
	checkBufData(split2, data[3:])

	// The part of the test that checks whether buffers are freed is not relevant if
	// pooling is disabled.
	if mem.PoolingEnabled {
		// If any of the following frees actually free the buffer, the test will fail.
		buf.Free()
		split2.Free()

		ready = true
		split1.Free()

		if !freed {
			t.Fatalf("Buffer never freed")
		}
	}
}

func checkForPanic(t *testing.T, wantErr string) {
	t.Helper()
	r := recover()
	if r == nil {
		t.Fatalf("Use after free did not panic")
	}
	if r.(string) != wantErr {
		t.Fatalf("panic called with %v, want %s", r, wantErr)
	}
}
