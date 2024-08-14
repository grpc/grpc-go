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
	"fmt"
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

// Tests that a buffer created with NewBuffer, which when later freed, invokes
// the free function with the correct data.
func (s) TestBuffer_NewBufferAndFree(t *testing.T) {
	data := "abcd"
	errCh := make(chan error, 1)
	freeF := func(got []byte) {
		if !bytes.Equal(got, []byte(data)) {
			errCh <- fmt.Errorf("Free function called with bytes %s, want %s", string(got), string(data))
			return
		}
		errCh <- nil
	}

	buf := mem.NewBuffer([]byte(data), freeF)
	if got := buf.ReadOnlyData(); !bytes.Equal(got, []byte(data)) {
		t.Fatalf("Buffer contains data %s, want %s", string(got), string(data))
	}

	// Verify that the free function is invoked when all references are freed.
	buf.Free()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timeout waiting for Buffer to be freed")
	}
}

// Tests that a buffer created with NewBuffer, on which an additional reference
// is acquired, which when later freed, invokes the free function with the
// correct data, but only after all references are released.
func (s) TestBuffer_NewBufferRefAndFree(t *testing.T) {
	data := "abcd"
	errCh := make(chan error, 1)
	freeF := func(got []byte) {
		if !bytes.Equal(got, []byte(data)) {
			errCh <- fmt.Errorf("Free function called with bytes %s, want %s", string(got), string(data))
			return
		}
		errCh <- nil
	}

	buf := mem.NewBuffer([]byte(data), freeF)
	if got := buf.ReadOnlyData(); !bytes.Equal(got, []byte(data)) {
		t.Fatalf("Buffer contains data %s, want %s", string(got), string(data))
	}

	bufRef := buf.Ref()
	if got := bufRef.ReadOnlyData(); !bytes.Equal(got, []byte(data)) {
		t.Fatalf("New reference to the Buffer contains data %s, want %s", string(got), string(data))
	}

	// Verify that the free function is not invoked when all references are yet
	// to be freed.
	buf.Free()
	select {
	case <-errCh:
		t.Fatalf("Free function called before all references freed")
	case <-time.After(defaultTestShortTimeout):
	}

	// Verify that the free function is invoked when all references are freed.
	bufRef.Free()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timeout waiting for Buffer to be freed")
	}
}

// testBufferPool is a buffer pool that makes new buffer without pooling, and
// notifies on a channel that a buffer was returned to the pool.
type testBufferPool struct {
	putCh chan []byte
}

func (t *testBufferPool) Get(length int) []byte {
	return make([]byte, length)
}

func (t *testBufferPool) Put(data []byte) {
	t.putCh <- data
}

func newTestBufferPool() *testBufferPool {
	return &testBufferPool{putCh: make(chan []byte, 1)}
}

// Tests that a buffer created with Copy, which when later freed, returns the underlying
// byte slice to the buffer pool.
func (s) TestBuffer_CopyAndFree(t *testing.T) {
	data := "abcd"
	testPool := newTestBufferPool()

	buf := mem.Copy([]byte(data), testPool)
	if got := buf.ReadOnlyData(); !bytes.Equal(got, []byte(data)) {
		t.Fatalf("Buffer contains data %s, want %s", string(got), string(data))
	}

	// Verify that the free function is invoked when all references are freed.
	buf.Free()
	select {
	case got := <-testPool.putCh:
		if !bytes.Equal(got, []byte(data)) {
			t.Fatalf("Free function called with bytes %s, want %s", string(got), string(data))
		}
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timeout waiting for Buffer to be freed")
	}
}

// Tests that a buffer created with Copy, on which an additional reference is
// acquired, which when later freed, returns the underlying byte slice to the
// buffer pool.
func (s) TestBuffer_CopyRefAndFree(t *testing.T) {
	data := "abcd"
	testPool := newTestBufferPool()

	buf := mem.Copy([]byte(data), testPool)
	if got := buf.ReadOnlyData(); !bytes.Equal(got, []byte(data)) {
		t.Fatalf("Buffer contains data %s, want %s", string(got), string(data))
	}

	bufRef := buf.Ref()
	if got := bufRef.ReadOnlyData(); !bytes.Equal(got, []byte(data)) {
		t.Fatalf("New reference to the Buffer contains data %s, want %s", string(got), string(data))
	}

	// Verify that the free function is not invoked when all references are yet
	// to be freed.
	buf.Free()
	select {
	case <-testPool.putCh:
		t.Fatalf("Free function called before all references freed")
	case <-time.After(defaultTestShortTimeout):
	}

	// Verify that the free function is invoked when all references are freed.
	bufRef.Free()
	select {
	case got := <-testPool.putCh:
		if !bytes.Equal(got, []byte(data)) {
			t.Fatalf("Free function called with bytes %s, want %s", string(got), string(data))
		}
	case <-time.After(defaultTestTimeout):
		t.Fatalf("Timeout waiting for Buffer to be freed")
	}
}

func (s) TestBuffer_Split(t *testing.T) {
	ready := false
	freed := false
	data := []byte{1, 2, 3, 4}
	buf := mem.NewBuffer(data, func(bytes []byte) {
		if !ready {
			t.Fatalf("Freed too early")
		}
		freed = true
	})
	checkBufData := func(b *mem.Buffer, expected []byte) {
		if !bytes.Equal(b.ReadOnlyData(), expected) {
			t.Fatalf("Buffer did not contain expected data %v, got %v", expected, b.ReadOnlyData())
		}
	}

	// Take a ref of the original buffer
	ref1 := buf.Ref()

	split1 := buf.Split(2)
	checkBufData(buf, data[:2])
	checkBufData(split1, data[2:])
	// Check that even though buf was split, the reference wasn't modified
	checkBufData(ref1, data)
	ref1.Free()

	// Check that splitting the buffer more than once works as intended.
	split2 := split1.Split(1)
	checkBufData(split1, data[2:3])
	checkBufData(split2, data[3:])

	// If any of the following frees actually free the buffer, the test will fail.
	buf.Free()
	split2.Free()

	ready = true
	split1.Free()

	if !freed {
		t.Fatalf("Buffer never freed")
	}
}

func checkForPanic(t *testing.T, wantErr string) {
	t.Helper()
	r := recover()
	if r == nil {
		t.Fatalf("Use after free dit not panic")
	}
	if r.(string) != wantErr {
		t.Fatalf("panic called with %v, want %s", r, wantErr)
	}
}

func (s) TestBuffer_ReadOnlyDataAfterFree(t *testing.T) {
	// Verify that reading before freeing does not panic.
	buf := mem.NewBuffer([]byte("abcd"), nil)
	buf.ReadOnlyData()

	buf.Free()
	defer checkForPanic(t, "Cannot read freed buffer")
	buf.ReadOnlyData()
}

func (s) TestBuffer_RefAfterFree(t *testing.T) {
	// Verify that acquiring a ref before freeing does not panic.
	buf := mem.NewBuffer([]byte("abcd"), nil)
	bufRef := buf.Ref()
	defer bufRef.Free()

	buf.Free()
	defer checkForPanic(t, "Cannot ref freed buffer")
	buf.Ref()
}

func (s) TestBuffer_SplitAfterFree(t *testing.T) {
	// Verify that splitting before freeing does not panic.
	buf := mem.NewBuffer([]byte("abcd"), nil)
	bufSplit := buf.Split(2)
	defer bufSplit.Free()

	buf.Free()
	defer checkForPanic(t, "Cannot split freed buffer")
	buf.Split(1)
}

func (s) TestBuffer_FreeAfterFree(t *testing.T) {
	buf := mem.NewBuffer([]byte("abcd"), nil)
	if buf.Len() != 4 {
		t.Fatalf("Buffer length is %d, want 4", buf.Len())
	}

	// Ensure that a double free does not panic.
	buf.Free()
	buf.Free()
}
