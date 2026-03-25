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

	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/mem"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	internal.SetBufferPoolingThresholdForTesting.(func(int))(0)

	grpctest.RunSubTests(t, s{})
}

// Tests that a buffer created with NewBuffer, which when later freed, invokes
// the free function with the correct data.
func (s) TestBuffer_NewBufferAndFree(t *testing.T) {
	data := "abcd"
	freed := false
	freeF := poolFunc(func(got *[]byte) {
		if !bytes.Equal(*got, []byte(data)) {
			t.Fatalf("Free function called with bytes %s, want %s", string(*got), data)
		}
		freed = true
	})

	buf := newBuffer([]byte(data), freeF)
	if got := buf.ReadOnlyData(); !bytes.Equal(got, []byte(data)) {
		t.Fatalf("Buffer contains data %s, want %s", string(got), string(data))
	}

	// Verify that the free function is invoked when all references are freed.
	buf.Free()
	if !freed {
		t.Fatalf("Buffer not freed")
	}
}

// Tests that a buffer created with NewBuffer, on which an additional reference
// is acquired, which when later freed, invokes the free function with the
// correct data, but only after all references are released.
func (s) TestBuffer_NewBufferRefAndFree(t *testing.T) {
	data := "abcd"
	freed := false
	freeF := poolFunc(func(got *[]byte) {
		if !bytes.Equal(*got, []byte(data)) {
			t.Fatalf("Free function called with bytes %s, want %s", string(*got), string(data))
		}
		freed = true
	})

	buf := newBuffer([]byte(data), freeF)
	if got := buf.ReadOnlyData(); !bytes.Equal(got, []byte(data)) {
		t.Fatalf("Buffer contains data %s, want %s", string(got), string(data))
	}

	buf.Ref()
	if got := buf.ReadOnlyData(); !bytes.Equal(got, []byte(data)) {
		t.Fatalf("New reference to the Buffer contains data %s, want %s", string(got), string(data))
	}

	// Verify that the free function is not invoked when all references are yet
	// to be freed.
	buf.Free()
	if freed {
		t.Fatalf("Free function called before all references freed")
	}

	// Verify that the free function is invoked when all references are freed.
	buf.Free()
	if !freed {
		t.Fatalf("Buffer not freed")
	}
}

func (s) TestBuffer_NewBufferHandlesShortBuffers(t *testing.T) {
	const threshold = 100

	// Update the pooling threshold, since that's what's being tested.
	internal.SetBufferPoolingThresholdForTesting.(func(int))(threshold)
	t.Cleanup(func() {
		internal.SetBufferPoolingThresholdForTesting.(func(int))(0)
	})

	// Make a pool with a buffer whose capacity is larger than the pooling
	// threshold, but whose length is less than the threshold.
	b := make([]byte, threshold/2, threshold*2)
	pool := &singleBufferPool{
		t:    t,
		data: &b,
	}

	// Get a Buffer, then free it. If NewBuffer decided that the Buffer
	// shouldn't get pooled, Free will be a noop and singleBufferPool will not
	// have been updated.
	mem.NewBuffer(&b, pool).Free()

	if pool.data != nil {
		t.Fatalf("Buffer not returned to pool")
	}
}

func (s) TestBuffer_FreeAfterFree(t *testing.T) {
	buf := newBuffer([]byte("abcd"), mem.NopBufferPool{})
	if buf.Len() != 4 {
		t.Fatalf("Buffer length is %d, want 4", buf.Len())
	}

	// Ensure that a double free does panic.
	buf.Free()
	defer checkForPanic(t, "Cannot free freed buffer")
	buf.Free()
}

type singleBufferPool struct {
	t    *testing.T
	data *[]byte
}

func (s *singleBufferPool) Get(length int) *[]byte {
	if len(*s.data) != length {
		s.t.Fatalf("Invalid requested length, got %d want %d", length, len(*s.data))
	}
	return s.data
}

func (s *singleBufferPool) Put(b *[]byte) {
	if s.data != b {
		s.t.Fatalf("Wrong buffer returned to pool, got %p want %p", b, s.data)
	}
	s.data = nil
}

// Tests that a buffer created with Copy, which when later freed, returns the underlying
// byte slice to the buffer pool.
func (s) TestBuffer_CopyAndFree(t *testing.T) {
	data := []byte("abcd")
	testPool := &singleBufferPool{
		t:    t,
		data: &data,
	}

	buf := mem.Copy(data, testPool)
	if got := buf.ReadOnlyData(); !bytes.Equal(got, data) {
		t.Fatalf("Buffer contains data %s, want %s", string(got), string(data))
	}

	// Verify that the free function is invoked when all references are freed.
	buf.Free()
	if testPool.data != nil {
		t.Fatalf("Buffer not freed")
	}
}

// Tests that a buffer created with Copy, on which an additional reference is
// acquired, which when later freed, returns the underlying byte slice to the
// buffer pool.
func (s) TestBuffer_CopyRefAndFree(t *testing.T) {
	data := []byte("abcd")
	testPool := &singleBufferPool{
		t:    t,
		data: &data,
	}

	buf := mem.Copy(data, testPool)
	if got := buf.ReadOnlyData(); !bytes.Equal(got, data) {
		t.Fatalf("Buffer contains data %s, want %s", string(got), string(data))
	}

	buf.Ref()
	if got := buf.ReadOnlyData(); !bytes.Equal(got, []byte(data)) {
		t.Fatalf("New reference to the Buffer contains data %s, want %s", string(got), string(data))
	}

	// Verify that the free function is not invoked when all references are yet
	// to be freed.
	buf.Free()
	if testPool.data == nil {
		t.Fatalf("Free function called before all references freed")
	}

	// Verify that the free function is invoked when all references are freed.
	buf.Free()
	if testPool.data != nil {
		t.Fatalf("Free never called")
	}
}

func (s) TestBuffer_ReadOnlyDataAfterFree(t *testing.T) {
	// Verify that reading before freeing does not panic.
	buf := newBuffer([]byte("abcd"), mem.NopBufferPool{})
	buf.ReadOnlyData()

	buf.Free()
	defer checkForPanic(t, "Cannot read freed buffer")
	buf.ReadOnlyData()
}

func (s) TestBuffer_RefAfterFree(t *testing.T) {
	// Verify that acquiring a ref before freeing does not panic.
	buf := newBuffer([]byte("abcd"), mem.NopBufferPool{})
	buf.Ref()

	// This first call should not panic and bring the ref counter down to 1
	buf.Free()
	// This second call actually frees the buffer
	buf.Free()
	defer checkForPanic(t, "Cannot ref freed buffer")
	buf.Ref()
}

func (s) TestBuffer_SplitAfterFree(t *testing.T) {
	// Verify that splitting before freeing does not panic.
	buf := newBuffer([]byte("abcd"), mem.NopBufferPool{})
	buf, bufSplit := mem.SplitUnsafe(buf, 2)

	bufSplit.Free()
	buf.Free()
	defer checkForPanic(t, "Cannot split freed buffer")
	mem.SplitUnsafe(buf, 2)
}

type poolFunc func(*[]byte)

func (p poolFunc) Get(int) *[]byte {
	panic("Get should never be called")
}

func (p poolFunc) Put(i *[]byte) {
	p(i)
}

func (s) TestBuffer_Split(t *testing.T) {
	ready := false
	freed := false
	data := []byte{1, 2, 3, 4}
	buf := mem.NewBuffer(&data, poolFunc(func(*[]byte) {
		if !ready {
			t.Fatalf("Freed too early")
		}
		freed = true
	}))

	buf, split1 := mem.SplitUnsafe(buf, 2)
	if !bytes.Equal(buf.ReadOnlyData(), data[:2]) {
		t.Fatalf("Buffer did not contain expected data %v, got %v", data[:2], buf.ReadOnlyData())
	}
	if !bytes.Equal(split1.ReadOnlyData(), data[2:]) {
		t.Fatalf("Buffer did not contain expected data %v, got %v", data[2:], split1.ReadOnlyData())
	}

	// Check that splitting the buffer more than once works as intended.
	split1, split2 := mem.SplitUnsafe(split1, 1)
	if !bytes.Equal(split1.ReadOnlyData(), data[2:3]) {
		t.Fatalf("Buffer did not contain expected data %v, got %v", data[2:3], split1.ReadOnlyData())
	}
	if !bytes.Equal(split2.ReadOnlyData(), data[3:]) {
		t.Fatalf("Buffer did not contain expected data %v, got %v", data[3:], split2.ReadOnlyData())
	}

	// If any of the following frees actually free the buffer, the test will fail.
	buf.Free()
	split2.Free()

	ready = true
	split1.Free()

	if !freed {
		t.Fatalf("Buffer never freed")
	}
}

func (s) TestBuffer_Slice(t *testing.T) {
	ready := false
	freed := false
	data := []byte{1, 2, 3, 4}
	buf := mem.NewBuffer(&data, poolFunc(func(*[]byte) {
		if !ready {
			t.Fatalf("Freed too early")
		}
		freed = true
	}))

	// Slice the buffer and verify the data.
	slice1 := buf.Slice(1, 3)
	if !bytes.Equal(slice1.ReadOnlyData(), data[1:3]) {
		t.Fatalf("Buffer did not contain expected data %v, got %v", data[1:3], slice1.ReadOnlyData())
	}

	// Verify the original buffer is not modified.
	if !bytes.Equal(buf.ReadOnlyData(), data) {
		t.Fatalf("Buffer did not contain expected data %v, got %v", data, buf.ReadOnlyData())
	}

	// Slice the slice.
	slice2 := slice1.Slice(0, 1)
	if !bytes.Equal(slice2.ReadOnlyData(), data[1:2]) {
		t.Fatalf("Buffer did not contain expected data %v, got %v", data[1:2], slice2.ReadOnlyData())
	}

	// Free original and first slice — root should not be freed yet.
	buf.Free()
	slice1.Free()

	// The last slice keeps the root alive.
	if !bytes.Equal(slice2.ReadOnlyData(), data[1:2]) {
		t.Fatalf("Buffer did not contain expected data %v, got %v", data[1:2], slice2.ReadOnlyData())
	}

	ready = true
	slice2.Free()

	if !freed {
		t.Fatalf("Buffer never freed")
	}
}

func (s) TestBuffer_SliceAfterFree(t *testing.T) {
	buf := newBuffer([]byte("abcd"), mem.NopBufferPool{})
	buf.Free()
	defer checkForPanic(t, "Cannot slice freed buffer")
	buf.Slice(0, 2)
}

type namedCtor struct {
	name   string
	newBuf func() mem.Buffer
}

var ctors = []namedCtor{
	{name: "pooled", newBuf: newPooledBuffer},
	{name: "slice", newBuf: newSliceBuf},
}

func (s) TestBuffer_SliceBasic(t *testing.T) {
	type sliceCase struct {
		start, end int
		want       []byte
	}
	cases := []sliceCase{
		{1, 3, []byte{2, 3}},
		{0, 4, []byte{1, 2, 3, 4}},
		{0, 0, []byte{}},
		{4, 4, []byte{}},
	}
	for _, c := range ctors {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s[%d:%d]", c.name, tc.start, tc.end), func(t *testing.T) {
				buf := c.newBuf()
				got := buf.Slice(tc.start, tc.end)
				if !bytes.Equal(got.ReadOnlyData(), tc.want) {
					t.Fatalf("Buffer did not contain expected data %v, got %v", tc.want, got.ReadOnlyData())
				}
			})
		}
	}
}

func (s) TestBuffer_SliceSubslice(t *testing.T) {
	for _, c := range ctors {
		t.Run(c.name+" subslice", func(t *testing.T) {
			buf := c.newBuf()
			slice := buf.Slice(1, 3)
			slice2 := slice.Slice(0, 1)
			if !bytes.Equal(slice2.ReadOnlyData(), []byte{2}) {
				t.Fatalf("Buffer did not contain expected data %v, got %v", []byte{2}, buf.ReadOnlyData())
			}
		})
	}
}

func (s) TestBuffer_SliceEmpty(t *testing.T) {
	buf := newEmptyBuf()
	got := buf.Slice(0, 0)
	if !bytes.Equal(got.ReadOnlyData(), nil) {
		t.Fatalf("Buffer did not contain expected data %v, got %v", nil, got.ReadOnlyData())
	}
}

func (s) TestBuffer_SliceBoundsCheck(t *testing.T) {
	type panicCase struct {
		name       string
		start, end int
	}
	nonEmptyCases := []panicCase{
		{"end_out_of_bounds", 0, 5},
		{"start_negative", -1, 3},
		{"start_greater_than_end", 3, 0},
	}
	tests := []struct {
		name       string
		buf        func() mem.Buffer
		panicCases []panicCase
	}{
		{
			name:       "buffer",
			buf:        newPooledBuffer,
			panicCases: nonEmptyCases,
		},
		{
			name:       "SliceBuffer",
			buf:        newSliceBuf,
			panicCases: nonEmptyCases,
		},
		{
			name: "emptyBuffer",
			buf:  newEmptyBuf,
			panicCases: []panicCase{
				{"end_out_of_bounds", 0, 1},
				{"start_negative", -1, 0},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, pc := range tt.panicCases {
				t.Run(pc.name, func(t *testing.T) {
					buf := tt.buf()
					defer func() {
						if recover() == nil {
							t.Fatalf("Slice(%d, %d) did not panic", pc.start, pc.end)
						}
					}()
					buf.Slice(pc.start, pc.end)
				})
			}
		})
	}
}

func newPooledBuffer() mem.Buffer {
	return newBuffer([]byte{1, 2, 3, 4}, mem.NopBufferPool{})
}

func newSliceBuf() mem.Buffer {
	return mem.SliceBuffer{1, 2, 3, 4}
}

func newEmptyBuf() mem.Buffer {
	var bs mem.BufferSlice
	return bs.MaterializeToBuffer(mem.NopBufferPool{})
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
