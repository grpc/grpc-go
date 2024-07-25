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
	"io"
	"testing"

	"google.golang.org/grpc/mem"
)

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
			in:   mem.BufferSlice{mem.NewBuffer([]byte("abcd"), nil)},
			want: 4,
		},
		{
			name: "multiple",
			in: mem.BufferSlice{
				mem.NewBuffer([]byte("abcd"), nil),
				mem.NewBuffer([]byte("abcd"), nil),
				mem.NewBuffer([]byte("abcd"), nil),
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
		mem.NewBuffer([]byte("abcd"), nil),
		mem.NewBuffer([]byte("abcd"), nil),
	}
	bsRef := bs.Ref()

	// Free the original buffer slice and verify that the reference can still
	// read data from it.
	bs.Free()
	got := bsRef.Materialize()
	want := []byte("abcdabcd")
	if !bytes.Equal(got, want) {
		t.Errorf("BufferSlice.Materialize() = %s, want %s", string(got), string(want))
	}
}

func (s) TestBufferSlice_LazyMaterialize(t *testing.T) {
	tests := []struct {
		name     string
		in       mem.BufferSlice
		pool     mem.BufferPool
		wantData []byte
	}{
		{
			name:     "single",
			in:       mem.BufferSlice{mem.NewBuffer([]byte("abcd"), nil)},
			pool:     nil, // LazyMaterialize should not use the pool in this case.
			wantData: []byte("abcd"),
		},
		{
			name: "multiple",
			in: mem.BufferSlice{
				mem.NewBuffer([]byte("abcd"), nil),
				mem.NewBuffer([]byte("abcd"), nil),
				mem.NewBuffer([]byte("abcd"), nil),
			},
			pool:     mem.DefaultBufferPool(),
			wantData: []byte("abcdabcdabcd"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.LazyMaterialize(tt.pool)
			if !bytes.Equal(got.ReadOnlyData(), tt.wantData) {
				t.Errorf("BufferSlice.LazyMaterialize() = %s, want %s", string(got.ReadOnlyData()), string(tt.wantData))
			}
		})
	}
}

func (s) TestBufferSlice_Reader(t *testing.T) {
	bs := mem.BufferSlice{
		mem.NewBuffer([]byte("abcd"), nil),
		mem.NewBuffer([]byte("abcd"), nil),
		mem.NewBuffer([]byte("abcd"), nil),
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

func (s) TestBufferSlice_Writer(t *testing.T) {
	var bs mem.BufferSlice
	pool := mem.DefaultBufferPool()
	writer := mem.NewWriter(&bs, pool)

	for _, data := range [][]byte{
		[]byte("abcd"),
		[]byte("abcd"),
		[]byte("abcd"),
	} {
		n, err := writer.Write(data)
		if n != len(data) {
			t.Fatalf("BufferSlice.Writer() wrote %d bytes, want %d", n, len(data))
		}
		if err != nil {
			t.Fatalf("BufferSlice.Writer() failed unexpectedly: %v", err)
		}
	}
	gotData := bs.Materialize()
	wantData := []byte("abcdabcdabcd")
	if !bytes.Equal(gotData, wantData) {
		t.Errorf("BufferSlice.Materialize() = %s, want %s", string(gotData), string(wantData))
	}
}
