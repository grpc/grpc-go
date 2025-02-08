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

const (
	defaultDecompressedData = "default decompressed data"
	decompressionErrorMsg   = "invalid compression format"
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

// compressWithDeterministicError compresses the input data and returns a BufferSlice.
func compressWithDeterministicError(t *testing.T, input []byte) mem.BufferSlice {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(input); err != nil {
		t.Fatalf("compressInput() failed to write data: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("compressInput() failed to close gzip writer: %v", err)
	}
	compressedData := buf.Bytes()
	return mem.BufferSlice{mem.NewBuffer(&compressedData, nil)}
}

// MockDecompressor is a mock implementation of a decompressor used for testing purposes.
// It simulates decompression behavior, returning either decompressed data or an error based on the ShouldError flag.
type MockDecompressor struct {
	ShouldError bool // Flag to control whether the decompression should simulate an error.
}

// Do simulates decompression. It returns a predefined error if ShouldError is true,
// or a fixed set of decompressed data if ShouldError is false.
func (m *MockDecompressor) Do(_ io.Reader) ([]byte, error) {
	if m.ShouldError {
		return nil, errors.New(decompressionErrorMsg)
	}
	return []byte(defaultDecompressedData), nil
}

// Type returns the string identifier for the MockDecompressor.
func (m *MockDecompressor) Type() string {
	return "MockDecompressor"
}

// TestDecompress tests the decompress function behaves correctly for following scenarios
// decompress successfully when message is <= maxReceiveMessageSize
// errors when message > maxReceiveMessageSize
// decompress successfully when maxReceiveMessageSize is MaxInt
// errors when the decompressed message has an invalid format
// errors when the decompressed message exceeds the maxReceiveMessageSize.
func (s) TestDecompress(t *testing.T) {
	compressor := encoding.GetCompressor("gzip")
	validDecompressor := &MockDecompressor{ShouldError: false}
	invalidFormatDecompressor := &MockDecompressor{ShouldError: true}

	testCases := []struct {
		name                  string
		input                 mem.BufferSlice
		dc                    Decompressor
		maxReceiveMessageSize int
		want                  []byte
		wantErr               error
	}{
		{
			name:                  "Decompresses successfully with sufficient buffer size",
			input:                 compressWithDeterministicError(t, []byte("decompressed data")),
			dc:                    nil,
			maxReceiveMessageSize: 50,
			want:                  []byte("decompressed data"),
			wantErr:               nil,
		},
		{
			name:                  "Fails due to exceeding maxReceiveMessageSize",
			input:                 compressWithDeterministicError(t, []byte("message that is too large")),
			dc:                    nil,
			maxReceiveMessageSize: len("message that is too large") - 1,
			want:                  nil,
			wantErr:               status.Errorf(codes.ResourceExhausted, "grpc: received message after decompression larger than max %d", len("message that is too large")-1),
		},
		{
			name:                  "Decompresses to exactly maxReceiveMessageSize",
			input:                 compressWithDeterministicError(t, []byte("exact size message")),
			dc:                    nil,
			maxReceiveMessageSize: len("exact size message"),
			want:                  []byte("exact size message"),
			wantErr:               nil,
		},
		{
			name:                  "Decompresses successfully with maxReceiveMessageSize MaxInt",
			input:                 compressWithDeterministicError(t, []byte("large message")),
			dc:                    nil,
			maxReceiveMessageSize: math.MaxInt,
			want:                  []byte("large message"),
			wantErr:               nil,
		},
		{
			name:                  "Fails with decompression error due to invalid format",
			input:                 compressWithDeterministicError(t, []byte("invalid compressed data")),
			dc:                    invalidFormatDecompressor,
			maxReceiveMessageSize: 50,
			want:                  nil,
			wantErr:               status.Errorf(codes.Internal, "grpc: failed to decompress the received message: %v", errors.New(decompressionErrorMsg)),
		},
		{
			name:                  "Fails with resourceExhausted error when decompressed message exceeds maxReceiveMessageSize",
			input:                 compressWithDeterministicError(t, []byte("large compressed data")),
			dc:                    validDecompressor,
			maxReceiveMessageSize: 20,
			want:                  nil,
			wantErr:               status.Errorf(codes.ResourceExhausted, "grpc: message after decompression larger than max (%d vs. %d)", 25, 20),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			output, err := decompress(compressor, tc.input, tc.dc, tc.maxReceiveMessageSize, mem.DefaultBufferPool())
			if !cmp.Equal(err, tc.wantErr, cmpopts.EquateErrors()) {
				t.Fatalf("decompress() err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !cmp.Equal(tc.want, output.Materialize()) {
				t.Fatalf("decompress() output mismatch: got = %v, want = %v", output.Materialize(), tc.want)
			}
		})
	}
}
