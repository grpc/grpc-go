/*
 *
 * Copyright 2014 gRPC authors.
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

package grpc

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"math"
	"reflect"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	protoenc "google.golang.org/grpc/encoding/proto"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/transport"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/status"
	perfpb "google.golang.org/grpc/test/codec_perf"
	"google.golang.org/protobuf/proto"
)

type fullReader struct {
	data []byte
}

func (f *fullReader) ReadHeader(header []byte) error {
	buf, err := f.Read(len(header))
	defer buf.Free()
	if err != nil {
		return err
	}

	buf.CopyTo(header)
	return nil
}

func (f *fullReader) Read(n int) (mem.BufferSlice, error) {
	if len(f.data) == 0 {
		return nil, io.EOF
	}

	if len(f.data) < n {
		data := f.data
		f.data = nil
		return mem.BufferSlice{mem.SliceBuffer(data)}, io.ErrUnexpectedEOF
	}

	buf := f.data[:n]
	f.data = f.data[n:]

	return mem.BufferSlice{mem.SliceBuffer(buf)}, nil
}

var _ CallOption = EmptyCallOption{} // ensure EmptyCallOption implements the interface

func (s) TestSimpleParsing(t *testing.T) {
	bigMsg := bytes.Repeat([]byte{'x'}, 1<<24)
	for _, test := range []struct {
		// input
		p []byte
		// outputs
		err error
		b   []byte
		pt  payloadFormat
	}{
		{nil, io.EOF, nil, compressionNone},
		{[]byte{0, 0, 0, 0, 0}, nil, nil, compressionNone},
		{[]byte{0, 0, 0, 0, 1, 'a'}, nil, []byte{'a'}, compressionNone},
		{[]byte{1, 0}, io.ErrUnexpectedEOF, nil, compressionNone},
		{[]byte{0, 0, 0, 0, 10, 'a'}, io.ErrUnexpectedEOF, nil, compressionNone},
		// Check that messages with length >= 2^24 are parsed.
		{append([]byte{0, 1, 0, 0, 0}, bigMsg...), nil, bigMsg, compressionNone},
	} {
		buf := &fullReader{test.p}
		parser := &parser{r: buf, bufferPool: mem.DefaultBufferPool()}
		pt, b, err := parser.recvMsg(math.MaxInt32)
		if err != test.err || !bytes.Equal(b.Materialize(), test.b) || pt != test.pt {
			t.Fatalf("parser{%v}.recvMsg(_) = %v, %v, %v\nwant %v, %v, %v", test.p, pt, b, err, test.pt, test.b, test.err)
		}
	}
}

func (s) TestMultipleParsing(t *testing.T) {
	// Set a byte stream consists of 3 messages with their headers.
	p := []byte{0, 0, 0, 0, 1, 'a', 0, 0, 0, 0, 2, 'b', 'c', 0, 0, 0, 0, 1, 'd'}
	b := &fullReader{p}
	parser := &parser{r: b, bufferPool: mem.DefaultBufferPool()}

	wantRecvs := []struct {
		pt   payloadFormat
		data []byte
	}{
		{compressionNone, []byte("a")},
		{compressionNone, []byte("bc")},
		{compressionNone, []byte("d")},
	}
	for i, want := range wantRecvs {
		pt, data, err := parser.recvMsg(math.MaxInt32)
		if err != nil || pt != want.pt || !reflect.DeepEqual(data.Materialize(), want.data) {
			t.Fatalf("after %d calls, parser{%v}.recvMsg(_) = %v, %v, %v\nwant %v, %v, <nil>",
				i, p, pt, data, err, want.pt, want.data)
		}
	}

	pt, data, err := parser.recvMsg(math.MaxInt32)
	if err != io.EOF {
		t.Fatalf("after %d recvMsgs calls, parser{%v}.recvMsg(_) = %v, %v, %v\nwant _, _, %v",
			len(wantRecvs), p, pt, data, err, io.EOF)
	}
}

func (s) TestEncode(t *testing.T) {
	for _, test := range []struct {
		// input
		msg proto.Message
		// outputs
		hdr  []byte
		data []byte
		err  error
	}{
		{nil, []byte{0, 0, 0, 0, 0}, []byte{}, nil},
	} {
		data, err := encode(getCodec(protoenc.Name), test.msg)
		if err != test.err || !bytes.Equal(data.Materialize(), test.data) {
			t.Errorf("encode(_, %v) = %v, %v; want %v, %v", test.msg, data, err, test.data, test.err)
			continue
		}
		if hdr, _ := msgHeader(data, nil, compressionNone); !bytes.Equal(hdr, test.hdr) {
			t.Errorf("msgHeader(%v, false) = %v; want %v", data, hdr, test.hdr)
		}
	}
}

func (s) TestCompress(t *testing.T) {
	bestCompressor, err := NewGZIPCompressorWithLevel(gzip.BestCompression)
	if err != nil {
		t.Fatalf("Could not initialize gzip compressor with best compression.")
	}
	bestSpeedCompressor, err := NewGZIPCompressorWithLevel(gzip.BestSpeed)
	if err != nil {
		t.Fatalf("Could not initialize gzip compressor with best speed compression.")
	}

	defaultCompressor, err := NewGZIPCompressorWithLevel(gzip.BestSpeed)
	if err != nil {
		t.Fatalf("Could not initialize gzip compressor with default compression.")
	}

	level5, err := NewGZIPCompressorWithLevel(5)
	if err != nil {
		t.Fatalf("Could not initialize gzip compressor with level 5 compression.")
	}

	for _, test := range []struct {
		// input
		data []byte
		cp   Compressor
		dc   Decompressor
		// outputs
		err error
	}{
		{make([]byte, 1024), NewGZIPCompressor(), NewGZIPDecompressor(), nil},
		{make([]byte, 1024), bestCompressor, NewGZIPDecompressor(), nil},
		{make([]byte, 1024), bestSpeedCompressor, NewGZIPDecompressor(), nil},
		{make([]byte, 1024), defaultCompressor, NewGZIPDecompressor(), nil},
		{make([]byte, 1024), level5, NewGZIPDecompressor(), nil},
	} {
		b := new(bytes.Buffer)
		if err := test.cp.Do(b, test.data); err != test.err {
			t.Fatalf("Compressor.Do(_, %v) = %v, want %v", test.data, err, test.err)
		}
		if b.Len() >= len(test.data) {
			t.Fatalf("The compressor fails to compress data.")
		}
		if p, err := test.dc.Do(b); err != nil || !bytes.Equal(test.data, p) {
			t.Fatalf("Decompressor.Do(%v) = %v, %v, want %v, <nil>", b, p, err, test.data)
		}
	}
}

func (s) TestToRPCErr(t *testing.T) {
	for _, test := range []struct {
		// input
		errIn error
		// outputs
		errOut error
	}{
		{transport.ErrConnClosing, status.Error(codes.Unavailable, transport.ErrConnClosing.Desc)},
		{io.ErrUnexpectedEOF, status.Error(codes.Internal, io.ErrUnexpectedEOF.Error())},
	} {
		err := toRPCErr(test.errIn)
		if _, ok := status.FromError(err); !ok {
			t.Errorf("toRPCErr{%v} returned type %T, want %T", test.errIn, err, status.Error)
		}
		if !testutils.StatusErrEqual(err, test.errOut) {
			t.Errorf("toRPCErr{%v} = %v \nwant %v", test.errIn, err, test.errOut)
		}
	}
}

// bmEncode benchmarks encoding a Protocol Buffer message containing mSize
// bytes.
func bmEncode(b *testing.B, mSize int) {
	cdc := getCodec(protoenc.Name)
	msg := &perfpb.Buffer{Body: make([]byte, mSize)}
	encodeData, _ := encode(cdc, msg)
	encodedSz := int64(len(encodeData))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encode(cdc, msg)
	}
	b.SetBytes(encodedSz)
}

func BenchmarkEncode1B(b *testing.B) {
	bmEncode(b, 1)
}

func BenchmarkEncode1KiB(b *testing.B) {
	bmEncode(b, 1024)
}

func BenchmarkEncode8KiB(b *testing.B) {
	bmEncode(b, 8*1024)
}

func BenchmarkEncode64KiB(b *testing.B) {
	bmEncode(b, 64*1024)
}

func BenchmarkEncode512KiB(b *testing.B) {
	bmEncode(b, 512*1024)
}

func BenchmarkEncode1MiB(b *testing.B) {
	bmEncode(b, 1024*1024)
}

// bmCompressor benchmarks a compressor of a Protocol Buffer message containing
// mSize bytes.
func bmCompressor(b *testing.B, mSize int, cp Compressor) {
	payload := make([]byte, mSize)
	cBuf := bytes.NewBuffer(make([]byte, mSize))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cp.Do(cBuf, payload)
		cBuf.Reset()
	}
}

func BenchmarkGZIPCompressor1B(b *testing.B) {
	bmCompressor(b, 1, NewGZIPCompressor())
}

func BenchmarkGZIPCompressor1KiB(b *testing.B) {
	bmCompressor(b, 1024, NewGZIPCompressor())
}

func BenchmarkGZIPCompressor8KiB(b *testing.B) {
	bmCompressor(b, 8*1024, NewGZIPCompressor())
}

func BenchmarkGZIPCompressor64KiB(b *testing.B) {
	bmCompressor(b, 64*1024, NewGZIPCompressor())
}

func BenchmarkGZIPCompressor512KiB(b *testing.B) {
	bmCompressor(b, 512*1024, NewGZIPCompressor())
}

func BenchmarkGZIPCompressor1MiB(b *testing.B) {
	bmCompressor(b, 1024*1024, NewGZIPCompressor())
}

type mockCompressor struct {
	DecompressedData []byte
	ErrDecompress    error
	Size             int
	CustomReader     io.Reader
}

func (m *mockCompressor) Compress(w io.Writer) (io.WriteCloser, error) {
	return nil, nil
}

func (m *mockCompressor) Decompress(r io.Reader) (io.Reader, error) {
	if m.ErrDecompress != nil {
		return nil, m.ErrDecompress
	}
	if m.CustomReader != nil {
		return m.CustomReader, nil
	}
	return bytes.NewReader(m.DecompressedData), nil
}

func (m *mockCompressor) DecompressedSize(compressedBytes mem.BufferSlice) int {
	return m.Size
}

func (m *mockCompressor) Name() string {
	return "mockCompressor"
}

type ErrorReader struct{}

func (e *ErrorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("simulated io.Copy read error")
}

func TestDecompress(t *testing.T) {
	tests := []struct {
		name                  string
		compressor            encoding.Compressor
		input                 mem.BufferSlice
		maxReceiveMessageSize int
		want                  mem.BufferSlice
		size                  int
		error                 error
	}{
		{
			name: "Successful decompression",
			compressor: &mockCompressor{
				DecompressedData: []byte("decompressed data"),
				Size:             17,
			},
			input:                 mem.BufferSlice{},
			maxReceiveMessageSize: 100,
			want: func() mem.BufferSlice {
				decompressed := []byte("decompressed data")
				return mem.BufferSlice{mem.NewBuffer(&decompressed, nil)}
			}(),
			size:  1,
			error: nil,
		},
		{
			name: "Error during decompression",
			compressor: &mockCompressor{
				ErrDecompress: errors.New("decompression error"),
			},
			input:                 mem.BufferSlice{},
			maxReceiveMessageSize: 100,
			want:                  nil,
			size:                  0,
			error:                 errors.New("decompression error"),
		},
		{
			name: "Buffer overflow",
			compressor: &mockCompressor{
				DecompressedData: []byte("overflow data"),
				Size:             100,
			},
			input:                 mem.BufferSlice{},
			maxReceiveMessageSize: 5,
			want:                  nil,
			size:                  6,
			error:                 errors.New("overflow: message larger than max size receivable by client (5 bytes)"),
		},
		{
			name: "MaxInt64 receive size with small data",
			compressor: &mockCompressor{
				DecompressedData: []byte("small data"),
				Size:             10,
			},
			input:                 mem.BufferSlice{},
			maxReceiveMessageSize: math.MaxInt64,
			want: func() mem.BufferSlice {
				smallDecompressed := []byte("small data, small data ")
				return mem.BufferSlice{mem.NewBuffer(&smallDecompressed, nil)}
			}(),
			size:  1,
			error: nil,
		}, {
			name: "Error during io.Copy",
			compressor: &mockCompressor{
				CustomReader: &ErrorReader{},
			},
			input:                 mem.BufferSlice{},
			maxReceiveMessageSize: 100,
			want:                  nil,
			size:                  0,
			error:                 errors.New("simulated io.Copy read error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, size, err := decompress(tt.compressor, tt.input, tt.maxReceiveMessageSize, nil)
			// check both err and tt.error are either nil or non-nil, the condition will be false
			if (err != nil) != (tt.error != nil) {
				t.Errorf("unexpected error state: got err=%v, want err=%v", err, tt.error)
			}
			if size != tt.size {
				t.Errorf("decompress() size = %d, want %d", size, tt.size)
			}
			if len(tt.want) != len(output) {
				t.Errorf("decompress() output length = %d, want %d", output, tt.want)
			}
		})
	}
}
