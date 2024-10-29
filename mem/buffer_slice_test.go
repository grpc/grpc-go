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
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"testing"

	"google.golang.org/grpc/mem"
)

const (
	minReadSize = 1
	// Should match the constant in buffer_slice.go (another package)
	readAllBufSize = 32 * 1024 // 32 KiB
)

func newBuffer(data []byte, pool mem.BufferPool) mem.Buffer {
	return mem.NewBuffer(&data, pool)
}

func (s) TestBufferSlice_Len(t *testing.T) {
	tests := []struct {
		name string
		in   mem.BufferSlice
		want int
	}{
		{
			name: "empty",
			in:   nil,
			want: 0,
		},
		{
			name: "single",
			in:   mem.BufferSlice{newBuffer([]byte("abcd"), nil)},
			want: 4,
		},
		{
			name: "multiple",
			in: mem.BufferSlice{
				newBuffer([]byte("abcd"), nil),
				newBuffer([]byte("abcd"), nil),
				newBuffer([]byte("abcd"), nil),
			},
			want: 12,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.Len(); got != tt.want {
				t.Errorf("BufferSlice.Len() = %v, want %v", got, tt.want)
			}
		})
	}
}

func (s) TestBufferSlice_Ref(t *testing.T) {
	// Create a new buffer slice and a reference to it.
	bs := mem.BufferSlice{
		newBuffer([]byte("abcd"), nil),
		newBuffer([]byte("abcd"), nil),
	}
	bs.Ref()

	// Free the original buffer slice and verify that the reference can still
	// read data from it.
	bs.Free()
	got := bs.Materialize()
	want := []byte("abcdabcd")
	if !bytes.Equal(got, want) {
		t.Errorf("BufferSlice.Materialize() = %s, want %s", string(got), string(want))
	}
}

func (s) TestBufferSlice_MaterializeToBuffer(t *testing.T) {
	tests := []struct {
		name     string
		in       mem.BufferSlice
		pool     mem.BufferPool
		wantData []byte
	}{
		{
			name:     "single",
			in:       mem.BufferSlice{newBuffer([]byte("abcd"), nil)},
			pool:     nil, // MaterializeToBuffer should not use the pool in this case.
			wantData: []byte("abcd"),
		},
		{
			name: "multiple",
			in: mem.BufferSlice{
				newBuffer([]byte("abcd"), nil),
				newBuffer([]byte("abcd"), nil),
				newBuffer([]byte("abcd"), nil),
			},
			pool:     mem.DefaultBufferPool(),
			wantData: []byte("abcdabcdabcd"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer tt.in.Free()
			got := tt.in.MaterializeToBuffer(tt.pool)
			defer got.Free()
			if !bytes.Equal(got.ReadOnlyData(), tt.wantData) {
				t.Errorf("BufferSlice.MaterializeToBuffer() = %s, want %s", string(got.ReadOnlyData()), string(tt.wantData))
			}
		})
	}
}

func (s) TestBufferSlice_Reader(t *testing.T) {
	bs := mem.BufferSlice{
		newBuffer([]byte("abcd"), nil),
		newBuffer([]byte("abcd"), nil),
		newBuffer([]byte("abcd"), nil),
	}
	wantData := []byte("abcdabcdabcd")

	reader := bs.Reader()
	var gotData []byte
	// Read into a buffer of size 1 until EOF, and verify that the data matches.
	for {
		buf := make([]byte, 1)
		n, err := reader.Read(buf)
		if n > 0 {
			gotData = append(gotData, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("BufferSlice.Reader() failed unexpectedly: %v", err)
		}
	}
	if !bytes.Equal(gotData, wantData) {
		t.Errorf("BufferSlice.Reader() returned data %v, want %v", string(gotData), string(wantData))
	}

	// Reader should have released its references to the underlying buffers, but
	// bs still holds its reference and it should be able to read data from it.
	gotData = bs.Materialize()
	if !bytes.Equal(gotData, wantData) {
		t.Errorf("BufferSlice.Materialize() = %s, want %s", string(gotData), string(wantData))
	}
}

// TestBufferSlice_ReadAll_Reads exercises ReadAll by allowing it to read
// various combinations of data, empty data, EOF.
func (s) TestBufferSlice_ReadAll_Reads(t *testing.T) {
	testcases := []struct {
		name     string
		reads    []readStep
		wantErr  string
		wantBufs int
	}{
		{
			name: "EOF",
			reads: []readStep{
				{
					err: io.EOF,
				},
			},
		},
		{
			name: "data,EOF",
			reads: []readStep{
				{
					n: minReadSize,
				},
				{
					err: io.EOF,
				},
			},
			wantBufs: 1,
		},
		{
			name: "data+EOF",
			reads: []readStep{
				{
					n:   minReadSize,
					err: io.EOF,
				},
			},
			wantBufs: 1,
		},
		{
			name: "0,data+EOF",
			reads: []readStep{
				{},
				{
					n:   minReadSize,
					err: io.EOF,
				},
			},
			wantBufs: 1,
		},
		{
			name: "0,data,EOF",
			reads: []readStep{
				{},
				{
					n: minReadSize,
				},
				{
					err: io.EOF,
				},
			},
			wantBufs: 1,
		},
		{
			name: "data,data+EOF",
			reads: []readStep{
				{
					n: minReadSize,
				},
				{
					n:   minReadSize,
					err: io.EOF,
				},
			},
			wantBufs: 1,
		},
		{
			name: "error",
			reads: []readStep{
				{
					err: errors.New("boom"),
				},
			},
			wantErr: "boom",
		},
		{
			name: "data+error",
			reads: []readStep{
				{
					n:   minReadSize,
					err: errors.New("boom"),
				},
			},
			wantErr:  "boom",
			wantBufs: 1,
		},
		{
			name: "data,data+error",
			reads: []readStep{
				{
					n: minReadSize,
				},
				{
					n:   minReadSize,
					err: errors.New("boom"),
				},
			},
			wantErr:  "boom",
			wantBufs: 1,
		},
		{
			name: "data,data+EOF - whole buf",
			reads: []readStep{
				{
					n: minReadSize,
				},
				{
					n:   readAllBufSize - minReadSize,
					err: io.EOF,
				},
			},
			wantBufs: 1,
		},
		{
			name: "data,data,EOF - whole buf",
			reads: []readStep{
				{
					n: minReadSize,
				},
				{
					n: readAllBufSize - minReadSize,
				},
				{
					err: io.EOF,
				},
			},
			wantBufs: 1,
		},
		{
			name: "data,data,EOF - 2 bufs",
			reads: []readStep{
				{
					n: readAllBufSize,
				},
				{
					n: minReadSize,
				},
				{
					n: readAllBufSize - minReadSize,
				},
				{
					n: minReadSize,
				},
				{
					err: io.EOF,
				},
			},
			wantBufs: 3,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			pool := &testPool{
				allocated: make(map[*[]byte]struct{}),
			}
			r := &stepReader{
				reads: tc.reads,
			}
			data, err := mem.ReadAll(r, pool)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("ReadAll() returned err %v, wanted %q", err, tc.wantErr)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
			}
			gotData := data.Materialize()
			if !bytes.Equal(r.read, gotData) {
				t.Fatalf("ReadAll() returned data %q, wanted %q", gotData, r.read)
			}
			if len(data) != tc.wantBufs {
				t.Fatalf("ReadAll() returned %d bufs, wanted %d bufs", len(data), tc.wantBufs)
			}
			// all but last should be full buffers
			for i := 0; i < len(data)-1; i++ {
				if data[i].Len() != readAllBufSize {
					t.Fatalf("ReadAll() returned data length %d, wanted %d", data[i].Len(), readAllBufSize)
				}
			}
			data.Free()
			if len(pool.allocated) > 0 {
				t.Fatalf("got %d allocated buffers, wanted none", len(pool.allocated))
			}
		})
	}
}

func (s) TestBufferSlice_ReadAll_WriteTo(t *testing.T) {
	testcases := []struct {
		name string
		size int
	}{
		{
			name: "small",
			size: minReadSize,
		},
		{
			name: "exact size",
			size: readAllBufSize,
		},
		{
			name: "big",
			size: readAllBufSize * 3,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			pool := &testPool{
				allocated: make(map[*[]byte]struct{}),
			}
			buf := make([]byte, tc.size)
			_, err := rand.Read(buf)
			if err != nil {
				t.Fatal(err)
			}
			r := bytes.NewBuffer(buf)
			data, err := mem.ReadAll(r, pool)
			if err != nil {
				t.Fatal(err)
			}

			gotData := data.Materialize()
			if !bytes.Equal(buf, gotData) {
				t.Fatalf("ReadAll() = %q, wanted %q", gotData, buf)
			}
			data.Free()
			if len(pool.allocated) > 0 {
				t.Fatalf("wanted no allocated buffers, got %d", len(pool.allocated))
			}
		})
	}
}

func ExampleNewWriter() {
	var bs mem.BufferSlice
	pool := mem.DefaultBufferPool()
	writer := mem.NewWriter(&bs, pool)

	for _, data := range [][]byte{
		[]byte("abcd"),
		[]byte("abcd"),
		[]byte("abcd"),
	} {
		n, err := writer.Write(data)
		fmt.Printf("Wrote %d bytes, err: %v\n", n, err)
	}
	fmt.Println(string(bs.Materialize()))
	// Output:
	// Wrote 4 bytes, err: <nil>
	// Wrote 4 bytes, err: <nil>
	// Wrote 4 bytes, err: <nil>
	// abcdabcdabcd
}

var (
	_ io.Reader      = (*stepReader)(nil)
	_ mem.BufferPool = (*testPool)(nil)
)

// readStep describes what a single stepReader.Read should do - how much data
// to return and what error to return.
type readStep struct {
	n   int
	err error
}

// stepReader implements io.Reader that reads specified amount of data and/or
// returns the specified error in specified steps.
// The read data is accumulated in the read field.
type stepReader struct {
	reads []readStep
	read  []byte
}

func (s *stepReader) Read(buf []byte) (int, error) {
	if len(s.reads) == 0 {
		panic("unexpected Read() call")
	}
	read := s.reads[0]
	s.reads = s.reads[1:]
	_, err := rand.Read(buf[:read.n])
	if err != nil {
		panic(err)
	}
	s.read = append(s.read, buf[:read.n]...)
	return read.n, read.err
}

// testPool is an implementation of BufferPool that allows to ensure that:
// - there are matching Put calls for all Get calls.
// - there are no unexpected Put calls.
type testPool struct {
	allocated map[*[]byte]struct{}
}

func (t *testPool) Get(length int) *[]byte {
	buf := make([]byte, length)
	t.allocated[&buf] = struct{}{}
	return &buf
}

func (t *testPool) Put(buf *[]byte) {
	if _, ok := t.allocated[buf]; !ok {
		panic("unexpected put")
	}
	delete(t.allocated, buf)
}
