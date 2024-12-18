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
	"io"
	"math"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	_ "google.golang.org/grpc/encoding/gzip"
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

func (f *fullReader) ReadMessageHeader(header []byte) error {
	buf, err := f.Read(len(header))
	defer buf.Free()
	if err != nil {
		return err
	}

	buf.CopyTo(header)
	return nil
}

func (f *fullReader) Read(n int) (mem.BufferSlice, error) {
	if n == 0 {
		return nil, nil
	}

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

// compressData compresses data using gzip and returns the compressed bytes.
// It now accepts *testing.T to handle errors during compression.
func compressData(t *testing.T, data []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		t.Fatalf("compressData() failed to write data: %v", err)
	}

	if err := gz.Close(); err != nil {
		t.Fatalf("compressData() failed to close gzip writer: %v", err)
	}
	return buf.Bytes()
}

// compressInput compresses input data and returns a BufferSlice.
func compressInput(input []byte) mem.BufferSlice {
	compressedData := compressData(nil, input)
	return mem.BufferSlice{mem.NewBuffer(&compressedData, nil)}
}

// TestDecompress tests the decompress function with various scenarios, including
// successful decompression, error handling, and edge cases like overflow or
// premature data end. It ensures that the function behaves correctly with different
// inputs, buffer sizes, and error conditions, using the "gzip" compressor for testing.
func TestDecompress(t *testing.T) {
	c := encoding.GetCompressor("gzip")

	tests := []struct {
		name                  string
		compressor            encoding.Compressor
		input                 []byte
		maxReceiveMessageSize int
		want                  []byte
		wantErr               error
	}{
		{
			name:                  "Decompresses successfully with sufficient buffer size",
			compressor:            c,
			input:                 []byte("decompressed data"),
			maxReceiveMessageSize: 50,
			want:                  []byte("decompressed data"),
			wantErr:               nil,
		},
		{
			name:                  "Fails due to exceeding maxReceiveMessageSize",
			compressor:            c,
			input:                 []byte("small message that is too large"),
			maxReceiveMessageSize: 5,
			want:                  nil,
			wantErr:               errMaxMessageSizeExceeded,
		},
		{
			name:                  "Decompresses to exactly maxReceiveMessageSize",
			compressor:            c,
			input:                 []byte("exact size message"),
			maxReceiveMessageSize: len("exact size message"),
			want:                  []byte("exact size message"),
			wantErr:               nil,
		},
		{
			name:                  "Handles maxReceiveMessageSize as MaxInt",
			compressor:            c,
			input:                 []byte("small message"),
			maxReceiveMessageSize: math.MaxInt,
			want:                  []byte("small message"),
			wantErr:               nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressedMsg := compressInput(tt.input)
			output, err := decompress(tt.compressor, compressedMsg, tt.maxReceiveMessageSize, mem.DefaultBufferPool())

			if !cmp.Equal(err, tt.wantErr, cmpopts.EquateErrors()) {
				t.Fatalf("decompress() error = %v, wantErr = %v", err, tt.wantErr)
			}

			if diff := cmp.Diff(tt.want, output.Materialize()); diff != "" {
				t.Fatalf("decompress() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
