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

type poolFunc func(*[]byte)

func (p poolFunc) Get(length int) *[]byte {
	panic("Get should never be called")
}

func (p poolFunc) Put(i *[]byte) {
	p(i)
}

func (s) TestBuffer_Split(t *testing.T) {
	ready := false
	freed := false
	data := []byte{1, 2, 3, 4}
	buf := mem.NewBuffer(&data, poolFunc(func(bytes *[]byte) {
		if !ready {
			t.Fatalf("Freed too early")
		}
		freed = true
	}))
	checkBufData := func(b mem.Buffer, expected []byte) {
		t.Helper()
		if !bytes.Equal(b.ReadOnlyData(), expected) {
			t.Fatalf("Buffer did not contain expected data %v, got %v", expected, b.ReadOnlyData())
		}
	}

	buf, split1 := mem.SplitUnsafe(buf, 2)
	checkBufData(buf, data[:2])
	checkBufData(split1, data[2:])

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
	if msg, ok := r.(string); !ok || msg != wantErr {
		t.Fatalf("panic called with %v, want %s", r, wantErr)
	}
}
