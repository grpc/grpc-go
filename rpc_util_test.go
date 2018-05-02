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
	"reflect"
	"testing"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	protoenc "google.golang.org/grpc/encoding/proto"
	"google.golang.org/grpc/status"
	perfpb "google.golang.org/grpc/test/codec_perf"
	"google.golang.org/grpc/transport"
)

type fullReader struct {
	reader io.Reader
}

func (f fullReader) Read(p []byte) (int, error) {
	return io.ReadFull(f.reader, p)
}

var _ CallOption = EmptyCallOption{} // ensure EmptyCallOption implements the interface

func TestEncode(t *testing.T) {
	for _, test := range []struct {
		// input
		msg proto.Message
		cp  Compressor
		// outputs
		data []byte
		err  error
	}{
		{nil, nil, []byte{}, nil},
	} {
		data, err := encode(encoding.GetCodec(protoenc.Name), test.msg, nil, nil, nil)
		if err != test.err || !bytes.Equal(data, test.data) {
			t.Fatalf("encode(_, _, %v, _) = %v, %v\nwant %v, %v", test.cp, data, err, test.data, test.err)
		}
	}
}

func TestCompress(t *testing.T) {

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

func TestToRPCErr(t *testing.T) {
	for _, test := range []struct {
		// input
		errIn error
		// outputs
		errOut error
	}{
		{transport.StreamError{Code: codes.Unknown, Desc: ""}, status.Error(codes.Unknown, "")},
		{transport.ErrConnClosing, status.Error(codes.Unavailable, transport.ErrConnClosing.Desc)},
	} {
		err := toRPCErr(test.errIn)
		if _, ok := status.FromError(err); !ok {
			t.Fatalf("toRPCErr{%v} returned type %T, want %T", test.errIn, err, status.Error(codes.Unknown, ""))
		}
		if !reflect.DeepEqual(err, test.errOut) {
			t.Fatalf("toRPCErr{%v} = %v \nwant %v", test.errIn, err, test.errOut)
		}
	}
}

func TestParseDialTarget(t *testing.T) {
	for _, test := range []struct {
		target, wantNet, wantAddr string
	}{
		{"unix:etcd:0", "unix", "etcd:0"},
		{"unix:///tmp/unix-3", "unix", "/tmp/unix-3"},
		{"unix://domain", "unix", "domain"},
		{"unix://etcd:0", "unix", "etcd:0"},
		{"unix:///etcd:0", "unix", "/etcd:0"},
		{"passthrough://unix://domain", "tcp", "passthrough://unix://domain"},
		{"https://google.com:443", "tcp", "https://google.com:443"},
		{"dns:///google.com", "tcp", "dns:///google.com"},
		{"/unix/socket/address", "tcp", "/unix/socket/address"},
	} {
		gotNet, gotAddr := parseDialTarget(test.target)
		if gotNet != test.wantNet || gotAddr != test.wantAddr {
			t.Errorf("parseDialTarget(%q) = %s, %s want %s, %s", test.target, gotNet, gotAddr, test.wantNet, test.wantAddr)
		}
	}
}

// bmEncode benchmarks encoding a Protocol Buffer message containing mSize
// bytes.
func bmEncode(b *testing.B, mSize int) {
	cdc := encoding.GetCodec(protoenc.Name)
	msg := &perfpb.Buffer{Body: make([]byte, mSize)}
	encodeData, _ := encode(cdc, msg, nil, nil, nil)
	// 5 bytes of gRPC-specific message header
	// is added to the message before it is written
	// to the wire.
	encodedSz := int64(5 + len(encodeData))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encode(cdc, msg, nil, nil, nil)
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
